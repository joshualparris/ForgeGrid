package agentbridge

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Store struct {
	mu       sync.RWMutex
	dataDir  string
	agents   map[string]AgentRegistration
	messages map[string]AgentMessage
}

func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	dataDir := filepath.Join(home, ".local", "share", "forgegrid", "agentbridge")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create agentbridge data dir: %w", err)
	}

	s := &Store{
		dataDir:  dataDir,
		agents:   make(map[string]AgentRegistration),
		messages: make(map[string]AgentMessage),
	}

	if err := s.load(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Store) load() error {
	agentsPath := filepath.Join(s.dataDir, "agents.json")
	if b, err := os.ReadFile(agentsPath); err == nil {
		if err := json.Unmarshal(b, &s.agents); err != nil {
			return fmt.Errorf("corrupt agents.json: %w", err)
		}
	}

	msgsPath := filepath.Join(s.dataDir, "messages.json")
	if b, err := os.ReadFile(msgsPath); err == nil {
		if err := json.Unmarshal(b, &s.messages); err != nil {
			return fmt.Errorf("corrupt messages.json: %w", err)
		}
	}
	return nil
}

func (s *Store) save() error {
	// Save agents
	agB, err := json.MarshalIndent(s.agents, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(s.dataDir, "agents.json"), agB); err != nil {
		return err
	}

	// Save messages
	msgB, err := json.MarshalIndent(s.messages, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(s.dataDir, "messages.json"), msgB); err != nil {
		return err
	}
	return nil
}

func atomicWrite(path string, data []byte) error {
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (s *Store) RegisterAgent(name, tokenHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.agents[name] = AgentRegistration{
		Name:      name,
		TokenHash: tokenHash,
		CreatedAt: time.Now(),
	}
	return s.save()
}

func (s *Store) GetAgent(name string) (AgentRegistration, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.agents[name]
	return a, ok
}

func (s *Store) AddMessage(msg AgentMessage) (AgentMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if msg.IdempotencyKey != "" {
		for _, existing := range s.messages {
			if existing.Sender == msg.Sender && existing.IdempotencyKey == msg.IdempotencyKey {
				return existing, nil // Return existing message
			}
		}
	}

	if _, ok := s.messages[msg.ID]; ok {
		return msg, nil // Idempotent fallback
	}

	if msg.Recipient == "#all-agents" {
		msg.Receipts = make(map[string]*MessageReceipt)
		for name := range s.agents {
			if name != msg.Sender { // Exclude sender from actionable Receipts
				msg.Receipts[name] = &MessageReceipt{
					Status: StatusPending,
				}
			}
		}
	}

	s.messages[msg.ID] = msg
	return msg, s.save()
}

func (s *Store) GetMessage(id string) (AgentMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	msg, ok := s.messages[id]
	return msg, ok
}

func (s *Store) TransitionMessage(id, recipient, action string, result json.RawMessage) (AgentMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msg, ok := s.messages[id]
	if !ok {
		return AgentMessage{}, fmt.Errorf("not found")
	}

	if msg.Recipient != recipient && msg.Recipient != "#all-agents" {
		return AgentMessage{}, fmt.Errorf("forbidden")
	}

	if msg.Recipient == "#all-agents" {
		receipt, ok := msg.Receipts[recipient]
		if !ok {
			return AgentMessage{}, fmt.Errorf("forbidden") // True broadcast snapshot enforcement
		}

		now := time.Now()
		switch action {
		case "acknowledge":
			if receipt.Status == StatusPending {
				receipt.Status = StatusAcknowledged
				receipt.AcknowledgedAt = &now
			}
		case "complete":
			if receipt.Status != StatusCompleted && receipt.Status != StatusFailed {
				receipt.Status = StatusCompleted
				receipt.CompletedAt = &now
				receipt.Result = result
			}
		case "fail":
			if receipt.Status != StatusCompleted && receipt.Status != StatusFailed {
				receipt.Status = StatusFailed
				receipt.CompletedAt = &now
				receipt.Result = result
			}
		default:
			return AgentMessage{}, fmt.Errorf("invalid action")
		}
	} else {
		now := time.Now()
		switch action {
		case "acknowledge":
			if msg.Status == StatusPending {
				msg.Status = StatusAcknowledged
				msg.AcknowledgedAt = &now
			}
		case "complete":
			if msg.Status != StatusCompleted && msg.Status != StatusFailed {
				msg.Status = StatusCompleted
				msg.CompletedAt = &now
				msg.Result = result
			}
		case "fail":
			if msg.Status != StatusCompleted && msg.Status != StatusFailed {
				msg.Status = StatusFailed
				msg.CompletedAt = &now
				msg.Result = result
			}
		default:
			return AgentMessage{}, fmt.Errorf("invalid action")
		}
	}

	s.messages[id] = msg
	if err := s.save(); err != nil {
		return AgentMessage{}, err
	}

	return msg, nil
}

func (s *Store) GetInbox(recipient string, limit, offset int, includeOutgoing bool, statuses ...MessageStatus) []AgentMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	statusMap := make(map[MessageStatus]bool)
	for _, st := range statuses {
		statusMap[st] = true
	}

	var inbox []AgentMessage
	for _, m := range s.messages {
		isRecipient := false
		isSender := (m.Sender == recipient)

		if m.Recipient == recipient {
			isRecipient = true
		} else if m.Recipient == "#all-agents" {
			if _, ok := m.Receipts[recipient]; ok {
				isRecipient = true
			}
		}

		if (isRecipient || (isSender && includeOutgoing)) && time.Now().Before(m.ExpiresAt) {
			mCopy := m

			if m.Recipient == "#all-agents" {
				if receipt, ok := m.Receipts[recipient]; ok {
					mCopy.Status = receipt.Status
					mCopy.AcknowledgedAt = receipt.AcknowledgedAt
					mCopy.CompletedAt = receipt.CompletedAt
					mCopy.Result = receipt.Result
				} else if isSender {
					mCopy.Status = StatusCompleted // Sender sees its own group message as completed
				}
			}

			// Clear Receipts map to enforce receipt privacy
			mCopy.Receipts = nil

			if len(statusMap) == 0 || statusMap[mCopy.Status] {
				inbox = append(inbox, mCopy)
			}
		}
	}

	// Sort newest first
	sort.Slice(inbox, func(i, j int) bool {
		return inbox[i].CreatedAt.After(inbox[j].CreatedAt)
	})

	if offset >= len(inbox) {
		return []AgentMessage{}
	}

	end := offset + limit
	if end > len(inbox) {
		end = len(inbox)
	}

	return inbox[offset:end]
}

func (s *Store) EnforceRetention(limit int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.messages) <= limit {
		return nil
	}

	var all []AgentMessage
	for _, m := range s.messages {
		all = append(all, m)
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})

	// Keep only the newest 'limit' messages
	s.messages = make(map[string]AgentMessage)
	for i := 0; i < limit; i++ {
		s.messages[all[i].ID] = all[i]
	}

	return s.save()
}

func GenerateID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
