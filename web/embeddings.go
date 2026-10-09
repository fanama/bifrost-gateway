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

// embeddingsController regroupe le testeur d'embeddings : choix des moteurs,
// calcul et rendu des vecteurs, similarite cosinus.
type embeddingsController struct {
	uiShared
	embeddings embeddingService
}

// ---- Embeddings ----

type EmbedSampleBar struct {
	Height   int
	Value    string
	Positive bool
}

type EmbedResultItem struct {
	Index      int
	Text       string
	Count      int
	SampleBars []EmbedSampleBar
	JSONVector string
	CSVVector  string
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		va := float64(a[i])
		vb := float64(b[i])
		dot += va * vb
		normA += va * va
		normB += vb * vb
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// embeddingChoices rend la liste complete proposee au testeur : la liste
// curatee, puis les modeles du catalogue declares de type embedding (ajoutes
// depuis la page Modeles), sans doublon par identifiant. Une erreur de lecture
// est remontee : taire le catalogue priverait silencieusement l'utilisateur
// de ses propres modeles.
func (s *embeddingsController) embeddingChoices(ctx context.Context) ([]domain.EmbeddingChoice, error) {
	choices := make([]domain.EmbeddingChoice, 0, len(domain.EmbeddingChoices)+4)
	seen := make(map[string]bool, len(domain.EmbeddingChoices)+4)
	for _, c := range domain.EmbeddingChoices {
		choices = append(choices, c)
		seen[strings.ToLower(c.Model)] = true
	}
	models, err := s.models.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range models {
		choice, ok := domain.EmbeddingChoiceFromModel(m)
		if !ok {
			continue
		}
		key := strings.ToLower(choice.Model)
		if seen[key] {
			continue
		}
		seen[key] = true
		choices = append(choices, choice)
	}
	return choices, nil
}

// findEmbeddingChoice cherche un modele dans la liste complete du testeur,
// insensible a la casse : un modele issu du catalogue se selectionne et se
// resout exactement comme un choix curate.
func (s *embeddingsController) findEmbeddingChoice(ctx context.Context, modelID string) (*domain.EmbeddingChoice, error) {
	if choice := domain.FindEmbeddingChoice(modelID); choice != nil {
		return choice, nil
	}
	choices, err := s.embeddingChoices(ctx)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(modelID)
	for i := range choices {
		if strings.EqualFold(choices[i].Model, name) {
			return &choices[i], nil
		}
	}
	return nil, nil
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

	var items []EmbedResultItem
	for i, item := range resp.Data {
		textLabel := ""
		if i < len(inputs) {
			textLabel = inputs[i]
		}

		var strFloats []string
		for _, f := range item.Embedding {
			strFloats = append(strFloats, fmt.Sprintf("%.6f", f))
		}
		jsonVec, _ := json.Marshal(item.Embedding)

		sampleCount := 40
		if len(item.Embedding) < sampleCount {
			sampleCount = len(item.Embedding)
		}
		var maxVal float32 = 0.0001
		for _, v := range item.Embedding[:sampleCount] {
			abs := float32(math.Abs(float64(v)))
			if abs > maxVal {
				maxVal = abs
			}
		}

		var bars []EmbedSampleBar
		for _, v := range item.Embedding[:sampleCount] {
			abs := float32(math.Abs(float64(v)))
			h := int((abs/maxVal)*90) + 10
			if h > 100 {
				h = 100
			}
			bars = append(bars, EmbedSampleBar{
				Height:   h,
				Value:    fmt.Sprintf("%.5f", v),
				Positive: v >= 0,
			})
		}

		items = append(items, EmbedResultItem{
			Index:      item.Index,
			Text:       textLabel,
			Count:      len(item.Embedding),
			SampleBars: bars,
			JSONVector: string(jsonVec),
			CSVVector:  strings.Join(strFloats, ","),
		})
	}

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
