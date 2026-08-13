package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"forgegrid/internal/execution"
	"forgegrid/internal/gitworkspace"
	"forgegrid/internal/models"
	"forgegrid/internal/network"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

type Worker struct {
	CoordinatorURL string
	WorkerID       string
	Token          string
	NodeName       string
	Client         *http.Client
	Workspace      string
	Insecure       bool
	Fingerprint    string

	mu           sync.Mutex
	activeJobs   map[string]context.CancelFunc
	allowedRepos map[string]bool
	allowPush    bool
	Labels       []string
	Capabilities []string
}

type WorkerCredentials struct {
	WorkerID       string `json:"worker_id"`
	Token          string `json:"token"`
	CoordinatorURL string `json:"coordinator_url"`
	Fingerprint    string `json:"fingerprint"`
	NodeName       string `json:"node_name"`
	Insecure       bool   `json:"insecure"`
}

type Policy struct {
	AllowedRepos []string `json:"allowed_repos"`
	AllowPush    bool     `json:"allow_push"`
	Labels       []string `json:"labels"`
	Capabilities []string `json:"capabilities"`
}

func getWorkerCredsPath() string {
	return filepath.Join(getWorkerDataDir(), "worker_creds.json")
}

func WorkerCredsPath() string {
	return getWorkerCredsPath()
}

func getWorkerPolicyPath() string {
	return filepath.Join(getWorkerDataDir(), "worker_policy.json")
}

func WorkerPolicyPath() string {
	return getWorkerPolicyPath()
}

func getWorkerDataDir() string {
	var dir string
	if runtime.GOOS == "windows" {
		dir = os.Getenv("LOCALAPPDATA")
		if dir == "" {
			dir = os.Getenv("APPDATA")
		}
	} else {
		dir = os.Getenv("XDG_DATA_HOME")
		if dir == "" {
			home, _ := os.UserHomeDir()
			dir = filepath.Join(home, ".local", "share")
		}
	}
	if dir == "" {
		dir = "."
	}
	name := "ForgeGrid"
	if runtime.GOOS == "linux" {
		name = "forgegrid"
	}
	return filepath.Join(dir, name)
}

func WorkerDataDir() string {
	return getWorkerDataDir()
}

