package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"forgegrid/internal/models"
)

type githubRepo struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	FullName      string    `json:"full_name"`
	Description   string    `json:"description"`
	Private       bool      `json:"private"`
	Archived      bool      `json:"archived"`
	DefaultBranch string    `json:"default_branch"`
	Language      string    `json:"language"`
	CloneURL      string    `json:"clone_url"`
	HTMLURL       string    `json:"html_url"`
	UpdatedAt     time.Time `json:"updated_at"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

type githubUser struct {
	Login string `json:"login"`
}

func (c *Coordinator) githubToken() string {
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		return token
	}
	b, err := os.ReadFile(filepath.Join(c.Store.Dir(), "github-token.txt"))
	if err != nil {
		return ""
	}
	if info, err := os.Stat(filepath.Join(c.Store.Dir(), "github-token.txt")); err == nil && info.Mode().Perm()&0077 != 0 {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func maskSecretError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		msg = strings.ReplaceAll(msg, token, "***")
	}
	return msg
}

func (c *Coordinator) refreshGitHubProjects(ctx context.Context) error {
	token := c.githubToken()
	if token == "" {
		c.Store.Mu.Lock()
		c.Store.ProjectLibrary.Connected = false
		c.Store.ProjectLibrary.LastError = "GitHub not connected - set GITHUB_TOKEN or create github-token.txt in the coordinator data folder"
		err := c.Store.Save()
		c.Store.Mu.Unlock()
		return err
	}

	client := &http.Client{Timeout: 20 * time.Second}
	login, err := githubLogin(ctx, client, token)
	if err != nil {
		c.setGitHubError(err)
		return err
	}
	repos, err := githubRepos(ctx, client, token)
	if err != nil {
		c.setGitHubError(err)
		return err
	}

	c.Store.Mu.Lock()
	defer c.Store.Mu.Unlock()
	previous := c.Store.ProjectLibrary.Projects
	next := make(map[string]*models.Project, len(repos))
	for _, repo := range repos {
		id := repo.FullName
		project := &models.Project{
			ID:            id,
			Name:          repo.Name,
			FullName:      repo.FullName,
			Owner:         repo.Owner.Login,
			Description:   repo.Description,
			Private:       repo.Private,
			Archived:      repo.Archived,
			DefaultBranch: repo.DefaultBranch,
			Language:      repo.Language,
			CloneURL:      repo.CloneURL,
			HTMLURL:       repo.HTMLURL,
			UpdatedAt:     repo.UpdatedAt,
			Source:        "github",
		}
		if old := previous[id]; old != nil {
			project.LastUsedAt = old.LastUsedAt
			project.Favorite = old.Favorite
		}
		next[id] = project
	}
	c.Store.ProjectLibrary = models.ProjectLibrary{
		Projects:    next,
		LastRefresh: time.Now(),
		Login:       login,
		Connected:   true,
	}
	return c.Store.Save()
}

func (c *Coordinator) setGitHubError(err error) {
	c.Store.Mu.Lock()
	c.Store.ProjectLibrary.Connected = false
	c.Store.ProjectLibrary.LastError = maskSecretError(err)
	c.Store.Save()
	c.Store.Mu.Unlock()
}

func githubLogin(ctx context.Context, client *http.Client, token string) (string, error) {
	var user githubUser
	if err := githubGet(ctx, client, token, "https://api.github.com/user", &user); err != nil {
		return "", err
	}
	return user.Login, nil
}

func githubRepos(ctx context.Context, client *http.Client, token string) ([]githubRepo, error) {
	var all []githubRepo
	nextURL := "https://api.github.com/user/repos?per_page=100&affiliation=owner,collaborator,organization_member&sort=updated&direction=desc"
	for nextURL != "" {
		var page []githubRepo
		link, err := githubGetWithLink(ctx, client, token, nextURL, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		nextURL = nextLink(link)
	}
	return all, nil
}

func githubGet(ctx context.Context, client *http.Client, token, apiURL string, target interface{}) error {
	_, err := githubGetWithLink(ctx, client, token, apiURL, target)
	return err
}

func githubGetWithLink(ctx context.Context, client *http.Client, token, apiURL string, target interface{}) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GitHub API returned %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return "", err
	}
	return resp.Header.Get("Link"), nil
}

func nextLink(linkHeader string) string {
	for _, part := range strings.Split(linkHeader, ",") {
		sections := strings.Split(part, ";")
		if len(sections) < 2 {
			continue
		}
		if strings.TrimSpace(sections[1]) != `rel="next"` {
			continue
		}
		raw := strings.TrimSpace(sections[0])
		raw = strings.TrimPrefix(raw, "<")
		raw = strings.TrimSuffix(raw, ">")
		if _, err := url.ParseRequestURI(raw); err == nil {
			return raw
		}
	}
	return ""
}

func sortedProjects(projects map[string]*models.Project) []models.Project {
	out := make([]models.Project, 0, len(projects))
	for _, project := range projects {
		if project != nil {
			out = append(out, *project)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Favorite != out[j].Favorite {
			return out[i].Favorite
		}
		if !out[i].LastUsedAt.Equal(out[j].LastUsedAt) {
			return out[i].LastUsedAt.After(out[j].LastUsedAt)
		}
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return strings.ToLower(out[i].FullName) < strings.ToLower(out[j].FullName)
	})
	return out
}
