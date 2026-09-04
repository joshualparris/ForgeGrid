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
	"forgegrid/internal/store"
	fgupdate "forgegrid/internal/update"
	"forgegrid/internal/version"
)

func TestUpdateStatusExplainsBusyAndAvailableWorkers(t *testing.T) {
	c := testCoordinator(t)
	c.Store.Jobs["job-1"].Status = models.StatusRunning
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
	writeMultiArchUpdateManifest(t, dir, version, true)
}

// writeMultiArchUpdateManifest writes a manifest with a windows/amd64
// artifact and, when includeX86 is true, an additional windows/386
// artifact with different content (and therefore a different checksum),
// so tests can prove each architecture only ever matches its own artifact.
func writeMultiArchUpdateManifest(t *testing.T, dir, version string, includeX86 bool) {
	t.Helper()
	amd64Bin := filepath.Join(dir, "Windows", "ForgeGrid.exe")
	if err := os.MkdirAll(filepath.Dir(amd64Bin), 0700); err != nil {
		t.Fatal(err)
	}
	amd64Content := []byte("windows amd64 exe")
	if err := os.WriteFile(amd64Bin, amd64Content, 0600); err != nil {
		t.Fatal(err)
	}
	amd64Hash := sha256.Sum256(amd64Content)
	artifacts := []map[string]interface{}{{
		"role":         "worker",
		"platform":     "windows",
		"architecture": "amd64",
		"sha256":       hex.EncodeToString(amd64Hash[:]),
		"path":         "Windows/ForgeGrid.exe",
	}}
	if includeX86 {
		x86Bin := filepath.Join(dir, "Windows", "ForgeGrid-x86.exe")
		x86Content := []byte("windows 386 exe")
		if err := os.WriteFile(x86Bin, x86Content, 0600); err != nil {
			t.Fatal(err)
		}
		x86Hash := sha256.Sum256(x86Content)
		artifacts = append(artifacts, map[string]interface{}{
			"role":         "worker",
			"platform":     "windows",
			"architecture": "386",
			"sha256":       hex.EncodeToString(x86Hash[:]),
			"path":         "Windows/ForgeGrid-x86.exe",
		})
	}
	manifest := map[string]interface{}{
		"schema_version": "1",
		"product":        "ForgeGrid",
		"version":        version,
		"commit":         "test",
		"protocol":       "1",
		"artifacts":      artifacts,
	}
	b, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "update-manifest.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestQueueWorkerUpdateSelectsArchitectureSpecificArtifact(t *testing.T) {
	c := testCoordinator(t)
	c.Store.Jobs = map[string]*models.Job{}
	c.Store.Workers["worker-1"].OS = "windows"
	c.Store.Workers["worker-1"].Architecture = "amd64"
	c.Store.Workers["worker-1"].Version = version.InfoData{Version: "0.8.0", Protocol: "1"}
	c.Store.Workers["worker-386"] = &models.WorkerState{
		ID:           "worker-386",
		NodeName:     "Laptop10",
		OS:           "windows",
		Architecture: "386",
		Version:      version.InfoData{Version: "0.8.0", Protocol: "1"},
		TokenHash:    hashToken("worker-386-token"),
		Status:       "online",
	}
	c.Store.Workers["worker-arm64"] = &models.WorkerState{
		ID:           "worker-arm64",
		NodeName:     "UnsupportedArchWorker",
		OS:           "windows",
		Architecture: "arm64",
		Version:      version.InfoData{Version: "0.8.0", Protocol: "1"},
		TokenHash:    hashToken("worker-arm64-token"),
		Status:       "online",
	}
	writeMultiArchUpdateManifest(t, c.Store.Dir(), "0.9.0", true)

	body := bytes.NewBufferString(`{"worker_ids":["worker-1","worker-386","worker-arm64"],"policy":"idle"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/updates/workers", body)
	w := httptest.NewRecorder()
	c.handleQueueWorkerUpdates(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("queue status = %d, body=%s", w.Code, w.Body.String())
	}

	amd64Req := c.Store.Workers["worker-1"].UpdateRequest
	x86Req := c.Store.Workers["worker-386"].UpdateRequest
	if amd64Req == nil || x86Req == nil {
		t.Fatalf("expected both compatible workers to be queued: amd64=%v 386=%v", amd64Req, x86Req)
	}
	if amd64Req.ArtifactArch != "amd64" || x86Req.ArtifactArch != "386" {
		t.Fatalf("wrong artifact arch assigned: amd64Req=%q x86Req=%q", amd64Req.ArtifactArch, x86Req.ArtifactArch)
	}
	if amd64Req.ArtifactSHA256 == x86Req.ArtifactSHA256 {
		t.Fatalf("amd64 and 386 workers were queued the same artifact checksum")
	}

	if req := c.Store.Workers["worker-arm64"].UpdateRequest; req != nil {
		t.Fatalf("expected unsupported architecture worker to NOT be queued, got %+v", req)
	}
	view := c.updateViewLocked(c.Store.Workers["worker-arm64"], mustLoadManifest(t, c.Store.Dir()))
	if view.Status != "unavailable" || view.ArtifactPresent {
		t.Fatalf("expected unsupported arch worker to fail closed, got status=%q artifact_present=%v reason=%q", view.Status, view.ArtifactPresent, view.Reason)
	}
}

func mustLoadManifest(t *testing.T, dir string) *fgupdate.Manifest {
	t.Helper()
	m, err := fgupdate.LoadManifest(filepath.Join(dir, "update-manifest.json"))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	return m
}

func TestWorkerUpdateRequestSurvivesStoreReload(t *testing.T) {
	c := testCoordinator(t)
	c.Store.Workers["worker-1"].UpdateRequest = &models.WorkerUpdateRequest{
		ID:            "update-abc",
		TargetVersion: "0.9.0",
		Status:        "running",
		Message:       "Update state changed to APPLYING",
	}
	if err := c.Store.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	reloaded, err := store.NewStore(c.Store.Dir())
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	req := reloaded.Workers["worker-1"].UpdateRequest
	if req == nil {
		t.Fatalf("update request lost across reload")
	}
	if req.Status != "running" {
		t.Fatalf("reload silently changed update status: got %q, want %q (only an explicit worker report may change this)", req.Status, "running")
	}
}
