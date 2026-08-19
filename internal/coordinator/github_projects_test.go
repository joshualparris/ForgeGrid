package coordinator

import (
	"os"
	"path/filepath"
	"testing"

	"forgegrid/internal/store"
)

func TestNextLink(t *testing.T) {
	link := `<https://api.github.com/user/repos?page=2>; rel="next", <https://api.github.com/user/repos?page=4>; rel="last"`
	if got, want := nextLink(link), "https://api.github.com/user/repos?page=2"; got != want {
		t.Fatalf("nextLink = %q, want %q", got, want)
	}
}

func TestNextLinkMissing(t *testing.T) {
	if got := nextLink(`<https://api.github.com/user/repos?page=4>; rel="last"`); got != "" {
		t.Fatalf("nextLink = %q, want empty", got)
	}
}

func TestGitHubTokenFileRequiresOwnerOnlyPermissions(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	s, err := store.NewStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	path := filepath.Join(s.Dir(), "github-token.txt")
	if err := os.WriteFile(path, []byte("loose"), 0644); err != nil {
		t.Fatalf("write loose token: %v", err)
	}
	c := &Coordinator{Store: s}
	if got := c.githubToken(); got != "" {
		t.Fatalf("expected loose token file to be ignored, got %q", got)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatalf("chmod token: %v", err)
	}
	if got := c.githubToken(); got != "loose" {
		t.Fatalf("expected secure token file to be loaded, got %q", got)
	}
}
