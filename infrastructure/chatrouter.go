package infrastructure

import (
	"context"
	"strings"

	"bridge-gateway/domain"
)

// OnnxChatRouter dirige les conversations vers le moteur ONNX local, le reste
// partant vers Bifrost. Meme philosophie que EmbeddingRouter :
//
//  1. la configuration declare le provider "onnx" ;
//  2. le modele demande figure au catalogue comme modele de chat onnx ;
//  3. sinon repli distant.
//
// Un echec du moteur local est remonte tel quel : basculer silencieusement
// vers un provider distant cacherait une mauvaise configuration et payerait
// des tokens pour un appel declare local.
type OnnxChatRouter struct {
	onnx    *OnnxChatProvider
	inner   domain.LLMProvider
	catalog domain.ModelCatalogRepository
}

func NewOnnxChatRouter(onnx *OnnxChatProvider, inner domain.LLMProvider) *OnnxChatRouter {
	return &OnnxChatRouter{onnx: onnx, inner: inner}
}

// WithCatalog branche le catalogue de l'UI : un modele de chat ajoute depuis
// la page Modeles resout vers son moteur (regle 2). Sans catalogue, la regle 1
// reste operationnelle.
func (r *OnnxChatRouter) WithCatalog(catalog domain.ModelCatalogRepository) *OnnxChatRouter {
	r.catalog = catalog
	return r
}

var _ domain.LLMProvider = (*OnnxChatRouter)(nil)
var _ domain.StreamingLLMProvider = (*OnnxChatRouter)(nil)

func (r *OnnxChatRouter) Chat(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (domain.LLMResult, error) {
	if r.local(cfg) {
		return r.onnx.Chat(ctx, cfg, messages)
	}
	return r.inner.Chat(ctx, cfg, messages)
}

func (r *OnnxChatRouter) ChatStream(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (<-chan domain.StreamEvent, error) {
	if r.local(cfg) {
		return r.onnx.ChatStream(ctx, cfg, messages)
	}
	return domain.StreamFrom(ctx, r.inner, cfg, messages)
}

// local tranche : provider onnx, ou modele du catalogue declare onnx/chat.
func (r *OnnxChatRouter) local(cfg *domain.ChatConfig) bool {
	if cfg == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Provider), domain.ProviderOnnx) {
		return true
	}
	return r.onnxCatalogModel(strings.TrimSpace(cfg.Model))
}

// onnxCatalogModel verifie que le modele designe est, au catalogue, un modele
// de chat execute par le moteur onnx. Lecture best-effort : une panne du
// catalogue rend la main a la regle de provider, jamais un echec.
func (r *OnnxChatRouter) onnxCatalogModel(model string) bool {
	if r.catalog == nil || model == "" || r.onnx == nil {
		return false
	}
	models, err := r.catalog.List(context.Background())
	if err != nil {
		return false
	}
	for _, m := range models {
		if !strings.EqualFold(m.Name, model) {
			continue
		}
		return strings.EqualFold(m.Provider, domain.ProviderOnnx) &&
			strings.EqualFold(domain.NormalizeModelKind(m.Kind), domain.ModelKindChat)
	}
	return false
}
