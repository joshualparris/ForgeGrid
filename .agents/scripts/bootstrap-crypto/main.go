package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
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

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: bootstrap-crypto <generate|decrypt> [args...]")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "generate":
		if len(os.Args) != 4 {
			fmt.Println("Usage: generate <priv_out> <pub_out>")
			os.Exit(1)
		}
		generateKeys(os.Args[2], os.Args[3])
	case "decrypt":
		if len(os.Args) != 4 {
			fmt.Println("Usage: decrypt <priv_in> <bundle_in>")
			os.Exit(1)
		}
		decryptBundle(os.Args[2], os.Args[3])
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
	
	// Print fingerprint
	hash := sha256.Sum256(pubBytes)
	fmt.Printf("%x\n", hash)
}

func decryptBundle(privPath, bundlePath string) {
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
	
	// We use RSA-OAEP with SHA-256
	plaintext, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privKey, bundleBytes, nil)
	if err != nil {
		fmt.Printf("Error decrypting bundle: %v\n", err)
		os.Exit(1)
	}

	var b Bundle
	if err := json.Unmarshal(plaintext, &b); err != nil {
		fmt.Printf("Error parsing bundle JSON: %v\n", err)
		os.Exit(1)
	}

	// Validate fields
	if b.SchemaVersion != "1" {
		fmt.Printf("Invalid schema version: %s\n", b.SchemaVersion)
		os.Exit(1)
	}
	if b.AgentName == "" || b.Token == "" || b.RelayURL == "" || b.Fingerprint == "" || b.BootstrapID == "" {
		fmt.Println("Bundle is missing required fields")
		os.Exit(1)
	}
	
	expiry, err := time.Parse(time.RFC3339, b.Expiry)
	if err != nil {
		fmt.Printf("Invalid expiry format: %v\n", err)
		os.Exit(1)
	}
	if time.Now().After(expiry) {
		fmt.Println("Bundle has expired")
		os.Exit(1)
	}

	// Output validated JSON
	out, _ := json.Marshal(b)
	fmt.Println(string(out))
}
