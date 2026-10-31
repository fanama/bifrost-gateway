package chat

import (
	"testing"

	"bridge-gateway/domain/config"
	"bridge-gateway/domain/provider"
)

func TestServedByOnnxExplicitProvider(t *testing.T) {
	cfg := &config.ChatConfig{Provider: " ONNX ", Model: "anything"}
	if !ServedByOnnx(cfg, nil) {
		t.Fatal("an explicit onnx provider must be served locally")
	}
}

func TestServedByOnnxCatalogModel(t *testing.T) {
	cfg := &config.ChatConfig{Provider: "mistral", Model: "smollm2-135m-instruct"}
	model := &provider.ModelInfo{
		Name:     "SmolLM2-135M-Instruct",
		Provider: "onnx",
		Kind:     provider.ModelKindChat,
	}
	if !ServedByOnnx(cfg, model) {
		t.Fatal("a catalog chat model served by onnx must route locally")
	}
}

func TestServedByOnnxRejectsNonChatOrForeignModels(t *testing.T) {
	cfg := &config.ChatConfig{Provider: "mistral", Model: "shared-name"}

	cases := []struct {
		name  string
		model *provider.ModelInfo
	}{
		{"embedding model", &provider.ModelInfo{Name: "shared-name", Provider: "onnx", Kind: provider.ModelKindEmbedding}},
		{"remote provider", &provider.ModelInfo{Name: "shared-name", Provider: "mistral", Kind: provider.ModelKindChat}},
		{"other model", &provider.ModelInfo{Name: "another-model", Provider: "onnx", Kind: provider.ModelKindChat}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if ServedByOnnx(cfg, tc.model) {
				t.Error("a non onnx/chat catalog model must not be routed locally")
			}
		})
	}
}

func TestServedByOnnxHandlesMissingInputs(t *testing.T) {
	if ServedByOnnx(nil, &provider.ModelInfo{Provider: "onnx", Kind: provider.ModelKindChat}) {
		t.Error("a nil configuration cannot be served locally")
	}
	if ServedByOnnx(&config.ChatConfig{Provider: "mistral", Model: "x"}, nil) {
		t.Error("without a catalog match the configured provider decides")
	}
}
