package infrastructure

import (
	"errors"
	"strings"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

func TestFormatBifrostErrorExtractsProviderMessage(t *testing.T) {
	status := 400
	raw := map[string]any{
		"message": "Invalid request: reasoning_effort is not supported",
		"type":    "invalid_request_error",
	}

	err := formatBifrostError(&schemas.BifrostError{
		StatusCode:  &status,
		Error:       &schemas.ErrorField{},
		ExtraFields: schemas.BifrostErrorExtraFields{RawResponse: raw},
	})

	got := err.Error()
	if !strings.Contains(got, "reasoning_effort is not supported") {
		t.Errorf("provider message missing from %q", got)
	}
	if strings.Contains(got, "unknown error") {
		t.Errorf("generic message must be replaced, got %q", got)
	}
	if !strings.Contains(got, "HTTP 400") {
		t.Errorf("HTTP status missing from %q", got)
	}
	if strings.Contains(got, `"type"`) {
		t.Errorf("raw body must not duplicate the extracted message, got %q", got)
	}
}

func TestFormatBifrostErrorReadsNestedErrorBody(t *testing.T) {
	status := 401
	err := formatBifrostError(&schemas.BifrostError{
		StatusCode: &status,
		Error:      &schemas.ErrorField{Message: "unknown error"},
		ExtraFields: schemas.BifrostErrorExtraFields{
			RawResponse: `{"error":{"message":"invalid api key","type":"authentication_error"}}`,
		},
	})

	got := err.Error()
	if !strings.Contains(got, "invalid api key") {
		t.Errorf("nested provider message missing from %q", got)
	}
	if !strings.Contains(got, "HTTP 401") {
		t.Errorf("HTTP status missing from %q", got)
	}
}

func TestFormatBifrostErrorAddsHintWhenBodyIsEmpty(t *testing.T) {
	status := 400
	err := formatBifrostError(&schemas.BifrostError{
		StatusCode: &status,
		Error:      &schemas.ErrorField{Message: "unknown error"},
	})

	got := err.Error()
	for _, want := range []string{"provider rejected the request", "HTTP 400", "response_format", "reasoning_effort"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from %q", want, got)
		}
	}
}

func TestProviderErrorAlwaysReportsStatus(t *testing.T) {
	err := error(&ProviderError{
		StatusCode: 400,
		Message:    "bad request",
		Detail:     "unsupported parameter",
	})
	if got, want := err.Error(), "bifrost error: bad request: unsupported parameter (HTTP 400)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	var pe *ProviderError
	if !errors.As(err, &pe) || pe.StatusCode != 400 {
		t.Error("ProviderError must stay discoverable through errors.As")
	}
}
