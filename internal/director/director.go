package director

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"forgegrid/internal/manifest"
	"forgegrid/internal/models"
	"forgegrid/internal/store"
)

type Director struct {
	Store *store.Store
	mu    sync.Mutex
}

func New(s *store.Store) *Director {
	return &Director{Store: s}
}

func (d *Director) SubmitManifest(m *manifest.Manifest) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.Store.Mu.Lock()
	defer d.Store.Mu.Unlock()

	for name, task := range m.Tasks {
		jobID := "job-" + cryptoRandomHex(16)

		assignedWorker := d.selectWorker(task.Requirements)

		if assignedWorker == "" {
			return fmt.Errorf("no eligible online worker found for task '%s': %s", name, d.ExplainEligibility(task.Requirements))
		}
		worker := d.Store.Workers[assignedWorker]
		workerName := assignedWorker
		if worker != nil && worker.NodeName != "" {
			workerName = worker.NodeName
		}
		stages := bindAgentProfiles(task.Stages, worker)

		agentRequested := "auto"
		for _, cap := range task.Requirements.Capabilities {
			if strings.HasPrefix(cap, "agent:") {
				agentRequested = strings.TrimPrefix(cap, "agent:")
				break
			}
		}

		job := &models.Job{
			ID:             jobID,
			WorkerID:       assignedWorker,
			WorkerName:     workerName,
			ProjectName:    m.Project,
			TaskName:       name,
			Description:    task.Description,
			Task:           "execute",
			Status:         models.StatusPending,
			CreatedAt:      time.Now(),
			Profile:        stages[0].Profile,
			Parameters:     stages[0].Parameters,
			Tools:          stages[0].Tools,
			TimeoutSeconds: stages[0].TimeoutSeconds,
			Artefacts:      append([]string{}, task.Artefacts...),
			RequiredLabels: append([]string{}, task.Requirements.Labels...),
			RequiredCaps:   append([]string{}, task.Requirements.Capabilities...),
			AgentRequested: agentRequested,
			MaxRetries:     stages[0].MaxRetries,
			RepositoryURL:  m.Repository.URL,
			BaseCommit:     m.Repository.BaseCommit,
			BranchName:     m.Repository.Branch,
			CommitChanges:  stages[0].Changes.Commit,
			PushChanges:    stages[0].Changes.Push,
			CommitMessage:  stages[0].Changes.CommitMessage,
			CreatePR:       m.Repository.CreatePR,
			PRTitle:        m.Repository.PRTitle,
			PRBody:         m.Repository.PRBody,
			CurrentStage:   0,
		}

		for _, s := range stages {
			job.Stages = append(job.Stages, models.JobStage{
				Name:           s.Name,
				Profile:        s.Profile,
				Parameters:     s.Parameters,
				Tools:          s.Tools,
				TimeoutSeconds: s.TimeoutSeconds,
				Status:         models.StatusPending,
			})
		}

		d.Store.Jobs[jobID] = job
		log.Printf("Director dispatched task '%s' to worker %s as job %s", name, assignedWorker, jobID)
	}

	d.Store.Save()
	return nil
}

func bindAgentProfiles(stages []manifest.Execution, worker *models.WorkerState) []manifest.Execution {
	out := make([]manifest.Execution, len(stages))
	copy(out, stages)
	if worker == nil {
		return out
	}
	for i := range out {
		if out[i].Profile != "ai" {
			continue
		}
		// If an action has profile "ai", it doesn't need to be swapped to CodexExec.
		// The scheduler and worker logic will handle "ai" jobs.
	}
	return out
}

func (d *Director) ExplainEligibility(req manifest.Requirements) string {
	if len(d.Store.Workers) == 0 {
		return "no machines are paired yet"
	}
	var explanations []string
	for _, w := range d.Store.Workers {
		name := w.NodeName
		if name == "" {
			name = w.ID
		}
		reasons := d.workerIneligibleReasons(w, req)
		if len(reasons) == 0 {
			explanations = append(explanations, fmt.Sprintf("%s: ready", name))
			continue
		}
		explanations = append(explanations, fmt.Sprintf("%s: %s", name, strings.Join(reasons, "; ")))
	}
	sort.Strings(explanations)
	return strings.Join(explanations, " | ")
}

