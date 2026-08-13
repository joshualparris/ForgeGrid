package gitworkspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Manager handles restricted git operations to prepare isolated worktrees
type Manager struct {
	BaseDir      string
	AllowedRepos map[string]bool
	AllowPush    bool
}

type Options struct {
	AllowedRepos map[string]bool
	AllowPush    bool
}

// NewManager creates a new Git workspace manager
func NewManager(baseDir string, opts ...Options) *Manager {
	var options Options
	if len(opts) > 0 {
		options = opts[0]
	}
	return &Manager{
		BaseDir:      baseDir,
		AllowedRepos: options.AllowedRepos,
		AllowPush:    options.AllowPush,
	}
}

// PrepareWorkspace clones (if needed), fetches, and creates an isolated worktree
func (m *Manager) PrepareWorkspace(repoURL, pinnedBaseCommit, generatedBranchName, taskID string) (string, error) {
	if len(m.AllowedRepos) == 0 || !m.AllowedRepos[repoURL] {
		return "", fmt.Errorf("repository not in allowlist: %s", repoURL)
	}
	if strings.TrimSpace(pinnedBaseCommit) == "" {
		return "", fmt.Errorf("base commit is required for git workspace jobs")
	}

	repoName := filepath.Base(repoURL)
	repoName = strings.TrimSuffix(repoName, ".git")
	mainRepoDir := filepath.Join(m.BaseDir, repoName)
	worktreeDir := filepath.Join(m.BaseDir, fmt.Sprintf("%s-worktree-%s", repoName, taskID))

	// 1. Clone if it doesn't exist
	if _, err := os.Stat(mainRepoDir); os.IsNotExist(err) {
		if err := m.runGit(m.BaseDir, "clone", repoURL, repoName); err != nil {
			return "", fmt.Errorf("failed to clone repository: %w", err)
		}
	}

	// 2. Fetch latest
	if err := m.runGit(mainRepoDir, "fetch", "origin"); err != nil {
		return "", fmt.Errorf("failed to fetch remote: %w", err)
	}

	// 3. Verify commit SHA
	if err := m.runGit(mainRepoDir, "cat-file", "-e", pinnedBaseCommit+"^{commit}"); err != nil {
		return "", fmt.Errorf("pinned base commit %s not found or invalid: %w", pinnedBaseCommit, err)
	}

	if err := m.runGit(mainRepoDir, "show-ref", "--verify", "--quiet", "refs/heads/"+generatedBranchName); err == nil {
		return "", fmt.Errorf("branch already exists locally: %s", generatedBranchName)
	}
	if err := m.runGit(mainRepoDir, "ls-remote", "--exit-code", "--heads", "origin", generatedBranchName); err == nil {
		return "", fmt.Errorf("branch already exists on origin: %s", generatedBranchName)
	}

	if err := m.runGit(mainRepoDir, "worktree", "add", "-b", generatedBranchName, worktreeDir, pinnedBaseCommit); err != nil {
		return "", fmt.Errorf("failed to create worktree: %w", err)
	}

	return worktreeDir, nil
}

// CleanWorktree checks if the worktree is clean
func (m *Manager) IsWorktreeClean(worktreeDir string) (bool, error) {
	out, err := m.runGitWithOutput(worktreeDir, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("failed to check worktree status: %w", err)
	}
	return len(strings.TrimSpace(out)) == 0, nil
}

// ProduceDiff produces a diff and status report
func (m *Manager) ProduceDiff(worktreeDir string) (string, error) {
	status, err := m.runGitWithOutput(worktreeDir, "status")
	if err != nil {
		return "", fmt.Errorf("failed to get status: %w", err)
	}
	diff, err := m.runGitWithOutput(worktreeDir, "diff")
	if err != nil {
		return "", fmt.Errorf("failed to get diff: %w", err)
	}
	return fmt.Sprintf("STATUS:\n%s\n\nDIFF:\n%s", status, diff), nil
}

// CleanupWorktree removes the worktree safely
func (m *Manager) CleanupWorktree(mainRepoDir, worktreeDir, branchName string) error {
	// First remove the worktree
	if err := m.runGit(mainRepoDir, "worktree", "remove", "--force", worktreeDir); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}
	// Delete the branch
	if err := m.runGit(mainRepoDir, "branch", "-D", branchName); err != nil {
		return fmt.Errorf("failed to delete worktree branch: %w", err)
	}
	return nil
}

