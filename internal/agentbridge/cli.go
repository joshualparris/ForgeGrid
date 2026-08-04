package agentbridge

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"forgegrid/internal/network"
)

func RunCLI(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: forgegrid agent-bridge <command> [args...]")
		fmt.Println("Commands: serve, rotate-tls, register, configure-client, reset-client, send, inbox, ack, complete, fail")
		os.Exit(1)
	}

	cmd := args[0]
	switch cmd {
	case "serve":
		serveCmd(args[1:])
	case "rotate-tls":
		rotateTLSCmd(args[1:])
	case "register":
		registerCmd(args[1:])
	case "configure-client":
		configureClientCmd(args[1:])
	case "reset-client":
		resetClientCmd(args[1:])
	case "send":
		sendCmd(args[1:])
	case "inbox":
		inboxCmd(args[1:])
	case "ack":
		ackCmd(args[1:])
	case "complete":
		completeCmd(args[1:])
	case "fail":
		failCmd(args[1:])
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

func getTLSKeys(dataDir string) (certPath, keyPath, fingerprint string, err error) {
	certPath = filepath.Join(dataDir, "cert.pem")
	keyPath = filepath.Join(dataDir, "key.pem")

	certBytes, errCert := os.ReadFile(certPath)
	_, errKey := os.ReadFile(keyPath)

	if errCert == nil && errKey == nil {
		// Existing keys
		fp, err := network.FingerprintFromPEM(certBytes)
		if err != nil {
			return "", "", "", fmt.Errorf("corrupt cert.pem: %v", err)
		}
		return certPath, keyPath, fp, nil
	}

	// Generate new keys
	certPEM, keyPEM, fp, err := network.GenerateSelfSignedCert()
	if err != nil {
		return "", "", "", err
	}
	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		return "", "", "", err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return "", "", "", err
	}
	return certPath, keyPath, fp, nil
}

func serveCmd(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.String("port", "9090", "Port to serve AgentBridge on")
	insecure := fs.Bool("insecure", false, "Disable TLS")
	fs.Parse(args)

	store, err := NewStore()
	if err != nil {
		log.Fatalf("Store error: %v", err)
	}

	server := NewServer(store)

	// Server settings for hardening
	srv := &http.Server{
		Addr:           ":" + *port,
		Handler:        http.MaxBytesHandler(http.DefaultServeMux, 1024*1024),
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	srv.Handler = mux

	log.Printf("Starting AgentBridge relay on %s", srv.Addr)

	if *insecure {
		log.Fatal(srv.ListenAndServe())
	} else {
		certPath, keyPath, fp, err := getTLSKeys(store.dataDir)
		if err != nil {
			log.Fatalf("TLS error: %v", err)
		}
		log.Printf("TLS Fingerprint: %s", fp)
		log.Fatal(srv.ListenAndServeTLS(certPath, keyPath))
	}
}

func rotateTLSCmd(args []string) {
	fmt.Println("WARNING: Rotating TLS certificates will break all existing client connections.")
	fmt.Println("All clients must be reconfigured with the new fingerprint.")

	store, err := NewStore()
	if err != nil {
		log.Fatalf("Store error: %v", err)
	}

	certPath := filepath.Join(store.dataDir, "cert.pem")
	keyPath := filepath.Join(store.dataDir, "key.pem")
	os.Remove(certPath)
	os.Remove(keyPath)

	_, _, fp, err := getTLSKeys(store.dataDir)
	if err != nil {
		log.Fatalf("Failed to generate new TLS keys: %v", err)
	}
	fmt.Printf("Successfully rotated TLS certificates. New fingerprint: %s\n", fp)
}

func registerCmd(args []string) {
	fs := flag.NewFlagSet("register", flag.ExitOnError)
	name := fs.String("name", "", "Agent name")
	fs.Parse(args)

	if *name == "" {
		log.Fatal("Agent name required")
	}

	store, err := NewStore()
	if err != nil {
		log.Fatalf("Store error: %v", err)
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("Failed to generate token: %v", err)
	}
	secret := hex.EncodeToString(b)

	hash := sha256.Sum256([]byte(secret))
	hashStr := hex.EncodeToString(hash[:])

	if err := store.RegisterAgent(*name, hashStr); err != nil {
		log.Fatalf("Register error: %v", err)
	}

	fmt.Printf("Agent %s registered successfully.\n", *name)
	fmt.Printf("Authentication Token: %s\n", secret)
	fmt.Println("SAVE THIS TOKEN SECURELY. IT WILL NOT BE SHOWN AGAIN.")
}

type ClientConfig struct {
	Name        string `json:"name"`
	Token       string `json:"token"`
	URL         string `json:"url"`
	Fingerprint string `json:"fingerprint"`
}

func getConfigPath() string {
	if os.PathSeparator == '\\' {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
		}
		return filepath.Join(localAppData, "ForgeGrid", "agentclient.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "forgegrid", "agentclient.json")
}

func configureClientCmd(args []string) {
	fs := flag.NewFlagSet("configure-client", flag.ExitOnError)
	name := fs.String("name", "", "Agent name")
	tokenFile := fs.String("token-file", "", "File containing the agent token (will be deleted after reading)")
	url := fs.String("url", "https://127.0.0.1:9090", "Relay URL")
	fp := fs.String("fingerprint", "", "TLS Fingerprint")
	fs.Parse(args)

	if *name == "" || *fp == "" {
		log.Fatal("--name and --fingerprint are required")
	}

	var token string
	if *tokenFile != "" {
		b, err := os.ReadFile(*tokenFile)
		if err != nil {
			log.Fatalf("Failed to read token file: %v", err)
		}
		token = strings.TrimSpace(string(b))

		// Securely overwrite and delete
		zeroes := make([]byte, len(b))
		if err := os.WriteFile(*tokenFile, zeroes, 0600); err != nil {
			log.Fatalf("Failed to overwrite token file: %v", err)
		}
		if err := os.Remove(*tokenFile); err != nil {
			log.Fatalf("Failed to delete token file: %v", err)
		}
	} else {
		// Fallback for when token-file is not provided
		fmt.Print("Enter token: ")
		pwd, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			log.Fatalf("Failed to read token: %v", err)
		}
		token = strings.TrimSpace(string(pwd))
	}

	if token == "" {
		log.Fatal("Token is required")
	}

	cfg := ClientConfig{
		Name:        *name,
		Token:       token,
		URL:         *url,
		Fingerprint: *fp,
	}

	path := getConfigPath()
	os.MkdirAll(filepath.Dir(path), 0700)

	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, b, 0600); err != nil {
		log.Fatalf("Failed to save config: %v", err)
	}
	fmt.Printf("Client configured successfully at %s.\n", path)
	fmt.Println("Note: Token is currently stored in plaintext on disk. Directory is restricted to current user (0700/0600).")
}

