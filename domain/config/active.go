package config

import "bridge-gateway/domain/errs"

// MarkActive applique l'invariant \"une seule configuration active par projet\"
// a la collection fournie : la configuration id devient active, toutes les
// autres du meme projet sont desactivees, et les autres projets ne sont pas
// touches.
//
// La recherche de la cible est faite avant toute mutation : si id n'appartient
// pas au projet, la collection reste inchangee et ErrConfigNotFound est
// retournee. L'adaptateur peut ainsi faire echouer sa transaction sans laisser
// le projet sans configuration active.
func MarkActive(configs []ChatConfig, projectID string, id string) error {
	found := false
	for i := range configs {
		if configs[i].ProjectID == projectID && configs[i].ID == id {
			found = true
			break
		}
	}
	if !found {
		return errs.ErrConfigNotFound
	}

	for i := range configs {
		if configs[i].ProjectID != projectID {
			continue
		}
		configs[i].Active = configs[i].ID == id
	}
	return nil
}
