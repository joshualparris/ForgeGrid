package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"forgegrid/internal/execution"
	"forgegrid/internal/gitworkspace"
	"forgegrid/internal/models"
	"forgegrid/internal/network"
	fgupdate "forgegrid/internal/update"
	"forgegrid/internal/version"

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

	mu              sync.Mutex
	activeJobs      map[string]context.CancelFunc
	stopOnce        sync.Once
	stopCh          chan struct{}
	loopsDone       sync.WaitGroup
	allowedRepos    map[string]bool
	allowPush       bool
	allowBootstrap  bool
	Labels          []string
	Capabilities    []string
	capabilityAllow []string
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
	AllowedRepos   []string `json:"allowed_repos"`
	AllowPush      bool     `json:"allow_push"`
	AllowBootstrap bool     `json:"allow_bootstrap"`
	Labels         []string `json:"labels"`
	Capabilities   []string `json:"capabilities"`
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

func WorkerStatusPath() string {
	return filepath.Join(getWorkerDataDir(), "worker_status.json")
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
		NodeName:        nodeName,
		Workspace:       workspace,
		Insecure:        insecure,
		activeJobs:      make(map[string]context.CancelFunc),
		stopCh:          make(chan struct{}),
		allowedRepos:    parseRepoAllowlist(os.Getenv("FORGEGRID_ALLOWED_REPOS")),
		allowPush:       os.Getenv("FORGEGRID_ALLOW_PUSH") == "true",
		allowBootstrap:  os.Getenv("FORGEGRID_ALLOW_BOOTSTRAP") == "true",
		Labels:          parseCSV(os.Getenv("FORGEGRID_LABELS")),
		capabilityAllow: parseCSV(os.Getenv("FORGEGRID_CAPABILITIES")),
	}
	w.LoadPolicy()
	if envRepos := strings.TrimSpace(os.Getenv("FORGEGRID_ALLOWED_REPOS")); envRepos != "" {
		w.allowedRepos = parseRepoAllowlist(envRepos)
	}
	if os.Getenv("FORGEGRID_ALLOW_PUSH") == "true" {
		w.allowPush = true
	}
	if os.Getenv("FORGEGRID_ALLOW_BOOTSTRAP") == "true" {
		w.allowBootstrap = true
	}
	if labels := strings.TrimSpace(os.Getenv("FORGEGRID_LABELS")); labels != "" {
		w.Labels = parseCSV(labels)
	}
	if capabilities := strings.TrimSpace(os.Getenv("FORGEGRID_CAPABILITIES")); capabilities != "" {
		w.capabilityAllow = parseCSV(capabilities)
	}
	w.RefreshCapabilities()
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

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]int, len(a))
	for _, v := range a {
		set[v]++
	}
	for _, v := range b {
		if set[v] == 0 {
			return false
		}
		set[v]--
	}
	return true
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
		w.capabilityAllow = parseCSV(capabilities)
		w.RefreshCapabilities()
	}
}

func (w *Worker) ValidateCapabilities() ([]string, []string) {
	detected := DetectCapabilities()
	if len(w.capabilityAllow) == 0 {
		return detected, nil
	}
	detectedSet := make(map[string]bool, len(detected))
	for _, cap := range detected {
		detectedSet[cap] = true
	}
	valid := make([]string, 0, len(w.capabilityAllow))
	missing := make([]string, 0)
	seen := make(map[string]bool)
	for _, cap := range w.capabilityAllow {
		cap = strings.ToLower(strings.TrimSpace(cap))
		if cap == "" || seen[cap] {
			continue
		}
		seen[cap] = true
		if detectedSet[cap] {
			valid = append(valid, cap)
		} else {
			missing = append(missing, cap)
		}
	}
	return valid, missing
}

func (w *Worker) RefreshCapabilities() {
	valid, _ := w.ValidateCapabilities()
	w.Capabilities = valid
}

func DetectCapabilities() []string {
	checks := []struct {
		name string
		ok   func() bool
	}{
		{"git", commandOK("git", "--version")},
		{"python", pythonOK},
		{"go", commandOK("go", "version")},
		{"node", commandOK("node", "--version")},
		{"antigravity", antigravityOK},
		{"codex", commandOK("codex", "--version")},
		{"godot", godotOK},
	}
	var caps []string
	hasAgent := false
	for _, check := range checks {
		if check.ok() {
			caps = append(caps, check.name)
			if check.name == "antigravity" || check.name == "codex" {
				hasAgent = true
			}
		}
	}
	if hasAgent {
		caps = append(caps, "ai-agent")
	}
	return caps
}

