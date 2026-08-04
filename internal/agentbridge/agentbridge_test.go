package agentbridge

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	
	"forgegrid/internal/network"
)

func setupTestServer(t *testing.T) (*Server, *Store, string) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	s, err := NewStore()
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	server := NewServer(s)
	return server, s, tmpDir
}

func doReq(t *testing.T, handler http.HandlerFunc, method, path, auth string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func TestAuthAndRejection(t *testing.T) {
	server, store, _ := setupTestServer(t)
	store.agents["fedora"] = AgentRegistration{Name: "fedora", TokenHash: "00b6241dddb1bc0bc657026c85cb001a0eeaee5715a4d4c9f031b8afc4ba7e1f"}

	w := doReq(t, server.handleMessages, "POST", "/api/v1/agent-messages", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", w.Code)
	}
	w = doReq(t, server.handleMessages, "POST", "/api/v1/agent-messages", "fedora:wrong", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 with wrong token, got %d", w.Code)
	}
}

func TestOversizedAndMalformed(t *testing.T) {
	server, store, _ := setupTestServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	store.agents["fedora"] = AgentRegistration{Name: "fedora", TokenHash: "00b6241dddb1bc0bc657026c85cb001a0eeaee5715a4d4c9f031b8afc4ba7e1f"}
	auth := "fedora:fedora-secret"

	// Malformed JSON (400)
	req := httptest.NewRequest("POST", "/api/v1/agent-messages", strings.NewReader("{bad json}"))
	req.Header.Set("Authorization", "Bearer "+auth)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for malformed json, got %d", w.Code)
	}

	// Unknown fields (400)
	req = httptest.NewRequest("POST", "/api/v1/agent-messages", strings.NewReader(`{"recipient":"fedora", "task_id":"t1", "type":"instruction", "body":"x", "unknown":"y"}`))
	req.Header.Set("Authorization", "Bearer "+auth)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for unknown field, got %d", w.Code)
	}

	// Invalid recipient (404)
	req = httptest.NewRequest("POST", "/api/v1/agent-messages", strings.NewReader(`{"recipient":"nobody", "task_id":"t1", "type":"instruction", "body":"x"}`))
	req.Header.Set("Authorization", "Bearer "+auth)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for missing recipient, got %d", w.Code)
	}

	// Oversized body (400 or 413 depending on MaxBytesReader behavior, standard http returns 400 via Decode err)
	bigBody := make([]byte, 2*1024*1024)
	req = httptest.NewRequest("POST", "/api/v1/agent-messages", bytes.NewReader(bigBody))
	req.Header.Set("Authorization", "Bearer "+auth)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected oversized body rejection, got %d", w.Code)
	}

	// Overlong body string (> 256KB constraint)
	longStr := strings.Repeat("a", 300*1024)
	req = httptest.NewRequest("POST", "/api/v1/agent-messages", strings.NewReader(`{"recipient":"fedora", "task_id":"t1", "type":"instruction", "body":"`+longStr+`"}`))
	req.Header.Set("Authorization", "Bearer "+auth)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for >256KB body field, got %d", w.Code)
	}
}

