package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"forgegrid/internal/version"
)

const (
	StatusCurrent      = "current"
	StatusAvailable    = "available"
	StatusQueued       = "queued"
	StatusBusy         = "busy"
	StatusIncompatible = "incompatible"
	StatusUnavailable  = "unavailable"
)

type Manifest struct {
	SchemaVersion             string     `json:"schema_version"`
	Product                   string     `json:"product"`
	Version                   string     `json:"version"`
	Commit                    string     `json:"commit"`
	GeneratedAt               string     `json:"generated_at"`
	MinimumCoordinatorVersion string     `json:"minimum_coordinator_version"`
	MinimumWorkerVersion      string     `json:"minimum_worker_version"`
	Protocol                  string     `json:"protocol"`
	Artifacts                 []Artifact `json:"artifacts"`
}

type Artifact struct {
	Role         string `json:"role"`
	Platform     string `json:"platform"`
	Architecture string `json:"architecture"`
	SHA256       string `json:"sha256"`
	Path         string `json:"path,omitempty"`
	URL          string `json:"url,omitempty"`
	Size         int64  `json:"size,omitempty"`
}

type Request struct {
	ID              string   `json:"id"`
	TargetVersion   string   `json:"target_version"`
	TargetCommit    string   `json:"target_commit"`
	Artifact        Artifact `json:"artifact"`
	Policy          string   `json:"policy"`
	Status          string   `json:"status"`
	Message         string   `json:"message,omitempty"`
	RequestedAt     string   `json:"requested_at"`
	StartedAt       string   `json:"started_at,omitempty"`
	FinishedAt      string   `json:"finished_at,omitempty"`
	RollbackReady   bool     `json:"rollback_ready,omitempty"`
	HealthCheckPath string   `json:"health_check_path,omitempty"`
}

type WorkerUpdateView struct {
	WorkerID        string           `json:"worker_id"`
	WorkerName      string           `json:"worker_name"`
	Current         version.InfoData `json:"current"`
	LatestVersion   string           `json:"latest_version"`
	Status          string           `json:"status"`
	Reason          string           `json:"reason,omitempty"`
	PendingRequest  *Request         `json:"pending_request,omitempty"`
	Compatible      bool             `json:"compatible"`
	ArtifactPresent bool             `json:"artifact_present"`
}

var checksumRE = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

func LoadManifest(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	base := filepath.Dir(path)
	if err := ValidateManifest(&m, base); err != nil {
		return nil, err
	}
	return &m, nil
}

func ValidateManifest(m *Manifest, baseDir string) error {
	if m == nil {
		return errors.New("manifest is nil")
	}
	if m.SchemaVersion != "1" {
		return fmt.Errorf("unsupported update manifest schema %q", m.SchemaVersion)
	}
	if m.Product != "ForgeGrid" || strings.TrimSpace(m.Version) == "" {
		return errors.New("manifest must name ForgeGrid and include a version")
	}
	if len(m.Artifacts) == 0 {
		return errors.New("manifest has no artifacts")
	}
	seen := map[string]bool{}
	for _, a := range m.Artifacts {
		key := a.Role + "/" + a.Platform + "/" + a.Architecture
		if seen[key] {
			return fmt.Errorf("duplicate artifact %s", key)
		}
		seen[key] = true
		if a.Role != "worker" && a.Role != "coordinator" && a.Role != "all" {
			return fmt.Errorf("unsupported artifact role %q", a.Role)
		}
		if a.Platform == "" || a.Architecture == "" {
			return fmt.Errorf("artifact %s is missing platform or architecture", key)
		}
		if !checksumRE.MatchString(a.SHA256) {
			return fmt.Errorf("artifact %s has invalid sha256", key)
		}
		if a.Path == "" && a.URL == "" {
			return fmt.Errorf("artifact %s has no source", key)
		}
		if a.Path != "" {
			clean := filepath.Clean(a.Path)
			if filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
				return fmt.Errorf("artifact %s path must stay inside update bundle", key)
			}
			if baseDir != "" {
				if _, err := os.Stat(filepath.Join(baseDir, clean)); err != nil {
					return fmt.Errorf("artifact %s is missing: %w", key, err)
				}
			}
		}
		if a.URL != "" {
			u, err := url.Parse(a.URL)
			if err != nil || (u.Scheme != "https" && u.Scheme != "file") {
				return fmt.Errorf("artifact %s url must be https or file", key)
			}
		}
	}
	return nil
}

func SelectArtifact(m *Manifest, role, platform, arch string) (Artifact, bool) {
	if m == nil {
		return Artifact{}, false
	}
	for _, a := range m.Artifacts {
		if (a.Role == role || a.Role == "all") && a.Platform == platform && a.Architecture == arch {
			return a, true
		}
	}
	return Artifact{}, false
}

func NeedsUpdate(current, latest string) bool {
	current = strings.TrimSpace(current)
	latest = strings.TrimSpace(latest)
	return latest != "" && (current == "" || current != latest)
}

func CurrentWorkerArtifact(m *Manifest) (Artifact, bool) {
	return SelectArtifact(m, "worker", runtime.GOOS, runtime.GOARCH)
}

func VerifyFile(path, wantSHA string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, wantSHA) {
		return fmt.Errorf("checksum mismatch: got %s want %s", got, wantSHA)
	}
	return nil
}
