package infrastructure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"bridge-gateway/domain"
)

func embedJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	return raw
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func TestLocalEmbedderEmbedsSingleText(t *testing.T) {
	e := NewLocalEmbedder(LocalEmbedderOptions{})
	cfg := &domain.ChatConfig{Provider: "local", Model: "local-embedding"}

	resp, err := e.Embed(context.Background(), cfg, &domain.EmbeddingRequest{
		Input: embedJSON(t, "Le chat dort paisiblement sur le canape"),
	})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if resp.Object != "list" {
		t.Errorf("expected object list, got %q", resp.Object)
	}
	if resp.Model != "local-embedding" {
		t.Errorf("expected model local-embedding, got %q", resp.Model)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Data))
	}
	item := resp.Data[0]
	if item.Object != "embedding" || item.Index != 0 {
		t.Errorf("unexpected item meta: %#v", item)
	}
	if len(item.Embedding) != localDefaultDimensions {
		t.Fatalf("expected %d dimensions, got %d", localDefaultDimensions, len(item.Embedding))
	}
	var norm float64
	for _, v := range item.Embedding {
		norm += float64(v) * float64(v)
	}
	if math.Abs(norm-1) > 1e-5 {
		t.Errorf("expected L2-normalized vector, got norm %f", norm)
	}
	if resp.Usage.TotalTokens == 0 {
		t.Errorf("expected non-zero token usage")
	}
}

func TestLocalEmbedderEmbedsBatchInOrder(t *testing.T) {
	e := NewLocalEmbedder(LocalEmbedderOptions{})
	cfg := &domain.ChatConfig{Provider: "local", Model: "local-embedding"}

	resp, err := e.Embed(context.Background(), cfg, &domain.EmbeddingRequest{
		Input: embedJSON(t, []string{"alpha", "beta", "gamma"}),
	})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(resp.Data) != 3 {
		t.Fatalf("expected 3 items, got %d", len(resp.Data))
	}
	for i, item := range resp.Data {
		if item.Index != i {
			t.Errorf("expected index %d, got %d", i, item.Index)
		}
		if len(item.Embedding) != localDefaultDimensions {
			t.Fatalf("item %d: expected %d dimensions, got %d", i, localDefaultDimensions, len(item.Embedding))
		}
	}
	if cosineSimilarity(resp.Data[0].Embedding, resp.Data[1].Embedding) == 1 {
		t.Errorf("distinct texts produced identical vectors")
	}
}

func TestLocalEmbedderRespectsDimensions(t *testing.T) {
	e := NewLocalEmbedder(LocalEmbedderOptions{})
	cfg := &domain.ChatConfig{Provider: "local"}
	dims := 16

	resp, err := e.Embed(context.Background(), cfg, &domain.EmbeddingRequest{
		Input:      embedJSON(t, "texte"),
		Dimensions: &dims,
	})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if got := len(resp.Data[0].Embedding); got != 16 {
		t.Errorf("expected 16 dimensions, got %d", got)
	}
}

func TestLocalEmbedderBase64Encoding(t *testing.T) {
	e := NewLocalEmbedder(LocalEmbedderOptions{DefaultDimensions: 8})
	cfg := &domain.ChatConfig{Provider: "local"}
	format := "base64"

	resp, err := e.Embed(context.Background(), cfg, &domain.EmbeddingRequest{
		Input:          embedJSON(t, "texte"),
		EncodingFormat: &format,
	})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	item := resp.Data[0]
	if item.EmbeddingStr == nil {
		t.Fatalf("expected base64 embedding string")
	}
	if item.Embedding != nil {
		t.Errorf("expected no raw floats alongside base64")
	}
	raw, err := base64.StdEncoding.DecodeString(*item.EmbeddingStr)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	if len(raw) != 8*4 {
		t.Errorf("expected %d bytes, got %d", 8*4, len(raw))
	}
}

