package handlers

import (
	"encoding/json"
	"net/http"
)

// Protocole SSE compatible OpenAI : types des trames et writer qui les emet.
// Les handlers qui produisent le flux vivent dans stream.go.

// ChatCompletionChunk est la trame d'un flux SSE. L'objet vaut
// "chat.completion.chunk", et non "chat.completion" comme en reponse unique.
type ChatCompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *Usage        `json:"usage,omitempty"`
}

// ChunkChoice porte un delta partiel. finish_reason reste null jusqu'a la
// derniere trame, qui l'enseigne a "stop".
type ChunkChoice struct {
	Index        int        `json:"index"`
	Delta        ChunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

// ChunkDelta est l'increment de texte. Role n'est present que sur la premiere
// trame, Content peut etre vide.
type ChunkDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// sseWriter ecrit un flux SSE conforme au protocole OpenAI : chaque trame est un
// bloc "data: <json>" suivi d'une ligne vide, le flux se terminant par
// "data: [DONE]".
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	failed  bool
}

// newSSEWriter pose les en-tetes du flux et rend un writer. ok vaut false si le
// ResponseWriter ne sait pas vider son tampon, auquel cas le streaming est
// impossible a rendre progressivement et doit etre refuse.
func newSSEWriter(w http.ResponseWriter) (*sseWriter, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	// Empeche un reverse proxy de mettre le flux en tampon et de le restituer
	// d'un seul bloc.
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	return &sseWriter{w: w, flusher: flusher}, true
}

// writeFrame emet une trame. Un echec d'ecriture est note plutot que remonte :
// le client est parti et rien ne peut plus lui etre dit.
func (s *sseWriter) writeFrame(payload any) {
	if s.failed {
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		s.failed = true
		return
	}
	if _, err := s.w.Write([]byte("data: " + string(raw) + "\n\n")); err != nil {
		s.failed = true
		return
	}
	s.flusher.Flush()
}

// writeDone clot le flux. Sans elle, le client ne distingue pas une fin de reponse
// d'une coupure de connexion.
func (s *sseWriter) writeDone() {
	if s.failed {
		return
	}
	if _, err := s.w.Write([]byte("data: [DONE]\n\n")); err != nil {
		return
	}
	s.flusher.Flush()
}

func withChunk(base ChatCompletionChunk, model string, delta ChunkDelta) ChatCompletionChunk {
	chunk := base
	chunk.Model = model
	chunk.Choices = []ChunkChoice{{Index: 0, Delta: delta}}
	return chunk
}

func withChunkFinish(base ChatCompletionChunk, model, reason string) ChatCompletionChunk {
	chunk := base
	chunk.Model = model
	finish := reason
	chunk.Choices = []ChunkChoice{{Index: 0, Delta: ChunkDelta{}, FinishReason: &finish}}
	return chunk
}
