package coordinator

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgegrid/internal/models"
	"forgegrid/internal/version"
)

func TestUpdateStatusExplainsBusyAndAvailableWorkers(t *testing.T) {
	c := testCoordinator(t)
	c.Store.Workers["worker-1"].OS = "windows"
	c.Store.Workers["worker-1"].Architecture = "amd64"
	c.Store.Workers["worker-1"].Version = version.InfoData{Version: "0.8.0", Protocol: "1"}
	c.Store.Workers["worker-2"] = &models.WorkerState{
		ID:           "worker-2",
		NodeName:     "ThinkPad-Lenovo",
		OS:           "windows",
		Architecture: "amd64",
		Version:      version.InfoData{Version: "0.7.0", Protocol: "1"},
		TokenHash:    hashToken("worker-2-token"),
		Status:       "online",
	}
	writeUpdateManifest(t, c.Store.Dir(), "0.9.0")

	req := httptest.NewRequest(http.MethodGet, "/api/updates/status", nil)
	w := httptest.NewRecorder()
	c.handleUpdateStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Workers []struct {
			WorkerID string `json:"worker_id"`
			Status   string `json:"status"`
			Reason   string `json:"reason"`
		} `json:"workers"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	statuses := map[string]string{}
	reasons := map[string]string{}
	for _, worker := range body.Workers {
		statuses[worker.WorkerID] = worker.Status
		reasons[worker.WorkerID] = worker.Reason
	}
	if statuses["worker-1"] != "busy" {
		t.Fatalf("worker-1 status = %q reason=%q", statuses["worker-1"], reasons["worker-1"])
	}
	if statuses["worker-2"] != "available" {
		t.Fatalf("worker-2 status = %q reason=%q", statuses["worker-2"], reasons["worker-2"])
	}
}

func TestQueueWorkerUpdateAndWorkerPoll(t *testing.T) {
	c := testCoordinator(t)
	c.Store.Jobs = map[string]*models.Job{}
	c.Store.Workers["worker-1"].OS = "windows"
	c.Store.Workers["worker-1"].Architecture = "amd64"
	c.Store.Workers["worker-1"].Version = version.InfoData{Version: "0.8.0", Protocol: "1"}
	writeUpdateManifest(t, c.Store.Dir(), "0.9.0")

	body := bytes.NewBufferString(`{"worker_ids":["worker-1"],"policy":"idle"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/updates/workers", body)
	w := httptest.NewRecorder()
	c.handleQueueWorkerUpdates(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("queue status = %d, body=%s", w.Code, w.Body.String())
	}
	if c.Store.Workers["worker-1"].UpdateRequest == nil {
		t.Fatalf("expected queued update")
	}

	poll := httptest.NewRequest(http.MethodGet, "/api/updates/worker?worker_id=worker-1", nil)
	poll.Header.Set("Authorization", "Bearer worker-token")
	pollW := httptest.NewRecorder()
	c.handleWorkerUpdatePoll(pollW, poll)
	if pollW.Code != http.StatusOK {
		t.Fatalf("poll status = %d, body=%s", pollW.Code, pollW.Body.String())
	}
	if !strings.Contains(pollW.Body.String(), `"target_version":"0.9.0"`) {
		t.Fatalf("poll body missing update: %s", pollW.Body.String())
	}
}

func writeUpdateManifest(t *testing.T, dir, version string) {
	t.Helper()
	bin := filepath.Join(dir, "Windows", "ForgeGrid.exe")
	if err := os.MkdirAll(filepath.Dir(bin), 0700); err != nil {
		t.Fatal(err)
	}
	content := []byte("windows exe")
	if err := os.WriteFile(bin, content, 0600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(content)
	manifest := map[string]interface{}{
		"schema_version": "1",
		"product":        "ForgeGrid",
		"version":        version,
		"commit":         "test",
		"protocol":       "1",
		"artifacts": []map[string]interface{}{{
			"role":         "worker",
			"platform":     "windows",
			"architecture": "amd64",
			"sha256":       hex.EncodeToString(h[:]),
			"path":         "Windows/ForgeGrid.exe",
		}},
	}
	b, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "update-manifest.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
}