func TestLocalEmbedderRejectsNumericInput(t *testing.T) {
	e := NewLocalEmbedder(LocalEmbedderOptions{})
	_, err := e.Embed(context.Background(), &domain.ChatConfig{Provider: "local"}, &domain.EmbeddingRequest{
		Input: embedJSON(t, []any{0.1, 0.2, 0.3}),
	})
	var invalid *domain.InvalidRequestError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected InvalidRequestError, got %v", err)
	}
	if invalid.Param != "input" {
		t.Errorf("expected param input, got %q", invalid.Param)
	}
}

func TestLocalEmbedderRejectsOversizedDimensions(t *testing.T) {
	e := NewLocalEmbedder(LocalEmbedderOptions{})
	dims := localMaxDimensions + 1
	_, err := e.Embed(context.Background(), &domain.ChatConfig{Provider: "local"}, &domain.EmbeddingRequest{
		Input:      embedJSON(t, "texte"),
		Dimensions: &dims,
	})
	var invalid *domain.InvalidRequestError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected InvalidRequestError, got %v", err)
	}
	if invalid.Param != "dimensions" {
		t.Errorf("expected param dimensions, got %q", invalid.Param)
	}
}

// Le vectoriseur par hachage reste un modele lexical : deux textes proches
// doivent scorer nettement plus haut qu'un texte sans rapport. C'est le
// contrat que le front affiche en score de similarite cosinus.
func TestLocalEmbedderSimilarTextsScoreHigher(t *testing.T) {
	e := NewLocalEmbedder(LocalEmbedderOptions{})
	cfg := &domain.ChatConfig{Provider: "local"}

	resp, err := e.Embed(context.Background(), cfg, &domain.EmbeddingRequest{
		Input: embedJSON(t, []string{
			"le chat dort paisiblement sur le canape",
			"le chat dort sur le canape",
			"les marches boursiers ont baisse cette semaine",
		}),
	})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	similar := cosineSimilarity(resp.Data[0].Embedding, resp.Data[1].Embedding)
	unrelated := cosineSimilarity(resp.Data[0].Embedding, resp.Data[2].Embedding)
	if similar <= unrelated {
		t.Errorf("expected similar texts to score higher: similar=%f unrelated=%f", similar, unrelated)
	}
	if similar < unrelated+0.1 {
		t.Errorf("expected a clear margin between similar (%f) and unrelated (%f)", similar, unrelated)
	}
}

func TestLocalEmbedderIsDeterministic(t *testing.T) {
	e := NewLocalEmbedder(LocalEmbedderOptions{})
	cfg := &domain.ChatConfig{Provider: "local"}
	req := &domain.EmbeddingRequest{Input: embedJSON(t, "meme texte deux fois")}

	first, err := e.Embed(context.Background(), cfg, req)
	if err != nil {
		t.Fatalf("first embed: %v", err)
	}
	second, err := e.Embed(context.Background(), cfg, req)
	if err != nil {
		t.Fatalf("second embed: %v", err)
	}
	a, b := first.Data[0].Embedding, second.Data[0].Embedding
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("vector not deterministic at index %d: %f != %f", i, a[i], b[i])
		}
	}
}

// countingEmbedder sert de provider local ou distant dans les tests de
// routage et compte les appels recus.
type countingEmbedder struct {
	calls int
	model string
	err   error
	resp  *domain.EmbeddingResponse
	// cfg conserve la configuration effective recue, pour verifier les
	// substitutions de provider faites par le routeur.
	cfg *domain.ChatConfig
}

func (c *countingEmbedder) Embed(_ context.Context, cfg *domain.ChatConfig, _ *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	c.calls++
	c.model = cfg.Model
	c.cfg = cfg
	if c.err != nil {
		return nil, c.err
	}
	if c.resp != nil {
		return c.resp, nil
	}
	return &domain.EmbeddingResponse{Object: "list", Model: cfg.Model}, nil
}

