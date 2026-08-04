package worker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"forgegrid/internal/models"
)

func TestDirectoryTraversalBlocked(t *testing.T) {
	ws := t.TempDir()
	w := &Worker{
		Workspace: ws,
		WorkerID:  "test-worker",
	}

	job := models.Job{
		ID:             "job-1",
		CommandLinux:   "echo 'hello'",
		CommandWindows: "echo 'hello'",
		Artefacts:      []string{"../../etc/passwd"},
	}

	// We can test the sanitization logic directly.
	err := w.collectArtefacts(job.ID, job.Artefacts, "http://dummy")
	if err == nil || !strings.Contains(err.Error(), "escapes workspace") {
		t.Errorf("expected directory traversal error, got %v", err)
	}
}

func TestJobCancellation(t *testing.T) {
	ws := t.TempDir()
	
	// Create a dummy coordinator to intercept status updates
	statuses := []string{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "job-cancel-test") {
			var req struct {
				Status string `json:"status"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if req.Status != "" {
				statuses = append(statuses, req.Status)
			}
		}
	}))
	defer ts.Close()

	w := &Worker{
		Workspace:      ws,
		WorkerID:       "test-worker",
		Client:         ts.Client(),
		CoordinatorURL: ts.URL,
	}

	cmd := "sleep 10"
	if os.PathSeparator == '\\' {
		cmd = "timeout 10"
	}

	job := models.Job{
		ID:             "job-cancel-test",
		CommandLinux:   cmd,
		CommandWindows: cmd,
	}

	// Start job execution in a goroutine
	go w.executeJob(job)
	
	time.Sleep(100 * time.Millisecond) // Let it start

	// Cancel the job
	w.cancelJob(job.ID)

	time.Sleep(200 * time.Millisecond) // Wait for cancellation to take effect

	// Check that status was updated to cancelled or failed (due to kill)
	foundEndState := false
	for _, s := range statuses {
		if s == "failed" || s == "cancelled" {
			foundEndState = true
			break
		}
	}

	if !foundEndState {
		t.Errorf("expected job to end after cancellation, statuses: %v", statuses)
	}
}

func TestCommandExecutionAndLogStreaming(t *testing.T) {
	ws := t.TempDir()
	
	logsReceived := []string{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Status string   `json:"status"`
			Logs   []string `json:"logs"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		logsReceived = append(logsReceived, req.Logs...)
	}))
	defer ts.Close()

	worker := &Worker{
		Workspace:      ws,
		WorkerID:       "test-worker",
		Client:         ts.Client(),
		CoordinatorURL: ts.URL,
	}

	cmd := "echo stream test"
	if os.PathSeparator == '\\' {
		cmd = "cmd.exe /c echo stream test"
	}

	job := models.Job{
		ID:             "job-exec-test",
		CommandLinux:   cmd,
		CommandWindows: cmd,
	}

	worker.executeJob(job)

	// Wait a moment for async logs
	time.Sleep(200 * time.Millisecond)

	found := false
	for _, l := range logsReceived {
		if strings.Contains(l, "stream test") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'stream test' in logs, got %v", logsReceived)
	}
}
