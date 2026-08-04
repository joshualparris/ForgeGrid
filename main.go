package main

import (
	"flag"
	"fmt"
	"os"

	"forgegrid/internal/agentbridge"
	"forgegrid/internal/coordinator"
	"forgegrid/internal/store"
	"forgegrid/internal/worker"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "agent-bridge" {
		agentbridge.RunCLI(os.Args[2:])
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

	flag.Parse()

	if *mode == "" {
		fmt.Println("Usage: forgegrid -mode <coordinator|worker> [options]")
		fmt.Println("Example Coordinator: forgegrid -mode coordinator -port 8080")
		fmt.Println("Example Worker: forgegrid -mode worker -name Worker-1 -coordinator 192.168.1.10 -code 123456 -fingerprint <FP>")
		os.Exit(1)
	}

	dataDir := "./forgegrid-data"
	os.MkdirAll(dataDir, 0755)

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
		w.Start()

		// Block forever
		select {}
	} else {
		fmt.Println("Unknown mode:", *mode)
		os.Exit(1)
	}
}
