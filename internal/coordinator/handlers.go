package coordinator

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"forgegrid/internal/director"
	"forgegrid/internal/manifest"
	"forgegrid/internal/models"
)

func writeError(w http.ResponseWriter, status int, code, message, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(models.ErrorResponse{
		Code:    code,
		Message: message,
		Detail:  detail,
	})
}

func hashToken(token string) string {
	h := sha256.New()
	h.Write([]byte(token))
	return hex.EncodeToString(h.Sum(nil))
}

func cryptoRandomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generatePairingCode() string {
	b := make([]byte, 4)
	rand.Read(b)
	// We want a 6 digit code
	val := (int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])) & 0x7FFFFFFF
	return fmt.Sprintf("%06d", val%1000000)
}

func (c *Coordinator) handleStart(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (c *Coordinator) handleStatus(w http.ResponseWriter, r *http.Request) {
	c.Store.Mu.RLock()
	defer c.Store.Mu.RUnlock()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ip":          c.IP,
		"identity":    c.Store.CoordinatorCfg.Identity,
		"fingerprint": c.Fingerprint,
	})
}

func (c *Coordinator) handleGenerateCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "")
		return
	}
	code := generatePairingCode()
	c.Store.Mu.Lock()
	c.Store.CoordinatorCfg.PairingCode = code
	c.Store.CoordinatorCfg.PairingExpiry = time.Now().Add(5 * time.Minute)
	c.Store.CoordinatorCfg.PairingFailures = 0
	c.Store.Save()
	c.Store.Mu.Unlock()
	json.NewEncoder(w).Encode(map[string]string{"code": code})
}

func (c *Coordinator) handlePair(w http.ResponseWriter, r *http.Request) {
	// Limit request size to prevent oversized request attacks
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		Code              string `json:"code"`
		NodeName          string `json:"node_name"`
		OS                string `json:"os"`
		OSVersion         string `json:"os_version"`
		CPUModel          string `json:"cpu_model"`
		Architecture      string `json:"architecture"`
		PhysicalCores     int    `json:"physical_cores"`
		LogicalProcessors int    `json:"logical_processors"`
		TotalRAM          uint64 `json:"total_ram"`
		AvailableRAM      uint64 `json:"available_ram"`
		FreeWorkspaceDisk uint64 `json:"free_workspace_disk"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Malformed JSON", err.Error())
		return
	}

	if req.NodeName == "" || len(req.NodeName) > 64 {
		writeError(w, http.StatusBadRequest, "INVALID_NODE_NAME", "Node name is invalid or too long", "")
		return
	}

	c.Store.Mu.Lock()
	defer c.Store.Mu.Unlock()

	if c.Store.CoordinatorCfg.PairingFailures > 5 {
		writeError(w, http.StatusTooManyRequests, "RATE_LIMIT", "Too many failed pairing attempts", "Generate a new code")
		return
	}

	if c.Store.CoordinatorCfg.PairingCode == "" || c.Store.CoordinatorCfg.PairingCode != req.Code || time.Now().After(c.Store.CoordinatorCfg.PairingExpiry) {
		c.Store.CoordinatorCfg.PairingFailures++
		c.Store.Save()
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid or expired pairing code", "")
		return
	}

	// Invalidate code
	c.Store.CoordinatorCfg.PairingCode = ""
	c.Store.CoordinatorCfg.PairingFailures = 0

	// Generate token and ID
	token := cryptoRandomHex(32)
	workerID := "worker-" + cryptoRandomHex(16)

	c.Store.Workers[workerID] = &models.WorkerState{
		ID:                workerID,
		NodeName:          req.NodeName,
		OS:                req.OS,
		OSVersion:         req.OSVersion,
		CPUModel:          req.CPUModel,
		Architecture:      req.Architecture,
		PhysicalCores:     req.PhysicalCores,
		LogicalProcessors: req.LogicalProcessors,
		TotalRAM:          req.TotalRAM,
		AvailableRAM:      req.AvailableRAM,
		FreeWorkspaceDisk: req.FreeWorkspaceDisk,
		TokenHash:         hashToken(token),
		LastSeen:          time.Now(),
		Status:            "online",
	}
	c.Store.Save()

	json.NewEncoder(w).Encode(map[string]string{
		"worker_id": workerID,
		"token":     token,
	})
}

func (c *Coordinator) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		WorkerID     string `json:"worker_id"`
		AvailableRAM uint64 `json:"available_ram"`
		FreeDisk     uint64 `json:"free_workspace_disk"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Malformed JSON", "")
		return
	}

	token := r.Header.Get("Authorization")
	token = strings.TrimPrefix(token, "Bearer ")

	c.Store.Mu.Lock()
	defer c.Store.Mu.Unlock()

	worker, ok := c.Store.Workers[req.WorkerID]
	if !ok || worker.TokenHash != hashToken(token) {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", "")
		return
	}

	worker.AvailableRAM = req.AvailableRAM
	worker.FreeWorkspaceDisk = req.FreeDisk
	worker.LastSeen = time.Now()
	worker.Status = "online"
	c.Store.Save()
	w.WriteHeader(http.StatusOK)
}

