package infrastructure

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"strings"

	"bridge-gateway/domain"
)

// Elements de format de l'API d'embedding, partages par tous les moteurs
// (local embarque et ONNX) : validation de encoding_format, relecture de
// "input", construction de la reponse et encodage base64.

// resolveEncodingFormat valide encoding_format et rend "float" ou "base64".
// Partage par les moteurs local et ONNX pour que l'API expose les memes
// formats quel que soit le runtime qui repond.
func resolveEncodingFormat(req *domain.EmbeddingRequest) (string, error) {
	if req.EncodingFormat == nil || strings.TrimSpace(*req.EncodingFormat) == "" {
		return "float", nil
	}
	encoding := strings.ToLower(strings.TrimSpace(*req.EncodingFormat))
	if encoding != "float" && encoding != "base64" {
		return "", &domain.InvalidRequestError{Param: "encoding_format", Reason: "only float and base64 are supported"}
	}
	return encoding, nil
}

// newEmbeddingItem construit un item de reponse dans le format demande.
func newEmbeddingItem(index int, vec []float32, encoding string) domain.EmbeddingItem {
	item := domain.EmbeddingItem{Index: index, Object: "embedding"}
	if encoding == "base64" {
		encoded := encodeFloat32Base64(vec)
		item.EmbeddingStr = &encoded
		return item
	}
	item.Embedding = vec
	return item
}

// parseEmbeddingTexts decode la forme OpenAI de "input" : une chaine ou un
// tableau de chaines. Un tableau numerique (embeddings a re-encoder) est
// rejete explicitement : le runtime local n'accepte que du texte, et un
// message clair vaut mieux qu'un echec de unmarshal obscur.
func parseEmbeddingTexts(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, &domain.InvalidRequestError{Param: "input", Reason: "input is required"}
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return []string{single}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		if len(many) == 0 {
			return nil, &domain.InvalidRequestError{Param: "input", Reason: "input is required"}
		}
		return many, nil
	}
	return nil, &domain.InvalidRequestError{
		Param:  "input",
		Reason: "must be a string or an array of strings (numeric input is not supported by the local runtime)",
	}
}

// encodeFloat32Base64 rend l'encodage base64 (float32 little-endian) attendu
// par encoding_format=base64 de l'API OpenAI.
func encodeFloat32Base64(vec []float32) string {
	buf := make([]byte, 4*len(vec))
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return base64.StdEncoding.EncodeToString(buf)
}
