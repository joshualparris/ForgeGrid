package coordinator

import (
	"testing"
	"time"

	"forgegrid/internal/models"
	"forgegrid/internal/store"
)

func TestSchedulerRequirements(t *testing.T) {
	s, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	c := &Coordinator{
		Store: s,
	}

	w1 := &models.WorkerState{
		ID:           "worker-1",
		Status:       "online",
		OS:           "linux",
		PhysicalCores: 2,
		AvailableRAM:  2 * 1024 * 1024 * 1024, // 2 GB
		LastSeen:      time.Now(),
	}

	w2 := &models.WorkerState{
		ID:           "worker-2",
		Status:       "online",
		OS:           "windows",
		PhysicalCores: 8,
		AvailableRAM:  16 * 1024 * 1024 * 1024, // 16 GB
		LastSeen:      time.Now(),
	}

	c.Store.Workers[w1.ID] = w1
	c.Store.Workers[w2.ID] = w2

	// Job 1: needs 4 GB RAM, linux. Worker 1 doesn't have enough RAM, so it should remain pending.
	j1 := &models.Job{
		ID:       "job-1",
		Status:   "pending",
		TargetOS: "linux",
		MinRAMGB: 4,
		MinCores: 2,
	}

	// Job 2: needs 4 GB RAM, windows. Worker 2 has enough.
	j2 := &models.Job{
		ID:       "job-2",
		Status:   "pending",
		TargetOS: "windows",
		MinRAMGB: 4,
		MinCores: 4,
	}

	c.Store.Jobs[j1.ID] = j1
	c.Store.Jobs[j2.ID] = j2

	c.ScheduleJobs()

	if j1.WorkerID != "" {
		t.Errorf("job-1 should not be scheduled due to RAM constraints, but got worker %s", j1.WorkerID)
	}

	if j2.WorkerID != "worker-2" {
		t.Errorf("job-2 should be scheduled to worker-2, but got %s", j2.WorkerID)
	}
}
