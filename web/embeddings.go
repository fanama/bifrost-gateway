package web

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bridge-gateway/domain"
)

// embeddingsController regroupe le testeur d'embeddings : choix des moteurs,
// calcul et rendu des vecteurs, similarite cosinus.
type embeddingsController struct {
	uiShared
	embeddings embeddingService
}

// pageEmbeddings rend le testeur. La page expose la liste curatee des
// modeles d'embedding (domain.EmbeddingChoices) completee par les modeles du
// catalogue declares de type embedding : c'est le moteur selectionne ici qui
// compte, jamais la configuration du projet. L'exemple cURL part du modele
// par defaut et suit la selection via updateCurlSnippet.
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

	modelID := strings.TrimSpace(r.FormValue("embed_model"))
	if modelID == "" {
		modelID = domain.ModelLocalEmbedding
	}
	choice, err := s.findEmbeddingChoice(r.Context(), modelID)
	if err != nil {
		s.renderToast(w, "#embed-error", err.Error())
		return
	}
	if choice == nil {
		s.renderToast(w, "#embed-error", "modèle d'embedding inconnu : "+modelID)
		return
	}

	mode := strings.TrimSpace(r.FormValue("mode"))
	var dims *int
	if dStr := strings.TrimSpace(r.FormValue("dimensions")); dStr != "" {
		if d, err := strconv.Atoi(dStr); err == nil && d > 0 {
			dims = &d
		}
	}

	var inputs []string
	if mode == "compare" {
		txtA := strings.TrimSpace(r.FormValue("input_text_a"))
		txtB := strings.TrimSpace(r.FormValue("input_text_b"))
		if txtA == "" || txtB == "" {
			s.renderToast(w, "#embed-error", "les deux textes sont requis pour la comparaison")
			return
		}
		inputs = []string{txtA, txtB}
	} else {
		txt := strings.TrimSpace(r.FormValue("input_text"))
		if txt == "" {
			s.renderToast(w, "#embed-error", "le texte à encoder est requis")
			return
		}
		inputs = []string{txt}
	}

	var rawInput []byte
	if len(inputs) == 1 {
		rawInput, err = json.Marshal(inputs[0])
	} else {
		rawInput, err = json.Marshal(inputs)
	}
	if err != nil {
		s.renderToast(w, "#embed-error", err.Error())
		return
	}

	req := &domain.EmbeddingRequest{
		Input:      rawInput,
		Dimensions: dims,
	}

	// Le testeur ne choisit que dans la liste curatee ou le catalogue
	// (modeles type embedding) : la configuration du projet est ignoree, le
	// moteur derive du modele selectionne (local, ONNX, Ollama). Un echec de
	// moteur remonte tel quel, sans repli silencieux vers un autre provider.
	selectedCfg := &domain.ChatConfig{
		Name:     "Testeur d'embeddings",
		Provider: choice.Provider,
		Model:    choice.Model,
	}

	start := time.Now()
	resp, err := s.embeddings.EmbedWithConfig(r.Context(), selectedCfg, req)
	if err != nil {
		s.renderToast(w, "#embed-error", err.Error())
		return
	}
	latency := time.Since(start).Milliseconds()

	if len(resp.Data) == 0 {
		s.renderToast(w, "#embed-error", "aucun vecteur renvoyé par le modèle")
		return
	}

	items := buildEmbedResultItems(resp.Data, inputs)

	hasSim := false
	var sim float64
	var simPct float64
	var simBarWidth float64
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
		"Items":              items,
		"HasSimilarity":      hasSim,
		"Similarity":         sim,
		"SimilarityPercent":  simPct,
		"SimilarityBarWidth": simBarWidth,
	})
}
