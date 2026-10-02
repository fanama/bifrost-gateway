package domain

import "context"

// LLMResult est la reponse d'un provider augmente du modele qui a reellement
// produit le texte.
//
// Le champ Model n'est pas cosmetique : le routeur peut substituer un autre
// modele a la configuration demandee, donc l'appelant ne doit pas supposer que
// le modele de sa configuration est celui qui a repondu.
type LLMResult struct {
	Content string
	Model   string
}

type LLMProvider interface {
	Chat(ctx context.Context, cfg *ChatConfig, messages []ChatMessage) (LLMResult, error)
}

// Classifier evalue la complexite d'une conversation et propose un tier de
// l'echelle de routage. confidence vaut entre 0 et 1.
//
// Le domaine ne fixe pas la maniere de classer : une implementation locale ou un
// service distant sont interchangeables. Une erreur n'est jamais bloquante,
// l'appelant doit alors conserver la configuration d'origine.
type Classifier interface {
	Classify(ctx context.Context, messages []ChatMessage) (tier string, confidence float64, err error)
}
