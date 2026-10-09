package web

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"bridge-gateway/domain"
)

// Parsing et relecture des formulaires de configuration : parties pures,
// partagees par le chat et les projets, sans dependance au rendu.
func parseConfigForm(r *http.Request) *domain.ChatConfig {
	_ = r.ParseForm()
	return &domain.ChatConfig{
		Name:             strings.TrimSpace(r.FormValue("name")),
		Provider:         strings.ToLower(strings.TrimSpace(r.FormValue("provider"))),
		Model:            strings.TrimSpace(r.FormValue("model")),
		Tier:             strings.ToLower(strings.TrimSpace(r.FormValue("tier"))),
		BaseURL:          strings.TrimSpace(r.FormValue("base_url")),
		APIKey:           strings.TrimSpace(r.FormValue("api_key")),
		SystemPrompt:     strings.TrimSpace(r.FormValue("system_prompt")),
		Temperature:      parseFloatOpt(r.FormValue("temperature")),
		TopP:             parseFloatOpt(r.FormValue("top_p")),
		MaxTokens:        parseIntOpt(r.FormValue("max_tokens")),
		FrequencyPenalty: parseFloatOpt(r.FormValue("frequency_penalty")),
		PresencePenalty:  parseFloatOpt(r.FormValue("presence_penalty")),
		ResponseFormat:   strings.TrimSpace(r.FormValue("response_format")),
		ResponseSchema:   strings.TrimSpace(r.FormValue("response_schema")),
	}
}

// parseSchemaRows rend le JSON Schema construit depuis le formulaire visuel.
//
// La textarea response_schema reste prioritaire : elle permet de saisir un
// schema que le constructeur n'exprime pas (types unions, imbrication), sans
// perdre l'assistance visuelle pour le cas courant.
func parseSchemaRows(r *http.Request) string {
	if raw := strings.TrimSpace(r.FormValue("response_schema")); raw != "" {
		return raw
	}

	names := r.Form["schema_field_name[]"]
	required := map[int]bool{}
	for _, v := range r.Form["schema_field_required[]"] {
		if i, err := strconv.Atoi(v); err == nil {
			required[i] = true
		}
	}

	// Chaque colonne du tableau est une liste d'inputs de meme nom, dont l'ordre
	// suit l'ordre des lignes. Les colonnes absentes restent des slices vides :
	// at() sur une slice hors bornes donne la valeur nulle plutot qu'une panique,
	// ce qui evite de dependre du nombre exact de colonnes envoyees.
	at := func(column []string, i int) string {
		if i < len(column) {
			return column[i]
		}
		return ""
	}

	types := r.Form["schema_field_type[]"]
	descriptions := r.Form["schema_field_description[]"]
	enums := r.Form["schema_field_enum[]"]
	mins := r.Form["schema_field_min[]"]
	maxs := r.Form["schema_field_max[]"]
	patterns := r.Form["schema_field_pattern[]"]
	itemTypes := r.Form["schema_field_items[]"]

	fields := make([]domain.SchemaField, 0, len(names))
	for i, name := range names {
		fields = append(fields, domain.SchemaField{
			Name:        name,
			Type:        at(types, i),
			Description: at(descriptions, i),
			Required:    required[i],
			Enum:        at(enums, i),
			Min:         at(mins, i),
			Max:         at(maxs, i),
			Pattern:     at(patterns, i),
			ItemsType:   at(itemTypes, i),
		})
	}

	if len(fields) == 0 {
		return ""
	}

	// Une construction invalide ne doit pas bloquer la sauvegarde du reste de la
	// configuration : on ne retient alors aucun schema, et la validation de la
	// configuration signalera que json_schema en exige un.
	schema, err := domain.BuildJSONSchema(fields)
	if err != nil {
		return ""
	}
	return schema
}

func parseFloatOpt(raw string) *float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}
	return &v
}

func parseIntOpt(raw string) *int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &v
}

// schemaRow est une ligne du constructeur visuel. Index est la position
// stable de la ligne, reprise dans la case a cocher "obligatoire" pour que
// l'index survive au tri des champs du schema.
type schemaRow struct {
	Index       int
	Name        string
	Type        string
	Description string
	Required    bool
	Enum        string
	Min         string
	Max         string
	Pattern     string
	ItemsType   string
}

type configFormData struct {
	Editing          *domain.ChatConfig
	Action           string
	Target           string
	ProjectID        string
	IsCreate         bool
	UseDefaults      bool
	Providers        []domain.Provider
	SelectedProvider string
	SelectedModel    string
	Models           []string
	// SchemaFields restitue les champs du schema enregistre pour que l'edition
	// parte de l'etat reel plutot que d'un formulaire vide.
	SchemaFields     []schemaRow
	SchemaFieldTypes []string
	ResponseSchema   string
	// EditingFormat est le format reellement applique. Il peut differer du champ
	// brut enregistre : un schema stocke vaut declaration d'intention et impose
	// json_schema meme si le select avait ete laisse sur "aucun". Afficher la
	// valeur brute laisserait croire que le schema est ignore.
	EditingFormat string
}

// schemaRowsFromStored relit un schema enregistre pour reafficher ses champs.
// Un schema illisible ne bloque pas l'edition : le formulaire se presente alors
// avec une seule ligne vide, et la validation signalera le probleme a
// l'enregistrement.
func schemaRowsFromStored(raw string) []schemaRow {
	rows := []schemaRow{}

	schema, err := domain.ParseJSONSchema(raw)
	if err == nil {
		properties, _ := schema["properties"].(map[string]any)
		required := map[string]bool{}
		if list, ok := schema["required"].([]any); ok {
			for _, item := range list {
				if name, ok := item.(string); ok {
					required[name] = true
				}
			}
		}

		names := make([]string, 0, len(properties))
		for name := range properties {
			names = append(names, name)
		}
		// Un schema est un objet : l'ordre de ses cles n'a pas de sens en JSON,
		// mais il en a un dans un formulaire. Le trier rend l'edition stable.
		sort.Strings(names)

		for i, name := range names {
			property, _ := properties[name].(map[string]any)
			row := schemaRow{Index: i, Name: name, Type: "string", Required: required[name]}
			if t, ok := property["type"].(string); ok && t != "" {
				row.Type = t
			}
			if d, ok := property["description"].(string); ok {
				row.Description = d
			}
			// Les contraintes sont relues avec le meme code qui les ecrit : sans
			// cela, un champ enregistre avec un enum ou un minimum ressortirait
			// vide a l'edition, et l'utilisateur perdrait sa saisie.
			row.Enum = domain.EnumValues(property)
			row.Min = domain.MinMaxString(property, "minLength", "minimum")
			row.Max = domain.MinMaxString(property, "maxLength", "maximum")
			if p, ok := property["pattern"].(string); ok {
				row.Pattern = p
			}
			row.ItemsType = domain.ItemsType(property)
			rows = append(rows, row)
		}
	}

	if len(rows) == 0 {
		// Toujours une ligne : un tableau vide n'offre aucun moyen d'ajouter un
		// premier champ.
		rows = append(rows, schemaRow{Index: 0, Type: "string"})
	}
	return rows
}

// editingSchema rend le schema a reafficher, celui d'une configuration en cours
// d'édition.
func editingSchema(editing *domain.ChatConfig) string {
	if editing == nil {
		return ""
	}
	return editing.ResponseSchema
}
