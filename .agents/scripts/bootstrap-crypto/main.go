package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"forgegrid/internal/agentbridge"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: bootstrap-crypto <generate|encrypt|decrypt-and-apply> [args...]")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "generate":
		if len(os.Args) != 4 {
			fmt.Println("Usage: generate <priv_out> <pub_out>")
			os.Exit(1)
		}
		generateKeys(os.Args[2], os.Args[3])
	case "encrypt":
		if len(os.Args) != 5 {
			fmt.Println("Usage: encrypt <pub_in> <plain_json_in> <hybrid_out>")
			os.Exit(1)
		}
		encryptBundle(os.Args[2], os.Args[3], os.Args[4])
	case "decrypt-and-apply":
		if len(os.Args) != 5 {
			fmt.Println("Usage: decrypt-and-apply <priv_in> <bundle_in> <forgegrid_exe>")
			os.Exit(1)
		}
		if err := decryptAndApply(os.Args[2], os.Args[3], os.Args[4]); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Println("Unknown command")
		os.Exit(1)
	}
}

func generateKeys(privPath, pubPath string) {
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		fmt.Printf("Error generating key: %v\n", err)
		os.Exit(1)
	}

	privBytes := x509.MarshalPKCS1PrivateKey(key)
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privBytes,
	})
	if err := os.WriteFile(privPath, privPEM, 0600); err != nil {
		fmt.Printf("Error writing private key: %v\n", err)
		os.Exit(1)
	}

	pubBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		fmt.Printf("Error marshaling public key: %v\n", err)
		os.Exit(1)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})
	if err := os.WriteFile(pubPath, pubPEM, 0644); err != nil {
		fmt.Printf("Error writing public key: %v\n", err)
		os.Exit(1)
	}

	hash := sha256.Sum256(pubBytes)
	fmt.Printf("%x\n", hash)
}

func encryptBundle(pubPath, plainInPath, hybridOutPath string) {
	pubPEM, err := os.ReadFile(pubPath)
	if err != nil {
		fmt.Printf("Error reading public key: %v\n", err)
		os.Exit(1)
	}
	block, _ := pem.Decode(pubPEM)
	if block == nil {
		fmt.Println("Failed to parse PEM block")
		os.Exit(1)
	}
	pubKeyAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		fmt.Printf("Error parsing public key: %v\n", err)
		os.Exit(1)
	}
	pubKey, ok := pubKeyAny.(*rsa.PublicKey)
	if !ok {
		fmt.Println("Not an RSA public key")
		os.Exit(1)
	}

	plaintext, err := os.ReadFile(plainInPath)
	if err != nil {
		fmt.Printf("Error reading plaintext: %v\n", err)
		os.Exit(1)
	}

	var bd agentbridge.BundleData
	if err := json.Unmarshal(plaintext, &bd); err != nil {
		fmt.Printf("Error parsing plaintext JSON: %v\n", err)
		os.Exit(1)
	}

	eb, err := agentbridge.GenerateBootstrapBundle(pubKey, bd)
	if err != nil {
		fmt.Printf("GenerateBootstrapBundle failed: %v\n", err)
		os.Exit(1)
	}

	outBytes, err := json.MarshalIndent(eb, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling encrypted bundle: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(hybridOutPath, outBytes, 0600); err != nil {
		fmt.Printf("Error writing encrypted bundle: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Encrypted bundle created successfully.")
}

type WindowsReplayStore struct {
	Path     string
	LockFile string
}

type ReplayState struct {
	Status string    `json:"status"` // "reserved" or "consumed"
	Time   time.Time `json:"time"`
}

func (w *WindowsReplayStore) load() (map[string]ReplayState, error) {
	b, err := os.ReadFile(w.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]ReplayState), nil
		}
		return nil, err
	}
	var m map[string]ReplayState
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("corrupt replay store: %v", err)
	}
	var dummy interface{}
	if err := dec.Decode(&dummy); err == nil {
		return nil, fmt.Errorf("corrupt replay store: trailing data")
	}

	for id, state := range m {
		if id == "" {
			return nil, fmt.Errorf("corrupt replay store: empty ID")
		}
		if state.Status != "reserved" && state.Status != "consumed" {
			return nil, fmt.Errorf("corrupt replay store: invalid status %s", state.Status)
		}
		if state.Time.IsZero() {
			return nil, fmt.Errorf("corrupt replay store: zero timestamp")
		}
	}
	return m, nil
}

func (w *WindowsReplayStore) save(m map[string]ReplayState) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := w.Path + ".tmp"
	if err := os.WriteFile(tmpPath, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, w.Path)
}

func (w *WindowsReplayStore) update(fn func(map[string]ReplayState) error) error {
	f, err := os.OpenFile(w.LockFile, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("failed to open lock file: %v", err)
	}
	defer f.Close()

	if err := lockFile(f); err != nil {
		return err
	}
	defer unlockFile(f)

	m, err := w.load()
	if err != nil {
		return err
	}
	if err := fn(m); err != nil {
		return err
	}
	return w.save(m)
}

