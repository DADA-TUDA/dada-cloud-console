# VM Track Design — AppServer + Unified App Lifecycle

**Date:** 2026-05-14  
**Status:** Approved  
**Related ADR:** ADR-007 (Portainer Edge Agent as Remote Docker Runtime Layer)

---

## 1. Problem Statement

v1 deploys Apps exclusively to Kubernetes via GitOps (gitops-agent → Git → ArgoCD → K8s CRDs).  
v2 adds a parallel **VM track**: customer workloads run as Docker Compose stacks on dedicated VDS instances, managed by Portainer Edge Agent.

The user-facing model must be **unified**: an `App` is an `App` regardless of whether it runs in K8s or on a VM. Only the infrastructure backing it differs.

---

## 2. Architecture

### Two execution tracks, one App interface

```
┌──────────────────────────────────────────────────────────────────────┐
│                    console-backend (Go/Gin)                          │
│                                                                      │
│  /app-servers          /environments/{id}/apps        /apps/{name}   │
│       ↓                         ↓                          ↓         │
│              operations table (postgres) — shared                    │
└──────────────────────┬───────────────────────────────────────────────┘
                       │
         ┌─────────────┴──────────────┐
         │                            │
         ▼                            ▼
┌─────────────────┐         ┌──────────────────────┐
│  gitops-agent   │         │   portainer-agent     │
│  (k8s track)   │         │   (vm track)           │
│                 │         │                        │
│  claims where   │         │  claims where          │
│  env.runtime=k8s│         │  env.runtime=vm        │
│  OR no env      │         │  OR action in          │
│                 │         │  (CreateAppServer,      │
│  → render YAML  │         │   DeleteAppServer)      │
│  → git commit   │         │                        │
│  → ArgoCD       │         │  → terraform exec      │
└─────────────────┘         │  → portainer REST API  │
         ↓                  │  → compose render      │
    clusters/…/             │  → git commit          │
    app.yaml                └──────────────────────┘
                                       ↓
                               vm-servers/{proj}/{env}/
                               {server}/{app}/
                               docker-compose.yml
                                       ↓
                               Portainer CE API
                                       ↓
                               Edge Agent on VM
                                       ↓
                               Docker Compose stack
```

### Dispatch rules

**gitops-agent** — query unchanged, gains one extra filter:
```sql
SELECT o.* FROM operations o
LEFT JOIN environments e ON e.id = o.environment_id
WHERE o.status = 'Queued'
  AND (e.runtime = 'k8s' OR o.environment_id IS NULL)
  AND o.action NOT IN ('CreateAppServer', 'DeleteAppServer')
FOR UPDATE SKIP LOCKED LIMIT 1
```

**portainer-agent** — new query:
```sql
SELECT o.* FROM operations o
LEFT JOIN environments e ON e.id = o.environment_id
WHERE o.status = 'Queued'
  AND (
    o.action IN ('CreateAppServer', 'DeleteAppServer')
    OR e.runtime = 'vm'
  )
FOR UPDATE SKIP LOCKED LIMIT 1
```

---

## 3. Database — Migration 004

### 3.1 New table: `app_servers` *(must be created before environments is altered)*

Stores VM + Portainer infrastructure state. One row per provisioned VDS.

```sql
CREATE TABLE IF NOT EXISTS app_servers (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id            UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name                  VARCHAR(255) NOT NULL,
    -- VM state (populated after terraform apply)
    vm_ip                 VARCHAR(45),
    vm_provider_id        VARCHAR(255),      -- OpenStack instance UUID
    terraform_workspace   VARCHAR(500),      -- abs path on agent PVC, e.g. /tf-workspaces/{id}
    -- Portainer state (populated after edge agent connects)
    portainer_endpoint_id INTEGER,
    -- Lifecycle
    status  VARCHAR(50) NOT NULL DEFAULT 'Provisioning'
            CHECK (status IN ('Provisioning','WaitingForAgent','Ready',
                              'Deleting','Deleted','Failed')),
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(project_id, name)
);

CREATE INDEX IF NOT EXISTS idx_app_servers_project ON app_servers(project_id);
```

### 3.2 Extend `environments` *(after app_servers exists)*

