package tests

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"bridge-gateway/application"
	"bridge-gateway/domain"
)

// Ces tests couvrent la sortie structuree : construction du schema par le
// constructeur visuel, validation de la configuration, et surcharge par la
// requete.

func TestBuildJSONSchemaFromVisualBuilder(t *testing.T) {
	raw, err := domain.BuildJSONSchema([]domain.SchemaField{
		{Name: "ville", Type: "string", Description: "nom de la ville", Required: true},
		{Name: "population", Type: "integer", Required: true},
		{Name: "pays", Type: "string"},
	})
	if err != nil {
		t.Fatalf("build schema: %v", err)
	}

	schema, err := domain.ParseJSONSchema(raw)
	if err != nil {
		t.Fatalf("the generated schema must be valid: %v (%s)", err, raw)
	}
	if schema["type"] != "object" {
		t.Errorf(`expected type "object", got %v`, schema["type"])
	}
	if schema["additionalProperties"] != false {
		t.Errorf("additionalProperties must be false to forbid invented fields, got %v", schema["additionalProperties"])
	}

	properties, _ := schema["properties"].(map[string]any)
	for _, name := range []string{"ville", "population", "pays"} {
		if _, ok := properties[name]; !ok {
			t.Errorf("missing property %q in %s", name, raw)
		}
	}
	if got := properties["population"].(map[string]any)["type"]; got != "integer" {
		t.Errorf("expected integer type, got %v", got)
	}
	if got := properties["ville"].(map[string]any)["description"]; got != "nom de la ville" {
		t.Errorf("description must be kept, got %v", got)
	}

	required, _ := schema["required"].([]any)
	if len(required) != 2 {
		t.Errorf("expected 2 required fields, got %v", required)
	}
}

func TestBuildJSONSchemaRejectsBadInput(t *testing.T) {
	cases := map[string][]domain.SchemaField{
		"aucun champ":  {},
		"lignes vides": {{Name: "  ", Type: "string"}},
		"doublon":      {{Name: "ville", Type: "string"}, {Name: "ville", Type: "string"}},
		"type inconnu": {{Name: "ville", Type: "tuile"}},
	}
	for name, fields := range cases {
		if _, err := domain.BuildJSONSchema(fields); err == nil {
			t.Errorf("%s: expected an error, got nil", name)
		}
	}
}