func (c *Coordinator) handleListWorkers(w http.ResponseWriter, r *http.Request) {
	c.Store.Mu.RLock()
	defer c.Store.Mu.RUnlock()
	var workers []models.WorkerDTO
	for _, w := range c.Store.Workers {
		workers = append(workers, w.ToDTO())
	}

	sort.Slice(workers, func(i, j int) bool {
		if workers[i].NodeName == workers[j].NodeName {
			return workers[i].ID < workers[j].ID
		}
		return workers[i].NodeName < workers[j].NodeName
	})

	json.NewEncoder(w).Encode(workers)
}

func (c *Coordinator) handleDisconnectWorker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var req struct {
		WorkerID string `json:"worker_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Malformed JSON", "")
		return
	}

	c.Store.Mu.Lock()
	defer c.Store.Mu.Unlock()

	if _, ok := c.Store.Workers[req.WorkerID]; ok {
		delete(c.Store.Workers, req.WorkerID)
		c.Store.Save()
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "disconnected"})
	} else {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Worker not found", "")
	}
}

func (c *Coordinator) handleTestJob(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var req struct {
		WorkerID string `json:"worker_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Malformed JSON", "")
		return
	}

	c.Store.Mu.Lock()
	defer c.Store.Mu.Unlock()

	if _, ok := c.Store.Workers[req.WorkerID]; !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Worker not found", "")
		return
	}

	jobID := "job-" + cryptoRandomHex(16)
	challenge := cryptoRandomHex(32)
	job := &models.Job{
		ID:        jobID,
		WorkerID:  req.WorkerID,
		Task:      "test",
		Status:    models.StatusPending,
		Challenge: challenge,
	}
	c.Store.Jobs[jobID] = job
	c.Store.Save()

	json.NewEncoder(w).Encode(job)
}

func (c *Coordinator) handleListJobs(w http.ResponseWriter, r *http.Request) {
	c.Store.Mu.RLock()
	defer c.Store.Mu.RUnlock()

	workerID := r.URL.Query().Get("worker_id")
	token := r.Header.Get("Authorization")

	if workerID != "" {
		token = strings.TrimPrefix(token, "Bearer ")
		worker, ok := c.Store.Workers[workerID]
		if !ok || worker.TokenHash != hashToken(token) {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", "")
			return
		}
		var jobs []models.Job
		for _, j := range c.Store.Jobs {
			if j.WorkerID == workerID && j.Status == models.StatusPending {
				jobs = append(jobs, *j)
			}
		}
		json.NewEncoder(w).Encode(jobs)
		return
	}

	var jobs []models.Job
	for _, j := range c.Store.Jobs {
		jobs = append(jobs, *j)
	}

	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].ID < jobs[j].ID
	})

	json.NewEncoder(w).Encode(jobs)
}

