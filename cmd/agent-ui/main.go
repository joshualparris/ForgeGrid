package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"forgegrid/internal/agentbridge"
)

//go:embed ui/*
var uiFS embed.FS

type ClientConfig struct {
	Name        string `json:"name"`
	Token       string `json:"token"`
	URL         string `json:"url"`
	Fingerprint string `json:"fingerprint"`
}

func main() {
	log.Println("Starting AgentBridge GUI Proxy...")

	// 1. Read config
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	cfgPath := filepath.Join(localAppData, "ForgeGrid", "agentclient.json")
	
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		log.Fatalf("Could not read agentclient.json. Please run configure-client first: %v", err)
	}

	// Remove BOM if present (powershell artifact)
	bStr := strings.TrimPrefix(string(b), "\xef\xbb\xbf")
	
	var cfg ClientConfig
	if err := json.Unmarshal([]byte(bStr), &cfg); err != nil {
		log.Fatalf("Failed to parse agentclient.json: %v", err)
	}

	// 2. Initialize AgentBridge client
	// Use insecure=false if fingerprint is present, otherwise true
	insecure := cfg.Fingerprint == "" || cfg.Fingerprint == "dummy"
	client, err := agentbridge.NewClient(cfg.URL, cfg.Name, cfg.Token, cfg.Fingerprint, insecure)
	if err != nil {
		log.Fatalf("Failed to init AgentBridge client: %v", err)
	}
	log.Printf("Connected as agent: %s to %s", cfg.Name, cfg.URL)

	// 3. Setup HTTP routes
	mux := http.NewServeMux()

	// API: Get Inbox
	mux.HandleFunc("/api/inbox", func(w http.ResponseWriter, r *http.Request) {
		msgs, err := client.GetInbox()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if msgs == nil {
			msgs = []agentbridge.AgentMessage{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(msgs)
	})

	// API: Send Message
	mux.HandleFunc("/api/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Recipient string `json:"recipient"`
			TaskID    string `json:"task_id"`
			Body      string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		if req.TaskID == "" {
			req.TaskID = fmt.Sprintf("gui-task-%d", time.Now().Unix())
		}

		msg, err := client.SendMessage(req.Recipient, req.TaskID, agentbridge.TypeInstruction, req.Body, 3600, "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(msg)
	})

	// API: Current Agent Info
	mux.HandleFunc("/api/me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"name": cfg.Name,
			"url":  cfg.URL,
		})
	})

	// 4. Serve UI
	uiSub, err := fs.Sub(uiFS, "ui")
	if err != nil {
		log.Fatalf("Failed to setup embedded UI: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(uiSub)))

	// 5. Start server
	port := "9095"
	serverURL := "http://localhost:" + port
	
	go func() {
		time.Sleep(500 * time.Millisecond)
		openBrowser(serverURL)
	}()

	log.Printf("Listening on %s", serverURL)
	if err := http.ListenAndServe("127.0.0.1:"+port, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		log.Printf("Failed to open browser: %v", err)
	}
}
