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
		ID:       "worker-123",
		NodeName: "build-laptop",
		OS:       "linux",
		Status:   "online",
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
		if j.WorkerName != "build-laptop" {
			t.Errorf("Expected job worker name build-laptop, got %s", j.WorkerName)
		}
		if j.Profile != "GoTest" {
			t.Errorf("Expected profile 'GoTest', got %s", j.Profile)
		}
		if j.ProjectName != "ForgeGrid" || j.TaskName != "build" {
			t.Errorf("Expected project/task metadata, got %q/%q", j.ProjectName, j.TaskName)
		}
		if j.CreatedAt.IsZero() {
			t.Error("Expected CreatedAt to be set")
		}
	}
}

func TestDirectorDoesNotAssignNewWorkToBusyWorker(t *testing.T) {
	s, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	s.Mu.Lock()
	s.Workers["busy"] = &models.WorkerState{
		ID:                "busy",
		NodeName:          "fast-but-busy",
		OS:                "linux",
		Status:            "online",
		AvailableRAM:      32 * 1024 * 1024 * 1024,
		LogicalProcessors: 16,
	}
	s.Workers["idle"] = &models.WorkerState{
		ID:                "idle",
		NodeName:          "steady-idle",
		OS:                "linux",
		Status:            "online",
		AvailableRAM:      8 * 1024 * 1024 * 1024,
		LogicalProcessors: 4,
	}
	s.Jobs["existing"] = &models.Job{
		ID:       "existing",
		WorkerID: "busy",
		Status:   models.StatusRunning,
	}
	s.Mu.Unlock()

	m, err := manifest.Parse(strings.NewReader(`
project: "ForgeGrid"
tasks:
  build:
    requirements:
      os: "linux"
    execution:
      profile: "GoTest"
`))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if err := New(s).SubmitManifest(m); err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}

	for id, j := range s.Jobs {
		if id == "existing" {
			continue
		}
		if j.WorkerID != "idle" {
			t.Fatalf("expected new job on idle worker, got %s", j.WorkerID)
		}
		if j.WorkerName != "steady-idle" {
			t.Fatalf("expected worker name steady-idle, got %s", j.WorkerName)
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

func TestDirectorDispatchRequiresLabelsAndCapabilities(t *testing.T) {
	s, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	s.Mu.Lock()
	s.Workers["basic"] = &models.WorkerState{
		ID:                "basic",
		OS:                "linux",
		Status:            "online",
		AvailableRAM:      16 * 1024 * 1024 * 1024,
		LogicalProcessors: 8,
	}
	s.Workers["godot"] = &models.WorkerState{
		ID:                "godot",
		OS:                "linux",
		Status:            "online",
		AvailableRAM:      8 * 1024 * 1024 * 1024,
		LogicalProcessors: 4,
		Labels:            []string{"windows-build"},
		Capabilities:      []string{"godot"},
	}
	s.Mu.Unlock()

	m, err := manifest.Parse(strings.NewReader(`
project: "Game"
tasks:
  export:
    requirements:
      os: "linux"
      labels: ["windows-build"]
      capabilities: ["godot"]
    execution:
      profile: "GodotExport"
`))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if err := New(s).SubmitManifest(m); err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}

	for _, j := range s.Jobs {
		if j.WorkerID != "godot" {
			t.Fatalf("expected godot worker, got %s", j.WorkerID)
		}
	}
}

func TestDirectorAutoAIAgentChoosesIdleAntigravityPythonWorker(t *testing.T) {
	s, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	s.Mu.Lock()
	s.Workers["thinkpad"] = &models.WorkerState{
		ID:                "thinkpad",
		NodeName:          "ThinkPad-Lenovo",
		OS:                "windows",
		Status:            "online",
		AvailableRAM:      8 * 1024 * 1024 * 1024,
		LogicalProcessors: 4,
		Capabilities:      []string{"git", "python", "antigravity", "ai-agent"},
	}
	s.Workers["probook"] = &models.WorkerState{
		ID:                "probook",
		NodeName:          "probook",
		OS:                "windows",
		Status:            "online",
		AvailableRAM:      16 * 1024 * 1024 * 1024,
		LogicalProcessors: 12,
		Capabilities:      []string{"git", "go", "node", "codex", "ai-agent"},
	}
	s.Jobs["busy"] = &models.Job{ID: "busy", WorkerID: "probook", Status: models.StatusRunning}
	s.Mu.Unlock()

	m, err := manifest.Parse(strings.NewReader(`
project: "Whispering-Wilds"
tasks:
  ai_improvement:
    requirements:
      capabilities: ["ai-agent", "python"]
    execution:
      profile: "AIAgentAuto"
      parameters:
        prompt: "make a safe change"
      changes:
        commit: true
`))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if err := New(s).SubmitManifest(m); err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	var job *models.Job
	for _, j := range s.Jobs {
		if j.ID != "busy" {
			job = j
		}
	}
	if job == nil {
		t.Fatal("expected dispatched job")
	}
	if job.WorkerID != "thinkpad" {
		t.Fatalf("expected ThinkPad, got %s", job.WorkerID)
	}
	if job.Profile != "AIAgent" {
		t.Fatalf("expected AIAgent profile, got %s", job.Profile)
	}
}

func TestDirectorEligibilityExplainsMissingAgentValidationAndBusy(t *testing.T) {
	s, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	s.Mu.Lock()
	s.Workers["thinkpad"] = &models.WorkerState{ID: "thinkpad", NodeName: "ThinkPad-Lenovo", OS: "windows", Status: "online", Capabilities: []string{"antigravity", "ai-agent"}}
	s.Workers["probook"] = &models.WorkerState{ID: "probook", NodeName: "probook", OS: "windows", Status: "online", Capabilities: []string{"codex", "ai-agent", "python"}}
	s.Jobs["busy"] = &models.Job{ID: "busy", WorkerID: "probook", Status: models.StatusRunning}
	s.Mu.Unlock()

	req := manifest.Requirements{Capabilities: []string{"ai-agent", "python"}}
	explanation := New(s).ExplainEligibility(req)
	if !strings.Contains(explanation, "ThinkPad-Lenovo: Python not available") {
		t.Fatalf("missing ThinkPad explanation: %s", explanation)
	}
	if !strings.Contains(explanation, "probook: busy") {
		t.Fatalf("missing probook busy explanation: %s", explanation)
	}
}

func TestDirectorExplicitCodexDoesNotUseAntigravityOnlyWorker(t *testing.T) {
	s, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	s.Mu.Lock()
	s.Workers["thinkpad"] = &models.WorkerState{ID: "thinkpad", NodeName: "ThinkPad-Lenovo", OS: "windows", Status: "online", Capabilities: []string{"antigravity", "ai-agent", "python"}}
	s.Mu.Unlock()

	m, err := manifest.Parse(strings.NewReader(`
project: "Whispering-Wilds"
tasks:
  ai_improvement:
    requirements:
      capabilities: ["codex", "python"]
    execution:
      profile: "CodexExec"
      parameters:
        prompt: "make a safe change"
`))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	err = New(s).SubmitManifest(m)
	if err == nil {
		t.Fatal("expected explicit Codex job to reject Antigravity-only worker")
	}
	if !strings.Contains(err.Error(), "Codex not available") {
		t.Fatalf("expected Codex explanation, got %v", err)
	}
}

func TestDirectorDispatchStages(t *testing.T) {
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
    stages:
      - name: "Agent"
        profile: "GoTest"
      - name: "Test"
        profile: "GoTest"
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
		if len(j.Stages) != 2 {
			t.Errorf("Expected 2 stages, got %d", len(j.Stages))
		}
		if j.Stages[0].Name != "Agent" || j.Stages[1].Name != "Test" {
			t.Errorf("Expected Agent and Test stages, got %s and %s", j.Stages[0].Name, j.Stages[1].Name)
		}
	}
}
