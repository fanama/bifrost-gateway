package infrastructure

import (
	"context"
	"strings"

	"bridge-gateway/domain"
)

// EmbeddingRouter distribue les requetes d'embedding entre trois moteurs :
// le runtime Go embarque (provider "local"), le runtime ONNX in-process
// (provider "onnx"), et le repli distant Bifrost (mistral, ollama...).
//
// L'ordre de resolution est la regle centrale :
//
//  1. un modele explicitement demande par le client qui appartient a la liste
//     curatee (domain.EmbeddingChoices) designe son moteur — c'est ce qui
//     permet d'appeler nomic-embed-text ou onnx/all-MiniLM-L6-v2 depuis
//     l'API sans dependre de la config active du projet ;
//  2. sinon, le provider de la configuration tranche ("local" et "onnx"
//     restent locaux, tout le reste part vers Bifrost).
//
// Un echec des moteurs locaux est remonte tel quel. Une bascule silencieuse
// vers un provider distant cacherait une mauvaise configuration et ferait
// payer des tokens pour un appel declare local.
type EmbeddingRouter struct {
	local domain.EmbeddingProvider
	onnx  domain.EmbeddingProvider
	inner domain.EmbeddingProvider
}

// NewEmbeddingRouter branche les moteurs : runtime local, runtime ONNX (peut
// valoir nil tant qu'aucun modele ONNX n'est propose), et le provider de
// repli (typiquement le BifrostLLMProvider).
func NewEmbeddingRouter(local, onnx, inner domain.EmbeddingProvider) *EmbeddingRouter {
	return &EmbeddingRouter{local: local, onnx: onnx, inner: inner}
}

var _ domain.EmbeddingProvider = (*EmbeddingRouter)(nil)

func (r *EmbeddingRouter) Embed(ctx context.Context, cfg *domain.ChatConfig, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	if cfg == nil {
		return nil, domain.ErrNoActiveConfig
	}

	// 1. Le modele demande prime sur la configuration.
	if req != nil {
		if choice := domain.FindEmbeddingChoice(req.Model); choice != nil {
			switch strings.ToLower(choice.Provider) {
			case domain.ProviderLocal:
				return r.local.Embed(ctx, cfg, req)
			case domain.ProviderOnnx:
				if r.onnx == nil {
					return nil, &domain.InvalidRequestError{Param: "model", Reason: "onnx runtime not available"}
				}
				return r.onnx.Embed(ctx, choiceConfig(cfg, choice), req)
			default:
				// Provider distant connu : seule la substitution est faite.
				// BaseURL et APIKey sont effaces pour que le provider enregistre
				// (ou l'URL par defaut) soit utilise, jamais ceux d'une autre
				// configuration active par ailleurs.
				return r.inner.Embed(ctx, choiceConfig(cfg, choice), req)
			}
		}
		// Modele inconnu explicitement demande : on retombe sur les regles de
		// configuration, et c'est au provider distant de juger le modele.
	}

	// 2. La configuration tranche sinon.
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	switch provider {
	case domain.ProviderLocal:
		return r.local.Embed(ctx, cfg, req)
	case domain.ProviderOnnx:
		if r.onnx == nil {
			return nil, &domain.InvalidRequestError{Param: "model", Reason: "onnx runtime not available"}
		}
		return r.onnx.Embed(ctx, cfg, req)
	}
	return r.inner.Embed(ctx, cfg, req)
}

// choiceConfig derive la configuration effective d'un choix : meme projet,
// memme parametres de generation, mais provider et model du choix — sans
// cela, un choix ollama partirait avec l'URL et la cle de la config mistral
// activee sur le projet.
func choiceConfig(cfg *domain.ChatConfig, choice *domain.EmbeddingChoice) *domain.ChatConfig {
	clone := *cfg
	clone.Provider = choice.Provider
	clone.Model = choice.Model
	clone.BaseURL = ""
	clone.APIKey = ""
	return &clone
}