func resetClientCmd(args []string) {
	os.Remove(getConfigPath())
	fmt.Println("Client configuration reset.")
}

func getClient(fs *flag.FlagSet) *Client {
	url := fs.String("url", "https://127.0.0.1:9090", "Relay URL")
	name := fs.String("name", "", "Agent name")
	fp := fs.String("fingerprint", "", "TLS fingerprint")
	insecure := fs.Bool("insecure", false, "Disable TLS verification")
	fs.Parse(os.Args[3:])

	var token string

	// Try loading from config if values not provided via flags
	if *name == "" || token == "" {
		b, err := os.ReadFile(getConfigPath())
		if err == nil {
			var cfg ClientConfig
			if json.Unmarshal(b, &cfg) == nil {
				if *url == "https://127.0.0.1:9090" {
					*url = cfg.URL
				}
				if *name == "" {
					*name = cfg.Name
				}
				if token == "" {
					token = cfg.Token
				}
				if *fp == "" {
					*fp = cfg.Fingerprint
				}
			}
		}
	}

	// Try env vars
	if *name == "" {
		*name = os.Getenv("AGENT_NAME")
	}
	if token == "" {
		token = os.Getenv("AGENT_TOKEN")
	}
	if *fp == "" {
		*fp = os.Getenv("AGENT_FINGERPRINT")
	}

	if *name == "" || token == "" {
		log.Fatal("Agent name and token are required (via env, or configure-client)")
	}

	client, err := NewClient(*url, *name, token, *fp, *insecure)
	if err != nil {
		log.Fatalf("Client error: %v", err)
	}
	return client
}

func sendCmd(args []string) {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	to := fs.String("to", "", "Recipient")
	task := fs.String("task", "", "Task ID")
	msgType := fs.String("type", string(TypeInstruction), "Message type")
	body := fs.String("message", "", "Message body")
	idem := fs.String("idempotency-key", "", "Idempotency key")
	client := getClient(fs)

	if *to == "" || *body == "" {
		log.Fatal("--to and --message required")
	}

	msg, err := client.SendMessage(*to, *task, MessageType(*msgType), *body, 3600, *idem)
	if err != nil {
		log.Fatalf("Send error: %v", err)
	}
	fmt.Printf("Message sent. ID: %s\n", msg.ID)
}

func inboxCmd(args []string) {
	fs := flag.NewFlagSet("inbox", flag.ExitOnError)
	client := getClient(fs)

	msgs, err := client.GetInbox()
	if err != nil {
		log.Fatalf("Inbox error: %v", err)
	}

	b, _ := json.MarshalIndent(msgs, "", "  ")
	fmt.Println(string(b))
}

func ackCmd(args []string) {
	fs := flag.NewFlagSet("ack", flag.ExitOnError)
	id := fs.String("message-id", "", "Message ID")
	client := getClient(fs)

	if *id == "" {
		log.Fatal("--message-id required")
	}

	msg, err := client.Acknowledge(*id)
	if err != nil {
		log.Fatalf("Ack error: %v", err)
	}
	fmt.Printf("Message %s acknowledged.\n", msg.ID)
}

func completeCmd(args []string) {
	fs := flag.NewFlagSet("complete", flag.ExitOnError)
	id := fs.String("message-id", "", "Message ID")
	resFile := fs.String("result-file", "", "Result JSON file")
	client := getClient(fs)

	if *id == "" {
		log.Fatal("--message-id required")
	}

	var res json.RawMessage
	if *resFile != "" {
		b, err := os.ReadFile(*resFile)
		if err != nil {
			log.Fatalf("Read error: %v", err)
		}
		res = json.RawMessage(b)
	} else {
		res = json.RawMessage(`{"status":"ok"}`)
	}

	msg, err := client.Complete(*id, res)
	if err != nil {
		log.Fatalf("Complete error: %v", err)
	}
	fmt.Printf("Message %s completed.\n", msg.ID)
}

func failCmd(args []string) {
	fs := flag.NewFlagSet("fail", flag.ExitOnError)
	id := fs.String("message-id", "", "Message ID")
	resFile := fs.String("result-file", "", "Result JSON file")
	client := getClient(fs)

	if *id == "" {
		log.Fatal("--message-id required")
	}

	var res json.RawMessage
	if *resFile != "" {
		b, err := os.ReadFile(*resFile)
		if err != nil {
			log.Fatalf("Read error: %v", err)
		}
		res = json.RawMessage(b)
	} else {
		res = json.RawMessage(`{"status":"failed"}`)
	}

	msg, err := client.Fail(*id, res)
	if err != nil {
		log.Fatalf("Fail error: %v", err)
	}
	fmt.Printf("Message %s failed.\n", msg.ID)
}
