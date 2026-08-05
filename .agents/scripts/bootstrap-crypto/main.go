package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"forgegrid/internal/agentbridge"
)

func getConfigPath() string {
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
		}
		return filepath.Join(localAppData, "ForgeGrid", "agentclient.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "forgegrid", "agentclient.json")
}

func environmentWithLocalAppData(environ []string, localAppData string) []string {
	result := make([]string, 0, len(environ)+1)
	for _, entry := range environ {
		name, _, found := strings.Cut(entry, "=")
		if found && strings.EqualFold(name, "LOCALAPPDATA") {
			continue
		}
		result = append(result, entry)
	}
	return append(result, "LOCALAPPDATA="+localAppData)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("  bootstrap-crypto generate <private-key-out> <public-key-out>")
		fmt.Println("  bootstrap-crypto generate-protected <private-blob-out> <public-key-out>")
		fmt.Println("  bootstrap-crypto encrypt <public-key-in> <json-in> <signer-private-key> <hybrid-out>")
		fmt.Println("  bootstrap-crypto decrypt-and-apply <private-key-in> <hybrid-in> <signer-public-key> <expected-signer-fingerprint> <forgegrid-exe-path>")
		fmt.Println("  bootstrap-crypto decrypt-protected-and-apply <private-blob-in> <hybrid-in> <signer-public-key> <expected-signer-fingerprint> <forgegrid-exe-path>")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "generate-protected":
		if len(os.Args) != 4 {
			fmt.Println("Usage: generate-protected <priv_blob_out> <pub_out>")
			os.Exit(1)
		}
		generateProtectedKeys(os.Args[2], os.Args[3])
	case "decrypt-protected-and-apply":
		if len(os.Args) != 7 {
			fmt.Println("Usage: decrypt-protected-and-apply <priv_blob> <bundle> <signer_pub_in> <expected_fingerprint> <forgegrid_exe>")
			os.Exit(1)
		}
		if err := decryptProtectedAndApply(os.Args[2], os.Args[3], os.Args[4], os.Args[5], os.Args[6]); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	case "generate":
		if len(os.Args) != 4 {
			fmt.Println("Usage: generate <priv_out> <pub_out>")
			os.Exit(1)
		}
		generateKeys(os.Args[2], os.Args[3])
	case "encrypt":
		if len(os.Args) != 6 {
			fmt.Println("Usage: encrypt <pub_in> <plain_json_in> <signer_priv_in> <hybrid_out>")
			os.Exit(1)
		}
		encryptBundle(os.Args[2], os.Args[3], os.Args[4], os.Args[5])
	case "decrypt-and-apply":
		if len(os.Args) != 7 {
			fmt.Println("Usage: decrypt-and-apply <priv_in> <bundle_in> <signer_pub_in> <expected_fingerprint> <forgegrid_exe>")
			os.Exit(1)
		}
		if err := decryptAndApply(os.Args[2], os.Args[3], os.Args[4], os.Args[5], os.Args[6]); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Println("Unknown command")
		os.Exit(1)
	}
}

