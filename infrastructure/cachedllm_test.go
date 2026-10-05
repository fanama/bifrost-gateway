package infrastructure

import (
	"testing"

	"bridge-gateway/domain"
)

// TestCacheKeyDistinguishesResponseSchemas verrouille une fuite de donnees : la
// cle de cache ne comptait que le format, pas le schema. Deux configurations
// json_schema de schemas differents produisaient donc la meme cle, et la reponse
// conforme au schema A etait servie a une requete qui attendait le schema B.
func TestCacheKeyDistinguishesResponseSchemas(t *testing.T) {
	p := &CachedLLMProvider{}
	messages := []domain.ChatMessage{{Role: "user", Content: "Bonjour"}}

	base := func(schema string) *domain.ChatConfig {
		return &domain.ChatConfig{
			Provider:       "openrouter",
			Model:          "ministral-14b-latest",
			ResponseFormat: "json_schema",
			ResponseSchema: schema,
		}
	}

	a := `{"type":"object","properties":{"nom":{"type":"string"}}}`
	b := `{"type":"object","properties":{"age":{"type":"integer"}}}`

	keyA, err := p.cacheKey(base(a), messages)
	if err != nil {
		t.Fatalf("cacheKey: %v", err)
	}
	keyB, err := p.cacheKey(base(b), messages)
	if err != nil {
		t.Fatalf("cacheKey: %v", err)
	}
	if keyA == keyB {
		t.Fatal("deux schemas differents partagent la meme cle de cache : une reponse serait servie pour un schema qu'elle ne respecte pas")
	}

	// La stabilite reste indispensable : deux appels identiques doivent viser la
	// meme entree, sans quoi le cache ne sert plus a rien.
	keyA2, err := p.cacheKey(base(a), messages)
	if err != nil {
		t.Fatalf("cacheKey: %v", err)
	}
	if keyA != keyA2 {
		t.Fatal("la cle de cache doit etre stable pour une meme configuration")
	}
}

// TestCacheKeyDistinguishesImplicitSchemaFormat couvre le cas du select laisse
// sur "aucun" : la cle doit dependre du schema stocke, que le format soit
// declare ou deduit.
func TestCacheKeyDistinguishesImplicitSchemaFormat(t *testing.T) {
	p := &CachedLLMProvider{}
	messages := []domain.ChatMessage{{Role: "user", Content: "Bonjour"}}

	schema := `{"type":"object","properties":{"nom":{"type":"string"}}}`
	withFormat := &domain.ChatConfig{Provider: "openrouter", Model: "m", ResponseFormat: "json_schema", ResponseSchema: schema}
	withoutFormat := &domain.ChatConfig{Provider: "openrouter", Model: "m", ResponseFormat: "", ResponseSchema: schema}

	k1, err := p.cacheKey(withFormat, messages)
	if err != nil {
		t.Fatalf("cacheKey: %v", err)
	}
	k2, err := p.cacheKey(withoutFormat, messages)
	if err != nil {
		t.Fatalf("cacheKey: %v", err)
	}
	if k1 != k2 {
		t.Fatal("un format declare et un format deduit doivent designer la meme sortie : le cache les separerait pour rien")
	}
}
