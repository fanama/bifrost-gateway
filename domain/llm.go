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

// StreamEvent est un fragment incremental d'une reponse.
//
// Delta ne contient que le texte nouveau : l'appelant le concatene pour
// reconstituer la reponse. Role n'est renseigne que sur le premier fragment,
// comme dans le protocole OpenAI. Done marque le dernier fragment : apres lui,
// plus rien n'arrive.
type StreamEvent struct {
	Delta string
	Role  string
	// Model est le modele qui a reellement produit le texte. Comme pour
	// LLMResult, le routeur peut substituer un autre modele, donc l'appelant ne
	// doit pas supposer celui de sa configuration.
	Model string
	Done  bool
	// Err signale une coupure de milieu de flux. Le client a deja recu des
	// fragments : la reponse HTTP est donc partie en 200 et ne peut plus
	//porter un statut d'erreur, seule une trame d'erreur SSE peut l'annoncer.
	Err error
}

// StreamingLLMProvider est l'extension optionnelle de LLMProvider qui produit
// la reponse par fragments.
//
// Elle est volontairement separee de LLMProvider : le streaming est un capacite
// de plus, pas une obligation. Un decorateur ou un provider qui ne l'implemente
// pas reste utilisable, l'appelant retombe alors sur Chat et redistribue le
// texte en un seul fragment.
type StreamingLLMProvider interface {
	// ChatStream ouvre le flux. La chaine produite est toujours close par
	// l'implementation, y compris en cas d'erreur : une erreur de milieu de
	// flux est transportee par la fermeture de la chaine, pas par une valeur
	// de retour, puisque l'appelant a deja recu des fragments a ce stade.
	ChatStream(ctx context.Context, cfg *ChatConfig, messages []ChatMessage) (<-chan StreamEvent, error)
}

// StreamViaFallback rend une reponse non streamee sous forme d'un flux d'un
// seul fragment. C'est le repli des implementations qui ne savent pas streamer :
// le client recoit une reponse SSE valide, simplement non progressive.
func StreamViaFallback(result LLMResult, err error) (<-chan StreamEvent, error) {
	if err != nil {
		return nil, err
	}
	out := make(chan StreamEvent, 1)
	out <- StreamEvent{Delta: result.Content, Role: "assistant", Model: result.Model, Done: true}
	close(out)
	return out, nil
}

// StreamFrom ouvre un flux sur n'importe quel LLMProvider : elle utilise
// ChatStream quand le provider sait streamer, et se replie sur Chat sinon.
//
// C'est le point unique de decision, partage par le cache, le routeur et le cas
// d'usage, pour qu'une implementation non streamee reste utilisable partout
// plutot que d'obliger chaque decorateur a refaire le test de type.
func StreamFrom(ctx context.Context, provider LLMProvider, cfg *ChatConfig, messages []ChatMessage) (<-chan StreamEvent, error) {
	if streaming, ok := provider.(StreamingLLMProvider); ok {
		return streaming.ChatStream(ctx, cfg, messages)
	}
	result, err := provider.Chat(ctx, cfg, messages)
	if err != nil {
		return nil, err
	}
	return StreamViaFallback(result, nil)
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
