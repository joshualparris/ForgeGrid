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
	"time"

	"forgegrid/internal/network"
)

func setupTestServer(t *testing.T) (*Server, *Store, string) {
	tmpDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", filepath.Join(tmpDir, "localappdata"))
	t.Setenv("USERPROFILE", filepath.Join(tmpDir, "userprofile"))
	t.Setenv("HOME", tmpDir)
	
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
	t.Setenv("LOCALAPPDATA", filepath.Join(t.TempDir(), "localappdata"))
	t.Setenv("USERPROFILE", filepath.Join(t.TempDir(), "userprofile"))
	t.Setenv("HOME", filepath.Join(t.TempDir(), "home"))
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

	req := httptest.NewRequest("POST", "/api/v1/agent-messages", strings.NewReader("{bad json}"))
	req.Header.Set("Authorization", "Bearer "+auth)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for malformed json, got %d", w.Code)
	}

	req = httptest.NewRequest("POST", "/api/v1/agent-messages", strings.NewReader(`{"recipient":"fedora", "task_id":"t1", "type":"instruction", "body":"x", "unknown":"y"}`))
	req.Header.Set("Authorization", "Bearer "+auth)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for unknown field, got %d", w.Code)
	}

	req = httptest.NewRequest("POST", "/api/v1/agent-messages", strings.NewReader(`{"recipient":"nobody", "task_id":"t1", "type":"instruction", "body":"x"}`))
	req.Header.Set("Authorization", "Bearer "+auth)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for missing recipient, got %d", w.Code)
	}

	bigBody := make([]byte, 2*1024*1024)
	req = httptest.NewRequest("POST", "/api/v1/agent-messages", bytes.NewReader(bigBody))
	req.Header.Set("Authorization", "Bearer "+auth)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected oversized body rejection, got %d", w.Code)
	}
	
	longStr := strings.Repeat("a", 300*1024)
	req = httptest.NewRequest("POST", "/api/v1/agent-messages", strings.NewReader(`{"recipient":"fedora", "task_id":"t1", "type":"instruction", "body":"`+longStr+`"}`))
	req.Header.Set("Authorization", "Bearer "+auth)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for >256KB body field, got %d", w.Code)
	}

	// Multiple JSON objects
	req = httptest.NewRequest("POST", "/api/v1/agent-messages", strings.NewReader(`{"recipient":"fedora", "task_id":"t1", "type":"instruction", "body":"x"}{"recipient":"fedora"}`))
	req.Header.Set("Authorization", "Bearer "+auth)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for multiple JSON objects, got %d", w.Code)
	}
}

