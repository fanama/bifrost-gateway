package web

import (
	"context"
	"strings"

	"bridge-gateway/domain"
)

// Resolution des moteurs d'embedding proposes au testeur : liste curatee
// (domain.EmbeddingChoices) completee par les modeles du catalogue declares de
// type embedding, sans doublon par identifiant.

// embeddingChoices rend la liste complete proposee au testeur : la liste
// curatee, puis les modeles du catalogue declares de type embedding (ajoutes
// depuis la page Modeles), sans doublon par identifiant. Une erreur de lecture
// est remontee : taire le catalogue priverait silencieusement l'utilisateur
// de ses propres modeles.
func (s *embeddingsController) embeddingChoices(ctx context.Context) ([]domain.EmbeddingChoice, error) {
	choices := make([]domain.EmbeddingChoice, 0, len(domain.EmbeddingChoices)+4)
	seen := make(map[string]bool, len(domain.EmbeddingChoices)+4)
	for _, c := range domain.EmbeddingChoices {
		choices = append(choices, c)
		seen[strings.ToLower(c.Model)] = true
	}
	models, err := s.models.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range models {
		choice, ok := domain.EmbeddingChoiceFromModel(m)
		if !ok {
			continue
		}
		key := strings.ToLower(choice.Model)
		if seen[key] {
			continue
		}
		seen[key] = true
		choices = append(choices, choice)
	}
	return choices, nil
}

// findEmbeddingChoice cherche un modele dans la liste complete du testeur,
// insensible a la casse : un modele issu du catalogue se selectionne et se
// resout exactement comme un choix curate.
func (s *embeddingsController) findEmbeddingChoice(ctx context.Context, modelID string) (*domain.EmbeddingChoice, error) {
	if choice := domain.FindEmbeddingChoice(modelID); choice != nil {
		return choice, nil
	}
	choices, err := s.embeddingChoices(ctx)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(modelID)
	for i := range choices {
		if strings.EqualFold(choices[i].Model, name) {
			return &choices[i], nil
		}
	}
	return nil, nil
}
