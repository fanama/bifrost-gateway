package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// formRequest fabrique la requete que le navigateur envoie pour le tableau du
// constructeur de schema. Les slices d'un formulaire HTML se retrouvent dans
// r.Form sous forme de colonnes paralleles, d'ou la construction par nombre de
// lignes.
func formRequest(t *testing.T, rows [][]string) *http.Request {
	t.Helper()
	form := url.Values{}
	columns := []string{
		"schema_field_name",
		"schema_field_type",
		"schema_field_description",
		"schema_field_enum",
		"schema_field_min",
		"schema_field_max",
		"schema_field_pattern",
		"schema_field_items",
	}
	for c, column := range columns {
		for _, row := range rows {
			value := ""
			if c < len(row) {
				value = row[c]
			}
			form.Add(column+"[]", value)
		}
	}
	r := httptest.NewRequest("POST", "/projects/p/configs", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	return r
}

func decode(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("schema illisible : %v\n%s", err, raw)
	}
	return out
}

// TestSchemaRowsRoundTrip ferme la boucle du formulaire : ce que le tableau
// produit doit ressortir identique a l'edition, colonne par colonne. C'est la
// moitie retour du parcours, celle que l'utilisateur voit en rouvrant la
// configuration.
func TestSchemaRowsRoundTrip(t *testing.T) {
	rows := [][]string{
		{"ville", "string", "Ville", "Paris, Lyon", "2", "40", "^[A-Z]", ""},
		{"population", "integer", "Habitants", "", "0", "20000000", "", ""},
		{"actif", "boolean", "Interrompu", "", "", "", "", ""},
		{"tags", "array", "Mots-cles", "", "", "", "", "string"},
	}
	schema := parseSchemaRows(formRequest(t, rows))

	back := schemaRowsFromStored(schema)
	if len(back) != len(rows) {
		t.Fatalf("round-trip : %d lignes saisies, %d relues", len(rows), len(back))
	}

	// Les lignes sont triees par nom a la relecture ; on compare donc par nom.
	byName := map[string]schemaRow{}
	for _, row := range back {
		byName[row.Name] = row
	}

	for _, want := range rows {
		got, ok := byName[want[0]]
		if !ok {
			t.Fatalf("champ %q absent apres relecture (relu : %v)", want[0], byName)
		}
		if got.Type != want[1] {
			t.Errorf("%s : type %q, attendu %q", want[0], got.Type, want[1])
		}
		if got.Description != want[2] {
			t.Errorf("%s : description %q, attendu %q", want[0], got.Description, want[2])
		}
		if got.Enum != want[3] {
			t.Errorf("%s : enum %q, attendu %q", want[0], got.Enum, want[3])
		}
		if got.Min != want[4] {
			t.Errorf("%s : min %q, attendu %q", want[0], got.Min, want[4])
		}
		if got.Max != want[5] {
			t.Errorf("%s : max %q, attendu %q", want[0], got.Max, want[5])
		}
		if got.Pattern != want[6] {
			t.Errorf("%s : motif %q, attendu %q", want[0], got.Pattern, want[6])
		}
		if got.ItemsType != want[7] {
			t.Errorf("%s : type d'element %q, attendu %q", want[0], got.ItemsType, want[7])
		}
	}
}

// TestSchemaRowsFromStoredAlwaysOffersOneRow garantit qu'un tableau vide n'existe
// jamais a l'edition : sans ligne, l'utilisateur n'a aucun moyen d'ajouter un
// premier champ.
func TestSchemaRowsFromStoredAlwaysOffersOneRow(t *testing.T) {
	for _, raw := range []string{"", "pas du json", `{"type":"object"}`} {
		rows := schemaRowsFromStored(raw)
		if len(rows) != 1 {
			t.Errorf("%q : %d lignes, attendu 1", raw, len(rows))
		}
	}
}

// TestParseSchemaRowsAcceptsArbitraryRowCount verifie qu'aucun nombre de lignes
// n'est impose par le serveur : c'est la promesse faite a l'utilisateur quand il
// peut ajouter et retirer des lignes.
func TestParseSchemaRowsAcceptsArbitraryRowCount(t *testing.T) {
	for _, count := range []int{1, 2, 5, 12} {
		rows := make([][]string, 0, count)
		for i := 0; i < count; i++ {
			rows = append(rows, []string{"champ" + string(rune('a'+i%26)), "string", "", "", "", "", "", ""})
		}
		r := formRequest(t, rows)

		schema := decode(t, parseSchemaRows(r))
		properties, _ := schema["properties"].(map[string]any)
		if len(properties) != count {
			t.Fatalf("%d lignes envoyees, %d proprietes produites", count, len(properties))
		}
	}
}

