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

	"forgegrid/internal/network"
)

func RunCLI(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: forgegrid agent-bridge <command> [args...]")
		fmt.Println("Commands: serve, register, send, inbox, ack, complete")
		os.Exit(1)
	}

	cmd := args[0]
	switch cmd {
	case "serve":
		serveCmd(args[1:])
	case "register":
		registerCmd(args[1:])
	case "send":
		sendCmd(args[1:])
	case "inbox":
		inboxCmd(args[1:])
	case "ack":
		ackCmd(args[1:])
	case "complete":
		completeCmd(args[1:])
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		os.Exit(1)
	}
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
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	addr := ":" + *port
	log.Printf("Starting AgentBridge relay on %s", addr)

	if *insecure {
		log.Fatal(http.ListenAndServe(addr, mux))
	} else {
		certPEM, keyPEM, fp, err := network.GenerateSelfSignedCert()
		if err != nil {
			log.Fatalf("TLS error: %v", err)
		}
		
		certPath := store.dataDir + "/cert.pem"
		keyPath := store.dataDir + "/key.pem"
		os.WriteFile(certPath, certPEM, 0600)
		os.WriteFile(keyPath, keyPEM, 0600)
		
		log.Printf("TLS Fingerprint: %s", fp)
		log.Fatal(http.ListenAndServeTLS(addr, certPath, keyPath, mux))
	}
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
	rand.Read(b)
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

func getClient(fs *flag.FlagSet) *Client {
	url := fs.String("url", "https://127.0.0.1:9090", "Relay URL")
	name := fs.String("name", "", "Agent name")
	token := fs.String("token", "", "Agent token")
	insecure := fs.Bool("insecure", false, "Disable TLS verification")
	fs.Parse(os.Args[3:]) // adjust based on flags position

	if *name == "" || *token == "" {
		// Try environment variables
		*name = os.Getenv("AGENT_NAME")
		*token = os.Getenv("AGENT_TOKEN")
		if *name == "" || *token == "" {
			log.Fatal("Agent name and token are required")
		}
	}
	return NewClient(*url, *name, *token, *insecure)
}

func sendCmd(args []string) {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	to := fs.String("to", "", "Recipient")
	task := fs.String("task", "", "Task ID")
	msgType := fs.String("type", string(TypeInstruction), "Message type")
	body := fs.String("message", "", "Message body")
	client := getClient(fs)

	if *to == "" || *body == "" {
		log.Fatal("--to and --message required")
	}

	msg, err := client.SendMessage(*to, *task, MessageType(*msgType), *body, 3600)
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
