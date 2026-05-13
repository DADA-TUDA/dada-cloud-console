# GitOps Agent — Implementation Plan

**Goal:** Extract git state management into a dedicated `gitops-agent` service. The backend stops touching git entirely. The agent owns clone/pull/push, manifest rendering, bidirectional sync, and the audit commit log.

---

## Architecture

```
┌──────────────────┐         shared postgres          ┌──────────────────────────────┐
│  console-backend │  ──── writes operations ────▶   │        gitops-agent           │
│                  │  ◀─── reads resource_snapshots ─  │                              │
│  (no git code)   │  ◀─── reads git_commits ────────  │  ┌─────────────────────┐    │
└──────────────────┘                                  │  │   DB Watcher         │    │
                                                      │  │   polls operations   │    │
                                                      │  │   Queued → render    │    │
                                                      │  │   → commit → push    │    │
                                                      │  └──────────┬──────────┘    │
                                                      │             │               │
                                                      │  ┌──────────▼──────────┐    │
                                                      │  │   Git Watcher        │    │
                                                      │  │   polls remote HEAD  │    │
                                                      │  │   manual changes →   │    │
                                                      │  │   sync snapshots     │    │
                                                      │  └──────────┬──────────┘    │
                                                      │             │               │
                                                      │  ┌──────────▼──────────┐    │
                                                      │  │   Webhook Server     │    │
                                                      │  │   (optional, if GH   │    │
                                                      │  │    integration on)   │    │
                                                      │  └─────────────────────┘    │
                                                      └──────────────────────────────┘
                                                                    │
                                                              argo-infra git repo
```

---

## Repo Layout

```
dada-cloud/
  gitops-agent/
    cmd/agent/main.go
    internal/
      config/config.go
      db/
        pool.go
        operations.go      — claim/update operations
        commits.go         — write git_commits audit rows
        snapshots.go       — upsert resource_snapshots
        integrations.go    — read git_integrations config
      git/
        manager.go         — clone, pull, push, list-since
        repo.go            — local repo lifecycle (path, lock)
      github/
        client.go          — GitHub REST API (PR, check runs, repo info)
        webhook.go         — /webhook/github handler + HMAC verify
      renderer/
        servicedatabase.go
        app.go
        publicapi.go
      worker/
        db_watcher.go      — DB → Git loop
        git_watcher.go     — Git → DB loop
      server/
        server.go          — HTTP: /healthz + /webhook/github
    Dockerfile
```

Shared renderer code is duplicated here for now. When backend/gitops-agent renderer logic diverges it becomes a `pkg/renderer` internal module.

---

## DB Schema Changes

### Migration 003 — git_commits

```sql
CREATE TABLE IF NOT EXISTS git_commits (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sha          VARCHAR(40) NOT NULL,
    repo_url     VARCHAR(500) NOT NULL,
    branch       VARCHAR(255) NOT NULL,
    path         VARCHAR(500) NOT NULL,
    message      TEXT NOT NULL,
    author_name  VARCHAR(255) NOT NULL,
    author_email VARCHAR(255) NOT NULL,
    operation_id UUID REFERENCES operations(id),
    -- source: 'agent' (written by us) or 'manual' (detected from git)
    source       VARCHAR(20) NOT NULL DEFAULT 'agent',
    pushed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    synced_at    TIMESTAMPTZ             -- when Git Watcher processed it
);

CREATE INDEX IF NOT EXISTS idx_git_commits_operation ON git_commits(operation_id);
CREATE INDEX IF NOT EXISTS idx_git_commits_sha       ON git_commits(sha);
CREATE INDEX IF NOT EXISTS idx_git_commits_pushed_at ON git_commits(pushed_at DESC);
```

### Migration 003 — git_integrations

One row per project. Null = use global agent config (env vars).

```sql
CREATE TABLE IF NOT EXISTS git_integrations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    provider        VARCHAR(20) NOT NULL CHECK (provider IN ('github', 'bitbucket')),
    repo_url        VARCHAR(500) NOT NULL,
    branch          VARCHAR(255) NOT NULL DEFAULT 'main',
    -- encrypted PAT or OAuth token (AES-GCM, key from env GITOPS_ENCRYPTION_KEY)
    token_encrypted BYTEA,
    webhook_secret  VARCHAR(255),
    -- optional: open a PR instead of direct push when true
    use_pr          BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(project_id)
);
```

