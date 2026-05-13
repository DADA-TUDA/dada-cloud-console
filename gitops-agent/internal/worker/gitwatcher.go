package worker

import (
	"context"
	"encoding/json"
	"path"
	"regexp"
	"time"

	"github.com/dada-tuda/console/gitops-agent/internal/config"
	"github.com/dada-tuda/console/gitops-agent/internal/crypto"
	"github.com/dada-tuda/console/gitops-agent/internal/db"
	"github.com/dada-tuda/console/gitops-agent/internal/git"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// appPathRe matches clusters/<cluster>/projects/<project>/environments/<env>/apps/<app>/app.yaml
// Capture groups: 1=project, 2=env, 3=app
var appPathRe = regexp.MustCompile(`^clusters/[^/]+/projects/([^/]+)/environments/([^/]+)/apps/([^/]+)/app\.yaml$`)

// GitWatcher polls remote repos for new commits and syncs them to the DB.
type GitWatcher struct {
	pool     *pgxpool.Pool
	cfg      *config.Config
	managers map[string]*git.Manager
}

func NewGitWatcher(pool *pgxpool.Pool, cfg *config.Config, defaultMgr *git.Manager) *GitWatcher {
	return &GitWatcher{
		pool: pool,
		cfg:  cfg,
		managers: map[string]*git.Manager{
			cfg.DefaultRepoURL: defaultMgr,
		},
	}
}

func (w *GitWatcher) Start(ctx context.Context) {
	log.Info().Dur("interval", w.cfg.PollIntervalGit).Msg("git-watcher started")
	ticker := time.NewTicker(w.cfg.PollIntervalGit)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.poll(ctx)
		}
	}
}

// TriggerNow allows the webhook handler to request an immediate sync.
func (w *GitWatcher) TriggerNow(ctx context.Context) {
	w.poll(ctx)
}

func (w *GitWatcher) poll(ctx context.Context) {
	// Build list of managers to poll: default + any project integrations.
	managers := w.currentManagers(ctx)

	for _, mgr := range managers {
		if err := w.syncRepo(ctx, mgr); err != nil {
			log.Error().Err(err).Str("repo", mgr.RepoURL()).Msg("git-watcher: sync failed")
		}
	}
}

func (w *GitWatcher) currentManagers(ctx context.Context) []*git.Manager {
	mgrs := []*git.Manager{w.managers[w.cfg.DefaultRepoURL]}

	integrations, err := db.AllIntegrations(ctx, w.pool)
	if err != nil {
		log.Error().Err(err).Msg("git-watcher: loading integrations")
		return mgrs
	}

	for _, ig := range integrations {
		if _, ok := w.managers[ig.RepoURL]; ok {
			mgrs = append(mgrs, w.managers[ig.RepoURL])
			continue
		}
		token, err := crypto.DecryptToken(w.cfg.EncryptionKey, ig.TokenEncrypted)
		if err != nil {
			log.Warn().Err(err).Str("repo", ig.RepoURL).Msg("git-watcher: decrypt token failed, skipping")
			continue
		}
		mgr := git.New(git.RepoConfig{
			RepoURL:   ig.RepoURL,
			Branch:    ig.Branch,
			Username:  ig.Provider,
			Token:     token,
			LocalBase: w.cfg.RepoLocalPath,
		})
		w.managers[ig.RepoURL] = mgr
		mgrs = append(mgrs, mgr)
	}
	return mgrs
}

func (w *GitWatcher) syncRepo(ctx context.Context, mgr *git.Manager) error {
	if err := mgr.EnsureCloned(); err != nil {
		return err
	}

	lastSHA, err := db.GetSyncState(ctx, w.pool, mgr.RepoURL(), mgr.Branch())
	if err != nil {
		return err
	}

	commits, err := mgr.CommitsSince(lastSHA)
	if err != nil {
		return err
	}
	if len(commits) == 0 {
		return nil
	}

	log.Info().Str("repo", mgr.RepoURL()).Int("commits", len(commits)).Msg("git-watcher: new commits")

	for _, c := range commits {
		w.processCommit(ctx, mgr, c)
	}

	// Advance sync state to the latest commit.
	newSHA := commits[len(commits)-1].SHA
	return db.SetSyncState(ctx, w.pool, mgr.RepoURL(), mgr.Branch(), newSHA)
}

func (w *GitWatcher) processCommit(ctx context.Context, mgr *git.Manager, c git.Commit) {
	for _, filePath := range c.Files {
		m := appPathRe.FindStringSubmatch(filePath)
		if m == nil {
			continue
		}
		projectSlug, envSlug, appName := m[1], m[2], m[3]
		w.syncAppFile(ctx, mgr, filePath, projectSlug, envSlug, appName, c)
	}
}

func (w *GitWatcher) syncAppFile(ctx context.Context, mgr *git.Manager, filePath, projectSlug, envSlug, appName string, c git.Commit) {
	_ = path.Base(filePath) // satisfy import

	// Resolve project + environment IDs.
	var projectID uuid.UUID
	var environmentID uuid.UUID
	err := w.pool.QueryRow(ctx, `
		SELECT p.id, e.id
		FROM projects p JOIN environments e ON e.project_id = p.id
		WHERE p.name = $1 AND e.name = $2
	`, projectSlug, envSlug).Scan(&projectID, &environmentID)
	if err != nil {
		log.Warn().Err(err).Str("project", projectSlug).Str("env", envSlug).Msg("git-watcher: project/env not found")
		return
	}

	summaryJSON, _ := json.Marshal(map[string]any{
		"git_sha":     c.SHA,
		"git_message": c.Message,
		"git_author":  c.Author,
		"app_name":    appName,
		"status":      "Unknown",
		"message":     "Synced from git",
	})

	envUUID := &environmentID
	if err := db.UpsertSnapshot(ctx, w.pool,
		projectID, envUUID,
		"App", appName, "Unknown", summaryJSON, c.When,
	); err != nil {
		log.Error().Err(err).Str("app", appName).Msg("git-watcher: upsert snapshot")
		return
	}

	// Record the commit in git_commits (no operation_id — originated in git).
	if err := db.InsertCommit(ctx, w.pool,
		c.SHA, mgr.RepoURL(), mgr.Branch(), filePath, c.Message,
		c.Author, c.Email, nil, "git",
	); err != nil {
		log.Warn().Err(err).Str("sha", c.SHA).Msg("git-watcher: record commit")
	}
}
