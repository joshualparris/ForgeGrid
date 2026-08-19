package coordinator

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"forgegrid/internal/director"
	"forgegrid/internal/manifest"
	"forgegrid/internal/models"
	"forgegrid/internal/network"
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

func (c *Coordinator) isAdminRequest(r *http.Request) bool {
	user, pass, ok := r.BasicAuth()
	return ok && user == "admin" && c.AdminToken != "" && pass == c.AdminToken
}

func (c *Coordinator) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !c.isAdminRequest(r) {
			w.Header().Set("WWW-Authenticate", `Basic realm="ForgeGrid Dashboard"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	}
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

func (c *Coordinator) persistArtifacts(jobID string, artifacts []models.Artifact) []models.Artifact {
	stored := make([]models.Artifact, 0, len(artifacts))
	for i, artifact := range artifacts {
		clean := filepath.Clean(artifact.Path)
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			continue
		}
		if artifact.ContentBase64 != "" {
			b, err := base64.StdEncoding.DecodeString(artifact.ContentBase64)
			if err == nil && int64(len(b)) == artifact.Size && len(b) <= 10*1024*1024 {
				dir := filepath.Join(c.Store.Dir(), "artifacts", jobID)
				if os.MkdirAll(dir, 0700) == nil {
					name := fmt.Sprintf("%03d-%s", i, filepath.Base(clean))
					if artifact.Packaged && artifact.PackageName != "" {
						name = fmt.Sprintf("%03d-%s", i, filepath.Base(artifact.PackageName))
					}
					if os.WriteFile(filepath.Join(dir, name), b, 0600) == nil {
						artifact.DownloadURL = fmt.Sprintf("/api/jobs/%s/artifacts/%d", jobID, i)
					}
				}
			}
		}
		artifact.ContentBase64 = ""
		stored = append(stored, artifact)
	}
	return stored
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

func (c *Coordinator) handleProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "")
		return
	}
	c.Store.Mu.RLock()
	library := c.Store.ProjectLibrary
	projects := sortedProjects(library.Projects)
	c.Store.Mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"connected":    library.Connected,
		"login":        library.Login,
		"last_refresh": library.LastRefresh,
		"last_error":   library.LastError,
		"projects":     projects,
	})
}

func (c *Coordinator) handleProjectsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "")
		return
	}
	if err := c.refreshGitHubProjects(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, "GITHUB_REFRESH_FAILED", "Failed to refresh GitHub projects", maskSecretError(err))
		return
	}
	c.handleProjects(w, httptestLikeGet(r))
}

func httptestLikeGet(r *http.Request) *http.Request {
	next := r.Clone(r.Context())
	next.Method = http.MethodGet
	return next
}

func (c *Coordinator) handleProjectFavorite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		ProjectID string `json:"project_id"`
		Favorite  bool   `json:"favorite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Malformed JSON", "")
		return
	}
	c.Store.Mu.Lock()
	defer c.Store.Mu.Unlock()
	project := c.Store.ProjectLibrary.Projects[req.ProjectID]
	if project == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Project not found", "")
		return
	}
	project.Favorite = req.Favorite
	c.Store.Save()
	json.NewEncoder(w).Encode(project)
}

func (c *Coordinator) handleProjectInspect(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodPost:
	default:
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "")
		return
	}
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "project_id is required", "")
		return
	}
	force := r.Method == http.MethodPost || r.URL.Query().Get("refresh") == "true"
	inspection, err := c.inspectProject(r.Context(), projectID, force)
	if err != nil {
		writeError(w, http.StatusBadGateway, "INSPECTION_FAILED", "Failed to inspect project", maskSecretError(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(inspection)
}

func (c *Coordinator) handleSessionStart(w http.ResponseWriter, r *http.Request) {
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

	agentPort := r.URL.Query().Get("agent_port")
	if agentPort == "" {
		agentPort = "9091"
	}
	controllerURL := c.controllerURLForRequest(r)
	agentURL := fmt.Sprintf("https://%s:%s", c.IP, agentPort)
	agentFP := localAgentBridgeFingerprint()
	bootstrap := fmt.Sprintf(".\\ForgeGrid.exe runner bootstrap -name \"RUNNER_NAME\" -controller %s -code %s -fingerprint %s -agent-url %s -agent-fingerprint %s", controllerURL, code, c.Fingerprint, agentURL, agentFP)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"controller_url":          controllerURL,
		"controller_fingerprint":  c.Fingerprint,
		"agent_url":               agentURL,
		"agent_fingerprint":       agentFP,
		"pairing_code":            code,
		"windows_bootstrap":       bootstrap,
		"reconnect_command":       ".\\ForgeGrid.exe -mode worker",
		"pairing_expires_seconds": "300",
	})
}