```sql
-- runtime: which execution track backs this environment
ALTER TABLE environments
  ADD COLUMN runtime VARCHAR(20) NOT NULL DEFAULT 'k8s'
      CHECK (runtime IN ('k8s', 'vm'));

-- app_server_id: FK to the VM backing a 'vm' environment (nullable for k8s)
ALTER TABLE environments
  ADD COLUMN app_server_id UUID REFERENCES app_servers(id) ON DELETE SET NULL;
```

### 3.3 `resource_snapshots` — unified App state

No new table. Both K8s Apps and VM Apps use `resource_snapshots` with `kind = 'App'`.  
VM-specific fields go into `summary_json`:

```jsonc
// summary_json for a VM-track App
{
  "runtime": "vm",
  "app_server_id": "uuid",
  "app_server_name": "client-a-prod-1",
  "image": "ghcr.io/client/api:1.2.3",
  "port": 8080,
  "env_vars": { "DATABASE_URL": "..." },
  "portainer_stack_id": 42,
  "compose_git_path": "vm-servers/client-a/prod/client-a-prod-1/api/docker-compose.yml",
  "compose_git_sha": "abc123",
  "container_status": "running"
}
```

### 3.4 New Operation payload structs (Go)

```go
// CreateAppServerPayload — provision VDS + register in Portainer
type CreateAppServerPayload struct {
    Name       string `json:"name"`          // "client-a-prod-1"
    Flavor     string `json:"flavor"`        // "1vcpu-2gb" | "2vcpu-4gb" | "4vcpu-8gb"
    OSImage    string `json:"os_image"`      // "ubuntu-22.04"
    Region     string `json:"region"`        // "ru-1"
    SSHKeyName string `json:"ssh_key_name"`  // keypair name in OpenStack project
}

// DeleteAppServerPayload — terraform destroy + Portainer endpoint delete
type DeleteAppServerPayload struct {
    AppServerName string `json:"app_server_name"`
}

// CreateAppPayload (existing, extended with optional VM fields)
type CreateAppPayload struct {
    Name          string            `json:"name"`
    Image         string            `json:"image"`
    Port          int               `json:"port"`
    Replicas      int               `json:"replicas,omitempty"` // k8s only
    Profile       string            `json:"profile,omitempty"`  // k8s only
    AppServerName string            `json:"app_server_name,omitempty"` // vm only
    EnvVars       map[string]string `json:"env_vars,omitempty"`        // vm only
}

// DeployImageVersionPayload (existing, unchanged — works for both tracks)
type DeployImageVersionPayload struct {
    AppName string `json:"app_name"`
    Image   string `json:"image"`
}

// UpdateAppEnvVarsPayload — vm track only
type UpdateAppEnvVarsPayload struct {
    AppName string            `json:"app_name"`
    EnvVars map[string]string `json:"env_vars"`
}
```

---

## 4. Operation Status Flows

### CreateAppServer
```
Created
  → Queued            (backend API creates, portainer-agent advances)
  → ProvisioningVM    (terraform init + apply; writes vm_ip, vm_provider_id to app_servers)
  → WaitingForAgent   (polls GET /api/endpoints?name={server_name} every 10s, timeout 10min;
                        endpoint appears in Portainer when edge agent first connects)
  → Ready             (endpoint found with Status=1; portainer_endpoint_id written to app_servers)
  → Failed            (terraform error OR agent timeout)
```

Note: The global `PORTAINER_EDGE_KEY` encodes the Portainer URL + credentials and is
pre-configured once per Portainer instance. Each VM uses the same key but a unique
`EDGE_ID = server_name`. Portainer auto-creates the endpoint entry when the agent connects.

### DeleteAppServer
```
Created
  → Queued
  → DeletingStacks    (Portainer: delete all stacks on endpoint)
  → DeletingVM        (terraform destroy; removes workspace dir)
  → Deleted           (marks app_servers.status = 'Deleted')
  → Failed
```

### CreateApp (vm track)
```
Created
  → Queued
  → RenderingCompose  (portainer-agent renders docker-compose.yml)
  → CommittingCompose (git commit + push to vm-servers/... path)
  → DeployingStack    (POST /api/stacks → Portainer pulls compose from git)
  → Ready             (stack running; upsert resource_snapshots)
  → Failed
```

### DeployImageVersion / UpdateAppEnvVars (vm track)
```
Created
  → Queued
  → RenderingCompose  (re-render with new image or env_vars)
  → CommittingCompose (git commit + push)
  → UpdatingStack     (PUT /api/stacks/{id} → Portainer redeploys)
  → Ready
  → Failed
```

