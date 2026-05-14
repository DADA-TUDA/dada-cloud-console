# VM Track Design — AppServer + Unified App Lifecycle

**Date:** 2026-05-14  
**Status:** Approved  
**Related ADR:** ADR-007 (Portainer Edge Agent as Remote Docker Runtime Layer)

---

## 1. Problem Statement

v1 deploys Apps exclusively to Kubernetes via GitOps (gitops-agent → Git → ArgoCD → K8s CRDs).  
v2 adds a parallel **VM track**: customer workloads run as Docker Compose stacks on dedicated VDS
instances (Beget Cloud), managed remotely via Portainer CE + Edge Agent.

The user-facing model is **unified**: an `App` is an `App` regardless of runtime. Only the
infrastructure backing the environment differs.

---

## 2. Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│                    console-backend (Go/Gin)                          │
│  /app-servers    /environments/{id}/apps    /apps/{name}/logs        │
│                    operations table (postgres) — shared              │
└──────────────────┬───────────────────────────────────────────────────┘
                   │
     ┌─────────────┴──────────────┐
     │                            │
     ▼                            ▼
┌──────────────┐         ┌────────────────────────┐
│ gitops-agent │         │    portainer-agent      │
│ (k8s track)  │         │    (vm track)           │
│              │         │                         │
│ env.runtime  │         │ env.runtime=vm           │
│ = k8s        │         │ OR action in             │
│              │         │ (CreateAppServer,         │
│ → YAML       │         │  DeleteAppServer)         │
│ → Git        │         │                         │
│ → ArgoCD     │         │  terraform executor      │
└──────────────┘         │  portainer REST client   │
       ↓                 │  compose renderer        │
  clusters/…/            │  git commit              │
  app.yaml               └────────────────────────┘
                                    │
                    ┌───────────────┼───────────────┐
                    ▼               ▼               ▼
             Portainer CE     vm-servers/…/    Terraform state
             REST API         docker-compose   (postgres backend)
                    │
             Edge Agent
             (on each VM)
                    │
             Docker Engine
             + Compose stack
             + node_exporter
             + cAdvisor
             + filebeat
```

### Dispatch SQL rules

**gitops-agent** (unchanged + one extra filter):
```sql
SELECT o.* FROM operations o
LEFT JOIN environments e ON e.id = o.environment_id
WHERE o.status = 'Queued'
  AND (e.runtime = 'k8s' OR o.environment_id IS NULL)
  AND o.action NOT IN ('CreateAppServer', 'DeleteAppServer')
