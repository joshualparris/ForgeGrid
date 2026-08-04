package coordinator

import (
	"time"
)

func (c *Coordinator) ScheduleJobs() {
	c.Store.Mu.Lock()
	defer c.Store.Mu.Unlock()

	for _, job := range c.Store.Jobs {
		if job.Status == "pending" && job.WorkerID == "" {
			// Find an eligible worker
			for _, w := range c.Store.Workers {
				if w.Status != "online" || time.Since(w.LastSeen) > 15*time.Second {
					continue
				}

				if job.TargetOS != "" && job.TargetOS != w.OS {
					continue
				}

				if job.MinCores > 0 && job.MinCores > w.PhysicalCores {
					continue
				}

				if job.MinRAMGB > 0 {
					minBytes := uint64(job.MinRAMGB) * 1024 * 1024 * 1024
					if w.AvailableRAM < minBytes {
						continue
					}
				}

				// Assign to this worker
				job.WorkerID = w.ID
				break
			}
		}
	}
	c.Store.Save()
}