func commandOK(name string, args ...string) func() bool {
	return func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		return cmd.Run() == nil
	}
}

func pythonOK() bool {
	return commandOK("python", "-c", "import sys; sys.exit(0)")() || commandOK("python3", "-c", "import sys; sys.exit(0)")()
}

func antigravityOK() bool {
	if path := strings.TrimSpace(os.Getenv("ANTIGRAVITY_PATH")); path != "" {
		return fileExecutableExists(path)
	}
	if _, err := exec.LookPath("antigravity"); err == nil {
		return true
	}
	if _, err := exec.LookPath("antigravity.exe"); err == nil {
		return true
	}
	for _, root := range []string{os.Getenv("LOCALAPPDATA"), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		if root == "" {
			continue
		}
		for _, candidate := range []string{
			filepath.Join(root, "Programs", "Antigravity", "Antigravity.exe"),
			filepath.Join(root, "Antigravity", "Antigravity.exe"),
			filepath.Join(root, "Google", "Antigravity", "Antigravity.exe"),
		} {
			if fileExecutableExists(candidate) {
				return true
			}
		}
	}
	return false
}

func godotOK() bool {
	return commandOK("godot", "--version")() || commandOK("godot4", "--version")()
}

func commandPathOK(path string, args ...string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	return cmd.Run() == nil
}

func fileExecutableExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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
	w.allowBootstrap = p.AllowBootstrap
	w.Labels = append([]string{}, p.Labels...)
	w.capabilityAllow = append([]string{}, p.Capabilities...)
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
	info.Version = version.Info()
	info.Labels = append([]string{}, w.Labels...)
	validCaps, drift := w.ValidateCapabilities()
	if len(drift) > 0 {
		log.Printf("Capability drift detected: %v are configured but missing from PATH", drift)
	}
	w.Capabilities = append([]string{}, validCaps...)
	info.Capabilities = append([]string{}, validCaps...)

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
		"version":             hw.Version,
	}
	body, _ := json.Marshal(reqBody)

	resp, err := w.Client.Post(w.CoordinatorURL+"/api/workers/pair", "application/json", bytes.NewReader(body))
	if err != nil {
		w.writeStatus("pair_failed", classifyConnectionError(err), false)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errRes models.ErrorResponse
		json.NewDecoder(resp.Body).Decode(&errRes)
		w.writeStatus("pair_failed", fmt.Sprintf("%s: %s", errRes.Code, errRes.Message), false)
		return fmt.Errorf("pairing failed: %s - %s", errRes.Code, errRes.Message)
	}

	var res struct {
		WorkerID string `json:"worker_id"`
		Token    string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		w.writeStatus("pair_failed", err.Error(), false)
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

	w.writeStatus("paired", "Worker paired and trusted coordinator fingerprint "+w.Fingerprint, true)
	fmt.Println("Successfully paired. Worker ID:", w.WorkerID)
	return nil
}

func (w *Worker) Start() {
	if w.Client == nil {
		w.Client = &http.Client{Timeout: 10 * time.Second} // Should only happen in tests that bypassed Pair
	}
	
	w.cleanupUpdateFiles()
	w.verifyUpdateTransaction()

	w.loopsDone.Add(2)
	go w.heartbeatLoop()
	go w.jobLoop()
}

