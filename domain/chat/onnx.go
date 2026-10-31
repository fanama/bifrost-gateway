package chat

import (
	"strings"

	"bridge-gateway/domain/config"
	"bridge-gateway/domain/provider"
)

// ServedByOnnx indique si une configuration doit etre servie par le moteur
// ONNX local. Deux cas, dans cet ordre :
//
//  1. la configuration declare explicitement le provider "onnx" ;
//  2. le modele demande figure au catalogue comme modele de chat execute par
//     le moteur onnx (meme nom, provider onnx, kind chat).
//
// Le nom du modele doit correspondre a celui de la configuration : un modele
// homonyme declare pour un autre fournisseur ne doit jamais detourner l'appel.
// Le catalogue est lu par l'adaptateur, qui passe ici la fiche trouvee (ou nil).
func ServedByOnnx(cfg *config.ChatConfig, catalogModel *provider.ModelInfo) bool {
	if cfg == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Provider), provider.ProviderOnnx) {
		return true
	}
	if catalogModel == nil {
		return false
	}

	name := strings.TrimSpace(cfg.Model)
	if name == "" || !strings.EqualFold(strings.TrimSpace(catalogModel.Name), name) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(catalogModel.Provider), provider.ProviderOnnx) &&
		strings.EqualFold(provider.NormalizeModelKind(catalogModel.Kind), provider.ModelKindChat)
}