### CreateApp (k8s track) — unchanged
```
Created → Queued → Rendering → CommittingToGit → Committed → WaitingForArgoSync → Ready
```

---

## 5. `portainer-agent` Service

### Directory layout

```
portainer-agent/
  cmd/portainer-agent/
    main.go              — signal handling, wiring
  internal/
    config/
      config.go          — env var loading
    db/
      pool.go            — pgx connect
      operations.go      — claim/update/fail operations
      appservers.go      — CRUD for app_servers table
      snapshots.go       — upsert resource_snapshots (shared pattern with gitops-agent)
    terraform/
      executor.go        — wraps hashicorp/terraform-exec; manages workspaces
      templates/
        main.tf.tmpl     — OpenStack instance + floating IP
        variables.tf     — vars passed per AppServer
        cloud-init.yaml.tmpl — installs docker + edge agent
    portainer/
      client.go          — thin HTTP client over Portainer REST API
      models.go          — Endpoint, Stack, Container response structs
    compose/
      renderer.go        — renders docker-compose.yml from App spec
    worker/
      vm_watcher.go      — DB poll loop: claims + dispatches VM operations
      status_watcher.go  — polls Portainer every 30s: syncs container status → resource_snapshots
    server/
      server.go          — GET /healthz
  Dockerfile
  go.mod
```

### Config (env vars)

```
# Postgres (same DB as backend and gitops-agent)
DATABASE_URL

# Portainer CE
PORTAINER_URL          — https://portainer.internal.dada-tuda.ru
PORTAINER_API_TOKEN    — Portainer API token (admin service account)

# OpenStack (Beget)
OS_AUTH_URL
OS_TENANT_NAME
OS_USERNAME
OS_PASSWORD
OS_REGION_NAME

# Terraform
TF_WORKSPACE_BASE      — /var/lib/tf-workspaces   (PVC-backed)
TF_STATE_CONN_STR      — postgres DSN for terraform pg backend

# Git (same repo as gitops-agent, different path prefix)
GITOPS_DEFAULT_REPO_URL
GITOPS_DEFAULT_BRANCH
GITOPS_DEFAULT_USERNAME
GITOPS_DEFAULT_TOKEN
GITOPS_REPO_LOCAL_PATH — /var/lib/gitops-repos (shared PVC or separate)
GITOPS_BOT_NAME
GITOPS_BOT_EMAIL

# Poll intervals
VM_POLL_INTERVAL_DB     — 5s
VM_POLL_INTERVAL_STATUS — 30s

# Edge agent registration
PORTAINER_EDGE_KEY     — from Portainer CE "Add Edge Environment" page
```

---

## 6. Terraform Template

### `main.tf.tmpl`

```hcl
terraform {
  required_providers {
    openstack = {
      source  = "terraform-provider-openstack/openstack"
      version = "~> 1.54"
    }
  }
  backend "pg" {}   # conn_str passed via: terraform init -backend-config="conn_str=$TF_STATE_CONN_STR"
                    # schema_name is set per-workspace to isolate state: schema_{app_server_id}
}

provider "openstack" {
  auth_url    = var.os_auth_url
  tenant_name = var.os_tenant_name
  user_name   = var.os_username
  password    = var.os_password
  region      = var.os_region
}

resource "openstack_compute_instance_v2" "app_server" {
  name            = var.server_name
  image_name      = var.os_image          # "ubuntu-22.04"
  flavor_name     = var.flavor            # "1vcpu-2gb"
  key_pair        = var.ssh_key_name
  security_groups = ["default", "dada-app-server"]

  network { name = var.network_name }

  user_data = templatefile("${path.module}/cloud-init.yaml", {
    portainer_edge_key = var.portainer_edge_key
    edge_id            = var.server_name
  })
}

resource "openstack_networking_floatingip_v2" "fip" {
  pool = "external"
}

resource "openstack_compute_floatingip_associate_v2" "fip" {
  floating_ip = openstack_networking_floatingip_v2.fip.address
  instance_id = openstack_compute_instance_v2.app_server.id
}

output "vm_ip"  { value = openstack_networking_floatingip_v2.fip.address }
output "vm_id"  { value = openstack_compute_instance_v2.app_server.id }
```

