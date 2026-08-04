package agentbridge

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Server struct {
	store *Store
	mu    sync.Mutex
	rates map[string]int
	reset map[string]time.Time
}

func NewServer(s *Store) *Server {
	return &Server{
		store: s,
		rates: make(map[string]int),
		reset: make(map[string]time.Time),
	}
}

func (s *Server) authenticate(r *http.Request) (string, bool) {
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}

	s.mu.Lock()
	now := time.Now()
	if t, ok := s.reset[ip]; ok && now.After(t) {
		s.rates[ip] = 0
		delete(s.reset, ip)
	}
	if s.rates[ip] > 10 {
		s.reset[ip] = now.Add(time.Minute)
		s.mu.Unlock()
		return "", false
	}
	s.mu.Unlock()

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
		s.mu.Lock()
		s.rates[ip]++
		s.mu.Unlock()
		return "", false
	}

	s.mu.Lock()
	s.rates[ip] = 0 // Reset on success
	s.mu.Unlock()

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

	// Reject multiple JSON objects (Fix 6)
	var dummy json.RawMessage
	if err := dec.Decode(&dummy); err != io.EOF {
		http.Error(w, "Bad request: multiple objects", http.StatusBadRequest)
		return
	}

	// Length Validation (Fix 9)
	if len(req.Recipient) == 0 || len(req.Recipient) > 100 {
		http.Error(w, "Bad request: recipient length invalid", http.StatusBadRequest)
		return
	}
	if len(req.TaskID) == 0 || len(req.TaskID) > 100 {
		http.Error(w, "Bad request: task ID length invalid", http.StatusBadRequest)
		return
	}
	if len(req.IdempotencyKey) > 100 {
		http.Error(w, "Bad request: idempotency key too long", http.StatusBadRequest)
		return
	}
	if len(req.Body) == 0 || len(req.Body) > 256*1024 {
		http.Error(w, "Bad request: body too large or empty", http.StatusBadRequest)
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

	msgID, err := GenerateID()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	msg := AgentMessage{
		ID:             msgID,
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

	msg, err = s.store.AddMessage(msg)
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

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/agent-messages/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	id, action := parts[0], parts[1]

	var req struct {
		Result json.RawMessage `json:"result"`
	}

	if action == "complete" || action == "fail" {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512*1024))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "Bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		var dummy json.RawMessage
		if err := dec.Decode(&dummy); err != io.EOF {
			http.Error(w, "Bad request: multiple objects", http.StatusBadRequest)
			return
		}
	}

	msg, err := s.store.TransitionMessage(id, agent, action, req.Result)
	if err != nil {
		if err.Error() == "not found" {
			http.Error(w, "Not found", http.StatusNotFound)
		} else if err.Error() == "forbidden" {
			http.Error(w, "Forbidden", http.StatusForbidden)
		} else if err.Error() == "invalid action" {
			http.Error(w, "Not found", http.StatusNotFound) // map invalid action to 404
		} else {
			http.Error(w, "Internal error", http.StatusInternalServerError)
		}
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

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
