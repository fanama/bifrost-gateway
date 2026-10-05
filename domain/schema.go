package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// SchemaField est un champ du constructeur visuel de JSON Schema. L'interface ne
// fait saisir que ces quelques proprietes : le formulaire produit un schema, et
// non un blob JSON a mettre en forme.
//
// Ce type vit dans le domaine parce que la regle qui produit le schema est une
// regle metier : l'UI doit livrer exactement ce que le provider recevra.
type SchemaField struct {
	Name        string
	Type        string
	Description string
	Required    bool

	// Constraints. Elles ne sont ajoutees au schema que si l'utilisateur les a
	// saisies : un schema reste minimal tant que rien n'est demande, ce qui evite
	// d'envoyer au provider des mots-cles qu'il ne saurait pas traiter.
	//
	// Ces valeurs sont des chaines parce qu'elles viennent d'un formulaire ; leur
	// conversion en nombre est faite ici, avec un refus explicite plutot qu'un
	// zero silencieux.
	Enum string
	// Min et Max sont interpretes selon le type du champ : longueur pour une
	// chaine, borne de valeur pour un nombre. Un seul couple de colonnes suffit
	// a couvrir les deux, et l'interface n'expose pas deux paires de champs qui
	// ne seraient jamais saisies simultanement.
	Min       string
	Max       string
	Pattern   string
	ItemsType string
}

// schemaTypes est l'ensemble des types simples exposed par le constructeur.
// Les types listes sont ceux que tous les providers de sortie structuree
// comprennent ; un type absent serait rejete a l'appel.
var schemaTypes = []string{"string", "number", "integer", "boolean", "object", "array"}

// SchemaFieldTypes liste les types proposes par le constructeur.
func SchemaFieldTypes() []string {
	out := make([]string, len(schemaTypes))
	copy(out, schemaTypes)
	return out
}

// BuildJSONSchema rend le JSON Schema correspondant aux champs saisis.
//
// additionalProperties vaut false : c'est ce qui interdit au modele d'inventer
// des champs, et c'est implicitement exige par le mode strict d'OpenAI.
func BuildJSONSchema(fields []SchemaField) (string, error) {
	cleaned := make([]SchemaField, 0, len(fields))
	seen := map[string]bool{}

	for _, f := range fields {
		name := strings.TrimSpace(f.Name)
		if name == "" {
			// Une ligne vide est une ligne que l'utilisateur n'a pas encore
			// remplie : elle ne doit pas produire un champ sans nom.
			continue
		}
		if seen[name] {
			return "", fmt.Errorf("champ duplique : %q", name)
		}
		seen[name] = true

		kind := strings.ToLower(strings.TrimSpace(f.Type))
		if kind == "" {
			kind = "string"
		}
		if !isSchemaType(kind) {
			return "", fmt.Errorf("type inconnu pour le champ %q : %q", name, f.Type)
		}

		cleaned = append(cleaned, SchemaField{
			Name:        name,
			Type:        kind,
			Description: strings.TrimSpace(f.Description),
			Required:    f.Required,
			Enum:        strings.TrimSpace(f.Enum),
			Min:         strings.TrimSpace(f.Min),
			Max:         strings.TrimSpace(f.Max),
			Pattern:     strings.TrimSpace(f.Pattern),
			ItemsType:   strings.ToLower(strings.TrimSpace(f.ItemsType)),
		})
	}

	if len(cleaned) == 0 {
		return "", fmt.Errorf("le schema doit comporter au moins un champ")
	}

	properties := map[string]any{}
	required := make([]string, 0, len(cleaned))
	for _, f := range cleaned {
		property, err := buildProperty(f)
		if err != nil {
			return "", fmt.Errorf("champ %q : %w", f.Name, err)
		}
		properties[f.Name] = property
		if f.Required {
			required = append(required, f.Name)
		}
	}

	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	// La cle "required" n'est posee que s'il y a des champs obligatoires : un
	// tableau vide est invalide en JSON Schema et rejetera le schema.
	if len(required) > 0 {
		sort.Strings(required)
		schema["required"] = required
	}

	encoded, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return "", fmt.Errorf("schema serialisable : %v", err)
	}
	return string(encoded), nil
}

func isSchemaType(kind string) bool {
	for _, known := range schemaTypes {
		if known == kind {
			return true
		}
	}
	return false
}

// SchemaAllowsStrict indique si un schema peut etre envoye en mode strict.
//
// OpenAI exige en mode strict que chaque propriete declaree figure aussi dans
// "required". Un schema ou un champ est facultatif ne peut donc pas y passer :
// annoncer strict dans ce cas ferait rejeter la requete par le provider. La
// regle est appliquee ici plutot que laissee au provider, pour que le mode
// strict annonce soit toujours tenable.
func SchemaAllowsStrict(schema map[string]any) bool {
	properties, _ := schema["properties"].(map[string]any)
	if len(properties) == 0 {
		return false
	}

	required := map[string]bool{}
	if list, ok := schema["required"].([]any); ok {
		for _, item := range list {
			if name, ok := item.(string); ok {
				required[name] = true
			}
		}
	}

	for name := range properties {
		if !required[name] {
			return false
		}
	}
	return true
}

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

	switch f.Type {
	case "string":
		if v, ok, err := intConstraint("minLength", f.Min); err != nil {
			return nil, err
		} else if ok {
			property["minLength"] = v
		}
		if v, ok, err := intConstraint("maxLength", f.Max); err != nil {
			return nil, err
		} else if ok {
			property["maxLength"] = v
		}
		if f.Pattern != "" {
			property["pattern"] = f.Pattern
		}
	case "number", "integer":
		if v, ok, err := floatConstraint("minimum", f.Min); err != nil {
			return nil, err
		} else if ok {
			property["minimum"] = v
		}
		if v, ok, err := floatConstraint("maximum", f.Max); err != nil {
			return nil, err
		} else if ok {
			property["maximum"] = v
		}
	case "array":
		itemType := f.ItemsType
		if itemType == "" {
			itemType = "string"
		}
		if !isSchemaType(itemType) {
			return nil, fmt.Errorf("type d'element inconnu : %q", f.ItemsType)
		}
		property["items"] = map[string]any{"type": itemType}
	}

	return property, nil
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