func TestEmbeddingRouterDispatchesByProvider(t *testing.T) {
	local := &countingEmbedder{}
	inner := &countingEmbedder{}
	router := NewEmbeddingRouter(local, nil, inner)

	if _, err := router.Embed(context.Background(), &domain.ChatConfig{Provider: "LOCAL", Model: "m"}, &domain.EmbeddingRequest{Input: embedJSON(t, "x")}); err != nil {
		t.Fatalf("local route: %v", err)
	}
	if local.calls != 1 || inner.calls != 0 {
		t.Fatalf("expected local only, got local=%d inner=%d", local.calls, inner.calls)
	}

	if _, err := router.Embed(context.Background(), &domain.ChatConfig{Provider: "mistral", Model: "m"}, &domain.EmbeddingRequest{Input: embedJSON(t, "x")}); err != nil {
		t.Fatalf("distant route: %v", err)
	}
	if inner.calls != 1 || local.calls != 1 {
		t.Fatalf("expected inner for mistral, got local=%d inner=%d", local.calls, inner.calls)
	}
}

// Un echec du runtime local est remonte tel quel : pas de bascule
// silencieuse vers un provider distant sur un appel declare local.
func TestEmbeddingRouterPropagatesLocalError(t *testing.T) {
	local := &countingEmbedder{err: errors.New("modele local indisponible")}
	inner := &countingEmbedder{}
	router := NewEmbeddingRouter(local, nil, inner)

	_, err := router.Embed(context.Background(), &domain.ChatConfig{Provider: "local"}, &domain.EmbeddingRequest{Input: embedJSON(t, "x")})
	if err == nil || err.Error() != "modele local indisponible" {
		t.Fatalf("expected local error, got %v", err)
	}
	if inner.calls != 0 {
		t.Errorf("inner must not be called on local failure, got %d calls", inner.calls)
	}
}

func TestEmbeddingRouterRejectsNilConfig(t *testing.T) {
	router := NewEmbeddingRouter(&countingEmbedder{}, nil, &countingEmbedder{})
	if _, err := router.Embed(context.Background(), nil, &domain.EmbeddingRequest{Input: embedJSON(t, "x")}); !errors.Is(err, domain.ErrNoActiveConfig) {
		t.Fatalf("expected ErrNoActiveConfig, got %v", err)
	}
}

// Une requete peut demander explicitement le modele embarque depuis une
// configuration distante : c'est le cas de l'exemple cURL du testeur, dont
// l'API equivalente ne doit rien envoyer vers un provider payant.
func TestEmbeddingRouterRoutesOnModelName(t *testing.T) {
	local := &countingEmbedder{}
	inner := &countingEmbedder{}
	router := NewEmbeddingRouter(local, nil, inner)

	req := &domain.EmbeddingRequest{Input: embedJSON(t, "x"), Model: LocalEmbeddingModel}
	if _, err := router.Embed(context.Background(), &domain.ChatConfig{Provider: "mistral", Model: "mistral-embed"}, req); err != nil {
		t.Fatalf("model route: %v", err)
	}
	if local.calls != 1 || inner.calls != 0 {
		t.Fatalf("expected local by model name, got local=%d inner=%d", local.calls, inner.calls)
	}
}

// Un modele curaté distant (nomic-embed-text) route vers Bifrost avec un
// provider substitue : URL et cle de la config active sont effacees pour que
// le provider enregistre (ollama) soit utilise, jamais ceux d'une config
// mistral activee sur le meme projet.
func TestEmbeddingRouterChoiceSubstitutesProviderAndClearsCredentials(t *testing.T) {
	local := &countingEmbedder{}
	onnx := &countingEmbedder{}
	inner := &countingEmbedder{}
	router := NewEmbeddingRouter(local, onnx, inner)

	cfg := &domain.ChatConfig{
		Provider: "mistral",
		Model:    "mistral-small",
		BaseURL:  "https://api.mistral.ai",
		APIKey:   "cle-secrete",
	}
	req := &domain.EmbeddingRequest{Input: embedJSON(t, "x"), Model: "nomic-embed-text"}
	if _, err := router.Embed(context.Background(), cfg, req); err != nil {
		t.Fatalf("choice route: %v", err)
	}
	if inner.calls != 1 || local.calls != 0 || onnx.calls != 0 {
		t.Fatalf("expected inner only, got local=%d onnx=%d inner=%d", local.calls, onnx.calls, inner.calls)
	}
	eff := inner.cfg
	if eff.Provider != "ollama" || eff.Model != "nomic-embed-text" {
		t.Errorf("expected ollama/nomic-embed-text, got %s/%s", eff.Provider, eff.Model)
	}
	if eff.BaseURL != "" || eff.APIKey != "" {
		t.Errorf("credentials/base URL must be cleared, got BaseURL=%q APIKey=%q", eff.BaseURL, eff.APIKey)
	}
	if eff.ProjectID != cfg.ProjectID {
		t.Errorf("project must be preserved: %q", eff.ProjectID)
	}
}

