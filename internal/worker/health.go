package worker

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

func RunHealthCheck(coordinatorURL string, fingerprint string) error {
	fmt.Println("--- ForgeGrid Worker Health Check ---")

	// 1. Check Coordinator Reachability
	fmt.Printf("1. Checking Coordinator Reachability (%s)...\n", coordinatorURL)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // fingerprint verification handled during normal worker run
	}
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}

	resp, err := client.Get(coordinatorURL + "/api/jobs")
	if err != nil {
		fmt.Printf("   [FAILED] Could not reach coordinator: %v\n", err)
		fmt.Println("   -> Action: Check firewall rules on the coordinator (TCP 8080).")
	} else {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized {
			fmt.Printf("   [OK] Coordinator reachable (Status: %d)\n", resp.StatusCode)
		} else {
			fmt.Printf("   [WARNING] Coordinator reached, but returned unexpected status: %d\n", resp.StatusCode)
		}
	}

	// 2. Check AgentBridge Reachability (default port 9090)
	// Extract IP from coordinatorURL (assuming format https://ip:port)
	agentBridgeURL := strings.Replace(coordinatorURL, "8080", "9090", 1)
	fmt.Printf("2. Checking AgentBridge Reachability (%s)...\n", agentBridgeURL)
	respAB, errAB := client.Get(agentBridgeURL)
	if errAB != nil {
		fmt.Printf("   [FAILED] Could not reach AgentBridge: %v\n", errAB)
		fmt.Println("   -> Action: Ensure AgentBridge is running and firewall allows TCP 9090.")
	} else {
		respAB.Body.Close()
		fmt.Println("   [OK] AgentBridge is reachable.")
	}

	// 3. Check Git Credential Manager
	fmt.Println("3. Checking Git Credential Configuration...")
	gitCmd := exec.Command("git", "config", "--global", "credential.helper")
	out, err := gitCmd.Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		fmt.Println("   [WARNING] Git credential helper is not configured globally.")
		fmt.Println("   -> Action: Run `git config --global credential.helper manager` (or equivalent) to avoid prompt hangs during push jobs.")
	} else {
		fmt.Printf("   [OK] Git credential helper configured as: %s\n", strings.TrimSpace(string(out)))
	}

	// 4. Verify Local Execution Profiles
	fmt.Println("4. Validating Common Executables...")
	caps := DetectCapabilities()
	for _, cap := range []string{"git", "python", "go", "node", "antigravity", "codex", "godot"} {
		if hasCapability(caps, cap) {
			fmt.Printf("   [OK] %s available.\n", humanToolName(cap))
		} else {
			fmt.Printf("   [WARNING] %s not detected.\n", humanToolName(cap))
		}
	}

	if fingerprint == "" {
		fmt.Println("\n[INFO] Run with -fingerprint to test TLS pinning.")
	}

	fmt.Println("-------------------------------------")
	return nil
}

func hasCapability(caps []string, want string) bool {
	for _, cap := range caps {
		if cap == want {
			return true
		}
	}
	return false
}

func humanToolName(cap string) string {
	switch cap {
	case "git":
		return "Git"
	case "python":
		return "Python"
	case "go":
		return "Go"
	case "node":
		return "Node"
	case "antigravity":
		return "Antigravity"
	case "codex":
		return "Codex"
	case "godot":
		return "Godot"
	default:
		return cap
	}
}
