package attempts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var safeAttempt = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}#[1-9][0-9]*$`)
var ErrAlreadyClaimed = errors.New("attempt already claimed")
var ErrTerminal = errors.New("attempt is already terminal")

type State string

const (
	StateClaimed   State = "claimed"
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

type Attempt struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	Number    int       `json:"number"`
	State     State     `json:"state"`
	ClaimedAt time.Time `json:"claimed_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Store struct{ directory string }

func New(directory string) (Store, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return Store{}, err
	}
	return Store{directory: directory}, nil
}
func ID(taskID string, number int) string { return fmt.Sprintf("%s#%d", taskID, number) }
func (store Store) Claim(taskID string, number int) (Attempt, error) {
	id := ID(taskID, number)
	if !safeAttempt.MatchString(id) {
		return Attempt{}, fmt.Errorf("unsafe attempt id %q", id)
	}
	path := filepath.Join(store.directory, id+".json")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		existing, readErr := store.Load(id)
		if readErr != nil {
			return Attempt{}, readErr
		}
		if terminal(existing.State) {
			return existing, ErrTerminal
		}
		return existing, ErrAlreadyClaimed
	}
	if err != nil {
		return Attempt{}, err
	}
	now := time.Now().UTC()
	attempt := Attempt{ID: id, TaskID: taskID, Number: number, State: StateClaimed, ClaimedAt: now, UpdatedAt: now}
	encoderErr := json.NewEncoder(file).Encode(attempt)
	closeErr := file.Close()
	if encoderErr != nil {
		return Attempt{}, encoderErr
	}
	if closeErr != nil {
		return Attempt{}, closeErr
	}
	return attempt, nil
}
func (store Store) Load(id string) (Attempt, error) {
	var attempt Attempt
	data, err := os.ReadFile(filepath.Join(store.directory, id+".json"))
	if err != nil {
		return attempt, err
	}
	err = json.Unmarshal(data, &attempt)
	return attempt, err
}
func terminal(state State) bool {
	return state == StateSucceeded || state == StateFailed || state == StateCancelled
}
