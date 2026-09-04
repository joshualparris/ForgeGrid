package agentbridge

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebUIHandlerServesEmbeddedAssets(t *testing.T) {
	h, err := WebUIHandler()
	if err != nil {
		t.Fatalf("WebUIHandler() error: %v", err)
	}

	for _, tc := range []struct {
		path       string
		wantStatus int
		wantInBody string
	}{
		{"/", http.StatusOK, "<title>AgentBridge</title>"},
		// http.FileServer 301-redirects /index.html -> / by design; that's
		// standard net/http behavior, not something this test needs to
		// re-verify beyond not erroring.
		{"/index.html", http.StatusMovedPermanently, ""},
		{"/app.js", http.StatusOK, "agent-status"},
		{"/styles.css", http.StatusOK, "--accent"},
		{"/does-not-exist.txt", http.StatusNotFound, ""},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.wantStatus {
			t.Fatalf("%s: status = %d, want %d", tc.path, rec.Code, tc.wantStatus)
		}
		if tc.wantInBody != "" {
			body, _ := io.ReadAll(rec.Body)
			if !strings.Contains(string(body), tc.wantInBody) {
				t.Fatalf("%s: body missing %q, got %d bytes", tc.path, tc.wantInBody, len(body))
			}
		}
	}
}

func TestWebUIDoesNotShadowAPIRoutes(t *testing.T) {
	// Regression guard: mounting the static file server at "/" must never
	// take priority over the more specific /api/v1/... routes registered by
	// Server.RegisterRoutes on the same mux (Go's ServeMux picks the most
	// specific pattern regardless of registration order, but this proves it
	// end-to-end against the real mux wiring rather than trusting that).
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	store.dataDir = t.TempDir()
	server := NewServer(store)

	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	uiHandler, err := WebUIHandler()
	if err != nil {
		t.Fatalf("WebUIHandler() error: %v", err)
	}
	mux.Handle("/", uiHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	// Unauthenticated, so this must be the API's 401, not the GUI's 200/404 -
	// proving the request reached handleStatus, not the file server.
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected /api/v1/agent-status to reach the API handler (401 unauthenticated), got %d", rec.Code)
	}
}
