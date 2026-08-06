package agentbridge

import (
	"encoding/json"
	"time"
)

type MessageType string

const (
	TypeInstruction     MessageType = "instruction"
	TypeAcknowledgement MessageType = "acknowledgement"
	TypeProgress        MessageType = "progress"
	TypeResult          MessageType = "result"
	TypeError           MessageType = "error"
	TypeQuestion        MessageType = "question"
	TypeAnswer          MessageType = "answer"
	TypeShutdownNotice  MessageType = "shutdown_notice"
	TypeChat            MessageType = "chat"
	TypeSystem          MessageType = "system"
)

type MessageStatus string

const (
	StatusPending      MessageStatus = "pending"
	StatusAcknowledged MessageStatus = "acknowledged"
	StatusCompleted    MessageStatus = "completed"
	StatusFailed       MessageStatus = "failed"
	StatusExpired      MessageStatus = "expired"
)

type AgentMessage struct {
	ID             string          `json:"id"`
	Sender         string          `json:"sender"`
	Recipient      string          `json:"recipient"`
	TaskID         string          `json:"task_id"`
	Type           MessageType     `json:"type"`
	Body           string          `json:"body"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	ExpiresAt      time.Time       `json:"expires_at"`
	Status         MessageStatus   `json:"status"`
	AcknowledgedAt *time.Time      `json:"acknowledged_at,omitempty"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
	Receipts       map[string]*MessageReceipt `json:"receipts,omitempty"` // For #all-agents
}

type MessageReceipt struct {
	Status         MessageStatus   `json:"status"`
	AcknowledgedAt *time.Time      `json:"acknowledged_at,omitempty"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
}

type AgentRegistration struct {
	Name      string    `json:"name"`
	TokenHash string    `json:"token_hash"`
	CreatedAt time.Time `json:"created_at"`
}
