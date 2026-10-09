package docs

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestSpecIsLoadable : la spec embarquee est un YAML valide, en OpenAPI 3,
// avec les routes contractuelles et le schema d'erreur.
func TestSpecIsLoadable(t *testing.T) {
	data, err := spec.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatalf("openapi.yaml absent du binaire: %v", err)
	}
	var doc struct {
		OpenAPI    string                    `yaml:"openapi"`
		Paths      map[string]map[string]any `yaml:"paths"`
		Components struct {
			SecuritySchemes map[string]any `yaml:"securitySchemes"`
			Schemas         map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("YAML invalide: %v", err)
	}
	if doc.OpenAPI != "3.0.3" {
		t.Errorf("openapi=%q, attendu 3.0.3", doc.OpenAPI)
	}
	for _, p := range []string{
		"/v1/chat/completions", "/v1/embeddings", "/v1/models", "/v1/models/{model}",
		"/health/liveness", "/health/readiness", "/health/test_connection",
	} {
		if _, ok := doc.Paths[p]; !ok {
			t.Errorf("chemin absent: %s", p)
		}
	}
	for _, s := range []string{"ChatCompletionRequest", "EmbeddingResponse", "Error", "Model"} {
		if _, ok := doc.Components.Schemas[s]; !ok {
			t.Errorf("schema absent: %s", s)
		}
	}
	if _, ok := doc.Components.SecuritySchemes["bearerAuth"]; !ok {
		t.Error("securitySchemes bearerAuth absent")
	}
}

// TestSpecHandlerSertLeFichier : GET renvoie le YAML brut avec le bon type,
// une methode non lue est refusee.
func TestSpecHandlerSertLeFichier(t *testing.T) {
	srv := httptest.NewServer(SpecHandler())
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/yaml") {
		t.Errorf("content-type %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "openapi: 3.0.3") {
		t.Error("corps different de la spec")
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST attendu en 405, got err=%v status=%v", err, r)
	}
}

// TestUIHandlerPage : la page Swagger UI pointe bien sur /openapi.yaml local.
func TestUIHandlerPage(t *testing.T) {
	rec := httptest.NewRecorder()
	UIHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/swagger", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/openapi.yaml") || !strings.Contains(body, "swagger-ui-bundle") {
		t.Error("page sans lien vers la spec ou sans Swagger UI")
	}
	// Try it out doit rappeler l'origine qui sert la page : la spec declare
	// :4000 et :8080, dont une seule repond selon le mode de demarrage.
	if !strings.Contains(body, "requestInterceptor") || !strings.Contains(body, "window.location.origin") {
		t.Error("page sans requestInterceptor d'origine pour Try it out")
	}
}

// TestRegister : les deux routes sont montees sur le mux de l'application.
func TestRegister(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux)
	for _, path := range []string{"/swagger", "/openapi.yaml"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s -> %d", path, rec.Code)
		}
	}
}