func TestMessageLifecycle(t *testing.T) {
	server, store, tmpDir := setupTestServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	store.agents["fedora"] = AgentRegistration{Name: "fedora", TokenHash: "00b6241dddb1bc0bc657026c85cb001a0eeaee5715a4d4c9f031b8afc4ba7e1f"}
	store.agents["windows"] = AgentRegistration{Name: "windows", TokenHash: "f7e4dc3579fa7e347f56f3d0aa9bbaa565f95cb5c8052607c947534d9f88c825"}

	reqBody := `{"recipient":"windows", "task_id":"t1", "type":"instruction", "body":"hello", "ttl_seconds": 60, "idempotency_key": "k1"}`

	// Send
	req := httptest.NewRequest("POST", "/api/v1/agent-messages", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer fedora:fedora-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d", w.Code)
	}
	var msg AgentMessage
	json.Unmarshal(w.Body.Bytes(), &msg)

	// Duplicate send (Idempotent)
	req2 := httptest.NewRequest("POST", "/api/v1/agent-messages", strings.NewReader(reqBody))
	req2.Header.Set("Authorization", "Bearer fedora:fedora-secret")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	var msg2 AgentMessage
	json.Unmarshal(w2.Body.Bytes(), &msg2)
	if msg.ID != msg2.ID {
		t.Errorf("Idempotent send failed: expected %s, got %s", msg.ID, msg2.ID)
	}

	// Read inbox
	req = httptest.NewRequest("GET", "/api/v1/agent-messages/inbox", nil)
	req.Header.Set("Authorization", "Bearer windows:windows-secret")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var inbox []AgentMessage
	json.Unmarshal(w.Body.Bytes(), &inbox)
	if len(inbox) != 1 {
		t.Fatalf("Windows should see 1 message, got %d", len(inbox))
	}

	// Ack
	req = httptest.NewRequest("POST", "/api/v1/agent-messages/"+msg.ID+"/acknowledge", nil)
	req.Header.Set("Authorization", "Bearer windows:windows-secret")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Ack failed: %d", w.Code)
	}

	// Complete
	resBody := `{"result":{"status":"ok"}}`
	req = httptest.NewRequest("POST", "/api/v1/agent-messages/"+msg.ID+"/complete", strings.NewReader(resBody))
	req.Header.Set("Authorization", "Bearer windows:windows-secret")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Complete failed: %d", w.Code)
	}

	// Disappear from inbox
	req = httptest.NewRequest("GET", "/api/v1/agent-messages/inbox", nil)
	req.Header.Set("Authorization", "Bearer windows:windows-secret")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	json.Unmarshal(w.Body.Bytes(), &inbox)
	if len(inbox) != 0 {
		t.Fatalf("Windows inbox should be empty, got %d", len(inbox))
	}

	// Restart persistence
	store2, _ := NewStore()
	store2.dataDir = filepath.Join(tmpDir, ".local", "share", "forgegrid", "agentbridge")
	store2.load()
	if m, ok := store2.GetMessage(msg.ID); !ok || m.Status != StatusCompleted {
		t.Errorf("Persistence failed")
	}

	// Token not exposed
	if bytes.Contains(w.Body.Bytes(), []byte("secret")) {
		t.Errorf("Token exposed in response!")
	}
}

func TestIntegrationConcurrent(t *testing.T) {
	// Concurrent HTTP Integration test with TLS
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	s, _ := NewStore()
	s.dataDir = filepath.Join(tmpDir, ".local", "share", "forgegrid", "agentbridge")
	os.MkdirAll(s.dataDir, 0700)

	certPEM, keyPEM, fp, _ := network.GenerateSelfSignedCert()
	cert, _ := tls.X509KeyPair(certPEM, keyPEM)

	server := NewServer(s)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	srv := httptest.NewUnstartedServer(mux)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	defer srv.Close()

	agents := 7
	for i := 0; i < agents; i++ {
		// Mock passwords: agent-i-secret
		s.RegisterAgent(fmt.Sprintf("agent-%d", i), "") // Just register to get ID
	}

	// Force plain auth keys for test without mocking hash logic manually
	s.mu.Lock()
	for i := 0; i < agents; i++ {
		a := s.agents[fmt.Sprintf("agent-%d", i)]
		// token is "secret"
		a.TokenHash = "2bb80d537b1da3e38bd30361aa855686bde0eacd7162fef6a25fe97bf527a25b" // sha256("secret")
		s.agents[a.Name] = a
	}
	s.mu.Unlock()
	s.save()

	var wg sync.WaitGroup
	client, _ := NewClient(srv.URL, "agent-0", "secret", fp, false)
	client.HTTPClient.Transport.(*http.Transport).TLSClientConfig = network.PinTLSConfig(fp)

	// Send 700 messages concurrently
	for i := 0; i < 700; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			recip := fmt.Sprintf("agent-%d", idx%agents)
			c, _ := NewClient(srv.URL, "agent-0", "secret", fp, false)
			c.SendMessage(recip, "t1", TypeInstruction, "hello", 3600, fmt.Sprintf("key-%d", idx))
		}(i)
	}

	wg.Wait()

	s.mu.Lock()
	count := len(s.messages)
	s.mu.Unlock()
	if count != 700 {
		t.Fatalf("Expected 700 messages, got %d", count)
	}

	// Simulate Restart (create new Store, point to same files)
	s2, _ := NewStore()
	s2.dataDir = s.dataDir
	s2.load()

	if len(s2.messages) != 700 {
		t.Fatalf("Expected 700 messages after restart, got %d", len(s2.messages))
	}
}
