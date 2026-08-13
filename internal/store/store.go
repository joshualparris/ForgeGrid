package store

import (
	"encoding/json"
	"forgegrid/internal/models"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	Mu             sync.RWMutex
	dir            string
	Workers        map[string]*models.WorkerState
	Jobs           map[string]*models.Job
	CoordinatorCfg models.CoordinatorState
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	s := &Store{
		dir:     dir,
		Workers: make(map[string]*models.WorkerState),
		Jobs:    make(map[string]*models.Job),
	}
	s.load()
	return s, nil
}

func (s *Store) Dir() string {
	return s.dir
}

func (s *Store) load() {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	b, err := os.ReadFile(filepath.Join(s.dir, "coordinator.json"))
	if err == nil {
		json.Unmarshal(b, &s.CoordinatorCfg)
	}
	b, err = os.ReadFile(filepath.Join(s.dir, "workers.json"))
	if err == nil {
		json.Unmarshal(b, &s.Workers)
	}
	b, err = os.ReadFile(filepath.Join(s.dir, "jobs.json"))
	if err == nil {
		json.Unmarshal(b, &s.Jobs)
	}
}

func (s *Store) Save() error {
	saveFile := func(name string, v interface{}) error {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		// Atomic save
		tmp := filepath.Join(s.dir, name+".tmp")
		if err := os.WriteFile(tmp, b, 0600); err != nil {
			return err
		}
		return os.Rename(tmp, filepath.Join(s.dir, name))
	}

	if err := saveFile("coordinator.json", s.CoordinatorCfg); err != nil {
		return err
	}
	if err := saveFile("workers.json", s.Workers); err != nil {
		return err
	}
	if err := saveFile("jobs.json", s.Jobs); err != nil {
		return err
	}
	return nil
}

func (s *Store) DeleteJob(id string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	if _, exists := s.Jobs[id]; !exists {
		return nil
	}

	delete(s.Jobs, id)
	return s.Save()
}
