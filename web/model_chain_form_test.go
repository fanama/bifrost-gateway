package web

import (
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"bridge-gateway/domain"
)

func TestParseConfigFormReadsFallbackRows(t *testing.T) {
	form := url.Values{}
	form.Set("name", "cfg")
	form.Set("provider", "mistral")
	form.Set("model", "m-large")
	form.Add("fallback_provider[]", "mistral")
	form.Add("fallback_model[]", "m-small")
	form.Add("fallback_provider[]", "openai")
	form.Add("fallback_model[]", "gpt-4o-mini")
	form.Add("fallback_provider[]", "inconnu")
	form.Add("fallback_model[]", "m-hors-registre")
	form.Add("fallback_provider[]", "mistral")
	form.Add("fallback_model[]", "")

	r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	cfg := parseConfigForm(r, []string{"mistral", "openai"})
	want := []domain.ModelChoice{
		{Provider: "mistral", Model: "m-small"},
		{Provider: "openai", Model: "gpt-4o-mini"},
		{Provider: "", Model: "m-hors-registre"},
	}
	if !reflect.DeepEqual(cfg.FallbackModels, want) {
		t.Errorf("fallbacks = %#v, want %#v", cfg.FallbackModels, want)
	}
	if cfg.Model != "m-large" {
		t.Errorf("primary model = %q", cfg.Model)
	}
}

func TestParseConfigFormWithoutFallbacks(t *testing.T) {
	form := url.Values{}
	form.Set("name", "cfg")
	form.Set("provider", "mistral")
	form.Set("model", "m-large")

	r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if cfg := parseConfigForm(r, []string{"mistral"}); cfg.FallbackModels != nil {
		t.Errorf("fallbacks = %#v, want nil", cfg.FallbackModels)
	}
}
