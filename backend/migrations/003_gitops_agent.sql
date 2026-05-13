-- 003_gitops_agent.sql
-- Tables owned by gitops-agent: commit audit log, per-project git integration config,
-- and last-seen HEAD tracking for the Git Watcher.

CREATE TABLE IF NOT EXISTS git_commits (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    sha          VARCHAR(40) NOT NULL,
    repo_url     VARCHAR(500) NOT NULL,
    branch       VARCHAR(255) NOT NULL,
    path         VARCHAR(500) NOT NULL,
    message      TEXT        NOT NULL,
    author_name  VARCHAR(255) NOT NULL,
    author_email VARCHAR(255) NOT NULL,
    operation_id UUID        REFERENCES operations(id),
    -- 'agent' = written by gitops-agent; 'manual' = detected from git history
    source       VARCHAR(20) NOT NULL DEFAULT 'agent',
    pushed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    synced_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_git_commits_operation ON git_commits(operation_id);
CREATE INDEX IF NOT EXISTS idx_git_commits_sha       ON git_commits(sha);
CREATE INDEX IF NOT EXISTS idx_git_commits_pushed_at ON git_commits(pushed_at DESC);

-- One row per project. Null = agent uses global env-var config.
CREATE TABLE IF NOT EXISTS git_integrations (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    provider        VARCHAR(20) NOT NULL CHECK (provider IN ('github', 'bitbucket')),
    repo_url        VARCHAR(500) NOT NULL,
    branch          VARCHAR(255) NOT NULL DEFAULT 'main',
    -- AES-GCM encrypted PAT or OAuth token (key = GITOPS_ENCRYPTION_KEY env var)
    token_encrypted BYTEA,
    webhook_secret  VARCHAR(255),
    use_pr          BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(project_id)
);

-- Last-seen remote HEAD per repo+branch for the Git Watcher.
CREATE TABLE IF NOT EXISTS git_sync_state (
    repo_url   VARCHAR(500) NOT NULL,
    branch     VARCHAR(255) NOT NULL,
    last_sha   VARCHAR(40)  NOT NULL,
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (repo_url, branch)
);