// TestParseSchemaRowsCarriesEveryConstraintColumn verrouille que chaque colonne
// du tableau atteint le schema : une colonne ajoutee cote UI mais oubliee dans
// la lecture donnerait un schema silencieusement plus pauvre.
func TestParseSchemaRowsCarriesEveryConstraintColumn(t *testing.T) {
	r := formRequest(t, [][]string{
		{"ville", "string", "Ville", "Paris, Lyon", "2", "40", "^[A-Z]", ""},
		{"population", "integer", "Habitants", "", "0", "20000000", "", ""},
		{"actif", "boolean", "", "", "", "", "", ""},
		{"tags", "array", "Mots-cles", "", "", "", "", "string"},
	})

	schema := decode(t, parseSchemaRows(r))
	properties, _ := schema["properties"].(map[string]any)

	ville, _ := properties["ville"].(map[string]any)
	if ville["minLength"] != float64(2) || ville["maxLength"] != float64(40) {
		t.Errorf("bornes de chaine absentes : %v", ville)
	}
	if ville["pattern"] != "^[A-Z]" {
		t.Errorf("motif absent : %v", ville)
	}
	values, _ := ville["enum"].([]any)
	if len(values) != 2 || values[0] != "Paris" || values[1] != "Lyon" {
		t.Errorf("enum incorrect : %v", ville["enum"])
	}

	population, _ := properties["population"].(map[string]any)
	if population["minimum"] != float64(0) || population["maximum"] != float64(20000000) {
		t.Errorf("bornes numeriques absentes : %v", population)
	}

	tags, _ := properties["tags"].(map[string]any)
	items, _ := tags["items"].(map[string]any)
	if items["type"] != "string" {
		t.Errorf("type d'element absent : %v", tags)
	}

	// Aucun champ n'est obligatoire : la cle "required" doit etre absente, un
	// tableau vide etant invalide en JSON Schema.
	if _, present := schema["required"]; present {
		t.Errorf("aucun champ obligatoire, required ne doit pas etre pose : %v", schema["required"])
	}
}

// TestParseSchemaRowsPairsRequiredByIndex verifie que la case obligatoire est
// appariee a sa propre ligne. Une ligne facultative placee avant une obligatoire
// fait echouer l'appariement si l'on se contente de lire l'ordre du tableau.
func TestParseSchemaRowsPairsRequiredByIndex(t *testing.T) {
	rows := [][]string{
		{"premier", "string", "", "", "", "", "", ""},
		{"second", "string", "", "", "", "", "", ""},
		{"troisieme", "string", "", "", "", "", "", ""},
	}
	form := url.Values{}
	for c, column := range []string{
		"schema_field_name", "schema_field_type", "schema_field_description",
		"schema_field_enum", "schema_field_min", "schema_field_max",
		"schema_field_pattern", "schema_field_items",
	} {
		for _, row := range rows {
			value := ""
			if c < len(row) {
				value = row[c]
			}
			form.Add(column+"[]", value)
		}
	}
	// Seul le troisieme champ est obligatoire.
	form.Add("schema_field_required[]", "2")

	r := httptest.NewRequest("POST", "/projects/p/configs", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}

	schema := decode(t, parseSchemaRows(r))
	required, _ := schema["required"].([]any)
	if len(required) != 1 || required[0] != "troisieme" {
		t.Fatalf("required = %v, attendu [troisieme]", schema["required"])
	}
}

// TestParseSchemaRowsPrefersTextarea verifie que la saisie directe reste
// prioritaire : elle sert aux schemas que le constructeur n'exprime pas.
func TestParseSchemaRowsPrefersTextarea(t *testing.T) {
	form := url.Values{}
	form.Set("response_schema", `{"type":"object","properties":{"a":{"type":"string"}}}`)
	form.Add("schema_field_name[]", "ignore")
	form.Add("schema_field_type[]", "integer")

	r := httptest.NewRequest("POST", "/projects/p/configs", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}

	// La textarea l'emporte : le champ construit par le tableau ne doit pas
	// apparaitre, sinon l'utilisateur verrait passer une saisie qu'il a ecrasee.
	got := parseSchemaRows(r)
	if !strings.Contains(got, `"a"`) {
		t.Fatalf("la textarea doit l'emporter, obtenu : %s", got)
	}
	if strings.Contains(got, "ignore") {
		t.Fatalf("la textarea doit primer sur le constructeur, obtenu : %s", got)
	}
}

// TestParseSchemaRowsIgnoresInvalidInputWithoutPanicking couvre les entrees que
// l'interface peut produire : une ligne encore vide, ou une saisie absurde. Le
// formulaire ne doit jamais faire echouer la sauvegarde entiere.
func TestParseSchemaRowsIgnoresInvalidInputWithoutPanicking(t *testing.T) {
	cases := map[string][][]string{
		"ligne vide":              {{"ville", "string", "", "", "", "", "", ""}, {"", "string", "", "", "", "", "", ""}},
		"type inconnu":            {{"ville", "ville", "", "", "", "", "", ""}},
		"borne non numerique":     {{"ville", "string", "", "", "abc", "", "", ""}},
		"type d element invalide": {{"tags", "array", "", "", "", "", "", "tuple"}},
	}

	for name, rows := range cases {
		t.Run(name, func(t *testing.T) {
			raw := parseSchemaRows(formRequest(t, rows))
			if raw == "" {
				return // aucun schema retemu : la validation de la configuration le signalera
			}
			decode(t, raw) // doit rester du JSON Schema lisible
		})
	}
}
