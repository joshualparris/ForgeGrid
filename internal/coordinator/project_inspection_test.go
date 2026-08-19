package coordinator

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forgegrid/internal/models"
	"forgegrid/internal/store"
)

func TestSafeForgeGridBranch(t *testing.T) {
	now := time.Date(2026, 8, 19, 13, 9, 8, 0, time.UTC)
	branch := safeForgeGridBranch("codex", "Add a new forest area; rm -rf /", now)
	if branch != "forgegrid/codex/add-a-new-forest-area-rm-rf-20260819-130908" {
		t.Fatalf("branch = %q", branch)
	}
	if strings.ContainsAny(branch, " ;") {
		t.Fatalf("branch contains unsafe separators: %q", branch)
	}
}

func TestBuildInspectionDetectsGoPythonNodeGodot(t *testing.T) {
	inspection := buildInspection("josh/project", "main", "abc123", []string{
		"go.mod",
		"pyproject.toml",
		"tests/test_game.py",
		"package.json",
		"package-lock.json",
		"project.godot",
	}, map[string]string{"test": "vitest", "build": "vite build"}, false)

	for _, want := range []string{"Go", "Python", "Node", "Godot"} {
		if !hasString(inspection.ProjectTypes, want) {
			t.Fatalf("expected project type %s in %#v", want, inspection.ProjectTypes)
		}
	}
	actionIDs := make([]string, 0, len(inspection.AvailableActions))
	for _, action := range inspection.AvailableActions {
		actionIDs = append(actionIDs, action.ID)
	}
	for _, want := range []string{"codex", "go-test", "go-race", "python-test", "node-test", "node-build", "godot-export-windows"} {
		if !hasString(actionIDs, want) {
			t.Fatalf("expected action %s in %#v", want, actionIDs)
		}
	}
}

func TestBuildInspectionDoesNotInventPythonTests(t *testing.T) {
	inspection := buildInspection("josh/story", "main", "abc123", []string{
		"game.py",
		"README.md",
	}, nil, false)
	actionIDs := make([]string, 0, len(inspection.AvailableActions))
	for _, action := range inspection.AvailableActions {
		actionIDs = append(actionIDs, action.ID)
	}
	if hasString(actionIDs, "python-test") {
		t.Fatalf("unexpected python-test action in %#v", actionIDs)
	}
	if !hasString(inspection.Warnings, "No automated Python test suite detected") {
		t.Fatalf("expected no-test warning, got %#v", inspection.Warnings)
	}
}

func TestInspectProjectReturnsCachedInspectionWithoutGitHubToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	s, err := store.NewStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	cachedAt := time.Now()
	s.ProjectLibrary.Projects["josh/repo"] = &models.Project{
		ID:        "josh/repo",
		FullName:  "josh/repo",
		UpdatedAt: cachedAt.Add(-time.Hour),
		Inspection: &models.ProjectInspection{
			ProjectID:           "josh/repo",
			ProjectTypes:        []string{"Go"},
			InspectionTimestamp: cachedAt,
		},
	}
	c := &Coordinator{Store: s}

	inspection, err := c.inspectProject(nil, "josh/repo", false)
	if err != nil {
		t.Fatalf("expected cached inspection, got %v", err)
	}
	if !hasString(inspection.ProjectTypes, "Go") {
		t.Fatalf("expected cached Go inspection, got %#v", inspection.ProjectTypes)
	}
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
