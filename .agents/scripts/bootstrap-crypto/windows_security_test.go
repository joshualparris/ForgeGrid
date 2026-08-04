//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forgegrid/internal/agentbridge"
)

func TestProtectedBootstrapErrors(t *testing.T) {
	binPath := buildBinary(t)
	tmpDir := t.TempDir()
	
	// Create invalid private blob
	privBlobPath := filepath.Join(tmpDir, "private.blob")
	if err := os.WriteFile(privBlobPath, []byte("invalid-blob"), 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	
	bundlePath := filepath.Join(tmpDir, "bundle.json")
	if err := os.WriteFile(bundlePath, []byte("{}"), 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cmd := exec.Command(binPath, "decrypt-protected-and-apply", privBlobPath, bundlePath, "dummy-forgegrid.exe")
	err := cmd.Run()
	if err == nil {
		t.Fatalf("Expected decrypt-protected-and-apply to fail on invalid input, but it succeeded")
	}
	if exitError, ok := err.(*exec.ExitError); !ok {
		t.Fatalf("Expected ExitError, got %v", err)
	} else if exitError.ExitCode() == 0 {
		t.Fatalf("Expected non-zero exit code, got 0")
	}
}

func TestWindowsSecurity(t *testing.T) {
	binPath := buildBinary(t)
	tmpDir := t.TempDir()
	
	realForgeGridExe := filepath.Join(tmpDir, "forgegrid.exe")
	if out, err := exec.Command("go", "build", "-o", realForgeGridExe, "forgegrid").CombinedOutput(); err != nil {
		t.Fatalf("Failed to build real forgegrid: %v\n%s", err, string(out))
	}

	privBlobPath := filepath.Join(tmpDir, "private.blob")
	pubPath := filepath.Join(tmpDir, "pub.pem")

	// 1. Plaintext private-key files are never created (it generates protected)
	cmdGen := exec.Command(binPath, "generate-protected", privBlobPath, pubPath)
	if out, err := cmdGen.CombinedOutput(); err != nil {
		t.Fatalf("generate-protected failed: %v\nOutput: %s", err, string(out))
	}

	// Verify private.blob is protected
	blobBytes, err := os.ReadFile(privBlobPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if bytes.Contains(blobBytes, []byte("RSA PRIVATE KEY")) {
		t.Fatalf("private.blob contains plaintext PEM header")
	}

	// 2. Encrypt a dummy bundle
	plainIn := filepath.Join(tmpDir, "plain.json")
	b := agentbridge.BundleData{
		SchemaVersion: "1",
		AgentName:     "windows-test",
		Token:         "dummy-test-token-not-valid",
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
	cmdEnc := exec.Command(binPath, "encrypt", pubPath, plainIn, hybridOut)
	if out, err := cmdEnc.CombinedOutput(); err != nil {
		t.Fatalf("encrypt failed: %v\nOutput: %s", err, string(out))
	}

	// Set up local app data
	localAppData := filepath.Join(tmpDir, "LocalAppData_Security")
	os.Setenv("LOCALAPPDATA", localAppData)
	defer os.Unsetenv("LOCALAPPDATA")

	// 3. Decrypt and apply protected bundle
	cmdDec := exec.Command(binPath, "decrypt-protected-and-apply", privBlobPath, hybridOut, realForgeGridExe)
	cmdDec.Env = append(os.Environ(), "LOCALAPPDATA="+localAppData)
	out, err := cmdDec.CombinedOutput()
	if err != nil {
		t.Fatalf("decrypt-protected-and-apply failed: %v\nOutput: %s", err, string(out))
	}

	// Check token leakage
	if bytes.Contains(out, []byte("dummy-test-token-not-valid")) {
		t.Fatalf("Token leaked in output!")
	}

	// Check temporary token files
	bootstrapDir := filepath.Join(localAppData, "ForgeGrid", "bootstrap")
	entries, _ := os.ReadDir(bootstrapDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "tmp_token_") {
			t.Fatalf("Temporary token file was created and not cleaned up: %s", e.Name())
		}
	}

	// Final config matching
	configPath := filepath.Join(localAppData, "ForgeGrid", "agentclient.json")
	cfgBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Config file not created")
	}
	var cfg agentbridge.ClientConfig
	if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
		t.Fatalf("Config unmarshal failed")
	}
	if cfg.Token != "dummy-test-token-not-valid" {
		t.Fatalf("Token does not match")
	}

	// 4. Verify ACL excludes unrelated user (we can't easily query ACL, but icacls helps)
	verifyCmd := exec.Command("icacls", configPath)
	aclOut, _ := verifyCmd.CombinedOutput()
	if strings.Contains(string(aclOut), "BUILTIN\\Users") {
		t.Fatalf("Config ACL includes BUILTIN\\Users, which should be excluded")
	}
	verifyDirCmd := exec.Command("icacls", bootstrapDir)
	aclDirOut, _ := verifyDirCmd.CombinedOutput()
	if strings.Contains(string(aclDirOut), "BUILTIN\\Users") {
		t.Fatalf("Bootstrap dir ACL includes BUILTIN\\Users, which should be excluded")
	}

	// The unlock error was tested in main_test.go
	// LockFileEx unexpected error test
	t.Run("UnexpectedLockError", func(t *testing.T) {
		f, err := os.CreateTemp("", "fake")
		if err != nil {
			t.Fatalf("CreateTemp failed: %v", err)
		}
		path := f.Name()
		// Close the file so the handle is invalid
		f.Close()
		os.Remove(path)

		err = lockFile(f)
		if err == nil {
			t.Fatalf("Expected lockFile to fail on invalid handle, but it succeeded")
		}
		if !strings.Contains(err.Error(), "unexpected lock error") {
			t.Fatalf("Expected 'unexpected lock error', got: %v", err)
		}
	})
}