func generateProtectedKeys(privBlobPath, pubPath string) {
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

	protectedPEM, err := protectData(privPEM)
	if err != nil {
		fmt.Printf("Error protecting private key: %v\n", err)
		os.Exit(1)
	}

	if err := writeSecureBlob(privBlobPath, protectedPEM); err != nil {
		fmt.Printf("Error writing protected private key: %v\n", err)
		os.Exit(1)
	}

	for i := range privBytes {
		privBytes[i] = 0
	}
	for i := range privPEM {
		privPEM[i] = 0
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

func encryptBundle(pubPath, plainInPath, signerPrivPath, hybridOutPath string) {
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

	signerPrivPEM, err := os.ReadFile(signerPrivPath)
	if err != nil {
		fmt.Printf("Error reading signer private key: %v\n", err)
		os.Exit(1)
	}
	signerBlock, _ := pem.Decode(signerPrivPEM)
	if signerBlock == nil {
		fmt.Println("Failed to parse signer private PEM block")
		os.Exit(1)
	}
	if len(signerBlock.Bytes) != ed25519.PrivateKeySize {
		fmt.Println("Invalid Ed25519 private key size")
		os.Exit(1)
	}
	signPriv := ed25519.PrivateKey(signerBlock.Bytes)

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

	eb, err := agentbridge.GenerateSignedBootstrapBundle(pubKey, signPriv, bd)
	if err != nil {
		fmt.Printf("GenerateSignedBootstrapBundle failed: %v\n", err)
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
	var extra interface{}
	if err := dec.Decode(&extra); err != io.EOF {
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
	return writeSecureBlob(w.Path, b)
}

func (w *WindowsReplayStore) update(fn func(map[string]ReplayState) error) (err error) {
	f, openErr := os.OpenFile(w.LockFile, os.O_RDWR|os.O_CREATE, 0600)
	if openErr != nil {
		return fmt.Errorf("failed to open lock file: %v", openErr)
	}
	defer f.Close()

	if lockErr := lockFile(f); lockErr != nil {
		return lockErr
	}

	defer func() {
		if unlockErr := unlockFile(f); unlockErr != nil {
			if err != nil {
				err = fmt.Errorf("%w (also failed to unlock: %v)", err, unlockErr)
			} else {
				err = fmt.Errorf("failed to unlock: %w", unlockErr)
			}
		}
	}()

	m, loadErr := w.load()
	if loadErr != nil {
		return loadErr
	}
	if fnErr := fn(m); fnErr != nil {
		return fnErr
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

func decryptProtectedAndApply(privBlobPath, bundlePath, signerPubPath, expectedSignerFingerprint, forgegridExe string) error {
	// 1. Unprotect and Decrypt
	protectedBytes, err := os.ReadFile(privBlobPath)
	if err != nil {
		return fmt.Errorf("error reading private blob: %w", err)
	}

	privPEM, err := unprotectData(protectedBytes)
	if err != nil {
		return fmt.Errorf("error unprotecting private key: %w", err)
	}

	block, _ := pem.Decode(privPEM)
	if block == nil {
		return fmt.Errorf("failed to parse PEM block")
	}
	privKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("error parsing private key: %w", err)
	}

	// Clear privPEM
	for i := range privPEM {
		privPEM[i] = 0
	}

	signerPubPEM, err := os.ReadFile(signerPubPath)
	if err != nil {
		return fmt.Errorf("error reading signer public key: %w", err)
	}
	signerPubBlock, _ := pem.Decode(signerPubPEM)
	if signerPubBlock == nil {
		return fmt.Errorf("failed to parse signer public PEM block")
	}
	if len(signerPubBlock.Bytes) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid Ed25519 public key size")
	}
	signPub := ed25519.PublicKey(signerPubBlock.Bytes)

	bundleBytes, err := os.ReadFile(bundlePath)
	if err != nil {
		return fmt.Errorf("error reading encrypted bundle: %w", err)
	}

	var eb agentbridge.EncryptedBundle
	if err := json.Unmarshal(bundleBytes, &eb); err != nil {
		return fmt.Errorf("error parsing encrypted bundle JSON: %w", err)
	}

	if eb.SignerFingerprint != expectedSignerFingerprint {
		return fmt.Errorf("signer fingerprint mismatch")
	}

	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	bootstrapDir := filepath.Join(localAppData, "ForgeGrid", "bootstrap")
	if err := secureBootstrapDirectory(bootstrapDir); err != nil {
		return fmt.Errorf("failed to secure bootstrap dir: %w", err)
	}

	rs := &WindowsReplayStore{
		Path:     filepath.Join(bootstrapDir, "replay-state.json"),
		LockFile: filepath.Join(bootstrapDir, "replay-state.lock"),
	}

	hostname, _ := os.Hostname()
	bd, err := agentbridge.ValidateBootstrapBundle(&eb, privKey, signPub, hostname, rs)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	success := false
	defer func() {
		if !success {
			rs.Release(eb.BootstrapID)
		}
	}()

	// 4. Pass token to ForgeGrid configure-client via stdin
	cmd := exec.Command(forgegridExe, "agent-bridge", "configure-client",
		"--name", bd.AgentName,
		"--url", bd.RelayURL,
		"--fingerprint", bd.Fingerprint,
		"--token-stdin")
	cmd.Env = environmentWithLocalAppData(os.Environ(), localAppData)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("configure-client start failed: %w", err)
	}

	_, err = stdin.Write([]byte(bd.Token))
	stdin.Close() // Must close to signal EOF
	if err != nil {
		return fmt.Errorf("failed to write token to stdin: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("configure-client failed: %w", err)
	}

	// 6. Verify config exists and matches
	configPath := getConfigPath()
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
	var extra interface{}
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("config error: trailing data")
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
	if err := bestEffortDelete(privBlobPath); err != nil {
		fmt.Printf("Warning: Failed to best-effort delete private blob: %v\n", err)
	}

	fmt.Println("Successfully decrypted bundle and configured client.")
	return nil
}

func decryptAndApply(privPath, bundlePath, signerPubPath, expectedSignerFingerprint, forgegridExe string) error {
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

	signerPubPEM, err := os.ReadFile(signerPubPath)
	if err != nil {
		return fmt.Errorf("error reading signer public key: %w", err)
	}
	signerPubBlock, _ := pem.Decode(signerPubPEM)
	if signerPubBlock == nil {
		return fmt.Errorf("failed to parse signer public PEM block")
	}
	if len(signerPubBlock.Bytes) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid Ed25519 public key size")
	}
	signPub := ed25519.PublicKey(signerPubBlock.Bytes)

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
	if err := secureBootstrapDirectory(bootstrapDir); err != nil {
		return fmt.Errorf("failed to secure bootstrap dir: %w", err)
	}

	rs := &WindowsReplayStore{
		Path:     filepath.Join(bootstrapDir, "replay-state.json"),
		LockFile: filepath.Join(bootstrapDir, "replay-state.lock"),
	}

	bd, err := agentbridge.ValidateBootstrapBundle(&eb, privKey, signPub, "windows-test", rs)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	success := false
	defer func() {
		if !success {
			rs.Release(eb.BootstrapID)
		}
	}()

	// 4. Pass token to ForgeGrid configure-client via stdin
	cmd := exec.Command(forgegridExe, "agent-bridge", "configure-client",
		"--name", bd.AgentName,
		"--url", bd.RelayURL,
		"--fingerprint", bd.Fingerprint,
		"--token-stdin")
	cmd.Env = environmentWithLocalAppData(os.Environ(), localAppData)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("configure-client start failed: %w", err)
	}

	_, err = stdin.Write([]byte(bd.Token))
	stdin.Close() // Must close to signal EOF
	if err != nil {
		return fmt.Errorf("failed to write token to stdin: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("configure-client failed: %w", err)
	}

	// 6. Verify config exists and matches
	configPath := getConfigPath()
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
	var extra interface{}
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("config error: trailing data")
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
