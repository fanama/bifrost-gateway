package tests

import (
	"errors"
	"strings"
	"testing"

	"bridge-gateway/domain"
)

func TestEnrichmentInjectsSystemPromptWhenNoSystemMessage(t *testing.T) {
	svc := domain.NewEnrichmentService()
	req := &domain.ChatRequest{
		Model: "gemma4:e4b",
		Messages: []domain.ChatMessage{
			{Role: "user", Content: "hi"},
		},
		Metadata: &domain.RequestMetadata{
			TeamMetadata: &domain.TeamMetadata{
				CostCenter:   "codex",
				SystemPrompt: "You are helpful",
			},
		},
	}

	out, err := svc.Enrich(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Messages[0].Role != "system" {
		t.Errorf("expected system message first, got role=%s", out.Messages[0].Role)
	}
	if out.Messages[0].Content != "You are helpful" {
		t.Errorf("expected system prompt, got %s", out.Messages[0].Content)
	}
	if out.Messages[1].Role != "user" {
		t.Errorf("expected user message second, got role=%s", out.Messages[1].Role)
	}
}

func TestEnrichmentRaisesWhenNoMetadata(t *testing.T) {
	svc := domain.NewEnrichmentService()
	req := &domain.ChatRequest{
		Model:    "gemma4:e4b",
		Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}},
	}

	_, err := svc.Enrich(req)
	if err == nil {
		t.Fatal("expected MissingAttributionError, got nil")
	}

	var missingErr *domain.MissingAttributionError
	if !errors.As(err, &missingErr) {
		t.Fatalf("expected MissingAttributionError, got %T", err)
	}

	hasCC := false
	for _, f := range missingErr.MissingFields {
		if f == "cost_center" {
			hasCC = true
		}
	}
	if !hasCC {
		t.Errorf("expected cost_center missing, got %v", missingErr.MissingFields)
	}
}

func TestEnrichmentRaisesWhenCostCenterMissing(t *testing.T) {
	svc := domain.NewEnrichmentService()
	req := &domain.ChatRequest{
		Model:    "gemma4:e4b",
		Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}},
		Metadata: &domain.RequestMetadata{
			TeamMetadata: &domain.TeamMetadata{},
		},
	}

	_, err := svc.Enrich(req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "cost_center") {
		t.Errorf("expected error about cost_center, got: %s", err.Error())
	}
}

func TestEnrichmentPreservesExistingSystemMessage(t *testing.T) {
	svc := domain.NewEnrichmentService()
	req := &domain.ChatRequest{
		Model: "gemma4:e4b",
		Messages: []domain.ChatMessage{
			{Role: "system", Content: "user-defined"},
			{Role: "user", Content: "hi"},
		},
		Metadata: &domain.RequestMetadata{
			TeamMetadata: &domain.TeamMetadata{
				CostCenter:   "codex",
				SystemPrompt: "default",
			},
		},
	}

	out, err := svc.Enrich(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(out.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out.Messages))
	}
	if out.Messages[0].Role != "system" || out.Messages[0].Content != "user-defined" {
		t.Errorf("expected user-defined system message, got %s:%s", out.Messages[0].Role, out.Messages[0].Content)
	}
}

