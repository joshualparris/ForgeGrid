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

	// Create signer keys
	signPub, signPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Ed25519 GenerateKey failed: %v", err)
	}
	signerPubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: signPub})
	signerPubPath := filepath.Join(tmpDir, "signer_pub.pem")
	os.WriteFile(signerPubPath, signerPubPEM, 0644)
	signerPrivPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: signPriv})
	signerPrivPath := filepath.Join(tmpDir, "signer_priv.pem")
	os.WriteFile(signerPrivPath, signerPrivPEM, 0600)

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
	cmdEnc := exec.Command(binPath, "encrypt", pubPath, plainIn, signerPrivPath, hybridOut)
	if out, err := cmdEnc.CombinedOutput(); err != nil {
		t.Fatalf("encrypt failed: %v\nOutput: %s", err, string(out))
	}

	// Set up local app data
	localAppData := filepath.Join(tmpDir, "LocalAppData_Security")
	os.Setenv("LOCALAPPDATA", localAppData)
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

	// 5. TestConfigReplacement: Write a different valid config to the same path
	cfg.Token = "second-token-valid"
	bBytes2, _ := json.Marshal(cfg)
	plainIn2 := filepath.Join(tmpDir, "plain2.json")
	os.WriteFile(plainIn2, bBytes2, 0600)

	hybridOut2 := filepath.Join(tmpDir, "hybrid2.json")
	cmdEnc2 := exec.Command(binPath, "encrypt", pubPath, plainIn2, signerPrivPath, hybridOut2)
	cmdEnc2.Run()

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
	json.Unmarshal(cfgBytes2, &cfg2)
	if cfg2.Token != "second-token-valid" {
		t.Fatalf("Token does not match after replacement, got %s", cfg2.Token)
	}

	// Verify final ACL still excludes ordinary users
	verifyCmd2 := exec.Command("icacls", configPath)
	aclOut2, _ := verifyCmd2.CombinedOutput()
	if strings.Contains(string(aclOut2), "BUILTIN\\Users") {
		t.Fatalf("Config ACL after replacement includes BUILTIN\\Users")
	}

	// 6. TestReplayStoreReplacement
	// Reserve and commit A
	rsPath := filepath.Join(bootstrapDir, "replay-state.json")
	lockPath := filepath.Join(bootstrapDir, "replay-state.lock")
	rs := &WindowsReplayStore{Path: rsPath, LockFile: lockPath}
	rs.Reserve("boot-A")
	rs.Release("boot-A")
	rs.Reserve("boot-A")
	rs.Commit("boot-A")

	// Reopen store, confirm A is consumed
	rs2 := &WindowsReplayStore{Path: rsPath, LockFile: lockPath}
	if err := rs2.Reserve("boot-A"); err == nil || !strings.Contains(err.Error(), "already consumed") {
		t.Fatalf("Expected already consumed error for A, got %v", err)
	}
	rs2.Reserve("boot-B")
	rs2.Commit("boot-B")

	// Confirm both survive restart
	rs3 := &WindowsReplayStore{Path: rsPath, LockFile: lockPath}
	if err := rs3.Reserve("boot-A"); err == nil {
		t.Fatalf("A should be consumed")
	}
	if err := rs3.Reserve("boot-B"); err == nil {
		t.Fatalf("B should be consumed")
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