// Le modele ONNX curaté route vers le moteur ONNX, jamais vers Bifrost.
func TestEmbeddingRouterRoutesOnnxChoice(t *testing.T) {
	local := &countingEmbedder{}
	onnx := &countingEmbedder{}
	inner := &countingEmbedder{}
	router := NewEmbeddingRouter(local, onnx, inner)

	req := &domain.EmbeddingRequest{Input: embedJSON(t, "x"), Model: domain.ModelOnnxMiniLM}
	if _, err := router.Embed(context.Background(), &domain.ChatConfig{Provider: "mistral"}, req); err != nil {
		t.Fatalf("onnx route: %v", err)
	}
	if onnx.calls != 1 || local.calls != 0 || inner.calls != 0 {
		t.Fatalf("expected onnx only, got local=%d onnx=%d inner=%d", local.calls, onnx.calls, inner.calls)
	}
}

// Une configuration au provider onnx sert aussi le moteur ONNX, par
// symetrie avec le provider local.
func TestEmbeddingRouterRoutesOnnxByConfigProvider(t *testing.T) {
	onnx := &countingEmbedder{}
	inner := &countingEmbedder{}
	router := NewEmbeddingRouter(&countingEmbedder{}, onnx, inner)

	if _, err := router.Embed(context.Background(), &domain.ChatConfig{Provider: "ONNX"}, &domain.EmbeddingRequest{Input: embedJSON(t, "x")}); err != nil {
		t.Fatalf("onnx provider route: %v", err)
	}
	if onnx.calls != 1 || inner.calls != 0 {
		t.Fatalf("expected onnx by provider, got onnx=%d inner=%d", onnx.calls, inner.calls)
	}
}

// Sans moteur ONNX configure, la demande ONNX echoue explicitement plutot
// que de partir vers un provider distant.
func TestEmbeddingRouterOnnxChoiceWithoutEngine(t *testing.T) {
	router := NewEmbeddingRouter(&countingEmbedder{}, nil, &countingEmbedder{})
	req := &domain.EmbeddingRequest{Input: embedJSON(t, "x"), Model: domain.ModelOnnxMiniLM}
	_, err := router.Embed(context.Background(), &domain.ChatConfig{Provider: "mistral"}, req)
	var invalid *domain.InvalidRequestError
	if !errors.As(err, &invalid) || invalid.Param != "model" {
		t.Fatalf("expected InvalidRequestError on model, got %v", err)
	}
}

// Un modele explicitement inconnu retombe sur la configuration, inchangee :
// c'est au provider distant de juger le modele, pas au routeur de deviner.
func TestEmbeddingRouterUnknownModelFallsBackToConfig(t *testing.T) {
	inner := &countingEmbedder{}
	router := NewEmbeddingRouter(&countingEmbedder{}, &countingEmbedder{}, inner)

	cfg := &domain.ChatConfig{Provider: "openai", Model: "text-embedding-3-small", APIKey: "k"}
	req := &domain.EmbeddingRequest{Input: embedJSON(t, "x"), Model: "modele-inconnu"}
	if _, err := router.Embed(context.Background(), cfg, req); err != nil {
		t.Fatalf("unknown model route: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("expected inner, got %d calls", inner.calls)
	}
	if inner.cfg.Provider != "openai" || inner.cfg.APIKey != "k" {
		t.Errorf("config must pass through unchanged, got %+v", inner.cfg)
	}
}
