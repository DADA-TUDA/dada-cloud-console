# Changelog

All notable changes to dada-cloud-console are documented here.
Follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/) and [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## 0.3.0 - 2026-05-13

### Features
- **gitops-agent**: new standalone service that replaces the in-process git worker in the backend. Handles all clone/pull/rebase/push operations with LWW conflict resolution, per-project git integrations (GitHub / Bitbucket via AES-GCM encrypted token), and a Git→DB watcher that syncs manual commits back to `resource_snapshots`
- **gitops-agent — DB Watcher**: polls the `operations` table with `SELECT FOR UPDATE SKIP LOCKED`, renders YAML manifests (ServiceDatabase, App, PublicApi), commits to git, records in `git_commits`, marks operations Committed / Failed
- **gitops-agent — Git Watcher**: polls remote HEAD every 30 s, walks new commits via `CommitsSince`, matches `app.yaml` paths and upserts `resource_snapshots` with last-write-wins timestamp semantics
- **gitops-agent — Webhook server**: `/webhook/github` endpoint with HMAC-SHA256 signature verification triggers an immediate sync on push events
- **gitops-agent — Per-project integrations**: `git_integrations` table links projects to their own git repo / branch / token; agent falls back to the default repo when no row exists
- **DB migration 003**: adds `git_commits`, `git_integrations`, and `git_sync_state` tables
- **Helm chart**: adds `gitopsAgent` deployment, PVC, and secret templates; chart bumped to 0.3.0
- **Domains UI**: Domains section with Add Domain modal on the app detail page (frontend)
- **PublicApi**: `CreatePublicApi` typed action with backend API handler, git renderer, and status simulation

### Fixes
- **CI**: packages:write permission for ghcr.io push; GHCR_TOKEN PAT; consolidate docker push to Jenkins
- **helm**: `Recreate` strategy on backend deployment for `ReadWriteOnce` PVC
- **renderer**: align ServiceDatabase manifest with updated XRD fields
- **frontend**: async state management and cross-tab auth sync improvements
- **api**: `scanOperation` helper used consistently in all operation endpoints
- **image validation**: allow uppercase registry hosts and port numbers; cap replicas at 10

### Refactor
- **backend**: remove `internal/gitwriter` and `internal/worker` packages — git operations fully delegated to gitops-agent
- **backend config**: remove stale `GIT_STATE_REPO_PATH`, `GIT_BOT_NAME`, `GIT_BOT_EMAIL` env vars
- **helm values**: remove stale `GIT_*` backend env vars; remove git-state volume mount from backend deployment

### Documentation
- README updated for v2 (apps API, second vertical slice, typed actions)
- ADR-001–005 accepted; ADR-006 added (v1 complete); v2 implementation plan added

---

## 0.1.0 - 2026-05-06

Initial MVP release. See tag `dada-console-v0.1-mvp`.

### Features
- GitOps-backed self-service console (backend, frontend, Helm chart)
- ServiceDatabase provisioning via `CreateServiceDatabase` operation
- Helm chart with backend, frontend, ingress, RBAC, migration job
- CI pipeline: GitHub Actions + Jenkins with GHCR image push
- Developer workflow: docker-compose, hot-reload, pgAdmin