FOR UPDATE SKIP LOCKED LIMIT 1
```

**portainer-agent**:
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

```sql
-- ① Create app_servers FIRST (environments will FK to it)
CREATE TABLE IF NOT EXISTS app_servers (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id            UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name                  VARCHAR(255) NOT NULL,
    -- VM state (populated after terraform apply)
    vm_ip                 VARCHAR(45),
    vm_provider_id        VARCHAR(255),      -- OpenStack instance UUID
    terraform_workspace   VARCHAR(500),      -- /var/lib/tf-workspaces/{id}
    -- Portainer state
    portainer_endpoint_id INTEGER,           -- Portainer env ID, populated after agent connects
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

-- ② Extend environments
ALTER TABLE environments
  ADD COLUMN IF NOT EXISTS runtime VARCHAR(20) NOT NULL DEFAULT 'k8s'
      CHECK (runtime IN ('k8s', 'vm'));
ALTER TABLE environments
  ADD COLUMN IF NOT EXISTS app_server_id UUID REFERENCES app_servers(id) ON DELETE SET NULL;
```

### New Operation payload structs

```go
// models/operation.go additions

type CreateAppServerPayload struct {
    Name       string `json:"name"`           // "client-a-prod-1"
    Flavor     string `json:"flavor"`         // "1vcpu-2gb" | "2vcpu-4gb" | "4vcpu-8gb"
    OSImage    string `json:"os_image"`       // "ubuntu-22.04"
    Region     string `json:"region"`         // "ru-1"
    SSHKeyName string `json:"ssh_key_name"`   // keypair name registered in OpenStack
}

type DeleteAppServerPayload struct {
    AppServerName string `json:"app_server_name"`
}

// CreateAppPayload extended (k8s fields remain, vm fields are additive)
type CreateAppPayload struct {
    Name          string            `json:"name"`
    Image         string            `json:"image"`
    Port          int               `json:"port"`
    Replicas      int               `json:"replicas,omitempty"`      // k8s only
    Profile       string            `json:"profile,omitempty"`       // k8s only
    AppServerName string            `json:"app_server_name,omitempty"` // vm only
    EnvVars       map[string]string `json:"env_vars,omitempty"`        // vm only
}

// Existing, unchanged — works for both tracks
type DeployImageVersionPayload struct {
    AppName string `json:"app_name"`
    Image   string `json:"image"`
}

// New — vm track only
type UpdateAppEnvVarsPayload struct {
    AppName string            `json:"app_name"`
    EnvVars map[string]string `json:"env_vars"`
}
```

### resource_snapshots.summary_json schema (VM App)

```jsonc
{
  "runtime": "vm",
  "app_server_id": "uuid",
  "app_server_name": "client-a-prod-1",
  "image": "ghcr.io/client/api:1.2.3",
  "port": 8080,
  "env_vars": {},                          // NOT stored here — sensitive. See §3 note.
  "portainer_stack_id": 42,
  "compose_git_path": "vm-servers/client-a/prod/client-a-prod-1/api/docker-compose.yml",
  "compose_git_sha": "abc123",
  "container_status": "running",           // running | stopped | error
  "container_id": "sha256:abc...",
  "cpu_percent": 12.4,                     // updated by status_watcher
  "mem_mb": 256
}
```

> **Security note:** env_vars are never stored in resource_snapshots (may contain secrets).
> They live only in `operations.payload` (encrypted at rest) and in the rendered
> docker-compose.yml in the git repo (which should be private / access-controlled).

---

## 4. Operation Status Flows

### CreateAppServer
```
Created → Queued
       → ProvisioningVM      portainer-agent: POST /api/endpoints → get EdgeKey + endpoint_id
                              terraform init + apply (VDS created, cloud-init runs)
                              writes vm_ip + vm_provider_id to app_servers
       → WaitingForAgent      poll GET /api/endpoints/{id} every 10s
                              agent connects when cloud-init finishes (~2-4 min)
                              timeout after 10 min → Failed
       → Ready                portainer_endpoint_id written to app_servers
```

### DeleteAppServer
```
Created → Queued
       → DeletingStacks       Portainer: DELETE /api/stacks/{id}?endpointId={id} for all stacks
       → DeletingVM           terraform destroy (removes OpenStack instance + FIP)
                              rm -rf terraform workspace dir
       → Deleted              app_servers.status = 'Deleted'
```

### CreateApp / UpdateApp (vm track)
```
Created → Queued
       → RenderingCompose     render docker-compose.yml from spec
       → CommittingCompose    git add + commit + push to vm-servers/…
       → DeployingStack       POST /api/stacks (create) or PUT /api/stacks/{id}/git/redeploy
       → Ready                upsert resource_snapshots with portainer_stack_id
```

### CreateApp (k8s track) — unchanged
```
Created → Queued → Rendering → CommittingToGit → Committed → WaitingForArgoSync → Ready
```

---

## 5. Portainer CE — API Reference (source-verified)

> **Verified against:** `portainer/portainer` develop branch + official docs.  
> We use **regular stacks per endpoint** (not Edge Stacks). Edge Stacks broadcast to
> EdgeGroups — not suitable for per-AppServer control. Regular stacks target a specific
> endpoint directly and support git-backed deployment + redeploy API.

### Authentication

```
Header: X-API-Key: ptr_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

Create a dedicated `dada-platform-bot` user in Portainer (admin role).  
Generate token: Account → My account → Access tokens → Add access token.  
**No programmatic token creation API in CE — must be done via UI once.**  
For short automation: `POST /api/auth` returns a JWT (8h expiry). Use API key for services.

### 5.1 Pre-requisite: Enable EnforceEdgeID in Portainer Settings

**Critical:** Go to Portainer → Settings → Edge Compute → enable  
**"Enforce use of Portainer generated Edge ID"** before creating any edge environments via API.  
Without this, `EdgeID` is absent from the creation response and from GET /api/endpoints/{id}
until the agent first connects (GitHub issues #6947, #7807).

### 5.2 Create Edge Environment (before terraform apply)

```http
POST /api/endpoints
Content-Type: multipart/form-data

Name                    = "client-a-prod-1"
EndpointCreationType    = 4                              # 4 = Edge Agent on Docker
ContainerEngine         = "docker"
URL                     = "https://portainer.dada.ru"   # Portainer server URL — NOT agent URL
                                                         # (omitting causes "hostname cannot be empty")
EdgeTunnelServerAddress = "portainer.dada.ru:8000"      # tunnel server addr:port
EdgeCheckinInterval     = 15
GroupID                 = 1
```

**Response** (201):
```json
{
  "Id": 12,
  "Name": "client-a-prod-1",
  "Type": 4,
  "EdgeKey": "aHR0cHM6Ly9wb3J0YWluZXIuZGFkYS5ydXw4LjguOC44OjgwMDB8ZmluZ2VycHJpbnR8MTI=",
  "EdgeID": "550e8400-e29b-41d4-a716-446655440000",
  "Status": 1,
  "LastCheckInDate": 0,
  "Heartbeat": false
}
```

> **EdgeKey decoded:** `https://portainer.dada.ru|portainer.dada.ru:8000|tls_fingerprint|12`  
> Store `Id` → `portainer_endpoint_id`, `EdgeKey` → pass to cloud-init, `EdgeID` → EDGE_ID env var.

### 5.3 Poll Endpoint Connectivity

```http
GET /api/endpoints/{id}
```

**⚠️ `Status: 1` is NOT a connectivity signal** — edge endpoints are created with `Status: 1`
immediately regardless of whether the agent has connected.

Correct detection:
```go
// Agent has ever connected:
connected := endpoint.LastCheckInDate > 0 && endpoint.Heartbeat == true
```

Poll every 10s. Timeout 10 min → mark operation Failed.

```json
{
  "Id": 12,
  "Status": 1,
  "LastCheckInDate": 1716123456,   // > 0 means agent connected at least once
  "Heartbeat": true,               // true = agent checked in within (interval*2 + 20)s
  "EdgeCheckinInterval": 15
}
```

### 5.4 Deploy Stack from Git

> **Breaking change Portainer 2.27+:** Old path `POST /api/stacks?method=repository&type=1`  
> was **removed**. Use the new path below for all versions 2.19+.

```http
POST /api/stacks/create/standalone/repository?endpointId={endpointId}
Content-Type: application/json

{
  "Name": "api",
  "RepositoryURL": "https://github.com/dada-tuda/argo-infra",
  "RepositoryReferenceName": "refs/heads/main",
  "ComposeFile": "vm-servers/client-a/prod/client-a-prod-1/api/docker-compose.yml",
  "RepositoryAuthentication": true,
  "RepositoryUsername": "dada-bot",
  "RepositoryPassword": "ghp_xxxx",
  "TLSSkipVerify": false,
  "Env": [],
  "AutoUpdate": null
}
```

**Response** (200): `{ "Id": 7, "Name": "api", ... }`  
Store `Id` as `portainer_stack_id` in `resource_snapshots.summary_json`.

### 5.5 Redeploy Stack from Git (after compose changes)

```http
PUT /api/stacks/{stackId}/git/redeploy?endpointId={endpointId}
Content-Type: application/json

{
  "pullImage": true,
  "prune": false,
  "RepositoryReferenceName": "refs/heads/main",
  "RepositoryAuthentication": true,
  "RepositoryUsername": "dada-bot",
  "RepositoryPassword": "ghp_xxxx"
}
```

> Portainer pulls the latest git commit and redeploys. This works for regular stacks.
> (Edge Stacks have no git-pull redeploy API in CE — separate subsystem, not used here.)

### 5.6 Delete Stack

```http
DELETE /api/stacks/{stackId}?endpointId={endpointId}
```

### 5.7 Container Logs (streaming)

Step 1 — resolve container ID by compose service label:
```http
GET /api/endpoints/{endpointId}/docker/containers/json
    ?filters={"label":["dada.io/app=api"]}
```

Step 2 — stream logs (HTTP chunked transfer, NOT SSE):
```http
GET /api/endpoints/{endpointId}/docker/containers/{containerId}/logs
    ?stdout=1&stderr=1&follow=1&tail=200&timestamps=1
```

> **Log format:** Docker multiplexes stdout/stderr with an 8-byte header per chunk  
> (1 byte stream type + 3 bytes zero + 4 bytes big-endian length). Parse with  
> `github.com/docker/docker/pkg/stdcopy.StdCopy()` or strip 8-byte header manually.
>
> **Nginx proxy:** must set `proxy_buffering off; proxy_read_timeout 3600s; gzip off;`  
> otherwise streaming stalls.

Backend proxies chunked Docker log stream → frontend receives as `text/event-stream` (SSE).

---

## 6. Terraform — Beget Provider

> **Critical finding:** Beget Cloud does NOT expose OpenStack API. Cannot use
> `terraform-provider-openstack/openstack`. Must use Beget's own provider:
> `tf.beget.com/beget/beget`.  
>
> **Second critical finding:** `beget_compute_instance` has NO `user_data`/cloud-init argument.
> VM bootstrap (Docker, Edge Agent, observability) is done via **SSH provisioning**
> after Terraform creates the instance (see §7).

### Provider config

```hcl
# portainer-agent/internal/terraform/templates/main.tf.tmpl

terraform {
  required_providers {
    beget = {
      source = "tf.beget.com/beget/beget"
      # No version constraints in public registry — pin via lock file
    }
  }

  # State per-AppServer in Postgres
  # Init: terraform init
  #         -backend-config="conn_str=$TF_STATE_CONN_STR"
  #         -backend-config="schema_name=tfstate_${app_server_id}"
  backend "pg" {}
}

provider "beget" {
  token = var.beget_token   # Bearer token from Beget control panel
}
```

### Variables

```hcl
# portainer-agent/internal/terraform/templates/variables.tf

variable "beget_token"    { type = string; sensitive = true }
variable "server_name"    { type = string }   # "client-a-prod-1"
variable "region"         { type = string }   # "ru1" | "ru2" | "kz1" | "eu1"
variable "cpu"            { type = number; default = 2 }
variable "ram_mb"         { type = number; default = 2048 }
variable "disk_mb"        { type = number; default = 20480 }
variable "software_id"    { type = number }   # Ubuntu 22.04 ID from beget_softwares datasource
variable "ssh_key_id"     { type = string }   # ID of pre-registered SSH key in Beget
```

### Resources

```hcl
# Discover available software (images) — use in data pipeline, not per-VM
data "beget_softwares" "all" {}

resource "beget_compute_instance" "app_server" {
  name   = var.server_name
  region = var.region

  cpu     = var.cpu
  ram_mb  = var.ram_mb
  disk_mb = var.disk_mb

  image {
    software {
      id = var.software_id   # Ubuntu 22.04 ID (query once via data source)
    }
  }

  access {
    ssh_keys = [var.ssh_key_id]
  }
}

# Additional public IP if needed (Beget provides one by default)
# resource "beget_additional_ip" "extra" { ... }

output "vm_ip" { value = beget_compute_instance.app_server.ip_address }
output "vm_id" { value = beget_compute_instance.app_server.id }
```

### terraform-exec Go integration

```go
// portainer-agent/internal/terraform/executor.go

import "github.com/hashicorp/terraform-exec/tfexec"  // v0.25.2

type Executor struct {
    terraformBin string   // path to terraform binary in Docker image
    pgConnStr    string   // postgres DSN for state backend
}

func (e *Executor) Apply(ctx context.Context, workspaceDir, workspaceID string, vars map[string]string) (map[string]string, error) {
    tf, err := tfexec.NewTerraform(workspaceDir, e.terraformBin)
    if err != nil { return nil, err }

    if err := tf.Init(ctx,
        tfexec.Backend(true),
        tfexec.BackendConfig("conn_str="+e.pgConnStr),
        tfexec.BackendConfig("schema_name=tfstate_"+workspaceID),
        tfexec.Upgrade(false),
    ); err != nil { return nil, fmt.Errorf("tf init: %w", err) }

    applyOpts := []tfexec.ApplyOption{tfexec.Lock(true)}
    for k, v := range vars {
        applyOpts = append(applyOpts, tfexec.Var(k+"="+v))
    }
    if err := tf.Apply(ctx, applyOpts...); err != nil {
        return nil, fmt.Errorf("tf apply: %w", err)
    }

    return e.Output(ctx, workspaceDir)
}

func (e *Executor) Destroy(ctx context.Context, workspaceDir, workspaceID string, vars map[string]string) error {
    tf, err := tfexec.NewTerraform(workspaceDir, e.terraformBin)
    if err != nil { return err }
    if err := tf.Init(ctx,
        tfexec.BackendConfig("conn_str="+e.pgConnStr),
        tfexec.BackendConfig("schema_name=tfstate_"+workspaceID),
    ); err != nil { return err }
    destroyOpts := []tfexec.DestroyOption{}
    for k, v := range vars {
        destroyOpts = append(destroyOpts, tfexec.Var(k+"="+v))
    }
    return tf.Destroy(ctx, destroyOpts...)
}

func (e *Executor) Output(ctx context.Context, workspaceDir string) (map[string]string, error) {
    tf, _ := tfexec.NewTerraform(workspaceDir, e.terraformBin)
    out, err := tf.Output(ctx)
    if err != nil { return nil, err }
    result := make(map[string]string, len(out))
    for k, v := range out {
        result[k] = strings.Trim(string(v.Value), `"`)
    }
    return result, nil
}
```

> **Dockerfile:** multi-stage build — copy terraform binary from `hashicorp/terraform:1.9`.
>
> **PG backend isolation:** each AppServer gets `schema_name = "tfstate_{app_server_uuid}"`.
> Postgres creates the schema + `states` table automatically on first `terraform init`.

### Beget Software ID discovery (run once, hardcode)

```bash
# Using Beget Terraform data source to find Ubuntu 22.04 software ID
terraform console
> data.beget_softwares.all.softwares
# Find the entry where name contains "Ubuntu 22.04"
# Hardcode that ID in config (it won't change)
```

### Beget regions

| Region | Location |
|---|---|
| `ru1` | Russia, St. Petersburg |
| `ru2` | Russia, Moscow |
| `kz1` | Kazakhstan, Astana |
| `eu1` | Europe, Riga (Latvia) |

---

## 7. SSH Bootstrap — Complete VM Setup

> **Why SSH instead of cloud-init:** Beget's Terraform provider has no `user_data` argument.
> After `terraform apply` completes and `vm_ip` is known, portainer-agent SSHes into the VM
> and runs a bootstrap script. The deployment SSH key is pre-registered in Beget and passed
> as `ssh_key_id` to the Terraform resource.

### Bootstrap flow in portainer-agent

```
terraform apply → vm_ip obtained
    ↓
