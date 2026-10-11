package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"bridge-gateway/domain"
)

// Le streaming a son propre fichier : la reponse non streamee tient dans le
// handler de chat, la reponse SSE partage avec elle les enveloppes d'erreur
// mais a une surface et une temporisation toutes deux propres. Le protocole
// (types de trames et writer) vit dans sse.go.

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
