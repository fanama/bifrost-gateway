package infrastructure

import (
	"context"
	"net/url"
	"os"
	"strings"

	"bridge-gateway/domain"
	"github.com/maximhq/bifrost/core/schemas"
)

// resolveProvider retrouve un provider enregistre par son nom ou son identifiant.
// Il est partage entre le provider Bifrost et le routeur, qui doit employer les
// memes credentials pour un tier qu'il substitue a la configuration active.
func resolveProvider(ctx context.Context, repo domain.ProviderRepository, providerName string) *domain.Provider {
	if repo == nil {
		return nil
	}
	list, err := repo.List(ctx)
	if err != nil {
		return nil
	}
	for _, prov := range list {
		if strings.EqualFold(prov.Name, providerName) || strings.EqualFold(prov.ID, providerName) {
			found := prov
			return &found
		}
	}
	return nil
}

// ResolveEnv developpe une valeur configuree sous la forme d'un placeholder
// d'environnement ({env:VAR}, env:VAR, ${VAR}, $VAR) ; toute autre valeur est
// rendue telle quelle.
func ResolveEnv(val string) string {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "{env:") && strings.HasSuffix(trimmed, "}") {
		envName := strings.TrimSuffix(strings.TrimPrefix(trimmed, "{env:"), "}")
		return os.Getenv(strings.TrimSpace(envName))
	}
	if strings.HasPrefix(trimmed, "env:") {
		envName := strings.TrimPrefix(trimmed, "env:")
		return os.Getenv(strings.TrimSpace(envName))
	}
	if strings.HasPrefix(trimmed, "${") && strings.HasSuffix(trimmed, "}") {
		envName := strings.TrimSuffix(strings.TrimPrefix(trimmed, "${"), "}")
		return os.Getenv(strings.TrimSpace(envName))
	}
	if strings.HasPrefix(trimmed, "$") && !strings.Contains(trimmed, "/") && !strings.Contains(trimmed, " ") {
		return os.Getenv(strings.TrimPrefix(trimmed, "$"))
	}
	return trimmed
}

// ToModelProvider traduit le nom d'un provider (celui de config.yaml ou de
// l'UI) vers l'enumeration du SDK Bifrost. Tout nom inconnu retombe sur le
// protocole OpenAI-compatible, ce qui couvre les passerelles privees et vLLM.
func ToModelProvider(s string) (schemas.ModelProvider, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" {
		return schemas.OpenAI, nil
	}
	switch v {
	case "ollama":
		return schemas.Ollama, nil
	case "openai", "openai-compatible", "custom", "generic", "proxy":
		return schemas.OpenAI, nil
	case "azure":
		return schemas.Azure, nil
	case "vertex", "vertexai":
		return schemas.Vertex, nil
	case "google", "gemini":
		return schemas.Gemini, nil
	case "mistral", "mistralai":
		return schemas.Mistral, nil
	case "anthropic", "claude":
		return schemas.Anthropic, nil
	case "groq":
		return schemas.Groq, nil
	case "cohere":
		return schemas.Cohere, nil
	case "bedrock", "aws":
		return schemas.Bedrock, nil
	case "openrouter":
		return schemas.OpenRouter, nil
	case "perplexity":
		return schemas.Perplexity, nil
	case "cerebras":
		return schemas.Cerebras, nil
	case "xai", "grok":
		return schemas.XAI, nil
	case "replicate":
		return schemas.Replicate, nil
	case "nebius":
		return schemas.Nebius, nil
	case "elevenlabs":
		return schemas.Elevenlabs, nil
	case "huggingface":
		return schemas.HuggingFace, nil
	case "sgl":
		return schemas.SGL, nil
	case "parasail":
		return schemas.Parasail, nil
	case "vllm":
		return schemas.VLLM, nil
	case "runway":
		return schemas.Runway, nil
	default:
		// Any other custom provider (e.g. private gateway, vLLM, custom proxy)
		// falls back to the standard OpenAI-compatible protocol with its custom BaseURL.
		return schemas.OpenAI, nil
	}
}

func defaultBaseURLFor(p schemas.ModelProvider) string {
	switch p {
	case schemas.Ollama:
		return "http://localhost:11434"
	case schemas.OpenAI:
		return "https://api.openai.com"
	case schemas.Mistral:
		return "https://api.mistral.ai"
	case schemas.Anthropic:
		return "https://api.anthropic.com"
	case schemas.Groq:
		return "https://api.groq.com/openai"
	case schemas.OpenRouter:
		return "https://openrouter.ai/api"
	case schemas.Gemini:
		return "https://generativelanguage.googleapis.com/v1beta"
	case schemas.Perplexity:
		return "https://api.perplexity.ai"
	case schemas.XAI:
		return "https://api.x.ai"
	default:
		return ""
	}
}

// normalizeBaseURL aligns a configured base URL with the path Bifrost appends
// to it. The OpenAI-compatible, Anthropic, Mistral and Ollama providers all
// concatenate BaseURL with a path that already carries the API version
// ("/v1/chat/completions", "/v1/messages"), so a base URL ending in "/v1" would
// produce "/v1/v1/chat/completions" and the provider would answer with an HTML
// error page instead of JSON. Version segments Bifrost does not append itself
// are preserved, because it appends a version-less path to them (Gemini's
// "/v1beta" + "/models").
func normalizeBaseURL(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return ""
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		// Not an absolute URL (e.g. an unresolved env placeholder): leave as is.
		return trimmed
	}

	path := parsed.Path
	for _, suffix := range []string{"/chat/completions", "/v1"} {
		if trimmedPath, ok := strings.CutSuffix(path, suffix); ok {
			path = trimmedPath
		}
	}
	parsed.Path = strings.TrimRight(path, "/")
	parsed.RawPath = ""

	return strings.TrimRight(parsed.String(), "/")
}
