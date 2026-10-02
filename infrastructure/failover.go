package infrastructure

import (
	"context"
	"errors"
	"strings"
)

// FailoverReason explique pourquoi une tentative a ete abandonnee, afin que la
// decision soit journalisee et testable sans passer par une chaine de messages.
type FailoverReason string

const (
	// FailoverNone : l'appel a reussi.
	FailoverNone FailoverReason = ""
	// FailoverRateLimit : quota epuise ou trop de requetes (429).
	FailoverRateLimit FailoverReason = "rate_limit"
	// FailoverContextLength : l'historique deborde sur ce modele (400).
	FailoverContextLength FailoverReason = "context_length"
	// FailoverUnavailable : panne, erreur serveur ou reseau indisponible.
	FailoverUnavailable FailoverReason = "unavailable"
	// FailoverAuth : cle refusee ou droits manquants (401/403).
	FailoverAuth FailoverReason = "auth"
	// FailoverModel : le tier referencia un modele inexistant ou indisponible
	// pour ce provider (404). C'est un defaut de la cible, pas de la requete :
	// aucun retry sur ce modele ne peut aboutir, mais le tier suivant le peut.
	FailoverModel FailoverReason = "model_unavailable"
	// FailoverClient : autre erreur 4xx, non corrigeable par un autre modele.
	FailoverClient FailoverReason = "client"
)

// failoverReason determine si un echec de provider justifie une tentative sur
// le modele suivant de l'echelle.
//
// Les 5xx, 429 et les pannes reseau sont retenus car un autre provider peut
// repondre. Le depassement de fenetre de contexte l'est aussi : c'est le seul
// cas ou le remede est vraiment le modele lui-meme, plus grand. Les 401/403
// basculent aussi, comme demande, mais FailoverAuth reste distinct des autres
// raisons dans les logs pour qu'une mauvaise cle API ne passe pas inapercee.
//
// Un delai depasse n'est PAS termine : le timeout du provider (30 s par defaut
// chez Bifrost) ne concerne que ce modele-la, alors qu'un autre modele peut
// repondre dans la meme fenetre. Seule une annulation venue de l'appelant est
// definitive, et c'est le contexte de la requete qui la constate, pas l'erreur.
func failoverReason(err error) FailoverReason {
	if err == nil {
		return FailoverNone
	}

	// Une requete annulee par le client n'est pas un echec provider : relancer
	// sur un autre modele ne servirait personne.
	if errors.Is(err, context.Canceled) {
		return FailoverClient
	}

	var provErr *ProviderError
	if !errors.As(err, &provErr) {
		// Echec qui n'a pas de structure HTTP (client Bifrost, initialisation) :
		// traite comme une indisponibilite, le prochain tier peut reussir.
		return FailoverUnavailable
	}

	switch {
	case provErr.StatusCode == 429:
		return FailoverRateLimit
	case provErr.StatusCode == 401 || provErr.StatusCode == 403:
		return FailoverAuth
	case provErr.StatusCode >= 500:
		return FailoverUnavailable
	case provErr.StatusCode == 404 && isModelUnavailableError(provErr):
		// Un tier qui pointe sur un modele inexistant ne peut jamais repondre :
		// seule la cible est fautive, pas la requete.
		return FailoverModel
	case provErr.StatusCode == 400 && isContextLengthError(provErr):
		return FailoverContextLength
	case provErr.StatusCode >= 400 && provErr.StatusCode < 500:
		return FailoverClient
	default:
		return FailoverUnavailable
	}
}

// isModelUnavailableError distingue un 404 qui porte sur le modele d'un 404 qui
// porte sur le chemin de l'API. Le premier est remediable par un autre modele,
// le second ne l'est pas : reessayer consommerait des appels pour rien.
func isModelUnavailableError(err *ProviderError) bool {
	switch err.Code {
	case "model_not_found", "unknown_model", "model_not_available":
		return true
	}
	haystack := strings.ToLower(err.Message + " " + err.Detail)
	if !strings.Contains(haystack, "model") {
		return false
	}
	for _, marker := range []string{
		"not found",
		"does not exist",
		"unknown model",
		"no such model",
		"not available",
		"is not supported",
		"unsupported model",
		"not supported",
	} {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	return false
}

// isContextLengthError reconnait les libelles employes par les providers pour
// signaler un historique trop long. Aucun ne peut etre garanti, donc on en
// accepte plusieurs et on reste liberal : un faux positif coute une tentative
// supplementaire, un faux negatif fait echouer la requete alors qu'un modele
// plus grand aurait repondu.
func isContextLengthError(err *ProviderError) bool {
	haystack := strings.ToLower(err.Message + " " + err.Detail)
	switch err.Code {
	case "context_length_exceeded", "model_context_length_exceeded", "string_above_max_length":
		return true
	}
	for _, marker := range []string{
		"context length",
		"context_length",
		"context window",
		"maximum context",
		"too many tokens",
		"reduce the length",
		"prompt is too long",
		"input length",
	} {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	return false
}
