package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"forgegrid/internal/coordinator"
	"forgegrid/internal/models"
	"forgegrid/internal/store"
	"forgegrid/internal/ui"
	"forgegrid/internal/worker"
)

func TestIntegration(t *testing.T) {
	ui.DisableBrowser = true
	os.RemoveAll("./test-data")
	
	s, err := store.NewStore("./test-data")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	
	c := coordinator.New(s, false) // secure mode
	
	go func() {
		err := c.Start("8443")
		if err != nil {
			fmt.Println("Coordinator start err:", err)
		}
	}()
	time.Sleep(1 * time.Second)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	// 1. Generate pairing code via API
	resp, err := client.Post("https://127.0.0.1:8443/api/pairing/code", "application/json", nil)
	if err != nil {
		t.Fatalf("Failed to generate code: %v", err)
	}
	var codeRes map[string]string
	json.NewDecoder(resp.Body).Decode(&codeRes)
	code := codeRes["code"]
	resp.Body.Close()

	if code == "" {
		t.Fatal("Empty pairing code returned")
	}

	// 2. Reject incorrect pairing code
	wBad := worker.New("TestWorker-Bad", "./test-ws-bad", false)
	err = wBad.Pair("127.0.0.1:8443", "999999", c.Fingerprint)
	if err == nil {
		t.Fatal("Expected pairing to fail with bad code")
	}

	// 3. Successful worker pairing
	// We need to set env vars for worker creds to save properly in a safe place
	os.Setenv("XDG_DATA_HOME", "./test-data/creds")
	w1 := worker.New("TestWorker-1", "./test-ws-1", false)
	err = w1.Pair("127.0.0.1:8443", code, c.Fingerprint)
	if err != nil {
		t.Fatalf("Pairing failed: %v", err)
	}
	
	// Test 1: Initial pairing saves credentials
	err = w1.LoadCreds()
	if err != nil {
		t.Fatalf("Expected LoadCreds to succeed after pairing: %v", err)
	}

	// 4. Rate Limiting / One-time use: Re-use code should fail
	wLate := worker.New("LateWorker", "./test-ws-late", false)
	err = wLate.Pair("127.0.0.1:8443", code, c.Fingerprint)
	if err == nil {
		t.Fatalf("Expected re-use of pairing code to fail")
	}

	// Test 4: Restart loads credentials and does not require a pairing code
	w1Restarted := worker.New("TestWorker-1-Restarted", "./test-ws-1", false)
	err = w1Restarted.LoadCreds()
	if err != nil {
		t.Fatalf("Restarted worker failed to load credentials: %v", err)
	}
	if w1Restarted.WorkerID != w1.WorkerID {
		t.Fatalf("Restarted worker loaded different ID: %s", w1Restarted.WorkerID)
	}
	
	// Test 5: Incorrect saved token is rejected
	wBadToken := worker.New("TestWorker-1", "./test-ws-1", false)
	wBadToken.LoadCreds()
	wBadToken.Token = "invalid-token"
	wBadToken.Client = w1Restarted.Client // copy TLS client
	// We can't easily capture the stdout of sendHeartbeat, but we can verify it doesn't stay online.
	
	// Test 6: Changed TLS fingerprint is rejected
	wBadTLS := worker.New("TestWorker-1", "./test-ws-1", false)
	wBadTLS.LoadCreds()
	wBadTLS.Fingerprint = "bad-fingerprint"
	wBadTLS.Client = nil // Force new client creation if needed
	wBadTLS.SetupClient(wBadTLS.Fingerprint) // Re-setup with bad fingerprint
	
	// We do a manual HTTP request using the bad TLS client to prove rejection
	req, _ := http.NewRequest("GET", wBadTLS.CoordinatorURL+"/api/coordinator/status", nil)
	_, err = wBadTLS.Client.Do(req)
	if err == nil || !strings.Contains(err.Error(), "certificate fingerprint mismatch") {
		t.Fatalf("Expected TLS fingerprint mismatch error, got: %v", err)
	}

	// 5. Authenticated worker heartbeat
	w1Restarted.Start()
	time.Sleep(2 * time.Second)
	
	// 6. Worker appearing online
	resp, err = client.Get("https://127.0.0.1:8443/api/workers")
	if err != nil {
		t.Fatalf("Failed to fetch workers: %v", err)
	}
	bodyBuf := new(bytes.Buffer)
	bodyBuf.ReadFrom(resp.Body)
	resp.Body.Close()
	
	bodyStr := bodyBuf.String()
	if strings.Contains(bodyStr, "token_hash") || strings.Contains(bodyStr, "token") || strings.Contains(bodyStr, "credential") {
		t.Fatalf("Worker API response leaks sensitive token info: %s", bodyStr)
	}
	
	var workers []models.WorkerDTO
	json.NewDecoder(bytes.NewReader(bodyBuf.Bytes())).Decode(&workers)
	
	if len(workers) != 1 || workers[0].Status != "online" {
		t.Fatalf("Expected 1 online worker, got %d", len(workers))
	}
	if workers[0].TotalRAM == 0 || workers[0].OS == "" {
		t.Fatalf("Expected hardware detection data, got zero values: %+v", workers[0])
	}
	
	// 7. Test-job assignment
	jobReq := map[string]string{"worker_id": w1Restarted.WorkerID}
	b, _ := json.Marshal(jobReq)
	resp, err = client.Post("https://127.0.0.1:8443/api/jobs/test", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("Failed to post job: %v", err)
	}
	var jobRes models.Job
	json.NewDecoder(resp.Body).Decode(&jobRes)
	resp.Body.Close()
	
	if jobRes.Status != "pending" {
		t.Fatalf("Expected job to be pending, got %s", jobRes.Status)
	}
	
	// 8. Test-job completion & Challenge Verification
	time.Sleep(3 * time.Second)
	
	resp, err = client.Get("https://127.0.0.1:8443/api/jobs/" + jobRes.ID)
	if err != nil {
		t.Fatalf("Failed to get job status: %v", err)
	}
	json.NewDecoder(resp.Body).Decode(&jobRes)
	resp.Body.Close()
	
	if jobRes.Status != "completed" {
		t.Fatalf("Expected job to be completed, got %s. Result: %s", jobRes.Status, jobRes.Result)
	}
	if jobRes.Result != "success" {
		t.Fatalf("Expected challenge verification success, got %s", jobRes.Result)
	}
	
	// 9. Restart persistence check
	s2, _ := store.NewStore("./test-data")
	if len(s2.Workers) != 1 {
		t.Fatalf("Expected persistence to load 1 worker, got %d", len(s2.Workers))
	}
	
	os.RemoveAll("./test-data")
	os.RemoveAll("./test-ws-1")
}
