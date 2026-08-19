package coordinator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"forgegrid/internal/models"
	"forgegrid/internal/store"
)

func testCoordinator(t *testing.T) *Coordinator {
	t.Helper()
	s, err := store.NewStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	c := &Coordinator{Store: s, AdminToken: "admin-token"}
	s.Workers["worker-1"] = &models.WorkerState{
		ID:        "worker-1",
		NodeName:  "probook",
		TokenHash: hashToken("worker-token"),
		Status:    "online",
		LastSeen:  time.Now(),
	}
	s.Jobs["job-1"] = &models.Job{
		ID:        "job-1",
		WorkerID:  "worker-1",
		Task:      "test",
		Status:    models.StatusPending,
		CreatedAt: time.Now(),
	}
	return c
}

func TestAdminRoutesRejectAnonymousRequests(t *testing.T) {
	c := testCoordinator(t)

	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		method  string
		path    string
	}{
		{"workers", c.requireAdmin(c.handleListWorkers), http.MethodGet, "/api/workers"},
		{"jobs", c.handleListJobs, http.MethodGet, "/api/jobs"},
		{"job detail", c.handleJobAction, http.MethodGet, "/api/jobs/job-1"},
		{"job delete", c.handleJobAction, http.MethodDelete, "/api/jobs/job-1"},
		{"job retry", c.handleJobAction, http.MethodPost, "/api/jobs/job-1/retry"},
		{"job cancel", c.handleJobAction, http.MethodPost, "/api/jobs/job-1/cancel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			tc.handler(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestWorkerCanOnlyPollOwnPendingAndCancelRequestedJobs(t *testing.T) {
	c := testCoordinator(t)
	c.Store.Jobs["job-2"] = &models.Job{
		ID:       "job-2",
		WorkerID: "worker-1",
		Task:     "execute",
		Status:   models.StatusCancelRequested,
	}
	c.Store.Jobs["job-3"] = &models.Job{
		ID:       "job-3",
		WorkerID: "worker-1",
		Task:     "execute",
		Status:   models.StatusRunning,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/jobs?worker_id=worker-1", nil)
	req.Header.Set("Authorization", "Bearer worker-token")
	w := httptest.NewRecorder()
	c.handleListJobs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var jobs []models.Job
	if err := json.NewDecoder(w.Body).Decode(&jobs); err != nil {
		t.Fatalf("decode jobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want pending + cancel requested", len(jobs))
	}
	for _, job := range jobs {
		if job.Status == models.StatusRunning {
			t.Fatalf("running job leaked through worker poll: %+v", job)
		}
	}
}
