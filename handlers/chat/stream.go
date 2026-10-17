package chat

import (
	"net/http"
	"strings"
	"time"

	"bridge-gateway/domain"
	"bridge-gateway/handlers/httpx"
)

func (h *ChatHandler) handleMasterStream(w http.ResponseWriter, r *http.Request, req *domain.ChatRequest) {
	if !httpx.ValidMessages(w, req) {
		return
	}

	enriched, err := h.enrichment.Enrich(req)
	if err != nil {
		httpx.WriteDomainError(w, err)
		return
	}

	content := ""
	if len(enriched.Messages) > 0 {
		content = "Enriched request processed. Model: " + req.Model
	}

	stream, _ := domain.StreamViaFallback(domain.LLMResult{
		Content: content,
		Model:   req.Model,
	}, nil)
	writeSSE(w, r, stream, req.Model, req.StreamOptions)
}

func (h *ChatHandler) handleProjectStream(w http.ResponseWriter, r *http.Request, req *domain.ChatRequest, projectID string) {
	cfg, err := h.configs.Active(r.Context(), projectID)
	if err != nil {
		httpx.WriteDomainError(w, err)
		return
	}
	if !httpx.ValidMessages(w, req) {
		return
	}

	messages := req.Messages
	history := messages[:len(messages)-1]
	last := messages[len(messages)-1]

	stream, err := h.chat.Stream(r.Context(), cfg.ID, history, last.Content, domain.OptionsFrom(req))
	if err != nil {
		httpx.WriteDomainError(w, err)
		return
	}

	writeSSE(w, r, stream, cfg.Model, req.StreamOptions)
}

// writeSSE emits the OpenAI SSE chunk sequence: assistant role chunk, content
// delta chunks, finish_reason chunk, optional usage chunk, and [DONE].
func writeSSE(w http.ResponseWriter, r *http.Request, stream <-chan domain.StreamEvent, model string, opts *domain.StreamOptions) {
	sse, ok := newSSEWriter(w)
	if !ok {
		httpx.WriteError(w, http.StatusInternalServerError, "streaming is not supported by this server")
		return
	}

	base := ChatCompletionChunk{
		ID:      newCompletionID(),
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
	}
	sse.writeFrame(withChunk(base, model, ChunkDelta{Role: "assistant"}))

	pumpSSEStream(r, sse, base, stream, model, opts)
}

func pumpSSEStream(r *http.Request, sse *sseWriter, base ChatCompletionChunk, stream <-chan domain.StreamEvent, model string, opts *domain.StreamOptions) {
	var assembled strings.Builder
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-stream:
			if !open {
				finishStream(sse, base, model, assembled.String(), opts)
				return
			}
			if handleSSEEvent(sse, base, event, &model, &assembled, opts) {
				return
			}
		}
	}
}

func handleSSEEvent(sse *sseWriter, base ChatCompletionChunk, event domain.StreamEvent, model *string, assembled *strings.Builder, opts *domain.StreamOptions) bool {
	if event.Err != nil {
		sse.writeFrame(map[string]any{"error": map[string]any{
			"message": event.Err.Error(),
			"type":    httpx.ErrTypeServer,
			"param":   nil,
			"code":    nil,
		}})
		sse.writeDone()
		return true
	}
	if event.Model != "" {
		*model = event.Model
	}
	if event.Delta != "" {
		assembled.WriteString(event.Delta)
		sse.writeFrame(withChunk(base, *model, ChunkDelta{Content: event.Delta}))
	}
	if event.Done {
		finishStream(sse, base, *model, assembled.String(), opts)
		return true
	}
	return false
}

func finishStream(sse *sseWriter, base ChatCompletionChunk, model, content string, opts *domain.StreamOptions) {
	sse.writeFrame(withChunkFinish(base, model, "stop"))

	if opts != nil && opts.IncludeUsage {
		usage := buildUsage(content)
		final := base
		final.Model = model
		final.Choices = []ChunkChoice{}
		final.Usage = &usage
		sse.writeFrame(final)
	}

	sse.writeDone()
}

func buildUsage(content string) Usage {
	promptTokens, completionTokens := estimateTokens(content)
	return Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}
}
