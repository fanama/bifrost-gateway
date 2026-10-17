package chat

import (
	"encoding/json"
	"net/http"
)

// ChatCompletionChunk is a single SSE frame in an OpenAI streaming response.
type ChatCompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *Usage        `json:"usage,omitempty"`
}

// ChunkChoice carries a partial delta; finish_reason stays null until the last frame.
type ChunkChoice struct {
	Index        int        `json:"index"`
	Delta        ChunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

// ChunkDelta is the incremental text delta; Role is only set on the first frame.
type ChunkDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// sseWriter writes OpenAI-compatible SSE frames ("data: <json>\n\n" terminated
// by "data: [DONE]\n\n").
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	failed  bool
}

func newSSEWriter(w http.ResponseWriter) (*sseWriter, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	return &sseWriter{w: w, flusher: flusher}, true
}

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
