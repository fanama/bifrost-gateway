package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"bridge-gateway/domain"
)

// Types de la reponse non streamee et fabrication du corps JSON, partages par
// le chemin master et le chemin cle de projet.

type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int                `json:"index"`
	Message      domain.ChatMessage `json:"message"`
	FinishReason string             `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func buildCompletionResponse(model, content string) ChatCompletionResponse {
	promptTokens, completionTokens := estimateTokens(content)
	return ChatCompletionResponse{
		ID:      newCompletionID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{
			{
				Index: 0,
				Message: domain.ChatMessage{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: "stop",
			},
		},
		Usage: Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

func estimateTokens(text string) (prompt, completion int) {
	words := len(strings.Fields(text))
	completion = words/2 + 1
	return completion, completion
}

func buildResponse(req *domain.EnrichedRequest) ChatCompletionResponse {
	content := ""
	if len(req.Messages) > 0 {
		content = "Enriched request processed. Model: " + req.Model
	}

	return ChatCompletionResponse{
		ID:      newCompletionID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []Choice{
			{
				Index: 0,
				Message: domain.ChatMessage{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: "stop",
			},
		},
		Usage: Usage{
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		},
	}
}

// newCompletionID fournit un identifiant unique par reponse, au format
// "chatcmpl-<hex>" comme chez OpenAI. Un identifiant constant casse les clients
// qui indexent leurs traces ou leur cache dessus. domain.NewID() n'est pas
// reutilise ici : son prefixe "cfg-" est un detail interne qui n'a rien a
// faire dans une reponse publique.
func newCompletionID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "chatcmpl-000000000000000000000000"
	}
	return "chatcmpl-" + hex.EncodeToString(b)
}
