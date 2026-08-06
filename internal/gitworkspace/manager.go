package gitworkspace

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Manager handles restricted git operations to prepare isolated worktrees
type Manager struct {
	BaseDir      string
	AllowedRepos map[string]bool
}

// NewManager creates a new Git workspace manager
func NewManager(baseDir string, allowedRepos []string) *Manager {
	allowed := make(map[string]bool)
	for _, repo := range allowedRepos {
		allowed[repo] = true
	}
	return &Manager{
		BaseDir:      baseDir,
		AllowedRepos: allowed,
	}
}

// PrepareWorkspace clones (if needed), fetches, and creates an isolated worktree
func (m *Manager) PrepareWorkspace(repoURL, pinnedBaseCommit, generatedBranchName, taskID string) (string, error) {
	if !m.AllowedRepos[repoURL] {
		return "", fmt.Errorf("repository not in allowlist: %s", repoURL)
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

	// 4. Create ForgeGrid-owned worktree
	// git worktree add -b <branchName> <path> <commit>
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

// Internal restricted git command execution
func (m *Manager) runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}

func (m *Manager) runGitWithOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return out.String(), err
}