wait ~30s for SSH port to open (retry with backoff)
    ↓
SSH connect (key from AGENT_SSH_PRIVATE_KEY env)
    ↓
upload bootstrap.sh via SFTP (or heredoc over stdin)
    ↓
execute: sudo bash /tmp/bootstrap.sh
    ↓
monitor stdout for "BOOTSTRAP_COMPLETE" marker
    ↓
continue to WaitingForAgent phase
```

### Go SSH client

```go
// portainer-agent/internal/ssh/client.go
import "golang.org/x/crypto/ssh"

func RunBootstrap(ctx context.Context, host, user, privateKeyPEM string, script string) error {
    signer, err := ssh.ParsePrivateKey([]byte(privateKeyPEM))
    config := &ssh.ClientConfig{
        User: user,   // "root" on Beget Ubuntu VDS
        Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
        HostKeyCallback: ssh.InsecureIgnoreHostKey(), // acceptable for provisioning
        Timeout: 10 * time.Second,
    }

    // Retry until SSH is ready (VM may still be booting)
    var client *ssh.Client
    for i := 0; i < 30; i++ {
        client, err = ssh.Dial("tcp", host+":22", config)
        if err == nil { break }
        select { case <-ctx.Done(): return ctx.Err()
                 case <-time.After(10*time.Second): }
    }
    if err != nil { return fmt.Errorf("ssh connect after retries: %w", err) }
    defer client.Close()

    session, _ := client.NewSession()
    defer session.Close()

    // Stream output for logging
    session.Stdout = os.Stdout
    session.Stderr = os.Stderr

    return session.Run("bash -s") // script piped via Stdin
}
```

### `bootstrap.sh` — complete VM setup script

```bash
#!/usr/bin/env bash
# portainer-agent/internal/ssh/bootstrap.sh.tmpl
# Rendered by portainer-agent Go text/template, piped via SSH stdin
# Template vars: {{.ServerName}} {{.EdgeKey}} {{.EdgeID}}
#                {{.PrometheusRemoteWriteURL}} {{.PrometheusUser}} {{.PrometheusPass}}
#                {{.ElasticsearchURL}} {{.ElasticsearchAPIKey}}
set -euo pipefail

