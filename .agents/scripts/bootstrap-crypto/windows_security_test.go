//go:build windows

package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
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

	signerPubPath := filepath.Join(tmpDir, "signer_pub.pem")
	if err := os.WriteFile(signerPubPath, []byte("invalid-signer"), 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cmd := exec.Command(binPath, "decrypt-protected-and-apply", privBlobPath, bundlePath, signerPubPath, "dummy-fingerprint", "dummy-forgegrid.exe")
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

	hostname, err := os.Hostname()
	if err != nil {
		t.Fatalf("os.Hostname failed: %v", err)
	}

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

	// Preserve a retry copy of private.blob for the second test
	backupPrivBlobPath := filepath.Join(tmpDir, "private.blob.backup")
	if err := os.WriteFile(backupPrivBlobPath, blobBytes, 0600); err != nil {
		t.Fatalf("Failed to backup private blob: %v", err)
	}

	// Create signer keys
	signPub, signPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Ed25519 GenerateKey failed: %v", err)
	}
	signerPubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: signPub})
	signerPubPath := filepath.Join(tmpDir, "signer_pub.pem")
	if err := os.WriteFile(signerPubPath, signerPubPEM, 0644); err != nil {
		t.Fatalf("Failed to write signer public key: %v", err)
	}
	signerPrivPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: signPriv})
	signerPrivPath := filepath.Join(tmpDir, "signer_priv.pem")
	if err := os.WriteFile(signerPrivPath, signerPrivPEM, 0600); err != nil {
		t.Fatalf("Failed to write signer private key: %v", err)
	}

	// 2. Encrypt a dummy bundle
	plainIn := filepath.Join(tmpDir, "plain.json")
	firstBootstrapID := "boot-" + time.Now().UTC().Format("150405999999")
	b := agentbridge.BundleData{
		SchemaVersion: "1",
		AgentName:     hostname,
		Token:         "dummy-test-token-not-valid",
		RelayURL:      "https://127.0.0.1:9091",
		Fingerprint:   strings.Repeat("a", 64),
		Issued:        time.Now().UTC().Format(time.RFC3339),
		Expiry:        time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339),
		BootstrapID:   firstBootstrapID,
	}
	bBytes, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if err := os.WriteFile(plainIn, bBytes, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	hybridOut := filepath.Join(tmpDir, "hybrid.json")
	cmdEnc := exec.Command(binPath, "encrypt", pubPath, plainIn, signerPrivPath, hybridOut)
	if out, err := cmdEnc.CombinedOutput(); err != nil {
		t.Fatalf("encrypt failed: %v\nOutput: %s", err, string(out))
	}

	// Set up local app data
	localAppData := filepath.Join(tmpDir, "LocalAppData_Security")
	if err := os.Setenv("LOCALAPPDATA", localAppData); err != nil {
		t.Fatalf("Failed to set LOCALAPPDATA: %v", err)
	}
	defer os.Unsetenv("LOCALAPPDATA")

	signPubHash := sha256.Sum256(signPub)
	expectedFingerprint := hex.EncodeToString(signPubHash[:])

	// 3. Decrypt and apply protected bundle
	cmdDec := exec.Command(binPath, "decrypt-protected-and-apply", privBlobPath, hybridOut, signerPubPath, expectedFingerprint, realForgeGridExe)
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
	entries, err := os.ReadDir(bootstrapDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "tmp_token_") {
			t.Fatalf("Temporary token file was created and not cleaned up: %s", e.Name())
		}
	}

	// Final config matching
	configPath := filepath.Join(localAppData, "ForgeGrid", "agentclient.json")
	cfgBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Config file not created: %v", err)
	}
	var cfg agentbridge.ClientConfig
	if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
		t.Fatalf("Config unmarshal failed: %v", err)
	}
	if cfg.Token != "dummy-test-token-not-valid" {
		t.Fatalf("Token does not match")
	}

	// 4. Verify ACL excludes unrelated user (we can't easily query ACL, but icacls helps)
	verifyCmd := exec.Command("icacls", configPath)
	aclOut, err := verifyCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("icacls verify failed: %v", err)
	}
	if strings.Contains(string(aclOut), "BUILTIN\\Users") {
		t.Fatalf("Config ACL includes BUILTIN\\Users, which should be excluded")
	}
	verifyDirCmd := exec.Command("icacls", bootstrapDir)
	aclDirOut, err := verifyDirCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("icacls verify dir failed: %v", err)
	}
	if strings.Contains(string(aclDirOut), "BUILTIN\\Users") {
		t.Fatalf("Bootstrap dir ACL includes BUILTIN\\Users, which should be excluded")
	}

	// 5. TestConfigReplacement: Write a different valid config to the same path
	secondBootstrapID := "boot-second-" + time.Now().UTC().Format("150405999999")
	b2 := agentbridge.BundleData{
		SchemaVersion: "1",
		AgentName:     hostname,
		Token:         "second-token-valid",
		RelayURL:      "https://127.0.0.1:9091",
		Fingerprint:   strings.Repeat("a", 64),
		Issued:        time.Now().UTC().Format(time.RFC3339),
		Expiry:        time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339),
		BootstrapID:   secondBootstrapID,
	}
	bBytes2, err := json.Marshal(b2)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	plainIn2 := filepath.Join(tmpDir, "plain2.json")
	if err := os.WriteFile(plainIn2, bBytes2, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	hybridOut2 := filepath.Join(tmpDir, "hybrid2.json")
	cmdEnc2 := exec.Command(binPath, "encrypt", pubPath, plainIn2, signerPrivPath, hybridOut2)
	if out, err := cmdEnc2.CombinedOutput(); err != nil {
		t.Fatalf("encrypt second bundle failed: %v\nOutput: %s", err, string(out))
	}

	// Restore private.blob from backup
	backupBytes, err := os.ReadFile(backupPrivBlobPath)
	if err != nil {
		t.Fatalf("ReadFile backup failed: %v", err)
	}
	if err := os.WriteFile(privBlobPath, backupBytes, 0600); err != nil {
		t.Fatalf("Failed to restore private.blob: %v", err)
	}

	// Decrypt and apply second time (proving MoveFileEx replacement works)
	cmdDec2 := exec.Command(binPath, "decrypt-protected-and-apply", privBlobPath, hybridOut2, signerPubPath, expectedFingerprint, realForgeGridExe)
	cmdDec2.Env = append(os.Environ(), "LOCALAPPDATA="+localAppData)
	if out, err := cmdDec2.CombinedOutput(); err != nil {
		t.Fatalf("decrypt-protected-and-apply second time failed: %v\nOutput: %s", err, string(out))
	}

	// Verify the second content replaced the first
	cfgBytes2, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Config file missing after replacement")
	}
	var cfg2 agentbridge.ClientConfig
	if err := json.Unmarshal(cfgBytes2, &cfg2); err != nil {
		t.Fatalf("Config unmarshal failed: %v", err)
	}
	if cfg2.Token != "second-token-valid" {
		t.Fatalf("Token does not match after replacement, got %s", cfg2.Token)
	}
	if cfg2.Token == "dummy-test-token-not-valid" {
		t.Fatalf("First token is still present")
	}

	// Verify final ACL still excludes ordinary users
	verifyCmd2 := exec.Command("icacls", configPath)
	aclOut2, err := verifyCmd2.CombinedOutput()
	if err != nil {
		t.Fatalf("icacls verify 2 failed: %v", err)
	}
	if strings.Contains(string(aclOut2), "BUILTIN\\Users") {
		t.Fatalf("Config ACL after replacement includes BUILTIN\\Users")
	}

	// Check that no tmp file remains
	entries2, err := os.ReadDir(filepath.Dir(configPath))
	if err != nil {
		t.Fatalf("ReadDir 2 failed: %v", err)
	}
	for _, e := range entries2 {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("Temporary config file was not cleaned up: %s", e.Name())
		}
	}

	// 6. TestReplayStoreReplacement
	// Reserve and commit A
	rsPath := filepath.Join(bootstrapDir, "replay-state.json")
	lockPath := filepath.Join(bootstrapDir, "replay-state.lock")
	rs := &WindowsReplayStore{Path: rsPath, LockFile: lockPath}
	if err := rs.Reserve("boot-A"); err != nil {
		t.Fatalf("Reserve boot-A failed: %v", err)
	}
	if err := rs.Release("boot-A"); err != nil {
		t.Fatalf("Release boot-A failed: %v", err)
	}
	if err := rs.Reserve("boot-A"); err != nil {
		t.Fatalf("Reserve boot-A second time failed: %v", err)
	}
	if err := rs.Commit("boot-A"); err != nil {
		t.Fatalf("Commit boot-A failed: %v", err)
	}

	// Reopen store, confirm A is consumed
	rs2 := &WindowsReplayStore{Path: rsPath, LockFile: lockPath}
	if err := rs2.Reserve("boot-A"); err == nil || !strings.Contains(err.Error(), "already consumed") {
		t.Fatalf("Expected already consumed error for A, got %v", err)
	}
	if err := rs2.Reserve("boot-B"); err != nil {
		t.Fatalf("Reserve boot-B failed: %v", err)
	}
	if err := rs2.Commit("boot-B"); err != nil {
		t.Fatalf("Commit boot-B failed: %v", err)
	}

	// Confirm both survive restart, and that the original bundle IDs are also consumed.
	rs3 := &WindowsReplayStore{Path: rsPath, LockFile: lockPath}
	if err := rs3.Reserve("boot-A"); err == nil {
		t.Fatalf("A should be consumed")
	}
	if err := rs3.Reserve("boot-B"); err == nil {
		t.Fatalf("B should be consumed")
	}
	if err := rs3.Reserve(firstBootstrapID); err == nil {
		t.Fatalf("First bootstrap ID should be consumed")
	}
	if err := rs3.Reserve(secondBootstrapID); err == nil {
		t.Fatalf("Second bootstrap ID should be consumed")
	}

	// LockFileEx unexpected error test
	t.Run("UnexpectedLockError", func(t *testing.T) {
		f, err := os.CreateTemp("", "fake")
		if err != nil {
			t.Fatalf("CreateTemp failed: %v", err)
		}
		path := f.Name()
		// Close the file so the handle is invalid
		if err := f.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatalf("Remove failed: %v", err)
		}

		err = lockFile(f)
		if err == nil {
			t.Fatalf("Expected lockFile to fail on invalid handle, but it succeeded")
		}
		if !strings.Contains(err.Error(), "unexpected lock error") {
			t.Fatalf("Expected 'unexpected lock error', got: %v", err)
		}
	})
}
