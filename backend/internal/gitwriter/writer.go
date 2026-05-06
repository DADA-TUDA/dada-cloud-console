package gitwriter

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dada-tuda/console/backend/internal/config"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// GitWriter manages writing Kubernetes manifests to the GitOps state repository.
type GitWriter struct {
	repoPath string
	botName  string
	botEmail string
}

// New creates a GitWriter from config.
func New(cfg *config.Config) *GitWriter {
	return &GitWriter{
		repoPath: cfg.GitStateRepoPath,
		botName:  cfg.GitBotName,
		botEmail: cfg.GitBotEmail,
	}
}

// CommitManifest writes the given YAML content to path within the repo and commits it.
// Returns the commit SHA on success.
func (gw *GitWriter) CommitManifest(relativePath, yamlContent, commitMessage string) (string, error) {
	repo, err := git.PlainOpen(gw.repoPath)
	if err != nil {
		return "", fmt.Errorf("opening repo at %s: %w", gw.repoPath, err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("getting worktree: %w", err)
	}

	// Write file
	absPath := filepath.Join(gw.repoPath, relativePath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return "", fmt.Errorf("creating directories: %w", err)
	}
	if err := os.WriteFile(absPath, []byte(yamlContent), 0o644); err != nil {
		return "", fmt.Errorf("writing manifest file: %w", err)
	}

	// Stage file
	if _, err := worktree.Add(relativePath); err != nil {
		return "", fmt.Errorf("staging file: %w", err)
	}

	// Commit
	hash, err := worktree.Commit(commitMessage, &git.CommitOptions{
		Author: &object.Signature{
			Name:  gw.botName,
			Email: gw.botEmail,
			When:  time.Now(),
		},
	})
	if err != nil {
		return "", fmt.Errorf("committing: %w", err)
	}

	return hash.String(), nil
}
