package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Valeurs acceptees pour ChatConfig.ResponseFormat, alignees sur l'API OpenAI.
const (
	ResponseFormatText       = "text"
	ResponseFormatJSONObject = "json_object"
	ResponseFormatJSONSchema = "json_schema"
)

// EffectiveResponseFormat rend le format reellement applique.
//
// Un schema enregistre vaut declaration d'intention : l'utilisateur a saisi un
// constructeur de schema, il s'attend a ce qu'il contraigne les reponses. La
// seule maniere de l'ignorer serait que le select "Format de reponse" soit
// laisse sur "aucun", ce qui arrive facilement puisque les deux contrôles sont
// independants dans le formulaire. Sans cette regle, la configuration est
// validee, le schema est conserve, et le modele repond en texte libre : le
// silence du cote serveur est alors indiscernable d'un schema applique.
func (c *ChatConfig) EffectiveResponseFormat() string {
	format := strings.TrimSpace(c.ResponseFormat)
	if format == "" && strings.TrimSpace(c.ResponseSchema) != "" {
		return ResponseFormatJSONSchema
	}
	return format
}

// ResponseFormatMap rend la forme attendue par l'API OpenAI. Elle renvoie nil
// lorsque rien n'est demande, ce que l'infrastructure interprete comme
// "ne pas envoyer le parametre".
func (c *ChatConfig) ResponseFormatMap() map[string]any {
	switch c.EffectiveResponseFormat() {
	case ResponseFormatJSONObject:
		return map[string]any{"type": ResponseFormatJSONObject}
	case ResponseFormatText:
		return map[string]any{"type": ResponseFormatText}
	case ResponseFormatJSONSchema:
		schema, err := c.ResponseSchemaObject()
		if err != nil {
			// La validation bloque cette combinaison a l'enregistrement ; on
			// degrade plutot que de faire echouer l'appel.
			return nil
		}
		return map[string]any{
			"type": ResponseFormatJSONSchema,
			"json_schema": map[string]any{
				"name":   c.ResponseSchemaName(),
				"schema": schema,
				// Le mode strict est ce qui distingue un schema applique d'une
				// simple suggestion. Il n'est annonce que lorsqu'il est tenable :
				// un schema comportant un champ facultatif serait refuse par le
				// provider si strict valait true.
				"strict": SchemaAllowsStrict(schema),
			},
		}
	}
	return nil
}

// ResponseSchemaName derive le nom exige par l'API. OpenAI impose un nom
// alphanumerique, alors qu'un nom de configuration est libre.
func (c *ChatConfig) ResponseSchemaName() string {
	var out []rune
	for _, r := range strings.TrimSpace(c.Name) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	name := string(out)
	if name == "" {
		return "response"
	}
	return name
}

// ResponseSchemaObject relit le schema stocke. Seuls les schemas d'objet sont
// acceptes : tous les providers de sortie structuree les attendent ainsi.
func (c *ChatConfig) ResponseSchemaObject() (map[string]any, error) {
	return ParseJSONSchema(c.ResponseSchema)
}

// ParseJSONSchema relit un JSON Schema d'objet. Partage par la validation de la
// configuration et celle d'une requete, afin que les deux acceptent exactement
// les memes schemas.
func ParseJSONSchema(raw string) (map[string]any, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("aucun schema fourni")
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(trimmed), &schema); err != nil {
		return nil, fmt.Errorf("schema JSON invalide: %v", err)
	}
	if t, _ := schema["type"].(string); t != "object" {
		return nil, errors.New(`le schema doit declarer "type": "object"`)
	}
	if _, ok := schema["properties"]; !ok {
		return nil, errors.New(`le schema doit declarer "properties"`)
	}
	return schema, nil
}

// ParseResponseFormat traduit la forme OpenAI d'un response_format en couple
// (format, schema), afin que la configuration reste l'unique representation en
// circulation. Sans cela, la surcharge d'une requete devrait traverser
// l'infrastructure avec une forme differente de celle de la configuration.
func ParseResponseFormat(m map[string]any) (format string, schema string, err error) {
	if m == nil {
		return "", "", nil
	}
	rawType, _ := m["type"].(string)
	switch strings.ToLower(strings.TrimSpace(rawType)) {
	case "", ResponseFormatText:
		return ResponseFormatText, "", nil
	case ResponseFormatJSONObject:
		return ResponseFormatJSONObject, "", nil
	case ResponseFormatJSONSchema:
		spec, _ := m["json_schema"].(map[string]any)
		if spec == nil {
			return "", "", errors.New(`"json_schema" est requis pour le type json_schema`)
		}
		body, ok := spec["schema"]
		if !ok {
			return "", "", errors.New(`"json_schema.schema" est requis`)
		}
		encoded, err := json.Marshal(body)
		if err != nil {
			return "", "", fmt.Errorf("schema illisible: %v", err)
		}
		var checked map[string]any
		if err := json.Unmarshal(encoded, &checked); err != nil {
			return "", "", fmt.Errorf("schema JSON invalide: %v", err)
		}
		if t, _ := checked["type"].(string); t != "object" {
			return "", "", errors.New(`le schema doit declarer "type": "object"`)
		}
		if _, ok := checked["properties"]; !ok {
			return "", "", errors.New(`le schema doit declarer "properties"`)
		}
		return ResponseFormatJSONSchema, string(encoded), nil
	default:
		return "", "", fmt.Errorf("type inconnu: %q", rawType)
	}
}