### Migration 003 — git_sync_state

Tracks last seen HEAD per repo+branch for the Git Watcher.

```sql
CREATE TABLE IF NOT EXISTS git_sync_state (
    repo_url   VARCHAR(500) NOT NULL,
    branch     VARCHAR(255) NOT NULL,
    last_sha   VARCHAR(40)  NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (repo_url, branch)
);
```

---

## Config (env vars)

```
# Postgres (same DB as backend)
DATABASE_URL

# Default git target (used when project has no git_integrations row)
GITOPS_DEFAULT_REPO_URL      — e.g. https://github.com/DADA-TUDA/argo-infra.git
GITOPS_DEFAULT_BRANCH        — e.g. console-migration
GITOPS_DEFAULT_USERNAME      — git HTTP username
GITOPS_DEFAULT_TOKEN         — git HTTP token (PAT)

# Local clone directory (PVC-backed in k8s)
GITOPS_REPO_LOCAL_PATH       — /var/lib/gitops-repos  (one subdir per repo)

# Bot identity
GITOPS_BOT_NAME              — DADA Platform Bot
GITOPS_BOT_EMAIL             — bot@dada-tuda.ru

# DB Watcher
GITOPS_POLL_INTERVAL_DB      — 3s
# Git Watcher
GITOPS_POLL_INTERVAL_GIT     — 30s

# Webhook server (only needed if GitHub integration enabled)
GITOPS_WEBHOOK_PORT          — 9090
GITOPS_ENCRYPTION_KEY        — 32-byte hex for token encryption

# GitHub API (optional, for PR creation / check runs)
GITHUB_APP_ID
GITHUB_APP_PRIVATE_KEY
```

---

## DB Watcher Loop

**Ownership:** gitops-agent fully owns `Queued → Rendering → CommittingToGit → Committed → WaitingForArgoSync`. The backend worker (`backend/internal/worker/`) is removed.

```
every GITOPS_POLL_INTERVAL_DB:
  claim one operation WHERE status = 'Queued' (SELECT FOR UPDATE SKIP LOCKED)
  mark → Rendering

  load project/env info (name, namespace)
  resolve git config: git_integrations row OR default env vars

  ensure local repo:
    if not cloned → git clone --depth=1 repo_url branch
    else → git pull --ff-only (with retry on conflict)

  render YAML → renderer.Render(kind, spec)

  write file to worktree path
  git add + git commit (author = bot)
  git push

  insert git_commits row (source=agent, sha, path, operation_id)

  update operations: status=Committed, git_commit=sha, git_path=path
  mark → WaitingForArgoSync
```

**Conflict on push (LWW):**
```
if push rejected (non-fast-forward):
  git pull --rebase
  re-push
  if still rejected → fail operation with error_code=GIT_CONFLICT
```

LWW means the rebase puts our commit on top — our write wins. Manual edits to the same file that happened between our pull and push are rebased under our commit (git rebase semantics). If rebase itself conflicts → fail the operation so the user can retry.

---

## Git Watcher Loop

Detects commits made directly in git (manual edits, infra team pushes) and syncs back to DB.

```
every GITOPS_POLL_INTERVAL_GIT (or on GitHub webhook push event):
  for each active repo+branch (from git_integrations + default):
    fetch remote (no checkout)
    read remote HEAD sha
    compare to git_sync_state.last_sha
    if same → skip

    list commits since last_sha (git log last_sha..HEAD)
    for each new commit:
      if commit sha already in git_commits (source=agent) → mark synced_at, skip
      # this is a manual commit
      parse changed file paths
      for each path matching clusters/*/projects/*/environments/*/apps/*/app.yaml:
        read file content → parse App CR
        resolve project/env from path segments
        upsert resource_snapshots (kind=App, LWW: last_synced_at = commit timestamp)
      insert git_commits row (source=manual, sha, path, operation_id=null)

    update git_sync_state.last_sha = remote HEAD
```

