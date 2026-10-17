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
	if provider, ok := modelProviderAliases[v]; ok {
		return provider, nil
	}
	// Any other custom provider (private gateway, vLLM, custom proxy) falls
	// back to the standard OpenAI-compatible protocol with its custom BaseURL.
	return schemas.OpenAI, nil
}

// modelProviderAliases maps configuration/UI provider names to the Bifrost SDK
// enum. OpenAI-compatible aliases are explicit so they stay documented.
var modelProviderAliases = map[string]schemas.ModelProvider{
	"ollama":            schemas.Ollama,
	"openai":            schemas.OpenAI,
	"openai-compatible": schemas.OpenAI,
	"custom":            schemas.OpenAI,
	"generic":           schemas.OpenAI,
	"proxy":             schemas.OpenAI,
	"azure":             schemas.Azure,
	"vertex":            schemas.Vertex,
	"vertexai":          schemas.Vertex,
	"google":            schemas.Gemini,
	"gemini":            schemas.Gemini,
	"mistral":           schemas.Mistral,
	"mistralai":         schemas.Mistral,
	"anthropic":         schemas.Anthropic,
	"claude":            schemas.Anthropic,
	"groq":              schemas.Groq,
	"cohere":            schemas.Cohere,
	"bedrock":           schemas.Bedrock,
	"aws":               schemas.Bedrock,
	"openrouter":        schemas.OpenRouter,
	"perplexity":        schemas.Perplexity,
	"cerebras":          schemas.Cerebras,
	"xai":               schemas.XAI,
	"grok":              schemas.XAI,
	"replicate":         schemas.Replicate,
	"nebius":            schemas.Nebius,
	"elevenlabs":        schemas.Elevenlabs,
	"huggingface":       schemas.HuggingFace,
	"sgl":               schemas.SGL,
	"parasail":          schemas.Parasail,
	"vllm":              schemas.VLLM,
	"runway":            schemas.Runway,
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
