package web

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bridge-gateway/domain"
)

// embeddingsController groups the embeddings tester: engine selection, vector
// computation, and cosine similarity rendering.
type embeddingsController struct {
	uiShared
	embeddings embeddingService
}

// pageEmbeddings renders the tester. The page exposes the curated embedding
// model list (domain.EmbeddingChoices) completed by catalog models declared as
// embeddings: the engine selected here is what counts, never the project
// configuration. The cURL sample starts from the default model and follows the
// selection through updateCurlSnippet.
func (s *embeddingsController) pageEmbeddings(w http.ResponseWriter, r *http.Request) {
	baseURL := chatAPIBaseURL(r)
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	curlSnippet := fmt.Sprintf(`curl %s/v1/embeddings \
  -H "Authorization: Bearer <VOTRE_CLE_API>" \
  -H "Content-Type: application/json" \
  -d '{"model":"%s","input":"Le chat dort paisiblement sur le canapé."}'`, baseURL, domain.ModelLocalEmbedding)

	choices, err := s.embeddingChoices(r.Context())
	if err != nil {
		s.renderToast(w, "#embed-error", err.Error())
		return
	}
	s.render(w, "page_embeddings", map[string]any{
		"Active":       "embeddings",
		"Title":        "Embeddings",
		"BaseURL":      baseURL,
		"DefaultModel": domain.ModelLocalEmbedding,
		"EmbedChoices": choices,
		"CurlSnippet":  curlSnippet,
	})
}

func (s *embeddingsController) computeEmbeddings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderToast(w, "#embed-error", "requête invalide")
		return
	}

	choice, msg := s.resolveEmbeddingChoice(r.Context(), r.FormValue("embed_model"))
	if msg != "" {
		s.renderToast(w, "#embed-error", msg)
		return
	}

	inputs, mode, dims, msg := parseEmbeddingInput(r)
	if msg != "" {
		s.renderToast(w, "#embed-error", msg)
		return
	}
	req, err := newEmbeddingRequest(inputs, dims)
	if err != nil {
		s.renderToast(w, "#embed-error", err.Error())
		return
	}

	resp, latency, msg := s.runEmbedding(r.Context(), choice, req)
	if msg != "" {
		s.renderToast(w, "#embed-error", msg)
		return
	}
	s.renderEmbeddingResult(w, resp, inputs, mode, latency)
}

// runEmbedding calls the selected engine. The tester ignores the project
// configuration and derives the engine from the chosen model (local, ONNX,
// Ollama); a provider failure propagates as-is, with no silent fallback.
func (s *embeddingsController) runEmbedding(ctx context.Context, choice *domain.EmbeddingChoice, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, int64, string) {
	cfg := &domain.ChatConfig{
		Name:     "Testeur d'embeddings",
		Provider: choice.Provider,
		Model:    choice.Model,
	}
	start := time.Now()
	resp, err := s.embeddings.EmbedWithConfig(ctx, cfg, req)
	if err != nil {
		return nil, 0, err.Error()
	}
	if len(resp.Data) == 0 {
		return nil, 0, "aucun vecteur renvoyé par le modèle"
	}
	return resp, time.Since(start).Milliseconds(), ""
}

// resolveEmbeddingChoice validates the selected model against the curated list
// and catalog. It returns a user-facing message on failure.
func (s *embeddingsController) resolveEmbeddingChoice(ctx context.Context, raw string) (*domain.EmbeddingChoice, string) {
	modelID := strings.TrimSpace(raw)
	if modelID == "" {
		modelID = domain.ModelLocalEmbedding
	}
	choice, err := s.findEmbeddingChoice(ctx, modelID)
	if err != nil {
		return nil, err.Error()
	}
	if choice == nil {
		return nil, "modèle d'embedding inconnu : " + modelID
	}
	return choice, ""
}

// parseEmbeddingInput reads the text(s), comparison mode, and optional
// dimensions from the submitted form.
func parseEmbeddingInput(r *http.Request) (inputs []string, mode string, dims *int, msg string) {
	mode = strings.TrimSpace(r.FormValue("mode"))
	if dStr := strings.TrimSpace(r.FormValue("dimensions")); dStr != "" {
		if d, err := strconv.Atoi(dStr); err == nil && d > 0 {
			dims = &d
		}
	}
	if mode == "compare" {
		a := strings.TrimSpace(r.FormValue("input_text_a"))
		b := strings.TrimSpace(r.FormValue("input_text_b"))
		if a == "" || b == "" {
			return nil, mode, dims, "les deux textes sont requis pour la comparaison"
		}
		return []string{a, b}, mode, dims, ""
	}
	text := strings.TrimSpace(r.FormValue("input_text"))
	if text == "" {
		return nil, mode, dims, "le texte à encoder est requis"
	}
	return []string{text}, mode, dims, ""
}

// newEmbeddingRequest encodes a single text as a JSON string and several texts
// as a JSON array, matching the OpenAI embeddings payload.
func newEmbeddingRequest(inputs []string, dims *int) (*domain.EmbeddingRequest, error) {
	var raw []byte
	var err error
	if len(inputs) == 1 {
		raw, err = json.Marshal(inputs[0])
	} else {
		raw, err = json.Marshal(inputs)
	}
	if err != nil {
		return nil, err
	}
	return &domain.EmbeddingRequest{Input: raw, Dimensions: dims}, nil
}

// renderEmbeddingResult renders vectors and, in comparison mode, the cosine
// similarity between the first two embeddings.
func (s *embeddingsController) renderEmbeddingResult(w http.ResponseWriter, resp *domain.EmbeddingResponse, inputs []string, mode string, latency int64) {
	hasSim := false
	var sim, simPct, simBarWidth float64
	if mode == "compare" && len(resp.Data) >= 2 {
		hasSim = true
		sim = cosineSimilarity(resp.Data[0].Embedding, resp.Data[1].Embedding)
		simPct = sim * 100
		simBarWidth = math.Max(0, math.Min(100, sim*100))
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "embed_result", map[string]any{
		"Model":              resp.Model,
		"Dimensions":         len(resp.Data[0].Embedding),
		"Usage":              resp.Usage,
		"LatencyMS":          latency,
		"Items":              buildEmbedResultItems(resp.Data, inputs),
		"HasSimilarity":      hasSim,
		"Similarity":         sim,
		"SimilarityPercent":  simPct,
		"SimilarityBarWidth": simBarWidth,
	})
}