// CommitAndMaybePush commits all changes in the worktree and optionally pushes to origin.
func (m *Manager) CommitAndMaybePush(worktreeDir, repoURL, commitMsg string, push bool) (string, error) {
	if push {
		if !m.AllowPush {
			return "", fmt.Errorf("push denied by worker policy")
		}
		if len(m.AllowedRepos) == 0 || !m.AllowedRepos[repoURL] {
			return "", fmt.Errorf("push denied because repository is not in allowlist: %s", repoURL)
		}
	}

	if err := m.runGit(worktreeDir, "add", "."); err != nil {
		return "", fmt.Errorf("failed to add changes: %w", err)
	}

	status, err := m.runGitWithOutput(worktreeDir, "status", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("failed to check status before commit: %w", err)
	}
	if len(strings.TrimSpace(status)) == 0 {
		return "No changes to commit.", nil
	}

	if strings.TrimSpace(commitMsg) == "" {
		return "", fmt.Errorf("commit message is required")
	}
	if err := m.runGit(worktreeDir, "commit", "-m", commitMsg); err != nil {
		return "", fmt.Errorf("failed to commit changes: %w", err)
	}

	if !push {
		return "Committed changes locally; push disabled for this job.", nil
	}
	if err := m.runGit(worktreeDir, "push", "origin", "HEAD"); err != nil {
		return "", fmt.Errorf("failed to push changes: %w", err)
	}

	return "Committed and pushed changes to origin.", nil
}

func (m *Manager) CreatePullRequest(worktreeDir, title, body string) (string, error) {
	if strings.TrimSpace(title) == "" {
		title = "ForgeGrid automated changes"
	}
	out, err := m.runWithOutput(worktreeDir, "gh", "pr", "create", "--title", title, "--body", body)
	if err != nil {
		return "", fmt.Errorf("failed to create pull request: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func (m *Manager) CollectArtifacts(worktreeDir string, patterns []string) ([]Artifact, error) {
	var artifacts []Artifact
	seen := make(map[string]bool)
	for _, pattern := range patterns {
		matches, err := artifactMatches(worktreeDir, pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid artifact pattern %s: %w", pattern, err)
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil || info.IsDir() {
				continue
			}
			rel, err := filepath.Rel(worktreeDir, match)
			if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
				continue
			}
			if seen[rel] {
				continue
			}
			sum, err := fileSHA256(match)
			if err != nil {
				return nil, err
			}
			artifact := Artifact{Path: rel, Size: info.Size(), SHA256: sum}
			if info.Size() <= 10*1024*1024 {
				b, err := os.ReadFile(match)
				if err != nil {
					return nil, err
				}
				artifact.ContentBase64 = base64.StdEncoding.EncodeToString(b)
			}
			artifacts = append(artifacts, artifact)
			seen[rel] = true
		}
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
}

func artifactMatches(root, pattern string) ([]string, error) {
	clean := filepath.Clean(pattern)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return nil, fmt.Errorf("artifact pattern escapes workspace")
	}
	if strings.HasSuffix(pattern, "/**") || strings.HasSuffix(pattern, string(filepath.Separator)+"**") {
		dir := strings.TrimSuffix(strings.TrimSuffix(pattern, "/**"), string(filepath.Separator)+"**")
		base := filepath.Join(root, filepath.Clean(dir))
		var matches []string
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() {
				matches = append(matches, path)
			}
			return nil
		})
		if os.IsNotExist(err) {
			return nil, nil
		}
		return matches, err
	}
	return filepath.Glob(filepath.Join(root, clean))
}

type Artifact struct {
	Path          string
	Size          int64
	SHA256        string
	ContentBase64 string
}

// Internal restricted git command execution
func (m *Manager) runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}

func (m *Manager) runGitWithOutput(dir string, args ...string) (string, error) {
	return m.runWithOutput(dir, "git", args...)
}

func (m *Manager) runWithOutput(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil && stderr.Len() > 0 {
		return out.String(), fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), err
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
