package coordinator

import (
	"crypto/sha256"
	"encoding/hex"
	"forgegrid/internal/agentbridge"
	"forgegrid/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeGateway struct {
	messages []agentbridge.AgentMessage
	agents   []string
}

func (f *fakeGateway) SendMessage(recipient, taskID string, msgType agentbridge.MessageType, body string, ttl int, idempotencyKey string) (*agentbridge.AgentMessage, error) {
	for _, m := range f.messages {
		if m.IdempotencyKey == idempotencyKey {
			return &m, nil // Idempotency retry
		}
	}
	msg := agentbridge.AgentMessage{
		ID:             "msg-123",
		Sender:         "coordinator",
		Recipient:      recipient,
		TaskID:         taskID,
		Type:           msgType,
		Body:           body,
		Status:         agentbridge.StatusPending,
		IdempotencyKey: idempotencyKey,
		CreatedAt:      time.Now(),
		ExpiresAt:      time.Now().Add(time.Duration(ttl) * time.Second),
	}
	f.messages = append(f.messages, msg)
	return &msg, nil
}

func (f *fakeGateway) GetInbox(limit, offset int, includeOutgoing bool, statuses ...agentbridge.MessageStatus) ([]agentbridge.AgentMessage, error) {
	return f.messages, nil
}

func (f *fakeGateway) GetAgents() ([]string, error) {
	return f.agents, nil
}

func (f *fakeGateway) GetDeliveryStatus(id string) (*agentbridge.AgentMessage, error) {
	if len(f.messages) > 0 {
		msg := f.messages[0]
		if msg.Sender != "coordinator" {
			return nil, nil // shouldn't happen in our tests mostly
		}
		return &msg, nil
	}
	return nil, nil
}

func (f *fakeGateway) Acknowledge(id string) (*agentbridge.AgentMessage, error) {
	if len(f.messages) > 0 {
		f.messages[0].Status = agentbridge.StatusAcknowledged
		now := time.Now()
		f.messages[0].AcknowledgedAt = &now
		return &f.messages[0], nil
	}
	return nil, nil
}

func (f *fakeGateway) Status() (string, bool, error) {
	return "coordinator-agent", true, nil
}

func setupTestCoordinator(t *testing.T) (*Coordinator, *fakeGateway) {
	s, _ := store.NewStore(t.TempDir())
	s.CoordinatorCfg.AdminToken = "testtoken"
	s.CoordinatorCfg.Identity = "coordinator"
	s.Save()

	c := New(s, true)
	c.IP = "127.0.0.1"

	gw := &fakeGateway{
		agents: []string{"agent-1", "agent-2"},
	}
	c.MessagingGateway = gw

	return c, gw
}

func TestMessaging_ConfigLoading(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agentclient.json")

	// Valid config
	os.WriteFile(path, []byte(`{"name":"testagent","token":"sec","url":"http://127.0.0.1","fingerprint":"fp"}`), 0600)
	cfg, err := agentbridge.LoadClientConfig(path)
	if err != nil || cfg.Name != "testagent" {
		t.Fatalf("Expected successful load: %v", err)
	}

	// Missing field
	os.WriteFile(path, []byte(`{"name":"","token":"sec","url":"http://127.0.0.1","fingerprint":"fp"}`), 0600)
	_, err = agentbridge.LoadClientConfig(path)
	if err == nil {
		t.Fatalf("Expected error for missing name")
	}

	// Malformed JSON
	os.WriteFile(path, []byte(`{"name":"testagent"`), 0600)
	_, err = agentbridge.LoadClientConfig(path)
	if err == nil {
		t.Fatalf("Expected error for malformed json")
	}
}

func TestMessaging_LiveIntegration(t *testing.T) {
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)
	os.Setenv("HOME", t.TempDir())

	abStore, _ := agentbridge.NewStore()
	tokenHash := sha256.Sum256([]byte("secret"))
	abStore.RegisterAgent("coord-agent", hex.EncodeToString(tokenHash[:]))
	abServer := agentbridge.NewServer(abStore)

	mux := http.NewServeMux()
	abServer.RegisterRoutes(mux)
	ts := httptest.NewTLSServer(mux)
	defer ts.Close()
	fpBytes := sha256.Sum256(ts.Certificate().Raw)
	fingerprint := hex.EncodeToString(fpBytes[:])

	// Write agentclient.json to the expected path
	cfgPath := agentbridge.GetConfigPath()
	os.MkdirAll(filepath.Dir(cfgPath), 0700)
	os.WriteFile(cfgPath, []byte(`{"name":"coord-agent","token":"secret","url":"`+ts.URL+`","fingerprint":"`+fingerprint+`"}`), 0600)

	gw, err := NewLiveMessagingGateway()
	if err != nil {
		t.Fatalf("NewLiveMessagingGateway failed: %v", err)
	}

	id, available, err := gw.Status()
	if err != nil || !available || id != "coord-agent" {
		t.Fatalf("Status failed: %v, available=%v, id=%s", err, available, id)
	}
}