func TestMessageLifecycle(t *testing.T) {
	server, store, tmpDir := setupTestServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	store.agents["fedora"] = AgentRegistration{Name: "fedora", TokenHash: "00b6241dddb1bc0bc657026c85cb001a0eeaee5715a4d4c9f031b8afc4ba7e1f"}
	store.agents["windows"] = AgentRegistration{Name: "windows", TokenHash: "f7e4dc3579fa7e347f56f3d0aa9bbaa565f95cb5c8052607c947534d9f88c825"}

	reqBody := `{"recipient":"windows", "task_id":"t1", "type":"instruction", "body":"hello", "ttl_seconds": 60, "idempotency_key": "k1"}`
	
	req := httptest.NewRequest("POST", "/api/v1/agent-messages", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer fedora:fedora-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d", w.Code)
	}
	var msg AgentMessage
	json.Unmarshal(w.Body.Bytes(), &msg)

	req2 := httptest.NewRequest("POST", "/api/v1/agent-messages", strings.NewReader(reqBody))
	req2.Header.Set("Authorization", "Bearer fedora:fedora-secret")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	var msg2 AgentMessage
	json.Unmarshal(w2.Body.Bytes(), &msg2)
	if msg.ID != msg2.ID {
		t.Errorf("Idempotent send failed: expected %s, got %s", msg.ID, msg2.ID)
	}

	req = httptest.NewRequest("GET", "/api/v1/agent-messages/inbox", nil)
	req.Header.Set("Authorization", "Bearer windows:windows-secret")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var inbox []AgentMessage
	json.Unmarshal(w.Body.Bytes(), &inbox)
	if len(inbox) != 1 {
		t.Fatalf("Windows should see 1 message, got %d. Code: %d, Body: %s", len(inbox), w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", "/api/v1/agent-messages/"+msg.ID+"/acknowledge", nil)
	req.Header.Set("Authorization", "Bearer windows:windows-secret")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Ack failed: %d", w.Code)
	}

	resBody := `{"result":{"status":"ok"}}`
	req = httptest.NewRequest("POST", "/api/v1/agent-messages/"+msg.ID+"/complete", strings.NewReader(resBody))
	req.Header.Set("Authorization", "Bearer windows:windows-secret")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Complete failed: %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/api/v1/agent-messages/inbox", nil)
	req.Header.Set("Authorization", "Bearer windows:windows-secret")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	json.Unmarshal(w.Body.Bytes(), &inbox)
	if len(inbox) != 0 {
		t.Fatalf("Windows inbox should be empty, got %d", len(inbox))
	}

	store2, _ := NewStore()
	store2.dataDir = filepath.Join(tmpDir, ".local", "share", "forgegrid", "agentbridge")
	store2.load()
	if m, ok := store2.GetMessage(msg.ID); !ok || m.Status != StatusCompleted {
		t.Errorf("Persistence failed")
	}

	if bytes.Contains(w.Body.Bytes(), []byte("secret")) {
		t.Errorf("Token exposed in response!")
	}
}

func TestIntegrationConcurrent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", filepath.Join(tmpDir, "localappdata"))
	t.Setenv("USERPROFILE", filepath.Join(tmpDir, "userprofile"))
	t.Setenv("HOME", tmpDir)
	
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

	agents := 7
	s.mu.Lock()
	for i := 0; i < agents; i++ {
		a := AgentRegistration{
			Name:      fmt.Sprintf("agent-%d", i),
			TokenHash: "2bb80d537b1da3e38bd30361aa855686bde0eacd7162fef6a25fe97bf527a25b", // sha256("secret")
			CreatedAt: time.Now(),
		}
		s.agents[a.Name] = a
	}
	s.mu.Unlock()
	s.save()

	var wg sync.WaitGroup
	sem := make(chan struct{}, 50) // Limit concurrency to avoid ephemeral port / backlog exhaustion on Windows
	// 700 messages sent concurrently (100 per agent sending to others)
	for i := 0; i < 700; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			
			senderId := idx % agents
			recipId := (idx + 1) % agents
			c, err := NewClient(srv.URL, fmt.Sprintf("agent-%d", senderId), "secret", fp, false)
			if err == nil {
				c.HTTPClient.Timeout = 60 * time.Second
			}
			if err != nil {
				t.Errorf("Failed client creation: %v", err)
				return
			}
			_, err = c.SendMessage(fmt.Sprintf("agent-%d", recipId), "t1", TypeInstruction, "hello", 3600, fmt.Sprintf("key-%d", idx))
			if err != nil {
				t.Errorf("Failed to send message: %v", err)
			}
		}(i)
	}
	
	wg.Wait()
	
	s.mu.Lock()
	count := len(s.messages)
	s.mu.Unlock()
	if count != 700 {
		t.Fatalf("Expected 700 messages, got %d", count)
	}

	// Read inboxes and process exactly once
	for i := 0; i < agents; i++ {
		wg.Add(1)
		go func(agentId int) {
			defer wg.Done()
			c, _ := NewClient(srv.URL, fmt.Sprintf("agent-%d", agentId), "secret", fp, false)
			c.HTTPClient.Timeout = 60 * time.Second
			inbox, err := c.GetInbox()
			if err != nil {
				t.Errorf("Failed to read inbox for agent %d: %v", agentId, err)
				return
			}
			for _, m := range inbox {
				_, err := c.Acknowledge(m.ID)
				if err != nil {
					t.Errorf("Failed ack: %v", err)
				}
				_, err = c.Complete(m.ID, []byte(`{"status":"ok"}`))
				if err != nil {
					t.Errorf("Failed complete: %v", err)
				}
			}
		}(i)
	}
	wg.Wait()

	// Verify all 700 are completed
	s.mu.Lock()
	for _, m := range s.messages {
		if m.Status != StatusCompleted {
			t.Errorf("Message %s not completed, status %s", m.ID, m.Status)
		}
	}
	s.mu.Unlock()

	// Stop the server
	srv.Close()

	// Create new server from same data
	s2, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	s2.dataDir = s.dataDir
	if err := s2.load(); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	server2 := NewServer(s2)
	mux2 := http.NewServeMux()
	server2.RegisterRoutes(mux2)
	
	srv2 := httptest.NewUnstartedServer(mux2)
	srv2.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv2.StartTLS()
	defer srv2.Close()

	// Verify agents still authenticate
	for i := 0; i < agents; i++ {
		c, err := NewClient(srv2.URL, fmt.Sprintf("agent-%d", i), "secret", fp, false)
		if err != nil {
			t.Fatalf("Failed client creation after restart: %v", err)
		}
		
		if i == 0 {
			// send at least one fresh message after restart
			msg, err := c.SendMessage("agent-1", "t2", TypeInstruction, "hello again", 3600, "new-key")
			if err != nil {
				t.Fatalf("SendMessage failed after restart: %v", err)
			}
			
			c2, _ := NewClient(srv2.URL, "agent-1", "secret", fp, false)
			inbox, err := c2.GetInbox()
			if err != nil {
				t.Fatalf("GetInbox failed after restart: %v", err)
			}
			if len(inbox) != 1 || inbox[0].ID != msg.ID {
				t.Fatalf("Did not find fresh message in inbox")
			}
			
			if _, err := c2.Acknowledge(msg.ID); err != nil {
				t.Fatalf("Acknowledge failed after restart: %v", err)
			}
			
			if _, err := c2.Complete(msg.ID, []byte(`{"status":"ok"}`)); err != nil {
				t.Fatalf("Complete failed after restart: %v", err)
			}
		}
	}

	// Verify persistence again
	s3, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore failed for s3: %v", err)
	}
	s3.dataDir = s.dataDir
	if err := s3.load(); err != nil {
		t.Fatalf("s3.load failed: %v", err)
	}
	if len(s3.messages) != 701 {
		t.Fatalf("Expected 701 messages in s3, got %d", len(s3.messages))
	}
}
