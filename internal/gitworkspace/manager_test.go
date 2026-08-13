package gitworkspace

import (
	"os"
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
