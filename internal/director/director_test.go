package director

import (
	"strings"
	"testing"

	"forgegrid/internal/manifest"
	"forgegrid/internal/models"
	"forgegrid/internal/store"
)

func TestDirectorDispatch(t *testing.T) {
	ws := t.TempDir()
	s, err := store.NewStore(ws)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	w := &models.WorkerState{
		ID:     "worker-123",
		OS:     "linux",
		Status: "online",
	}
	s.Mu.Lock()
	s.Workers[w.ID] = w
	s.Mu.Unlock()

	dir := New(s)

	yamlData := `
project: "ForgeGrid"
tasks:
  build:
    requirements:
      os: "linux"
    execution:
      profile: "GoTest"
      parameters:
        package: "./..."
`
	m, err := manifest.Parse(strings.NewReader(yamlData))
	if err != nil {
		t.Fatalf("Failed to parse manifest: %v", err)
	}

	err = dir.SubmitManifest(m)
	if err != nil {
		t.Fatalf("Expected nil, got %v", err)
	}

	s.Mu.RLock()
	defer s.Mu.RUnlock()
	if len(s.Jobs) != 1 {
		t.Fatalf("Expected 1 job, got %d", len(s.Jobs))
	}

	for _, j := range s.Jobs {
		if j.WorkerID != "worker-123" {
			t.Errorf("Expected job assigned to worker-123, got %s", j.WorkerID)
		}
		if j.Profile != "GoTest" {
			t.Errorf("Expected profile 'GoTest', got %s", j.Profile)
		}
	}
}

func TestDirectorDispatchNoWorker(t *testing.T) {
	ws := t.TempDir()
	s, err := store.NewStore(ws)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// No workers online
	dir := New(s)

	yamlData := `
project: "ForgeGrid"
tasks:
  build:
    requirements:
      os: "linux"
    execution:
      profile: "GoTest"
`
	m, err := manifest.Parse(strings.NewReader(yamlData))
	if err != nil {
		t.Fatalf("Failed to parse manifest: %v", err)
	}

	err = dir.SubmitManifest(m)
	if err == nil {
		t.Fatalf("Expected error for no worker, got nil")
	}
}
