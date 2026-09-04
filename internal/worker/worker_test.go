package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestLoadCredsPersistsNameOverHostname(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("XDG_DATA_HOME", tmpDir)
	os.Setenv("LOCALAPPDATA", tmpDir)
	os.Setenv("APPDATA", tmpDir)
	ResetCredentials()

	// Simulate credentials saved with a custom name
	creds := WorkerCredentials{
		WorkerID:       "worker-456",
		NodeName:       "Custom-Name",
	}
	path := getWorkerCredsPath()
	os.MkdirAll(filepath.Dir(path), 0700)
	b, _ := json.MarshalIndent(creds, "", "  ")
	os.WriteFile(path, b, 0600)

	hostname, _ := os.Hostname()

	// 1. Worker created with default OS hostname
	w1 := New(hostname, "./tmp-ws", true)
	w1.LoadCreds()
	if w1.NodeName != "Custom-Name" {
		t.Fatalf("Expected NodeName to be restored from creds to Custom-Name, got %s", w1.NodeName)
	}

	// 2. Worker created with "Unnamed-Node" (fallback test)
	w2 := New("Unnamed-Node", "./tmp-ws", true)
	w2.LoadCreds()
	if w2.NodeName != "Custom-Name" {
		t.Fatalf("Expected NodeName to be restored from creds, got %s", w2.NodeName)
	}

	// 3. Worker created with an explicit custom override (not the hostname and not Unnamed)
	// Currently, LoadCreds overrides ONLY if NodeName matches defaults, so explicit explicit custom
	// is NOT overridden by LoadCreds. Let's verify that behavior.
	w3 := New("Different-Custom", "./tmp-ws", true)
	w3.LoadCreds()
	if w3.NodeName != "Different-Custom" {
		t.Fatalf("Expected NodeName to remain Different-Custom if explicitly provided and not default, got %s", w3.NodeName)
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
	w.SetLabelsAndCapabilities("", "go,non_existent_tool_12345")

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

func TestDetectCapabilitiesIncludesGitAndAIAgentWhenAvailable(t *testing.T) {
	caps := DetectCapabilities()
	if _, err := exec.LookPath("git"); err == nil && !hasWorkerString(caps, "git") {
		t.Fatalf("expected git capability when git is available, got %#v", caps)
	}
	if hasWorkerString(caps, "antigravity") || hasWorkerString(caps, "codex") {
		if !hasWorkerString(caps, "ai-agent") {
			t.Fatalf("expected ai-agent capability when a coding agent is available, got %#v", caps)
		}
	}
}

func TestClassifyConnectionErrorExplainsTLSMismatch(t *testing.T) {
	msg := classifyConnectionError(fmt.Errorf("Get https://host: certificate fingerprint mismatch! expected: old got: new"))
	if !strings.Contains(msg, "Worker cannot verify coordinator identity") {
		t.Fatalf("unexpected message: %s", msg)
	}
}

func hasWorkerString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestLoadPolicyRestoresBootstrapPermission(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("LOCALAPPDATA", tmpDir)
	t.Setenv("APPDATA", tmpDir)

	if err := WritePolicy(Policy{
		AllowedRepos:   []string{"https://github.com/example/repo.git"},
		AllowPush:      true,
		AllowBootstrap: true,
		Labels:         []string{"trusted"},
		Capabilities:   []string{"go"},
	}); err != nil {
		t.Fatalf("WritePolicy failed: %v", err)
	}

	w := New("TestNode", "./tmp-ws", true)
	if !w.allowBootstrap {
		t.Fatal("expected allowBootstrap to be restored from policy")
	}
	if !w.allowPush {
		t.Fatal("expected allowPush to be restored from policy")
	}
	if !w.allowedRepos["https://github.com/example/repo.git"] {
		t.Fatal("expected allowed repo to be restored from policy")
	}
}

func TestRunAutoValidationRunsGoTests(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not available")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/validation\n\ngo 1.23\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main_test.go"), []byte(`package validation

import "testing"

func TestValidation(t *testing.T) {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	results := runAutoValidation(context.Background(), root, []string{"go"})
	if len(results) != 1 {
		t.Fatalf("expected one validation result, got %#v", results)
	}
	if results[0].Name != "Go tests" || results[0].Status != "COMPLETED" {
		t.Fatalf("unexpected validation result: %#v", results[0])
	}
}
