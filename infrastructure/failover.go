package infrastructure

import "bridge-gateway/domain/routing"

// FailoverReason and constants are re-exported from domain/routing.
type FailoverReason = routing.FailoverReason

const (
	FailoverNone          = routing.FailoverNone
	FailoverRateLimit     = routing.FailoverRateLimit
	FailoverContextLength = routing.FailoverContextLength
	FailoverUnavailable   = routing.FailoverUnavailable
	FailoverAuth          = routing.FailoverAuth
	FailoverModel         = routing.FailoverModel
	FailoverClient        = routing.FailoverClient
)

func failoverReason(err error) FailoverReason {
	return routing.ClassifyFailure(err)
}

func isModelUnavailableError(err *ProviderError) bool {
	return routing.IsModelUnavailableError(err)
}

func isContextLengthError(err *ProviderError) bool {
	return routing.IsContextLengthError(err)
}