func TestMessaging_SenderIdentityServerSide(t *testing.T) {
	c, gw := setupTestCoordinator(t)

	reqBody := `{"recipient": "agent-1", "type": "chat", "body": "hello", "idempotency_key": "123"}`
	req := httptest.NewRequest("POST", "/api/dashboard/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://example.com")
	req.Host = "example.com"

	w := httptest.NewRecorder()
	c.handleMessages(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Ensure sender is "coordinator" (enforced by the gateway, browser input ignored)
	if gw.messages[0].Sender != "coordinator" {
		t.Errorf("Sender should be forced to coordinator identity, got %s", gw.messages[0].Sender)
	}
}

func TestMessaging_Idempotency(t *testing.T) {
	c, gw := setupTestCoordinator(t)

	reqBody := `{"recipient": "agent-1", "type": "chat", "body": "hello", "idempotency_key": "abc"}`

	// First send
	req1 := httptest.NewRequest("POST", "/api/dashboard/messages", strings.NewReader(reqBody))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Origin", "http://example.com")
	req1.Host = "example.com"

	w1 := httptest.NewRecorder()
	c.handleMessages(w1, req1)

	if len(gw.messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(gw.messages))
	}

	// Retry
	req2 := httptest.NewRequest("POST", "/api/dashboard/messages", strings.NewReader(reqBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Origin", "http://example.com")
	req2.Host = "example.com"

	w2 := httptest.NewRecorder()
	c.handleMessages(w2, req2)

	if len(gw.messages) != 1 {
		t.Fatalf("Expected exactly 1 message after retry, got %d", len(gw.messages))
	}
}

func TestMessaging_SameOriginProtection(t *testing.T) {
	c, _ := setupTestCoordinator(t)

	reqBody := `{"recipient": "agent-1", "type": "chat", "body": "hello", "idempotency_key": "123"}`

	// Exact match
	req := httptest.NewRequest("POST", "/api/dashboard/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://example.com")
	req.Host = "example.com"
	w := httptest.NewRecorder()
	c.handleMessages(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	// Cross-origin
	req = httptest.NewRequest("POST", "/api/dashboard/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.com")
	req.Host = "example.com"
	w = httptest.NewRecorder()
	c.handleMessages(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for cross-origin, got %d", w.Code)
	}

	// Missing origin and sec-fetch-site
	req = httptest.NewRequest("POST", "/api/dashboard/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "example.com"
	w = httptest.NewRecorder()
	c.handleMessages(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for missing origin headers, got %d", w.Code)
	}

	// Host suffix trick
	req = httptest.NewRequest("POST", "/api/dashboard/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://anotherexample.com")
	req.Host = "example.com"
	w = httptest.NewRecorder()
	c.handleMessages(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for host suffix trick, got %d", w.Code)
	}
}

func TestMessaging_OversizedMessageRejected(t *testing.T) {
	c, _ := setupTestCoordinator(t)

	largeBody := strings.Repeat("a", 16385)
	reqBody := `{"recipient": "agent-1", "type": "chat", "body": "` + largeBody + `", "idempotency_key": "123"}`
	req := httptest.NewRequest("POST", "/api/dashboard/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://example.com")
	req.Host = "example.com"

	w := httptest.NewRecorder()
	c.handleMessages(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected 413 for oversized body, got %d", w.Code)
	}
}

func TestMessaging_JSONValidation(t *testing.T) {
	c, _ := setupTestCoordinator(t)

	// Trailing JSON
	reqBody := `{"recipient": "agent-1", "type": "chat", "body": "hello", "idempotency_key": "123"}{}`
	req := httptest.NewRequest("POST", "/api/dashboard/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://example.com")
	req.Host = "example.com"
	w := httptest.NewRecorder()
	c.handleMessages(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for trailing JSON, got %d", w.Code)
	}

	// Unknown fields
	reqBody = `{"recipient": "agent-1", "type": "chat", "body": "hello", "idempotency_key": "123", "unknown": "x"}`
	req = httptest.NewRequest("POST", "/api/dashboard/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://example.com")
	req.Host = "example.com"
	w = httptest.NewRecorder()
	c.handleMessages(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for unknown fields, got %d", w.Code)
	}

	// Empty body
	req = httptest.NewRequest("POST", "/api/dashboard/messages", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://example.com")
	req.Host = "example.com"
	w = httptest.NewRecorder()
	c.handleMessages(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for empty body, got %d", w.Code)
	}

	// Application/JSON with charset
	reqBody = `{"recipient": "agent-1", "type": "chat", "body": "hello", "idempotency_key": "123"}`
	req = httptest.NewRequest("POST", "/api/dashboard/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Origin", "http://example.com")
	req.Host = "example.com"
	w = httptest.NewRecorder()
	c.handleMessages(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for application/json with charset, got %d", w.Code)
	}

	// Missing Task ID for Instruction
	reqBody = `{"recipient": "agent-1", "type": "instruction", "body": "do", "idempotency_key": "123"}`
	req = httptest.NewRequest("POST", "/api/dashboard/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://example.com")
	req.Host = "example.com"
	w = httptest.NewRecorder()
	c.handleMessages(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing task ID on instruction, got %d", w.Code)
	}
}

func TestMessaging_UnavailableAgentBridge(t *testing.T) {
	c, _ := setupTestCoordinator(t)
	c.MessagingGateway = nil // Simulate unavailable

	req := httptest.NewRequest("GET", "/api/dashboard/messaging/agents", nil)
	w := httptest.NewRecorder()

	c.handleMessagingAgents(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503 for unavailable gateway, got %d", w.Code)
	}
}
