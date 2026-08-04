package worker

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

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
	
	activeJobsMu   sync.Mutex
	activeJobs     map[string]context.CancelFunc
}

type WorkerCredentials struct {
	WorkerID       string `json:"worker_id"`
	Token          string `json:"token"`
	CoordinatorURL string `json:"coordinator_url"`
	Fingerprint    string `json:"fingerprint"`
	NodeName       string `json:"node_name"`
	Insecure       bool   `json:"insecure"`
}

func getWorkerCredsPath() string {
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
	return filepath.Join(dir, name, "worker_creds.json")
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
	return &Worker{
		NodeName:   nodeName,
		Workspace:  workspace,
		Insecure:   insecure,
		activeJobs: make(map[string]context.CancelFunc),
	}
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
		w.executeJob(job)
	}
}

func (w *Worker) updateJobStatus(jobID, status, result string, logs []string) {
	reqBody := map[string]interface{}{
		"status": status,
		"result": result,
		"logs":   logs,
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

func (w *Worker) cancelJob(jobID string) {
	w.activeJobsMu.Lock()
	if cancel, ok := w.activeJobs[jobID]; ok {
		cancel()
	}
	w.activeJobsMu.Unlock()
}

func (w *Worker) executeJob(job models.Job) {
	if job.Task == "test" {
		w.executeTestJob(job)
		return
	}

	jobDir := filepath.Join(w.Workspace, "jobs", job.ID)
	os.MkdirAll(jobDir, 0755)

	ctx, cancel := context.WithCancel(context.Background())
	w.activeJobsMu.Lock()
	if w.activeJobs == nil {
		w.activeJobs = make(map[string]context.CancelFunc)
	}
	w.activeJobs[job.ID] = cancel
	w.activeJobsMu.Unlock()

	defer func() {
		w.activeJobsMu.Lock()
		delete(w.activeJobs, job.ID)
		w.activeJobsMu.Unlock()
		cancel()
	}()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmdStr := job.CommandWindows
		if cmdStr == "" {
			w.updateJobStatus(job.ID, "failed", "Missing Windows command", []string{"No windows command specified for task"})
			return
		}
		cmd = exec.CommandContext(ctx, "cmd", "/c", cmdStr)
	} else {
		cmdStr := job.CommandLinux
		if cmdStr == "" {
			w.updateJobStatus(job.ID, "failed", "Missing Linux command", []string{"No linux command specified for task"})
			return
		}
		cmd = exec.CommandContext(ctx, "sh", "-c", cmdStr)
	}
	cmd.Dir = jobDir

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		w.updateJobStatus(job.ID, "failed", err.Error(), []string{"Failed to open stdout"})
		return
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		w.updateJobStatus(job.ID, "failed", err.Error(), []string{"Failed to open stderr"})
		return
	}

	if err := cmd.Start(); err != nil {
		w.updateJobStatus(job.ID, "failed", err.Error(), []string{"Failed to start command"})
		return
	}

	logCh := make(chan string, 100)
	var wg sync.WaitGroup
	wg.Add(2)

	readPipe := func(r io.Reader) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			logCh <- scanner.Text()
		}
	}
	go readPipe(stdoutPipe)
	go readPipe(stderrPipe)

	// Background routine to flush logs
	doneFlushing := make(chan struct{})
	go func() {
		var batch []string
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case line, ok := <-logCh:
				if !ok {
					if len(batch) > 0 {
						w.updateJobStatus(job.ID, "running", "", batch)
					}
					close(doneFlushing)
					return
				}
				batch = append(batch, line)
				if len(batch) >= 50 {
					w.updateJobStatus(job.ID, "running", "", batch)
					batch = nil
				}
			case <-ticker.C:
				if len(batch) > 0 {
					w.updateJobStatus(job.ID, "running", "", batch)
					batch = nil
				}
			}
		}
	}()

	err = cmd.Wait()
	wg.Wait()
	close(logCh)
	<-doneFlushing

	if err != nil {
		w.updateJobStatus(job.ID, "failed", err.Error(), []string{"Command failed or cancelled"})
		return
	}

	if len(job.Artefacts) > 0 {
		if err := w.collectArtefacts(job.ID, job.Artefacts, jobDir); err != nil {
			w.updateJobStatus(job.ID, "failed", err.Error(), []string{"Artefact collection failed: " + err.Error()})
			return
		}
	}

	w.updateJobStatus(job.ID, "completed", "success", []string{"Job completed successfully"})
}

func (w *Worker) collectArtefacts(jobID string, patterns []string, jobDir string) error {
	absJobDir, err := filepath.Abs(jobDir)
	if err != nil {
		return err
	}
	
	// Ensure jobDir doesn't end with separator for strict bounds checking
	absJobDir = filepath.Clean(absJobDir)

	for _, p := range patterns {
		// Prevent obvious traversal in pattern
		if strings.Contains(p, "..") {
			return fmt.Errorf("artefact path %s escapes workspace", p)
		}
		
		matches, err := filepath.Glob(filepath.Join(absJobDir, p))
		if err != nil {
			return err
		}
		for _, m := range matches {
			absMatch, err := filepath.Abs(m)
			if err != nil {
				return err
			}
			absMatch = filepath.Clean(absMatch)
			if !strings.HasPrefix(absMatch, absJobDir+string(filepath.Separator)) && absMatch != absJobDir {
				return fmt.Errorf("artefact path %s escapes workspace", m)
			}
			
			// Simulate upload logic
			// In reality, we'd package and POST here.
		}
	}
	return nil
}

func (w *Worker) executeTestJob(job models.Job) {
	fmt.Println("Starting test job:", job.ID)
	
	hw, _ := w.getHardwareInfo()
	
	w.updateJobStatus(job.ID, "running", "", []string{
		fmt.Sprintf("Job started on %s (ID: %s)", w.NodeName, w.WorkerID),
		fmt.Sprintf("OS: %s | CPU: %s", hw.OS, hw.CPUModel),
		fmt.Sprintf("PID: %d", os.Getpid()),
	})
	
	logs := []string{
		fmt.Sprintf("Received challenge: %s", job.Challenge),
	}
	
	h := sha256.Sum256([]byte(job.Challenge))
	result := hex.EncodeToString(h[:])
	
	logs = append(logs, fmt.Sprintf("Calculated SHA-256: %s", result))
	
	w.updateJobStatus(job.ID, "completed", result, logs)
}
