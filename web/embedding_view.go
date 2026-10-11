package web

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"bridge-gateway/domain"
)

// Mise en forme des vecteurs pour le rendu du testeur d'embeddings : echantillon
// de barres normalise, export JSON et CSV, similarite cosinus.

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

// buildEmbedResultItems formate chaque vecteur pour l'affichage : libelle,
// dimension, echantillon de barres, export JSON et CSV.
func buildEmbedResultItems(data []domain.EmbeddingItem, inputs []string) []EmbedResultItem {
	items := make([]EmbedResultItem, 0, len(data))
	for i, item := range data {
		textLabel := ""
		if i < len(inputs) {
			textLabel = inputs[i]
		}

		strFloats := make([]string, 0, len(item.Embedding))
		for _, f := range item.Embedding {
			strFloats = append(strFloats, fmt.Sprintf("%.6f", f))
		}
		jsonVec, _ := json.Marshal(item.Embedding)

		items = append(items, EmbedResultItem{
			Index:      item.Index,
			Text:       textLabel,
			Count:      len(item.Embedding),
			SampleBars: buildSampleBars(item.Embedding),
			JSONVector: string(jsonVec),
			CSVVector:  strings.Join(strFloats, ","),
		})
	}
	return items
}

// buildSampleBars rend les 40 premieres valeurs en barres dont la hauteur est
// normalisee sur la plus grande valeur absolue de l'echantillon.
func buildSampleBars(vector []float32) []EmbedSampleBar {
	if len(vector) == 0 {
		return nil
	}
	sampleCount := 40
	if len(vector) < sampleCount {
		sampleCount = len(vector)
	}
	var maxVal float32 = 0.0001
	for _, v := range vector[:sampleCount] {
		if abs := float32(math.Abs(float64(v))); abs > maxVal {
			maxVal = abs
		}
	}

	bars := make([]EmbedSampleBar, 0, sampleCount)
	for _, v := range vector[:sampleCount] {
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
	return bars
}
