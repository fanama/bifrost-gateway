package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"bridge-gateway/domain"
)

// Le streaming a son propre fichier : la reponse non streamee tient dans le
// handler de chat, la reponse SSE partage avec elle les enveloppes d'erreur
// mais a une surface et une temporisation toutes deux propres.

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

// handleMasterStream repond en SSE sur le chemin master, qui n'appelle aucun
// modele : le texte announce est celui que rendait deja la reponse unique, livre
// en un seul fragment.
func (h *ChatHandler) handleMasterStream(w http.ResponseWriter, r *http.Request, req *domain.ChatRequest) {
	if !validMessages(w, req) {
		return
	}

	enriched, err := h.enrichment.Enrich(req)
	if err != nil {
		var missingErr *domain.MissingAttributionError
		if errors.As(err, &missingErr) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
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

// handleProjectStream repond en SSE sur le chemin cle de projet, qui appelle la
// configuration active du projet.
func (h *ChatHandler) handleProjectStream(w http.ResponseWriter, r *http.Request, req *domain.ChatRequest, projectID string) {
	cfg, err := h.configs.Active(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, domain.ErrNoActiveConfig) {
			writeError(w, http.StatusBadRequest, "no active configuration for this project; activate one from the UI")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	messages := req.Messages
	if len(messages) == 0 {
		writeAPIError(w, http.StatusBadRequest, errTypeInvalidRequest, "messages", "", "messages is required")
		return
	}
	history := messages[:len(messages)-1]
	last := messages[len(messages)-1]

	stream, err := h.chat.Stream(r.Context(), cfg.ID, history, last.Content, domain.OptionsFrom(req))
	if err != nil {
		writeChatError(w, err)
		return
	}

	writeSSE(w, r, stream, cfg.Model, req.StreamOptions)
}

// writeSSE emet la sequence de trames attendue par le protocole OpenAI :
//
//  1. une trame portant le role assistant, sans texte ;
//  2. une trame par increment de texte ;
//  3. une trame de fin, avec finish_reason ;
//  4. le cas echeant, une trame de consommation (choices vide) ;
//  5. data: [DONE].
func writeSSE(w http.ResponseWriter, r *http.Request, stream <-chan domain.StreamEvent, model string, opts *domain.StreamOptions) {
	sse, ok := newSSEWriter(w)
	if !ok {
		// Sans flush le client ne recevrait tout qu'a la fin : ce serait une
		// reponse unique deguisee, pas un flux.
		writeError(w, http.StatusInternalServerError, "streaming is not supported by this server")
		return
	}

	base := ChatCompletionChunk{
		ID:      newCompletionID(),
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
	}

	sse.writeFrame(withChunk(base, model, ChunkDelta{Role: "assistant"}))

	var assembled strings.Builder

	for {
		select {
		case <-r.Context().Done():
			// Le client a coupe. Le flux amont se ferme de lui-meme, car il
			// surveille le meme contexte : rien ne fuit derriere.
			return

		case event, open := <-stream:
			if !open {
				finishStream(sse, base, model, assembled.String(), opts)
				return
			}

			if event.Err != nil {
				// Les en-tetes sont deja partis en 200 : l'echec ne peut plus
				// etre porte par un statut HTTP, il passe par une trame.
				sse.writeFrame(map[string]any{"error": map[string]any{
					"message": event.Err.Error(),
					"type":    "server_error",
					"param":   nil,
					"code":    nil,
				}})
				sse.writeDone()
				return
			}

			// Le modele du flux prime sur celui annonce : c'est lui qui a
			// reellement repondu, et le routeur a pu substituer un tier.
			if event.Model != "" {
				model = event.Model
			}
			if event.Delta != "" {
				assembled.WriteString(event.Delta)
				sse.writeFrame(withChunk(base, model, ChunkDelta{Content: event.Delta}))
			}
			if event.Done {
				finishStream(sse, base, model, assembled.String(), opts)
				return
			}
		}
	}
}

// finishStream clot une reponse streamsee : trame de fin, consommation
// optionnelle, puis [DONE].
func finishStream(sse *sseWriter, base ChatCompletionChunk, model, content string, opts *domain.StreamOptions) {
	sse.writeFrame(withChunkFinish(base, model, "stop"))

	if opts != nil && opts.IncludeUsage {
		usage := buildUsage(content)
		final := base
		final.Model = model
		// OpenAI reserve la trame de consommation : choices y est vide.
		final.Choices = []ChunkChoice{}
		final.Usage = &usage
		sse.writeFrame(final)
	}

	sse.writeDone()
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

// buildUsage reprend l'estimation de la reponse unique : un client ne doit pas
// voir deux comptes differents selon le mode.
func buildUsage(content string) Usage {
	promptTokens, completionTokens := estimateTokens(content)
	return Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}
}
