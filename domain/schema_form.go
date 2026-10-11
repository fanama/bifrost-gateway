package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// Ce fichier contient les lectures inverses du constructeur de schema : a
// partir d'un JSON Schema deja enregistre, il rehyrate les champs du
// formulaire d'edition. Elles ne participent pas a la construction du schema
// lui-meme (schema.go / schema_property.go).

// EnumValues rend les valeurs d'une propriete "enum", pour rehyrater le
// formulaire a l'edition.
func EnumValues(property map[string]any) string {
	list, ok := property["enum"].([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(list))
	for _, v := range list {
		parts = append(parts, fmt.Sprintf("%v", v))
	}
	return strings.Join(parts, ", ")
}

// ConstraintString rend une contrainte numerique sous forme de chaine, pour
// rehydrater le formulaire a l'edition.
func ConstraintString(property map[string]any, name string) string {
	switch v := property[name].(type) {
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case string:
		return v
	}
	return ""
}

// ItemsType rend le type des elements d'un tableau declare.
func ItemsType(property map[string]any) string {
	items, ok := property["items"].(map[string]any)
	if !ok {
		return ""
	}
	t, _ := items["type"].(string)
	return t
}

// MinMaxString relit la borne saisie, qu'elle ait ete enregistree comme
// longueur de chaine ou comme borne numerique. Le formulaire n'expose qu'un
// couple de colonnes, il faut donc regarder les deux mots-cles.
func MinMaxString(property map[string]any, lengthKey, boundKey string) string {
	if v := ConstraintString(property, lengthKey); v != "" {
		return v
	}
	return ConstraintString(property, boundKey)
}
