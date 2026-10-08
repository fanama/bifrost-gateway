package infrastructure

import (
	"context"
	"strings"

	"bridge-gateway/domain"
)

// EmbeddingRouter distribue les requetes d'embedding entre les moteurs :
// le runtime Go embarque (provider "local"), le runtime ONNX in-process
// (provider "onnx"), et le repli distant Bifrost (mistral, ollama...).
//
// L'ordre de resolution est la regle centrale :
//
//  1. un modele explicitement demande par le client qui appartient a la liste
//     curatee (domain.EmbeddingChoices) designe son moteur — c'est ce qui
//     permet d'appeler nomic-embed-text ou onnx/all-MiniLM-L6-v2 depuis
//     l'API sans dependre de la config active du projet ;
//  2. un identifiant onnx/<repertoire> designe le moteur ONNX, catalogue ou
//     non : les artefacts sur disque font foi, un modele retire du catalogue
//     reste executable tant qu'il est present ;
//  3. un modele du catalogue declare de type embedding designe son moteur —
//     c'est la porte d'entree des modeles ajoutes depuis la page Modeles ;
//  4. sinon, le provider de la configuration tranche ("local" et "onnx"
//     restent locaux, tout le reste part vers Bifrost).
//
// Un echec des moteurs locaux est remonte tel quel. Une bascule silencieuse
// vers un provider distant cacherait une mauvaise configuration et ferait
// payer des tokens pour un appel declare local.
type EmbeddingRouter struct {
	local   domain.EmbeddingProvider
	onnx    domain.EmbeddingProvider
	inner   domain.EmbeddingProvider
	catalog domain.ModelCatalogRepository
}

// NewEmbeddingRouter branche les moteurs : runtime local, runtime ONNX (peut
// valoir nil tant qu'aucun modele ONNX n'est propose), et le provider de
// repli (typiquement le BifrostLLMProvider).
func NewEmbeddingRouter(local, onnx, inner domain.EmbeddingProvider) *EmbeddingRouter {
	return &EmbeddingRouter{local: local, onnx: onnx, inner: inner}
}

// WithCatalog branche le catalogue de modeles de l'UI : un modele demande par
// l'API qui n'est ni curate ni prefixe onnx/ y est resolu vers son moteur
// (regle 3). Sans catalogue, le routeur reste completement fonctionnel : seuls
// les modeles ajoutes depuis la page Modeles sont alors invisibles de l'API.
func (r *EmbeddingRouter) WithCatalog(catalog domain.ModelCatalogRepository) *EmbeddingRouter {
	r.catalog = catalog
	return r
}

var _ domain.EmbeddingProvider = (*EmbeddingRouter)(nil)

func (r *EmbeddingRouter) Embed(ctx context.Context, cfg *domain.ChatConfig, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	if cfg == nil {
		return nil, domain.ErrNoActiveConfig
	}

	// 1. Le modele demande prime sur la configuration.
	if req != nil {
		if choice := domain.FindEmbeddingChoice(req.Model); choice != nil {
			return r.embedChoice(ctx, cfg, choice, req)
		}
		// 2. Prefixed onnx/ : le moteur ONNX, quoi qu'en dise le catalogue.
		if id := strings.TrimSpace(req.Model); strings.HasPrefix(id, domain.OnnxModelPrefix) {
			return r.embedChoice(ctx, cfg, &domain.EmbeddingChoice{
				Model:    id,
				Provider: domain.ProviderOnnx,
			}, req)
		}
		// 3. Modele du catalogue declare de type embedding.
		if choice := r.catalogChoice(ctx, req.Model); choice != nil {
			return r.embedChoice(ctx, cfg, choice, req)
		}
		// Modele inconnu explicitement demande : on retombe sur les regles de
		// configuration, et c'est au provider distant de juger le modele.
	}

	// 4. La configuration tranche sinon.
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

// embedChoice dirige une requete vers le moteur designe par un choix. Pour un
// choix distant, seule la substitution est faite : BaseURL et APIKey sont
// effaces pour que le provider enregistre (ou l'URL par defaut) soit utilise,
// jamais ceux d'une autre configuration active par ailleurs. local reçoit la
// configuration brute — il ignore URL et cle, et son echo de modele part de
// req.Model de toute facon.
func (r *EmbeddingRouter) embedChoice(ctx context.Context, cfg *domain.ChatConfig, choice *domain.EmbeddingChoice, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	switch strings.ToLower(choice.Provider) {
	case domain.ProviderLocal:
		return r.local.Embed(ctx, cfg, req)
	case domain.ProviderOnnx:
		if r.onnx == nil {
			return nil, &domain.InvalidRequestError{Param: "model", Reason: "onnx runtime not available"}
		}
		return r.onnx.Embed(ctx, choiceConfig(cfg, choice), req)
	default:
		return r.inner.Embed(ctx, choiceConfig(cfg, choice), req)
	}
}

// catalogChoice resout un modele demande vers le choix porte par le catalogue
// de l'UI, ou nil : catalogue absent, erreur de lecture, ou modele inconnu /
// non declare de type embedding. Un modele de chat du catalogue ne detourne
// jamais un appel d'embedding, meme homonyme.
func (r *EmbeddingRouter) catalogChoice(ctx context.Context, model string) *domain.EmbeddingChoice {
	if r.catalog == nil {
		return nil
	}
	name := strings.TrimSpace(model)
	if name == "" {
		return nil
	}
	models, err := r.catalog.List(ctx)
	if err != nil {
		// La lecture echoue : on rend la main aux regles de configuration
		// plutot que de transformer une panne du catalogue en echec d'embed.
		return nil
	}
	for _, m := range models {
		if !strings.EqualFold(m.Name, name) {
			continue
		}
		choice, ok := domain.EmbeddingChoiceFromModel(domain.ModelInfo{
			Name:     m.Name,
			Provider: m.Provider,
			Kind:     m.Kind,
		})
		if !ok {
			return nil
		}
		return &choice
	}
	return nil
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
