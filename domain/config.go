package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type ChatConfig struct {
	ID               string   `json:"id"`
	ProjectID        string   `json:"project_id"`
	Name             string   `json:"name"`
	Provider         string   `json:"provider"`
	Model            string   `json:"model"`
	Tier             string   `json:"tier,omitempty"`
	BaseURL          string   `json:"base_url,omitempty"`
	APIKey           string   `json:"api_key,omitempty"`
	SystemPrompt     string   `json:"system_prompt,omitempty"`
	Temperature      *float64 `json:"temperature,omitempty"`
	TopP             *float64 `json:"top_p,omitempty"`
	MaxTokens        *int     `json:"max_tokens,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
	ResponseFormat   string   `json:"response_format,omitempty"`
	// ResponseSchema porte le JSON Schema impose au modele lorsque
	// ResponseFormat vaut "json_schema". Il est stocke en clair : c'est ce que le
	// provider attend, et il doit etre relu sans reinterpretation.
	ResponseSchema string    `json:"response_schema,omitempty"`
	Active         bool      `json:"active"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Tiers de routage, du plus ecconomique au plus puissant. Une configuration
// sans tier reste utilisable : elle sert de point de depart mais ne figure pas
// dans l'echelle de bascule.
const (
	TierFast      = "fast"
	TierBalanced  = "balanced"
	TierFrontier  = "frontier"
	RoutingTiers  = 3
	tierUnordered = -1
)

// tierRanks fixe l'ordre de l'echelle. Il est volontairement code en dur : le
// rang d'un tier est une decision de produit, pas une donnee de configuration.
var tierRanks = map[string]int{
	TierFast:     0,
	TierBalanced: 1,
	TierFrontier: 2,
}

// TierRank renvoie le rang d'un tier dans l'echelle de routage. Un tier inconnu
// ou absent renvoie tierUnordered : la configuration est alors ignoree par le
// routeur plutot que de le placer arbitrairement en bout de chaine.
func (c *ChatConfig) TierRank() int {
	if c == nil {
		return tierUnordered
	}
	return TierRankOf(c.Tier)
}

// TierRankOf expose le rang d'un tier a partir de son seul nom.
func TierRankOf(tier string) int {
	if rank, ok := tierRanks[strings.ToLower(strings.TrimSpace(tier))]; ok {
		return rank
	}
	return tierUnordered
}

func (c *ChatConfig) Validate() error {
	var missing []string
	if strings.TrimSpace(c.Name) == "" {
		missing = append(missing, "name")
	}
	if strings.TrimSpace(c.Provider) == "" {
		missing = append(missing, "provider")
	}
	if strings.TrimSpace(c.Model) == "" {
		missing = append(missing, "model")
	}
	if len(missing) > 0 {
		return &ValidationError{Fields: missing}
	}

	if c.TierRank() == tierUnordered && strings.TrimSpace(c.Tier) != "" {
		return &ValidationError{Fields: []string{"tier"}, Reason: "doit valoir fast, balanced, frontier ou etre vide"}
	}

	if c.Temperature != nil && (*c.Temperature < 0 || *c.Temperature > 2) {
		return &ValidationError{Fields: []string{"temperature"}, Reason: "doit etre compris entre 0 et 2"}
	}
	if c.TopP != nil && (*c.TopP <= 0 || *c.TopP > 1) {
		return &ValidationError{Fields: []string{"top_p"}, Reason: "doit etre compris entre 0 (exclus) et 1"}
	}
	if c.MaxTokens != nil && *c.MaxTokens < 1 {
		return &ValidationError{Fields: []string{"max_tokens"}, Reason: "doit etre superieur a 0"}
	}
	if c.FrequencyPenalty != nil && (*c.FrequencyPenalty < -2 || *c.FrequencyPenalty > 2) {
		return &ValidationError{Fields: []string{"frequency_penalty"}, Reason: "doit etre compris entre -2 et 2"}
	}
	if c.PresencePenalty != nil && (*c.PresencePenalty < -2 || *c.PresencePenalty > 2) {
		return &ValidationError{Fields: []string{"presence_penalty"}, Reason: "doit etre compris entre -2 et 2"}
	}
	switch c.EffectiveResponseFormat() {
	case "", ResponseFormatText, ResponseFormatJSONObject:
	case ResponseFormatJSONSchema:
		if _, err := c.ResponseSchemaObject(); err != nil {
			return &ValidationError{Fields: []string{"response_schema"}, Reason: err.Error()}
		}
	default:
		return &ValidationError{Fields: []string{"response_format"}, Reason: "valeur inconnue"}
	}
	return nil
}

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

// TierOptions liste les tiers proposes par l'interface, dans l'ordre de l'echelle.
func TierOptions() []string {
	return []string{TierFast, TierBalanced, TierFrontier}
}

type ChatConfigRepository interface {
	UpdatableRepository[ChatConfig]
	ListByProject(ctx context.Context, projectID string) ([]ChatConfig, error)
	GetActiveByProject(ctx context.Context, projectID string) (*ChatConfig, error)
	SetActive(ctx context.Context, projectID string, id string) error
}