echo "[bootstrap] Starting VM setup for {{.ServerName}}"

# ── Docker ───────────────────────────────────────────────────────────────
apt-get update -qq
apt-get install -y -qq docker.io docker-compose-plugin curl wget
systemctl enable docker --now
echo "[bootstrap] Docker ready"

# ── node_exporter 1.8.2 ──────────────────────────────────────────────────
useradd --no-create-home --shell /bin/false node_exporter 2>/dev/null || true
NODE_VER="1.8.2"
wget -q "https://github.com/prometheus/node_exporter/releases/download/v${NODE_VER}/node_exporter-${NODE_VER}.linux-amd64.tar.gz" -O /tmp/ne.tar.gz
tar -xzf /tmp/ne.tar.gz -C /tmp
mv "/tmp/node_exporter-${NODE_VER}.linux-amd64/node_exporter" /usr/local/bin/
chown node_exporter:node_exporter /usr/local/bin/node_exporter
rm -rf /tmp/ne.tar.gz "/tmp/node_exporter-${NODE_VER}.linux-amd64"

cat > /etc/systemd/system/node_exporter.service << 'UNIT'
[Unit]
Description=Prometheus Node Exporter
After=network.target
[Service]
User=node_exporter
ExecStart=/usr/local/bin/node_exporter --collector.systemd --collector.processes
Restart=always
RestartSec=5
[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload && systemctl enable --now node_exporter
echo "[bootstrap] node_exporter :9100 started"

# ── cAdvisor v0.49.1 ─────────────────────────────────────────────────────
docker run -d \
  --name cadvisor --restart unless-stopped \
  -v /:/rootfs:ro -v /var/run:/var/run:ro \
  -v /sys:/sys:ro -v /var/lib/docker/:/var/lib/docker:ro \
  -p 8080:8080 gcr.io/cadvisor/cadvisor:v0.49.1
echo "[bootstrap] cAdvisor :8080 started"

# ── Prometheus Agent → central remote_write ───────────────────────────────
mkdir -p /etc/prometheus-agent /var/lib/prometheus-agent
cat > /etc/prometheus-agent/prometheus.yml << PROMEOF
global:
  scrape_interval: 30s
  external_labels:
    vm_name: "{{.ServerName}}"
    runtime: "vm"
scrape_configs:
  - job_name: node_exporter
    static_configs:
      - targets: ["localhost:9100"]
  - job_name: cadvisor
    static_configs:
      - targets: ["localhost:8080"]
    metric_relabel_configs:
      - source_labels: [container_label_dada_io_app]
        target_label: app
remote_write:
  - url: "{{.PrometheusRemoteWriteURL}}"
    basic_auth:
      username: "{{.PrometheusUser}}"
      password: "{{.PrometheusPass}}"
PROMEOF

docker run -d \
  --name prometheus-agent --restart unless-stopped \
  -v /etc/prometheus-agent/prometheus.yml:/etc/prometheus/prometheus.yml:ro \
  -v /var/lib/prometheus-agent:/prometheus \
  --network host \
  prom/prometheus:v2.53.0 \
  --config.file=/etc/prometheus/prometheus.yml \
  --enable-feature=agent \
  --storage.agent.path=/prometheus
echo "[bootstrap] Prometheus Agent started (remote_write)"

# ── Filebeat 8.13.4 → Elasticsearch ──────────────────────────────────────
mkdir -p /etc/filebeat
cat > /etc/filebeat/filebeat.yml << FBEOF
filebeat.inputs:
  - type: container
    paths:
      - /var/lib/docker/containers/*/*.log
    stream: all
    processors:
      - add_docker_metadata:
          host: "unix:///var/run/docker.sock"
      - drop_fields:
          fields: ["agent", "ecs"]
          ignore_missing: true
output.elasticsearch:
  hosts: ["{{.ElasticsearchURL}}"]
  api_key: "{{.ElasticsearchAPIKey}}"
  index: "dada-vm-logs-%{[container.labels.dada_io_app]:unknown}-%{+yyyy.MM.dd}"
setup.ilm.enabled: false
setup.template.enabled: false
logging.level: warning
FBEOF

docker run -d \
  --name filebeat --restart unless-stopped --user root \
  -v /etc/filebeat/filebeat.yml:/usr/share/filebeat/filebeat.yml:ro \
  -v /var/lib/docker/containers:/var/lib/docker/containers:ro \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  docker.elastic.co/beats/filebeat:8.13.4
echo "[bootstrap] Filebeat started"

# ── Portainer Edge Agent 2.21.0 ──────────────────────────────────────────
# EDGE_ID registered in Portainer via POST /api/endpoints before terraform ran
# EDGE_KEY from POST /api/endpoints response (EdgeKey field)
docker run -d \
  --name portainer_edge_agent --restart=always \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /var/lib/docker/volumes:/var/lib/docker/volumes \
  -v /:/host -v portainer_agent_data:/data \
  -e EDGE=1 \
  -e EDGE_ID="{{.EdgeID}}" \
  -e EDGE_KEY="{{.EdgeKey}}" \
  -e EDGE_INSECURE_POLL=0 \
  portainer/agent:2.21.0
echo "[bootstrap] Portainer Edge Agent started (EDGE_ID={{.EdgeID}})"

echo "BOOTSTRAP_COMPLETE"
```

> **SSH bootstrap notes:**
> - Script rendered with Go `text/template` before piping via `session.Run("bash -s")`.
> - portainer-agent scans stdout for `"BOOTSTRAP_COMPLETE"` to advance to `WaitingForAgent`.
> - SSH retry: 30 × 10s = 5 min max wait for SSH port after VM boot.
> - `EDGE_INSECURE_POLL=0` — change to `1` only if Portainer uses a self-signed TLS cert.
> - `--enable-feature=agent` = Prometheus Agent mode (no local TSDB, only remote_write).

---

## 8. Observability Architecture

### Metrics flow

```
VM
  node_exporter :9100  ─┐
  cAdvisor      :8080  ─┤── Prometheus Agent (remote_write) ──→ central Prometheus (K8s)
                         │                                              ↓
                         └── (direct scrape optional)            Grafana dashboards
```

**Recommended Grafana dashboard IDs:**
| Dashboard | ID |
|---|---|
| Node Exporter Full | 1860 |
| Docker Container Monitoring (cAdvisor) | 19908 |
| Cadvisor Exporter | 14282 |

### Logs flow

```
VM
  Docker containers → JSON log files → Filebeat → Elasticsearch → Kibana
  /var/lib/docker/containers/*/*.log
```

**Filebeat index pattern:** `dada-vm-logs-{app_name}-{date}`

**Elasticsearch API key** — create in Kibana: Stack Management → API Keys → Create.  
Scope: `{ "index": [{ "names": ["dada-vm-logs-*"], "privileges": ["create_doc", "create_index"] }] }`

### Prometheus scrape additions (central cluster)

If using pull-based scrape instead of remote_write (alternative approach):
```yaml
# prometheus/values.yaml addition (kube-prometheus-stack)
additionalScrapeConfigs:
  - job_name: vm_node_exporter
    static_configs:
      - targets:
          - "1.2.3.4:9100"    # written dynamically from app_servers.vm_ip
          - "1.2.3.5:9100"
        labels:
          runtime: vm
    relabel_configs:
      - source_labels: [__address__]
        target_label: instance

  - job_name: vm_cadvisor
    static_configs:
      - targets:
          - "1.2.3.4:8080"
          - "1.2.3.5:8080"
```

> **Recommended:** use **Prometheus Agent + remote_write** on each VM to avoid opening inbound ports
> from the K8s cluster to VMs. The VM pushes metrics out, which also works through NAT.

---

## 9. Docker Compose Renderer

### Git path convention

```
vm-servers/{project-name}/{env-name}/{server-name}/{app-name}/docker-compose.yml
```

### Rendered template

```yaml
# Generated by DADA Cloud Console — DO NOT EDIT MANUALLY
# Project: {{.ProjectName}} | App: {{.AppName}} | Server: {{.ServerName}}
# Generated: {{.Timestamp}} | Operation: {{.OperationID}}
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
      driver: json-file
      options:
        max-size: "100m"
        max-file: "5"
    labels:
      dada.io/project: "{{.ProjectName}}"
      dada.io/app: "{{.AppName}}"
      dada.io/managed: "true"
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:{{.Port}}/health"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
```

---

## 10. portainer-agent Service Layout

```
portainer-agent/
  cmd/portainer-agent/
    main.go                  — signal ctx, wire db+workers, start
  internal/
    config/
      config.go              — all env vars loaded here
    db/
      pool.go                — pgx connect
      operations.go          — claim (SKIP LOCKED), update status, fail
      appservers.go          — CRUD for app_servers table
      snapshots.go           — upsert resource_snapshots
    terraform/
      executor.go            — tfexec wrapper: init, apply, destroy, output
      workspace.go           — create/clean workspace dir, copy templates
      templates/
        main.tf.tmpl         — beget_compute_instance resource
        variables.tf
    ssh/
      client.go              — SSH connect with retry, run bootstrap script
      bootstrap.sh.tmpl      — Go text/template rendered before SSH exec
    portainer/
      client.go              — HTTP client: endpoints, stacks, containers, logs
      models.go              — Endpoint, Stack, Container structs
    compose/
      renderer.go            — text/template → docker-compose.yml
    git/
      manager.go             — clone/pull/commit/push (same pattern as gitops-agent)
    worker/
      vm_watcher.go          — DB poll: claim → dispatch by action
      create_appserver.go    — CreateAppServer flow (Portainer → Terraform → poll)
      delete_appserver.go    — DeleteAppServer flow
      create_app.go          — CreateApp vm flow
      update_app.go          — DeployImageVersion + UpdateAppEnvVars vm flow
      status_watcher.go      — poll container status → upsert resource_snapshots
    server/
      server.go              — GET /healthz
  Dockerfile                 — multi-stage: golang + terraform binary
  go.mod
```

### Config env vars

```
DATABASE_URL

PORTAINER_URL              https://portainer.internal.dada-tuda.ru
PORTAINER_API_TOKEN        ptr_xxxx

# Beget Cloud (NOT OpenStack — proprietary API)
BEGET_TOKEN                Bearer token from Beget control panel → API access
BEGET_REGION               ru1 | ru2 | kz1 | eu1
BEGET_SOFTWARE_ID          Ubuntu 22.04 software ID (query once via data source)
BEGET_SSH_KEY_ID           ID of SSH key registered in Beget account

# SSH bootstrap key (private key PEM — must match BEGET_SSH_KEY_ID public key)
AGENT_SSH_PRIVATE_KEY      -----BEGIN OPENSSH PRIVATE KEY-----...

TF_WORKSPACE_BASE          /var/lib/tf-workspaces   (PVC-backed)
TF_STATE_CONN_STR          postgres DSN for terraform pg backend
TF_BIN_PATH                /usr/local/bin/terraform

GITOPS_REPO_URL
GITOPS_BRANCH              main
GITOPS_USERNAME
GITOPS_TOKEN
GITOPS_REPO_LOCAL_PATH     /var/lib/gitops-repos    (PVC-backed or shared)
GITOPS_BOT_NAME            DADA Platform Bot
GITOPS_BOT_EMAIL           bot@dada-tuda.ru

PROMETHEUS_REMOTE_WRITE_URL
PROMETHEUS_REMOTE_WRITE_USER
PROMETHEUS_REMOTE_WRITE_PASS

ELASTICSEARCH_URL
ELASTICSEARCH_API_KEY

VM_POLL_INTERVAL_DB        5s
VM_POLL_INTERVAL_STATUS    30s
AGENT_CONNECT_TIMEOUT      10m
```

---

## 11. Backend API — New & Modified Endpoints

### AppServer endpoints (new)

```
POST   /api/v1/projects/{projectId}/app-servers
         → validates payload, inserts Operation{action=CreateAppServer}
         → 202 Accepted {operation, message}

GET    /api/v1/projects/{projectId}/app-servers
         → SELECT * FROM app_servers WHERE project_id = $1

GET    /api/v1/projects/{projectId}/app-servers/{serverName}
         → single AppServer + linked environment info

DELETE /api/v1/projects/{projectId}/app-servers/{serverName}
         → inserts Operation{action=DeleteAppServer}
```

### App endpoints (modified)

```
POST /api/v1/projects/{projectId}/environments/{envId}/apps
  if env.runtime = 'vm':
    validate: app_server_name required, replicas/profile ignored
    insert Operation{action=CreateApp, payload includes app_server_name + env_vars}
  if env.runtime = 'k8s':
    existing behaviour unchanged

PATCH /api/v1/projects/{projectId}/environments/{envId}/apps/{appName}/image
  both tracks: insert Operation{action=DeployImageVersion}
  portainer-agent handles for vm, gitops-agent for k8s

PATCH /api/v1/projects/{projectId}/environments/{envId}/apps/{appName}/env-vars
  vm track only
  Body: { "env_vars": { "KEY": "value" } }
  → insert Operation{action=UpdateAppEnvVars}

GET /api/v1/projects/{projectId}/environments/{envId}/apps/{appName}/logs
  vm track:
    resolve portainer_endpoint_id + container_id from resource_snapshots.summary_json
    proxy GET /api/endpoints/{eid}/docker/containers/{cid}/logs?follow=1&tail=200
    stream as chunked response (Content-Type: text/event-stream)
  k8s track: 501 Not Implemented (future)
```

---

## 12. Frontend Pages

| Page | Change |
|---|---|
| `/projects/{id}/app-servers` | **New** — list with status chips, create modal (flavor, region, SSH key), delete |
| `/projects/{id}/environments` | **New** — runtime selector (K8s/VM) when creating env; AppServer picker for VM |
| `/projects/{id}/apps` | **Updated** — runtime badge on each app card |
| `/projects/{id}/apps/{name}` | **Updated** — Logs tab (SSE stream), Env Vars section (VM only), image update works for both |

---

## 13. Implementation Phases

### Phase 1 — Foundation (no new service yet)
1. Migration 004: `app_servers` table + `environments.runtime/app_server_id`
2. New payload structs in `backend/internal/models/operation.go`
3. Backend API: `appservers.go` handler (list, create→op, delete→op)
4. gitops-agent: add `env.runtime = 'k8s'` filter to claim query
5. Deploy Portainer CE to dada-cloud cluster (helm chart, create bot user + token)

### Phase 2 — portainer-agent scaffold
6. `portainer-agent/` Go module, main.go, config, db pool (copy pool.go from gitops-agent)
7. `portainer/client.go` — endpoint CRUD + stack CRUD + log proxy
8. `terraform/executor.go` — tfexec wrapper
9. Terraform templates + cloud-init template
10. `compose/renderer.go`
11. `git/manager.go` — reuse gitops-agent pattern exactly

### Phase 3 — AppServer lifecycle
12. `worker/create_appserver.go` — full flow: Portainer endpoint → Terraform → poll
13. `worker/delete_appserver.go` — delete stacks → terraform destroy
14. Integration test: provision a real VDS on Beget dev tenant

### Phase 4 — App lifecycle on VM
15. `worker/create_app.go` — render + git commit + Portainer stack create
16. `worker/update_app.go` — DeployImageVersion + UpdateAppEnvVars
17. Backend: extend `CreateApp` handler for vm environments
18. Backend: `UpdateAppEnvVars` endpoint
19. Backend: logs proxy endpoint

### Phase 5 — Status + observability wiring
20. `worker/status_watcher.go` — poll container stats → upsert resource_snapshots
21. Verify node_exporter and cAdvisor metrics arrive in central Prometheus
22. Verify Filebeat log indices appear in Elasticsearch
23. Import Grafana dashboards 1860 + 19908

### Phase 6 — Frontend
24. AppServer list + create/delete pages
25. Environment creation: runtime selector
26. App detail: Logs tab + Env Vars section + runtime badge

### Phase 7 — Infra + CI
27. Portainer-agent Dockerfile (multi-stage: go build + terraform binary)
28. Helm chart: `portainerAgent` deployment + two PVCs (tf-workspaces, gitops-repos)
29. Jenkins pipeline: build + push portainer-agent image
30. ADR-007 status → Accepted

---

## 14. Open Questions — Resolved

| Question | Decision |
|---|---|
| VM track parallel or replaces K8s? | Parallel — `env.runtime` discriminates |
| Portainer executor: extend gitops-agent or separate? | Separate `portainer-agent` |
| VM provisioning? | Terraform + OpenStack (Beget) |
| app_deployments as separate DB concept? | No — unified as `App` in resource_snapshots |
| Metrics: pull or push? | Push — Prometheus Agent remote_write (works through NAT) |
| Log shipping: Filebeat or Docker log driver? | Filebeat in Docker container (more flexible, no daemon changes) |
| Prometheus Agent vs full Prometheus on VM? | Agent mode (`--enable-feature=agent`) — no local storage, only remote_write |
| Terraform state backend? | PostgreSQL pg backend, schema per AppServer workspace |
| Beget OpenStack API? | **No** — Beget has proprietary API only. Use `tf.beget.com/beget/beget` |
| cloud-init on Beget? | **Not supported** via Terraform provider. Use SSH bootstrap script instead |
| SSH key for bootstrap? | Pre-registered in Beget; private PEM in `AGENT_SSH_PRIVATE_KEY` env var |
| Edge endpoint: pre-create or auto-create? | Pre-create via API → get EdgeKey + EdgeID → pass to SSH bootstrap script → poll Heartbeat |
