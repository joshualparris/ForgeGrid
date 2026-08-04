package agentbridge

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockReplayStore struct {
	mu       sync.Mutex
	reserved map[string]bool
	consumed map[string]bool
}

func newMockReplayStore() *mockReplayStore {
	return &mockReplayStore{
		reserved: make(map[string]bool),
		consumed: make(map[string]bool),
	}
}

func (m *mockReplayStore) Reserve(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.consumed[id] {
		return errors.New("already consumed")
	}
	if m.reserved[id] {
		return errors.New("already reserved")
	}
	m.reserved[id] = true
	return nil
}

func (m *mockReplayStore) Commit(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.reserved[id] {
		return errors.New("not reserved")
	}
	if m.consumed[id] {
		return errors.New("already consumed")
	}
	delete(m.reserved, id)
	m.consumed[id] = true
	return nil
}

func (m *mockReplayStore) Release(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.consumed[id] {
		return errors.New("cannot release consumed")
	}
	delete(m.reserved, id)
	return nil
}

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
	if _, err := rand.Read(aesKey); err != nil {
		t.Fatalf("rand.Read failed: %v", err)
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("rand.Read failed: %v", err)
	}

	blockAES, err := aes.NewCipher(aesKey)
	if err != nil {
		t.Fatalf("NewCipher failed: %v", err)
	}
	gcm, err := cipher.NewGCM(blockAES)
	if err != nil {
		t.Fatalf("NewGCM failed: %v", err)
	}

	aad := getAAD(bData.SchemaVersion, bData.BootstrapID)
	ct := gcm.Seal(nil, nonce, pt, aad)

	hash := sha256.New()
	encKey, err := rsa.EncryptOAEP(hash, rand.Reader, pub, aesKey, nil)
	if err != nil {
		t.Fatalf("RSA encrypt failed: %v", err)
	}

	if eb.SchemaVersion == "" {
		eb.SchemaVersion = "1"
	}
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
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	pub := &priv.PublicKey

	wrongPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	t.Run("Success", func(t *testing.T) {
		eb := generateTestBundle(t, pub, nil)
		rs := newMockReplayStore()
		bd, err := ValidateBootstrapBundle(&eb, priv, "windows-test", rs)
		if err != nil {
			t.Fatalf("Failed to validate: %v", err)
		}
		if bd.Token != "secret-token" {
			t.Errorf("Token mismatch")
		}
	})

	t.Run("ModifiedCiphertext", func(t *testing.T) {
		eb := generateTestBundle(t, pub, nil)
		ct, err := base64.StdEncoding.DecodeString(eb.Ciphertext)
		if err != nil {
			t.Fatalf("DecodeString failed: %v", err)
		}
		ct[0] ^= 0xff
		eb.Ciphertext = base64.StdEncoding.EncodeToString(ct)
		rs := newMockReplayStore()
		if _, err := ValidateBootstrapBundle(&eb, priv, "windows-test", rs); err == nil {
			t.Errorf("Expected error on modified ciphertext")
		}
	})

	t.Run("ModifiedEncryptedKey", func(t *testing.T) {
		eb := generateTestBundle(t, pub, nil)
		k, err := base64.StdEncoding.DecodeString(eb.EncryptedAESKey)
		if err != nil {
			t.Fatalf("DecodeString failed: %v", err)
		}
		k[0] ^= 0xff
		eb.EncryptedAESKey = base64.StdEncoding.EncodeToString(k)
		rs := newMockReplayStore()
		if _, err := ValidateBootstrapBundle(&eb, priv, "windows-test", rs); err == nil {
			t.Errorf("Expected error on modified encrypted key")
		}
	})

	t.Run("WrongPrivateKey", func(t *testing.T) {
		eb := generateTestBundle(t, pub, nil)
		rs := newMockReplayStore()
		if _, err := ValidateBootstrapBundle(&eb, wrongPriv, "windows-test", rs); err == nil {
			t.Errorf("Expected error on wrong private key")
		}
	})

	t.Run("ExpiredBundle", func(t *testing.T) {
		eb := generateTestBundle(t, pub, func(bd *BundleData, eb *EncryptedBundle, _ []byte) {
			if bd != nil {
				bd.Issued = time.Now().Add(-2 * time.Minute).Format(time.RFC3339)
				bd.Expiry = time.Now().Add(-1 * time.Minute).Format(time.RFC3339)
			}
		})
		rs := newMockReplayStore()
		if _, err := ValidateBootstrapBundle(&eb, priv, "windows-test", rs); err == nil {
			t.Errorf("Expected error on expired bundle")
		} else if !strings.Contains(err.Error(), "expired") {
			t.Errorf("Expected expired error, got: %v", err)
		}
	})

	t.Run("FutureIssuedBundle", func(t *testing.T) {
		eb := generateTestBundle(t, pub, func(bd *BundleData, eb *EncryptedBundle, _ []byte) {
			if bd != nil {
				bd.Issued = time.Now().Add(10 * time.Minute).Format(time.RFC3339)
				bd.Expiry = time.Now().Add(25 * time.Minute).Format(time.RFC3339)
			}
		})
		rs := newMockReplayStore()
		if _, err := ValidateBootstrapBundle(&eb, priv, "windows-test", rs); err == nil {
			t.Errorf("Expected error on future issued bundle")
		} else if !strings.Contains(err.Error(), "future") {
			t.Errorf("Expected future error, got: %v", err)
		}
	})

	t.Run("WrongAgentName", func(t *testing.T) {
		eb := generateTestBundle(t, pub, func(bd *BundleData, eb *EncryptedBundle, _ []byte) {
			if bd != nil {
				bd.AgentName = "malicious-agent"
			}
		})
		rs := newMockReplayStore()
		if _, err := ValidateBootstrapBundle(&eb, priv, "windows-test", rs); err == nil {
			t.Errorf("Expected error on wrong agent name")
		} else if !strings.Contains(err.Error(), "unexpected agent_name") {
			t.Errorf("Expected unexpected agent_name error, got: %v", err)
		}
	})

	t.Run("DuplicateBootstrapID", func(t *testing.T) {
		eb := generateTestBundle(t, pub, nil)
		rs := newMockReplayStore()
		// First validation should succeed
		if _, err := ValidateBootstrapBundle(&eb, priv, "windows-test", rs); err != nil {
			t.Fatalf("First validation failed: %v", err)
		}
		// Manually commit it to simulate configuration success
		rs.Commit(eb.BootstrapID)
		// Second should fail with replay error
		if _, err := ValidateBootstrapBundle(&eb, priv, "windows-test", rs); err == nil {
			t.Errorf("Expected error on duplicate bootstrap ID")
		} else if !strings.Contains(err.Error(), "consumed") {
			t.Errorf("Expected consumed error, got: %v", err)
		}
	})

	t.Run("MalformedFingerprint", func(t *testing.T) {
		eb := generateTestBundle(t, pub, func(bd *BundleData, eb *EncryptedBundle, _ []byte) {
			if bd != nil {
				bd.Fingerprint = "short"
			}
		})
		rs := newMockReplayStore()
		if _, err := ValidateBootstrapBundle(&eb, priv, "windows-test", rs); err == nil {
			t.Errorf("Expected error on malformed fingerprint")
		} else if !strings.Contains(err.Error(), "fingerprint") {
			t.Errorf("Expected fingerprint error, got: %v", err)
		}
	})

	t.Run("MismatchedBootstrapIDs", func(t *testing.T) {
		eb := generateTestBundle(t, pub, func(bd *BundleData, eb *EncryptedBundle, _ []byte) {
			if eb != nil {
				eb.BootstrapID = "different-id"
			}
		})
		rs := newMockReplayStore()
		if _, err := ValidateBootstrapBundle(&eb, priv, "windows-test", rs); err == nil {
			t.Errorf("Expected error on mismatched bootstrap IDs")
		} else if !strings.Contains(err.Error(), "mismatched") && !strings.Contains(err.Error(), "decryption failed") {
			t.Errorf("Expected mismatched or decryption error, got: %v", err)
		}
	})

	t.Run("WrongSchemaVersion", func(t *testing.T) {
		eb := generateTestBundle(t, pub, func(bd *BundleData, eb *EncryptedBundle, _ []byte) {
			if eb != nil {
				eb.SchemaVersion = "2"
			}
		})
		rs := newMockReplayStore()
		if _, err := ValidateBootstrapBundle(&eb, priv, "windows-test", rs); err == nil {
			t.Errorf("Expected error on wrong schema version")
		} else if !strings.Contains(err.Error(), "schema_version") {
			t.Errorf("Expected schema_version error, got: %v", err)
		}

		ebInner := generateTestBundle(t, pub, func(bd *BundleData, eb *EncryptedBundle, _ []byte) {
			if bd != nil {
				bd.SchemaVersion = "2"
			}
		})
		if _, err := ValidateBootstrapBundle(&ebInner, priv, "windows-test", rs); err == nil {
			t.Errorf("Expected error on wrong inner schema version")
		} else if !strings.Contains(err.Error(), "schema_version") && !strings.Contains(err.Error(), "decryption failed") {
			t.Errorf("Expected schema_version or decryption error, got: %v", err)
		}
	})

	t.Run("TrailingJSONData", func(t *testing.T) {
		aesKey := make([]byte, 32)
		if _, err := rand.Read(aesKey); err != nil {
			t.Fatalf("rand.Read failed: %v", err)
		}
		nonce := make([]byte, 12)
		if _, err := rand.Read(nonce); err != nil {
			t.Fatalf("rand.Read failed: %v", err)
		}

		pt := []byte(`{"schema_version":"1","agent_name":"windows-test","token":"sec","relay_url":"url","fingerprint":"0000000000000000000000000000000000000000000000000000000000000000","issued":"2020-01-01T00:00:00Z","expiry":"2030-01-01T00:00:00Z","bootstrap_id":"id"} trailing`)

		blockAES, err := aes.NewCipher(aesKey)
		if err != nil {
			t.Fatalf("NewCipher failed: %v", err)
		}
		gcm, err := cipher.NewGCM(blockAES)
		if err != nil {
			t.Fatalf("NewGCM failed: %v", err)
		}

		aad := getAAD("1", "id")
		ct := gcm.Seal(nil, nonce, pt, aad)

		hash := sha256.New()
		encKey, err := rsa.EncryptOAEP(hash, rand.Reader, pub, aesKey, nil)
		if err != nil {
			t.Fatalf("EncryptOAEP failed: %v", err)
		}

		eb := EncryptedBundle{
			SchemaVersion:   "1",
			BootstrapID:     "id",
			EncryptedAESKey: base64.StdEncoding.EncodeToString(encKey),
			Nonce:           base64.StdEncoding.EncodeToString(nonce),
			Ciphertext:      base64.StdEncoding.EncodeToString(ct),
		}

		rs := newMockReplayStore()
		if _, err := ValidateBootstrapBundle(&eb, priv, "windows-test", rs); err == nil {
			t.Errorf("Expected error on trailing JSON data")
		} else if !strings.Contains(err.Error(), "trailing") {
			t.Errorf("Expected trailing data error, got: %v", err)
		}
	})

	t.Run("UnknownJSONFields", func(t *testing.T) {
		aesKey := make([]byte, 32)
		if _, err := rand.Read(aesKey); err != nil {
			t.Fatalf("rand.Read failed: %v", err)
		}
		nonce := make([]byte, 12)
		if _, err := rand.Read(nonce); err != nil {
			t.Fatalf("rand.Read failed: %v", err)
		}

		pt := []byte(`{"schema_version":"1","agent_name":"windows-test","unknown_field":"hack"}`)

		blockAES, err := aes.NewCipher(aesKey)
		if err != nil {
			t.Fatalf("NewCipher failed: %v", err)
		}
		gcm, err := cipher.NewGCM(blockAES)
		if err != nil {
			t.Fatalf("NewGCM failed: %v", err)
		}

		aad := getAAD("1", "id")
		ct := gcm.Seal(nil, nonce, pt, aad)

		hash := sha256.New()
		encKey, err := rsa.EncryptOAEP(hash, rand.Reader, pub, aesKey, nil)
		if err != nil {
			t.Fatalf("EncryptOAEP failed: %v", err)
		}

		eb := EncryptedBundle{
			SchemaVersion:   "1",
			BootstrapID:     "id",
			EncryptedAESKey: base64.StdEncoding.EncodeToString(encKey),
			Nonce:           base64.StdEncoding.EncodeToString(nonce),
			Ciphertext:      base64.StdEncoding.EncodeToString(ct),
		}

		rs := newMockReplayStore()
		if _, err := ValidateBootstrapBundle(&eb, priv, "windows-test", rs); err == nil {
			t.Errorf("Expected error on unknown JSON fields")
		} else if !strings.Contains(err.Error(), "unknown field") {
			t.Errorf("Expected unknown field error, got %v", err)
		}
	})
}

