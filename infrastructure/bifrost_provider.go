package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"bridge-gateway/domain"
	"github.com/maximhq/bifrost/core/schemas"
)

// BifrostLLMProvider est l'adaptateur domain.LLMProvider qui parle au SDK
// BifrostAI. Il resout les identifiants d'une configuration (provider
// enregistre ou cle/URL portees par la configuration elle-meme) puis delegue
// l'appel au client Bifrost correspondant.
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

func (p *BifrostLLMProvider) resolveProvider(ctx context.Context, providerName string) *domain.Provider {
	return resolveProvider(ctx, p.providers, providerName)
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

	return pumpBifrostStream(ctx, source, cfg.Model), nil
}

// pumpBifrostStream converts Bifrost chunks into domain events. A mid-stream
// error can no longer become an HTTP error, so it is forwarded as an SSE frame.
func pumpBifrostStream(ctx context.Context, source <-chan *schemas.BifrostStreamChunk, model string) <-chan domain.StreamEvent {
	out := make(chan domain.StreamEvent)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-source:
				if !ok {
					sendStreamEvent(ctx, out, domain.StreamEvent{Model: model, Done: true})
					return
				}
				event, ok := eventFromChunk(chunk, model)
				if !ok {
					continue
				}
				if !sendStreamEvent(ctx, out, event) {
					return
				}
				if event.Err != nil {
					return
				}
			}
		}
	}()
	return out
}

// eventFromChunk converts one chunk; ok is false when it carries no delta.
func eventFromChunk(chunk *schemas.BifrostStreamChunk, model string) (domain.StreamEvent, bool) {
	if err := streamChunkError(chunk); err != nil {
		return domain.StreamEvent{Err: err}, true
	}
	delta, role, ok := streamChunkDelta(chunk)
	if !ok {
		return domain.StreamEvent{}, false
	}
	return domain.StreamEvent{Delta: delta, Role: role, Model: model}, true
}

var _ domain.EmbeddingProvider = (*BifrostLLMProvider)(nil)

func (p *BifrostLLMProvider) Embed(ctx context.Context, cfg *domain.ChatConfig, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	input, err := bifrostEmbeddingInput(req)
	if err != nil {
		return nil, err
	}

	provider, err := ToModelProvider(cfg.Provider)
	if err != nil {
		return nil, err
	}

	client, err := p.clientFor(ctx, cfg)
	if err != nil {
		return nil, err
	}

	model := cfg.Model
	if req.Model != "" {
		model = req.Model
	}

	var params *schemas.EmbeddingParameters
	if req.EncodingFormat != nil || req.Dimensions != nil {
		params = &schemas.EmbeddingParameters{
			EncodingFormat: req.EncodingFormat,
			Dimensions:     req.Dimensions,
		}
	}

	resp, err := client.Embedding(ctx, provider, model, &input, params)
	if err != nil {
		return nil, err
	}

	return toDomainEmbeddingResponse(resp, model), nil
}

// bifrostEmbeddingInput validates and decodes the raw embedding input payload.
func bifrostEmbeddingInput(req *domain.EmbeddingRequest) (schemas.EmbeddingInput, error) {
	if req == nil || len(req.Input) == 0 {
		return schemas.EmbeddingInput{}, &domain.InvalidRequestError{Param: "input", Reason: "input is required"}
	}
	var input schemas.EmbeddingInput
	if err := json.Unmarshal(req.Input, &input); err != nil {
		return schemas.EmbeddingInput{}, &domain.InvalidRequestError{Param: "input", Reason: "invalid input format: " + err.Error()}
	}
	if input.Text == nil && len(input.Texts) == 0 && len(input.Embedding) == 0 && len(input.Embeddings) == 0 {
		return schemas.EmbeddingInput{}, &domain.InvalidRequestError{Param: "input", Reason: "input is required"}
	}
	return input, nil
}
