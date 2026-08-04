package agentbridge

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

type BundleData struct {
	SchemaVersion string `json:"schema_version"`
	AgentName     string `json:"agent_name"`
	Token         string `json:"token"`
	RelayURL      string `json:"relay_url"`
	Fingerprint   string `json:"fingerprint"`
	Issued        string `json:"issued"`
	Expiry        string `json:"expiry"`
	BootstrapID   string `json:"bootstrap_id"`
}

type EncryptedBundle struct {
	SchemaVersion   string `json:"schema_version"`
	BootstrapID     string `json:"bootstrap_id"`
	EncryptedAESKey string `json:"encrypted_aes_key"`
	Nonce           string `json:"nonce"`
	Ciphertext      string `json:"ciphertext"`
}

type ReplayStore interface {
	HasConsumed(id string) (bool, error)
	MarkConsumed(id string) error
}

func getAAD(schemaVersion, bootstrapID string) []byte {
	return []byte(schemaVersion + "|" + bootstrapID)
}

func GenerateBootstrapBundle(pub *rsa.PublicKey, bd BundleData) (*EncryptedBundle, error) {
	pt, err := json.Marshal(bd)
	if err != nil {
		return nil, err
	}

	aesKey := make([]byte, 32)
	if _, err := rand.Read(aesKey); err != nil {
		return nil, err
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	blockAES, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(blockAES)
	if err != nil {
		return nil, err
	}

	aad := getAAD(bd.SchemaVersion, bd.BootstrapID)
	ct := gcm.Seal(nil, nonce, pt, aad)

	hash := sha256.New()
	encKey, err := rsa.EncryptOAEP(hash, rand.Reader, pub, aesKey, nil)
	if err != nil {
		return nil, err
	}

	return &EncryptedBundle{
		SchemaVersion:   bd.SchemaVersion,
		BootstrapID:     bd.BootstrapID,
		EncryptedAESKey: base64.StdEncoding.EncodeToString(encKey),
		Nonce:           base64.StdEncoding.EncodeToString(nonce),
		Ciphertext:      base64.StdEncoding.EncodeToString(ct),
	}, nil
}

func DecryptBootstrapBundle(eb *EncryptedBundle, priv *rsa.PrivateKey) (*BundleData, error) {
	if eb == nil {
		return nil, errors.New("nil bundle")
	}
	encKey, err := base64.StdEncoding.DecodeString(eb.EncryptedAESKey)
	if err != nil {
		return nil, err
	}
	nonce, err := base64.StdEncoding.DecodeString(eb.Nonce)
	if err != nil {
		return nil, err
	}
	ct, err := base64.StdEncoding.DecodeString(eb.Ciphertext)
	if err != nil {
		return nil, err
	}

	hash := sha256.New()
	aesKey, err := rsa.DecryptOAEP(hash, rand.Reader, priv, encKey, nil)
	if err != nil {
		return nil, err
	}

	blockAES, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(blockAES)
	if err != nil {
		return nil, err
	}

	aad := getAAD(eb.SchemaVersion, eb.BootstrapID)
	pt, err := gcm.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, err
	}

	dec := json.NewDecoder(bytes.NewReader(pt))
	dec.DisallowUnknownFields()

	var bd BundleData
	if err := dec.Decode(&bd); err != nil {
		return nil, err
	}
	var dummy interface{}
	if err := dec.Decode(&dummy); err != io.EOF {
		return nil, errors.New("trailing JSON data")
	}

	return &bd, nil
}

func ValidateBootstrapBundle(eb *EncryptedBundle, priv *rsa.PrivateKey, expectedAgent string, rs ReplayStore) (*BundleData, error) {
	if eb.SchemaVersion != "1" {
		return nil, errors.New("invalid outer schema_version")
	}

	consumed, err := rs.HasConsumed(eb.BootstrapID)
	if err != nil {
		return nil, fmt.Errorf("failed to check replay store: %v", err)
	}
	if consumed {
		return nil, errors.New("bootstrap bundle has already been consumed")
	}

	bd, err := DecryptBootstrapBundle(eb, priv)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %v", err)
	}

	if bd.SchemaVersion != "1" {
		return nil, errors.New("invalid inner schema_version")
	}
	if bd.BootstrapID != eb.BootstrapID {
		return nil, errors.New("mismatched bootstrap_id")
	}
	if bd.AgentName != expectedAgent {
		return nil, errors.New("unexpected agent_name")
	}

	if len(bd.Fingerprint) != 64 {
		return nil, errors.New("fingerprint must be exactly 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(bd.Fingerprint); err != nil {
		return nil, errors.New("fingerprint is not valid hex")
	}

	if bd.Token == "" || bd.RelayURL == "" || bd.Issued == "" || bd.Expiry == "" || bd.BootstrapID == "" || bd.AgentName == "" || bd.SchemaVersion == "" {
		return nil, errors.New("missing required fields")
	}

	issued, err := time.Parse(time.RFC3339, bd.Issued)
	if err != nil {
		return nil, errors.New("invalid issued timestamp format")
	}
	expiry, err := time.Parse(time.RFC3339, bd.Expiry)
	if err != nil {
		return nil, errors.New("invalid expiry timestamp format")
	}

	now := time.Now()
	if issued.After(now.Add(5 * time.Minute)) {
		return nil, errors.New("issued timestamp is in the future")
	}
	if !expiry.After(issued) {
		return nil, errors.New("expiry must be after issued time")
	}
	if now.After(expiry) {
		return nil, errors.New("bootstrap bundle has expired")
	}

	if err := rs.MarkConsumed(bd.BootstrapID); err != nil {
		return nil, fmt.Errorf("failed to mark bundle as consumed: %v", err)
	}

	return bd, nil
}
