package infrastructure

import (
	"context"
	"encoding/base64"
	"strings"

	"bridge-gateway/domain"
	"github.com/maximhq/bifrost/core/schemas"
)

// DescribeImage sends one image to a vision-capable chat model and returns its
// answer. It is the Bifrost side of the image OCR adapter: RAG keeps using the
// domainrag.ImageReader port and never sees an SDK type.
func (p *BifrostLLMProvider) DescribeImage(
	ctx context.Context,
	cfg *domain.ChatConfig,
	prompt string,
	image []byte,
	mime string,
) (string, error) {
	if len(image) == 0 {
		return "", &domain.InvalidRequestError{Param: "file", Reason: "image is empty"}
	}
	provider, err := ToModelProvider(cfg.Provider)
	if err != nil {
		return "", err
	}
	client, err := p.clientFor(ctx, cfg)
	if err != nil {
		return "", err
	}
	resp, err := client.ChatCompletion(ctx, provider, cfg.Model, imageMessages(prompt, image, mime), nil)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(ExtractAssistantContent(resp)), nil
}

// imageMessages builds a one-turn multimodal request: the instruction as a text
// block followed by the image as a base64 data URL, which every OpenAI-style
// provider and Bifrost accept.
func imageMessages(prompt string, image []byte, mime string) []schemas.ChatMessage {
	url := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(image)
	return []schemas.ChatMessage{{
		Role: schemas.ChatMessageRole("user"),
		Content: &schemas.ChatMessageContent{ContentBlocks: []schemas.ChatContentBlock{
			{Type: schemas.ChatContentBlockTypeText, Text: &prompt},
			{Type: schemas.ChatContentBlockTypeImage, ImageURLStruct: &schemas.ChatInputImage{URL: url}},
		}},
	}}
}

var _ VisionChat = (*BifrostLLMProvider)(nil)
