package infrastructure

import (
	"bridge-gateway/domain"
	"github.com/maximhq/bifrost/core/schemas"
)

// Ce fichier regroupe les conversions entre le domaine et le SDK Bifrost, afin
// que les adaptateurs (BifrostLLMProvider, routeur) partagent exactement les
// memes payloads et les memes lectures de reponse.

func toDomainEmbeddingResponse(resp *schemas.BifrostEmbeddingResponse, defaultModel string) *domain.EmbeddingResponse {
	if resp == nil {
		return &domain.EmbeddingResponse{
			Object: "list",
			Model:  defaultModel,
		}
	}
	model := resp.Model
	if model == "" {
		model = defaultModel
	}
	obj := resp.Object
	if obj == "" {
		obj = "list"
	}
	return &domain.EmbeddingResponse{
		Object: obj,
		Data:   toDomainEmbeddingItems(resp.Data),
		Model:  model,
		Usage:  toDomainEmbeddingUsage(resp.Usage),
	}
}

func toDomainEmbeddingItems(data []schemas.EmbeddingData) []domain.EmbeddingItem {
	items := make([]domain.EmbeddingItem, len(data))
	for i, d := range data {
		itemObj := d.Object
		if itemObj == "" {
			itemObj = "embedding"
		}
		items[i] = domain.EmbeddingItem{
			Index:        d.Index,
			Object:       itemObj,
			Embedding:    d.Embedding.EmbeddingArray,
			EmbeddingStr: d.Embedding.EmbeddingStr,
		}
	}
	return items
}

func toDomainEmbeddingUsage(usage *schemas.BifrostLLMUsage) domain.EmbeddingUsage {
	if usage == nil {
		return domain.EmbeddingUsage{}
	}
	out := domain.EmbeddingUsage{PromptTokens: usage.PromptTokens, TotalTokens: usage.TotalTokens}
	if out.TotalTokens == 0 && out.PromptTokens > 0 {
		out.TotalTokens = out.PromptTokens
	}
	return out
}

// toBifrostMessages convertit les messages du domaine vers Bifrost. Partage par
// Chat et ChatStream pour que les deux chemins envoient exactement le meme
// payload au provider.
func toBifrostMessages(messages []domain.ChatMessage) []schemas.ChatMessage {
	out := make([]schemas.ChatMessage, 0, len(messages))
	for _, m := range messages {
		content := m.Content
		out = append(out, schemas.ChatMessage{
			Role:    schemas.ChatMessageRole(m.Role),
			Content: &schemas.ChatMessageContent{ContentStr: &content},
		})
	}
	return out
}

// streamChunkError rend l'erreur d'un fragment, ou nil si le fragment porte du
// contenu. Bifrost transporte l'echec dans le meme canal que le contenu.
func streamChunkError(chunk *schemas.BifrostStreamChunk) error {
	if chunk == nil || chunk.BifrostError == nil {
		return nil
	}
	return formatBifrostError(chunk.BifrostError)
}

// streamChunkDelta extrait le texte incremental d'un fragment. ok vaut false
// quand le fragment ne porte rien d'utile : les providers emettent des
// fragments vides (role seul, heartbeats) qui ne doivent pas produire de delta.
func streamChunkDelta(chunk *schemas.BifrostStreamChunk) (delta, role string, ok bool) {
	if chunk == nil || chunk.BifrostChatResponse == nil {
		return "", "", false
	}
	choices := chunk.BifrostChatResponse.Choices
	if len(choices) == 0 {
		return "", "", false
	}
	streamChoice := choices[0].ChatStreamResponseChoice
	if streamChoice == nil || streamChoice.Delta == nil {
		return "", "", false
	}

	delta = ""
	if streamChoice.Delta.Content != nil {
		delta = *streamChoice.Delta.Content
	}
	if streamChoice.Delta.Role != nil {
		role = *streamChoice.Delta.Role
	}

	// Un fragment qui ne porte ni texte ni role est un keepalive : le relayer
	// produirait des choices vides chez le client.
	if delta == "" && role == "" {
		return "", "", false
	}
	return delta, role, true
}

func chatParams(cfg *domain.ChatConfig) *schemas.ChatParameters {
	params := &schemas.ChatParameters{
		Temperature:         cfg.Temperature,
		TopP:                cfg.TopP,
		MaxCompletionTokens: cfg.MaxTokens,
		FrequencyPenalty:    cfg.FrequencyPenalty,
		PresencePenalty:     cfg.PresencePenalty,
	}
	if m := cfg.ResponseFormatMap(); m != nil {
		v := any(m)
		params.ResponseFormat = &v
	}
	if params.Temperature == nil &&
		params.TopP == nil &&
		params.MaxCompletionTokens == nil &&
		params.FrequencyPenalty == nil &&
		params.PresencePenalty == nil &&
		params.ResponseFormat == nil {
		return nil
	}
	return params
}

func ExtractAssistantContent(resp *schemas.BifrostChatResponse) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	choice := resp.Choices[0]
	if choice.ChatNonStreamResponseChoice == nil || choice.ChatNonStreamResponseChoice.Message == nil {
		return ""
	}
	msg := choice.ChatNonStreamResponseChoice.Message
	if msg.Content != nil && msg.Content.ContentStr != nil {
		return *msg.Content.ContentStr
	}
	return ""
}
