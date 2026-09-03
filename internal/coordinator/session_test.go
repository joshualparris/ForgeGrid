package coordinator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"forgegrid/internal/store"
)

func TestSessionStartPreservesNonDefaultLocalhostPort(t *testing.T) {
	s, err := store.NewStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	c := &Coordinator{
		Store:       s,
		IP:          "10.1.2.3",
		Insecure:    true,
		Fingerprint: "controller-fp",
	}

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18080/api/session/start?agent_port=9191", nil)
	w := httptest.NewRecorder()

	c.handleSessionStart(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got, want := body["controller_url"], "http://10.1.2.3:18080"; got != want {
		t.Fatalf("controller_url = %q, want %q", got, want)
	}
	if strings.Contains(body["windows_bootstrap"], "10.1.2.3:8080") {
		t.Fatalf("bootstrap command used default port: %s", body["windows_bootstrap"])
	}
	if !strings.Contains(body["windows_bootstrap"], "http://10.1.2.3:18080") {
		t.Fatalf("bootstrap command missing actual controller URL: %s", body["windows_bootstrap"])
	}
}