func TestBootstrapEncryptionConcurrent(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	pub := &priv.PublicKey

	t.Run("Two simultaneous reservations cannot both succeed", func(t *testing.T) {
		eb := generateTestBundle(t, pub, nil)
		rs := newMockReplayStore()

		var wg sync.WaitGroup
		successCount := 0
		var mu sync.Mutex

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := ValidateBootstrapBundle(&eb, priv, "windows-test", rs)
				if err == nil {
					mu.Lock()
					successCount++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()

		if successCount != 1 {
			t.Errorf("Expected exactly 1 successful reservation, got %d", successCount)
		}
	})

	t.Run("Validation failure releases reservation", func(t *testing.T) {
		eb := generateTestBundle(t, pub, func(bd *BundleData, eb *EncryptedBundle, _ []byte) {
			if bd != nil {
				bd.AgentName = "malicious-agent" // Causes validation failure after reservation
			}
		})
		rs := newMockReplayStore()

		_, err := ValidateBootstrapBundle(&eb, priv, "windows-test", rs)
		if err == nil {
			t.Fatalf("Expected validation failure")
		}

		// The reservation should be released, so reserving again directly should succeed
		if err := rs.Reserve(eb.BootstrapID); err != nil {
			t.Errorf("Expected reservation to be released, but got error: %v", err)
		}
	})

	t.Run("Configuration failure can release and retry", func(t *testing.T) {
		eb := generateTestBundle(t, pub, nil)
		rs := newMockReplayStore()

		// 1. Initial Validation
		_, err := ValidateBootstrapBundle(&eb, priv, "windows-test", rs)
		if err != nil {
			t.Fatalf("First validation failed: %v", err)
		}

		// 2. Simulate configuration failure -> release reservation
		if err := rs.Release(eb.BootstrapID); err != nil {
			t.Fatalf("Release failed: %v", err)
		}

		// 3. Retry Validation -> should succeed again
		_, err = ValidateBootstrapBundle(&eb, priv, "windows-test", rs)
		if err != nil {
			t.Fatalf("Retry validation failed: %v", err)
		}
	})

	t.Run("Successful completion permanently prevents reuse", func(t *testing.T) {
		eb := generateTestBundle(t, pub, nil)
		rs := newMockReplayStore()

		// 1. Initial Validation
		_, err := ValidateBootstrapBundle(&eb, priv, "windows-test", rs)
		if err != nil {
			t.Fatalf("First validation failed: %v", err)
		}

		// 2. Simulate configuration success -> commit reservation
		if err := rs.Commit(eb.BootstrapID); err != nil {
			t.Fatalf("Commit failed: %v", err)
		}

		// 3. Reuse attempt -> should fail
		_, err = ValidateBootstrapBundle(&eb, priv, "windows-test", rs)
		if err == nil {
			t.Fatalf("Expected failure on reuse after commit")
		} else if !strings.Contains(err.Error(), "consumed") {
			t.Errorf("Expected consumed error, got: %v", err)
		}
	})
}