### `cloud-init.yaml.tmpl`

```yaml
#cloud-config
package_update: true
package_upgrade: false
packages:
  - docker.io
  - docker-compose-plugin
  - curl

runcmd:
  - systemctl enable docker --now
  - |
    docker run -d \
      --name portainer_edge_agent \
      --restart always \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -v /var/lib/docker/volumes:/var/lib/docker/volumes \
      -e EDGE=1 \
      -e EDGE_ID=${edge_id} \
      -e EDGE_KEY=${portainer_edge_key} \
      -e AGENT_SECRET="" \
      portainer/agent:2.21.5
```

---

## 7. Portainer Client — Key Operations

```go
// client.go — key methods used by portainer-agent

// ListEndpoints — find edge env by name after agent connects
GET  /api/endpoints?name={name}

// GetEndpoint — poll until Status == 1 (connected)
GET  /api/endpoints/{id}

// DeleteEndpoint
DELETE /api/endpoints/{id}

// CreateStack — deploy compose from git
POST /api/stacks?type=2&method=repository&endpointId={id}
Body: {
  "name": "{app_name}",
  "repositoryURL": "https://github.com/…/argo-infra",
  "repositoryReferenceName": "refs/heads/main",
  "composeFile": "vm-servers/{proj}/{env}/{server}/{app}/docker-compose.yml",
  "repositoryAuthentication": true,
  "repositoryUsername": "…",
  "repositoryPassword": "…"
}

// UpdateStack — redeploy after compose file changes
PUT /api/stacks/{id}?endpointId={id}
Body: { "pullImage": true, "prune": false }

// DeleteStack
DELETE /api/stacks/{id}?endpointId={id}

// ListContainers — for status_watcher
GET /api/endpoints/{id}/docker/containers/json?all=true

// GetLogs — streaming logs
GET /api/endpoints/{id}/docker/containers/{containerId}/logs
    ?stdout=1&stderr=1&follow=1&tail=100
```

---

## 8. Docker Compose Renderer

### Git path convention

```
vm-servers/{project-name}/{env-name}/{server-name}/{app-name}/docker-compose.yml
```

Example:
```
vm-servers/client-a/prod/client-a-prod-1/api/docker-compose.yml
```

### Rendered compose template

```yaml
# Generated by DADA Cloud Console — DO NOT EDIT MANUALLY
# App: {{.AppName}} | Server: {{.ServerName}} | Generated: {{.Timestamp}}
version: "3.9"

services:
  {{.AppName}}:
    image: {{.Image}}
    restart: unless-stopped
    ports:
      - "{{.Port}}:{{.Port}}"
    environment:
{{- range $k, $v := .EnvVars}}
      {{$k}}: "{{$v}}"
{{- end}}
    logging:
      driver: "json-file"
      options:
        max-size: "50m"
        max-file: "3"
    labels:
      dada.io/project: "{{.ProjectName}}"
      dada.io/app: "{{.AppName}}"
      dada.io/managed: "true"
```

---

## 9. Backend API — New Endpoints

### AppServer endpoints

```
POST   /api/v1/projects/{projectId}/app-servers
         Body: { name, flavor, os_image, region, ssh_key_name }
         → Creates CreateAppServer operation

GET    /api/v1/projects/{projectId}/app-servers
         → Lists app_servers for project

GET    /api/v1/projects/{projectId}/app-servers/{serverName}
         → Single AppServer with status

DELETE /api/v1/projects/{projectId}/app-servers/{serverName}
         → Creates DeleteAppServer operation
```

### App endpoints — extended, not replaced

```
POST   /api/v1/projects/{projectId}/environments/{envId}/apps
         Existing endpoint — behaviour changes for vm environments:
         - if env.runtime == 'vm': requires app_server_name + env_vars in body
         - if env.runtime == 'k8s': existing behaviour (replicas, profile)

PATCH  /api/v1/projects/{projectId}/environments/{envId}/apps/{appName}/image
         Existing — works for both tracks

PATCH  /api/v1/projects/{projectId}/environments/{envId}/apps/{appName}/env-vars
         New endpoint — vm track only
         Body: { env_vars: { KEY: "value" } }

GET    /api/v1/projects/{projectId}/environments/{envId}/apps/{appName}/logs
         New endpoint — proxies Portainer log stream for vm track
         K8s track: TBD (future — kubectl logs proxy)
```