func TestEnrichmentPrependsSystemWhenNoSystemRole(t *testing.T) {
	svc := domain.NewEnrichmentService()
	req := &domain.ChatRequest{
		Model: "gemma4:e4b",
		Messages: []domain.ChatMessage{
			{Role: "user", Content: "hi"},
		},
		Metadata: &domain.RequestMetadata{
			TeamMetadata: &domain.TeamMetadata{
				CostCenter:   "codex",
				SystemPrompt: "be concise",
			},
		},
	}

	out, err := svc.Enrich(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Messages[0].Role != "system" || out.Messages[0].Content != "be concise" {
		t.Errorf("expected system:be concise, got %s:%s", out.Messages[0].Role, out.Messages[0].Content)
	}
	if out.Messages[1].Role != "user" || out.Messages[1].Content != "hi" {
		t.Errorf("expected user:hi, got %s:%s", out.Messages[1].Role, out.Messages[1].Content)
	}
}

func TestEnrichmentAuthMetadataOverridesTeamMetadata(t *testing.T) {
	svc := domain.NewEnrichmentService()
	req := &domain.ChatRequest{
		Model:    "gemma4:e4b",
		Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}},
		Metadata: &domain.RequestMetadata{
			TeamMetadata: &domain.TeamMetadata{
				CostCenter: "team-cc",
			},
			AuthMetadata: &domain.TeamMetadata{
				CostCenter: "key-cc",
			},
		},
	}

	out, err := svc.Enrich(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Labels["cost_center"] != "key-cc" {
		t.Errorf("expected cost_center=key-cc, got %s", out.Labels["cost_center"])
	}
}

func TestEnrichmentDefaultModelFillsMissingModel(t *testing.T) {
	svc := domain.NewEnrichmentService()
	req := &domain.ChatRequest{
		Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}},
		Metadata: &domain.RequestMetadata{
			TeamMetadata: &domain.TeamMetadata{
				CostCenter:   "codex",
				DefaultModel: "llama3",
			},
		},
	}

	out, err := svc.Enrich(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Model != "llama3" {
		t.Errorf("expected llama3, got %s", out.Model)
	}
}

func TestEnrichmentDefaultModelDoesNotOverrideExplicitModel(t *testing.T) {
	svc := domain.NewEnrichmentService()
	req := &domain.ChatRequest{
		Model:    "gemma4:e4b",
		Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}},
		Metadata: &domain.RequestMetadata{
			TeamMetadata: &domain.TeamMetadata{
				CostCenter:   "codex",
				DefaultModel: "llama3",
			},
		},
	}

	out, err := svc.Enrich(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Model != "gemma4:e4b" {
		t.Errorf("expected gemma4:e4b, got %s", out.Model)
	}
}

func TestEnrichmentResponseFormatFillsMissingField(t *testing.T) {
	svc := domain.NewEnrichmentService()
	req := &domain.ChatRequest{
		Model:    "gemma4:e4b",
		Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}},
		Metadata: &domain.RequestMetadata{
			TeamMetadata: &domain.TeamMetadata{
				CostCenter: "codex",
				ResponseFormat: map[string]any{
					"type": "json_object",
				},
			},
		},
	}

	out, err := svc.Enrich(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.ResponseFormat == nil || out.ResponseFormat["type"] != "json_object" {
		t.Errorf("expected response_format type=json_object, got %v", out.ResponseFormat)
	}
}

func TestEnrichmentReadsFromBifrostMetadata(t *testing.T) {
	svc := domain.NewEnrichmentService()
	req := &domain.ChatRequest{
		Model:    "gemma4:e4b",
		Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}},
		BifrostMetadata: &domain.RequestMetadata{
			TeamMetadata: &domain.TeamMetadata{
				CostCenter: "codex",
			},
		},
	}

	out, err := svc.Enrich(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Labels["cost_center"] != "codex" {
		t.Errorf("expected cost_center=codex, got %v", out.Labels)
	}
}

func TestEnrichmentNullAuthDoesNotShadowTeamValue(t *testing.T) {
	svc := domain.NewEnrichmentService()
	req := &domain.ChatRequest{
		Model:    "gemma4:e4b",
		Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}},
		Metadata: &domain.RequestMetadata{
			TeamMetadata: &domain.TeamMetadata{
				CostCenter: "codex",
			},
			AuthMetadata: &domain.TeamMetadata{},
		},
	}

	out, err := svc.Enrich(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Labels["cost_center"] != "codex" {
		t.Errorf("expected cost_center=codex, got %v", out.Labels)
	}
}