// Le mode strict n'est annonce que s'il est tenable : OpenAI exige que chaque
// propriete figure dans required.
func TestSchemaAllowsStrictOnlyWhenEveryFieldIsRequired(t *testing.T) {
	allRequired, err := domain.BuildJSONSchema([]domain.SchemaField{
		{Name: "a", Type: "string", Required: true},
		{Name: "b", Type: "string", Required: true},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	withOptional, err := domain.BuildJSONSchema([]domain.SchemaField{
		{Name: "a", Type: "string", Required: true},
		{Name: "b", Type: "string"},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	strictSchema, _ := domain.ParseJSONSchema(allRequired)
	if !domain.SchemaAllowsStrict(strictSchema) {
		t.Error("a schema whose fields are all required must allow strict")
	}

	looseSchema, _ := domain.ParseJSONSchema(withOptional)
	if domain.SchemaAllowsStrict(looseSchema) {
		t.Error("a schema with an optional field must not be announced as strict")
	}
}

func TestConfigValidationRequiresSchemaForJSONSchema(t *testing.T) {
	cfg := &domain.ChatConfig{
		Name:           "Extraction",
		Provider:       "openai",
		Model:          "gpt-4o",
		ResponseFormat: domain.ResponseFormatJSONSchema,
	}

	err := cfg.Validate()
	var verr *domain.ValidationError
	if err == nil {
		t.Fatal("json_schema without a schema must be rejected")
	}
	if !errors.As(err, &verr) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if len(verr.Fields) != 1 || verr.Fields[0] != "response_schema" {
		t.Errorf("expected the error to name response_schema, got %v", verr.Fields)
	}
}

func TestConfigResponseFormatMapShapes(t *testing.T) {
	if got := (&domain.ChatConfig{ResponseFormat: domain.ResponseFormatJSONObject}).ResponseFormatMap(); got["type"] != "json_object" {
		t.Errorf("expected json_object, got %v", got)
	}

	cfg := &domain.ChatConfig{
		Name:           "Mon Extraction",
		ResponseFormat: domain.ResponseFormatJSONSchema,
	}
	raw, err := domain.BuildJSONSchema([]domain.SchemaField{
		{Name: "ville", Type: "string", Required: true},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	cfg.ResponseSchema = raw

	shape := cfg.ResponseFormatMap()
	if shape["type"] != "json_schema" {
		t.Fatalf("expected json_schema, got %v", shape)
	}
	spec, _ := shape["json_schema"].(map[string]any)
	if spec == nil {
		t.Fatalf("missing json_schema block: %v", shape)
	}
	// Le nom doit rester alphanumerique alors que la configuration est libre.
	if spec["name"] != "Mon_Extraction" {
		t.Errorf("expected a sanitized name, got %v", spec["name"])
	}
	if spec["strict"] != true {
		t.Errorf("expected strict true for an all-required schema, got %v", spec["strict"])
	}
	if _, ok := spec["schema"]; !ok {
		t.Error("missing schema in the json_schema block")
	}
}

func TestParseResponseFormatFromRequest(t *testing.T) {
	cases := []struct {
		name       string
		input      map[string]any
		wantFormat string
		wantErr    bool
	}{
		{"json_object", map[string]any{"type": "json_object"}, domain.ResponseFormatJSONObject, false},
		{"text", map[string]any{"type": "text"}, domain.ResponseFormatText, false},
		{"inconnu", map[string]any{"type": "xml"}, "", true},
		{"schema sans json_schema", map[string]any{"type": "json_schema"}, "", true},
		{"schema sans schema", map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "x"}}, "", true},
	}
	for _, c := range cases {
		format, _, err := domain.ParseResponseFormat(c.input)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: expected an error, got %v", c.name, format)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
			continue
		}
		if format != c.wantFormat {
			t.Errorf("%s: expected %q, got %q", c.name, c.wantFormat, format)
		}
	}
}

// La surcharge par la requete doit gagner sur la configuration, et la
// configuration stockee ne doit jamais etre modifiee par un appel.
func TestRequestOptionsOverrideConfigWithoutMutatingIt(t *testing.T) {
	stored := &domain.ChatConfig{
		ID:             "cfg-1",
		Model:          "gpt-4o",
		Temperature:    floatPtr(0.7),
		MaxTokens:      intPtr(512),
		ResponseFormat: domain.ResponseFormatText,
	}

	zero := 0.0
	options := &domain.RequestOptions{
		Temperature:    &zero,
		MaxTokens:      intPtr(2048),
		ResponseFormat: map[string]any{"type": "json_object"},
	}

	effective, err := options.Apply(stored)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	if effective.Temperature == nil || *effective.Temperature != 0 {
		t.Errorf("temperature must be overridden by the request, got %v", effective.Temperature)
	}
	if effective.MaxTokens == nil || *effective.MaxTokens != 2048 {
		t.Errorf("max_tokens must be overridden by the request, got %v", effective.MaxTokens)
	}
	if effective.ResponseFormat != domain.ResponseFormatJSONObject {
		t.Errorf("response_format must be overridden, got %q", effective.ResponseFormat)
	}
	if effective.Model != stored.Model {
		t.Errorf("the model is never overridden, got %q", effective.Model)
	}

	if stored.Temperature == nil || *stored.Temperature != 0.7 {
		t.Errorf("the stored config must not be mutated, got %v", stored.Temperature)
	}
	if stored.MaxTokens == nil || *stored.MaxTokens != 512 {
		t.Errorf("the stored config must not be mutated, got %v", stored.MaxTokens)
	}
	if stored.ResponseFormat != domain.ResponseFormatText {
		t.Errorf("the stored config must not be mutated, got %q", stored.ResponseFormat)
	}
}

// Une surcharge absente doit laisser la configuration intacte : c'est le
// comportement par defaut de l'API, qui n'a pas change.
func TestRequestOptionsWithoutOverridesKeepsConfig(t *testing.T) {
	stored := &domain.ChatConfig{
		Model:          "gpt-4o",
		ResponseFormat: domain.ResponseFormatJSONObject,
	}

	for name, options := range map[string]*domain.RequestOptions{"nil": nil, "vide": {}} {
		effective, err := options.Apply(stored)
		if err != nil {
			t.Fatalf("%s: apply: %v", name, err)
		}
		if effective.ResponseFormat != stored.ResponseFormat {
			t.Errorf("%s: response_format changed to %q", name, effective.ResponseFormat)
		}
		if effective.Temperature != stored.Temperature {
			t.Errorf("%s: temperature changed", name)
		}
	}
}

// La surcharge doit atteindre le provider : le test observe les params recus.
func TestRequestResponseFormatReachesTheProvider(t *testing.T) {
	ctx := context.Background()
	store := NewTestStore(t)
	uc := application.NewConfigUseCase(store)

	cfg, err := uc.Create(ctx, &domain.ChatConfig{
		Name: "Extraction", Provider: "openai", Model: "gpt-4o",
	})
	if err != nil {
		t.Fatalf("create config: %v", err)
	}

	fake := &fakeLLM{}
	chat := application.NewChatUseCase(store, domain.NewEnrichmentService(), fake)

	req := &domain.ChatRequest{
		Messages: []domain.ChatMessage{{Role: "user", Content: "extrais"}},
		ResponseFormat: map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name": "extraction",
				"schema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"ville": map[string]any{"type": "string"}},
					"required":   []any{"ville"},
				},
			},
		},
	}

	if _, err := chat.Send(ctx, cfg.ID, req.Messages[:0], "extrais", domain.OptionsFrom(req)); err != nil {
		t.Fatalf("send: %v", err)
	}

	if fake.lastCfg == nil {
		t.Fatal("the provider received no configuration")
	}
	if fake.lastCfg.ResponseFormat != domain.ResponseFormatJSONSchema {
		t.Fatalf("expected json_schema at the provider, got %q", fake.lastCfg.ResponseFormat)
	}
	shape := fake.lastCfg.ResponseFormatMap()
	if shape == nil || shape["type"] != "json_schema" {
		t.Fatalf("the provider must receive the OpenAI shape, got %v", shape)
	}
	if _, err := json.Marshal(shape); err != nil {
		t.Fatalf("the shape must be serialisable: %v", err)
	}
}

