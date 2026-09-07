package domain

import (
	"crypto/rand"
	"encoding/hex"
)

func NewID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "cfg-0000000000000000"
	}
	return "cfg-" + hex.EncodeToString(b)
}
