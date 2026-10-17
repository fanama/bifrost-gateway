package config

import (
	"encoding/json"
	"fmt"
	"sort"
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
	cleaned, err := cleanSchemaFields(fields)
	if err != nil {
		return "", err
	}
	if len(cleaned) == 0 {
		return "", fmt.Errorf("le schema doit comporter au moins un champ")
	}

	schema, err := schemaObject(cleaned)
	if err != nil {
		return "", err
	}
	encoded, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return "", fmt.Errorf("schema serialisable : %v", err)
	}
	return string(encoded), nil
}

// cleanSchemaFields trims and validates the submitted rows, dropping blank
// rows the user has not filled in yet.
func cleanSchemaFields(fields []SchemaField) ([]SchemaField, error) {
	cleaned := make([]SchemaField, 0, len(fields))
	seen := map[string]bool{}
	for _, f := range fields {
		name := strings.TrimSpace(f.Name)
		if name == "" {
			continue
		}
		if seen[name] {
			return nil, fmt.Errorf("champ duplique : %q", name)
		}
		seen[name] = true

		kind := strings.ToLower(strings.TrimSpace(f.Type))
		if kind == "" {
			kind = "string"
		}
		if !isSchemaType(kind) {
			return nil, fmt.Errorf("type inconnu pour le champ %q : %q", name, f.Type)
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
	return cleaned, nil
}

// schemaObject builds the object schema and its required list. The "required"
// key is only set when non-empty: an empty array is invalid JSON Schema.
func schemaObject(fields []SchemaField) (map[string]any, error) {
	properties := map[string]any{}
	required := make([]string, 0, len(fields))
	for _, f := range fields {
		property, err := buildProperty(f)
		if err != nil {
			return nil, fmt.Errorf("champ %q : %w", f.Name, err)
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
	if len(required) > 0 {
		sort.Strings(required)
		schema["required"] = required
	}
	return schema, nil
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