func (w *Worker) verifyUpdateTransaction() {
	txID := os.Getenv("FORGEGRID_UPDATE_TX")

	tx, err := readTx()
	if err != nil {
		return
	}
	
	// If txID is not empty, ensure it matches. If empty (e.g. started by Windows SCM), we just use the active tx.
	if txID != "" && tx.ID != txID {
		return
	}

	if tx.CurrentState == "COMPLETED" || tx.CurrentState == "ROLLED_BACK" || tx.CurrentState == "ROLLBACK_FAILED" {
		return
	}

	if tx.CurrentState == "ROLLING_BACK" {
		// Wait briefly to ensure any prior connection closes, then send heartbeat
		time.Sleep(1 * time.Second)
		w.sendHeartbeat()
		status, _ := readStatus()
		if status == nil || status.State != "heartbeat_ok" {
			log.Printf("[Update] Rollback failed health verification: could not reconnect to coordinator")
			w.reportUpdate(tx.ID, "rollback_failed", "Rollback failed: could not reconnect to coordinator", true)
			tx.CurrentState = "ROLLBACK_FAILED"
			writeTx(tx)
			return
		}

		exe, _ := os.Executable()
		hash, _ := fileSHA256(exe)
		if tx.OldSHA256 != "" && hash != tx.OldSHA256 {
			log.Printf("[Update] Rollback failed health verification: running hash did not match old hash")
			w.reportUpdate(tx.ID, "rollback_failed", "Rollback failed: running hash did not match old hash", true)
			tx.CurrentState = "ROLLBACK_FAILED"
			writeTx(tx)
			return
		}

		w.reportUpdate(tx.ID, "rolled_back", "New worker did not reconnect within time limit. Previous version restored successfully. Reason: "+tx.RollbackReason, true)
		log.Printf("[Update] Previous worker restored")
		tx.CurrentState = "ROLLED_BACK"
		writeTx(tx)
		return
	}

	if tx.CurrentState != "VERIFYING_NEW_WORKER" && tx.CurrentState != "RESTARTING" {
		return
	}

	log.Printf("[Update] Candidate installed")
	log.Printf("[Update] Starting candidate worker")
	log.Printf("[Update] Waiting for coordinator handshake")

	exe, err := os.Executable()
	if err != nil {
		return
	}

	hash, err := fileSHA256(exe)
	if err != nil || hash != tx.ExpectedSHA256 {
		log.Printf("[Update] Candidate failed health verification: running hash %s did not match expected %s", hash, tx.ExpectedSHA256)
		os.Exit(1)
	}

	// Wait briefly to ensure any prior connection closes, then send heartbeat
	time.Sleep(1 * time.Second)
	w.sendHeartbeat()
	
	status, _ := readStatus()
	if status == nil || status.State != "heartbeat_ok" {
		log.Printf("[Update] Candidate failed health verification: could not reconnect to coordinator")
		os.Exit(1)
	}

	log.Printf("[Update] Candidate verified")
	w.reportUpdate(tx.ID, "completed", "Worker successfully updated and verified.", true)

	tx.CurrentState = "COMPLETED"
	writeTx(tx)
	log.Printf("[Update] Transaction completed")
}

func (w *Worker) cleanupUpdateFiles() {
	tx, _ := readTx()
	if tx != nil && (tx.CurrentState == "STAGED" || tx.CurrentState == "APPLYING" || tx.CurrentState == "RESTARTING" || tx.CurrentState == "VERIFYING_NEW_WORKER" || tx.CurrentState == "ROLLING_BACK") {
		// Active transaction, do not clean up
		return
	}

	updateDir := filepath.Join(getWorkerDataDir(), "updates")
	entries, err := os.ReadDir(updateDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "updater-helper-") {
			path := filepath.Join(updateDir, entry.Name())
			os.Remove(path)
		}
	}
}

func (w *Worker) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
		w.mu.Lock()
		for jobID, cancel := range w.activeJobs {
			cancel()
			delete(w.activeJobs, jobID)
		}
		w.mu.Unlock()
	})
	w.loopsDone.Wait()
}

func (w *Worker) heartbeatLoop() {
	defer w.loopsDone.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		w.sendHeartbeat()
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) sendHeartbeat() {
	validCaps, drift := w.ValidateCapabilities()
	if len(drift) > 0 {
		log.Printf("Capability drift detected: %v are configured but missing from PATH", drift)
	}
	w.mu.Lock()
	w.Capabilities = validCaps
	labels := append([]string{}, w.Labels...)
	capabilities := append([]string{}, w.Capabilities...)
	w.mu.Unlock()

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
		"labels":              labels,
		"capabilities":        capabilities,
		"version":             version.Info(),
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", w.CoordinatorURL+"/api/workers/heartbeat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.Token)

	resp, err := w.Client.Do(req)
	if err != nil {
		message := classifyConnectionError(err)
		w.writeStatus("heartbeat_failed", message, false)
		fmt.Println("Heartbeat failed:", message)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		w.writeStatus("authentication_failed", "Coordinator rejected saved worker token. Re-pair this worker from the dashboard.", false)
		fmt.Println("Authentication rejected by coordinator. Your credentials may have been revoked or the coordinator was reset.")
		fmt.Println("Please run ForgeGrid with --reset-worker to clear saved credentials and pair again.")
		os.Exit(1)
	}
	if resp.StatusCode >= 400 {
		w.writeStatus("heartbeat_failed", fmt.Sprintf("Coordinator returned HTTP %d", resp.StatusCode), false)
		return
	}
	w.writeStatus("heartbeat_ok", "Connected to trusted coordinator", true)
}

