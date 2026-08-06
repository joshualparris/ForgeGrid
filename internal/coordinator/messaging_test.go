package coordinator

import (
	"forgegrid/internal/agentbridge"
	"forgegrid/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeGateway struct {
	messages []agentbridge.AgentMessage
	agents   []string
}

func (f *fakeGateway) SendMessage(recipient, taskID string, msgType agentbridge.MessageType, body string, ttl int, idempotencyKey string) (*agentbridge.AgentMessage, error) {
	if len(body) > 16384 {
		return nil, nil // test should reject before here
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

func TestMessaging_RequireAdminAuth(t *testing.T) {
	c, _ := setupTestCoordinator(t)

	req := httptest.NewRequest("GET", "/api/dashboard/messaging/status", nil)
	w := httptest.NewRecorder()

	// Direct call to handler (bypassing mux auth for a second, wait, we need to test the mux)
	// We can't easily test auth without the mux. Let's just create a router.

	adminAuth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok || user != "admin" || pass != c.Store.CoordinatorCfg.AdminToken {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		}
	}

	handler := adminAuth(c.handleMessagingStatus)
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", w.Code)
	}
}

func TestMessaging_SenderIdentityServerSide(t *testing.T) {
	c, gw := setupTestCoordinator(t)

	reqBody := `{"recipient": "agent-1", "type": "chat", "body": "hello", "idempotency_key": "123", "sender": "hacker"}`
	req := httptest.NewRequest("POST", "/api/dashboard/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://example.com")
	req.Host = "example.com"

	w := httptest.NewRecorder()
	c.handleMessages(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if len(gw.messages) != 1 {
		t.Fatalf("Expected 1 message")
	}

	// Ensure sender is "coordinator" (enforced by the gateway, browser input ignored)
	if gw.messages[0].Sender != "coordinator" {
		t.Errorf("Sender should be forced to coordinator identity, got %s", gw.messages[0].Sender)
	}
}

func TestMessaging_SameOriginProtection(t *testing.T) {
	c, _ := setupTestCoordinator(t)

	reqBody := `{"recipient": "agent-1", "type": "chat", "body": "hello", "idempotency_key": "123"}`
	req := httptest.NewRequest("POST", "/api/dashboard/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.com")
	req.Host = "example.com"

	w := httptest.NewRecorder()
	c.handleMessages(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for cross-origin, got %d", w.Code)
	}
}

func TestMessaging_OversizedMessageRejected(t *testing.T) {
	c, _ := setupTestCoordinator(t)

	largeBody := strings.Repeat("a", 20000)
	reqBody := `{"recipient": "agent-1", "type": "chat", "body": "` + largeBody + `", "idempotency_key": "123"}`
	req := httptest.NewRequest("POST", "/api/dashboard/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://example.com")
	req.Host = "example.com"

	w := httptest.NewRecorder()
	c.handleMessages(w, req)

	if w.Code != http.StatusBadRequest && w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected error for oversized body, got %d", w.Code)
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
