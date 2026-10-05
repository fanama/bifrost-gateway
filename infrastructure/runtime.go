package infrastructure

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"bridge-gateway/domain"
	"github.com/maximhq/bifrost/core/schemas"
)

type BifrostLLMProvider struct {
	mu        sync.Mutex
	clients   map[string]*BifrostClient
	providers domain.ProviderRepository
	// maxRetries borne les reessais internes de Bifrost par appel. Quand le
	// routeur est actif, il vaut 1 : l'echelle de tiers assure deja la
	// re-tentative sur un autre modele, et cumuler les deux triplerait la
	// latence avant echec.
	maxRetries int
}

// defaultMaxRetries conserve le comportement historique quand aucun routage
// n'est configure.
const defaultMaxRetries = 2

func NewBifrostLLMProvider(providers ...domain.ProviderRepository) *BifrostLLMProvider {
	var repo domain.ProviderRepository
	if len(providers) > 0 {
		repo = providers[0]
	}
	return &BifrostLLMProvider{
		clients:    map[string]*BifrostClient{},
		providers:  repo,
		maxRetries: defaultMaxRetries,
	}
}

// SetMaxRetries fixe le nombre de reessais par appel. Une valeur nulle ou
// negative restaure la valeur par defaut, pour qu'une configuration incomplete
// n'empeche pas le demarrage.
func (p *BifrostLLMProvider) SetMaxRetries(n int) {
	if n <= 0 {
		n = defaultMaxRetries
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.maxRetries = n
	// Les clients existants gardent l'ancien budget : les reconstruire a chaque
	// appel serait plus couteux que de ne pas reappliquer la regle aux
	// configurations deja en cache.
	p.clients = map[string]*BifrostClient{}
}

// resolveProvider retrouve un provider enregistre par son nom ou son identifiant.
// Il est partage entre le provider Bifrost et le routeur, qui doit empowered les
// memes credentials pour un tier qu'il substitue a la configuration active.
func resolveProvider(ctx context.Context, repo domain.ProviderRepository, providerName string) *domain.Provider {
	if repo == nil {
		return nil
	}
	list, err := repo.List(ctx)
	if err != nil {
		return nil
	}
	for _, prov := range list {
		if strings.EqualFold(prov.Name, providerName) || strings.EqualFold(prov.ID, providerName) {
			found := prov
			return &found
		}
	}
	return nil
}

func (p *BifrostLLMProvider) resolveProvider(ctx context.Context, providerName string) *domain.Provider {
	return resolveProvider(ctx, p.providers, providerName)
}

func ResolveEnv(val string) string {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "{env:") && strings.HasSuffix(trimmed, "}") {
		envName := strings.TrimSuffix(strings.TrimPrefix(trimmed, "{env:"), "}")
		return os.Getenv(strings.TrimSpace(envName))
	}
	if strings.HasPrefix(trimmed, "env:") {
		envName := strings.TrimPrefix(trimmed, "env:")
		return os.Getenv(strings.TrimSpace(envName))
	}
	if strings.HasPrefix(trimmed, "${") && strings.HasSuffix(trimmed, "}") {
		envName := strings.TrimSuffix(strings.TrimPrefix(trimmed, "${"), "}")
		return os.Getenv(strings.TrimSpace(envName))
	}
	if strings.HasPrefix(trimmed, "$") && !strings.Contains(trimmed, "/") && !strings.Contains(trimmed, " ") {
		return os.Getenv(strings.TrimPrefix(trimmed, "$"))
	}
	return trimmed
}

func (p *BifrostLLMProvider) clientFor(ctx context.Context, cfg *domain.ChatConfig) (*BifrostClient, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if apiKey == "" || baseURL == "" {
		if prov := p.resolveProvider(ctx, cfg.Provider); prov != nil {
			if apiKey == "" {
				apiKey = strings.TrimSpace(prov.APIKey)
			}
			if baseURL == "" {
				baseURL = strings.TrimSpace(prov.BaseURL)
			}
		}
	}

	apiKey = ResolveEnv(apiKey)
	baseURL = ResolveEnv(baseURL)

	key := fmt.Sprintf("%s:%d:%s:%s", cfg.ID, cfg.UpdatedAt.Unix(), apiKey, baseURL)
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.clients[key]; ok {
		return c, nil
	}
	client, err := NewBifrostClient(NewDynamicAccount(cfg, apiKey, baseURL).withMaxRetries(p.maxRetries))
	if err != nil {
		return nil, fmt.Errorf("init bifrost client: %w", err)
	}
	p.clients[key] = client
	return client, nil
}

