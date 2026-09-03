package coordinator

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
)

type workerCreds struct {
	WorkerID       string `json:"worker_id"`
	Token          string `json:"token"`
	CoordinatorURL string `json:"coordinator_url"`
	Fingerprint    string `json:"fingerprint"`
	NodeName       string `json:"node_name"`
	Insecure       bool   `json:"insecure"`
}

func SubmitManifest(args []string) int {
	if len(args) < 1 {
		fmt.Println("Usage: forgegrid submit <manifest.yaml>")
		return 1
	}

	manifestPath := args[0]
	manifestData, err := ioutil.ReadFile(manifestPath)
	if err != nil {
		fmt.Printf("Failed to read manifest: %v\n", err)
		return 1
	}

	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	credsPath := filepath.Join(localAppData, "ForgeGrid", "worker_creds.json")
	if _, err := os.Stat(credsPath); os.IsNotExist(err) {
		home, _ := os.UserHomeDir()
		credsPath = filepath.Join(home, ".config", "forgegrid", "worker", "worker_creds.json")
	}

	credsData, err := ioutil.ReadFile(credsPath)
	if err != nil {
		fmt.Printf("Failed to read worker credentials from %s: %v\n", credsPath, err)
		return 1
	}

	var creds workerCreds
	if err := json.Unmarshal(credsData, &creds); err != nil {
		fmt.Printf("Failed to parse worker credentials: %v\n", err)
		return 1
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	req, err := http.NewRequest("POST", creds.CoordinatorURL+"/api/jobs/manifest", bytes.NewReader(manifestData))
	if err != nil {
		fmt.Printf("Failed to create request: %v\n", err)
		return 1
	}

	req.Header.Set("Authorization", "Bearer "+creds.Token)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Failed to submit manifest: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Manifest submission failed: %s\n%s\n", resp.Status, string(body))
		return 1
	}

	fmt.Printf("Manifest submitted successfully:\n%s\n", string(body))
	return 0
}
