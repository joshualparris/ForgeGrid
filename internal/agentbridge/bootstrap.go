package agentbridge

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	ct := gcm.Seal(nil, nonce, pt, nil)

	hash := sha256.New()
	encKey, err := rsa.EncryptOAEP(hash, rand.Reader, pub, aesKey, nil)
	if err != nil {
		return nil, err
	}

	return &EncryptedBundle{
		SchemaVersion:   "1",
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

	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, err
	}

	dec := json.NewDecoder(bytes.NewReader(pt))
	dec.DisallowUnknownFields()

	var bd BundleData
	if err := dec.Decode(&bd); err != nil {
		return nil, err
	}
	return &bd, nil
}
