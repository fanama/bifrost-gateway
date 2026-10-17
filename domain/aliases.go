package domain

import (
	"context"

	"bridge-gateway/domain/chat"
	"bridge-gateway/domain/config"
	"bridge-gateway/domain/embedding"
	"bridge-gateway/domain/llm"
	"bridge-gateway/domain/monitoring"
	"bridge-gateway/domain/project"
	"bridge-gateway/domain/provider"
	"bridge-gateway/domain/shared"
)

// Chat types.
type (
	ChatMessage         = chat.ChatMessage
	ChatRequest         = chat.ChatRequest
	StreamOptions       = chat.StreamOptions
	EnrichedRequest     = chat.EnrichedRequest
	RequestOptions      = chat.RequestOptions
	AttributionMetadata = chat.AttributionMetadata
	TeamMetadata        = chat.TeamMetadata
	RequestMetadata     = chat.RequestMetadata
	EnrichmentService   = chat.EnrichmentService
)

// LLM ports.
type (
	LLMResult            = llm.LLMResult
	LLMProvider          = llm.LLMProvider
	StreamEvent          = llm.StreamEvent
	StreamingLLMProvider = llm.StreamingLLMProvider
	Classifier           = llm.Classifier
	ProviderError        = llm.ProviderError
)

// Configuration, schema, and tiers.
type (
	ChatConfig           = config.ChatConfig
	ChatConfigRepository = config.ChatConfigRepository
	SchemaField          = config.SchemaField
)

const (
	ResponseFormatText       = config.ResponseFormatText
	ResponseFormatJSONObject = config.ResponseFormatJSONObject
	ResponseFormatJSONSchema = config.ResponseFormatJSONSchema
	TierFast                 = config.TierFast
	TierBalanced             = config.TierBalanced
	TierFrontier             = config.TierFrontier
	RoutingTiers             = config.RoutingTiers
)

// Project and API keys.
type (
	Project           = project.Project
	ProjectRepository = project.ProjectRepository
	APIKey            = project.APIKey
	APIKeyRepository  = project.APIKeyRepository
)

const APIKeyPrefix = project.APIKeyPrefix

// Providers and models.
type (
	Provider               = provider.Provider
	ProviderRepository     = provider.ProviderRepository
	ModelInfo              = provider.ModelInfo
	CatalogModel           = provider.CatalogModel
	ModelCatalogRepository = provider.ModelCatalogRepository
)

const (
	ModelKindChat      = provider.ModelKindChat
	ModelKindEmbedding = provider.ModelKindEmbedding
)

// Embedding.
type (
	EmbeddingChoice   = embedding.EmbeddingChoice
	EmbeddingRequest  = embedding.EmbeddingRequest
	EmbeddingItem     = embedding.EmbeddingItem
	EmbeddingUsage    = embedding.EmbeddingUsage
	EmbeddingResponse = embedding.EmbeddingResponse
	EmbeddingProvider = embedding.EmbeddingProvider
)

const (
	ProviderLocal       = embedding.ProviderLocal
	ModelLocalEmbedding = embedding.ModelLocalEmbedding
	ProviderOnnx        = embedding.ProviderOnnx
	OnnxModelPrefix     = embedding.OnnxModelPrefix
	ModelOnnxMiniLM     = embedding.ModelOnnxMiniLM
)

var EmbeddingChoices = embedding.EmbeddingChoices

// Monitoring snapshots.
type (
	MetricsSnapshot = monitoring.MetricsSnapshot
	RequestMetrics  = monitoring.RequestMetrics
	LatencyMetrics  = monitoring.LatencyMetrics
	StatusMetric    = monitoring.StatusMetric
	RouteMetric     = monitoring.RouteMetric
	ErrorMetric     = monitoring.ErrorMetric
	TimelineBucket  = monitoring.TimelineBucket
	RuntimeMetrics  = monitoring.RuntimeMetrics
	LLMMetrics      = monitoring.LLMMetrics
	LLMModelMetric  = monitoring.LLMModelMetric
)

// Shared repository/cache primitives.
type (
	Cache           = shared.Cache
	CacheKeyFactory = shared.CacheKeyFactory
)

type CrudRepository[T any] = shared.CrudRepository[T]
type UpdatableRepository[T any] = shared.UpdatableRepository[T]

// Functions kept as forwarding wrappers so existing call sites and tests do
// not need to change during the package split.
func NewEnrichmentService() *EnrichmentService          { return chat.NewEnrichmentService() }
func ExtractMetadata(req *ChatRequest) *RequestMetadata { return chat.ExtractMetadata(req) }
func OptionsFrom(req *ChatRequest) *RequestOptions      { return chat.OptionsFrom(req) }
func StreamViaFallback(r LLMResult, err error) (<-chan StreamEvent, error) {
	return llm.StreamViaFallback(r, err)
}
func StreamFrom(ctx context.Context, p LLMProvider, cfg *ChatConfig, m []ChatMessage) (<-chan StreamEvent, error) {
	return llm.StreamFrom(ctx, p, cfg, m)
}
func TierRankOf(tier string) int                         { return config.TierRankOf(tier) }
func TierOptions() []string                              { return config.TierOptions() }
func ParseJSONSchema(raw string) (map[string]any, error) { return config.ParseJSONSchema(raw) }
func ParseResponseFormat(m map[string]any) (string, string, error) {
	return config.ParseResponseFormat(m)
}
func SchemaFieldTypes() []string                           { return config.SchemaFieldTypes() }
func BuildJSONSchema(fields []SchemaField) (string, error) { return config.BuildJSONSchema(fields) }
func SchemaAllowsStrict(schema map[string]any) bool        { return config.SchemaAllowsStrict(schema) }
func EnumValues(property map[string]any) string            { return config.EnumValues(property) }
func ConstraintString(property map[string]any, name string) string {
	return config.ConstraintString(property, name)
}
func ItemsType(property map[string]any) string { return config.ItemsType(property) }
func MinMaxString(property map[string]any, lengthKey, boundKey string) string {
	return config.MinMaxString(property, lengthKey, boundKey)
}
func HashAPIKey(secret string) string                   { return project.HashAPIKey(secret) }
func NewAPIKey() (string, string, string)               { return project.NewAPIKey() }
func NormalizeModelKind(kind string) string             { return provider.NormalizeModelKind(kind) }
func FindEmbeddingChoice(model string) *EmbeddingChoice { return embedding.FindEmbeddingChoice(model) }
func EmbeddingChoiceFromModel(m ModelInfo) (EmbeddingChoice, bool) {
	return embedding.EmbeddingChoiceFromModel(m)
}
func NewCacheKeyFactory(prefix string) CacheKeyFactory { return shared.NewCacheKeyFactory(prefix) }
func NewID() string                                    { return shared.NewID() }
