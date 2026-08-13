package agentbridge

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

func RegisterAgentWithNewToken(name string) (string, error) {
	store, err := NewStore()
	if err != nil {
		return "", err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(b)
	hash := sha256.Sum256([]byte(secret))
	if err := store.RegisterAgent(name, hex.EncodeToString(hash[:])); err != nil {
		return "", err
	}
	return secret, nil
}
