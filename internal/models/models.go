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
	LastSeen          time.Time `json:"last_seen"`
	TokenHash         string    `json:"token_hash"`
	Status            string    `json:"status"` // online, offline
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
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`

	// Git Workspace Info
	RepositoryURL string `json:"repository_url,omitempty"`
	BaseCommit    string `json:"base_commit,omitempty"`
	BranchName    string `json:"branch_name,omitempty"`

	Challenge string `json:"challenge,omitempty"` // For test task
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
	LastSeen          time.Time `json:"last_seen"`
	Status            string    `json:"status"`
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
		LastSeen:          w.LastSeen,
		Status:            w.Status,
	}
}