func floatPtr(v float64) *float64 { return &v }
func intPtr(v int) *int           { return &v }

// TestStoredSchemaImpliesJSONSchema verrouille le cas reporte par un
// utilisateur : un schema construit depuis le formulaire est enregistre, mais le
// select "Format de reponse" est laisse sur "aucun". Le schema est alors
// stocke et la configuration validee, alors que le provider ne recevait aucun
// response_format et repondait en texte libre.
//
// Un schema enregistre vaut declaration d'intention : il doit contraindre les
// reponses.
func TestStoredSchemaImpliesJSONSchema(t *testing.T) {
	cfg := &domain.ChatConfig{
		Name:           "response schema",
		Provider:       "openrouter",
		Model:          "ministral-14b-latest",
		ResponseFormat: "",
		ResponseSchema: `{"type":"object","properties":{"nom":{"type":"string"}},"additionalProperties":false}`,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validation refuse la config : %v", err)
	}

	got := cfg.ResponseFormatMap()
	if got == nil {
		t.Fatal("schema stocke mais aucun response_format transmis au provider")
	}
	if got["type"] != domain.ResponseFormatJSONSchema {
		t.Fatalf("type = %v, attendu json_schema", got["type"])
	}
	spec, _ := got["json_schema"].(map[string]any)
	schema, _ := spec["schema"].(map[string]any)
	if _, ok := schema["properties"]; !ok {
		t.Fatalf("le schema n'est pas transmis : %v", spec)
	}
}

// TestExplicitFormatOverridesStoredSchema garantit que l'inference ne s'applique
// que lorsqu'aucun format n'a ete choisi. Un format explicite reste respecte.
func TestExplicitFormatOverridesStoredSchema(t *testing.T) {
	stored := `{"type":"object","properties":{"nom":{"type":"string"}}}`

	text := &domain.ChatConfig{ResponseFormat: "text", ResponseSchema: stored}
	if got := text.EffectiveResponseFormat(); got != "text" {
		t.Errorf("format explicite ecrase par l inference : %q", got)
	}
	if got := text.ResponseFormatMap(); got["type"] != "text" {
		t.Errorf("un format text explicite doit rester text, obtenu %v", got)
	}

	object := &domain.ChatConfig{ResponseFormat: "json_object", ResponseSchema: stored}
	if got := object.EffectiveResponseFormat(); got != "json_object" {
		t.Errorf("format explicite ecrase par l inference : %q", got)
	}

	none := &domain.ChatConfig{ResponseFormat: "", ResponseSchema: ""}
	if got := none.EffectiveResponseFormat(); got != "" {
		t.Errorf("sans schema ni format, rien ne doit etre impose, obtenu %q", got)
	}
	if got := none.ResponseFormatMap(); got != nil {
		t.Errorf("sans schema ni format, aucun parametre ne doit partir, obtenu %v", got)
	}
}

// TestStoredSchemaWithoutFormatIsStillValidated verifie que l inference ne
// contourne pas la validation du schema : un schema illisible enregistre avec un
// format vide doit toujours etre refuse, puisque c'est lui qui sera transmis.
func TestStoredSchemaWithoutFormatIsStillValidated(t *testing.T) {
	cfg := &domain.ChatConfig{
		Name:           "schema casse",
		Provider:       "openrouter",
		Model:          "ministral-14b-latest",
		ResponseFormat: "",
		ResponseSchema: `{"type":"chaîne"}`,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("un schema invalide enregistre avec un format vide doit etre refuse")
	}
	var verr *domain.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("erreur attendue de type ValidationError, obtenu %T", err)
	}
}
