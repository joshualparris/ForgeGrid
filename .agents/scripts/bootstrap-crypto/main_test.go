package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"forgegrid/internal/agentbridge"
)

func buildBinary(t *testing.T) string {
	binPath := filepath.Join(t.TempDir(), "bootstrap-crypto.exe")
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, string(out))
	}
	return binPath
}

func buildMockForgeGrid(t *testing.T, dir string) string {
	mockForgeGridSrc := filepath.Join(dir, "mock_forgegrid.go")
	if err := os.WriteFile(mockForgeGridSrc, []byte(`package main
import (
	"encoding/json"
	"os"
	"path/filepath"
)
type ClientConfig struct {
	Name        string `+"`json:\"name\"`"+`
	Token       string `+"`json:\"token\"`"+`
	URL         string `+"`json:\"url\"`"+`
	Fingerprint string `+"`json:\"fingerprint\"`"+`
}
func main() {
	if os.Getenv("MOCK_FAIL") == "1" {
		os.Exit(1)
	}
	var name, tokenFile, url, fp string
	for i, arg := range os.Args {
		if arg == "--token-file" && i+1 < len(os.Args) {
			tokenFile = os.Args[i+1]
		}
		if arg == "--name" && i+1 < len(os.Args) {
			name = os.Args[i+1]
		}
		if arg == "--url" && i+1 < len(os.Args) {
			url = os.Args[i+1]
		}
		if arg == "--fingerprint" && i+1 < len(os.Args) {
			fp = os.Args[i+1]
		}
	}
	
	token := ""
	if tokenFile != "" {
		b, err := os.ReadFile(tokenFile)
		if err == nil {
			token = string(b)
		}
		os.Remove(tokenFile)
	}
	
	// Create mock config to satisfy decrypt-and-apply
	localAppData := os.Getenv("LOCALAPPDATA")
	configPath := filepath.Join(localAppData, "ForgeGrid", "agentclient.json")
	os.MkdirAll(filepath.Dir(configPath), 0700)
	
	cfg := ClientConfig{
		Name:        name,
		Token:       token,
		URL:         url,
		Fingerprint: fp,
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(configPath, b, 0600); err != nil {
		os.Exit(2)
	}
}
`), 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	mockForgeGridExe := filepath.Join(dir, "mock_forgegrid.exe")
	if out, err := exec.Command("go", "build", "-o", mockForgeGridExe, mockForgeGridSrc).CombinedOutput(); err != nil {
		t.Fatalf("Failed to build mock forgegrid: %v\n%s", err, string(out))
	}
	return mockForgeGridExe
}

func TestReplayStore(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "replay-state.json")
	lockPath := filepath.Join(tmpDir, "replay-state.lock")
	rs := &WindowsReplayStore{Path: storePath, LockFile: lockPath}

	// 1. Concurrent Reserve calls allow exactly one success
	t.Run("ConcurrentReserve", func(t *testing.T) {
		id := "conc-test"
		var wg sync.WaitGroup
		successCount := 0
		var mu sync.Mutex

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rsLocal := &WindowsReplayStore{Path: storePath, LockFile: lockPath}
				err := rsLocal.Reserve(id)
				if err == nil {
					mu.Lock()
					successCount++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		if successCount != 1 {
			t.Fatalf("Expected exactly 1 success, got %d", successCount)
		}
	})

	// 2. Active reservations cannot be reclaimed
	t.Run("ActiveReservation", func(t *testing.T) {
		err := rs.Reserve("active-test")
		if err != nil {
			t.Fatalf("Reserve failed: %v", err)
		}
		err = rs.Reserve("active-test")
		if err == nil || !strings.Contains(err.Error(), "currently reserved") {
			t.Fatalf("Expected 'currently reserved' error, got %v", err)
		}
	})

	// 3. Stale reservations can be reclaimed
	t.Run("StaleReservation", func(t *testing.T) {
		err := rs.Reserve("stale-test")
		if err != nil {
			t.Fatalf("Reserve failed: %v", err)
		}
		// Manually backdate the time
		m, err := rs.load()
		if err != nil {
			t.Fatalf("load failed: %v", err)
		}
		state := m["stale-test"]
		state.Time = time.Now().Add(-16 * time.Minute)
		m["stale-test"] = state
		rs.save(m)

		err = rs.Reserve("stale-test")
		if err != nil {
			t.Fatalf("Failed to reclaim stale reservation: %v", err)
		}
	})

	// 4. Consumed IDs survive restart
	t.Run("ConsumedSurviveRestart", func(t *testing.T) {
		err := rs.Reserve("consume-test")
		if err != nil {
			t.Fatalf("Reserve failed: %v", err)
		}
		err = rs.Commit("consume-test")
		if err != nil {
			t.Fatalf("Commit failed: %v", err)
		}

		// New instance (simulate restart)
		rs2 := &WindowsReplayStore{Path: storePath, LockFile: lockPath}
		err = rs2.Reserve("consume-test")
		if err == nil || !strings.Contains(err.Error(), "already consumed") {
			t.Fatalf("Expected 'already consumed' error, got %v", err)
		}
	})

	// 5. Failure releases the reservation
	t.Run("ReleaseReservation", func(t *testing.T) {
		err := rs.Reserve("release-test")
		if err != nil {
			t.Fatalf("Reserve failed: %v", err)
		}
		err = rs.Release("release-test")
		if err != nil {
			t.Fatalf("Release failed: %v", err)
		}
		err = rs.Reserve("release-test")
		if err != nil {
			t.Fatalf("Failed to reserve after release: %v", err)
		}
	})
	// 6. Corrupt store tests
	t.Run("CorruptStore", func(t *testing.T) {
		// malformed JSON
		if err := os.WriteFile(storePath, []byte(`{"bad":`), 0600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
		if err := rs.Reserve("corrupt-test"); err == nil {
			t.Fatalf("Expected error on malformed JSON")
		}

		// unknown fields
		if err := os.WriteFile(storePath, []byte(`{"id":{"status":"reserved","time":"2023-01-01T00:00:00Z","unknown":1}}`), 0600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
		if err := rs.Reserve("corrupt-test2"); err == nil {
			t.Fatalf("Expected error on unknown fields")
		}

		// two valid JSON objects
		if err := os.WriteFile(storePath, []byte(`{"id1":{"status":"reserved","time":"2023-01-01T00:00:00Z"}}{"id2":{"status":"reserved","time":"2023-01-01T00:00:00Z"}}`), 0600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
		if err := rs.Reserve("corrupt-test3"); err == nil {
			t.Fatalf("Expected error on two valid JSON objects (trailing data)")
		}

		// valid JSON followed by malformed text
		if err := os.WriteFile(storePath, []byte(`{"id1":{"status":"reserved","time":"2023-01-01T00:00:00Z"}} trailing_text`), 0600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
		if err := rs.Reserve("corrupt-test3b"); err == nil {
			t.Fatalf("Expected error on valid JSON followed by malformed text")
		}

		// valid JSON followed by whitespace only
		if err := os.WriteFile(storePath, []byte(`{"id1":{"status":"reserved","time":"2023-01-01T00:00:00Z"}}   
		`), 0600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
		if err := rs.Reserve("corrupt-test3c"); err != nil {
			t.Fatalf("Unexpected error on valid JSON followed by whitespace only: %v", err)
		}

		// empty ID
		if err := os.WriteFile(storePath, []byte(`{"":{"status":"reserved","time":"2023-01-01T00:00:00Z"}}`), 0600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
		if err := rs.Reserve("corrupt-test4"); err == nil {
			t.Fatalf("Expected error on empty ID")
		}

		// clear file
		os.Remove(storePath)
	})
}

func TestInteroperabilityAndFailure(t *testing.T) {
	binPath := buildBinary(t)
	tmpDir := t.TempDir()
	mockForgeGridExe := buildMockForgeGrid(t, tmpDir)

	privPath := filepath.Join(tmpDir, "priv.pem")
	pubPath := filepath.Join(tmpDir, "pub.pem")

	// Generate keys
	cmd := exec.Command(binPath, "generate", privPath, pubPath)
	if err := cmd.Run(); err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	createBundle := func(pubPathArg string) string {
		plainIn := filepath.Join(tmpDir, "plain.json")
		b := agentbridge.BundleData{
			SchemaVersion: "1",
			AgentName:     "windows-test",
			Token:         "secret-token",
			RelayURL:      "https://127.0.0.1:9091",
			Fingerprint:   strings.Repeat("a", 64),
			Issued:        time.Now().Format(time.RFC3339),
			Expiry:        time.Now().Add(5 * time.Minute).Format(time.RFC3339),
			BootstrapID:   "boot-" + time.Now().Format("150405999999"),
		}
		bBytes, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		if err := os.WriteFile(plainIn, bBytes, 0600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		hybridOut := filepath.Join(tmpDir, "hybrid.json")
		// Using the encrypt command which calls GenerateBootstrapBundle internally
		cmd := exec.Command(binPath, "encrypt", pubPathArg, plainIn, hybridOut)
		if err := cmd.Run(); err != nil {
			t.Fatalf("encrypt failed: %v", err)
		}
		return hybridOut
	}

	t.Run("Interoperability", func(t *testing.T) {
		bundlePath := createBundle(pubPath)
		localAppData := filepath.Join(tmpDir, "LocalAppData_Interop")

		// Read it before decrypt-and-apply deletes it
		ebBytes, err := os.ReadFile(bundlePath)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		var eb agentbridge.EncryptedBundle
		if err := json.Unmarshal(ebBytes, &eb); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}

		cmd := exec.Command(binPath, "decrypt-and-apply", privPath, bundlePath, mockForgeGridExe)
		cmd.Env = append(os.Environ(), "LOCALAPPDATA="+localAppData)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("decrypt-and-apply failed: %v\nOutput: %s", err, string(out))
		}

		// No token appears in output or logs
		if bytes.Contains(out, []byte("secret-token")) {
			t.Fatalf("Token leaked in output!")
		}

		// Verify successful configuration commits consumption
		rs := &WindowsReplayStore{
			Path:     filepath.Join(localAppData, "ForgeGrid", "bootstrap", "replay-state.json"),
			LockFile: filepath.Join(localAppData, "ForgeGrid", "bootstrap", "replay-state.lock"),
		}

		err = rs.Reserve(eb.BootstrapID)
		if err == nil || !strings.Contains(err.Error(), "already consumed") {
			t.Fatalf("Expected already consumed, got %v", err)
		}
	})

	t.Run("ConfigurationFailure", func(t *testing.T) {
		// Generate fresh keys since the previous test deleted them on success
		privPath := filepath.Join(tmpDir, "priv_fail.pem")
		pubPath := filepath.Join(tmpDir, "pub_fail.pem")
		if err := exec.Command(binPath, "generate", privPath, pubPath).Run(); err != nil {
			t.Fatalf("generate failed: %v", err)
		}

		bundlePath := createBundle(pubPath)
		localAppData := filepath.Join(tmpDir, "LocalAppData_Fail")

		ebBytes, err := os.ReadFile(bundlePath)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		var eb agentbridge.EncryptedBundle
		if err := json.Unmarshal(ebBytes, &eb); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}

		// 1. The first apply attempt fails (simulate config failure)
		cmd := exec.Command(binPath, "decrypt-and-apply", privPath, bundlePath, mockForgeGridExe)
		cmd.Env = append(os.Environ(), "LOCALAPPDATA="+localAppData, "MOCK_FAIL=1")
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("Expected failure due to MOCK_FAIL=1")
		}
		if bytes.Contains(out, []byte("secret-token")) {
			t.Fatalf("Token leaked in output!")
		}

		// 2. The reservation is released (verify we can reserve it manually, or just retry)
		rs := &WindowsReplayStore{
			Path:     filepath.Join(localAppData, "ForgeGrid", "bootstrap", "replay-state.json"),
			LockFile: filepath.Join(localAppData, "ForgeGrid", "bootstrap", "replay-state.lock"),
		}
		m, err := rs.load()
		if err != nil {
			t.Fatalf("load failed: %v", err)
		}
		if _, ok := m[eb.BootstrapID]; ok {
			t.Fatalf("Expected reservation to be released, but found in store")
		}

		// 3. The same bundle can be retried and succeeds (MOCK_FAIL=0)
		privCopy := privPath + ".copy"
		b, err := os.ReadFile(privPath)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if err := os.WriteFile(privCopy, b, 0600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		cmd2 := exec.Command(binPath, "decrypt-and-apply", privPath, bundlePath, mockForgeGridExe)
		cmd2.Env = append(os.Environ(), "LOCALAPPDATA="+localAppData)
		out2, err := cmd2.CombinedOutput()
		if err != nil {
			t.Fatalf("Retry failed: %v\nOutput: %s", err, string(out2))
		}

		// 4. A third attempt is rejected as consumed
		// Re-create the bundle file and priv key since decrypt-and-apply deletes them on success
		if err := os.WriteFile(bundlePath, ebBytes, 0600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
		if err := os.WriteFile(privPath, b, 0600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
		cmd3 := exec.Command(binPath, "decrypt-and-apply", privPath, bundlePath, mockForgeGridExe)
		cmd3.Env = append(os.Environ(), "LOCALAPPDATA="+localAppData)
		out3, err := cmd3.CombinedOutput()
		if err == nil {
			t.Fatalf("Expected third attempt to fail as already consumed")
		}
		if !bytes.Contains(out3, []byte("already consumed")) {
			t.Fatalf("Expected already consumed message, got: %s", string(out3))
		}
	})
}
