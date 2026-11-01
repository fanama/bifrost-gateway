package infrastructure

import (
	"context"
	"net/http"
	"strings"

	"bridge-gateway/domain"
	domainrag "bridge-gateway/domain/rag"
)

// VisionChat is the capability an OCR backend must provide: one image plus a
// prompt in, one text answer out. Keeping it narrow lets the OCR adapter be
// tested without a provider or an ONNX runtime.
type VisionChat interface {
	DescribeImage(ctx context.Context, cfg *domain.ChatConfig, prompt string, image []byte, mime string) (string, error)
}

const ragOCRPrompt = "Extract every piece of readable text from this image, verbatim, in reading order. " +
	"If the image contains no text, describe its content in one or two factual sentences instead. " +
	"Answer with the text only, without commentary."

// RAGImageOCR implements domainrag.ImageReader with the project's active chat
// configuration, so the model used for OCR is the one the user already routes
// traffic to. The model must accept image parts (vision) ; a text-only model
// fails with the provider's error.
type RAGImageOCR struct {
	configs domain.ChatConfigRepository
	vision  VisionChat
}

// NewRAGImageOCR wires the OCR adapter. A nil vision backend makes image
// uploads fail explicitly instead of silently indexing nothing.
func NewRAGImageOCR(configs domain.ChatConfigRepository, vision VisionChat) *RAGImageOCR {
	return &RAGImageOCR{configs: configs, vision: vision}
}

var _ domainrag.ImageReader = (*RAGImageOCR)(nil)

// ReadImage transcribes one image with the active configuration of the project.
func (o *RAGImageOCR) ReadImage(ctx context.Context, projectID, fileName string, content []byte) (string, error) {
	mime, err := imageMime(content)
	if err != nil {
		return "", err
	}
	cfg, err := o.activeConfig(ctx, projectID)
	if err != nil {
		return "", err
	}
	text, err := o.vision.DescribeImage(ctx, cfg, ragOCRPrompt, content, mime)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

func (o *RAGImageOCR) activeConfig(ctx context.Context, projectID string) (*domain.ChatConfig, error) {
	if o == nil || o.vision == nil {
		return nil, &domain.InvalidRequestError{Param: "file", Reason: "image OCR is not configured"}
	}
	cfg, err := o.configs.GetActiveByProject(ctx, projectID)
	if err != nil || cfg == nil {
		return nil, &domain.InvalidRequestError{
			Param:  "file",
			Reason: "image OCR needs an active configuration with a vision model for this project",
		}
	}
	return cfg, nil
}

// imageMime sniffs the real format instead of trusting the file name, so a
// renamed scan still reaches the model with a usable data URL.
func imageMime(content []byte) (string, error) {
	mime := http.DetectContentType(content)
	if !strings.HasPrefix(mime, "image/") {
		return "", &domain.InvalidRequestError{Param: "file", Reason: "unrecognized image format"}
	}
	return mime, nil
}
