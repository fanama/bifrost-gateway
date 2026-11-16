package llm

import (
	"fmt"
	"strings"
)

// ProviderError preserves structured HTTP status and provider error code
// fields from an upstream LLM failure while keeping the historical text format
// expected by UI error toasts. Hint carries an actionable diagnostic used when
// the provider returned no readable message.
type ProviderError struct {
	StatusCode int
	Code       string
	Message    string
	Detail     string
	Hint       string
}

func (e *ProviderError) Error() string {
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = "unknown provider error"
	}

	var b strings.Builder
	b.WriteString("bifrost error: ")
	b.WriteString(msg)
	if detail := strings.TrimSpace(e.Detail); detail != "" && !strings.EqualFold(detail, msg) {
		b.WriteString(": ")
		b.WriteString(detail)
	}
	if e.StatusCode != 0 {
		fmt.Fprintf(&b, " (HTTP %d)", e.StatusCode)
	}
	if hint := strings.TrimSpace(e.Hint); hint != "" {
		b.WriteString(" — ")
		b.WriteString(hint)
	}
	return b.String()
}
