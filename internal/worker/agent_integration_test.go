package worker

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

)

func TestFakeAgentIntegrationPipeline(t *testing.T) {
	ws := t.TempDir()

	// Setup fake git repo to clone from
	remoteRepo := filepath.Join(ws, "remote-repo")
	if err := os.MkdirAll(remoteRepo, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(remoteRepo, "README.md"), []byte("Hello"), 0644)
	
	gitScript := `#!/bin/bash
git init
git branch -m main
git config user.email "test@example.com"
git config user.name "Test User"
git add README.md
git commit -m "Initial commit"
`
	scriptPath := filepath.Join(remoteRepo, "setup.sh")
	os.WriteFile(scriptPath, []byte(gitScript), 0755)
	
	cmd := exec.Command("bash", "./setup.sh")
	cmd.Dir = remoteRepo
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to setup git repo: %v", err)
	}

	// Coordinator mock
	mux := http.NewServeMux()
	mux.HandleFunc("/api/worker/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	})
	
	// Mock job assignment
	jobAssigned := false
	mux.HandleFunc("/api/jobs", func(w http.ResponseWriter, r *http.Request) {
		if jobAssigned {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
			return
		}
		jobAssigned = true
		w.Header().Set("Content-Type", "application/json")
		
		w.Write([]byte(`[{
			"id": "job-fake-1",
			"status": "PENDING",
			"task_name": "ai_task",
			"task": "execute",
			"profile": "ai",
			"agent_requested": "fake",
			"repository_url": "` + remoteRepo + `",
			"base_commit": "main",
			"branch_name": "forgegrid/test-branch",
			"commit_changes": true,
			"push_changes": false,
			"commit_message": "Agent changed something",
			"timeout_seconds": 60,
			"stages": [
				{
					"name": "Agent",
					"profile": "ai",
					"timeout_seconds": 60,
					"parameters": {"prompt": "CHANGE the file"}
				}
			]
		}]`))
	})

	mux.HandleFunc("/api/jobs/job-fake-1/claim", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"attempt_id": "attempt-1"}`))
	})

	jobStatusCalls := make(chan string, 10)
	mux.HandleFunc("/api/jobs/job-fake-1", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		jobStatusCalls <- string(body)
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Logf("Unhandled request: %s %s", r.Method, r.URL.Path)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	worker := New("test-worker", ws, true)
	worker.CoordinatorURL = srv.URL
	worker.Token = "secret"
	worker.Workspace = ws
	worker.allowPush = false
	worker.allowBootstrap = true
	worker.SetGitPolicy(remoteRepo, true)
	worker.capabilityAllow = []string{"agent:fake", "git"}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	worker.Start()
	defer worker.Stop()

	// Wait for completion
	completed := false
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for job completion")
		case msg := <-jobStatusCalls:
			t.Logf("Job status update: %s", msg)
			if strings.Contains(msg, `"status":"COMPLETED"`) {
				completed = true
				if !strings.Contains(msg, `"agent_actual":"fake"`) {
					t.Errorf("Expected agent_actual to be fake, got %s", msg)
				}
			} else if strings.Contains(msg, `"status":"FAILED"`) {
				t.Fatalf("Job failed: %s", msg)
			}
		}
		if completed {
			break
		}
	}
}
