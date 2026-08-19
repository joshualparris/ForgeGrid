package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"forgegrid/internal/agentbridge"
	"forgegrid/internal/coordinator"
	"forgegrid/internal/doctor"
	"forgegrid/internal/runner"
	"forgegrid/internal/session"
	"forgegrid/internal/store"
	"forgegrid/internal/version"
	"forgegrid/internal/worker"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version" || os.Args[1] == "-version") {
		info := version.Info()
		fmt.Printf("ForgeGrid %s commit=%s built=%s platform=%s/%s protocol=%s\n", info.Version, info.Commit, info.BuildTime, info.Platform, info.Architecture, info.Protocol)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "agent-bridge" {
		agentbridge.RunCLI(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "doctor" {
		os.Exit(doctor.Run(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "session" {
		os.Exit(session.Run(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "runner" {
		os.Exit(runner.Run(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "service" {
		if len(os.Args) < 3 {
			fmt.Println("Usage: forgegrid service <install|uninstall|start|stop|status> [worker options for install]")
			os.Exit(1)
		}
		if err := worker.ControlService(os.Args[2], os.Args[3:]); err != nil {
			fmt.Printf("Service command failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	mode := flag.String("mode", "", "coordinator or worker")
	port := flag.String("port", "8080", "coordinator port")
	nodeName := flag.String("name", "Unnamed-Node", "name of this node")
	coordIP := flag.String("coordinator", "", "IP of the coordinator (worker mode)")
	code := flag.String("code", "", "pairing code (worker mode)")
	insecure := flag.Bool("insecure", false, "Disable TLS (DEVELOPMENT ONLY)")
	fingerprint := flag.String("fingerprint", "", "TLS certificate fingerprint of the coordinator (worker mode)")
	resetWorker := flag.Bool("reset-worker", false, "Reset saved worker credentials")
	allowedRepos := flag.String("allowed-repos", "", "comma-separated repository URLs this worker may clone/fetch for git jobs")
	allowPush := flag.Bool("allow-push", false, "allow this worker to push committed job changes to git remotes")
	allowBootstrap := flag.Bool("allow-bootstrap", false, "allow this worker to bootstrap environment tools like go, node, godot")
	labels := flag.String("labels", "", "comma-separated worker labels, e.g. godot,low-power,windows-build")
	capabilities := flag.String("capabilities", "", "comma-separated worker capabilities, e.g. godot,codex,github-pr")
	writePolicy := flag.Bool("write-worker-policy", false, "write worker policy from flags, then exit")

	installService := flag.Bool("install-service", false, "install as a Windows service")
	startService := flag.Bool("start-service", false, "start the Windows service")
	stopService := flag.Bool("stop-service", false, "stop the Windows service")
	uninstallService := flag.Bool("uninstall-service", false, "uninstall the Windows service")
	serviceStatus := flag.Bool("service-status", false, "query the Windows service status")
	healthCheck := flag.Bool("health-check", false, "run diagnostics on the worker environment")

	flag.Parse()

	if *healthCheck {
		coordURL := *coordIP
		if coordURL == "" {
			coordURL = "https://127.0.0.1:8080"
		} else if !strings.HasPrefix(coordURL, "http") {
			coordURL = "https://" + coordURL + ":8080"
		}
		if err := worker.RunHealthCheck(coordURL, *fingerprint); err != nil {
			fmt.Printf("Health check failed: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	if *installService {
		if err := worker.ControlService("install", os.Args[2:]); err != nil {
			fmt.Printf("Failed to install service: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if *uninstallService {
		if err := worker.ControlService("uninstall", nil); err != nil {
			fmt.Printf("Failed to uninstall service: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if *startService {
		if err := worker.ControlService("start", nil); err != nil {
			fmt.Printf("Failed to start service: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if *stopService {
		if err := worker.ControlService("stop", nil); err != nil {
			fmt.Printf("Failed to stop service: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if *serviceStatus {
		if err := worker.ControlService("status", nil); err != nil {
			fmt.Printf("Failed to query service status: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	if *mode == "" {
		fmt.Println("Usage: forgegrid -mode <coordinator|worker> [options]")
		fmt.Println("Example Coordinator: forgegrid -mode coordinator -port 8080")
		fmt.Println("Example Worker: forgegrid -mode worker -name Worker-1 -coordinator 192.168.1.10 -code 123456 -fingerprint <FP>")
		os.Exit(1)
	}

	getDataDir := func() string {
		dir, _ := os.UserConfigDir()
		if dir == "" {
			dir, _ = os.UserHomeDir()
			dir = dir + "/.config"
		}
		return dir + "/forgegrid/coordinator"
	}
	dataDir := getDataDir()
	os.MkdirAll(dataDir, 0700)

	if *mode == "coordinator" {
		s, err := store.NewStore(dataDir)
		if err != nil {
			fmt.Printf("Failed to initialize store: %v\n", err)
			os.Exit(1)
		}
		c := coordinator.New(s, *insecure)
		if err := c.Start(*port); err != nil {
			fmt.Printf("Coordinator failed: %v\n", err)
			os.Exit(1)
		}
	} else if *mode == "worker" {
		if *writePolicy {
			if err := worker.WritePolicy(worker.Policy{
				AllowedRepos:   splitCSV(*allowedRepos),
				AllowPush:      *allowPush,
				AllowBootstrap: *allowBootstrap,
				Labels:         splitCSV(*labels),
				Capabilities:   splitCSV(*capabilities),
			}); err != nil {
				fmt.Printf("Failed to write worker policy: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Worker policy written successfully.")
			os.Exit(0)
		}

		if *resetWorker {
			err := worker.ResetCredentials()
			if err != nil {
				fmt.Printf("Failed to reset worker credentials: %v\n", err)
			} else {
				fmt.Println("Worker credentials reset successfully.")
			}
			os.Exit(0)
		}

		w := worker.New(*nodeName, "./forgegrid-workspace", *insecure)
		w.SetGitPolicy(*allowedRepos, *allowPush)
		w.SetLabelsAndCapabilities(*labels, *capabilities)

		err := w.LoadCreds()
		if err == nil {
			fmt.Println("Loaded saved worker credentials.")
			fmt.Printf("Reconnecting to %s as %s...\n", w.CoordinatorURL, w.WorkerID)
		} else {
			if *coordIP == "" || *code == "" {
				fmt.Println("Worker mode requires -coordinator and -code flags.")
				os.Exit(1)
			}
			if !*insecure && *fingerprint == "" {
				fmt.Println("Secure worker mode requires -fingerprint of the coordinator.")
				os.Exit(1)
			}
			if err := w.Pair(*coordIP, *code, *fingerprint); err != nil {
				fmt.Printf("Failed to pair: %v\n", err)
				os.Exit(1)
			}
		}

		runFunc := func() {
			w.Start()
			// Block forever
			select {}
		}

		if err := worker.RunService(runFunc); err != nil {
			fmt.Printf("Worker service error: %v\n", err)
			os.Exit(1)
		}
	} else if *mode == "update-helper" {
		worker.RunUpdater()
	} else {
		fmt.Println("Unknown mode:", *mode)
		os.Exit(1)
	}
}

func splitCSV(raw string) []string {
	var vals []string
	for _, v := range strings.Split(raw, ",") {
		v = strings.TrimSpace(v)
		if v != "" {
			vals = append(vals, v)
		}
	}
	return vals
}