**LWW in Git→DB direction:**  
`resource_snapshots.last_synced_at` is the tiebreaker. If DB already has a newer snapshot (from a console operation that completed after the manual git commit), the upsert is skipped via:
```sql
ON CONFLICT (project_id, environment_id, kind, name)
DO UPDATE SET ... WHERE resource_snapshots.last_synced_at < EXCLUDED.last_synced_at
```

---

## GitHub Webhook Server

Only active when `GITOPS_WEBHOOK_PORT` is set and a `git_integrations` row has `webhook_secret`.

```
POST /webhook/github
  verify HMAC-SHA256 signature (X-Hub-Signature-256 header vs webhook_secret)
  parse event type
  if push event:
    find git_integrations row by repo URL + branch
    trigger Git Watcher immediately for that repo (channel signal, no wait)
  if ping event:
    return 200 OK
  else:
    return 200 OK (ignore)
```

No GitHub OAuth for webhooks — secret is registered manually in GitHub repo settings. GitHub App auth (GITHUB_APP_ID + private key) is only needed later for PR creation and check runs.

---

## Backend Changes

1. **Delete** `backend/internal/gitwriter/` — renderer code moves to `gitops-agent/internal/renderer/`
2. **Delete** `backend/internal/worker/worker.go` — agent replaces the worker
3. **Keep** `backend/internal/config/config.go` (remove `GitStateRepoPath`, `GitBotName`, `GitBotEmail`)
4. **Keep** all API handlers — they still read `resource_snapshots`, `git_commits`, `operations`
5. **Remove** git_commits from any API response — commits are implementation detail, not product surface

---

## New Operations Status Flow

```
Created (backend API creates)
  → Queued (backend worker advances — stays in backend, just the advance step)
    → Rendering (gitops-agent claims)
      → CommittingToGit
        → Committed
          → WaitingForArgoSync
            → Ready  (future: Argo status watcher, not in this plan)
```

The `advanceCreatedToQueued` step (the one liner that moves Created→Queued) stays in backend or moves to agent — doesn't matter. Simplest: move to agent, backend only creates `Created` operations.

---

## Helm Chart Changes

New deployment in `helm/dada-cloud-console/`:

```yaml
gitopsAgent:
  enabled: true
  image:
    repository: dada-cloud-gitops-agent
    tag: "latest"
  env:
    GITOPS_DEFAULT_REPO_URL: "https://github.com/DADA-TUDA/argo-infra.git"
    GITOPS_DEFAULT_BRANCH: "console-migration"
    GITOPS_REPO_LOCAL_PATH: "/var/lib/gitops-repos"
    GITOPS_POLL_INTERVAL_DB: "3s"
    GITOPS_POLL_INTERVAL_GIT: "30s"
    GITOPS_WEBHOOK_PORT: "9090"
    GITOPS_BOT_NAME: "DADA Platform Bot"
    GITOPS_BOT_EMAIL: "bot@dada-tuda.ru"
  secret:
    DATABASE_URL: "..."
    GITOPS_DEFAULT_TOKEN: "change-me"
    GITOPS_ENCRYPTION_KEY: "change-me-32-bytes"
  persistence:
    enabled: true
    size: 2Gi
    mountPath: /var/lib/gitops-repos
```

---

## Task Sequence

- [ ] Migration 003: git_commits, git_integrations, git_sync_state tables
- [ ] gitops-agent scaffold: module, config, db pool
- [ ] Renderer package: copy + adapt servicedatabase/app/publicapi from backend
- [ ] git/manager.go: clone, pull-rebase, commit, push, list-since
- [ ] DB Watcher: claim loop, render, commit, push, update status
- [ ] Git Watcher: fetch, diff commits, parse paths, upsert snapshots
- [ ] Webhook server: HMAC verify, push event → trigger git watcher
- [ ] Backend: remove gitwriter + worker, add /git-commits API endpoint
- [ ] Helm: add gitops-agent deployment + PVC
- [ ] Migration 003 applied to prod DB