func (c *Coordinator) controllerURLForRequest(r *http.Request) string {
	scheme := "https"
	if c.Insecure {
		scheme = "http"
	}
	host := r.Host
	requestHost, requestPort, err := net.SplitHostPort(r.Host)
	if err == nil && (requestHost == "127.0.0.1" || requestHost == "localhost") {
		host = net.JoinHostPort(c.IP, requestPort)
	} else if r.Host == "127.0.0.1" || r.Host == "localhost" {
		host = c.IP
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

func localAgentBridgeFingerprint() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(home, ".local", "share", "forgegrid", "agentbridge", "cert.pem"))
	if err != nil {
		return ""
	}
	fp, err := network.FingerprintFromPEM(b)
	if err != nil {
		return ""
	}
	return fp
}

func (c *Coordinator) handlePair(w http.ResponseWriter, r *http.Request) {
	// Limit request size to prevent oversized request attacks
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		Code              string   `json:"code"`
		NodeName          string   `json:"node_name"`
		OS                string   `json:"os"`
		OSVersion         string   `json:"os_version"`
		CPUModel          string   `json:"cpu_model"`
		Architecture      string   `json:"architecture"`
		PhysicalCores     int      `json:"physical_cores"`
		LogicalProcessors int      `json:"logical_processors"`
		TotalRAM          uint64   `json:"total_ram"`
		AvailableRAM      uint64   `json:"available_ram"`
		FreeWorkspaceDisk uint64   `json:"free_workspace_disk"`
		Labels            []string `json:"labels"`
		Capabilities      []string `json:"capabilities"`
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
		Labels:            append([]string{}, req.Labels...),
		Capabilities:      append([]string{}, req.Capabilities...),
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
		WorkerID     string   `json:"worker_id"`
		AvailableRAM uint64   `json:"available_ram"`
		FreeDisk     uint64   `json:"free_workspace_disk"`
		Labels       []string `json:"labels"`
		Capabilities []string `json:"capabilities"`
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
	if req.Labels != nil {
		worker.Labels = append([]string{}, req.Labels...)
	}
	if req.Capabilities != nil {
		worker.Capabilities = append([]string{}, req.Capabilities...)
	}
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

func (c *Coordinator) handleWorkerPolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var req struct {
		WorkerID string `json:"worker_id"`
		Drain    *bool  `json:"drain"`
		Disabled *bool  `json:"disabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Malformed JSON", "")
		return
	}

	c.Store.Mu.Lock()
	defer c.Store.Mu.Unlock()
	worker, ok := c.Store.Workers[req.WorkerID]
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Worker not found", "")
		return
	}
	if req.Drain != nil {
		worker.Drain = *req.Drain
	}
	if req.Disabled != nil {
		worker.Disabled = *req.Disabled
	}
	c.Store.Save()
	json.NewEncoder(w).Encode(worker.ToDTO())
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

	worker, ok := c.Store.Workers[req.WorkerID]
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Worker not found", "")
		return
	}

	jobID := "job-" + cryptoRandomHex(16)
	challenge := cryptoRandomHex(32)
	job := &models.Job{
		ID:          jobID,
		WorkerID:    req.WorkerID,
		WorkerName:  worker.NodeName,
		ProjectName: "ForgeGrid",
		TaskName:    "Machine check",
		Description: "Built-in connectivity and execution check",
		Task:        "test",
		Status:      models.StatusPending,
		CreatedAt:   time.Now(),
		Challenge:   challenge,
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
			if j.WorkerID == workerID && (j.Status == models.StatusPending || j.Status == models.StatusCancelRequested) {
				jobs = append(jobs, *j)
			}
		}
		json.NewEncoder(w).Encode(jobs)
		return
	}
	if !c.isAdminRequest(r) {
		w.Header().Set("WWW-Authenticate", `Basic realm="ForgeGrid Dashboard"`)
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", "")
		return
	}

	var jobs []models.Job
	for _, j := range c.Store.Jobs {
		jobs = append(jobs, *j)
	}

	sort.Slice(jobs, func(i, j int) bool {
		left := jobs[i].CreatedAt
		right := jobs[j].CreatedAt
		if left.IsZero() {
			left = timeFromJob(jobs[i])
		}
		if right.IsZero() {
			right = timeFromJob(jobs[j])
		}
		return left.After(right)
	})

	json.NewEncoder(w).Encode(jobs)
}

func timeFromJob(job models.Job) time.Time {
	if job.StartTime != nil {
		return *job.StartTime
	}
	if job.EndTime != nil {
		return *job.EndTime
	}
	return time.Time{}
}

func (c *Coordinator) handleJobAction(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 65536) // allow larger body for logs
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid path", "")
		return
	}
	jobID := parts[3]

	if r.Method == http.MethodGet && len(parts) == 6 && parts[4] == "artifacts" {
		if !c.isAdminRequest(r) {
			w.Header().Set("WWW-Authenticate", `Basic realm="ForgeGrid Dashboard"`)
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", "")
			return
		}
		idx := parts[5]
		c.Store.Mu.RLock()
		job, ok := c.Store.Jobs[jobID]
		c.Store.Mu.RUnlock()
		if !ok || job == nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Job not found", "")
			return
		}
		matches, _ := filepath.Glob(filepath.Join(c.Store.Dir(), "artifacts", jobID, idx+"-*"))
		if len(matches) == 0 {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Artifact not found", "")
			return
		}
		http.ServeFile(w, r, matches[0])
		return
	}
	if r.Method == http.MethodGet && len(parts) == 6 && parts[4] == "logs" && parts[5] == "stream" {
		if !c.isAdminRequest(r) {
			w.Header().Set("WWW-Authenticate", `Basic realm="ForgeGrid Dashboard"`)
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", "")
			return
		}
		c.streamJobLogs(w, r, jobID)
		return
	}

	c.Store.Mu.Lock()
	defer c.Store.Mu.Unlock()

	job, ok := c.Store.Jobs[jobID]
	if !ok || job == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Job not found", "")
		return
	}

	if r.Method == http.MethodGet {
		if !c.isAdminRequest(r) {
			w.Header().Set("WWW-Authenticate", `Basic realm="ForgeGrid Dashboard"`)
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", "")
			return
		}
		json.NewEncoder(w).Encode(job)
		return
	}

	if r.Method == http.MethodDelete {
		if !c.isAdminRequest(r) {
			w.Header().Set("WWW-Authenticate", `Basic realm="ForgeGrid Dashboard"`)
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", "")
			return
		}
		c.Store.Mu.Unlock() // Unlock before calling DeleteJob which takes the lock
		err := c.Store.DeleteJob(jobID)
		c.Store.Mu.Lock() // Relock for the defer to unlock

		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to delete job", err.Error())
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
		return
	}

	if r.Method == http.MethodPost {
		if len(parts) == 5 && parts[4] == "cancel" {
			if !c.isAdminRequest(r) {
				w.Header().Set("WWW-Authenticate", `Basic realm="ForgeGrid Dashboard"`)
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", "")
				return
			}
			job.Status = models.StatusCancelRequested
			job.Result = "cancellation requested"
			c.Store.Save()
			json.NewEncoder(w).Encode(job)
			return
		}

		if len(parts) == 5 && parts[4] == "retry" {
			if !c.isAdminRequest(r) {
				w.Header().Set("WWW-Authenticate", `Basic realm="ForgeGrid Dashboard"`)
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", "")
				return
			}
			if job.Status != models.StatusFailed {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Only failed jobs can be retried", "")
				return
			}
			if job.MaxRetries > 0 && job.RetryCount >= job.MaxRetries {
				writeError(w, http.StatusBadRequest, "RETRY_LIMIT", "Retry limit reached", "")
				return
			}
			retry := *job
			retry.ID = "job-" + cryptoRandomHex(16)
			retry.AttemptID = ""
			retry.Status = models.StatusPending
			retry.StartTime = nil
			retry.EndTime = nil
			retry.Result = ""
			retry.Logs = nil
			retry.LogSeq = 0
			retry.Artifacts = nil
			retry.PushedBranch = ""
			retry.PRURL = ""
			retry.RetryOf = job.ID
			retry.RetryCount = job.RetryCount + 1
			retry.CreatedAt = time.Now()
			c.Store.Jobs[retry.ID] = &retry
			c.Store.Save()
			json.NewEncoder(w).Encode(&retry)
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
			AttemptID         string                    `json:"attempt_id"`
			Status            models.JobStatus          `json:"status"`
			Result            string                    `json:"result"`
			Logs              []byte                    `json:"logs"`
			LogSeq            int                       `json:"log_seq"`
			Artifacts         []models.Artifact         `json:"artifacts"`
			PushedBranch      string                    `json:"pushed_branch"`
			PRURL             string                    `json:"pr_url"`
			Stages            []models.JobStage         `json:"stages"`
			CurrentStage      *int                      `json:"current_stage"`
			FailureCode       string                    `json:"failure_code"`
			BaseBranch        string                    `json:"base_branch"`
			ResolvedBase      string                    `json:"resolved_base"`
			WorkBranch        string                    `json:"work_branch"`
			CommitSHA         string                    `json:"commit_sha"`
			WorkspaceID       string                    `json:"workspace_id"`
			WorkspaceRetained bool                      `json:"workspace_retained"`
			ChangedFiles      []models.ChangedFile      `json:"changed_files"`
			ValidationResults []models.ValidationResult `json:"validation_results"`
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

		if req.Stages != nil {
			job.Stages = req.Stages
		}
		if req.CurrentStage != nil {
			job.CurrentStage = *req.CurrentStage
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
			if req.Artifacts != nil {
				job.Artifacts = c.persistArtifacts(job.ID, req.Artifacts)
			}
			if req.PushedBranch != "" {
				job.PushedBranch = req.PushedBranch
			}
			if req.PRURL != "" {
				job.PRURL = req.PRURL
			}
			if req.FailureCode != "" {
				job.FailureCode = req.FailureCode
			}
			if req.BaseBranch != "" {
				job.BaseBranch = req.BaseBranch
			}
			if req.ResolvedBase != "" {
				job.ResolvedBase = req.ResolvedBase
			}
			if req.WorkBranch != "" {
				job.WorkBranch = req.WorkBranch
			}
			if req.CommitSHA != "" {
				job.CommitSHA = req.CommitSHA
			}
			if req.WorkspaceID != "" {
				job.WorkspaceID = req.WorkspaceID
			}
			if req.WorkspaceRetained {
				job.WorkspaceRetained = true
			}
			if req.ChangedFiles != nil {
				job.ChangedFiles = req.ChangedFiles
			}
			if req.ValidationResults != nil {
				job.ValidationResults = req.ValidationResults
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

func (c *Coordinator) streamJobLogs(w http.ResponseWriter, r *http.Request, jobID string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "STREAM_UNSUPPORTED", "Streaming unsupported", "")
		return
	}
	offset := 0
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			c.Store.Mu.RLock()
			job := c.Store.Jobs[jobID]
			if job == nil {
				c.Store.Mu.RUnlock()
				fmt.Fprintf(w, "event: error\ndata: job not found\n\n")
				flusher.Flush()
				return
			}
			logs := append([]byte{}, job.Logs...)
			status := job.Status
			c.Store.Mu.RUnlock()
			if offset < len(logs) {
				chunk := strings.ReplaceAll(string(logs[offset:]), "\n", "\\n")
				fmt.Fprintf(w, "event: logs\ndata: %s\n\n", chunk)
				offset = len(logs)
				flusher.Flush()
			}
			if status == models.StatusCompleted || status == models.StatusFailed || status == models.StatusCancelled {
				fmt.Fprintf(w, "event: done\ndata: %s\n\n", status)
				flusher.Flush()
				return
			}
		}
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
	c.markProjectUsed(m.Repository.URL)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "dispatched", "project": m.Project})
}

func (c *Coordinator) markProjectUsed(cloneURL string) {
	if strings.TrimSpace(cloneURL) == "" {
		return
	}
	c.Store.Mu.Lock()
	defer c.Store.Mu.Unlock()
	for _, project := range c.Store.ProjectLibrary.Projects {
		if project != nil && project.CloneURL == cloneURL {
			project.LastUsedAt = time.Now()
			c.Store.Save()
			return
		}
	}
}