func (w *WindowsReplayStore) Reserve(id string) error {
	return w.update(func(m map[string]ReplayState) error {
		state, exists := m[id]
		if exists {
			if state.Status == "consumed" {
				return fmt.Errorf("already consumed")
			}
			if state.Status == "reserved" {
				if time.Since(state.Time) < 15*time.Minute {
					return fmt.Errorf("currently reserved")
				}
				// Stale reservation! We can reclaim it.
			}
		}
		m[id] = ReplayState{Status: "reserved", Time: time.Now()}
		return nil
	})
}

func (w *WindowsReplayStore) Commit(id string) error {
	return w.update(func(m map[string]ReplayState) error {
		state, exists := m[id]
		if !exists || state.Status != "reserved" {
			return fmt.Errorf("not reserved")
		}
		m[id] = ReplayState{Status: "consumed", Time: time.Now()}
		return nil
	})
}

func (w *WindowsReplayStore) Release(id string) error {
	return w.update(func(m map[string]ReplayState) error {
		state, exists := m[id]
		if exists && state.Status == "reserved" {
			delete(m, id)
		}
		return nil
	})
}

func decryptAndApply(privPath, bundlePath, forgegridExe string) error {
	// 1. Decrypt
	privPEM, err := os.ReadFile(privPath)
	if err != nil {
		return fmt.Errorf("error reading private key: %w", err)
	}
	block, _ := pem.Decode(privPEM)
	if block == nil {
		return fmt.Errorf("failed to parse PEM block")
	}
	privKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("error parsing private key: %w", err)
	}

	bundleBytes, err := os.ReadFile(bundlePath)
	if err != nil {
		return fmt.Errorf("error reading encrypted bundle: %w", err)
	}

	var eb agentbridge.EncryptedBundle
	if err := json.Unmarshal(bundleBytes, &eb); err != nil {
		return fmt.Errorf("error parsing encrypted bundle JSON: %w", err)
	}

	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	bootstrapDir := filepath.Join(localAppData, "ForgeGrid", "bootstrap")
	if err := os.MkdirAll(bootstrapDir, 0700); err != nil {
		return fmt.Errorf("failed to create bootstrap dir: %w", err)
	}

	rs := &WindowsReplayStore{
		Path:     filepath.Join(bootstrapDir, "replay-state.json"),
		LockFile: filepath.Join(bootstrapDir, "replay-state.lock"),
	}

	bd, err := agentbridge.ValidateBootstrapBundle(&eb, privKey, "windows-test", rs)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	success := false
	defer func() {
		if !success {
			rs.Release(eb.BootstrapID)
		}
	}()

	// 4. Pass token to ForgeGrid configure-client
	tokenTmpPath := filepath.Join(bootstrapDir, "tmp_token_"+eb.BootstrapID)
	if err := os.WriteFile(tokenTmpPath, []byte(bd.Token), 0600); err != nil {
		return fmt.Errorf("failed to write temporary token: %w", err)
	}
	defer os.Remove(tokenTmpPath)

	cmd := exec.Command(forgegridExe, "agent-bridge", "configure-client",
		"--name", bd.AgentName,
		"--url", bd.RelayURL,
		"--fingerprint", bd.Fingerprint,
		"--token-file", tokenTmpPath)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("configure-client failed: %w", err)
	}

	// 5. Verify token file was deleted by configure-client
	if _, err := os.Stat(tokenTmpPath); !os.IsNotExist(err) {
		return fmt.Errorf("token file was not deleted by configure-client")
	}

	// 6. Verify config exists and matches
	configPath := filepath.Join(localAppData, "ForgeGrid", "agentclient.json")
	cfgBytes, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("config file not created")
	}
	var cfg agentbridge.ClientConfig
	dec := json.NewDecoder(bytes.NewReader(cfgBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return fmt.Errorf("config file is malformed: %v", err)
	}
	var dummy interface{}
	if err := dec.Decode(&dummy); err == nil {
		return fmt.Errorf("config file has trailing data")
	}
	if cfg.Name != bd.AgentName || cfg.URL != bd.RelayURL || cfg.Fingerprint != bd.Fingerprint || cfg.Token != bd.Token {
		return fmt.Errorf("config file contents do not match validated bundle")
	}

	// 7. Commit reservation
	if err := rs.Commit(eb.BootstrapID); err != nil {
		return fmt.Errorf("failed to commit reservation: %w", err)
	}
	success = true

	// 8. Cleanup bundle and best-effort overwrite it
	if err := bestEffortDelete(bundlePath); err != nil {
		fmt.Printf("Warning: Failed to best-effort delete bundle: %v\n", err)
	}
	// Also zero out privPath since it's the unencrypted temp file
	if err := bestEffortDelete(privPath); err != nil {
		fmt.Printf("Warning: Failed to best-effort delete private key: %v\n", err)
	}

	fmt.Println("Successfully decrypted bundle and configured client.")
	return nil
}

func bestEffortDelete(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	zeroes := make([]byte, info.Size())
	if err := os.WriteFile(path, zeroes, 0600); err != nil {
		return err
	}
	return os.Remove(path)
}
