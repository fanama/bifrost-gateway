package domain

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

const APIKeyPrefix = "sk-bridge-"

type APIKey struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	Digest    string    `json:"digest"`
	CreatedAt time.Time `json:"created_at"`
}

func (k *APIKey) Validate() error {
	var missing []string
	if strings.TrimSpace(k.Name) == "" {
		missing = append(missing, "name")
	}
	if strings.TrimSpace(k.ProjectID) == "" {
		missing = append(missing, "project_id")
	}
	if len(missing) > 0 {
		return &ValidationError{Fields: missing}
	}
	return nil
}

func (k *APIKey) Matches(secret string) bool {
	return k.Digest != "" && HashAPIKey(secret) == k.Digest
}

func HashAPIKey(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// NewAPIKey genere un secret (rendu une seule fois), son prefixe d'affichage
// et son empreinte SHA-256. Seule l'empreinte doit etre persistee.
func NewAPIKey() (secret, prefix, digest string) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failure: " + err.Error())
	}
	secret = APIKeyPrefix + hex.EncodeToString(b)
	prefix = secret[:len(APIKeyPrefix)+8]
	digest = HashAPIKey(secret)
	return secret, prefix, digest
}

type APIKeyRepository interface {
	CrudRepository[APIKey]
	ListByProject(ctx context.Context, projectID string) ([]APIKey, error)
	FindByDigest(ctx context.Context, digest string) (*APIKey, error)
}
