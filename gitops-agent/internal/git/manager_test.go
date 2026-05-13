package git_test

import (
	"testing"

	"github.com/dada-tuda/console/gitops-agent/internal/git"
)

func TestRepoSlug_HTTPS(t *testing.T) {
	mgr := git.New(git.RepoConfig{
		RepoURL:   "https://github.com/DADA-TUDA/argo-infra.git",
		Branch:    "main",
		LocalBase: "/tmp/test-repos",
	})
	// slug should be dada-tuda-argo-infra (lowercased, .git stripped)
	got := mgr.LocalPath()
	want := "/tmp/test-repos/dada-tuda-argo-infra"
	if got != want {
		t.Errorf("LocalPath() = %q, want %q", got, want)
	}
}

func TestRepoSlug_HTTPS_NoGit(t *testing.T) {
	mgr := git.New(git.RepoConfig{
		RepoURL:   "https://github.com/DADA-TUDA/console",
		Branch:    "main",
		LocalBase: "/tmp/test-repos",
	})
	got := mgr.LocalPath()
	want := "/tmp/test-repos/dada-tuda-console"
	if got != want {
		t.Errorf("LocalPath() = %q, want %q", got, want)
	}
}

func TestAccessors(t *testing.T) {
	cfg := git.RepoConfig{
		RepoURL:   "https://github.com/foo/bar.git",
		Branch:    "develop",
		LocalBase: "/tmp",
	}
	mgr := git.New(cfg)
	if mgr.RepoURL() != cfg.RepoURL {
		t.Errorf("RepoURL() = %q, want %q", mgr.RepoURL(), cfg.RepoURL)
	}
	if mgr.Branch() != cfg.Branch {
		t.Errorf("Branch() = %q, want %q", mgr.Branch(), cfg.Branch)
	}
}