---

## 10. Frontend Pages

### New: `/projects/{id}/app-servers`

- List of AppServers with status chips (Provisioning / WaitingForAgent / Ready / Failed)
- "Create App Server" modal: flavor selector, region, SSH key
- Row actions: Delete

### Updated: `/projects/{id}/apps` and `/projects/{id}/apps/{name}`

- App list and detail pages already exist
- Add: `runtime` badge ("K8s" | "VM") on app cards
- Add: "Logs" tab on app detail (streams `/api/.../logs`)
- Add: "Env Vars" section on app detail (vm only) — edit + save → UpdateAppEnvVars operation
- Image update — already exists, works for both tracks

### New: Environment creation flow (updated)

- When creating environment: choose `Runtime: Kubernetes | VM`
- If VM: select existing AppServer from dropdown
- Writes `environments.runtime = 'vm'` + `app_server_id`

---

## 11. Logs Streaming

Backend acts as a proxy for Portainer log streaming:

```
Frontend (EventSource / WebSocket)
    ↓
GET /api/v1/projects/{id}/environments/{envId}/apps/{appName}/logs?follow=true
    ↓
backend: resolves portainer_endpoint_id + portainer_stack_id from resource_snapshots.summary_json
    ↓
GET https://portainer/api/endpoints/{eid}/docker/containers/{cid}/logs?follow=1
    ↓
backend streams response back (chunked transfer)
```

Container ID resolved by: `GET /api/endpoints/{eid}/docker/containers/json?filters={"label":["dada.io/app={appName}"]}`

---

## 12. Task Sequence (Implementation Order)

### Phase 1 — Foundation
1. Migration 004: `runtime` + `app_server_id` on `environments`; `app_servers` table
2. New operation payload structs in `backend/internal/models/operation.go`
3. `portainer-agent/` scaffold: module, main.go, config, db pool
4. Portainer CE deployed in dada-cloud cluster (helm chart, service account token)

### Phase 2 — AppServer lifecycle
5. `portainer-agent/internal/terraform/executor.go` — workspace init, apply, destroy
6. Terraform templates: `main.tf.tmpl`, `variables.tf`, `cloud-init.yaml.tmpl`
7. `portainer-agent/internal/portainer/client.go` — endpoint CRUD
8. `portainer-agent/internal/worker/vm_watcher.go` — CreateAppServer flow
9. `portainer-agent/internal/worker/vm_watcher.go` — DeleteAppServer flow
10. Backend API handlers: `appservers.go` (list, create, delete)

### Phase 3 — App lifecycle on VM
11. `portainer-agent/internal/compose/renderer.go`
12. Git commit logic (reuse pattern from gitops-agent)
13. `portainer-agent/internal/portainer/client.go` — stack CRUD
14. vm_watcher: CreateApp (vm) flow
15. vm_watcher: DeployImageVersion (vm) + UpdateAppEnvVars flows
16. Backend: extend `CreateApp` handler for vm environments
17. Backend: `UpdateAppEnvVars` endpoint (new)
18. gitops-agent: add `env.runtime = 'k8s'` filter to claim query

### Phase 4 — Status + Logs
19. `portainer-agent/internal/worker/status_watcher.go` — polls containers, syncs resource_snapshots
20. Backend: `GET .../logs` proxy endpoint
21. Frontend: Logs tab with SSE stream

### Phase 5 — Frontend
22. Environment creation: runtime selector + AppServer picker
23. App list/detail: runtime badge, env vars section
24. AppServer list + create/delete UI
25. Image update UI (already exists, verify works for vm track)

### Phase 6 — Infra + CI
26. Portainer-agent Dockerfile (includes terraform binary)
27. Helm chart: `portainerAgent` deployment + PVC (tf-workspaces, gitops-repos)
28. Jenkins pipeline: build + push `portainer-agent` image
29. ADR-007 status → Accepted

---

## 13. Open Questions (Resolved)

| Question | Answer |
|---|---|
| VM track parallel or replaces K8s? | Parallel — env.runtime discriminates |
| Where does Portainer executor live? | Separate `portainer-agent` service |
| VM provisioning mechanism? | Terraform + OpenStack provider (Beget) |
| app_deployments as separate concept? | No — unified as `App` in resource_snapshots |
| MVP scope? | All features: create/delete server, create/update/delete app, logs |