func TestEnrichmentFullPayloadPreservesBehavior(t *testing.T) {
	svc := domain.NewEnrichmentService()
	req := &domain.ChatRequest{
		Model: "gemma4:e4b",
		Messages: []domain.ChatMessage{
			{Role: "user", Content: "hi"},
		},
		Metadata: &domain.RequestMetadata{
			TeamMetadata: &domain.TeamMetadata{
				CostCenter:   "codex",
				DefaultModel: "llama3",
				SystemPrompt: "NA",
			},
			AuthMetadata: &domain.TeamMetadata{
				CostCenter:   "codex",
				DefaultModel: "llama3",
				SystemPrompt: "NA",
			},
		},
	}

	out, err := svc.Enrich(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Model != "gemma4:e4b" {
		t.Errorf("expected gemma4:e4b, got %s", out.Model)
	}
	if out.Labels["cost_center"] != "codex" {
		t.Errorf("expected cost_center=codex, got %v", out.Labels)
	}
	if len(out.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out.Messages))
	}
	if out.Messages[0].Role != "system" || out.Messages[0].Content != "NA" {
		t.Errorf("expected system:NA, got %s:%s", out.Messages[0].Role, out.Messages[0].Content)
	}
	if out.Messages[1].Role != "user" || out.Messages[1].Content != "hi" {
		t.Errorf("expected user:hi, got %s:%s", out.Messages[1].Role, out.Messages[1].Content)
	}
	if out.ResponseFormat != nil {
		t.Errorf("expected no response_format, got %v", out.ResponseFormat)
	}
}

func TestEnrichmentTeamMetadataPopulatesLabelsWhenAuthEmpty(t *testing.T) {
	svc := domain.NewEnrichmentService()
	req := &domain.ChatRequest{
		Model:    "gemma4:e4b",
		Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}},
		Metadata: &domain.RequestMetadata{
			TeamMetadata: &domain.TeamMetadata{
				CostCenter: "codex",
			},
			AuthMetadata: &domain.TeamMetadata{},
		},
	}

	out, err := svc.Enrich(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Labels["cost_center"] != "codex" {
		t.Errorf("expected cost_center=codex, got %v", out.Labels)
	}
}

func TestEnrichmentPreservesGenerationParams(t *testing.T) {
	svc := domain.NewEnrichmentService()
	temp := 0.4
	maxTokens := 256
	req := &domain.ChatRequest{
		Model:    "gemma4:e4b",
		Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}},
		Metadata: &domain.RequestMetadata{
			TeamMetadata: &domain.TeamMetadata{CostCenter: "codex"},
		},
		Temperature:    &temp,
		MaxTokens:      &maxTokens,
		ResponseFormat: map[string]any{"type": "json_object"},
	}

	out, err := svc.Enrich(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Temperature == nil || *out.Temperature != 0.4 {
		t.Errorf("expected temperature 0.4, got %v", out.Temperature)
	}
	if out.MaxTokens == nil || *out.MaxTokens != 256 {
		t.Errorf("expected max_tokens 256, got %v", out.MaxTokens)
	}
	if out.ResponseFormat == nil || out.ResponseFormat["type"] != "json_object" {
		t.Errorf("expected response_format json_object, got %v", out.ResponseFormat)
	}
}

func TestEnrichmentInputWithNoMessagesInjectsLabels(t *testing.T) {
	svc := domain.NewEnrichmentService()
	req := &domain.ChatRequest{
		Model:    "gemma4:e4b",
		Messages: []domain.ChatMessage{},
		Metadata: &domain.RequestMetadata{
			TeamMetadata: &domain.TeamMetadata{
				CostCenter:   "codex",
				SystemPrompt: "You are helpful",
			},
		},
	}

	out, err := svc.Enrich(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(out.Messages) != 1 {
		t.Fatalf("expected 1 message (system), got %d", len(out.Messages))
	}
	if out.Messages[0].Role != "system" {
		t.Errorf("expected system message, got %s", out.Messages[0].Role)
	}
}
