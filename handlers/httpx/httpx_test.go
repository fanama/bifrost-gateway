package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"bridge-gateway/domain"
	"bridge-gateway/domain/errs"
)

func TestMapErrorCodeTable(t *testing.T) {
	cases := []struct {
		code       errs.Code
		wantStatus int
		wantType   string
	}{
		{errs.CodeValidation, http.StatusBadRequest, ErrTypeInvalidRequest},
		{errs.CodeInvalidRequest, http.StatusBadRequest, ErrTypeInvalidRequest},
		{errs.CodeUnauthorized, http.StatusUnauthorized, ErrTypeAuthentication},
		{errs.CodeForbidden, http.StatusForbidden, ErrTypeAuthentication},
		{errs.CodeNotFound, http.StatusNotFound, ErrTypeNotFound},
		{errs.CodeConflict, http.StatusConflict, ErrTypeInvalidRequest},
		{errs.CodeRateLimited, http.StatusTooManyRequests, ErrTypeRateLimit},
		{errs.CodeProviderUnavailable, http.StatusBadGateway, ErrTypeServer},
		{errs.CodeTimeout, http.StatusGatewayTimeout, ErrTypeServer},
		{errs.CodeCanceled, 499, ErrTypeInvalidRequest},
		{errs.CodeInternal, http.StatusInternalServerError, ErrTypeInvalidRequest},
	}

	for _, tc := range cases {
		status, errType, _ := MapErrorCode(tc.code)
		if status != tc.wantStatus || errType != tc.wantType {
			t.Errorf("MapErrorCode(%q) = (%d, %q), want (%d, %q)", tc.code, status, errType, tc.wantStatus, tc.wantType)
		}
	}
}

func TestWriteDomainErrorShapesOpenAIEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteDomainError(rec, &domain.InvalidRequestError{Param: "response_format", Reason: "invalid schema"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var payload map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	errObj := payload["error"]
	if errObj["param"] != "response_format" || errObj["type"] != ErrTypeInvalidRequest {
		t.Fatalf("unexpected error envelope: %+v", errObj)
	}
}
