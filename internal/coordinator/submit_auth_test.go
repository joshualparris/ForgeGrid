package coordinator

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"forgegrid/internal/models"
	"forgegrid/internal/store"
)

func setupTestCoordinatorWithWorker(t *testing.T) (*Coordinator, *models.WorkerState) {
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := New(st, true)
	c.AdminToken = "test-admin-token"
	
	workerID := "worker-123"
	token := "secret-token"
	st.Workers[workerID] = &models.WorkerState{
		ID:        workerID,
		TokenHash: hashToken(token),
		Status:    "online",
		LastSeen:  time.Now(),
	}
	return c, st.Workers[workerID]
}

func TestSubmitAuth(t *testing.T) {
	c, _ := setupTestCoordinatorWithWorker(t)

	var handlerCalled bool
	handler := c.submitAuth(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	tests := []struct {
		name           string
		setupReq       func(*http.Request)
		expectedStatus int
		expectCalled   bool
	}{
		{
			name: "1. authorised trusted client can submit a job",
			setupReq: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer secret-token")
			},
			expectedStatus: http.StatusOK,
			expectCalled:   true,
		},
		{
			name: "2. missing credentials are rejected",
			setupReq: func(r *http.Request) {
			},
			expectedStatus: http.StatusUnauthorized,
			expectCalled:   false,
		},
		{
			name: "3. invalid credentials are rejected",
			setupReq: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer invalid-token")
			},
			expectedStatus: http.StatusUnauthorized,
			expectCalled:   false,
		},
		{
			name: "5. dashboard/admin authentication continues working",
			setupReq: func(r *http.Request) {
				r.SetBasicAuth("admin", "test-admin-token")
			},
			expectedStatus: http.StatusOK,
			expectCalled:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerCalled = false
			req, _ := http.NewRequest("POST", server.URL, bytes.NewBufferString("{}"))
			tt.setupReq(req)

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}
			if handlerCalled != tt.expectCalled {
				t.Errorf("expected handler called to be %v, got %v", tt.expectCalled, handlerCalled)
			}
		})
	}
}
