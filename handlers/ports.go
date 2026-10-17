package handlers

import "bridge-gateway/handlers/httpx"

// Consumer-side delivery ports re-exported from handlers/httpx.
type (
	Authenticator      = httpx.Authenticator
	Enricher           = httpx.Enricher
	ChatCompletion     = httpx.ChatCompletion
	ActiveConfigLoader = httpx.ActiveConfigLoader
	ConfigQuerier      = httpx.ConfigQuerier
	ModelLister        = httpx.ModelLister
	Embedder           = httpx.Embedder
)
