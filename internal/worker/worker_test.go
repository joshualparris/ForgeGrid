package worker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkerCredentials(t *testing.T) {
	// Setup custom home dir to avoid polluting user space during tests
	tmpDir := t.TempDir()
	os.Setenv("XDG_DATA_HOME", tmpDir)
	os.Setenv("LOCALAPPDATA", tmpDir)
	os.Setenv("APPDATA", tmpDir)

	// Ensure reset handles cleanly when missing
	err := ResetCredentials()
	if err != nil {
		t.Fatalf("ResetCredentials should not fail if credentials don't exist: %v", err)
	}

	w1 := New("TestNode", "./tmp-ws", true)
	w1.WorkerID = "worker-123"
	w1.Token = "token-abc"
	w1.CoordinatorURL = "http://127.0.0.1:8080"
	w1.Fingerprint = "fp-xxx"

	creds := WorkerCredentials{
		WorkerID:       w1.WorkerID,
		Token:          w1.Token,
		CoordinatorURL: w1.CoordinatorURL,
		Fingerprint:    w1.Fingerprint,
		NodeName:       w1.NodeName,
		Insecure:       w1.Insecure,
	}

	path := getWorkerCredsPath()
	os.MkdirAll(filepath.Dir(path), 0700)
	b, _ := json.MarshalIndent(creds, "", "  ")
	os.WriteFile(path, b, 0600)

	// Test 2: Restart loads credentials
	w2 := New("TestNode", "./tmp-ws", true)
	err = w2.LoadCreds()
	if err != nil {
		t.Fatalf("Failed to load credentials: %v", err)
	}

	// Test 3: Restart reconnects with same ID
	if w2.WorkerID != w1.WorkerID {
		t.Fatalf("Expected WorkerID %s, got %s", w1.WorkerID, w2.WorkerID)
	}
	if w2.Token != w1.Token {
		t.Fatalf("Expected Token %s, got %s", w1.Token, w2.Token)
	}

	// Test 7: Reset removes credentials
	err = ResetCredentials()
	if err != nil {
		t.Fatalf("Failed to reset credentials: %v", err)
	}

	err = w2.LoadCreds()
	if err == nil {
		t.Fatalf("Expected LoadCreds to fail after reset, but it succeeded")
	}
}

func TestHardwareDetection(t *testing.T) {
	w := New("TestNode", "./tmp-ws", true)
	info, err := w.getHardwareInfo()
	if err != nil {
		t.Fatalf("Hardware detection failed: %v", err)
	}

	// Test 8: Disk detection returns non-zero
	if info.FreeWorkspaceDisk == 0 {
		t.Fatalf("FreeWorkspaceDisk reported 0. Workspace: %s", w.Workspace)
	}
	if info.TotalRAM == 0 {
		t.Fatalf("TotalRAM reported 0")
	}
}

func TestValidateCapabilities(t *testing.T) {
	w := New("TestNode", "./tmp-ws", true)
	w.Capabilities = []string{"go", "non_existent_tool_12345"}
	
	valid, drift := w.ValidateCapabilities()
	
	// "go" might not be installed on test env, but we can mock or just check logic.
	// Actually we expect non_existent_tool_12345 to ALWAYS be in drift.
	hasDrift := false
	for _, d := range drift {
		if d == "non_existent_tool_12345" {
			hasDrift = true
		}
	}
	
	// Avoid unused variable valid
	_ = valid
	
	if !hasDrift {
		t.Fatalf("Expected non_existent_tool_12345 to be detected as missing drift")
	}
}
