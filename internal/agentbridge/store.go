package agentbridge

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	s.messages[msg.ID] = msg
	return msg, s.save()
}

func (s *Store) GetMessage(id string) (AgentMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	msg, ok := s.messages[id]
	return msg, ok
}

func (s *Store) UpdateMessage(msg AgentMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[msg.ID] = msg
	return s.save()
}

func (s *Store) GetInbox(recipient string) []AgentMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var inbox []AgentMessage
	for _, m := range s.messages {
		if m.Recipient == recipient && time.Now().Before(m.ExpiresAt) {
			if m.Status == StatusPending || m.Status == StatusAcknowledged {
				inbox = append(inbox, m)
			}
		}
	}
	return inbox
}

func GenerateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
