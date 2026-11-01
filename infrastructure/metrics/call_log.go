package metrics

import (
	"strings"
	"time"

	"bridge-gateway/domain"
)

// newCallLog assemble l'entree de journal d'un appel provider : la requete
// envoyee (les messages) et la reponse produite.
//
// servedModel est le modele qui a reellement repondu ; il peut differer de la
// configuration quand le routeur a escalade. A defaut, le modele demande est
// retenu, pour qu'une entree en erreur avant reponse reste lisible.
func newCallLog(
	cfg *domain.ChatConfig,
	messages []domain.ChatMessage,
	servedModel, output string,
	stream bool,
	duration time.Duration,
	err error,
) domain.CallLog {
	model := strings.TrimSpace(servedModel)
	if model == "" {
		model = cfg.Model
	}
	entry := domain.CallLog{
		Time:     time.Now(),
		Provider: cfg.Provider,
		Model:    model,
		Stream:   stream,
		Duration: duration,
		Input:    formatMessages(messages),
		Output:   output,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	return entry
}

// formatMessages rend la requete sous une forme lisible dans la modale et
// exportable telle quelle : un bloc « role: contenu » par message.
func formatMessages(messages []domain.ChatMessage) string {
	if len(messages) == 0 {
		return ""
	}
	var builder strings.Builder
	for i, message := range messages {
		if i > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString(message.Role)
		builder.WriteString(": ")
		builder.WriteString(message.Content)
	}
	return builder.String()
}
