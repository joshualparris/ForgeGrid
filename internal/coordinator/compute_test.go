package coordinator

import (
	"bytes"
	"encoding/json"
	"forgegrid/internal/models"
	"forgegrid/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAtomicClaiming(t *testing.T) {
	dir, err := os.MkdirTemp("", "fg-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	s, err := store.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Workers["worker-1"] = &models.WorkerState{
		ID:        "worker-1",
		TokenHash: hashToken("token-1"),
	}
	s.Workers["worker-2"] = &models.WorkerState{
		ID:        "worker-2",
		TokenHash: hashToken("token-2"),
	}

	jobID := "job-1"
	s.Jobs[jobID] = &models.Job{
		ID:     jobID,
		Status: models.StatusPending,
	}

	c := &Coordinator{
		Store: s,
	}

	var wg sync.WaitGroup
	successes := int32(0)
	conflicts := int32(0)

	// Simulate 10 concurrent claim attempts
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(workerNum int) {
			defer wg.Done()

			reqBody := map[string]string{"worker_id": "worker-1"}
			if workerNum%2 == 0 {
				reqBody["worker_id"] = "worker-2"
			}
			bodyBytes, _ := json.Marshal(reqBody)

			req := httptest.NewRequest("POST", "/api/jobs/"+jobID+"/claim", bytes.NewReader(bodyBytes))
			if workerNum%2 == 0 {
				req.Header.Set("Authorization", "Bearer token-2")
			} else {
				req.Header.Set("Authorization", "Bearer token-1")
			}

			w := httptest.NewRecorder()
			
			// Extract handler logic to test the specific route
			mux := http.NewServeMux()
			mux.HandleFunc("/api/jobs/", c.handleJobAction)
			mux.ServeHTTP(w, req)

			if w.Code == http.StatusOK {
				atomic.AddInt32(&successes, 1)
			} else if w.Code == http.StatusConflict {
				atomic.AddInt32(&conflicts, 1)
			}
		}(i)
	}
	wg.Wait()

	if successes != 1 {
		t.Errorf("Expected exactly 1 successful claim, got %d", successes)
	}
	if conflicts != 9 {
		t.Errorf("Expected exactly 9 conflicts, got %d", conflicts)
	}

	if s.Jobs[jobID].Status != models.StatusClaimed {
		t.Errorf("Expected job status to be CLAIMED")
	}
}

func TestBatchConcurrency(t *testing.T) {
	dir, err := os.MkdirTemp("", "fg-batch-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	s, err := store.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	workerIDs := []string{"w1", "w2", "w3", "w4", "w5"}
	for _, id := range workerIDs {
		s.Workers[id] = &models.WorkerState{
			ID:        id,
			TokenHash: hashToken("token-" + id),
		}
	}

	for i := 0; i < 100; i++ {
		jobID := "job-compute-" + cryptoRandomHex(8)
		s.Jobs[jobID] = &models.Job{
			ID:         jobID,
			Task:       "compute.test",
			Status:     models.StatusPending,
		}
	}

	c := &Coordinator{
		Store: s,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/jobs/", c.handleJobAction)
	mux.HandleFunc("/api/jobs", c.handleListJobs)

	var wg sync.WaitGroup
	var completedCount int32
	var claimedCount int32

	workerRoutine := func(workerID string) {
		defer wg.Done()
		for {
			req := httptest.NewRequest("GET", "/api/jobs?worker_id="+workerID, nil)
			req.Header.Set("Authorization", "Bearer token-"+workerID)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			var jobs []models.Job
			json.Unmarshal(w.Body.Bytes(), &jobs)

			if len(jobs) == 0 {
				break // No more jobs
			}

			// Try to claim the first job
			job := jobs[0]
			reqBody := map[string]string{"worker_id": workerID}
			bodyBytes, _ := json.Marshal(reqBody)

			claimReq := httptest.NewRequest("POST", "/api/jobs/"+job.ID+"/claim", bytes.NewReader(bodyBytes))
			claimReq.Header.Set("Authorization", "Bearer token-"+workerID)
			claimRes := httptest.NewRecorder()
			mux.ServeHTTP(claimRes, claimReq)

			if claimRes.Code == http.StatusOK {
				atomic.AddInt32(&claimedCount, 1)

				var claimedJob models.Job
				json.Unmarshal(claimRes.Body.Bytes(), &claimedJob)

				// Complete it
				updateReqBody := map[string]interface{}{
					"attempt_id": claimedJob.AttemptID,
					"status":     models.StatusCompleted,
				}
				upBytes, _ := json.Marshal(updateReqBody)
				upReq := httptest.NewRequest("POST", "/api/jobs/"+job.ID, bytes.NewReader(upBytes))
				upReq.Header.Set("Authorization", "Bearer token-"+workerID)
				upRes := httptest.NewRecorder()
				mux.ServeHTTP(upRes, upReq)

				if upRes.Code == http.StatusOK {
					atomic.AddInt32(&completedCount, 1)
				}
			}
		}
	}

	// 20 concurrent worker routines
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go workerRoutine(workerIDs[i%len(workerIDs)])
	}

	wg.Wait()

	if claimedCount != 100 {
		t.Errorf("Expected exactly 100 successful claims, got %d", claimedCount)
	}
	if completedCount != 100 {
		t.Errorf("Expected exactly 100 successful completions, got %d", completedCount)
	}
}

func TestDuplicateCompletion(t *testing.T) {
	dir, err := os.MkdirTemp("", "fg-dup-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	s, err := store.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Workers["worker-1"] = &models.WorkerState{
		ID:        "worker-1",
		TokenHash: hashToken("token-1"),
	}

	jobID := "job-dup"
	attemptID := "attempt-1"
	s.Jobs[jobID] = &models.Job{
		ID:        jobID,
		Status:    models.StatusClaimed,
		WorkerID:  "worker-1",
		AttemptID: attemptID,
	}

	c := &Coordinator{Store: s}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/jobs/", c.handleJobAction)

	updateReqBody := map[string]interface{}{
		"attempt_id": attemptID,
		"status":     models.StatusCompleted,
	}
	upBytes, _ := json.Marshal(updateReqBody)

	// First completion
	req1 := httptest.NewRequest("POST", "/api/jobs/"+jobID, bytes.NewReader(upBytes))
	req1.Header.Set("Authorization", "Bearer token-1")
	w1 := httptest.NewRecorder()
	mux.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("First completion failed, got %d", w1.Code)
	}

	// Second duplicate completion
	req2 := httptest.NewRequest("POST", "/api/jobs/"+jobID, bytes.NewReader(upBytes))
	req2.Header.Set("Authorization", "Bearer token-1")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)

	if w2.Code != http.StatusConflict {
		t.Errorf("Expected duplicate completion to be rejected with 409 Conflict, got %d", w2.Code)
	}
}
