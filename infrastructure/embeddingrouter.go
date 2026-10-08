package infrastructure

import (
	"context"
	"strings"

	"bridge-gateway/domain"
)

// EmbeddingRouter dispatche les requetes d'embedding selon la configuration
// ou la requete : provider "local" sur la configuration, ou modele
// local-embedding explicitement demande — dans les deux cas, le runtime
// embarque repond. Tout le reste (mistral, openai, ollama...) part vers
// l'inner (Bifrost) comme avant.
//
// La regle modele couvre l'exemple cURL du testeur : un appelant peut
// demander local-embedding depuis une configuration distante (cle de projet,
// config active mistral) sans que rien ne parte vers un provider payant.
//
// Un echec du runtime local est remonte tel quel. Une bascule silencieuse
// vers un provider distant cacherait une mauvaise configuration et ferait
// payer des tokens pour un appel declare local.
type EmbeddingRouter struct {
	local domain.EmbeddingProvider
	inner domain.EmbeddingProvider
}

// NewEmbeddingRouter branche le runtime local et le provider de repli
// (typiquement le BifrostLLMProvider).
func NewEmbeddingRouter(local, inner domain.EmbeddingProvider) *EmbeddingRouter {
	return &EmbeddingRouter{local: local, inner: inner}
}

var _ domain.EmbeddingProvider = (*EmbeddingRouter)(nil)

func (r *EmbeddingRouter) Embed(ctx context.Context, cfg *domain.ChatConfig, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	if cfg == nil {
		return nil, domain.ErrNoActiveConfig
	}
	if usesLocalRuntime(cfg, req) {
		return r.local.Embed(ctx, cfg, req)
	}
	return r.inner.Embed(ctx, cfg, req)
}

// usesLocalRuntime decide du routage : le provider de la configuration, ou
// le nom du modele embarque porte par la requete. La configuration prime :
// une config local sert la requete meme si le client a oublie de nommer le
// modele embarque.
func usesLocalRuntime(cfg *domain.ChatConfig, req *domain.EmbeddingRequest) bool {
	if strings.EqualFold(strings.TrimSpace(cfg.Provider), LocalProviderName) {
		return true
	}
	return req != nil && strings.EqualFold(strings.TrimSpace(req.Model), LocalEmbeddingModel)
}
