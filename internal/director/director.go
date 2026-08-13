package director

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"sync"

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
			return fmt.Errorf("no eligible online worker found for task '%s'", name)
		}

		job := &models.Job{
			ID:             jobID,
			WorkerID:       assignedWorker,
			Task:           "execute",
			Status:         models.StatusPending,
			Profile:        task.Execution.Profile,
			Parameters:     task.Execution.Parameters,
			TimeoutSeconds: task.Execution.TimeoutSeconds,
			Artefacts:      append([]string{}, task.Artefacts...),
			RequiredLabels: append([]string{}, task.Requirements.Labels...),
			RequiredCaps:   append([]string{}, task.Requirements.Capabilities...),
			MaxRetries:     task.Execution.MaxRetries,
			RepositoryURL:  m.Repository.URL,
			BaseCommit:     m.Repository.BaseCommit,
			BranchName:     m.Repository.Branch,
			CommitChanges:  task.Execution.Changes.Commit,
			PushChanges:    task.Execution.Changes.Push,
			CommitMessage:  task.Execution.Changes.CommitMessage,
			CreatePR:       m.Repository.CreatePR,
			PRTitle:        m.Repository.PRTitle,
			PRBody:         m.Repository.PRBody,
		}

		d.Store.Jobs[jobID] = job
		log.Printf("Director dispatched task '%s' to worker %s as job %s", name, assignedWorker, jobID)
	}

	d.Store.Save()
	return nil
}

func (d *Director) selectWorker(req manifest.Requirements) string {
	bestWorker := ""
	bestScore := math.MinInt
	for _, w := range d.Store.Workers {
		if !workerEligible(w, req) {
			continue
		}
		score := workerScore(w, req) - d.workerLoad(w.ID)*100
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
	if !containsAll(w.Labels, req.Labels) || !containsAll(w.Capabilities, req.Capabilities) {
		return false
	}
	return true
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