func (w *Worker) writeStatus(state, message string, connected bool) {
	status := map[string]interface{}{
		"state":           state,
		"message":         message,
		"connected":       connected,
		"coordinator_url": w.CoordinatorURL,
		"fingerprint":     w.Fingerprint,
		"worker_id":       w.WorkerID,
		"node_name":       w.NodeName,
		"updated_at":      time.Now().Format(time.RFC3339),
	}
	b, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(getWorkerDataDir(), 0700); err != nil {
		return
	}
	_ = os.WriteFile(WorkerStatusPath(), b, 0600)
}

func classifyConnectionError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "certificate fingerprint mismatch"):
		return "Worker cannot verify coordinator identity. The saved TLS fingerprint does not match the coordinator certificate. Do not auto-trust this; confirm the coordinator is correct and re-pair only if the coordinator identity was intentionally reset. Detail: " + msg
	case strings.Contains(lower, "certificate") || strings.Contains(lower, "x509"):
		return "Worker cannot verify coordinator TLS certificate. Detail: " + msg
	case strings.Contains(lower, "connection refused"):
		return "Coordinator is not accepting connections at the saved address. Check that ForgeGrid is running and the address has not changed. Detail: " + msg
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded"):
		return "Network timeout reaching coordinator. Check Wi-Fi, firewall and coordinator address. Detail: " + msg
	default:
		return msg
	}
}

