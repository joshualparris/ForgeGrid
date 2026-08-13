package agentbridge

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	srv := &Server{
		store: s,
		rates: make(map[string]int),
		reset: make(map[string]time.Time),
	}
	go srv.cleanupLoop()
	return srv
}

func (s *Server) cleanupLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		if len(s.rates) > 10000 {
			s.rates = make(map[string]int)
			s.reset = make(map[string]time.Time)
		} else {
			for ip, t := range s.reset {
				if now.After(t) {
					delete(s.rates, ip)
					delete(s.reset, ip)
				}
			}
		}
		s.mu.Unlock()

		// Enforce retention limit for ordinary messages
		s.store.EnforceRetention(1000)
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
		delete(s.rates, ip)
		delete(s.reset, ip)
	}
	if s.rates[ip] > 10 {
		s.reset[ip] = now.Add(time.Minute)
		s.mu.Unlock()
		return "", false
	}
	s.mu.Unlock()

	fail := func() (string, bool) {
		s.mu.Lock()
		s.rates[ip]++
		if len(s.rates) > 20000 {
			s.rates = make(map[string]int)
			s.reset = make(map[string]time.Time)
		}
		s.mu.Unlock()
		return "", false
	}

	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return fail()
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return fail()
	}
	agentName, secret := parts[0], parts[1]

	hash := sha256.Sum256([]byte(secret))
	hashStr := hex.EncodeToString(hash[:])

	agent, ok := s.store.GetAgent(agentName)
	if !ok || subtle.ConstantTimeCompare([]byte(agent.TokenHash), []byte(hashStr)) != 1 {
		if err := s.store.Reload(); err == nil {
			agent, ok = s.store.GetAgent(agentName)
		}
	}
	if !ok || subtle.ConstantTimeCompare([]byte(agent.TokenHash), []byte(hashStr)) != 1 {
		return fail()
	}

	s.mu.Lock()
	delete(s.rates, ip)
	delete(s.reset, ip)
	s.mu.Unlock()

	return agentName, true
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/agent-messages", s.handleMessages)
	mux.HandleFunc("/api/v1/agent-messages/inbox", s.handleInbox)
	mux.HandleFunc("/api/v1/agent-messages/agents", s.handleAgents)
	mux.HandleFunc("/api/v1/agent-messages/", s.handleMessageActionOrDelivery)
	mux.HandleFunc("/api/v1/agent-status", s.handleStatus)
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	_, ok := s.authenticate(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	agents := s.store.GetAgentNames()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agents)
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
	if req.Type == TypeInstruction || req.Type == TypeProgress || req.Type == TypeResult {
		if len(req.TaskID) == 0 || len(req.TaskID) > 100 {
			http.Error(w, "Bad request: task ID length invalid", http.StatusBadRequest)
			return
		}
	} else if len(req.TaskID) > 100 {
		http.Error(w, "Bad request: task ID too long", http.StatusBadRequest)
		return
	}
	if len(req.IdempotencyKey) > 100 {
		http.Error(w, "Bad request: idempotency key too long", http.StatusBadRequest)
		return
	}
	if len(req.Body) == 0 || len(req.Body) > 16*1024 {
		http.Error(w, "Bad request: body too large or empty", http.StatusBadRequest)
		return
	}

	if req.Recipient != "#all-agents" {
		if _, ok := s.store.GetAgent(req.Recipient); !ok {
			http.Error(w, "Recipient not found", http.StatusNotFound)
			return
		}
	}

	// Validate type
	switch req.Type {
	case TypeInstruction, TypeAcknowledgement, TypeProgress, TypeResult, TypeError, TypeQuestion, TypeAnswer, TypeShutdownNotice, TypeChat, TypeSystem:
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

	limit := 100
	offset := 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if n, _ := fmt.Sscanf(l, "%d", &limit); n != 1 || limit <= 0 || limit > 1000 {
			limit = 100
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, _ := fmt.Sscanf(o, "%d", &offset); n != 1 || offset < 0 {
			offset = 0
		}
	}

	var statuses []MessageStatus
	if st := r.URL.Query().Get("status"); st != "" {
		for _, s := range strings.Split(st, ",") {
			statuses = append(statuses, MessageStatus(s))
		}
	} else {
		// By default, just like the old behavior, only return actionable messages
		statuses = []MessageStatus{StatusPending, StatusAcknowledged}
	}

	includeOutgoing := r.URL.Query().Get("include_outgoing") == "true"
	inbox := s.store.GetInbox(recipient, limit, offset, includeOutgoing, statuses...)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(inbox)
}

func (s *Server) handleMessageActionOrDelivery(w http.ResponseWriter, r *http.Request) {
	agent, ok := s.authenticate(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != http.MethodPost && r.Method != http.MethodGet {
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

	if action == "delivery" {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		msg, ok := s.store.GetMessage(id)
		if !ok {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		if msg.Sender != agent {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(msg)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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
