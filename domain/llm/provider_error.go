package llm

import "fmt"

// ProviderError preserves structured HTTP status and provider error code
// fields from an upstream LLM failure while keeping the historical text format
// expected by UI error toasts.
type ProviderError struct {
	StatusCode int
	Code       string
	Message    string
	Detail     string
}

func (e *ProviderError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("bifrost error: %s: %s", e.Message, e.Detail)
	}
	if e.StatusCode != 0 {
		return fmt.Sprintf("bifrost error: %s (HTTP %d)", e.Message, e.StatusCode)
	}
	return fmt.Sprintf("bifrost error: %s", e.Message)
}
