package doctor

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"forgegrid/internal/agentbridge"
	"forgegrid/internal/network"
	"forgegrid/internal/worker"
)

type check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type report struct {
	GeneratedAt string  `json:"generated_at"`
	OS          string  `json:"os"`
	Arch        string  `json:"arch"`
	Checks      []check `json:"checks"`
}

func Run(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	coordinatorURL := fs.String("coordinator-url", "", "Coordinator URL to probe; defaults to saved worker credentials")
	agentURL := fs.String("agent-url", "", "AgentBridge relay URL to probe; defaults to saved AgentBridge client config")
	jsonOut := fs.Bool("json", false, "Print machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	r := report{
		GeneratedAt: time.Now().Format(time.RFC3339),
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
	}
	add := func(name, status, detail string) {
		r.Checks = append(r.Checks, check{Name: name, Status: status, Detail: detail})
	}

	add("binary", "ok", fmt.Sprintf("ForgeGrid binary is runnable on %s/%s", runtime.GOOS, runtime.GOARCH))
	checkWorkerFiles(add)
	checkAgentBridge(add, strings.TrimSpace(*agentURL))
	checkCoordinator(add, strings.TrimSpace(*coordinatorURL))
	checkService(add)

	if *jsonOut {
		b, _ := json.MarshalIndent(r, "", "  ")
		fmt.Println(string(b))
	} else {
		printHuman(r)
	}

	for _, c := range r.Checks {
		if c.Status == "fail" {
			return 1
		}
	}
	return 0
}

func checkWorkerFiles(add func(string, string, string)) {
	credsPath := worker.WorkerCredsPath()
	if _, err := os.Stat(credsPath); err != nil {
		if os.IsNotExist(err) {
			add("worker credentials", "warn", fmt.Sprintf("missing: %s; pair this runner before service start", credsPath))
		} else {
			add("worker credentials", "fail", err.Error())
		}
	} else {
		add("worker credentials", "ok", credsPath)
	}

	policyPath := worker.WorkerPolicyPath()
	if _, err := os.Stat(policyPath); err != nil {
		if os.IsNotExist(err) {
			add("worker policy", "warn", fmt.Sprintf("missing: %s; labels/capabilities and push policy may be defaults", policyPath))
		} else {
			add("worker policy", "fail", err.Error())
		}
	} else {
		add("worker policy", "ok", policyPath)
	}

	if err := os.MkdirAll(worker.WorkerDataDir(), 0700); err != nil {
		add("worker data dir", "fail", err.Error())
	} else {
		add("worker data dir", "ok", worker.WorkerDataDir())
	}
}

func checkAgentBridge(add func(string, string, string), agentURL string) {
	cfgPath := agentbridge.GetConfigPath()
	cfg, err := agentbridge.LoadClientConfig(cfgPath)
	if err != nil {
		add("agentbridge config", "warn", fmt.Sprintf("%s: %v", cfgPath, err))
	} else {
		add("agentbridge config", "ok", fmt.Sprintf("identity %q at %s", cfg.Name, cfgPath))
	}

	probeURL := agentURL
	if probeURL == "" && cfg != nil {
		probeURL = cfg.URL
	}
	if probeURL == "" {
		return
	}

	fingerprint := ""
	if cfg != nil {
		fingerprint = cfg.Fingerprint
	}
	if err := probeURLReachable(probeURL, fingerprint); err != nil {
		add("agentbridge reachability", "fail", err.Error())
	} else {
		add("agentbridge reachability", "ok", probeURL)
	}

	if cfg == nil {
		return
	}
	client, err := agentbridge.NewClient(cfg.URL, cfg.Name, cfg.Token, cfg.Fingerprint, false)
	if err != nil {
		add("agentbridge auth", "fail", err.Error())
		return
	}
	if err := client.Status(); err != nil {
		add("agentbridge auth", "fail", err.Error())
	} else {
		add("agentbridge auth", "ok", "token accepted by relay")
	}
}

func checkCoordinator(add func(string, string, string), coordinatorURL string) {
	if coordinatorURL == "" {
		if creds, err := loadWorkerCreds(); err == nil {
			coordinatorURL = creds.CoordinatorURL
		}
	}
	if coordinatorURL == "" {
		add("coordinator reachability", "warn", "no coordinator URL supplied and no saved worker credentials found")
		return
	}
	if err := probeURLReachable(coordinatorURL, ""); err != nil {
		add("coordinator reachability", "fail", err.Error())
	} else {
		add("coordinator reachability", "ok", coordinatorURL)
	}
}

func checkService(add func(string, string, string)) {
	status, err := worker.ServiceStatus()
	if err != nil {
		if runtime.GOOS == "windows" {
			add("worker service", "warn", err.Error())
		} else {
			add("worker service", "warn", strings.TrimSpace(err.Error()+": "+status))
		}
		return
	}
	add("worker service", "ok", status)
}

func probeURLReachable(rawURL, fingerprint string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	host := u.Host
	if host == "" {
		host = rawURL
	}
	if !strings.Contains(host, ":") {
		if u.Scheme == "http" {
			host += ":80"
		} else {
			host += ":443"
		}
	}
	conn, err := net.DialTimeout("tcp", host, 3*time.Second)
	if err != nil {
		return err
	}
	conn.Close()

	if u.Scheme == "http" {
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(strings.TrimRight(rawURL, "/") + "/")
		if err == nil {
			resp.Body.Close()
		}
		return nil
	}

	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	if fingerprint != "" {
		tlsConfig = network.PinTLSConfig(fingerprint)
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsConfig}}
	resp, err := client.Get(strings.TrimRight(rawURL, "/") + "/")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func loadWorkerCreds() (*worker.WorkerCredentials, error) {
	b, err := os.ReadFile(worker.WorkerCredsPath())
	if err != nil {
		return nil, err
	}
	var creds worker.WorkerCredentials
	if err := json.Unmarshal(b, &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}

func printHuman(r report) {
	fmt.Printf("ForgeGrid Doctor - %s\n", r.GeneratedAt)
	fmt.Printf("System: %s/%s\n\n", r.OS, r.Arch)
	for _, c := range r.Checks {
		marker := "OK"
		if c.Status == "warn" {
			marker = "WARN"
		}
		if c.Status == "fail" {
			marker = "FAIL"
		}
		fmt.Printf("[%s] %s", marker, c.Name)
		if strings.TrimSpace(c.Detail) != "" {
			fmt.Printf(" - %s", c.Detail)
		}
		fmt.Println()
	}
}
