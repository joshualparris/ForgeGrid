package director

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
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

		var assignedWorker string
		for _, w := range d.Store.Workers {
			if w.Status == "online" && (task.Requirements.OS == "" || w.OS == task.Requirements.OS) {
				assignedWorker = w.ID
				break
			}
		}

		if assignedWorker == "" {
			return fmt.Errorf("no eligible online worker found for task '%s'", name)
		}

		job := &models.Job{
			ID:             jobID,
			WorkerID:       assignedWorker,
			Task:           "execute",
			Status:         "pending",
			Profile:        task.Execution.Profile,
			Args:           task.Execution.Args,
			Env:            task.Execution.Env,
			TimeoutSeconds: task.Execution.TimeoutSeconds,
		}

		d.Store.Jobs[jobID] = job
		log.Printf("Director dispatched task '%s' to worker %s as job %s", name, assignedWorker, jobID)
	}

	d.Store.Save()
	return nil
}

func cryptoRandomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
