package errs

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestCodeOfAndUnwrap(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		wantCode  Code
		wantParam string
	}{
		{"nil", nil, "", ""},
		{"sentinel not found", fmt.Errorf("lookup: %w", ErrConfigNotFound), CodeNotFound, ""},
		{"no active config", ErrNoActiveConfig, CodeInvalidRequest, ""},
		{"invalid api key", ErrInvalidAPIKey, CodeUnauthorized, ""},
		{"validation", &ValidationError{Fields: []string{"name"}}, CodeValidation, "name"},
		{"invalid request", &InvalidRequestError{Param: "response_format", Reason: "bad schema"}, CodeInvalidRequest, "response_format"},
		{"missing attribution", &MissingAttributionError{MissingFields: []string{"team"}}, CodeInvalidRequest, ""},
		{"wrapped with param", WithParam(CodeInvalidRequest, "messages", "messages is required"), CodeInvalidRequest, "messages"},
		{"wrapped cause", Wrap(CodeProviderUnavailable, "upstream down", errors.New("eof")), CodeProviderUnavailable, ""},
		{"canceled", context.Canceled, CodeCanceled, ""},
		{"timeout", context.DeadlineExceeded, CodeTimeout, ""},
		{"unknown error", errors.New("boom"), CodeInternal, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CodeOf(tc.err); got != tc.wantCode {
				t.Fatalf("CodeOf() = %q, want %q", got, tc.wantCode)
			}
			if got := ParamOf(tc.err); got != tc.wantParam {
				t.Fatalf("ParamOf() = %q, want %q", got, tc.wantParam)
			}
		})
	}
}

func TestSentinelIdentityPreservedWhenWrapped(t *testing.T) {
	wrapped := Wrap(CodeNotFound, "missing config", ErrConfigNotFound)
	if !errors.Is(wrapped, ErrConfigNotFound) {
		t.Fatal("expected errors.Is to match ErrConfigNotFound through Wrap")
	}
}
