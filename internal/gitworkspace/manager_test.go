package gitworkspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareWorkspaceRequiresAllowlistedRepo(t *testing.T) {
	manager := NewManager(t.TempDir(), Options{
		AllowedRepos: map[string]bool{
			"https://github.com/example/allowed.git": true,
		},
	})

	_, err := manager.PrepareWorkspace("https://github.com/example/blocked.git", "abc123", "branch", "task")
	if err == nil {
		t.Fatalf("expected non-allowlisted repository to be rejected")
	}
}

func TestPrepareWorkspaceRequiresBaseCommit(t *testing.T) {
	repo := "https://github.com/example/allowed.git"
	manager := NewManager(t.TempDir(), Options{
		AllowedRepos: map[string]bool{repo: true},
	})

	_, err := manager.PrepareWorkspace(repo, "", "branch", "task")
	if err == nil {
		t.Fatalf("expected empty base commit to be rejected")
	}
}

func TestCommitAndMaybePushRequiresPushPolicy(t *testing.T) {
	repo := "https://github.com/example/allowed.git"
	manager := NewManager(t.TempDir(), Options{
		AllowedRepos: map[string]bool{repo: true},
		AllowPush:    false,
	})

	_, err := manager.CommitAndMaybePush(t.TempDir(), repo, "test commit", true)
	if err == nil {
		t.Fatalf("expected push to be rejected when worker policy denies it")
	}
	if !strings.Contains(err.Error(), "push denied by worker policy") {
		t.Fatalf("expected push policy error, got %v", err)
	}
}

func TestCollectArtifactsSupportsRecursiveBuildPattern(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "build", "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build", "game.exe"), []byte("exe"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build", "nested", "data.pck"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	artifacts, err := NewManager(root).CollectArtifacts(root, []string{"build/**"})
	if err != nil {
		t.Fatalf("CollectArtifacts failed: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d: %#v", len(artifacts), artifacts)
	}
	if artifacts[0].SHA256 == "" || artifacts[1].SHA256 == "" {
		t.Fatalf("expected artifact checksums")
	}
}

func TestCollectArtifactsPackagesLargeCompressibleFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "build"), 0755); err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("0", 11*1024*1024)
	if err := os.WriteFile(filepath.Join(root, "build", "game.pck"), []byte(large), 0644); err != nil {
		t.Fatal(err)
	}

	artifacts, err := NewManager(root).CollectArtifacts(root, []string{"build/game.pck"})
	if err != nil {
		t.Fatalf("CollectArtifacts failed: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
	if !artifacts[0].Packaged {
		t.Fatalf("expected large compressible artifact to be packaged")
	}
	if artifacts[0].PackageName != "game.pck.zip" {
		t.Fatalf("unexpected package name %q", artifacts[0].PackageName)
	}
	if artifacts[0].ContentBase64 == "" {
		t.Fatalf("expected packaged artifact content")
	}
}

func TestValidateBranchNameRejectsUnsafeNames(t *testing.T) {
	bad := []string{"", "main", "master", "../escape", "forgegrid\\bad", "feature..bad", "/leading", "trailing/"}
	for _, name := range bad {
		if err := ValidateBranchName(name); err == nil {
			t.Fatalf("expected %q to be rejected", name)
		}
	}
	if err := ValidateBranchName("forgegrid/codex/add-a-test-20260819"); err != nil {
		t.Fatalf("expected safe branch name, got %v", err)
	}
}

func TestParseChangedFilesAndSecretGuard(t *testing.T) {
	raw := " M README.md\x00A  .env.local\x00R  old.txt\x00new.txt\x00"
	files := ParseChangedFiles(raw)
	if len(files) != 3 {
		t.Fatalf("expected 3 changed files, got %#v", files)
	}
	blocked := SecretLikeChangedFiles(files)
	if len(blocked) != 1 || blocked[0] != ".env.local" {
		t.Fatalf("expected .env.local to be blocked, got %#v", blocked)
	}
}

func TestPrepareJobWorkspaceUsesIsolatedJobDirectory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	base := t.TempDir()
	origin := filepath.Join(base, "origin")
	if err := os.MkdirAll(origin, 0755); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, origin, "init")
	runGitTest(t, origin, "config", "user.email", "forgegrid@example.test")
	runGitTest(t, origin, "config", "user.name", "ForgeGrid Test")
	if err := os.WriteFile(filepath.Join(origin, "README.md"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, origin, "add", "README.md")
	runGitTest(t, origin, "commit", "-m", "initial")
	sha := strings.TrimSpace(runGitOutputTest(t, origin, "rev-parse", "HEAD"))

	manager := NewManager(base, Options{AllowedRepos: map[string]bool{origin: true}})
	ws, err := manager.PrepareJobWorkspace(origin, sha, "forgegrid/codex/test-branch", "job/../../../abc123")
	if err != nil {
		t.Fatalf("PrepareJobWorkspace failed: %v", err)
	}
	if !strings.Contains(filepath.ToSlash(ws.WorkDir), "/workspaces/") {
		t.Fatalf("expected isolated workspace path, got %s", ws.WorkDir)
	}
	rel, err := filepath.Rel(base, ws.WorkDir)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("workspace escaped base: rel=%s err=%v", rel, err)
	}
	if ws.BaseCommit != sha {
		t.Fatalf("expected resolved base %s, got %s", sha, ws.BaseCommit)
	}
}

func runGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func runGitOutputTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return string(out)
}
