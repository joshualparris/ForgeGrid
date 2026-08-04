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
)

func buildBinary(t *testing.T) string {
	binPath := filepath.Join(t.TempDir(), "bootstrap-crypto.exe")
	cmd := exec.Command("go", "build", "-o", binPath, "main.go")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, string(out))
	}
	return binPath
}

func TestBootstrapCrypto(t *testing.T) {
	binPath := buildBinary(t)
	tmpDir := t.TempDir()

	// Mock ForgeGrid EXE (just a dummy script that prints to stdout)
	// The dummy script will just echo its arguments so we can verify them, and it MUST delete the token-file!
	// Actually, wait, Windows batch doesn't easily delete dynamically passed args without parsing. 
	// Let's use a small Go program for the mock ForgeGrid!
	mockForgeGridSrc := filepath.Join(tmpDir, "mock_forgegrid.go")
	os.WriteFile(mockForgeGridSrc, []byte(`package main
import (
	"os"
)
func main() {
	// find --token-file argument and delete it
	for i, arg := range os.Args {
		if arg == "--token-file" && i+1 < len(os.Args) {
			os.Remove(os.Args[i+1])
		}
	}
}
`), 0600)
	mockForgeGridExe := filepath.Join(tmpDir, "mock_forgegrid.exe")
	exec.Command("go", "build", "-o", mockForgeGridExe, mockForgeGridSrc).Run()

	privPath := filepath.Join(tmpDir, "priv.pem")
	pubPath := filepath.Join(tmpDir, "pub.pem")

	// 1. Generate keys
	cmd := exec.Command(binPath, "generate", privPath, pubPath)
	if err := cmd.Run(); err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	createBundle := func(mod func(*Bundle)) string {
		b := Bundle{
			SchemaVersion: "1",
			AgentName:     "windows-test",
			Token:         "secret-token",
			RelayURL:      "https://127.0.0.1:9091",
			Fingerprint:   strings.Repeat("a", 64),
			Issued:        time.Now().Format(time.RFC3339),
			Expiry:        time.Now().Add(5 * time.Minute).Format(time.RFC3339),
			BootstrapID:   "boot-" + time.Now().Format("150405999999"),
		}
		if mod != nil {
			mod(&b)
		}
		plainIn := filepath.Join(tmpDir, "plain.json")
		bBytes, _ := json.Marshal(b)
		os.WriteFile(plainIn, bBytes, 0600)

		hybridOut := filepath.Join(tmpDir, "hybrid.json")
		cmd := exec.Command(binPath, "encrypt", pubPath, plainIn, hybridOut)
		if err := cmd.Run(); err != nil {
			t.Fatalf("encrypt failed: %v", err)
		}
		return hybridOut
	}

	// 2. Success Case
	t.Run("Success", func(t *testing.T) {
		bundlePath := createBundle(nil)
		cmd := exec.Command(binPath, "decrypt-and-apply", privPath, bundlePath, mockForgeGridExe)
		
		localAppData := filepath.Join(tmpDir, "LocalAppData")
		cmd.Env = append(os.Environ(), "LOCALAPPDATA="+localAppData)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("decrypt-and-apply failed: %v\nOutput: %s", err, string(out))
		}
		if bytes.Contains(out, []byte("secret-token")) {
			t.Fatalf("Token leaked in output!")
		}
		
		// Verify bundle deleted
		if _, err := os.Stat(bundlePath); !os.IsNotExist(err) {
			t.Fatalf("Bundle not deleted")
		}
	})

	// 3. Duplicate Bootstrap ID
	t.Run("DuplicateBootstrapID", func(t *testing.T) {
		bundlePath := createBundle(func(b *Bundle) { b.BootstrapID = "duplicate-id" })
		localAppData := filepath.Join(tmpDir, "LocalAppData2")
		
		// Run once (success)
		cmd := exec.Command(binPath, "decrypt-and-apply", privPath, bundlePath, mockForgeGridExe)
		cmd.Env = append(os.Environ(), "LOCALAPPDATA="+localAppData)
		if err := cmd.Run(); err != nil {
			t.Fatalf("First run failed: %v", err)
		}
		
		// Re-create same bundle path because it was deleted
		bundlePath2 := createBundle(func(b *Bundle) { b.BootstrapID = "duplicate-id" })
		
		// Run again (should fail)
		cmd2 := exec.Command(binPath, "decrypt-and-apply", privPath, bundlePath2, mockForgeGridExe)
		cmd2.Env = append(os.Environ(), "LOCALAPPDATA="+localAppData)
		if err := cmd2.Run(); err == nil {
			t.Fatalf("Expected failure on duplicate bootstrap ID")
		}
	})

	// 4. Expiry
	t.Run("Expiry", func(t *testing.T) {
		bundlePath := createBundle(func(b *Bundle) { 
			b.Expiry = time.Now().Add(-1 * time.Minute).Format(time.RFC3339) 
		})
		cmd := exec.Command(binPath, "decrypt-and-apply", privPath, bundlePath, mockForgeGridExe)
		if err := cmd.Run(); err == nil {
			t.Fatalf("Expected failure on expired bundle")
		}
	})

	// 5. Wrong Agent
	t.Run("WrongAgent", func(t *testing.T) {
		bundlePath := createBundle(func(b *Bundle) { b.AgentName = "linux-test" })
		cmd := exec.Command(binPath, "decrypt-and-apply", privPath, bundlePath, mockForgeGridExe)
		if err := cmd.Run(); err == nil {
			t.Fatalf("Expected failure on wrong agent")
		}
	})

	// 6. Unknown Fields
	t.Run("UnknownFields", func(t *testing.T) {
		// we have to inject an unknown field into the encrypted bundle? No, into the plaintext.
		// wait, createBundle uses strict struct. We'll manually encrypt it.
		plainIn := filepath.Join(tmpDir, "plain_unknown.json")
		os.WriteFile(plainIn, []byte(`{"schema_version":"1","agent_name":"windows-test","token":"sec","relay_url":"u","fingerprint":"`+strings.Repeat("a", 64)+`","issued":"2030-01-01T00:00:00Z","expiry":"2030-01-01T00:05:00Z","bootstrap_id":"b1","unknown":"bad"}`), 0600)
		
		hybridOut := filepath.Join(tmpDir, "hybrid_unknown.json")
		exec.Command(binPath, "encrypt", pubPath, plainIn, hybridOut).Run()

		cmd := exec.Command(binPath, "decrypt-and-apply", privPath, hybridOut, mockForgeGridExe)
		if err := cmd.Run(); err == nil {
			t.Fatalf("Expected failure on unknown field")
		}
	})

	// 7. Tampering
	t.Run("Tampering", func(t *testing.T) {
		bp := createBundle(nil)
		bBytes, _ := os.ReadFile(bp)
		// flip a byte in the JSON string
		bBytes[len(bBytes)/2] ^= 0xFF
		os.WriteFile(bp, bBytes, 0600)

		cmd := exec.Command(binPath, "decrypt-and-apply", privPath, bp, mockForgeGridExe)
		if err := cmd.Run(); err == nil {
			t.Fatalf("Expected failure on tampered ciphertext")
		}
	})

	// 8. Wrong Key
	t.Run("WrongKey", func(t *testing.T) {
		bundlePath := createBundle(nil)
		
		// generate another key pair
		privPath2 := filepath.Join(tmpDir, "priv2.pem")
		pubPath2 := filepath.Join(tmpDir, "pub2.pem")
		exec.Command(binPath, "generate", privPath2, pubPath2).Run()

		cmd := exec.Command(binPath, "decrypt-and-apply", privPath2, bundlePath, mockForgeGridExe)
		if err := cmd.Run(); err == nil {
			t.Fatalf("Expected failure on wrong key")
		}
	})
}