func (p *BifrostLLMProvider) Chat(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (domain.LLMResult, error) {
	provider, err := ToModelProvider(cfg.Provider)
	if err != nil {
		return domain.LLMResult{}, err
	}

	client, err := p.clientFor(ctx, cfg)
	if err != nil {
		return domain.LLMResult{}, err
	}

	resp, err := client.ChatCompletion(ctx, provider, cfg.Model, toBifrostMessages(messages), chatParams(cfg))
	if err != nil {
		return domain.LLMResult{}, err
	}
	return domain.LLMResult{Content: ExtractAssistantContent(resp), Model: cfg.Model}, nil
}

var _ domain.StreamingLLMProvider = (*BifrostLLMProvider)(nil)

// ChatStream est l'implementation reelle du streaming : elle pompe le canal de
// Bifrost et le convertit en domain.StreamEvent.
//
// La conversion se fait dans une goroutine qui lit la source jusqu'a sa
// fermeture et ferme toujours la sortie, y compris si le client abandonne : le
// ctx est surveille pour ne pas laisser un flux ouvert apres une deconnexion.
func (p *BifrostLLMProvider) ChatStream(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (<-chan domain.StreamEvent, error) {
	provider, err := ToModelProvider(cfg.Provider)
	if err != nil {
		return nil, err
	}

	client, err := p.clientFor(ctx, cfg)
	if err != nil {
		return nil, err
	}

	source, err := client.ChatCompletionStream(ctx, provider, cfg.Model, toBifrostMessages(messages), chatParams(cfg))
	if err != nil {
		return nil, err
	}

	out := make(chan domain.StreamEvent)
	go func() {
		defer close(out)

		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-source:
				if !ok {
					// Le provider a fini : on termine le flux.
					select {
					case out <- domain.StreamEvent{Model: cfg.Model, Done: true}:
					case <-ctx.Done():
					}
					return
				}

				if streamErr := streamChunkError(chunk); streamErr != nil {
					// Une erreur de milieu de flux ne peut plus etre convertie en
					// reponse HTTP : la seule chose qui reste correcte est de
					// l'annoncer au client sous forme de trame SSE.
					select {
					case out <- domain.StreamEvent{Err: streamErr}:
					case <-ctx.Done():
					}
					return
				}

				delta, role, ok := streamChunkDelta(chunk)
				if !ok {
					continue
				}
				select {
				case out <- domain.StreamEvent{Delta: delta, Role: role, Model: cfg.Model}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return out, nil
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

func ToModelProvider(s string) (schemas.ModelProvider, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" {
		return schemas.OpenAI, nil
	}
	switch v {
	case "ollama":
		return schemas.Ollama, nil
	case "openai", "openai-compatible", "custom", "generic", "proxy":
		return schemas.OpenAI, nil
	case "azure":
		return schemas.Azure, nil
	case "vertex", "vertexai":
		return schemas.Vertex, nil
	case "google", "gemini":
		return schemas.Gemini, nil
	case "mistral", "mistralai":
		return schemas.Mistral, nil
	case "anthropic", "claude":
		return schemas.Anthropic, nil
	case "groq":
		return schemas.Groq, nil
	case "cohere":
		return schemas.Cohere, nil
	case "bedrock", "aws":
		return schemas.Bedrock, nil
	case "openrouter":
		return schemas.OpenRouter, nil
	case "perplexity":
		return schemas.Perplexity, nil
	case "cerebras":
		return schemas.Cerebras, nil
	case "xai", "grok":
		return schemas.XAI, nil
	case "replicate":
		return schemas.Replicate, nil
	case "nebius":
		return schemas.Nebius, nil
	case "elevenlabs":
		return schemas.Elevenlabs, nil
	case "huggingface":
		return schemas.HuggingFace, nil
	case "sgl":
		return schemas.SGL, nil
	case "parasail":
		return schemas.Parasail, nil
	case "vllm":
		return schemas.VLLM, nil
	case "runway":
		return schemas.Runway, nil
	default:
		// Any other custom provider (e.g. private gateway, vLLM, custom proxy)
		// falls back to the standard OpenAI-compatible protocol with its custom BaseURL.
		return schemas.OpenAI, nil
	}
}

func defaultBaseURLFor(p schemas.ModelProvider) string {
	switch p {
	case schemas.Ollama:
		return "http://localhost:11434"
	case schemas.OpenAI:
		return "https://api.openai.com"
	case schemas.Mistral:
		return "https://api.mistral.ai"
	case schemas.Anthropic:
		return "https://api.anthropic.com"
	case schemas.Groq:
		return "https://api.groq.com/openai"
	case schemas.OpenRouter:
		return "https://openrouter.ai/api"
	case schemas.Gemini:
		return "https://generativelanguage.googleapis.com/v1beta"
	case schemas.Perplexity:
		return "https://api.perplexity.ai"
	case schemas.XAI:
		return "https://api.x.ai"
	default:
		return ""
	}
}

// normalizeBaseURL aligns a configured base URL with the path Bifrost appends
// to it. The OpenAI-compatible, Anthropic, Mistral and Ollama providers all
// concatenate BaseURL with a path that already carries the API version
// ("/v1/chat/completions", "/v1/messages"), so a base URL ending in "/v1" would
// produce "/v1/v1/chat/completions" and the provider would answer with an HTML
// error page instead of JSON. Version segments Bifrost does not append itself
// are preserved, because it appends a version-less path to them (Gemini's
// "/v1beta" + "/models").
func normalizeBaseURL(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return ""
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		// Not an absolute URL (e.g. an unresolved env placeholder): leave as is.
		return trimmed
	}

	path := parsed.Path
	for _, suffix := range []string{"/chat/completions", "/v1"} {
		if trimmedPath, ok := strings.CutSuffix(path, suffix); ok {
			path = trimmedPath
		}
	}
	parsed.Path = strings.TrimRight(path, "/")
	parsed.RawPath = ""

	return strings.TrimRight(parsed.String(), "/")
}

type DynamicAccount struct {
	cfg     *domain.ChatConfig
	apiKey  string
	baseURL string
	// retries vaut 0 tant qu'il n'a pas ete fixe, ce qui laisse GetConfigForProvider
	// appliquer defaultMaxRetries.
	retries int
}

func NewDynamicAccount(cfg *domain.ChatConfig, customParams ...string) *DynamicAccount {
	apiKey := cfg.APIKey
	baseURL := cfg.BaseURL
	if len(customParams) > 0 && customParams[0] != "" {
		apiKey = customParams[0]
	}
	if len(customParams) > 1 && customParams[1] != "" {
		baseURL = customParams[1]
	}
	return &DynamicAccount{cfg: cfg, apiKey: apiKey, baseURL: baseURL}
}

func (a *DynamicAccount) withMaxRetries(n int) *DynamicAccount {
	a.retries = n
	return a
}

func (a *DynamicAccount) provider() (schemas.ModelProvider, error) {
	return ToModelProvider(a.cfg.Provider)
}

func (a *DynamicAccount) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	p, err := a.provider()
	if err != nil {
		return nil, err
	}
	return []schemas.ModelProvider{p}, nil
}

func (a *DynamicAccount) GetKeysForProvider(_ context.Context, _ schemas.ModelProvider) ([]schemas.Key, error) {
	if _, err := a.provider(); err != nil {
		return nil, err
	}
	return []schemas.Key{{
		Name:   a.cfg.Name,
		Value:  schemas.EnvVar{Val: ResolveEnv(a.apiKey)},
		Models: []string{},
		Weight: 1.0,
	}}, nil
}

func (a *DynamicAccount) GetConfigForProvider(provider schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	baseURL := normalizeBaseURL(ResolveEnv(a.baseURL))
	if baseURL == "" {
		baseURL = normalizeBaseURL(defaultBaseURLFor(provider))
	}
	retries := a.retries
	if retries <= 0 {
		retries = defaultMaxRetries
	}
	return &schemas.ProviderConfig{
		NetworkConfig: schemas.NetworkConfig{
			BaseURL:                        baseURL,
			DefaultRequestTimeoutInSeconds: 60,
			MaxRetries:                     retries,
			RetryBackoffInitial:            500 * time.Millisecond,
			RetryBackoffMax:                5 * time.Second,
		},
		ConcurrencyAndBufferSize: schemas.DefaultConcurrencyAndBufferSize,
	}, nil
}
