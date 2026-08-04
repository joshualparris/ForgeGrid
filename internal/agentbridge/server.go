package agentbridge

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	store *Store
}

func NewServer(s *Store) *Server {
	return &Server{store: s}
}

func (s *Server) authenticate(r *http.Request) (string, bool) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return "", false
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return "", false
	}
	agentName, secret := parts[0], parts[1]

	agent, ok := s.store.GetAgent(agentName)
	if !ok {
		return "", false
	}

	hash := sha256.Sum256([]byte(secret))
	hashStr := hex.EncodeToString(hash[:])

	if subtle.ConstantTimeCompare([]byte(agent.TokenHash), []byte(hashStr)) != 1 {
		return "", false
	}
	return agentName, true
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/agent-messages", s.handleMessages)
	mux.HandleFunc("/api/v1/agent-messages/inbox", s.handleInbox)
	mux.HandleFunc("/api/v1/agent-messages/", s.handleMessageAction)
	mux.HandleFunc("/api/v1/agent-status", s.handleStatus)
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	sender, ok := s.authenticate(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Recipient      string      `json:"recipient"`
		TaskID         string      `json:"task_id"`
		Type           MessageType `json:"type"`
		Body           string      `json:"body"`
		TTL            int         `json:"ttl_seconds"`
		IdempotencyKey string      `json:"idempotency_key,omitempty"`
	}

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512*1024)) // 512KB max
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "Bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	// Reject multiple JSON objects
	if dec.More() {
		http.Error(w, "Bad request: multiple objects", http.StatusBadRequest)
		return
	}

	if req.Recipient == "" || req.TaskID == "" || req.Body == "" {
		http.Error(w, "Bad request: missing required fields", http.StatusBadRequest)
		return
	}
	if len(req.Body) > 256*1024 {
		http.Error(w, "Bad request: body too large", http.StatusBadRequest)
		return
	}

	if _, ok := s.store.GetAgent(req.Recipient); !ok {
		http.Error(w, "Recipient not found", http.StatusNotFound)
		return
	}

	// Validate type
	switch req.Type {
	case TypeInstruction, TypeAcknowledgement, TypeProgress, TypeResult, TypeError, TypeQuestion, TypeAnswer, TypeShutdownNotice:
	default:
		http.Error(w, "Invalid message type", http.StatusBadRequest)
		return
	}

	if req.TTL <= 0 {
		req.TTL = 3600 // default 1 hour
	} else if req.TTL > 86400*7 {
		req.TTL = 86400 * 7 // cap at 1 week
	}

	msg := AgentMessage{
		ID:             GenerateID(),
		Sender:         sender,
		Recipient:      req.Recipient,
		TaskID:         req.TaskID,
		Type:           req.Type,
		Body:           req.Body,
		CreatedAt:      time.Now(),
		ExpiresAt:      time.Now().Add(time.Duration(req.TTL) * time.Second),
		Status:         StatusPending,
		IdempotencyKey: req.IdempotencyKey,
	}

	msg, err := s.store.AddMessage(msg)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(msg)
}

func (s *Server) handleInbox(w http.ResponseWriter, r *http.Request) {
	recipient, ok := s.authenticate(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	inbox := s.store.GetInbox(recipient)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(inbox)
}

func (s *Server) handleMessageAction(w http.ResponseWriter, r *http.Request) {
	agent, ok := s.authenticate(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/agent-messages/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	id, action := parts[0], parts[1]

	msg, ok := s.store.GetMessage(id)
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	// Only recipient can act on message (except maybe sender querying status)
	if msg.Recipient != agent {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	now := time.Now()

	switch action {
	case "acknowledge":
		if msg.Status == StatusPending {
			msg.Status = StatusAcknowledged
			msg.AcknowledgedAt = &now
			s.store.UpdateMessage(msg)
		}
	case "complete":
		var req struct {
			Result json.RawMessage `json:"result"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "Bad request: "+err.Error(), http.StatusBadRequest)
			return
		}

		if msg.Status != StatusCompleted && msg.Status != StatusFailed {
			msg.Status = StatusCompleted
			msg.CompletedAt = &now
			msg.Result = req.Result
			s.store.UpdateMessage(msg)
		}
	case "fail":
		var req struct {
			Result json.RawMessage `json:"result"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "Bad request: "+err.Error(), http.StatusBadRequest)
			return
		}

		if msg.Status != StatusCompleted && msg.Status != StatusFailed {
			msg.Status = StatusFailed
			msg.CompletedAt = &now
			msg.Result = req.Result
			s.store.UpdateMessage(msg)
		}
	default:
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msg)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	_, ok := s.authenticate(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Basic status
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