func ResetCredentials() error {
	path := getWorkerCredsPath()
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func New(nodeName, workspace string, insecure bool) *Worker {
	w := &Worker{
		NodeName:     nodeName,
		Workspace:    workspace,
		Insecure:     insecure,
		activeJobs:   make(map[string]context.CancelFunc),
		allowedRepos: parseRepoAllowlist(os.Getenv("FORGEGRID_ALLOWED_REPOS")),
		allowPush:    os.Getenv("FORGEGRID_ALLOW_PUSH") == "true",
		Labels:       parseCSV(os.Getenv("FORGEGRID_LABELS")),
		Capabilities: parseCSV(os.Getenv("FORGEGRID_CAPABILITIES")),
	}
	w.LoadPolicy()
	if envRepos := strings.TrimSpace(os.Getenv("FORGEGRID_ALLOWED_REPOS")); envRepos != "" {
		w.allowedRepos = parseRepoAllowlist(envRepos)
	}
	if os.Getenv("FORGEGRID_ALLOW_PUSH") == "true" {
		w.allowPush = true
	}
	if labels := strings.TrimSpace(os.Getenv("FORGEGRID_LABELS")); labels != "" {
		w.Labels = parseCSV(labels)
	}
	if capabilities := strings.TrimSpace(os.Getenv("FORGEGRID_CAPABILITIES")); capabilities != "" {
		w.Capabilities = parseCSV(capabilities)
	}
	return w
}

func parseCSV(raw string) []string {
	var vals []string
	for _, repo := range strings.Split(raw, ",") {
		repo = strings.TrimSpace(repo)
		if repo != "" {
			vals = append(vals, repo)
		}
	}
	return vals
}

func parseRepoAllowlist(raw string) map[string]bool {
	allowed := make(map[string]bool)
	for _, repo := range parseCSV(raw) {
		allowed[repo] = true
	}
	return allowed
}

func (w *Worker) SetGitPolicy(allowedRepos string, allowPush bool) {
	if strings.TrimSpace(allowedRepos) != "" {
		w.allowedRepos = parseRepoAllowlist(allowedRepos)
	}
	if allowPush {
		w.allowPush = true
	}
}

func (w *Worker) SetLabelsAndCapabilities(labels, capabilities string) {
	if strings.TrimSpace(labels) != "" {
		w.Labels = parseCSV(labels)
	}
	if strings.TrimSpace(capabilities) != "" {
		w.Capabilities = parseCSV(capabilities)
	}
}

func (w *Worker) LoadPolicy() error {
	b, err := os.ReadFile(getWorkerPolicyPath())
	if err != nil {
		return err
	}
	var p Policy
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	w.allowedRepos = make(map[string]bool)
	for _, repo := range p.AllowedRepos {
		repo = strings.TrimSpace(repo)
		if repo != "" {
			w.allowedRepos[repo] = true
		}
	}
	w.allowPush = p.AllowPush
	w.Labels = append([]string{}, p.Labels...)
	w.Capabilities = append([]string{}, p.Capabilities...)
	return nil
}

func WritePolicy(p Policy) error {
	dir := getWorkerDataDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(getWorkerPolicyPath(), b, 0600)
}

func (w *Worker) LoadCreds() error {
	path := getWorkerCredsPath()
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var creds WorkerCredentials
	if err := json.Unmarshal(b, &creds); err != nil {
		return err
	}
	w.WorkerID = creds.WorkerID
	w.Token = creds.Token
	w.CoordinatorURL = creds.CoordinatorURL
	if w.NodeName == "Unnamed-Node" || w.NodeName == "" {
		w.NodeName = creds.NodeName
	}
	w.Insecure = creds.Insecure
	w.Fingerprint = creds.Fingerprint
	w.SetupClient(w.Fingerprint)
	return nil
}

func (w *Worker) SetupClient(fingerprint string) {
	if w.Insecure {
		w.Client = &http.Client{Timeout: 10 * time.Second}
		return
	}
	tr := &http.Transport{
		TLSClientConfig: network.PinTLSConfig(fingerprint),
	}
	w.Client = &http.Client{
		Timeout:   10 * time.Second,
		Transport: tr,
	}
}

func (w *Worker) getHardwareInfo() (models.WorkerDTO, error) {
	var info models.WorkerDTO
	info.NodeName = w.NodeName
	info.OS = runtime.GOOS
	info.Architecture = runtime.GOARCH
	info.Labels = append([]string{}, w.Labels...)
	info.Capabilities = append([]string{}, w.Capabilities...)

	if h, err := host.Info(); err == nil {
		info.OSVersion = h.PlatformVersion
		if info.OSVersion == "" {
			info.OSVersion = h.OS
		}
	} else {
		info.OSVersion = "unknown"
	}

	if c, err := cpu.Info(); err == nil && len(c) > 0 {
		info.CPUModel = c[0].ModelName
	} else {
		info.CPUModel = "unknown"
	}

	if pc, err := cpu.Counts(false); err == nil {
		info.PhysicalCores = pc
	}
	if lc, err := cpu.Counts(true); err == nil {
		info.LogicalProcessors = lc
	}

	if v, err := mem.VirtualMemory(); err == nil {
		info.TotalRAM = v.Total
		info.AvailableRAM = v.Available
	}

	absWorkspace, err := filepath.Abs(w.Workspace)
	if err == nil {
		os.MkdirAll(absWorkspace, 0755)
		if d, err := disk.Usage(absWorkspace); err == nil {
			info.FreeWorkspaceDisk = d.Free
		}
	}

	return info, nil
}

func (w *Worker) Pair(ip, code, fingerprint string) error {
	w.Fingerprint = fingerprint
	w.SetupClient(fingerprint)

	scheme := "https"
	if w.Insecure {
		scheme = "http"
	}
	if !strings.Contains(ip, ":") && !strings.HasPrefix(ip, "http") {
		ip = ip + ":8080"
	}
	w.CoordinatorURL = fmt.Sprintf("%s://%s", scheme, ip)

	hw, _ := w.getHardwareInfo()

	reqBody := map[string]interface{}{
		"code":                code,
		"node_name":           hw.NodeName,
		"os":                  hw.OS,
		"os_version":          hw.OSVersion,
		"cpu_model":           hw.CPUModel,
		"architecture":        hw.Architecture,
		"physical_cores":      hw.PhysicalCores,
		"logical_processors":  hw.LogicalProcessors,
		"total_ram":           hw.TotalRAM,
		"available_ram":       hw.AvailableRAM,
		"free_workspace_disk": hw.FreeWorkspaceDisk,
		"labels":              hw.Labels,
		"capabilities":        hw.Capabilities,
	}
	body, _ := json.Marshal(reqBody)

	resp, err := w.Client.Post(w.CoordinatorURL+"/api/workers/pair", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errRes models.ErrorResponse
		json.NewDecoder(resp.Body).Decode(&errRes)
		return fmt.Errorf("pairing failed: %s - %s", errRes.Code, errRes.Message)
	}

	var res struct {
		WorkerID string `json:"worker_id"`
		Token    string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return err
	}

	w.WorkerID = res.WorkerID
	w.Token = res.Token

	// Save credentials securely
	creds := WorkerCredentials{
		WorkerID:       w.WorkerID,
		Token:          w.Token,
		CoordinatorURL: w.CoordinatorURL,
		Fingerprint:    w.Fingerprint,
		NodeName:       w.NodeName,
		Insecure:       w.Insecure,
	}

	path := getWorkerCredsPath()
	os.MkdirAll(filepath.Dir(path), 0700)
	b, _ := json.MarshalIndent(creds, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err == nil {
		os.Rename(tmp, path)
	}

	fmt.Println("Successfully paired. Worker ID:", w.WorkerID)
	return nil
}

func (w *Worker) Start() {
	if w.Client == nil {
		w.Client = &http.Client{Timeout: 10 * time.Second} // Should only happen in tests that bypassed Pair
	}
	go w.heartbeatLoop()
	go w.jobLoop()
}

func (w *Worker) heartbeatLoop() {
	for {
		w.sendHeartbeat()
		time.Sleep(5 * time.Second)
	}
}

func (w *Worker) sendHeartbeat() {
	var avail uint64
	if v, err := mem.VirtualMemory(); err == nil {
		avail = v.Available
	}
	var free uint64
	absWorkspace, err := filepath.Abs(w.Workspace)
	if err == nil {
		os.MkdirAll(absWorkspace, 0755)
		if d, err := disk.Usage(absWorkspace); err == nil {
			free = d.Free
		}
	}

	reqBody := map[string]interface{}{
		"worker_id":           w.WorkerID,
		"available_ram":       avail,
		"free_workspace_disk": free,
		"labels":              w.Labels,
		"capabilities":        w.Capabilities,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", w.CoordinatorURL+"/api/workers/heartbeat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.Token)

	resp, err := w.Client.Do(req)
	if err != nil {
		fmt.Println("Heartbeat failed:", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		fmt.Println("Authentication rejected by coordinator. Your credentials may have been revoked or the coordinator was reset.")
		fmt.Println("Please run ForgeGrid with --reset-worker to clear saved credentials and pair again.")
		os.Exit(1)
	}
}

func (w *Worker) jobLoop() {
	for {
		w.pollJobs()
		time.Sleep(2 * time.Second)
	}
}

func (w *Worker) pollJobs() {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/jobs?worker_id=%s", w.CoordinatorURL, w.WorkerID), nil)
	req.Header.Set("Authorization", "Bearer "+w.Token)

	resp, err := w.Client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var jobs []models.Job
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		return
	}

	for _, job := range jobs {
		if job.Status == models.StatusCancelled && job.Result == "cancelled" {
			w.cancelJob(job.ID)
		} else if job.Status == models.StatusPending {
			w.mu.Lock()
			if w.activeJobs == nil {
				w.activeJobs = make(map[string]context.CancelFunc)
			}
			_, active := w.activeJobs[job.ID]
			w.mu.Unlock()
			if !active {
				// Try to claim
				attemptID, ok := w.claimJob(job.ID)
				if ok {
					job.AttemptID = attemptID
					go w.executeJob(job)
				}
			}
		}
	}
}

func (w *Worker) cancelJob(jobID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if cancel, ok := w.activeJobs[jobID]; ok {
		cancel()
		delete(w.activeJobs, jobID)
	}
}

func (w *Worker) claimJob(jobID string) (string, bool) {
	reqBody := map[string]interface{}{}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/jobs/%s/claim", w.CoordinatorURL, jobID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.Token)
	resp, err := w.Client.Do(req)
	if err != nil {
		fmt.Println("DEBUG claim err:", err)
		return "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errRes map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errRes)
		fmt.Println("DEBUG claim status not OK:", resp.StatusCode, errRes)
		return "", false
	}

	var job models.Job
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return "", false
	}
	return job.AttemptID, true
}

func (w *Worker) updateJobStatus(jobID, attemptID string, status models.JobStatus, result string, logs []byte, seq int) {
	w.updateJobStatusWithMetadata(jobID, attemptID, status, result, logs, seq, nil, "", "")
}

func (w *Worker) updateJobStatusWithMetadata(jobID, attemptID string, status models.JobStatus, result string, logs []byte, seq int, artifacts []models.Artifact, pushedBranch, prURL string) {
	reqBody := map[string]interface{}{
		"attempt_id":    attemptID,
		"status":        status,
		"result":        result,
		"logs":          logs,
		"log_seq":       seq,
		"artifacts":     artifacts,
		"pushed_branch": pushedBranch,
		"pr_url":        prURL,
	}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/jobs/%s", w.CoordinatorURL, jobID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.Token)
	resp, err := w.Client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

func (w *Worker) executeJob(job models.Job) {
	fmt.Println("Starting job:", job.ID)

	ctx, cancel := context.WithCancel(context.Background())

	w.mu.Lock()
	if w.activeJobs == nil {
		w.activeJobs = make(map[string]context.CancelFunc)
	}
	w.activeJobs[job.ID] = cancel
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		delete(w.activeJobs, job.ID)
		w.mu.Unlock()
		cancel()
	}()

	hw, _ := w.getHardwareInfo()

	logSeq := 1
	startLogs := []byte(fmt.Sprintf("Job started on %s (ID: %s)\nOS: %s | CPU: %s\nPID: %d\n", w.NodeName, w.WorkerID, hw.OS, hw.CPUModel, os.Getpid()))
	w.updateJobStatus(job.ID, job.AttemptID, models.StatusRunning, "", startLogs, logSeq)
	logSeq++

	if job.Task == "test" {
		logs := []byte(fmt.Sprintf("Received challenge: %s\n", job.Challenge))
		h := sha256.Sum256([]byte(job.Challenge))
		result := hex.EncodeToString(h[:])
		logs = append(logs, []byte(fmt.Sprintf("Calculated SHA-256: %s\n", result))...)
		w.updateJobStatus(job.ID, job.AttemptID, models.StatusCompleted, result, logs, logSeq)
	} else if job.Task == "execute" {
		profile, err := execution.GetProfile(job.Profile)
		if err != nil {
			w.updateJobStatus(job.ID, job.AttemptID, models.StatusFailed, err.Error(), []byte(err.Error()+"\n"), logSeq)
			return
		}

		var workDir string
		var gm *gitworkspace.Manager
		var mainRepoDir string
		var branchName string

		var output []byte
		var artifacts []models.Artifact
		var pushedBranch string
		var prURL string
		finalResult := "success"
		finalStatus := models.StatusCompleted

		if job.RepositoryURL != "" {
			if !w.allowedRepos[job.RepositoryURL] {
				w.updateJobStatus(job.ID, job.AttemptID, models.StatusFailed, "repository not allowed", []byte("Repository is not in this worker's allowlist. Start the worker with -allowed-repos or FORGEGRID_ALLOWED_REPOS.\n"), logSeq)
				return
			}
			if job.PushChanges && !w.allowPush {
				w.updateJobStatus(job.ID, job.AttemptID, models.StatusFailed, "push not allowed", []byte("Job requested push_changes, but this worker was not started with -allow-push or FORGEGRID_ALLOW_PUSH=true.\n"), logSeq)
				return
			}
			gm = gitworkspace.NewManager(w.Workspace, gitworkspace.Options{
				AllowedRepos: w.allowedRepos,
				AllowPush:    w.allowPush,
			})

			branchName = job.BranchName
			if branchName == "" {
				branchName = "forgegrid-" + job.ID
			}

			wd, err := gm.PrepareWorkspace(job.RepositoryURL, job.BaseCommit, branchName, job.ID)
			if err != nil {
				w.updateJobStatus(job.ID, job.AttemptID, models.StatusFailed, "workspace prep error", []byte(err.Error()+"\n"), logSeq)
				return
			}
			workDir = wd
			mainRepoDir = filepath.Join(w.Workspace, strings.TrimSuffix(filepath.Base(job.RepositoryURL), ".git"))

			// Setup cleanup and reporting
			defer func() {
				// Attempt push if successful and requested
				if finalStatus == models.StatusCompleted && job.CommitChanges {
					commitMsg := job.CommitMessage
					if commitMsg == "" {
						commitMsg = "Automated commit by ForgeGrid worker"
					}
					commitResult, pushErr := gm.CommitAndMaybePush(workDir, job.RepositoryURL, commitMsg, job.PushChanges)
					output = append(output, []byte("\n--- GIT CHANGES ---\n"+commitResult+"\n")...)
					if pushErr != nil {
						output = append(output, []byte(fmt.Sprintf("\n--- GIT CHANGE FAILED ---\n%v", pushErr))...)
						finalStatus = models.StatusFailed
						finalResult = "git change failed"
					} else if job.PushChanges {
						pushedBranch = branchName
						if job.CreatePR {
							createdPR, prErr := gm.CreatePullRequest(workDir, job.PRTitle, job.PRBody)
							if prErr != nil {
								output = append(output, []byte(fmt.Sprintf("\n--- PR CREATION FAILED ---\n%v", prErr))...)
							} else {
								prURL = createdPR
								output = append(output, []byte("\n--- PULL REQUEST CREATED ---\n"+createdPR+"\n")...)
							}
						}
					}
				}

				if finalStatus == models.StatusCompleted && len(job.Artefacts) > 0 {
					collected, artErr := gm.CollectArtifacts(workDir, job.Artefacts)
					if artErr != nil {
						output = append(output, []byte(fmt.Sprintf("\n--- ARTIFACT COLLECTION FAILED ---\n%v\n", artErr))...)
					} else {
						for _, a := range collected {
							artifacts = append(artifacts, models.Artifact{
								Path:          a.Path,
								Size:          a.Size,
								SHA256:        a.SHA256,
								ContentBase64: a.ContentBase64,
								Packaged:      a.Packaged,
								PackageName:   a.PackageName,
							})
						}
						output = append(output, []byte(fmt.Sprintf("\n--- ARTIFACTS ---\nCollected %d artifact(s).\n", len(artifacts)))...)
					}
				}

				diff, diffErr := gm.ProduceDiff(workDir)
				if diffErr == nil {
					output = append(output, []byte("\n--- WORKSPACE STATUS ---\n"+diff)...)
				}
				if finalStatus == models.StatusCompleted && job.CommitChanges && !job.PushChanges {
					output = append(output, []byte(fmt.Sprintf("\n--- WORKTREE RETAINED ---\nLocal commit kept at %s on branch %s because push is disabled for this job.\n", workDir, branchName))...)
				} else if err := gm.CleanupWorktree(mainRepoDir, workDir, branchName); err != nil {
					output = append(output, []byte(fmt.Sprintf("\n--- CLEANUP FAILED ---\n%v\n", err))...)
				}
				// Need to push output again since we appended
				w.updateJobStatusWithMetadata(job.ID, job.AttemptID, finalStatus, finalResult, output, logSeq+1, artifacts, pushedBranch, prURL)
			}()
		} else {
			workDir, err = execution.SecureWorkspacePath(w.Workspace, ".")
			if err != nil {
				w.updateJobStatus(job.ID, job.AttemptID, models.StatusFailed, "workspace error", []byte(err.Error()+"\n"), logSeq)
				return
			}
		}

		timeoutSeconds := job.TimeoutSeconds
		if timeoutSeconds == 0 || timeoutSeconds > profile.MaxTimeoutSecs {
			timeoutSeconds = profile.MaxTimeoutSecs
		}
		timeout := time.Duration(timeoutSeconds) * time.Second

		execCtx, execCancel := context.WithTimeout(ctx, timeout)
		defer execCancel()

		executor := execution.NewExecutor()
		output, err = executor.Execute(execCtx, profile, job.Parameters, workDir)

		if err != nil {
			if execCtx.Err() == context.DeadlineExceeded {
				finalResult = "timeout"
				finalStatus = models.StatusFailed
				output = append(output, []byte("\nJob timed out")...)
			} else if ctx.Err() == context.Canceled {
				finalResult = "cancelled"
				finalStatus = models.StatusCancelled
				output = append(output, []byte("\nJob cancelled by coordinator")...)
			} else {
				finalResult = fmt.Sprintf("error: %v", err)
				finalStatus = models.StatusFailed
			}
		}

		if gm == nil {
			w.updateJobStatus(job.ID, job.AttemptID, finalStatus, finalResult, output, logSeq)
		}
	} else {
		w.updateJobStatus(job.ID, job.AttemptID, models.StatusFailed, "unknown task", []byte("Unsupported task type\n"), logSeq)
	}
}
