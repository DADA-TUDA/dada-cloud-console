# DADA Cloud Console

GitOps-backed self-service cloud console for managing Kubernetes platform resources.

## Architecture

```
UI → Backend → DB → Worker Job → Git desired state → Argo CD → K8s CRD → Controllers → Status back to UI
```

## Quick Start

### Prerequisites

- Docker + Docker Compose
- Go 1.22+
- Node.js 20+

### 1. Initialize dev environment

```bash
make dev-init
```

This starts PostgreSQL, runs DB migrations (on first backend start), and initializes the local GitOps state repository at `/tmp/dada-state-repo`.

### 2. Start backend

```bash
make dev-backend
```

Backend runs at http://localhost:8080. On first start, it applies DB migrations and seeds dev data.

### 3. Start frontend

In a new terminal:

```bash
make dev-frontend
```

Frontend runs at http://localhost:3000.

### 4. Log in

Open http://localhost:3000 and log in with:

| Username | Password | Role |
|----------|----------|------|
| admin | admin | Platform Admin |
| alex | admin | Developer |
| client | admin | Client Admin |

### First Vertical Slice

Try the full flow:

1. Log in as `alex` (developer)
2. Select project **DADA Internal**
3. Go to **Databases**
4. Select environment `prod`
5. Click **Create Database**
6. Fill in: name=`codex-lb-db`, database=`codexlb`, app=`codex-lb`, backup enabled
7. Watch the **Operations** tab — status advances through: Created → Queued → Rendering → CommittingToGit → Committed → WaitingForArgoSync → Syncing → Reconciling → **Ready**
8. Check the GitOps repo: `ls /tmp/dada-state-repo/clusters/beget-prod/projects/internal/`

## API

Backend API at http://localhost:8080/api/v1

| Method | Path | Description |
|--------|------|-------------|
| POST | /api/v1/auth/login | Login |
| GET | /api/v1/auth/me | Current user |
| GET | /api/v1/projects | List projects |
| GET | /api/v1/projects/:id | Project details |
| GET | /api/v1/projects/:id/environments/:envId/databases | List databases |
| POST | /api/v1/projects/:id/environments/:envId/databases | Create database |
| GET | /api/v1/projects/:id/operations | List operations |
| GET | /api/v1/projects/:id/operations/:opId | Get operation |
| POST | /api/v1/projects/:id/operations/:opId/retry | Retry failed op |

## Project structure

```
dada-cloud/
  backend/         # Go API + Worker
    cmd/server/    # Entry point
    internal/
      api/         # HTTP handlers
      auth/        # JWT
      config/      # Config from env
      db/          # DB connection + migrations
      gitwriter/   # YAML renderer + Git commit
      models/      # Data models
      worker/      # Async operation processor
    migrations/    # SQL migrations
  frontend/        # Next.js 16 app
    app/           # App Router pages
    components/    # UI components
    lib/           # API client, auth, types
  scripts/         # Dev helper scripts
  docker-compose.yml
  Makefile
```

## Roadmap

- **v1** (current): Foundation — ServiceDatabase creation, GitOps commits, status tracking
- **v2**: App lifecycle, domains, Redis, RabbitMQ, restore requests
- **v3**: Multi-tenant clients, quotas, plans, approval center
- **v4**: Marketplace, object storage, AI workers, observability, cost tracking
