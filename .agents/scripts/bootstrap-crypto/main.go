package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Bundle struct {
	SchemaVersion string `json:"schema_version"`
	AgentName     string `json:"agent_name"`
	Token         string `json:"token"`
	RelayURL      string `json:"relay_url"`
	Fingerprint   string `json:"fingerprint"`
	Issued        string `json:"issued"`
	Expiry        string `json:"expiry"`
	BootstrapID   string `json:"bootstrap_id"`
}

type HybridBundle struct {
	EncryptedKey string `json:"encrypted_key"` // base64
	Nonce        string `json:"nonce"`         // base64
	Ciphertext   string `json:"ciphertext"`    // base64
}

type ClientConfig struct {
	Name        string `json:"name"`
	Token       string `json:"token"`
	URL         string `json:"url"`
	Fingerprint string `json:"fingerprint"`
}

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
		decryptAndApply(os.Args[2], os.Args[3], os.Args[4])
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

	aesKey := make([]byte, 32)
	if _, err := rand.Read(aesKey); err != nil {
		fmt.Printf("Failed generating AES key: %v\n", err)
		os.Exit(1)
	}

	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		fmt.Printf("Failed generating nonce: %v\n", err)
		os.Exit(1)
	}

	blockAES, err := aes.NewCipher(aesKey)
	if err != nil {
		fmt.Printf("AES cipher error: %v\n", err)
		os.Exit(1)
	}
	aesgcm, err := cipher.NewGCM(blockAES)
	if err != nil {
		fmt.Printf("GCM cipher error: %v\n", err)
		os.Exit(1)
	}
	ciphertext := aesgcm.Seal(nil, nonce, plaintext, nil)

	encryptedKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pubKey, aesKey, nil)
	if err != nil {
		fmt.Printf("RSA encryption error: %v\n", err)
		os.Exit(1)
	}

	hybrid := HybridBundle{
		EncryptedKey: base64.StdEncoding.EncodeToString(encryptedKey),
		Nonce:        base64.StdEncoding.EncodeToString(nonce),
		Ciphertext:   base64.StdEncoding.EncodeToString(ciphertext),
	}

	outBytes, err := json.MarshalIndent(hybrid, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling hybrid bundle: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(hybridOutPath, outBytes, 0600); err != nil {
		fmt.Printf("Error writing hybrid bundle: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Encrypted bundle created successfully.")
}

func decryptAndApply(privPath, bundlePath, forgegridExe string) {
	// 1. Decrypt
	privPEM, err := os.ReadFile(privPath)
	if err != nil {
		fmt.Printf("Error reading private key: %v\n", err)
		os.Exit(1)
	}
	block, _ := pem.Decode(privPEM)
	if block == nil {
		fmt.Println("Failed to parse PEM block")
		os.Exit(1)
	}
	privKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		fmt.Printf("Error parsing private key: %v\n", err)
		os.Exit(1)
	}

	bundleBytes, err := os.ReadFile(bundlePath)
	if err != nil {
		fmt.Printf("Error reading encrypted bundle: %v\n", err)
		os.Exit(1)
	}

	var hybrid HybridBundle
	if err := json.Unmarshal(bundleBytes, &hybrid); err != nil {
		fmt.Printf("Error parsing hybrid bundle JSON: %v\n", err)
		os.Exit(1)
	}

	encryptedKey, err := base64.StdEncoding.DecodeString(hybrid.EncryptedKey)
	if err != nil {
		fmt.Println("Invalid encrypted_key base64")
		os.Exit(1)
	}
	nonce, err := base64.StdEncoding.DecodeString(hybrid.Nonce)
	if err != nil {
		fmt.Println("Invalid nonce base64")
		os.Exit(1)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(hybrid.Ciphertext)
	if err != nil {
		fmt.Println("Invalid ciphertext base64")
		os.Exit(1)
	}

	aesKey, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privKey, encryptedKey, nil)
	if err != nil {
		fmt.Printf("Error decrypting AES key: %v\n", err)
		os.Exit(1)
	}

	blockAES, err := aes.NewCipher(aesKey)
	if err != nil {
		fmt.Printf("AES cipher error: %v\n", err)
		os.Exit(1)
	}
	aesgcm, err := cipher.NewGCM(blockAES)
	if err != nil {
		fmt.Printf("GCM cipher error: %v\n", err)
		os.Exit(1)
	}
	plaintext, err := aesgcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		fmt.Printf("Error decrypting ciphertext: %v\n", err)
		os.Exit(1)
	}

	var b Bundle
	dec := json.NewDecoder(bytes.NewReader(plaintext))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		fmt.Printf("Error parsing bundle JSON: %v\n", err)
		os.Exit(1)
	}

	// 2. Validate
	if b.SchemaVersion != "1" {
		fmt.Printf("Invalid schema version: %s\n", b.SchemaVersion)
		os.Exit(1)
	}
	if b.AgentName != "windows-test" {
		fmt.Printf("Invalid agent name: %s\n", b.AgentName)
		os.Exit(1)
	}
	if b.Token == "" || b.RelayURL == "" || b.Fingerprint == "" || b.BootstrapID == "" {
		fmt.Println("Bundle is missing required fields")
		os.Exit(1)
	}
	if len(b.Fingerprint) != 64 {
		fmt.Printf("Invalid fingerprint length: %d\n", len(b.Fingerprint))
		os.Exit(1)
	}

	issued, err := time.Parse(time.RFC3339, b.Issued)
	if err != nil {
		fmt.Printf("Invalid issued format: %v\n", err)
		os.Exit(1)
	}
	expiry, err := time.Parse(time.RFC3339, b.Expiry)
	if err != nil {
		fmt.Printf("Invalid expiry format: %v\n", err)
		os.Exit(1)
	}
	
	now := time.Now()
	if now.After(expiry) {
		fmt.Println("Bundle has expired")
		os.Exit(1)
	}
	if expiry.Sub(issued) > 10*time.Minute {
		fmt.Println("Bundle lifetime exceeds 10 minutes")
		os.Exit(1)
	}

	// 3. Check and apply Bootstrap ID
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	bootstrapDir := filepath.Join(localAppData, "ForgeGrid", "bootstrap")
	if err := os.MkdirAll(bootstrapDir, 0700); err != nil {
		fmt.Printf("Failed to create bootstrap dir: %v\n", err)
		os.Exit(1)
	}

	// Safe filename constraint
	if strings.ContainsAny(b.BootstrapID, `/\.:`) {
		fmt.Println("Invalid characters in BootstrapID")
		os.Exit(1)
	}
	idPath := filepath.Join(bootstrapDir, b.BootstrapID)
	if _, err := os.Stat(idPath); err == nil {
		fmt.Println("BootstrapID already consumed")
		os.Exit(1)
	}

	if err := os.WriteFile(idPath, []byte(now.Format(time.RFC3339)), 0600); err != nil {
		fmt.Printf("Failed to write bootstrap ID: %v\n", err)
		os.Exit(1)
	}

	// 4. Pass token to ForgeGrid configure-client
	tokenTmpPath := filepath.Join(bootstrapDir, "tmp_token_"+b.BootstrapID)
	if err := os.WriteFile(tokenTmpPath, []byte(b.Token), 0600); err != nil {
		fmt.Printf("Failed to write temporary token: %v\n", err)
		os.Exit(1)
	}

	cmd := exec.Command(forgegridExe, "agent-bridge", "configure-client",
		"--name", b.AgentName,
		"--url", b.RelayURL,
		"--fingerprint", b.Fingerprint,
		"--token-file", tokenTmpPath)
	
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Remove(tokenTmpPath) // Cleanup on error
		fmt.Printf("configure-client failed: %v\n", err)
		os.Exit(1)
	}

	// 5. Verify token file was deleted by configure-client
	if _, err := os.Stat(tokenTmpPath); !os.IsNotExist(err) {
		fmt.Printf("Warning: configure-client did not delete token file %s, deleting now.\n", tokenTmpPath)
		os.Remove(tokenTmpPath)
	}

	// 6. Cleanup bundle and securely zero it out
	if err := secureDelete(bundlePath); err != nil {
		fmt.Printf("Warning: Failed to securely delete bundle: %v\n", err)
	}
	fmt.Println("Successfully decrypted bundle and configured client.")
}

func secureDelete(path string) error {
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
