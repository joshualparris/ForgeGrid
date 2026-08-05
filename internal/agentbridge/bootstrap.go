package agentbridge

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
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
	SchemaVersion      string `json:"schema_version"`
	BootstrapID        string `json:"bootstrap_id"`
	EncryptedAESKey    string `json:"encrypted_aes_key"`
	Nonce              string `json:"nonce"`
	Ciphertext         string `json:"ciphertext"`
	SignatureAlgorithm string `json:"signature_algorithm"`
	SignerFingerprint  string `json:"signer_fingerprint"`
	Signature          string `json:"signature"`
}

type ReplayStore interface {
	Reserve(id string) error
	Commit(id string) error
	Release(id string) error
}

func getAAD(schemaVersion, bootstrapID string) []byte {
	return []byte(schemaVersion + "|" + bootstrapID)
}

func ConstructSignedPayload(eb *EncryptedBundle, agentName, recipientFingerprint string) []byte {
	var buf bytes.Buffer
	writeField := func(s string) {
		length := uint32(len(s))
		binary.Write(&buf, binary.BigEndian, length)
		buf.WriteString(s)
	}
	writeField(eb.SchemaVersion)
	writeField(eb.BootstrapID)
	writeField(agentName)
	writeField(recipientFingerprint)
	writeField(eb.EncryptedAESKey)
	writeField(eb.Nonce)
	writeField(eb.Ciphertext)
	return buf.Bytes()
}

func GenerateSignedBootstrapBundle(pub *rsa.PublicKey, signPriv ed25519.PrivateKey, bd BundleData) (*EncryptedBundle, error) {
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

	eb := &EncryptedBundle{
		SchemaVersion:   bd.SchemaVersion,
		BootstrapID:     bd.BootstrapID,
		EncryptedAESKey: base64.StdEncoding.EncodeToString(encKey),
		Nonce:           base64.StdEncoding.EncodeToString(nonce),
		Ciphertext:      base64.StdEncoding.EncodeToString(ct),
	}

	pubBytes, _ := x509.MarshalPKIXPublicKey(pub)
	pubHash := sha256.Sum256(pubBytes)
	recipientFingerprint := hex.EncodeToString(pubHash[:])

	payload := ConstructSignedPayload(eb, bd.AgentName, recipientFingerprint)
	sig := ed25519.Sign(signPriv, payload)

	signPub := signPriv.Public().(ed25519.PublicKey)
	signPubHash := sha256.Sum256(signPub)

	eb.SignatureAlgorithm = "ed25519"
	eb.SignerFingerprint = hex.EncodeToString(signPubHash[:])
	eb.Signature = base64.StdEncoding.EncodeToString(sig)

	return eb, nil
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

func ValidateBootstrapBundle(eb *EncryptedBundle, priv *rsa.PrivateKey, expectedSignPub ed25519.PublicKey, expectedAgent string, rs ReplayStore) (bd *BundleData, err error) {
	if eb == nil {
		return nil, errors.New("nil bundle")
	}
	if rs == nil {
		return nil, errors.New("nil replay store")
	}
	if eb.SchemaVersion != "1" {
		return nil, errors.New("invalid outer schema_version")
	}

	if eb.SignatureAlgorithm != "ed25519" {
		return nil, errors.New("unknown or missing signature_algorithm")
	}

	expectedSignPubHash := sha256.Sum256(expectedSignPub)
	expectedSignerFingerprint := hex.EncodeToString(expectedSignPubHash[:])
	if eb.SignerFingerprint != expectedSignerFingerprint {
		return nil, errors.New("signer fingerprint mismatch")
	}

	sigBytes, err := base64.StdEncoding.DecodeString(eb.Signature)
	if err != nil {
		return nil, errors.New("invalid signature encoding")
	}

	pubBytes, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal recipient public key: %v", err)
	}
	pubHash := sha256.Sum256(pubBytes)
	recipientFingerprint := hex.EncodeToString(pubHash[:])

	payload := ConstructSignedPayload(eb, expectedAgent, recipientFingerprint)
	if !ed25519.Verify(expectedSignPub, payload, sigBytes) {
		return nil, errors.New("invalid signature")
	}

	if err := rs.Reserve(eb.BootstrapID); err != nil {
		return nil, fmt.Errorf("failed to reserve bootstrap ID: %v", err)
	}

	success := false
	defer func() {
		if !success {
			_ = rs.Release(eb.BootstrapID)
		}
	}()

	bd, err = DecryptBootstrapBundle(eb, priv)
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

	success = true
	return bd, nil
}
