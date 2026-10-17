package config

import (
	"fmt"
	"strconv"
	"strings"
)

// buildProperty rend la propriete JSON Schema d'un champ, avec les contraintes
// que l'utilisateur a effectivement saisies.
//
// Les contraintes sont groupees par type : proposer "minimum" sur une chaine, ou
// "minLength" sur un entier, produirait un schema que le provider refuse. Un
// champ dont le type change voit donc ses contraintes obsoletees simplement
// ignorees plutot que rejetees a l'enregistrement.
func buildProperty(f SchemaField) (map[string]any, error) {
	property := map[string]any{"type": f.Type}
	if f.Description != "" {
		property["description"] = f.Description
	}

	if values := splitEnum(f.Enum); len(values) > 0 {
		property["enum"] = values
	}

	var err error
	switch f.Type {
	case "string":
		err = applyStringConstraints(property, f)
	case "number", "integer":
		err = applyNumberConstraints(property, f)
	case "array":
		err = applyArrayItems(property, f)
	}
	if err != nil {
		return nil, err
	}
	return property, nil
}

func applyStringConstraints(property map[string]any, f SchemaField) error {
	if v, ok, err := intConstraint("minLength", f.Min); err != nil {
		return err
	} else if ok {
		property["minLength"] = v
	}
	if v, ok, err := intConstraint("maxLength", f.Max); err != nil {
		return err
	} else if ok {
		property["maxLength"] = v
	}
	if f.Pattern != "" {
		property["pattern"] = f.Pattern
	}
	return nil
}

func applyNumberConstraints(property map[string]any, f SchemaField) error {
	if v, ok, err := floatConstraint("minimum", f.Min); err != nil {
		return err
	} else if ok {
		property["minimum"] = v
	}
	if v, ok, err := floatConstraint("maximum", f.Max); err != nil {
		return err
	} else if ok {
		property["maximum"] = v
	}
	return nil
}

func applyArrayItems(property map[string]any, f SchemaField) error {
	itemType := f.ItemsType
	if itemType == "" {
		itemType = "string"
	}
	if !isSchemaType(itemType) {
		return fmt.Errorf("type d'element inconnu : %q", f.ItemsType)
	}
	property["items"] = map[string]any{"type": itemType}
	return nil
}

// splitEnum lit une liste de valeurs separees par des virgules. Les espaces sont
// retires pour que "a, b , c" et "a,b,c" donnent le meme schema.
func splitEnum(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func intConstraint(name, raw string) (int, bool, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return 0, false, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false, fmt.Errorf("%s : valeur invalide %q", name, raw)
	}
	if n < 0 {
		return 0, false, fmt.Errorf("%s : ne peut pas etre negatif", name)
	}
	return n, true, nil
}

func floatConstraint(name, raw string) (float64, bool, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return 0, false, nil
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, false, fmt.Errorf("%s : valeur invalide %q", name, raw)
	}
	return n, true, nil
}
