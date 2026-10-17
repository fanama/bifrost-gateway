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

	fields := schemaFieldsFromForm(r)
	if len(fields) == 0 {
		return ""
	}

	// An invalid build must not block saving the rest of the configuration:
	// keep no schema, and validation will report that json_schema requires one.
	schema, err := domain.BuildJSONSchema(fields)
	if err != nil {
		return ""
	}
	return schema
}

// schemaFieldsFromForm reads the columns of the visual schema builder. Each
// column is a list of same-named inputs ordered like the rows. Missing columns
// stay empty slices: at() on an out-of-range slice yields the zero value instead
// of panicking, so the code does not depend on how many columns were sent.
func schemaFieldsFromForm(r *http.Request) []domain.SchemaField {
	names := r.Form["schema_field_name[]"]
	required := requiredIndexes(r.Form["schema_field_required[]"])

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
			Type:        formColumn(types, i),
			Description: formColumn(descriptions, i),
			Required:    required[i],
			Enum:        formColumn(enums, i),
			Min:         formColumn(mins, i),
			Max:         formColumn(maxs, i),
			Pattern:     formColumn(patterns, i),
			ItemsType:   formColumn(itemTypes, i),
		})
	}
	return fields
}

func requiredIndexes(values []string) map[int]bool {
	required := map[int]bool{}
	for _, v := range values {
		if i, err := strconv.Atoi(v); err == nil {
			required[i] = true
		}
	}
	return required
}

func formColumn(column []string, i int) string {
	if i < len(column) {
		return column[i]
	}
	return ""
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
	if schema, err := domain.ParseJSONSchema(raw); err == nil {
		rows = rowsFromSchema(schema)
	}
	if len(rows) == 0 {
		// Always one line: an empty table offers no way to add a first field.
		rows = append(rows, schemaRow{Index: 0, Type: "string"})
	}
	return rows
}

func rowsFromSchema(schema map[string]any) []schemaRow {
	properties, _ := schema["properties"].(map[string]any)
	required := requiredPropertyNames(schema)
	names := sortedPropertyNames(properties)

	rows := make([]schemaRow, 0, len(names))
	for i, name := range names {
		property, _ := properties[name].(map[string]any)
		rows = append(rows, schemaRowFor(i, name, property, required[name]))
	}
	return rows
}

func requiredPropertyNames(schema map[string]any) map[string]bool {
	required := map[string]bool{}
	list, ok := schema["required"].([]any)
	if !ok {
		return required
	}
	for _, item := range list {
		if name, ok := item.(string); ok {
			required[name] = true
		}
	}
	return required
}

// sortedPropertyNames keeps the edit form stable: object key order is
// meaningless in JSON but not in a form.
func sortedPropertyNames(properties map[string]any) []string {
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// schemaRowFor re-reads a stored property with the same helpers that write it,
// so an enum or a minimum survives an edit round-trip.
func schemaRowFor(index int, name string, property map[string]any, required bool) schemaRow {
	row := schemaRow{Index: index, Name: name, Type: "string", Required: required}
	if t, ok := property["type"].(string); ok && t != "" {
		row.Type = t
	}
	if d, ok := property["description"].(string); ok {
		row.Description = d
	}
	row.Enum = domain.EnumValues(property)
	row.Min = domain.MinMaxString(property, "minLength", "minimum")
	row.Max = domain.MinMaxString(property, "maxLength", "maximum")
	if p, ok := property["pattern"].(string); ok {
		row.Pattern = p
	}
	row.ItemsType = domain.ItemsType(property)
	return row
}

// editingSchema rend le schema a reafficher, celui d'une configuration en cours
// d'édition.
func editingSchema(editing *domain.ChatConfig) string {
	if editing == nil {
		return ""
	}
	return editing.ResponseSchema
}