func (w *Worker) jobLoop() {
	defer w.loopsDone.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		w.pollJobs()
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) pollJobs() {
	validCaps, drift := w.ValidateCapabilities()
	w.mu.Lock()
	changed := !sameStringSet(w.Capabilities, validCaps)
	w.Capabilities = validCaps
	w.mu.Unlock()
	if len(drift) > 0 {
		log.Printf("Capability drift detected: %v are configured but missing from PATH", drift)
	}
	if changed || len(drift) > 0 {
		w.sendHeartbeat()
	}
	w.pollUpdateRequest()

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
		if job.Status == models.StatusCancelRequested {
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
	w.updateJobStatusWithMetadata(jobID, attemptID, status, result, logs, seq, nil, "", "", nil, 0)
}

func (w *Worker) updateJobStatusWithMetadata(jobID, attemptID string, status models.JobStatus, result string, logs []byte, seq int, artifacts []models.Artifact, pushedBranch, prURL string, stages []models.JobStage, currentStage int) {
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
	if stages != nil {
		reqBody["stages"] = stages
		reqBody["current_stage"] = currentStage
	}
	addJobResultMetadata(reqBody, jobResultMetadata{})
	w.postJobUpdate(jobID, reqBody)
}

type jobResultMetadata struct {
	FailureCode       string
	BaseBranch        string
	ResolvedBase      string
	WorkBranch        string
	CommitSHA         string
	WorkspaceID       string
	WorkspaceRetained bool
	ChangedFiles      []models.ChangedFile
	ValidationResults []models.ValidationResult
}

func (w *Worker) updateJobStatusFull(jobID, attemptID string, status models.JobStatus, result string, logs []byte, seq int, artifacts []models.Artifact, pushedBranch, prURL string, stages []models.JobStage, currentStage int, meta jobResultMetadata) {
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
	if stages != nil {
		reqBody["stages"] = stages
		reqBody["current_stage"] = currentStage
	}
	addJobResultMetadata(reqBody, meta)
	w.postJobUpdate(jobID, reqBody)
}

func addJobResultMetadata(reqBody map[string]interface{}, meta jobResultMetadata) {
	if meta.FailureCode != "" {
		reqBody["failure_code"] = meta.FailureCode
	}
	if meta.BaseBranch != "" {
		reqBody["base_branch"] = meta.BaseBranch
	}
	if meta.ResolvedBase != "" {
		reqBody["resolved_base"] = meta.ResolvedBase
	}
	if meta.WorkBranch != "" {
		reqBody["work_branch"] = meta.WorkBranch
	}
	if meta.CommitSHA != "" {
		reqBody["commit_sha"] = meta.CommitSHA
	}
	if meta.WorkspaceID != "" {
		reqBody["workspace_id"] = meta.WorkspaceID
	}
	if meta.WorkspaceRetained {
		reqBody["workspace_retained"] = true
	}
	if meta.ChangedFiles != nil {
		reqBody["changed_files"] = meta.ChangedFiles
	}
	if meta.ValidationResults != nil {
		reqBody["validation_results"] = meta.ValidationResults
	}
}

func (w *Worker) postJobUpdate(jobID string, reqBody map[string]interface{}) {
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/jobs/%s", w.CoordinatorURL, jobID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.Token)
	resp, err := w.Client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

func (w *Worker) pollUpdateRequest() {
	w.mu.Lock()
	active := len(w.activeJobs)
	w.mu.Unlock()
	if active > 0 {
		return
	}
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/updates/worker?worker_id=%s", w.CoordinatorURL, w.WorkerID), nil)
	req.Header.Set("Authorization", "Bearer "+w.Token)
	resp, err := w.Client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var body struct {
		Update *fgupdate.Request `json:"update"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Update == nil {
		return
	}
	go w.stageUpdate(*body.Update)
}

func (w *Worker) reportUpdate(updateID, status, message string, rollbackReady bool) {
	reqBody := map[string]interface{}{
		"worker_id":      w.WorkerID,
		"update_id":      updateID,
		"status":         status,
		"message":        message,
		"rollback_ready": rollbackReady,
	}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", w.CoordinatorURL+"/api/updates/report", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.Token)
	resp, err := w.Client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

func (w *Worker) stageUpdate(req fgupdate.Request) {
	w.mu.Lock()
	if len(w.activeJobs) > 0 {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()
	w.reportUpdate(req.ID, "running", "Staging update package", false)

	source := req.Artifact.Path
	if source == "" && strings.HasPrefix(req.Artifact.URL, "file://") {
		if u, err := url.Parse(req.Artifact.URL); err == nil {
			source = u.Path
		}
	}
	if source == "" {
		w.reportUpdate(req.ID, "failed", "Remote update downloads are not enabled for this worker yet; provide a trusted local update bundle", false)
		return
	}
	if !filepath.IsAbs(source) {
		abs, err := filepath.Abs(source)
		if err == nil {
			source = abs
		}
	}
	if err := fgupdate.VerifyFile(source, req.Artifact.SHA256); err != nil {
		w.reportUpdate(req.ID, "failed", "Update package failed checksum verification: "+err.Error(), false)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		w.reportUpdate(req.ID, "failed", "Could not locate running ForgeGrid executable: "+err.Error(), false)
		return
	}
	updateDir := filepath.Join(getWorkerDataDir(), "updates", req.ID)
	if err := os.MkdirAll(updateDir, 0700); err != nil {
		w.reportUpdate(req.ID, "failed", "Could not create update staging folder: "+err.Error(), false)
		return
	}
	rollbackPath := filepath.Join(updateDir, "rollback-"+filepath.Base(exe))
	stagedPath := filepath.Join(updateDir, filepath.Base(source))
	if err := copyFile(exe, rollbackPath, 0700); err != nil {
		w.reportUpdate(req.ID, "failed", "Could not prepare rollback copy: "+err.Error(), false)
		return
	}
	if err := copyFile(source, stagedPath, 0700); err != nil {
		w.reportUpdate(req.ID, "failed", "Could not stage update package: "+err.Error(), true)
		return
	}
	if err := fgupdate.VerifyFile(stagedPath, req.Artifact.SHA256); err != nil {
		w.reportUpdate(req.ID, "failed", "Staged update failed checksum verification: "+err.Error(), true)
		return
	}

	_ = os.Chmod(stagedPath, 0755)

	log.Printf("[Update] Candidate staged")

	// Prepare updater helper
	updaterPath := filepath.Join(updateDir, "updater-helper-"+filepath.Base(exe))
	if err := copyFile(exe, updaterPath, 0755); err != nil {
		w.reportUpdate(req.ID, "failed", "Could not copy updater helper: "+err.Error(), false)
		return
	}

	tx := &UpdateTransaction{
		ID:               req.ID,
		WorkerID:         w.WorkerID,
		OldBinaryPath:    exe,
		NewBinaryPath:    stagedPath,
		BackupBinaryPath: filepath.Join(filepath.Dir(exe), "previous-"+filepath.Base(exe)),
		ExpectedSHA256:   req.Artifact.SHA256,
		CurrentState:     "STAGED",
		StartedAt:        time.Now(),
		RestartDeadline:  time.Now().Add(60 * time.Second),
		WorkerPID:        os.Getpid(),
		LifecycleMode:    DetectCurrentLifecycle(),
	}

	// Prepare rollback backup
	if err := copyFile(exe, tx.BackupBinaryPath, 0755); err != nil {
		w.reportUpdate(req.ID, "failed", "Could not create backup: "+err.Error(), false)
		return
	}

	if oldHash, err := fileSHA256(exe); err == nil {
		tx.OldSHA256 = oldHash
	}

	if err := writeTx(tx); err != nil {
		w.reportUpdate(req.ID, "failed", "Could not write transaction: "+err.Error(), false)
		return
	}

	log.Printf("[Update] Launching update helper")
	cmd := exec.Command(updaterPath, "-mode", "update-helper")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		w.reportUpdate(req.ID, "failed", "Could not launch updater: "+err.Error(), true)
		return
	}

	w.reportUpdate(req.ID, "running", "Update staged. Launching updater helper...", false)
	log.Printf("[Update] Worker exiting for replacement")
	os.Exit(0)
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
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
		var workDir string
		var gm *gitworkspace.Manager
		var mainRepoDir string
		var branchName string
		var resultMeta jobResultMetadata

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

			ws, err := gm.PrepareJobWorkspace(job.RepositoryURL, job.BaseCommit, branchName, job.ID)
			if err != nil {
				w.updateJobStatus(job.ID, job.AttemptID, models.StatusFailed, "workspace prep error", []byte(err.Error()+"\n"), logSeq)
				return
			}
			workDir = ws.WorkDir
			mainRepoDir = ws.RepoDir
			resultMeta.WorkspaceID = ws.ID
			resultMeta.ResolvedBase = ws.BaseCommit
			resultMeta.WorkBranch = ws.BranchName
			output = append(output, []byte(fmt.Sprintf("\n--- GIT WORKSPACE ---\nRepository: %s\nBase: %s\nBranch: %s\nWorkspace: %s\n", job.RepositoryURL, ws.BaseCommit, ws.BranchName, ws.WorkDir))...)

			// Setup cleanup and reporting
			defer func() {
				// Attempt push if successful and requested
				if finalStatus == models.StatusCompleted && job.CommitChanges {
					changed, changeErr := gm.ChangedFiles(workDir)
					if changeErr != nil {
						output = append(output, []byte(fmt.Sprintf("\n--- CHANGE DETECTION FAILED ---\n%v\n", changeErr))...)
						finalStatus = models.StatusFailed
						finalResult = "change detection failed"
						resultMeta.FailureCode = "CHANGE_DETECTION_FAILED"
					}
					resultMeta.ChangedFiles = changed
					if finalStatus == models.StatusCompleted && len(changed) == 0 {
						finalResult = "no changes"
						output = append(output, []byte("\n--- GIT CHANGES ---\nNo files changed; nothing to commit.\n")...)
					}
					if finalStatus == models.StatusCompleted {
						blocked := gitworkspace.SecretLikeChangedFiles(changed)
						if len(blocked) > 0 {
							output = append(output, []byte("\n--- SECRET GUARD FAILED ---\nForgeGrid refused to commit likely secret files:\n"+strings.Join(blocked, "\n")+"\n")...)
							finalStatus = models.StatusFailed
							finalResult = "secret guard failed"
							resultMeta.FailureCode = "SECRET_GUARD_FAILED"
							resultMeta.WorkspaceRetained = true
						}
					}
				}
				if finalStatus == models.StatusCompleted && job.CommitChanges && finalResult != "no changes" {
					commitMsg := job.CommitMessage
					if commitMsg == "" {
						commitMsg = "Automated commit by ForgeGrid worker"
					}
					commitResult, pushErr := gm.CommitAndMaybePushDetailed(workDir, job.RepositoryURL, commitMsg, job.PushChanges)
					if commitResult != nil {
						resultMeta.CommitSHA = commitResult.CommitSHA
						output = append(output, []byte("\n--- GIT CHANGES ---\n"+commitResult.Message+"\n")...)
					}
					if pushErr != nil {
						output = append(output, []byte(fmt.Sprintf("\n--- GIT CHANGE FAILED ---\n%v", pushErr))...)
						finalStatus = models.StatusFailed
						finalResult = "git change failed"
						resultMeta.FailureCode = "GIT_CHANGE_FAILED"
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
				if finalStatus == models.StatusCompleted && job.CommitChanges && !job.PushChanges && finalResult != "no changes" {
					resultMeta.WorkspaceRetained = true
					output = append(output, []byte(fmt.Sprintf("\n--- WORKTREE RETAINED ---\nLocal commit kept at %s on branch %s because push is disabled for this job.\n", workDir, branchName))...)
				}
				if !resultMeta.WorkspaceRetained {
					if err := gm.CleanupWorktree(mainRepoDir, workDir, branchName); err != nil {
						output = append(output, []byte(fmt.Sprintf("\n--- CLEANUP FAILED ---\n%v\n", err))...)
					}
				}
				w.updateJobStatusFull(job.ID, job.AttemptID, finalStatus, finalResult, output, logSeq, artifacts, pushedBranch, prURL, job.Stages, job.CurrentStage, resultMeta)
			}()
		} else {
			var err error
			workDir, err = execution.SecureWorkspacePath(w.Workspace, ".")
			if err != nil {
				w.updateJobStatus(job.ID, job.AttemptID, models.StatusFailed, "workspace error", []byte(err.Error()+"\n"), logSeq)
				return
			}
		}

		if len(job.Stages) == 0 {
			job.Stages = []models.JobStage{{
				Profile:        job.Profile,
				Parameters:     job.Parameters,
				Tools:          job.Tools,
				TimeoutSeconds: job.TimeoutSeconds,
			}}
		}

		for i, stage := range job.Stages {
			job.CurrentStage = i
			stage.Status = models.StatusRunning
			startedAt := time.Now()
			stage.StartedAt = &startedAt
			job.Stages[i] = stage
			output = append(output, []byte(fmt.Sprintf("\n--- STAGE %d: %s ---\n", i+1, stage.Name))...)
			w.updateJobStatusWithMetadata(job.ID, job.AttemptID, models.StatusRunning, "", output, logSeq, nil, "", "", job.Stages, job.CurrentStage)
			logSeq++

			profile, err := execution.GetProfile(stage.Profile)
			if err != nil {
				stage.Status = models.StatusFailed
				stage.Result = err.Error()
				job.Stages[i] = stage
				finalResult = fmt.Sprintf("stage %d error: %v", i+1, err)
				finalStatus = models.StatusFailed
				break
			}
			if profile.Name == "BootstrapEnvironment" && !w.allowBootstrap {
				errStr := "worker not allowed to bootstrap environment. start worker with FORGEGRID_ALLOW_BOOTSTRAP=true"
				stage.Status = models.StatusFailed
				stage.Result = errStr
				job.Stages[i] = stage
				finalResult = "bootstrap forbidden"
				finalStatus = models.StatusFailed
				break
			}

			timeoutSeconds := stage.TimeoutSeconds
			if timeoutSeconds == 0 || timeoutSeconds > profile.MaxTimeoutSecs {
				timeoutSeconds = profile.MaxTimeoutSecs
			}
			timeout := time.Duration(timeoutSeconds) * time.Second

			execCtx, execCancel := context.WithTimeout(ctx, timeout)

			executor := execution.NewExecutor()
			stageOut, err := executor.Execute(execCtx, profile, stage.Parameters, stage.Tools, workDir)
			output = append(output, stageOut...)

			if err != nil {
				if execCtx.Err() == context.DeadlineExceeded {
					finalResult = fmt.Sprintf("stage %d timeout", i+1)
					finalStatus = models.StatusFailed
					stage.Status = models.StatusFailed
					stage.Result = "timeout"
					output = append(output, []byte(fmt.Sprintf("\nStage %d timed out", i+1))...)
				} else if ctx.Err() == context.Canceled {
					finalResult = "cancelled"
					finalStatus = models.StatusCancelled
					stage.Status = models.StatusFailed
					stage.Result = "cancelled"
					output = append(output, []byte("\nJob cancelled by coordinator")...)
				} else {
					finalResult = fmt.Sprintf("stage %d error: %v", i+1, err)
					finalStatus = models.StatusFailed
					stage.Status = models.StatusFailed
					stage.Result = err.Error()
				}
				endedAt := time.Now()
				stage.EndedAt = &endedAt
				stage.Duration = endedAt.Sub(startedAt).Round(time.Millisecond).String()
				job.Stages[i] = stage
				execCancel()
				break
			}

			endedAt := time.Now()
			stage.Status = models.StatusCompleted
			stage.EndedAt = &endedAt
			stage.Duration = endedAt.Sub(startedAt).Round(time.Millisecond).String()
			job.Stages[i] = stage
			execCancel()
		}

		if finalStatus == models.StatusCompleted && gm != nil && job.CommitChanges {
			validations := runAutoValidation(ctx, workDir, job.RequiredCaps)
			resultMeta.ValidationResults = validations
			for _, validation := range validations {
				output = append(output, []byte(fmt.Sprintf("\n--- VALIDATION: %s ---\n%s\n", validation.Name, validation.Output))...)
				if validation.Status != models.StatusCompleted {
					finalStatus = models.StatusFailed
					finalResult = "validation failed"
					resultMeta.FailureCode = "VALIDATION_FAILED"
					resultMeta.WorkspaceRetained = true
				}
			}
		}

		if gm == nil {
			w.updateJobStatusWithMetadata(job.ID, job.AttemptID, finalStatus, finalResult, output, logSeq, nil, "", "", job.Stages, job.CurrentStage)
		}
	} else {
		w.updateJobStatus(job.ID, job.AttemptID, models.StatusFailed, "unknown task", []byte("Unsupported task type\n"), logSeq)
	}
}

func runAutoValidation(ctx context.Context, workDir string, requiredCaps []string) []models.ValidationResult {
	var validations []models.ValidationResult
	caps := make(map[string]bool)
	for _, cap := range requiredCaps {
		caps[strings.ToLower(strings.TrimSpace(cap))] = true
	}
	add := func(name string, args ...string) {
		if len(args) == 0 {
			return
		}
		start := time.Now()
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = workDir
		out, err := cmd.CombinedOutput()
		end := time.Now()
		status := models.StatusCompleted
		if err != nil {
			status = models.StatusFailed
			out = append(out, []byte(fmt.Sprintf("\n%v", err))...)
		}
		validations = append(validations, models.ValidationResult{
			Name:      name,
			Status:    status,
			Output:    string(out),
			StartedAt: start,
			EndedAt:   end,
			Duration:  end.Sub(start).Round(time.Millisecond).String(),
		})
	}
	if caps["go"] && fileExists(filepath.Join(workDir, "go.mod")) {
		add("Go tests", "go", "test", "./...")
	}
	if caps["python"] || caps["python3"] {
		if fileExists(filepath.Join(workDir, "pyproject.toml")) || fileExists(filepath.Join(workDir, "requirements.txt")) || dirExists(filepath.Join(workDir, "tests")) {
			python := "python"
			if _, err := exec.LookPath(python); err != nil {
				python = "python3"
			}
			add("Python compile", python, "-m", "compileall", "-q", ".")
			if dirExists(filepath.Join(workDir, "tests")) {
				add("Python unittest", python, "-m", "unittest", "discover")
			}
		}
	}
	if caps["node"] && fileExists(filepath.Join(workDir, "package.json")) {
		if packageScriptExists(filepath.Join(workDir, "package.json"), "test") {
			add("Node tests", "npm", "test")
		}
		if packageScriptExists(filepath.Join(workDir, "package.json"), "build") {
			add("Node build", "npm", "run", "build")
		}
	}
	return validations
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func packageScriptExists(path, script string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		return false
	}
	return strings.TrimSpace(pkg.Scripts[script]) != ""
}