func (d *Director) workerIneligibleReasons(w *models.WorkerState, req manifest.Requirements) []string {
	var reasons []string
	if w.Status != "online" {
		reasons = append(reasons, "offline")
		return reasons
	}
	if w.Drain {
		reasons = append(reasons, "paused")
	}
	if w.Disabled {
		reasons = append(reasons, "disabled")
	}
	if d.workerLoad(w.ID) > 0 {
		reasons = append(reasons, "busy")
	}
	if req.OS != "" && w.OS != req.OS {
		reasons = append(reasons, fmt.Sprintf("needs %s, this is %s", req.OS, w.OS))
	}
	if req.MinCores > 0 && w.LogicalProcessors < req.MinCores {
		reasons = append(reasons, fmt.Sprintf("needs %d CPU threads", req.MinCores))
	}
	if req.MinRAMGB > 0 && w.AvailableRAM < uint64(req.MinRAMGB)*1024*1024*1024 {
		reasons = append(reasons, fmt.Sprintf("needs %dGB free RAM", req.MinRAMGB))
	}
	for _, label := range missingValues(w.Labels, req.Labels) {
		reasons = append(reasons, fmt.Sprintf("missing label %s", label))
	}
	for _, cap := range missingValues(w.Capabilities, req.Capabilities) {
		reasons = append(reasons, fmt.Sprintf("%s not available", humanCapability(cap)))
	}
	return reasons
}

func (d *Director) selectWorker(req manifest.Requirements) string {
	bestWorker := ""
	bestScore := math.MinInt
	for _, w := range d.Store.Workers {
		if !workerEligible(w, req) {
			continue
		}
		if d.workerLoad(w.ID) > 0 {
			continue
		}
		score := workerScore(w, req)
		if score > bestScore {
			bestScore = score
			bestWorker = w.ID
		}
	}
	return bestWorker
}

func workerEligible(w *models.WorkerState, req manifest.Requirements) bool {
	if w.Status != "online" || w.Drain || w.Disabled {
		return false
	}
	if req.OS != "" && w.OS != req.OS {
		return false
	}
	if req.MinCores > 0 && w.LogicalProcessors < req.MinCores {
		return false
	}
	if req.MinRAMGB > 0 && w.AvailableRAM < uint64(req.MinRAMGB)*1024*1024*1024 {
		return false
	}
	if !containsAll(w.Labels, req.Labels) || !workerHasCapabilities(w.Capabilities, req.Capabilities) {
		return false
	}
	return true
}

func workerHasCapabilities(have, want []string) bool {
	for _, cap := range want {
		if strings.HasPrefix(cap, "agent:") {
			if cap == "agent:auto" {
				// Must have at least one agent
				hasAnyAgent := false
				for _, h := range have {
					if strings.HasPrefix(h, "agent:") {
						hasAnyAgent = true
						break
					}
				}
				if !hasAnyAgent {
					return false
				}
			} else if !containsAll(have, []string{cap}) {
				return false
			}
			continue
		}
		if !containsAll(have, []string{cap}) {
			return false
		}
	}
	return true
}

func missingValues(have, want []string) []string {
	set := make(map[string]bool, len(have))
	for _, v := range have {
		set[v] = true
	}
	var missing []string
	for _, v := range want {
		if strings.HasPrefix(v, "agent:") {
			if v == "agent:auto" {
				hasAnyAgent := false
				for _, h := range have {
					if strings.HasPrefix(h, "agent:") {
						hasAnyAgent = true
						break
					}
				}
				if !hasAnyAgent {
					missing = append(missing, v)
				}
			} else if !set[v] {
				missing = append(missing, v)
			}
			continue
		}
		if !set[v] {
			missing = append(missing, v)
		}
	}
	return missing
}

func containsAny(have, want []string) bool {
	set := make(map[string]bool, len(have))
	for _, v := range have {
		set[v] = true
	}
	for _, v := range want {
		if set[v] {
			return true
		}
	}
	return false
}

func humanCapability(cap string) string {
	names := map[string]string{
		"agent:codex":       "Codex",
		"agent:antigravity": "Antigravity",
		"agent:auto":        "Auto AI Agent",
		"python":      "Python",
		"go":          "Go",
		"node":        "Node",
		"git":         "Git",
		"godot":       "Godot",
	}
	if name := names[cap]; name != "" {
		return name
	}
	return cap
}

func workerScore(w *models.WorkerState, req manifest.Requirements) int {
	score := w.LogicalProcessors*10 + int(w.AvailableRAM/(1024*1024*1024))
	if req.OS != "" && w.OS == req.OS {
		score += 1000
	}
	score += len(req.Labels)*100 + len(req.Capabilities)*100
	return score
}

func (d *Director) workerLoad(workerID string) int {
	load := 0
	for _, j := range d.Store.Jobs {
		if j.WorkerID == workerID && (j.Status == models.StatusPending || j.Status == models.StatusClaimed || j.Status == models.StatusRunning) {
			load++
		}
	}
	return load
}

func containsAll(have, want []string) bool {
	if len(want) == 0 {
		return true
	}
	set := make(map[string]bool, len(have))
	for _, v := range have {
		set[v] = true
	}
	for _, v := range want {
		if !set[v] {
			return false
		}
	}
	return true
}

func cryptoRandomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
