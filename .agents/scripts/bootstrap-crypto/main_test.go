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
	cmd := exec.Command("go", "build", "-o", binPath, "main.go")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, string(out))
	}
	return binPath
}

func buildMockForgeGrid(t *testing.T, dir string) string {
	mockForgeGridSrc := filepath.Join(dir, "mock_forgegrid.go")
	os.WriteFile(mockForgeGridSrc, []byte(`package main
import (
	"os"
	"path/filepath"
)
func main() {
	if os.Getenv("MOCK_FAIL") == "1" {
		os.Exit(1)
	}
	for i, arg := range os.Args {
		if arg == "--token-file" && i+1 < len(os.Args) {
			os.Remove(os.Args[i+1])
		}
	}
	// Create mock config to satisfy decrypt-and-apply
	localAppData := os.Getenv("LOCALAPPDATA")
	configPath := filepath.Join(localAppData, "ForgeGrid", "agentclient.json")
	os.MkdirAll(filepath.Dir(configPath), 0700)
	os.WriteFile(configPath, []byte("{}"), 0600)
}
`), 0600)
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
		m, _ := rs.load()
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
		bBytes, _ := json.Marshal(b)
		os.WriteFile(plainIn, bBytes, 0600)

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
		ebBytes, _ := os.ReadFile(bundlePath)
		var eb agentbridge.EncryptedBundle
		json.Unmarshal(ebBytes, &eb)

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
		exec.Command(binPath, "generate", privPath, pubPath).Run()

		bundlePath := createBundle(pubPath)
		localAppData := filepath.Join(tmpDir, "LocalAppData_Fail")

		ebBytes, _ := os.ReadFile(bundlePath)
		var eb agentbridge.EncryptedBundle
		json.Unmarshal(ebBytes, &eb)

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
		m, _ := rs.load()
		if _, ok := m[eb.BootstrapID]; ok {
			t.Fatalf("Expected reservation to be released, but found in store")
		}

		// 3. The same bundle can be retried and succeeds (MOCK_FAIL=0)
		privCopy := privPath + ".copy"
		b, _ := os.ReadFile(privPath)
		os.WriteFile(privCopy, b, 0600)

		cmd2 := exec.Command(binPath, "decrypt-and-apply", privPath, bundlePath, mockForgeGridExe)
		cmd2.Env = append(os.Environ(), "LOCALAPPDATA="+localAppData)
		out2, err := cmd2.CombinedOutput()
		if err != nil {
			t.Fatalf("Retry failed: %v\nOutput: %s", err, string(out2))
		}

		// 4. A third attempt is rejected as consumed
		// Re-create the bundle file and priv key since decrypt-and-apply deletes them on success
		os.WriteFile(bundlePath, ebBytes, 0600)
		os.WriteFile(privPath, b, 0600)
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
