-- 001_initial_schema.sql
-- Initial schema for DADA Cloud Console

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      VARCHAR(100) UNIQUE NOT NULL,
    email         VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,    -- bcrypt
    display_name  VARCHAR(255) NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Projects table
CREATE TABLE IF NOT EXISTS projects (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                VARCHAR(100) UNIQUE NOT NULL,  -- slug: internal, client-a
    display_name        VARCHAR(255) NOT NULL,
    owner_type          VARCHAR(50) NOT NULL DEFAULT 'team',
    owner_id            UUID,
    default_environment VARCHAR(50) NOT NULL DEFAULT 'prod',
    quotas              JSONB NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Project members (user-project-role)
CREATE TABLE IF NOT EXISTS project_members (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       VARCHAR(50) NOT NULL,  -- client-viewer, client-admin, developer, platform-admin
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(project_id, user_id)
);

-- Environments table
CREATE TABLE IF NOT EXISTS environments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name       VARCHAR(100) NOT NULL,  -- dev, prod
    namespace  VARCHAR(255) NOT NULL,  -- kubernetes namespace: internal-prod
    type       VARCHAR(20) NOT NULL CHECK (type IN ('dev', 'prod')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(project_id, name)
);

-- Operations table (core async state machine)
CREATE TABLE IF NOT EXISTS operations (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id         UUID NOT NULL REFERENCES users(id),
    project_id       UUID NOT NULL REFERENCES projects(id),
    environment_id   UUID REFERENCES environments(id),
    action           VARCHAR(100) NOT NULL,        -- CreateServiceDatabase, CreateApp, etc.
    resource_kind    VARCHAR(100) NOT NULL,        -- ServiceDatabase, App, ServiceEndpoint
    resource_name    VARCHAR(255) NOT NULL,
    status           VARCHAR(50) NOT NULL DEFAULT 'Created',
    payload          JSONB NOT NULL DEFAULT '{}',  -- typed action input
    validation_result JSONB,
    git_commit       VARCHAR(100),
    git_path         VARCHAR(500),
    argo_application VARCHAR(255),
    error_code       VARCHAR(100),
    error_message    TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Audit events (immutable)
CREATE TABLE IF NOT EXISTS audit_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id      UUID NOT NULL REFERENCES users(id),
    project_id    UUID REFERENCES projects(id),
    operation_id  UUID REFERENCES operations(id),
    action        VARCHAR(100) NOT NULL,
    resource_kind VARCHAR(100),
    resource_name VARCHAR(255),
    metadata      JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Resource snapshots (cached status from K8s/Argo)
CREATE TABLE IF NOT EXISTS resource_snapshots (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     UUID NOT NULL REFERENCES projects(id),
    environment_id UUID REFERENCES environments(id),
    kind           VARCHAR(100) NOT NULL,        -- ServiceDatabase, App
    name           VARCHAR(255) NOT NULL,
    phase          VARCHAR(50),                  -- Ready, Pending, Failed
    summary_json   JSONB NOT NULL DEFAULT '{}',  -- status details
    last_synced_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(project_id, environment_id, kind, name)
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_operations_project_id  ON operations(project_id);
CREATE INDEX IF NOT EXISTS idx_operations_status      ON operations(status);
CREATE INDEX IF NOT EXISTS idx_operations_created_at  ON operations(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_project_id   ON audit_events(project_id);
CREATE INDEX IF NOT EXISTS idx_audit_events_operation_id ON audit_events(operation_id);
CREATE INDEX IF NOT EXISTS idx_resource_snapshots_project_env ON resource_snapshots(project_id, environment_id);
CREATE INDEX IF NOT EXISTS idx_project_members_user   ON project_members(user_id);
