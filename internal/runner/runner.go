package runner

import (
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"forgegrid/internal/agentbridge"
	"forgegrid/internal/doctor"
	"forgegrid/internal/worker"
)

func Run(args []string) int {
	if len(args) == 0 {
		fmt.Println("Usage: forgegrid runner <bootstrap>")
		return 2
	}
	switch args[0] {
	case "bootstrap":
		return bootstrap(args[1:])
	default:
		fmt.Printf("Unknown runner command: %s\n", args[0])
		return 2
	}
}

func bootstrap(args []string) int {
	fs := flag.NewFlagSet("runner bootstrap", flag.ContinueOnError)
	name := fs.String("name", "Unnamed-Node", "Runner name")
	controller := fs.String("controller", "", "Controller URL, e.g. https://10.0.0.5:8080")
	code := fs.String("code", "", "Pairing code for first setup")
	fingerprint := fs.String("fingerprint", "", "Controller TLS fingerprint")
	insecure := fs.Bool("insecure", false, "Disable TLS verification for worker pairing")
	labels := fs.String("labels", "", "Comma-separated runner labels")
	capabilities := fs.String("capabilities", "", "Comma-separated runner capabilities")
	allowedRepos := fs.String("allowed-repos", "", "Comma-separated allowed repository URLs")
	allowPush := fs.Bool("allow-push", false, "Allow this runner to push git changes")
	agentName := fs.String("agent-name", "", "AgentBridge identity name")
	agentURL := fs.String("agent-url", "", "AgentBridge relay URL")
	agentFingerprint := fs.String("agent-fingerprint", "", "AgentBridge TLS fingerprint")
	agentTokenStdin := fs.Bool("agent-token-stdin", false, "Read AgentBridge token from stdin")
	installService := fs.Bool("install-service", false, "Install and start worker service after bootstrap")
	startWorker := fs.Bool("start-worker", false, "Start worker in the foreground after bootstrap")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *controller == "" {
		fmt.Println("runner bootstrap requires -controller")
		return 2
	}
	if !*insecure && *fingerprint == "" {
		fmt.Println("runner bootstrap requires -fingerprint unless -insecure is set")
		return 2
	}

	if err := worker.WritePolicy(worker.Policy{
		AllowedRepos: splitCSV(*allowedRepos),
		AllowPush:    *allowPush,
		Labels:       splitCSV(*labels),
		Capabilities: splitCSV(*capabilities),
	}); err != nil {
		fmt.Printf("[FAIL] write worker policy: %v\n", err)
		return 1
	}
	fmt.Printf("[OK] worker policy written: %s\n", worker.WorkerPolicyPath())

	w := worker.New(*name, "./forgegrid-workspace", *insecure)
	w.SetGitPolicy(*allowedRepos, *allowPush)
	w.SetLabelsAndCapabilities(*labels, *capabilities)
	if err := w.LoadCreds(); err == nil {
		fmt.Printf("[OK] loaded saved worker credentials: %s\n", w.WorkerID)
	} else {
		if *code == "" {
			fmt.Println("[FAIL] no saved worker credentials and no -code was provided")
			return 1
		}
		pairTarget := controllerPairTarget(*controller)
		if err := w.Pair(pairTarget, *code, *fingerprint); err != nil {
			fmt.Printf("[FAIL] pair worker: %v\n", err)
			return 1
		}
	}

	if *agentName != "" || *agentTokenStdin {
		if *agentName == "" || *agentURL == "" || *agentFingerprint == "" || !*agentTokenStdin {
			fmt.Println("[FAIL] AgentBridge setup requires -agent-name, -agent-url, -agent-fingerprint, and -agent-token-stdin")
			return 1
		}
		tokenBytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Printf("[FAIL] read AgentBridge token: %v\n", err)
			return 1
		}
		token := strings.TrimSpace(string(tokenBytes))
		if token == "" {
			fmt.Println("[FAIL] AgentBridge token cannot be empty")
			return 1
		}
		if err := agentbridge.SaveClientConfig(agentbridge.GetConfigPath(), agentbridge.ClientConfig{
			Name:        *agentName,
			Token:       token,
			URL:         *agentURL,
			Fingerprint: *agentFingerprint,
		}); err != nil {
			fmt.Printf("[FAIL] save AgentBridge config: %v\n", err)
			return 1
		}
		fmt.Printf("[OK] AgentBridge config written: %s\n", agentbridge.GetConfigPath())
	}

	doctorArgs := []string{"-coordinator-url", *controller}
	if *agentURL != "" {
		doctorArgs = append(doctorArgs, "-agent-url", *agentURL)
	}
	fmt.Println()
	codeExit := doctor.Run(doctorArgs)
	if codeExit != 0 {
		return codeExit
	}

	if *installService {
		serviceArgs := []string{"-name", *name}
		if err := worker.ControlService("install", serviceArgs); err != nil {
			fmt.Printf("[WARN] service install failed: %v\n", err)
		} else if err := worker.ControlService("start", nil); err != nil {
			fmt.Printf("[WARN] service start failed: %v\n", err)
		} else {
			fmt.Println("[OK] worker service installed and started")
			return 0
		}
	}

	if *startWorker {
		fmt.Println("[OK] starting worker; leave this terminal open")
		w.Start()
		select {}
	}

	fmt.Println()
	fmt.Println("Bootstrap complete. Start the worker with:")
	fmt.Println(".\\ForgeGrid.exe -mode worker")
	return 0
}

func controllerPairTarget(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && u.Host != "" {
		return u.Host
	}
	return strings.TrimRight(strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://"), "/")
}

func splitCSV(raw string) []string {
	var vals []string
	for _, val := range strings.Split(raw, ",") {
		val = strings.TrimSpace(val)
		if val != "" {
			vals = append(vals, val)
		}
	}
	return vals
}
