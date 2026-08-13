package models

import "time"

// Internal State Models

type WorkerState struct {
	ID                string    `json:"id"`
	NodeName          string    `json:"node_name"`
	OS                string    `json:"os"`
	OSVersion         string    `json:"os_version"`
	CPUModel          string    `json:"cpu_model"`
	Architecture      string    `json:"architecture"`
	PhysicalCores     int       `json:"physical_cores"`
	LogicalProcessors int       `json:"logical_processors"`
	TotalRAM          uint64    `json:"total_ram"`
	AvailableRAM      uint64    `json:"available_ram"`
	FreeWorkspaceDisk uint64    `json:"free_workspace_disk"`
	Labels            []string  `json:"labels,omitempty"`
	Capabilities      []string  `json:"capabilities,omitempty"`
	LastSeen          time.Time `json:"last_seen"`
	TokenHash         string    `json:"token_hash"`
	Status            string    `json:"status"` // online, offline
	Drain             bool      `json:"drain,omitempty"`
	Disabled          bool      `json:"disabled,omitempty"`
}

type JobStatus string

const (
	StatusPending         JobStatus = "PENDING"
	StatusClaimed         JobStatus = "CLAIMED"
	StatusRunning         JobStatus = "RUNNING"
	StatusCancelRequested JobStatus = "CANCEL_REQUESTED"
	StatusCancelled       JobStatus = "CANCELLED"
	StatusCompleted       JobStatus = "COMPLETED"
	StatusFailed          JobStatus = "FAILED"
)

type Job struct {
	ID        string     `json:"id"`
	AttemptID string     `json:"attempt_id,omitempty"` // For duplicate-attempt prevention
	WorkerID  string     `json:"worker_id"`
	Task      string     `json:"task"`
	Status    JobStatus  `json:"status"`
	StartTime *time.Time `json:"start_time,omitempty"`
	EndTime   *time.Time `json:"end_time,omitempty"`
	Result    string     `json:"result,omitempty"`
	Logs      []byte     `json:"logs,omitempty"`
	LogSeq    int        `json:"log_seq,omitempty"`

	// Structured Execution
	Profile        string            `json:"profile,omitempty"`
	Parameters     map[string]string `json:"parameters,omitempty"`
	Tools          []string          `json:"tools,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	Artefacts      []string          `json:"artefacts,omitempty"`
	Artifacts      []Artifact        `json:"artifacts,omitempty"`
	RequiredLabels []string          `json:"required_labels,omitempty"`
	RequiredCaps   []string          `json:"required_capabilities,omitempty"`
	MaxRetries     int               `json:"max_retries,omitempty"`
	RetryCount     int               `json:"retry_count,omitempty"`
	RetryOf        string            `json:"retry_of,omitempty"`
	
	// Multi-stage Orchestration
	Stages       []JobStage `json:"stages,omitempty"`
	CurrentStage int        `json:"current_stage,omitempty"`

	// Git Workspace Info
	RepositoryURL string `json:"repository_url,omitempty"`
	BaseCommit    string `json:"base_commit,omitempty"`
	BranchName    string `json:"branch_name,omitempty"`
	CommitChanges bool   `json:"commit_changes,omitempty"`
	PushChanges   bool   `json:"push_changes,omitempty"`
	CommitMessage string `json:"commit_message,omitempty"`
	CreatePR      bool   `json:"create_pr,omitempty"`
	PRTitle       string `json:"pr_title,omitempty"`
	PRBody        string `json:"pr_body,omitempty"`

	PushedBranch string `json:"pushed_branch,omitempty"`
	PRURL        string `json:"pr_url,omitempty"`

	Challenge string `json:"challenge,omitempty"` // For test task
}

type JobStage struct {
	Name           string            `json:"name"`
	Profile        string            `json:"profile"`
	Parameters     map[string]string `json:"parameters,omitempty"`
	Tools          []string          `json:"tools,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	Status         JobStatus         `json:"status"` // PENDING, RUNNING, COMPLETED, FAILED
	Result         string            `json:"result,omitempty"`
}

type Artifact struct {
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	SHA256        string `json:"sha256"`
	DownloadURL   string `json:"download_url,omitempty"`
	ContentBase64 string `json:"content_base64,omitempty"`
	Packaged      bool   `json:"packaged,omitempty"`
	PackageName   string `json:"package_name,omitempty"`
}

type CoordinatorState struct {
	Identity        string    `json:"identity"`
	PairingCode     string    `json:"pairing_code"`
	PairingExpiry   time.Time `json:"pairing_expiry"`
	PairingFailures int       `json:"pairing_failures"`
	AdminToken      string    `json:"admin_token"`
	CertPEM         []byte    `json:"cert_pem,omitempty"`
	KeyPEM          []byte    `json:"key_pem,omitempty"`
}

// Public API DTOs

type WorkerDTO struct {
	ID                string    `json:"id"`
	NodeName          string    `json:"node_name"`
	OS                string    `json:"os"`
	OSVersion         string    `json:"os_version"`
	CPUModel          string    `json:"cpu_model"`
	Architecture      string    `json:"architecture"`
	PhysicalCores     int       `json:"physical_cores"`
	LogicalProcessors int       `json:"logical_processors"`
	TotalRAM          uint64    `json:"total_ram"`
	AvailableRAM      uint64    `json:"available_ram"`
	FreeWorkspaceDisk uint64    `json:"free_workspace_disk"`
	Labels            []string  `json:"labels,omitempty"`
	Capabilities      []string  `json:"capabilities,omitempty"`
	LastSeen          time.Time `json:"last_seen"`
	Status            string    `json:"status"`
	Drain             bool      `json:"drain,omitempty"`
	Disabled          bool      `json:"disabled,omitempty"`
}

type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

func (w *WorkerState) ToDTO() WorkerDTO {
	return WorkerDTO{
		ID:                w.ID,
		NodeName:          w.NodeName,
		OS:                w.OS,
		OSVersion:         w.OSVersion,
		CPUModel:          w.CPUModel,
		Architecture:      w.Architecture,
		PhysicalCores:     w.PhysicalCores,
		LogicalProcessors: w.LogicalProcessors,
		TotalRAM:          w.TotalRAM,
		AvailableRAM:      w.AvailableRAM,
		FreeWorkspaceDisk: w.FreeWorkspaceDisk,
		Labels:            append([]string{}, w.Labels...),
		Capabilities:      append([]string{}, w.Capabilities...),
		LastSeen:          w.LastSeen,
		Status:            w.Status,
		Drain:             w.Drain,
		Disabled:          w.Disabled,
	}
}
