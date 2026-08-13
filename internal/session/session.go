package session

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"forgegrid/internal/network"
)

type coordinatorConfig struct {
	AdminToken string `json:"admin_token"`
}

func Run(args []string) int {
	if len(args) == 0 {
		fmt.Println("Usage: forgegrid session <start>")
		return 2
	}
	switch args[0] {
	case "start":
		return start(args[1:])
	default:
		fmt.Printf("Unknown session command: %s\n", args[0])
		return 2
	}
}

func start(args []string) int {
	fs := flag.NewFlagSet("session start", flag.ContinueOnError)
	controller := fs.String("controller", "https://127.0.0.1:8080", "Controller API URL")
	lanIP := fs.String("ip", outboundIP(), "LAN IP runners should use")
	port := fs.String("port", "8080", "Controller port runners should use")
	agentPort := fs.String("agent-port", "9091", "AgentBridge relay port runners should use")
	agentFingerprint := fs.String("agent-fingerprint", "", "AgentBridge TLS fingerprint; defaults to local relay certificate")
	insecure := fs.Bool("insecure", false, "Use HTTP for runner commands")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := loadCoordinatorConfig()
	if err != nil {
		fmt.Printf("Failed to read coordinator config: %v\n", err)
		return 1
	}
	status, err := coordinatorStatus(*controller, cfg.AdminToken)
	if err != nil {
		fmt.Printf("Failed to reach controller: %v\n", err)
		return 1
	}
	code, err := pairingCode(*controller, cfg.AdminToken)
	if err != nil {
		fmt.Printf("Failed to generate pairing code: %v\n", err)
		return 1
	}
	bridgeFP := strings.TrimSpace(*agentFingerprint)
	if bridgeFP == "" {
		bridgeFP = localAgentBridgeFingerprint()
	}

	scheme := "https"
	if *insecure {
		scheme = "http"
	}
	controllerURL := fmt.Sprintf("%s://%s:%s", scheme, *lanIP, *port)
	agentURL := fmt.Sprintf("https://%s:%s", *lanIP, *agentPort)

	fmt.Println("ForgeGrid Wi-Fi Session")
	fmt.Println()
	fmt.Printf("Controller: %s\n", controllerURL)
	fmt.Printf("Controller fingerprint: %s\n", status["fingerprint"])
	fmt.Printf("AgentBridge: %s\n", agentURL)
	if bridgeFP != "" {
		fmt.Printf("AgentBridge fingerprint: %s\n", bridgeFP)
	} else {
		fmt.Println("AgentBridge fingerprint: unknown; pass -agent-fingerprint or start the relay once")
	}
	fmt.Printf("Pairing code: %s\n", code)
	fmt.Println()
	fmt.Println("Windows runner bootstrap:")
	fmt.Printf(".\\ForgeGrid.exe runner bootstrap -name \"RUNNER_NAME\" -controller %s -code %s -fingerprint %s -agent-url %s -agent-fingerprint %s\n", controllerURL, code, status["fingerprint"], agentURL, bridgeFP)
	fmt.Println()
	fmt.Println("If already paired, runners can reconnect with:")
	fmt.Println(".\\ForgeGrid.exe -mode worker")
	return 0
}

func loadCoordinatorConfig() (*coordinatorConfig, error) {
	dir, _ := os.UserConfigDir()
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	b, err := os.ReadFile(filepath.Join(dir, "forgegrid", "coordinator", "coordinator.json"))
	if err != nil {
		return nil, err
	}
	var cfg coordinatorConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	if cfg.AdminToken == "" {
		return nil, fmt.Errorf("admin token is missing")
	}
	return &cfg, nil
}

func coordinatorStatus(baseURL, token string) (map[string]string, error) {
	var out map[string]string
	if err := controllerReq(http.MethodGet, baseURL, "/api/coordinator/status", token, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func pairingCode(baseURL, token string) (string, error) {
	var out map[string]string
	if err := controllerReq(http.MethodPost, baseURL, "/api/pairing/code", token, bytes.NewReader(nil), &out); err != nil {
		return "", err
	}
	return out["code"], nil
}

func controllerReq(method, baseURL, path, token string, body io.Reader, out interface{}) error {
	req, err := http.NewRequest(method, strings.TrimRight(baseURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:"+token)))
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func localAgentBridgeFingerprint() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(home, ".local", "share", "forgegrid", "agentbridge", "cert.pem"))
	if err != nil {
		return ""
	}
	fp, err := network.FingerprintFromPEM(b)
	if err != nil {
		return ""
	}
	return fp
}

func outboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
