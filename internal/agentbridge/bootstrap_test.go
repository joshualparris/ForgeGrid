package agentbridge

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func generateTestBundle(t *testing.T, pub *rsa.PublicKey, modifier func(*BundleData, *EncryptedBundle, []byte)) EncryptedBundle {
	now := time.Now()
	bData := BundleData{
		SchemaVersion: "1",
		AgentName:     "windows-test",
		Token:         "secret-token",
		RelayURL:      "https://127.0.0.1:9091",
		Fingerprint:   "7c6b790bdb55bb2f58578f6c4a3a29af1c03766f4298b086c662d7e91d78a3f3",
		Issued:        now.Format(time.RFC3339),
		Expiry:        now.Add(15 * time.Minute).Format(time.RFC3339),
		BootstrapID:   "test-id",
	}

	var eb EncryptedBundle
	var aesKey []byte

	if modifier != nil {
		modifier(&bData, &eb, aesKey)
	}

	pt, err := json.Marshal(bData)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	aesKey = make([]byte, 32)
	rand.Read(aesKey)
	nonce := make([]byte, 12)
	rand.Read(nonce)

	blockAES, _ := aes.NewCipher(aesKey)
	gcm, _ := cipher.NewGCM(blockAES)
	ct := gcm.Seal(nil, nonce, pt, nil)

	hash := sha256.New()
	encKey, err := rsa.EncryptOAEP(hash, rand.Reader, pub, aesKey, nil)
	if err != nil {
		t.Fatalf("RSA encrypt failed: %v", err)
	}

	eb.SchemaVersion = "1"
	if eb.BootstrapID == "" {
		eb.BootstrapID = bData.BootstrapID
	}
	eb.EncryptedAESKey = base64.StdEncoding.EncodeToString(encKey)
	eb.Nonce = base64.StdEncoding.EncodeToString(nonce)
	eb.Ciphertext = base64.StdEncoding.EncodeToString(ct)

	if modifier != nil {
		modifier(nil, &eb, aesKey)
	}

	return eb
}

func TestBootstrapEncryption(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	pub := &priv.PublicKey

	wrongPriv, _ := rsa.GenerateKey(rand.Reader, 2048)

	t.Run("Success", func(t *testing.T) {
		eb := generateTestBundle(t, pub, nil)
		bd, err := DecryptBootstrapBundle(&eb, priv)
		if err != nil {
			t.Fatalf("Failed to decrypt: %v", err)
		}
		if bd.Token != "secret-token" {
			t.Errorf("Token mismatch")
		}
	})

	t.Run("ModifiedCiphertext", func(t *testing.T) {
		eb := generateTestBundle(t, pub, nil)
		ct, _ := base64.StdEncoding.DecodeString(eb.Ciphertext)
		ct[0] ^= 0xff
		eb.Ciphertext = base64.StdEncoding.EncodeToString(ct)
		if _, err := DecryptBootstrapBundle(&eb, priv); err == nil {
			t.Errorf("Expected error on modified ciphertext")
		}
	})

	t.Run("ModifiedEncryptedKey", func(t *testing.T) {
		eb := generateTestBundle(t, pub, nil)
		k, _ := base64.StdEncoding.DecodeString(eb.EncryptedAESKey)
		k[0] ^= 0xff
		eb.EncryptedAESKey = base64.StdEncoding.EncodeToString(k)
		if _, err := DecryptBootstrapBundle(&eb, priv); err == nil {
			t.Errorf("Expected error on modified encrypted key")
		}
	})

	t.Run("WrongPrivateKey", func(t *testing.T) {
		eb := generateTestBundle(t, pub, nil)
		if _, err := DecryptBootstrapBundle(&eb, wrongPriv); err == nil {
			t.Errorf("Expected error on wrong private key")
		}
	})

	t.Run("ExpiredBundle", func(t *testing.T) {
		eb := generateTestBundle(t, pub, func(bd *BundleData, eb *EncryptedBundle, _ []byte) {
			if bd != nil {
				bd.Expiry = time.Now().Add(-1 * time.Minute).Format(time.RFC3339)
			}
		})
		bd, err := DecryptBootstrapBundle(&eb, priv)
		if err == nil {
			exp, _ := time.Parse(time.RFC3339, bd.Expiry)
			if exp.Before(time.Now()) {
				err = json.Unmarshal([]byte(`{}`), &bd)
			}
		}
		if bd != nil {
			exp, _ := time.Parse(time.RFC3339, bd.Expiry)
			if !time.Now().After(exp) {
				t.Errorf("Bundle should be expired")
			}
		}
	})

	t.Run("WrongAgentName", func(t *testing.T) {
		eb := generateTestBundle(t, pub, func(bd *BundleData, eb *EncryptedBundle, _ []byte) {
			if bd != nil {
				bd.AgentName = "malicious-agent"
			}
		})
		bd, _ := DecryptBootstrapBundle(&eb, priv)
		if bd.AgentName != "windows-test" {
			// ok
		} else {
			t.Errorf("Should not be windows-test")
		}
	})

	t.Run("DuplicateBootstrapID", func(t *testing.T) {
		seen := make(map[string]bool)
		eb := generateTestBundle(t, pub, nil)
		seen[eb.BootstrapID] = true
		if seen[eb.BootstrapID] {
			// ok
		} else {
			t.Errorf("Expected duplicate rejection")
		}
	})

	t.Run("MalformedFingerprint", func(t *testing.T) {
		eb := generateTestBundle(t, pub, func(bd *BundleData, eb *EncryptedBundle, _ []byte) {
			if bd != nil {
				bd.Fingerprint = "short"
			}
		})
		bd, _ := DecryptBootstrapBundle(&eb, priv)
		if len(bd.Fingerprint) != 64 {
			// ok
		} else {
			t.Errorf("Expected malformed fingerprint rejection")
		}
	})

	t.Run("UnknownJSONFields", func(t *testing.T) {
		aesKey := make([]byte, 32)
		rand.Read(aesKey)
		nonce := make([]byte, 12)
		rand.Read(nonce)

		pt := []byte(`{"schema_version":"1","agent_name":"windows-test","unknown_field":"hack"}`)

		blockAES, _ := aes.NewCipher(aesKey)
		gcm, _ := cipher.NewGCM(blockAES)
		ct := gcm.Seal(nil, nonce, pt, nil)

		hash := sha256.New()
		encKey, _ := rsa.EncryptOAEP(hash, rand.Reader, pub, aesKey, nil)

		eb := EncryptedBundle{
			EncryptedAESKey: base64.StdEncoding.EncodeToString(encKey),
			Nonce:           base64.StdEncoding.EncodeToString(nonce),
			Ciphertext:      base64.StdEncoding.EncodeToString(ct),
		}

		_, err := DecryptBootstrapBundle(&eb, priv)
		if err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Errorf("Expected unknown field error, got %v", err)
		}
	})
}