func (c *Coordinator) handleJobAction(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 65536) // allow larger body for logs
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid path", "")
		return
	}
	jobID := parts[3]

	c.Store.Mu.Lock()
	defer c.Store.Mu.Unlock()

	job, ok := c.Store.Jobs[jobID]
	if !ok || job == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Job not found", "")
		return
	}

	if r.Method == http.MethodGet {
		json.NewEncoder(w).Encode(job)
		return
	}

	if r.Method == http.MethodPost {
		if len(parts) == 5 && parts[4] == "cancel" {
			job.Status = models.StatusCancelRequested
			now := time.Now()
			job.EndTime = &now
			job.Result = "cancelled"
			c.Store.Save()
			json.NewEncoder(w).Encode(job)
			return
		}

		if len(parts) == 5 && parts[4] == "claim" {
			// Worker claiming the job
			token := r.Header.Get("Authorization")
			token = strings.TrimPrefix(token, "Bearer ")
			worker, ok := c.Store.Workers[job.WorkerID]
			if !ok || worker.TokenHash != hashToken(token) {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", "")
				return
			}
			if job.Status != models.StatusPending {
				writeError(w, http.StatusConflict, "CONFLICT", "Job is not pending", "")
				return
			}
			// Atomically assign AttemptID
			job.AttemptID = "attempt-" + cryptoRandomHex(16)
			job.Status = models.StatusClaimed
			now := time.Now()
			job.StartTime = &now
			c.Store.Save()
			json.NewEncoder(w).Encode(job)
			return
		}

		// Worker updating job status
		token := r.Header.Get("Authorization")
		token = strings.TrimPrefix(token, "Bearer ")
		worker, ok := c.Store.Workers[job.WorkerID]
		if !ok || worker.TokenHash != hashToken(token) {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", "")
			return
		}

		var req struct {
			AttemptID string           `json:"attempt_id"`
			Status    models.JobStatus `json:"status"`
			Result    string           `json:"result"`
			Logs      []byte           `json:"logs"`
			LogSeq    int              `json:"log_seq"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Malformed JSON", "")
			return
		}

		// All further updates require matching AttemptID
		if job.AttemptID == "" || req.AttemptID != job.AttemptID {
			writeError(w, http.StatusConflict, "CONFLICT", "Invalid attempt ID or duplicate claim", "")
			return
		}

		// Reject terminal state mutations
		if job.Status == models.StatusCancelled || job.Status == models.StatusCompleted || job.Status == models.StatusFailed {
			writeError(w, http.StatusConflict, "CONFLICT", "Job is already in terminal state", "")
			return
		}

		// Handle valid transitions
		if req.Status == models.StatusRunning || req.Status == models.StatusCancelled || req.Status == models.StatusCompleted || req.Status == models.StatusFailed {
			job.Status = req.Status
		} else {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid status transition", "")
			return
		}

		if req.Result != "" {
			if job.Task == "test" && req.Status == models.StatusCompleted {
				expected := hashToken(job.Challenge)
				if req.Result != expected {
					job.Result = "challenge verification failed"
					job.Logs = append(job.Logs, []byte("\nERROR: Coordinator rejected challenge result")...)
				} else {
					job.Result = "success"
				}
			} else {
				job.Result = req.Result
			}
		}

		// Byte-bounded sequence-numbered logs
		if req.LogSeq > job.LogSeq {
			job.Logs = append(job.Logs, req.Logs...)
			job.LogSeq = req.LogSeq
			// Cap logs at 1MB
			if len(job.Logs) > 1024*1024 {
				job.Logs = job.Logs[len(job.Logs)-1024*1024:]
			}
		}

		if req.Status == models.StatusCancelled || req.Status == models.StatusCompleted || req.Status == models.StatusFailed {
			now := time.Now()
			job.EndTime = &now
		}
		c.Store.Save()
		json.NewEncoder(w).Encode(job)
	}
}

func (c *Coordinator) handleManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	m, err := manifest.Parse(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Failed to parse manifest", err.Error())
		return
	}

	dir := director.New(c.Store)
	err = dir.SubmitManifest(m)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Failed to dispatch manifest tasks", err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "dispatched", "project": m.Project})
}
