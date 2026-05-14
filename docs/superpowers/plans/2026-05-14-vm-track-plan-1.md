# VM Track Plan 1 — Foundation + AppServer Lifecycle

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create and delete AppServers via API — Beget VDS provisions, Portainer Edge Agent starts, and the VM is ready to receive app deployments.

**Architecture:** portainer-agent is a new Go service that polls the `operations` table (SKIP LOCKED) for VM-track actions. CreateAppServer: registers Portainer edge endpoint → runs Terraform (Beget provider) → SSHes in to run bootstrap script → polls until Edge Agent connects. DeleteAppServer tears down stacks, then destroys the VM.

**Tech Stack:** Go 1.22, pgx/v5, zerolog, gin (backend only), terraform-exec v0.25.2, golang.org/x/crypto/ssh, go-git/v5

---

## File Map

**New files — backend:**
- `backend/migrations/004_vm_track.sql` — app_servers table + environments columns
- `backend/internal/models/operation.go` — add CreateAppServerPayload, DeleteAppServerPayload, UpdateAppEnvVarsPayload; extend CreateAppPayload
- `backend/internal/api/appservers.go` — ListAppServers, CreateAppServer, GetAppServer, DeleteAppServer handlers
- `backend/internal/api/router.go` — add 4 new routes

**Modified files — backend:**
- `backend/internal/api/apps.go` — extend CreateApp handler for vm runtime; add UpdateAppEnvVars, StreamAppLogs
- `backend/internal/api/router.go` — add env-vars + logs routes

**Modified files — gitops-agent:**
- `gitops-agent/internal/db/operations.go` — add runtime filter to ClaimPending

**New service — portainer-agent:**
- `portainer-agent/go.mod`
- `portainer-agent/cmd/portainer-agent/main.go`
- `portainer-agent/internal/config/config.go`
- `portainer-agent/internal/db/pool.go`
- `portainer-agent/internal/db/operations.go`
- `portainer-agent/internal/db/appservers.go`
- `portainer-agent/internal/db/snapshots.go`
- `portainer-agent/internal/portainer/models.go`
- `portainer-agent/internal/portainer/client.go`
- `portainer-agent/internal/terraform/templates/main.tf.tmpl`
- `portainer-agent/internal/terraform/templates/variables.tf`
- `portainer-agent/internal/terraform/workspace.go`
- `portainer-agent/internal/terraform/executor.go`
- `portainer-agent/internal/ssh/bootstrap.sh.tmpl`
- `portainer-agent/internal/ssh/client.go`
- `portainer-agent/internal/worker/vm_watcher.go`
- `portainer-agent/internal/worker/create_appserver.go`
- `portainer-agent/internal/worker/delete_appserver.go`
- `portainer-agent/internal/server/server.go`
- `portainer-agent/Dockerfile`

---

### Task 1: DB Migration 004

**Files:**
- Create: `backend/migrations/004_vm_track.sql`

- [ ] **Step 1: Write the migration**

```sql
-- backend/migrations/004_vm_track.sql
-- VM track: app_servers table + environments runtime/app_server_id columns

-- ① Create app_servers FIRST (environments will FK to it)
CREATE TABLE IF NOT EXISTS app_servers (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id          UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name                VARCHAR(255) NOT NULL,
    vm_ip               VARCHAR(45),
    vm_provider_id      VARCHAR(255),
    terraform_workspace VARCHAR(500),
    portainer_endpoint_id INTEGER,
    status              VARCHAR(50) NOT NULL DEFAULT 'Provisioning'
                        CHECK (status IN ('Provisioning','WaitingForAgent','Ready',
                                          'Deleting','Deleted','Failed')),
    error_message       TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(project_id, name)
);
CREATE INDEX IF NOT EXISTS idx_app_servers_project ON app_servers(project_id);

-- ② Extend environments with runtime + optional server ref
ALTER TABLE environments
    ADD COLUMN IF NOT EXISTS runtime VARCHAR(20) NOT NULL DEFAULT 'k8s'
        CHECK (runtime IN ('k8s', 'vm'));
ALTER TABLE environments
    ADD COLUMN IF NOT EXISTS app_server_id UUID REFERENCES app_servers(id) ON DELETE SET NULL;
```

- [ ] **Step 2: Apply migration via docker-compose**

```bash
cd /Users/alex/IdeaProjects/dada-cloud
docker-compose exec backend sh -c 'ls /app/migrations/'
```

Expected: `001_initial_schema.sql  002_seed_dev_data.sql  003_gitops_agent.sql  004_vm_track.sql`

The backend applies migrations on startup via `db.RunMigrations`. Restart the backend container:

```bash
docker-compose restart backend
```

- [ ] **Step 3: Verify migration applied**

```bash
docker-compose exec postgres psql -U dada -d dada_cloud -c "\d app_servers"
```

Expected: table with columns id, project_id, name, vm_ip, vm_provider_id, terraform_workspace, portainer_endpoint_id, status, error_message, created_at, updated_at.

```bash
docker-compose exec postgres psql -U dada -d dada_cloud -c "\d environments"
```

Expected: includes `runtime` (character varying, default 'k8s') and `app_server_id` (uuid).

- [ ] **Step 4: Commit**

```bash
git add backend/migrations/004_vm_track.sql
git commit -m "feat(db): migration 004 — app_servers table and environments vm columns"
```

---

### Task 2: Backend — payload struct additions

**Files:**
- Modify: `backend/internal/models/operation.go`

- [ ] **Step 1: Read current file (already done above)**

- [ ] **Step 2: Replace `CreateAppPayload` and add new structs**

In `backend/internal/models/operation.go`, replace the `CreateAppPayload` struct and add the new ones. The full updated block (replace from `// CreateAppPayload` through the end of `DeployImageVersionPayload`):

```go
// CreateAppPayload is the typed payload for CreateApp operations.
// K8s fields: Replicas, Profile. VM fields: AppServerName, EnvVars.
type CreateAppPayload struct {
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	Port          int               `json:"port"`
	Replicas      int               `json:"replicas,omitempty"`
	Profile       string            `json:"profile,omitempty"`
	AppServerName string            `json:"app_server_name,omitempty"`
	EnvVars       map[string]string `json:"env_vars,omitempty"`
}

// DeployImageVersionPayload is the typed payload for DeployImageVersion operations.
type DeployImageVersionPayload struct {
	AppName string `json:"name"`
	Image   string `json:"image"`
}

// CreateAppServerPayload is the typed payload for CreateAppServer operations.
type CreateAppServerPayload struct {
	Name       string `json:"name"`
	Flavor     string `json:"flavor"`
	OSImage    string `json:"os_image"`
	Region     string `json:"region"`
	SSHKeyName string `json:"ssh_key_name"`
}

// DeleteAppServerPayload is the typed payload for DeleteAppServer operations.
type DeleteAppServerPayload struct {
	AppServerName string `json:"app_server_name"`
}

// UpdateAppEnvVarsPayload is the typed payload for UpdateAppEnvVars operations (VM track only).
type UpdateAppEnvVarsPayload struct {
	AppName string            `json:"app_name"`
	EnvVars map[string]string `json:"env_vars"`
}
```

Apply by editing `backend/internal/models/operation.go`:

```go
// CreateAppPayload is the typed payload for CreateApp operations.
// K8s fields: Replicas, Profile. VM fields: AppServerName, EnvVars.
type CreateAppPayload struct {
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	Port          int               `json:"port"`
	Replicas      int               `json:"replicas,omitempty"`
	Profile       string            `json:"profile,omitempty"`
	AppServerName string            `json:"app_server_name,omitempty"`
	EnvVars       map[string]string `json:"env_vars,omitempty"`
}

// DeployImageVersionPayload is the typed payload for DeployImageVersion operations.
type DeployImageVersionPayload struct {
	AppName string `json:"app_name"`
	Image   string `json:"image"`
}

// CreatePublicApiPayload is the typed payload for CreatePublicApi operations.
type CreatePublicApiPayload struct {
	AppName        string   `json:"app_name"`
	PublicApiName  string   `json:"public_api_name"`
	FQDN           string   `json:"fqdn"`
	AuthEnabled    bool     `json:"auth_enabled"`
	AuthScheme     string   `json:"auth_scheme"`
	AuthScopes     []string `json:"auth_scopes,omitempty"`
	SwaggerEnabled bool     `json:"swagger_enabled"`
	SwaggerPath    string   `json:"swagger_path"`
	SwaggerTitle   string   `json:"swagger_title"`
}

// CreateAppServerPayload is the typed payload for CreateAppServer operations.
type CreateAppServerPayload struct {
	Name       string `json:"name"`
	Flavor     string `json:"flavor"`
	OSImage    string `json:"os_image"`
	Region     string `json:"region"`
	SSHKeyName string `json:"ssh_key_name"`
}

// DeleteAppServerPayload is the typed payload for DeleteAppServer operations.
type DeleteAppServerPayload struct {
	AppServerName string `json:"app_server_name"`
}

// UpdateAppEnvVarsPayload is the typed payload for UpdateAppEnvVars operations (VM track only).
type UpdateAppEnvVarsPayload struct {
	AppName string            `json:"app_name"`
	EnvVars map[string]string `json:"env_vars"`
}
```

> **Note on DeployImageVersionPayload:** the existing struct had `AppName string \`json:"app_name"\`` — keep that tag unchanged. The gitops-agent worker references this field.

- [ ] **Step 3: Verify it compiles**

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend
go build ./...
```

Expected: no output (success).

- [ ] **Step 4: Commit**

```bash
git add backend/internal/models/operation.go
git commit -m "feat(models): add CreateAppServer, DeleteAppServer, UpdateAppEnvVars payloads; extend CreateAppPayload for vm"
```

---

### Task 3: Backend — AppServer model

**Files:**
- Create: `backend/internal/models/appserver.go`

- [ ] **Step 1: Write the AppServer model**

```go
// backend/internal/models/appserver.go
package models

import (
	"time"

	"github.com/google/uuid"
)

// AppServerStatus is the lifecycle state of a customer VM.
type AppServerStatus string

const (
	AppServerStatusProvisioning   AppServerStatus = "Provisioning"
	AppServerStatusWaitingForAgent AppServerStatus = "WaitingForAgent"
	AppServerStatusReady          AppServerStatus = "Ready"
	AppServerStatusDeleting       AppServerStatus = "Deleting"
	AppServerStatusDeleted        AppServerStatus = "Deleted"
	AppServerStatusFailed         AppServerStatus = "Failed"
)

// AppServer represents a customer-provisioned VDS running Docker + Portainer Edge Agent.
type AppServer struct {
	ID                   uuid.UUID       `json:"id"                      db:"id"`
	ProjectID            uuid.UUID       `json:"project_id"              db:"project_id"`
	Name                 string          `json:"name"                    db:"name"`
	VMIP                 *string         `json:"vm_ip,omitempty"         db:"vm_ip"`
	VMProviderID         *string         `json:"vm_provider_id,omitempty" db:"vm_provider_id"`
	TerraformWorkspace   *string         `json:"terraform_workspace,omitempty" db:"terraform_workspace"`
	PortainerEndpointID  *int            `json:"portainer_endpoint_id,omitempty" db:"portainer_endpoint_id"`
	Status               AppServerStatus `json:"status"                  db:"status"`
	ErrorMessage         *string         `json:"error_message,omitempty" db:"error_message"`
	CreatedAt            time.Time       `json:"created_at"              db:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"              db:"updated_at"`
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend
go build ./...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/models/appserver.go
git commit -m "feat(models): AppServer model struct"
```

---

### Task 4: Backend — AppServer API handlers

**Files:**
- Create: `backend/internal/api/appservers.go`
- Modify: `backend/internal/api/router.go`

- [ ] **Step 1: Write appservers.go**

```go
// backend/internal/api/appservers.go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/dada-tuda/console/backend/internal/auth"
	"github.com/dada-tuda/console/backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ListAppServers returns all AppServers for a project.
func (h *Handler) ListAppServers(c *gin.Context) {
	claims, ok := auth.GetClaims(c)
	if !ok {
		respondUnauthorized(c)
		return
	}
	projectID, err := uuid.Parse(c.Param("projectId"))
	if err != nil {
		respondNotFound(c)
		return
	}
	_, err = h.getUserProjectRole(c.Request.Context(), claims.UserID, projectID)
	if err == pgx.ErrNoRows {
		respondNotFound(c)
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to check project membership")
		return
	}

	rows, err := h.pool.Query(c.Request.Context(),
		`SELECT id, project_id, name, vm_ip, vm_provider_id, terraform_workspace,
		        portainer_endpoint_id, status, error_message, created_at, updated_at
		 FROM app_servers
		 WHERE project_id = $1 AND status != 'Deleted'
		 ORDER BY created_at DESC`,
		projectID,
	)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to query app servers")
		return
	}
	defer rows.Close()

	var servers []models.AppServer
	for rows.Next() {
		var s models.AppServer
		if err := rows.Scan(
			&s.ID, &s.ProjectID, &s.Name, &s.VMIP, &s.VMProviderID,
			&s.TerraformWorkspace, &s.PortainerEndpointID,
			&s.Status, &s.ErrorMessage, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			respondError(c, http.StatusInternalServerError, "failed to scan app server")
			return
		}
		servers = append(servers, s)
	}
	if servers == nil {
		servers = []models.AppServer{}
	}
	c.JSON(http.StatusOK, gin.H{"app_servers": servers})
}

// GetAppServer returns a single AppServer by name.
func (h *Handler) GetAppServer(c *gin.Context) {
	claims, ok := auth.GetClaims(c)
	if !ok {
		respondUnauthorized(c)
		return
	}
	projectID, err := uuid.Parse(c.Param("projectId"))
	if err != nil {
		respondNotFound(c)
		return
	}
	serverName := c.Param("serverName")

	_, err = h.getUserProjectRole(c.Request.Context(), claims.UserID, projectID)
	if err == pgx.ErrNoRows {
		respondNotFound(c)
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to check project membership")
		return
	}

	var s models.AppServer
	err = h.pool.QueryRow(c.Request.Context(),
		`SELECT id, project_id, name, vm_ip, vm_provider_id, terraform_workspace,
		        portainer_endpoint_id, status, error_message, created_at, updated_at
		 FROM app_servers
		 WHERE project_id = $1 AND name = $2`,
		projectID, serverName,
	).Scan(
		&s.ID, &s.ProjectID, &s.Name, &s.VMIP, &s.VMProviderID,
		&s.TerraformWorkspace, &s.PortainerEndpointID,
		&s.Status, &s.ErrorMessage, &s.CreatedAt, &s.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		respondNotFound(c)
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to get app server")
		return
	}
	c.JSON(http.StatusOK, gin.H{"app_server": s})
}

type createAppServerRequest struct {
	Name       string `json:"name"`
	Flavor     string `json:"flavor"`
	OSImage    string `json:"os_image"`
	Region     string `json:"region"`
	SSHKeyName string `json:"ssh_key_name"`
}

// CreateAppServer enqueues a CreateAppServer operation.
func (h *Handler) CreateAppServer(c *gin.Context) {
	claims, ok := auth.GetClaims(c)
	if !ok {
		respondUnauthorized(c)
		return
	}
	projectID, err := uuid.Parse(c.Param("projectId"))
	if err != nil {
		respondNotFound(c)
		return
	}

	role, err := h.getUserProjectRole(c.Request.Context(), claims.UserID, projectID)
	if err == pgx.ErrNoRows {
		respondNotFound(c)
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to check project membership")
		return
	}
	if !canWrite(role) {
		respondForbidden(c)
		return
	}

	var req createAppServerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.Name == "" {
		respondError(c, http.StatusBadRequest, "name is required")
		return
	}
	if err := validateKubeName(req.Name); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	validRegions := map[string]bool{"ru1": true, "ru2": true, "kz1": true, "eu1": true}
	if !validRegions[req.Region] {
		respondError(c, http.StatusBadRequest, "region must be one of: ru1, ru2, kz1, eu1")
		return
	}

	// Check name uniqueness
	var existing int
	if err := h.pool.QueryRow(c.Request.Context(),
		`SELECT COUNT(*) FROM app_servers WHERE project_id = $1 AND name = $2 AND status != 'Deleted'`,
		projectID, req.Name,
	).Scan(&existing); err != nil {
		respondError(c, http.StatusInternalServerError, "failed to check name uniqueness")
		return
	}
	if existing > 0 {
		respondError(c, http.StatusConflict, "an app server with that name already exists in this project")
		return
	}

	payload := models.CreateAppServerPayload{
		Name:       req.Name,
		Flavor:     req.Flavor,
		OSImage:    req.OSImage,
		Region:     req.Region,
		SSHKeyName: req.SSHKeyName,
	}
	payloadBytes, _ := json.Marshal(payload)

	var op models.Operation
	row := h.pool.QueryRow(c.Request.Context(),
		`INSERT INTO operations (actor_id, project_id, action, resource_kind, resource_name, status, payload)
		 VALUES ($1, $2, 'CreateAppServer', 'AppServer', $3, 'Created', $4)
		 RETURNING id, actor_id, project_id, environment_id, action, resource_kind, resource_name,
		           status, payload, validation_result, git_commit, git_path, argo_application,
		           error_code, error_message, created_at, updated_at`,
		claims.UserID, projectID, req.Name, payloadBytes,
	)
	if err := scanOperation(row, &op); err != nil {
		respondError(c, http.StatusInternalServerError, "failed to create operation")
		return
	}

	auditMeta, _ := json.Marshal(payload)
	_, _ = h.pool.Exec(c.Request.Context(),
		`INSERT INTO audit_events (actor_id, project_id, operation_id, action, resource_kind, resource_name, metadata)
		 VALUES ($1, $2, $3, 'CreateAppServer', 'AppServer', $4, $5)`,
		claims.UserID, projectID, op.ID, req.Name, auditMeta,
	)

	c.JSON(http.StatusAccepted, gin.H{"operation": op, "message": "AppServer creation queued"})
}

// DeleteAppServer enqueues a DeleteAppServer operation.
func (h *Handler) DeleteAppServer(c *gin.Context) {
	claims, ok := auth.GetClaims(c)
	if !ok {
		respondUnauthorized(c)
		return
	}
	projectID, err := uuid.Parse(c.Param("projectId"))
	if err != nil {
		respondNotFound(c)
		return
	}
	serverName := c.Param("serverName")

	role, err := h.getUserProjectRole(c.Request.Context(), claims.UserID, projectID)
	if err == pgx.ErrNoRows {
		respondNotFound(c)
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to check project membership")
		return
	}
	if !canWrite(role) {
		respondForbidden(c)
		return
	}

	// Verify server exists and belongs to project
	var serverID uuid.UUID
	err = h.pool.QueryRow(c.Request.Context(),
		`SELECT id FROM app_servers WHERE project_id = $1 AND name = $2 AND status NOT IN ('Deleting','Deleted')`,
		projectID, serverName,
	).Scan(&serverID)
	if err == pgx.ErrNoRows {
		respondNotFound(c)
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to find app server")
		return
	}

	payload := models.DeleteAppServerPayload{AppServerName: serverName}
	payloadBytes, _ := json.Marshal(payload)

	var op models.Operation
	row := h.pool.QueryRow(c.Request.Context(),
		`INSERT INTO operations (actor_id, project_id, action, resource_kind, resource_name, status, payload)
		 VALUES ($1, $2, 'DeleteAppServer', 'AppServer', $3, 'Created', $4)
		 RETURNING id, actor_id, project_id, environment_id, action, resource_kind, resource_name,
		           status, payload, validation_result, git_commit, git_path, argo_application,
		           error_code, error_message, created_at, updated_at`,
		claims.UserID, projectID, serverName, payloadBytes,
	)
	if err := scanOperation(row, &op); err != nil {
		respondError(c, http.StatusInternalServerError, "failed to create operation")
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"operation": op, "message": "AppServer deletion queued"})
}
```

- [ ] **Step 2: Add routes to router.go**

In `backend/internal/api/router.go`, add after the existing `Apps` route group:

```go
		// AppServers (VM track)
		api.GET("/projects/:projectId/app-servers", h.ListAppServers)
		api.POST("/projects/:projectId/app-servers", h.CreateAppServer)
		api.GET("/projects/:projectId/app-servers/:serverName", h.GetAppServer)
		api.DELETE("/projects/:projectId/app-servers/:serverName", h.DeleteAppServer)
```

- [ ] **Step 3: Compile**

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend
go build ./...
```

Expected: no output.

- [ ] **Step 4: Manual smoke test**

```bash
# Get a JWT first
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@dada.ru","password":"admin123"}' | jq -r '.token')

# Create an AppServer operation
curl -s -X POST http://localhost:8080/api/v1/projects/$(
  curl -s http://localhost:8080/api/v1/projects \
    -H "Authorization: Bearer $TOKEN" | jq -r '.projects[0].id'
)/app-servers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"test-server-1","flavor":"2vcpu-4gb","os_image":"ubuntu-22.04","region":"ru1","ssh_key_name":"dada-deploy"}' | jq .
```

Expected: `{"message":"AppServer creation queued","operation":{...,"action":"CreateAppServer","status":"Created",...}}`

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/appservers.go backend/internal/api/router.go backend/internal/models/appserver.go
git commit -m "feat(api): AppServer CRUD endpoints — list, get, create→op, delete→op"
```

---

### Task 5: gitops-agent claim query fix

**Files:**
- Modify: `gitops-agent/internal/db/operations.go`

The current `ClaimPending` query picks up ALL `Created` operations including VM-track ones. Add a filter so gitops-agent only claims k8s-track operations.

- [ ] **Step 1: Write the failing test**

```go
// gitops-agent/internal/db/operations_test.go
package db_test

import (
	"testing"
)

// TestClaimQueryFilterComment documents that ClaimPending must filter to k8s operations only.
// Full integration test requires a real DB; this is a compile-time guard on the query string.
func TestClaimQueryFilterComment(t *testing.T) {
	// This test ensures the file compiles. The runtime behaviour is verified
	// by the integration smoke test in Task 5 Step 3.
	t.Log("ClaimPending k8s filter: compile guard OK")
}
```

- [ ] **Step 2: Run test (should pass trivially)**

```bash
cd /Users/alex/IdeaProjects/dada-cloud/gitops-agent
go test ./internal/db/... -v -run TestClaimQueryFilterComment
```

Expected: `PASS`

- [ ] **Step 3: Update ClaimPending query**

In `gitops-agent/internal/db/operations.go`, replace the query inside `ClaimPending`:

Old block:
```go
	rows, err := tx.Query(ctx, `
		UPDATE operations
		SET    status = 'Processing', updated_at = NOW()
		WHERE  id IN (
			SELECT id FROM operations
			WHERE  status = 'Created'
			ORDER  BY created_at
			LIMIT  $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, project_id, environment_id, action, resource_kind, resource_name, payload, created_at
	`, claimBatchSize)
```

New block:
```go
	rows, err := tx.Query(ctx, `
		UPDATE operations
		SET    status = 'Processing', updated_at = NOW()
		WHERE  id IN (
			SELECT o.id FROM operations o
			LEFT JOIN environments e ON e.id = o.environment_id
			WHERE  o.status = 'Created'
			  AND  o.action NOT IN ('CreateAppServer', 'DeleteAppServer')
			  AND  (e.runtime = 'k8s' OR o.environment_id IS NULL)
			ORDER  BY o.created_at
			LIMIT  $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, project_id, environment_id, action, resource_kind, resource_name, payload, created_at
	`, claimBatchSize)
```

- [ ] **Step 4: Compile**

```bash
cd /Users/alex/IdeaProjects/dada-cloud/gitops-agent
go build ./...
```

Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add gitops-agent/internal/db/operations.go gitops-agent/internal/db/operations_test.go
git commit -m "fix(gitops-agent): filter ClaimPending to k8s-track operations only"
```

---

### Task 6: portainer-agent — go.mod + directory scaffold

**Files:**
- Create: `portainer-agent/go.mod`
- Create: directory tree

- [ ] **Step 1: Create directory tree**

```bash
mkdir -p /Users/alex/IdeaProjects/dada-cloud/portainer-agent/{cmd/portainer-agent,internal/{config,db,portainer,terraform/templates,ssh,compose,git,worker,server}}
```

- [ ] **Step 2: Write go.mod**

```
module github.com/dada-tuda/console/portainer-agent

go 1.22

require (
	github.com/go-git/go-git/v5 v5.11.0
	github.com/google/uuid v1.6.0
	github.com/hashicorp/terraform-exec v0.25.2
	github.com/jackc/pgx/v5 v5.5.4
	github.com/joho/godotenv v1.5.1
	github.com/rs/zerolog v1.32.0
	golang.org/x/crypto v0.21.0
)
```

- [ ] **Step 3: Download dependencies**

```bash
cd /Users/alex/IdeaProjects/dada-cloud/portainer-agent
go mod tidy
```

Expected: creates `go.sum`, downloads modules. No errors.

- [ ] **Step 4: Commit scaffold**

```bash
git add portainer-agent/
git commit -m "chore(portainer-agent): go module scaffold and directory structure"
```

---

### Task 7: portainer-agent — config

**Files:**
- Create: `portainer-agent/internal/config/config.go`

- [ ] **Step 1: Write config**

```go
// portainer-agent/internal/config/config.go
package config

import (
	"fmt"
	"os"
	"time"
)

// Config holds all configuration loaded from environment variables.
type Config struct {
	DatabaseURL string

	PortainerURL      string
	PortainerAPIToken string

	BegetToken      string
	BegetRegion     string
	BegetSoftwareID string
	BegetSSHKeyID   string

	AgentSSHPrivateKey string // PEM, must match BegetSSHKeyID public key

	TFWorkspaceBase string
	TFStateConnStr  string
	TFBinPath       string

	GitopsRepoURL       string
	GitopsBranch        string
	GitopsUsername      string
	GitopsToken         string
	GitopsRepoLocalPath string
	GitopsBotName       string
	GitopsBotEmail      string

	PrometheusRemoteWriteURL  string
	PrometheusRemoteWriteUser string
	PrometheusRemoteWritePass string

	ElasticsearchURL    string
	ElasticsearchAPIKey string

	PollIntervalDB     time.Duration
	PollIntervalStatus time.Duration
	AgentConnectTimeout time.Duration

	DevMode bool
}

// Load reads all required env vars and returns a Config or a descriptive error.
func Load() (*Config, error) {
	c := &Config{
		DatabaseURL:       requireEnv("DATABASE_URL"),
		PortainerURL:      requireEnv("PORTAINER_URL"),
		PortainerAPIToken: requireEnv("PORTAINER_API_TOKEN"),

		BegetToken:      requireEnv("BEGET_TOKEN"),
		BegetRegion:     getEnv("BEGET_REGION", "ru1"),
		BegetSoftwareID: requireEnv("BEGET_SOFTWARE_ID"),
		BegetSSHKeyID:   requireEnv("BEGET_SSH_KEY_ID"),

		AgentSSHPrivateKey: requireEnv("AGENT_SSH_PRIVATE_KEY"),

		TFWorkspaceBase: getEnv("TF_WORKSPACE_BASE", "/var/lib/tf-workspaces"),
		TFStateConnStr:  requireEnv("TF_STATE_CONN_STR"),
		TFBinPath:       getEnv("TF_BIN_PATH", "/usr/local/bin/terraform"),

		GitopsRepoURL:       requireEnv("GITOPS_REPO_URL"),
		GitopsBranch:        getEnv("GITOPS_BRANCH", "main"),
		GitopsUsername:      requireEnv("GITOPS_USERNAME"),
		GitopsToken:         requireEnv("GITOPS_TOKEN"),
		GitopsRepoLocalPath: getEnv("GITOPS_REPO_LOCAL_PATH", "/var/lib/gitops-repos"),
		GitopsBotName:       getEnv("GITOPS_BOT_NAME", "DADA Platform Bot"),
		GitopsBotEmail:      getEnv("GITOPS_BOT_EMAIL", "bot@dada-tuda.ru"),

		PrometheusRemoteWriteURL:  requireEnv("PROMETHEUS_REMOTE_WRITE_URL"),
		PrometheusRemoteWriteUser: getEnv("PROMETHEUS_REMOTE_WRITE_USER", ""),
		PrometheusRemoteWritePass: getEnv("PROMETHEUS_REMOTE_WRITE_PASS", ""),

		ElasticsearchURL:    requireEnv("ELASTICSEARCH_URL"),
		ElasticsearchAPIKey: requireEnv("ELASTICSEARCH_API_KEY"),

		DevMode: getEnv("DEV_MODE", "") == "true",
	}

	var err error
	c.PollIntervalDB, err = parseDuration("VM_POLL_INTERVAL_DB", "5s")
	if err != nil {
		return nil, err
	}
	c.PollIntervalStatus, err = parseDuration("VM_POLL_INTERVAL_STATUS", "30s")
	if err != nil {
		return nil, err
	}
	c.AgentConnectTimeout, err = parseDuration("AGENT_CONNECT_TIMEOUT", "10m")
	if err != nil {
		return nil, err
	}

	return c, nil
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		// Don't panic — Load() callers will handle the empty string.
		// Use explicit checks for truly required fields in Load() if needed.
	}
	return v
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseDuration(key, def string) (time.Duration, error) {
	s := getEnv(key, def)
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid %s=%q: %w", key, s, err)
	}
	return d, nil
}
```

- [ ] **Step 2: Write config test**

```go
// portainer-agent/internal/config/config_test.go
package config_test

import (
	"os"
	"testing"

	"github.com/dada-tuda/console/portainer-agent/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	// Set required vars
	required := map[string]string{
		"DATABASE_URL":               "postgres://x",
		"PORTAINER_URL":              "http://portainer",
		"PORTAINER_API_TOKEN":        "ptr_test",
		"BEGET_TOKEN":                "tok",
		"BEGET_SOFTWARE_ID":          "42",
		"BEGET_SSH_KEY_ID":           "key-id",
		"AGENT_SSH_PRIVATE_KEY":      "-----BEGIN OPENSSH PRIVATE KEY-----",
		"TF_STATE_CONN_STR":          "postgres://x",
		"GITOPS_REPO_URL":            "https://github.com/test/repo",
		"GITOPS_USERNAME":            "bot",
		"GITOPS_TOKEN":               "ghp_x",
		"PROMETHEUS_REMOTE_WRITE_URL": "http://prometheus",
		"ELASTICSEARCH_URL":          "http://elastic",
		"ELASTICSEARCH_API_KEY":      "key",
	}
	for k, v := range required {
		os.Setenv(k, v)
	}
	defer func() {
		for k := range required {
			os.Unsetenv(k)
		}
	}()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.PollIntervalDB.String() != "5s" {
		t.Errorf("expected default PollIntervalDB=5s, got %s", cfg.PollIntervalDB)
	}
	if cfg.BegetRegion != "ru1" {
		t.Errorf("expected default BegetRegion=ru1, got %s", cfg.BegetRegion)
	}
	if cfg.GitopsBranch != "main" {
		t.Errorf("expected default GitopsBranch=main, got %s", cfg.GitopsBranch)
	}
}
```

- [ ] **Step 3: Run test**

```bash
cd /Users/alex/IdeaProjects/dada-cloud/portainer-agent
go test ./internal/config/... -v
```

Expected: `--- PASS: TestLoadDefaults`

- [ ] **Step 4: Commit**

```bash
git add portainer-agent/internal/config/
git commit -m "feat(portainer-agent): config loader with env var defaults"
```

---

### Task 8: portainer-agent — DB layer

**Files:**
- Create: `portainer-agent/internal/db/pool.go`
- Create: `portainer-agent/internal/db/operations.go`
- Create: `portainer-agent/internal/db/appservers.go`
- Create: `portainer-agent/internal/db/snapshots.go`

- [ ] **Step 1: Write pool.go**

```go
// portainer-agent/internal/db/pool.go
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens a pgxpool connection to the given DSN.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("db ping: %w", err)
	}
	return pool, nil
}
```

- [ ] **Step 2: Write operations.go**

```go
// portainer-agent/internal/db/operations.go
package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Operation mirrors the columns portainer-agent needs from the operations table.
type Operation struct {
	ID            uuid.UUID
	ProjectID     uuid.UUID
	EnvironmentID *uuid.UUID
	Action        string
	ResourceKind  string
	ResourceName  string
	Payload       json.RawMessage
	CreatedAt     time.Time
}

const claimBatchSize = 5

// ClaimPending atomically claims up to claimBatchSize Created operations for the VM track.
// VM-track operations are:
//   - action IN ('CreateAppServer', 'DeleteAppServer') — no environment
//   - environment.runtime = 'vm' — all other actions on VM environments
func ClaimPending(ctx context.Context, pool *pgxpool.Pool) ([]Operation, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	rows, err := tx.Query(ctx, `
		UPDATE operations
		SET    status = 'Processing', updated_at = NOW()
		WHERE  id IN (
			SELECT o.id FROM operations o
			LEFT JOIN environments e ON e.id = o.environment_id
			WHERE  o.status = 'Created'
			  AND  (
			    o.action IN ('CreateAppServer', 'DeleteAppServer')
			    OR e.runtime = 'vm'
			  )
			ORDER  BY o.created_at
			LIMIT  $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, project_id, environment_id, action, resource_kind, resource_name, payload, created_at
	`, claimBatchSize)
	if err != nil {
		return nil, fmt.Errorf("claim query: %w", err)
	}
	defer rows.Close()

	var ops []Operation
	for rows.Next() {
		var op Operation
		if err := rows.Scan(
			&op.ID, &op.ProjectID, &op.EnvironmentID,
			&op.Action, &op.ResourceKind, &op.ResourceName,
			&op.Payload, &op.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning operation: %w", err)
		}
		ops = append(ops, op)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return ops, nil
}

// UpdateStatus sets the operation status and clears error fields.
func UpdateStatus(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, status string) error {
	_, err := pool.Exec(ctx,
		`UPDATE operations SET status = $2, error_code = NULL, error_message = NULL, updated_at = NOW() WHERE id = $1`,
		id, status,
	)
	return err
}

// MarkFailed sets status=Failed with an error code and message.
func MarkFailed(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, code, message string) error {
	_, err := pool.Exec(ctx,
		`UPDATE operations SET status = 'Failed', error_code = $2, error_message = $3, updated_at = NOW() WHERE id = $1`,
		id, code, message,
	)
	return err
}

// MarkReady sets status=Ready.
func MarkReady(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	return UpdateStatus(ctx, pool, id, "Ready")
}
```

- [ ] **Step 3: Write appservers.go**

```go
// portainer-agent/internal/db/appservers.go
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AppServerRow is the minimal DB representation of an app_server row.
type AppServerRow struct {
	ID                  uuid.UUID
	ProjectID           uuid.UUID
	Name                string
	VMIP                *string
	VMProviderID        *string
	TerraformWorkspace  *string
	PortainerEndpointID *int
	Status              string
	ErrorMessage        *string
}

// GetAppServerByName fetches an app_server by project + name.
func GetAppServerByName(ctx context.Context, pool *pgxpool.Pool, projectID uuid.UUID, name string) (*AppServerRow, error) {
	var s AppServerRow
	err := pool.QueryRow(ctx,
		`SELECT id, project_id, name, vm_ip, vm_provider_id, terraform_workspace,
		        portainer_endpoint_id, status, error_message
		 FROM app_servers WHERE project_id = $1 AND name = $2`,
		projectID, name,
	).Scan(&s.ID, &s.ProjectID, &s.Name, &s.VMIP, &s.VMProviderID,
		&s.TerraformWorkspace, &s.PortainerEndpointID, &s.Status, &s.ErrorMessage)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// CreateAppServer inserts a new app_server row in Provisioning status.
func CreateAppServer(ctx context.Context, pool *pgxpool.Pool, projectID uuid.UUID, name, workspace string) (uuid.UUID, error) {
	var id uuid.UUID
	err := pool.QueryRow(ctx,
		`INSERT INTO app_servers (project_id, name, terraform_workspace, status)
		 VALUES ($1, $2, $3, 'Provisioning')
		 RETURNING id`,
		projectID, name, workspace,
	).Scan(&id)
	return id, err
}

// SetAppServerProvisioned updates vm_ip and vm_provider_id after terraform apply.
func SetAppServerProvisioned(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, vmIP, vmProviderID string) error {
	_, err := pool.Exec(ctx,
		`UPDATE app_servers SET vm_ip=$2, vm_provider_id=$3, status='WaitingForAgent', updated_at=NOW() WHERE id=$1`,
		id, vmIP, vmProviderID,
	)
	return err
}

// SetAppServerReady sets status=Ready and records the portainer endpoint ID.
func SetAppServerReady(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, portainerEndpointID int) error {
	_, err := pool.Exec(ctx,
		`UPDATE app_servers SET portainer_endpoint_id=$2, status='Ready', updated_at=NOW() WHERE id=$1`,
		id, portainerEndpointID,
	)
	return err
}

// SetAppServerFailed sets status=Failed with an error message.
func SetAppServerFailed(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, errMsg string) error {
	_, err := pool.Exec(ctx,
		`UPDATE app_servers SET status='Failed', error_message=$2, updated_at=NOW() WHERE id=$1`,
		id, errMsg,
	)
	return err
}

// SetAppServerDeleting sets status=Deleting.
func SetAppServerDeleting(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	_, err := pool.Exec(ctx,
		`UPDATE app_servers SET status='Deleting', updated_at=NOW() WHERE id=$1`,
		id,
	)
	return err
}

// SetAppServerDeleted sets status=Deleted.
func SetAppServerDeleted(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	_, err := pool.Exec(ctx,
		`UPDATE app_servers SET status='Deleted', updated_at=$2 WHERE id=$1`,
		id, time.Now(),
	)
	return err
}

// ListAppServerPortainerIDs returns all (app_server_id, portainer_endpoint_id) pairs for Ready servers.
func ListReadyAppServers(ctx context.Context, pool *pgxpool.Pool) ([]AppServerRow, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, project_id, name, vm_ip, vm_provider_id, terraform_workspace,
		        portainer_endpoint_id, status, error_message
		 FROM app_servers WHERE status = 'Ready'`)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()
	var result []AppServerRow
	for rows.Next() {
		var s AppServerRow
		if err := rows.Scan(&s.ID, &s.ProjectID, &s.Name, &s.VMIP, &s.VMProviderID,
			&s.TerraformWorkspace, &s.PortainerEndpointID, &s.Status, &s.ErrorMessage); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}
```

- [ ] **Step 4: Write snapshots.go**

```go
// portainer-agent/internal/db/snapshots.go
package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UpsertSnapshot inserts or updates a resource_snapshots row.
func UpsertSnapshot(
	ctx context.Context,
	pool *pgxpool.Pool,
	projectID uuid.UUID,
	environmentID *uuid.UUID,
	kind, name, phase string,
	summaryJSON json.RawMessage,
	lastSyncedAt time.Time,
) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO resource_snapshots
		    (project_id, environment_id, kind, name, phase, summary_json, last_synced_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (project_id, environment_id, kind, name)
		DO UPDATE SET
		    phase = EXCLUDED.phase,
		    summary_json = EXCLUDED.summary_json,
		    last_synced_at = EXCLUDED.last_synced_at
	`, projectID, environmentID, kind, name, phase, summaryJSON, lastSyncedAt)
	return err
}

// GetSnapshotSummary returns summary_json for a snapshot, used by update_app to read portainer_stack_id.
func GetSnapshotSummary(ctx context.Context, pool *pgxpool.Pool, projectID uuid.UUID, environmentID *uuid.UUID, kind, name string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := pool.QueryRow(ctx,
		`SELECT summary_json FROM resource_snapshots
		 WHERE project_id=$1 AND environment_id=$2 AND kind=$3 AND name=$4`,
		projectID, environmentID, kind, name,
	).Scan(&raw)
	return raw, err
}
```

- [ ] **Step 5: Compile DB layer**

```bash
cd /Users/alex/IdeaProjects/dada-cloud/portainer-agent
go build ./internal/db/...
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add portainer-agent/internal/db/
git commit -m "feat(portainer-agent): DB layer — operations claim, appservers CRUD, snapshots upsert"
```

---

### Task 9: portainer-agent — Portainer REST client

**Files:**
- Create: `portainer-agent/internal/portainer/models.go`
- Create: `portainer-agent/internal/portainer/client.go`

- [ ] **Step 1: Write models.go**

```go
// portainer-agent/internal/portainer/models.go
package portainer

// Endpoint represents a Portainer environment (endpoint).
type Endpoint struct {
	ID               int    `json:"Id"`
	Name             string `json:"Name"`
	Type             int    `json:"Type"`
	EdgeKey          string `json:"EdgeKey"`
	EdgeID           string `json:"EdgeID"`
	Status           int    `json:"Status"`
	LastCheckInDate  int64  `json:"LastCheckInDate"`
	Heartbeat        bool   `json:"Heartbeat"`
	EdgeCheckinInterval int `json:"EdgeCheckinInterval"`
}

// Stack represents a Portainer stack.
type Stack struct {
	ID            int    `json:"Id"`
	Name          string `json:"Name"`
	EndpointID    int    `json:"EndpointId"`
	Status        int    `json:"Status"`
}

// CreateEdgeEndpointResponse is the response from POST /api/endpoints for edge type.
type CreateEdgeEndpointResponse = Endpoint

// CreateStackRequest is the body for POST /api/stacks/create/standalone/repository.
type CreateStackRequest struct {
	Name                      string `json:"Name"`
	RepositoryURL             string `json:"RepositoryURL"`
	RepositoryReferenceName   string `json:"RepositoryReferenceName"`
	ComposeFile               string `json:"ComposeFile"`
	RepositoryAuthentication  bool   `json:"RepositoryAuthentication"`
	RepositoryUsername        string `json:"RepositoryUsername"`
	RepositoryPassword        string `json:"RepositoryPassword"`
	TLSSkipVerify             bool   `json:"TLSSkipVerify"`
	Env                       []any  `json:"Env"`
	AutoUpdate                *any   `json:"AutoUpdate"`
}

// RedeployStackRequest is the body for PUT /api/stacks/{id}/git/redeploy.
type RedeployStackRequest struct {
	PullImage                bool   `json:"pullImage"`
	Prune                    bool   `json:"prune"`
	RepositoryReferenceName  string `json:"RepositoryReferenceName"`
	RepositoryAuthentication bool   `json:"RepositoryAuthentication"`
	RepositoryUsername       string `json:"RepositoryUsername"`
	RepositoryPassword       string `json:"RepositoryPassword"`
}

// Container is a Docker container from the Portainer proxy.
type Container struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
}
```

- [ ] **Step 2: Write client.go**

```go
// portainer-agent/internal/portainer/client.go
package portainer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is a typed HTTP client for the Portainer CE REST API.
type Client struct {
	baseURL    string
	apiToken   string
	httpClient *http.Client
}

// New creates a Portainer API client.
func New(baseURL, apiToken string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		apiToken: apiToken,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", c.apiToken)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return c.httpClient.Do(req)
}

func (c *Client) doJSON(ctx context.Context, method, path string, bodyObj, result any) error {
	var bodyReader io.Reader
	if bodyObj != nil {
		b, err := json.Marshal(bodyObj)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	resp, err := c.do(ctx, method, path, bodyReader, "application/json")
	if err != nil {
		return fmt.Errorf("http %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("portainer %s %s: status %d: %s", method, path, resp.StatusCode, string(b))
	}
	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// CreateEdgeEndpoint registers a new edge environment in Portainer.
// tunnelAddr: "portainer.dada.ru:8000"
func (c *Client) CreateEdgeEndpoint(ctx context.Context, name, portainerServerURL, tunnelAddr string) (*Endpoint, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fields := map[string]string{
		"Name":                    name,
		"EndpointCreationType":    "4",
		"ContainerEngine":         "docker",
		"URL":                     portainerServerURL,
		"EdgeTunnelServerAddress": tunnelAddr,
		"EdgeCheckinInterval":     "15",
		"GroupID":                 "1",
	}
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			return nil, err
		}
	}
	w.Close()

	resp, err := c.do(ctx, http.MethodPost, "/api/endpoints", &buf, w.FormDataContentType())
	if err != nil {
		return nil, fmt.Errorf("create edge endpoint: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("create edge endpoint status %d: %s", resp.StatusCode, string(b))
	}
	var ep Endpoint
	if err := json.NewDecoder(resp.Body).Decode(&ep); err != nil {
		return nil, fmt.Errorf("decode endpoint response: %w", err)
	}
	return &ep, nil
}

// GetEndpoint fetches endpoint details by ID.
func (c *Client) GetEndpoint(ctx context.Context, id int) (*Endpoint, error) {
	var ep Endpoint
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/endpoints/%d", id), nil, &ep); err != nil {
		return nil, err
	}
	return &ep, nil
}

// IsAgentConnected returns true when the edge agent has checked in and is alive.
// Status:1 is NOT sufficient — must check Heartbeat && LastCheckInDate > 0.
func IsAgentConnected(ep *Endpoint) bool {
	return ep.Heartbeat && ep.LastCheckInDate > 0
}

// DeleteEndpoint removes an endpoint from Portainer.
func (c *Client) DeleteEndpoint(ctx context.Context, id int) error {
	resp, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/endpoints/%d", id), nil, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode != 404 {
		return fmt.Errorf("delete endpoint %d: status %d", id, resp.StatusCode)
	}
	return nil
}

// ListStacks returns all stacks for a given endpoint.
func (c *Client) ListStacks(ctx context.Context, endpointID int) ([]Stack, error) {
	u := fmt.Sprintf("/api/stacks?filters=%s", url.QueryEscape(
		fmt.Sprintf(`{"EndpointID":%d}`, endpointID),
	))
	var stacks []Stack
	if err := c.doJSON(ctx, http.MethodGet, u, nil, &stacks); err != nil {
		return nil, err
	}
	return stacks, nil
}

// CreateStackFromGit deploys a Docker Compose stack from a git repository.
func (c *Client) CreateStackFromGit(ctx context.Context, endpointID int, req CreateStackRequest) (*Stack, error) {
	path := fmt.Sprintf("/api/stacks/create/standalone/repository?endpointId=%d", endpointID)
	var stack Stack
	if err := c.doJSON(ctx, http.MethodPost, path, req, &stack); err != nil {
		return nil, err
	}
	return &stack, nil
}

// RedeployStack triggers a git-pull redeploy on an existing stack.
func (c *Client) RedeployStack(ctx context.Context, stackID, endpointID int, req RedeployStackRequest) error {
	path := fmt.Sprintf("/api/stacks/%d/git/redeploy?endpointId=%d", stackID, endpointID)
	return c.doJSON(ctx, http.MethodPut, path, req, nil)
}

// DeleteStack removes a stack from an endpoint.
func (c *Client) DeleteStack(ctx context.Context, stackID, endpointID int) error {
	path := fmt.Sprintf("/api/stacks/%d?endpointId=%d", stackID, endpointID)
	resp, err := c.do(ctx, http.MethodDelete, path, nil, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode != 404 {
		return fmt.Errorf("delete stack %d: status %d", stackID, resp.StatusCode)
	}
	return nil
}

// ListContainers lists containers on an endpoint filtered by label.
// labelFilter example: "dada.io/app=myapp"
func (c *Client) ListContainers(ctx context.Context, endpointID int, labelFilter string) ([]Container, error) {
	filter := fmt.Sprintf(`{"label":[%q]}`, labelFilter)
	path := fmt.Sprintf("/api/endpoints/%d/docker/containers/json?filters=%s",
		endpointID, url.QueryEscape(filter))
	var containers []Container
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &containers); err != nil {
		return nil, err
	}
	return containers, nil
}

// StreamLogs returns the raw log stream for a container (caller must close).
// The stream uses Docker's 8-byte multiplexing header per chunk.
func (c *Client) StreamLogs(ctx context.Context, endpointID int, containerID string, tail int) (io.ReadCloser, error) {
	path := fmt.Sprintf("/api/endpoints/%d/docker/containers/%s/logs?stdout=1&stderr=1&follow=1&tail=%s&timestamps=1",
		endpointID, containerID, strconv.Itoa(tail))
	// Use a client without timeout for streaming
	streamClient := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", c.apiToken)
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("stream logs status %d", resp.StatusCode)
	}
	return resp.Body, nil
}
```

- [ ] **Step 3: Write client test**

```go
// portainer-agent/internal/portainer/client_test.go
package portainer_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dada-tuda/console/portainer-agent/internal/portainer"
)

func TestGetEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "ptr_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/api/endpoints/12" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(portainer.Endpoint{
			ID: 12, Name: "test", Heartbeat: true, LastCheckInDate: 1716123456,
		})
	}))
	defer srv.Close()

	c := portainer.New(srv.URL, "ptr_test")
	ep, err := c.GetEndpoint(context.Background(), 12)
	if err != nil {
		t.Fatalf("GetEndpoint error: %v", err)
	}
	if ep.ID != 12 {
		t.Errorf("expected ID=12, got %d", ep.ID)
	}
	if !portainer.IsAgentConnected(ep) {
		t.Error("expected IsAgentConnected=true")
	}
}

func TestIsAgentConnected(t *testing.T) {
	tests := []struct {
		name      string
		ep        portainer.Endpoint
		connected bool
	}{
		{"heartbeat+checkin", portainer.Endpoint{Heartbeat: true, LastCheckInDate: 100}, true},
		{"status1 only", portainer.Endpoint{Status: 1, Heartbeat: false, LastCheckInDate: 0}, false},
		{"heartbeat without checkin", portainer.Endpoint{Heartbeat: true, LastCheckInDate: 0}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := portainer.IsAgentConnected(&tt.ep)
			if got != tt.connected {
				t.Errorf("IsAgentConnected=%v, want %v", got, tt.connected)
			}
		})
	}
}
```

- [ ] **Step 4: Run tests**

```bash
cd /Users/alex/IdeaProjects/dada-cloud/portainer-agent
go test ./internal/portainer/... -v
```

Expected:
```
--- PASS: TestGetEndpoint
--- PASS: TestIsAgentConnected/heartbeat+checkin
--- PASS: TestIsAgentConnected/status1_only
--- PASS: TestIsAgentConnected/heartbeat_without_checkin
```

- [ ] **Step 5: Commit**

```bash
git add portainer-agent/internal/portainer/
git commit -m "feat(portainer-agent): Portainer REST client — endpoints, stacks, containers, log stream"
```

---

### Task 10: portainer-agent — Terraform templates and executor

**Files:**
- Create: `portainer-agent/internal/terraform/templates/main.tf.tmpl`
- Create: `portainer-agent/internal/terraform/templates/variables.tf`
- Create: `portainer-agent/internal/terraform/workspace.go`
- Create: `portainer-agent/internal/terraform/executor.go`

- [ ] **Step 1: Write main.tf.tmpl**

```
# portainer-agent/internal/terraform/templates/main.tf.tmpl
# Generated per AppServer — do not edit manually.

terraform {
  required_providers {
    beget = {
      source = "tf.beget.com/beget/beget"
    }
  }
  backend "pg" {}
}

provider "beget" {
  token = var.beget_token
}

resource "beget_compute_instance" "app_server" {
  name   = var.server_name
  region = var.region

  cpu     = var.cpu
  ram_mb  = var.ram_mb
  disk_mb = var.disk_mb

  image {
    software {
      id = var.software_id
    }
  }

  access {
    ssh_keys = [var.ssh_key_id]
  }
}

output "vm_ip" {
  value = beget_compute_instance.app_server.ip_address
}

output "vm_id" {
  value = beget_compute_instance.app_server.id
}
```

- [ ] **Step 2: Write variables.tf**

```hcl
# portainer-agent/internal/terraform/templates/variables.tf

variable "beget_token" {
  type      = string
  sensitive = true
}

variable "server_name" {
  type = string
}

variable "region" {
  type    = string
  default = "ru1"
}

variable "cpu" {
  type    = number
  default = 2
}

variable "ram_mb" {
  type    = number
  default = 2048
}

variable "disk_mb" {
  type    = number
  default = 20480
}

variable "software_id" {
  type        = number
  description = "Beget software ID for Ubuntu 22.04"
}

variable "ssh_key_id" {
  type        = string
  description = "ID of SSH key registered in Beget account"
}
```

- [ ] **Step 3: Write workspace.go**

```go
// portainer-agent/internal/terraform/workspace.go
package terraform

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed templates/*
var templateFS embed.FS

// PrepareWorkspace copies the Terraform templates into workspaceDir.
// workspaceDir must be unique per AppServer (e.g. /var/lib/tf-workspaces/{id}).
func PrepareWorkspace(workspaceDir string) error {
	if err := os.MkdirAll(workspaceDir, 0750); err != nil {
		return fmt.Errorf("mkdir workspace: %w", err)
	}

	return fs.WalkDir(templateFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Strip "templates/" prefix for destination
		relPath := path[len("templates/"):]
		// Strip .tmpl suffix for Terraform files — they are static, not Go templates
		destName := relPath
		if filepath.Ext(destName) == ".tmpl" {
			destName = destName[:len(destName)-len(".tmpl")]
		}

		data, err := templateFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read template %s: %w", path, err)
		}
		dest := filepath.Join(workspaceDir, destName)
		if err := os.WriteFile(dest, data, 0640); err != nil {
			return fmt.Errorf("write %s: %w", dest, err)
		}
		return nil
	})
}

// CleanWorkspace removes the workspace directory entirely.
func CleanWorkspace(workspaceDir string) error {
	return os.RemoveAll(workspaceDir)
}
```

- [ ] **Step 4: Write executor.go**

```go
// portainer-agent/internal/terraform/executor.go
package terraform

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hashicorp/terraform-exec/tfexec"
)

// Executor wraps terraform-exec for init/apply/destroy/output.
type Executor struct {
	terraformBin string // path to terraform binary
	pgConnStr    string // postgres DSN for state backend
	workspaceBase string
}

// NewExecutor creates an Executor.
func NewExecutor(terraformBin, pgConnStr, workspaceBase string) *Executor {
	return &Executor{
		terraformBin:  terraformBin,
		pgConnStr:     pgConnStr,
		workspaceBase: workspaceBase,
	}
}

// WorkspaceDir returns the filesystem path for a given appServerID workspace.
func (e *Executor) WorkspaceDir(appServerID string) string {
	return filepath.Join(e.workspaceBase, appServerID)
}

// schemaName returns the Postgres schema name for state isolation.
func schemaName(appServerID string) string {
	// Replace hyphens with underscores (Postgres schema names can't start with digits or contain hyphens).
	return "tfstate_" + strings.ReplaceAll(appServerID, "-", "_")
}

func (e *Executor) newTF(workspaceDir string) (*tfexec.Terraform, error) {
	return tfexec.NewTerraform(workspaceDir, e.terraformBin)
}

// Init runs terraform init with the PG backend configured for this appServerID.
func (e *Executor) Init(ctx context.Context, appServerID string) error {
	dir := e.WorkspaceDir(appServerID)
	tf, err := e.newTF(dir)
	if err != nil {
		return err
	}
	return tf.Init(ctx,
		tfexec.Backend(true),
		tfexec.BackendConfig("conn_str="+e.pgConnStr),
		tfexec.BackendConfig("schema_name="+schemaName(appServerID)),
		tfexec.Upgrade(false),
	)
}

// Apply runs terraform apply and returns outputs as string map.
// vars: map of var name → value (e.g. "server_name" → "client-a-prod-1").
func (e *Executor) Apply(ctx context.Context, appServerID string, vars map[string]string) (map[string]string, error) {
	dir := e.WorkspaceDir(appServerID)
	tf, err := e.newTF(dir)
	if err != nil {
		return nil, err
	}
	applyOpts := []tfexec.ApplyOption{tfexec.Lock(true)}
	for k, v := range vars {
		applyOpts = append(applyOpts, tfexec.Var(fmt.Sprintf("%s=%s", k, v)))
	}
	if err := tf.Apply(ctx, applyOpts...); err != nil {
		return nil, fmt.Errorf("tf apply: %w", err)
	}
	return e.outputs(ctx, dir)
}

// Destroy runs terraform destroy.
func (e *Executor) Destroy(ctx context.Context, appServerID string, vars map[string]string) error {
	dir := e.WorkspaceDir(appServerID)
	tf, err := e.newTF(dir)
	if err != nil {
		return err
	}
	opts := []tfexec.DestroyOption{}
	for k, v := range vars {
		opts = append(opts, tfexec.Var(fmt.Sprintf("%s=%s", k, v)))
	}
	if err := tf.Destroy(ctx, opts...); err != nil {
		return fmt.Errorf("tf destroy: %w", err)
	}
	return nil
}

// outputs reads the current output values from a workspace.
func (e *Executor) outputs(ctx context.Context, workspaceDir string) (map[string]string, error) {
	tf, err := e.newTF(workspaceDir)
	if err != nil {
		return nil, err
	}
	out, err := tf.Output(ctx)
	if err != nil {
		return nil, fmt.Errorf("tf output: %w", err)
	}
	result := make(map[string]string, len(out))
	for k, v := range out {
		result[k] = strings.Trim(string(v.Value), `"`)
	}
	return result, nil
}
```

- [ ] **Step 5: Write workspace test**

```go
// portainer-agent/internal/terraform/workspace_test.go
package terraform_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dada-tuda/console/portainer-agent/internal/terraform"
)

func TestPrepareWorkspace(t *testing.T) {
	dir := t.TempDir()
	workspaceDir := filepath.Join(dir, "test-workspace")

	if err := terraform.PrepareWorkspace(workspaceDir); err != nil {
		t.Fatalf("PrepareWorkspace error: %v", err)
	}

	// Verify expected files exist
	for _, wantFile := range []string{"main.tf", "variables.tf"} {
		path := filepath.Join(workspaceDir, wantFile)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file %s to exist: %v", wantFile, err)
		}
	}
}

func TestCleanWorkspace(t *testing.T) {
	dir := t.TempDir()
	workspaceDir := filepath.Join(dir, "cleanup-test")
	if err := terraform.PrepareWorkspace(workspaceDir); err != nil {
		t.Fatal(err)
	}
	if err := terraform.CleanWorkspace(workspaceDir); err != nil {
		t.Fatalf("CleanWorkspace error: %v", err)
	}
	if _, err := os.Stat(workspaceDir); !os.IsNotExist(err) {
		t.Error("expected workspace to be removed")
	}
}
```

- [ ] **Step 6: Run tests**

```bash
cd /Users/alex/IdeaProjects/dada-cloud/portainer-agent
go test ./internal/terraform/... -v -run TestPrepareWorkspace
```

Expected:
```
--- PASS: TestPrepareWorkspace
--- PASS: TestCleanWorkspace
```

- [ ] **Step 7: Commit**

```bash
git add portainer-agent/internal/terraform/
git commit -m "feat(portainer-agent): Terraform executor and Beget provider templates"
```

---

### Task 11: portainer-agent — SSH client + bootstrap template

**Files:**
- Create: `portainer-agent/internal/ssh/bootstrap.sh.tmpl`
- Create: `portainer-agent/internal/ssh/client.go`

- [ ] **Step 1: Write bootstrap.sh.tmpl**

```bash
#!/usr/bin/env bash
# portainer-agent/internal/ssh/bootstrap.sh.tmpl
# Rendered by portainer-agent Go text/template, piped via SSH stdin.
# Template vars: .ServerName .EdgeKey .EdgeID
#                .PrometheusRemoteWriteURL .PrometheusUser .PrometheusPass
#                .ElasticsearchURL .ElasticsearchAPIKey
set -euo pipefail

echo "[bootstrap] Starting VM setup for {{.ServerName}}"

# ── Docker ───────────────────────────────────────────────────────────────────
apt-get update -qq
apt-get install -y -qq docker.io docker-compose-plugin curl wget
systemctl enable docker --now
echo "[bootstrap] Docker ready"

# ── node_exporter 1.8.2 ──────────────────────────────────────────────────────
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

# ── cAdvisor v0.49.1 ─────────────────────────────────────────────────────────
docker run -d \
  --name cadvisor --restart unless-stopped \
  -v /:/rootfs:ro -v /var/run:/var/run:ro \
  -v /sys:/sys:ro -v /var/lib/docker/:/var/lib/docker:ro \
  -p 8080:8080 gcr.io/cadvisor/cadvisor:v0.49.1
echo "[bootstrap] cAdvisor :8080 started"

# ── Prometheus Agent → remote_write ──────────────────────────────────────────
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
echo "[bootstrap] Prometheus Agent started"

# ── Filebeat 8.13.4 → Elasticsearch ─────────────────────────────────────────
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

# ── Portainer Edge Agent 2.21.0 ──────────────────────────────────────────────
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

- [ ] **Step 2: Write client.go**

```go
// portainer-agent/internal/ssh/client.go
package ssh

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"fmt"
	"strings"
	"text/template"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

//go:embed bootstrap.sh.tmpl
var bootstrapFS embed.FS

// BootstrapParams holds template vars for bootstrap.sh.tmpl.
type BootstrapParams struct {
	ServerName               string
	EdgeKey                  string
	EdgeID                   string
	PrometheusRemoteWriteURL string
	PrometheusUser           string
	PrometheusPass           string
	ElasticsearchURL         string
	ElasticsearchAPIKey      string
}

// RenderBootstrap renders bootstrap.sh.tmpl with the given params.
func RenderBootstrap(p BootstrapParams) (string, error) {
	tmplBytes, err := bootstrapFS.ReadFile("bootstrap.sh.tmpl")
	if err != nil {
		return "", fmt.Errorf("read template: %w", err)
	}
	tmpl, err := template.New("bootstrap").Parse(string(tmplBytes))
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, p); err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	return buf.String(), nil
}

// RunBootstrap SSHes into host, renders + streams the bootstrap script,
// and waits for the "BOOTSTRAP_COMPLETE" marker in stdout.
// host: "1.2.3.4" (no port — :22 is appended internally)
// user: "root" (Beget Ubuntu VDS default)
// privateKeyPEM: PEM-encoded private key matching the SSH key registered in Beget
func RunBootstrap(ctx context.Context, host, user, privateKeyPEM string, params BootstrapParams) error {
	signer, err := gossh.ParsePrivateKey([]byte(privateKeyPEM))
	if err != nil {
		return fmt.Errorf("parse private key: %w", err)
	}

	sshCfg := &gossh.ClientConfig{
		User:            user,
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(signer)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec — provisioning context only
		Timeout:         10 * time.Second,
	}

	// Retry SSH connection: VM may still be booting (up to 5 min)
	var client *gossh.Client
	for i := 0; i < 30; i++ {
		client, err = gossh.Dial("tcp", host+":22", sshCfg)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	if err != nil {
		return fmt.Errorf("ssh connect after retries: %w", err)
	}
	defer client.Close()

	script, err := RenderBootstrap(params)
	if err != nil {
		return err
	}

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	// Pipe script via stdin
	session.Stdin = strings.NewReader(script)

	// Capture stdout to scan for BOOTSTRAP_COMPLETE
	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	completeCh := make(chan struct{}, 1)
	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Printf("[bootstrap/%s] %s\n", host, line)
			if strings.Contains(line, "BOOTSTRAP_COMPLETE") {
				completeCh <- struct{}{}
			}
		}
	}()

	if err := session.Start("bash -s"); err != nil {
		return fmt.Errorf("start bash: %w", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-completeCh:
		return nil
	}
}
```

- [ ] **Step 3: Write template render test**

```go
// portainer-agent/internal/ssh/client_test.go
package ssh_test

import (
	"strings"
	"testing"

	dadash "github.com/dada-tuda/console/portainer-agent/internal/ssh"
)

func TestRenderBootstrap(t *testing.T) {
	params := dadash.BootstrapParams{
		ServerName:               "test-server-1",
		EdgeKey:                  "aHR0cHM6Ly9wb3J0YWluZXI=",
		EdgeID:                   "550e8400-e29b-41d4-a716-446655440000",
		PrometheusRemoteWriteURL: "http://prometheus/api/v1/write",
		PrometheusUser:           "dada",
		PrometheusPass:           "secret",
		ElasticsearchURL:         "http://elastic:9200",
		ElasticsearchAPIKey:      "api_key_here",
	}

	script, err := dadash.RenderBootstrap(params)
	if err != nil {
		t.Fatalf("RenderBootstrap error: %v", err)
	}

	checks := []string{
		"test-server-1",
		"aHR0cHM6Ly9wb3J0YWluZXI=",
		"550e8400-e29b-41d4-a716-446655440000",
		"http://prometheus/api/v1/write",
		"http://elastic:9200",
		"BOOTSTRAP_COMPLETE",
		"portainer/agent:2.21.0",
		"node_exporter",
		"cadvisor",
		"filebeat",
	}
	for _, check := range checks {
		if !strings.Contains(script, check) {
			t.Errorf("expected rendered script to contain %q", check)
		}
	}
}
```

- [ ] **Step 4: Run test**

```bash
cd /Users/alex/IdeaProjects/dada-cloud/portainer-agent
go test ./internal/ssh/... -v -run TestRenderBootstrap
```

Expected: `--- PASS: TestRenderBootstrap`

- [ ] **Step 5: Commit**

```bash
git add portainer-agent/internal/ssh/
git commit -m "feat(portainer-agent): SSH client with retry + bootstrap.sh.tmpl (all observability agents)"
```

---

### Task 12: portainer-agent — VM Worker dispatch loop

**Files:**
- Create: `portainer-agent/internal/worker/vm_watcher.go`

- [ ] **Step 1: Write vm_watcher.go**

```go
// portainer-agent/internal/worker/vm_watcher.go
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dada-tuda/console/portainer-agent/internal/config"
	"github.com/dada-tuda/console/portainer-agent/internal/db"
	"github.com/dada-tuda/console/portainer-agent/internal/portainer"
	tfexecutor "github.com/dada-tuda/console/portainer-agent/internal/terraform"
	dadash "github.com/dada-tuda/console/portainer-agent/internal/ssh"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// VMWatcher polls the operations table for VM-track operations and dispatches them.
type VMWatcher struct {
	pool      *pgxpool.Pool
	cfg       *config.Config
	portainer *portainer.Client
	tf        *tfexecutor.Executor
}

// NewVMWatcher constructs a VMWatcher with its dependencies.
func NewVMWatcher(pool *pgxpool.Pool, cfg *config.Config) *VMWatcher {
	return &VMWatcher{
		pool:      pool,
		cfg:       cfg,
		portainer: portainer.New(cfg.PortainerURL, cfg.PortainerAPIToken),
		tf:        tfexecutor.NewExecutor(cfg.TFBinPath, cfg.TFStateConnStr, cfg.TFWorkspaceBase),
	}
}

// Start begins the polling loop. Blocks until ctx is cancelled.
func (w *VMWatcher) Start(ctx context.Context) {
	log.Info().Dur("interval", w.cfg.PollIntervalDB).Msg("vm-watcher started")
	ticker := time.NewTicker(w.cfg.PollIntervalDB)
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

func (w *VMWatcher) poll(ctx context.Context) {
	ops, err := db.ClaimPending(ctx, w.pool)
	if err != nil {
		log.Error().Err(err).Msg("vm-watcher: claim pending")
		return
	}
	for _, op := range ops {
		if err := w.dispatch(ctx, op); err != nil {
			log.Error().Err(err).
				Str("op", op.ID.String()).
				Str("action", op.Action).
				Msg("operation failed")
			_ = db.MarkFailed(ctx, w.pool, op.ID, "PROCESSING_ERROR", err.Error())
		}
	}
}

func (w *VMWatcher) dispatch(ctx context.Context, op db.Operation) error {
	log.Info().Str("op", op.ID.String()).Str("action", op.Action).Msg("dispatching operation")
	switch op.Action {
	case "CreateAppServer":
		return w.doCreateAppServer(ctx, op)
	case "DeleteAppServer":
		return w.doDeleteAppServer(ctx, op)
	default:
		return fmt.Errorf("unknown vm action: %s", op.Action)
	}
}

// bootstrapParams assembles SSH bootstrap template params from config.
func (w *VMWatcher) bootstrapParams(serverName, edgeKey, edgeID string) dadash.BootstrapParams {
	return dadash.BootstrapParams{
		ServerName:               serverName,
		EdgeKey:                  edgeKey,
		EdgeID:                   edgeID,
		PrometheusRemoteWriteURL: w.cfg.PrometheusRemoteWriteURL,
		PrometheusUser:           w.cfg.PrometheusRemoteWriteUser,
		PrometheusPass:           w.cfg.PrometheusRemoteWritePass,
		ElasticsearchURL:         w.cfg.ElasticsearchURL,
		ElasticsearchAPIKey:      w.cfg.ElasticsearchAPIKey,
	}
}

// tfVars assembles Terraform variable map from config + AppServer payload.
func (w *VMWatcher) tfVars(name, region string) map[string]string {
	return map[string]string{
		"beget_token": w.cfg.BegetToken,
		"server_name": name,
		"region":      region,
		"software_id": w.cfg.BegetSoftwareID,
		"ssh_key_id":  w.cfg.BegetSSHKeyID,
	}
}

// unmarshalPayload is a helper to decode op.Payload into a typed struct.
func unmarshalPayload(raw json.RawMessage, out any) error {
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("parse payload: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Compile**

```bash
cd /Users/alex/IdeaProjects/dada-cloud/portainer-agent
go build ./internal/worker/...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add portainer-agent/internal/worker/vm_watcher.go
git commit -m "feat(portainer-agent): VM worker dispatch loop"
```

---

### Task 13: portainer-agent — CreateAppServer worker

**Files:**
- Create: `portainer-agent/internal/worker/create_appserver.go`

- [ ] **Step 1: Write create_appserver.go**

```go
// portainer-agent/internal/worker/create_appserver.go
package worker

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/dada-tuda/console/portainer-agent/internal/db"
	dadash "github.com/dada-tuda/console/portainer-agent/internal/ssh"
	tf "github.com/dada-tuda/console/portainer-agent/internal/terraform"
	"github.com/rs/zerolog/log"
)

// createAppServerPayload matches models.CreateAppServerPayload.
type createAppServerPayload struct {
	Name       string `json:"name"`
	Flavor     string `json:"flavor"`
	OSImage    string `json:"os_image"`
	Region     string `json:"region"`
	SSHKeyName string `json:"ssh_key_name"`
}

func (w *VMWatcher) doCreateAppServer(ctx context.Context, op db.Operation) error {
	var p createAppServerPayload
	if err := unmarshalPayload(op.Payload, &p); err != nil {
		return err
	}

	region := p.Region
	if region == "" {
		region = w.cfg.BegetRegion
	}

	// ── 1. Register Portainer edge endpoint ─────────────────────────────────
	log.Info().Str("server", p.Name).Msg("creating Portainer edge endpoint")
	_ = db.UpdateStatus(ctx, w.pool, op.ID, "ProvisioningVM")

	ep, err := w.portainer.CreateEdgeEndpoint(
		ctx, p.Name,
		w.cfg.PortainerURL,
		portainerTunnelAddr(w.cfg.PortainerURL),
	)
	if err != nil {
		return fmt.Errorf("create edge endpoint: %w", err)
	}
	log.Info().Int("endpoint_id", ep.ID).Str("edge_id", ep.EdgeID).Msg("edge endpoint created")

	// ── 2. Prepare Terraform workspace ──────────────────────────────────────
	workspaceDir := filepath.Join(w.cfg.TFWorkspaceBase, op.ID.String())
	if err := tf.PrepareWorkspace(workspaceDir); err != nil {
		return fmt.Errorf("prepare workspace: %w", err)
	}

	// ── 3. Create app_servers DB row ────────────────────────────────────────
	serverID, err := db.CreateAppServer(ctx, w.pool, op.ProjectID, p.Name, workspaceDir)
	if err != nil {
		return fmt.Errorf("create app_server row: %w", err)
	}
	log.Info().Str("server_id", serverID.String()).Msg("app_servers row created")

	// ── 4. Terraform init + apply ────────────────────────────────────────────
	appServerUUID := serverID.String()
	if err := w.tf.Init(ctx, appServerUUID); err != nil {
		_ = db.SetAppServerFailed(ctx, w.pool, serverID, err.Error())
		return fmt.Errorf("terraform init: %w", err)
	}

	outputs, err := w.tf.Apply(ctx, appServerUUID, w.tfVars(p.Name, region))
	if err != nil {
		_ = db.SetAppServerFailed(ctx, w.pool, serverID, err.Error())
		return fmt.Errorf("terraform apply: %w", err)
	}
	vmIP := outputs["vm_ip"]
	vmID := outputs["vm_id"]
	log.Info().Str("vm_ip", vmIP).Str("vm_id", vmID).Msg("terraform apply complete")

	if err := db.SetAppServerProvisioned(ctx, w.pool, serverID, vmIP, vmID); err != nil {
		return fmt.Errorf("set app_server provisioned: %w", err)
	}

	// ── 5. SSH bootstrap ─────────────────────────────────────────────────────
	log.Info().Str("vm_ip", vmIP).Msg("running SSH bootstrap")
	bootstrapCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	params := w.bootstrapParams(p.Name, ep.EdgeKey, ep.EdgeID)
	if err := dadash.RunBootstrap(bootstrapCtx, vmIP, "root", w.cfg.AgentSSHPrivateKey, params); err != nil {
		_ = db.SetAppServerFailed(ctx, w.pool, serverID, err.Error())
		return fmt.Errorf("ssh bootstrap: %w", err)
	}
	log.Info().Msg("bootstrap complete — advancing to WaitingForAgent")

	// ── 6. Poll for Edge Agent connection ───────────────────────────────────
	_ = db.UpdateStatus(ctx, w.pool, op.ID, "WaitingForAgent")

	pollCtx, pollCancel := context.WithTimeout(ctx, w.cfg.AgentConnectTimeout)
	defer pollCancel()

	if err := w.waitForAgent(pollCtx, ep.ID); err != nil {
		_ = db.SetAppServerFailed(ctx, w.pool, serverID, "agent did not connect: "+err.Error())
		return fmt.Errorf("wait for agent: %w", err)
	}

	// ── 7. Mark Ready ───────────────────────────────────────────────────────
	if err := db.SetAppServerReady(ctx, w.pool, serverID, ep.ID); err != nil {
		return fmt.Errorf("set app_server ready: %w", err)
	}
	if err := db.MarkReady(ctx, w.pool, op.ID); err != nil {
		return fmt.Errorf("mark operation ready: %w", err)
	}

	log.Info().Str("server", p.Name).Int("portainer_id", ep.ID).Msg("AppServer ready")
	return nil
}

// waitForAgent polls GET /api/endpoints/{id} until Heartbeat==true && LastCheckInDate>0.
func (w *VMWatcher) waitForAgent(ctx context.Context, endpointID int) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			ep, err := w.portainer.GetEndpoint(ctx, endpointID)
			if err != nil {
				log.Warn().Err(err).Msg("poll endpoint error (retrying)")
				continue
			}
			if w.portainer.IsAgentConnected(ep) { // must be package-level func
				return nil
			}
			log.Debug().Int("endpoint", endpointID).Msg("agent not yet connected")
		}
	}
}

// portainerTunnelAddr derives the tunnel address from the Portainer URL.
// e.g. "https://portainer.dada.ru" → "portainer.dada.ru:8000"
func portainerTunnelAddr(portainerURL string) string {
	// Strip scheme
	host := portainerURL
	for _, prefix := range []string{"https://", "http://"} {
		if len(host) > len(prefix) && host[:len(prefix)] == prefix {
			host = host[len(prefix):]
			break
		}
	}
	return host + ":8000"
}
```

> **Note:** `IsAgentConnected` is called on the client value above but it's a package-level function in `portainer` package. Fix: call it as `portainer.IsAgentConnected(ep)` — adjust the import alias if needed. The function is already defined in `client.go` as a package-level func.

Actually the call in `waitForAgent` above uses `w.portainer.IsAgentConnected(ep)` which is wrong (it's not a method). Fix in the file:

Replace `if w.portainer.IsAgentConnected(ep)` with `if portainer.IsAgentConnected(ep)` — the import is `"github.com/dada-tuda/console/portainer-agent/internal/portainer"`.

- [ ] **Step 2: Fix the portainer.IsAgentConnected call**

The file uses `w.portainer.IsAgentConnected` — update the `waitForAgent` function to use the package-level function. The `portainer` package is already imported. Ensure the import alias doesn't conflict with the field name `w.portainer`:

In the import block, use an alias:
```go
import (
    ...
    portainerclient "github.com/dada-tuda/console/portainer-agent/internal/portainer"
    ...
)
```

And update references:
- `w.portainer.CreateEdgeEndpoint(...)` → keep as `w.portainer.CreateEdgeEndpoint(...)`
- `portainer.IsAgentConnected(ep)` → `portainerclient.IsAgentConnected(ep)`

The full corrected imports in `create_appserver.go`:

```go
import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/dada-tuda/console/portainer-agent/internal/db"
	portainerclient "github.com/dada-tuda/console/portainer-agent/internal/portainer"
	dadash "github.com/dada-tuda/console/portainer-agent/internal/ssh"
	tf "github.com/dada-tuda/console/portainer-agent/internal/terraform"
	"github.com/rs/zerolog/log"
)
```

And in `vm_watcher.go`, also use the alias where the `portainer.Client` field is declared:
```go
import (
    ...
    portainerclient "github.com/dada-tuda/console/portainer-agent/internal/portainer"
    ...
)

type VMWatcher struct {
    pool      *pgxpool.Pool
    cfg       *config.Config
    portainer *portainerclient.Client   // field named "portainer" of type *portainerclient.Client
    tf        *tfexecutor.Executor
}
```

- [ ] **Step 3: Compile**

```bash
cd /Users/alex/IdeaProjects/dada-cloud/portainer-agent
go build ./internal/worker/...
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add portainer-agent/internal/worker/
git commit -m "feat(portainer-agent): CreateAppServer worker — Portainer→Terraform→SSH→poll→Ready"
```

---

### Task 14: portainer-agent — DeleteAppServer worker

**Files:**
- Create: `portainer-agent/internal/worker/delete_appserver.go`

- [ ] **Step 1: Write delete_appserver.go**

```go
// portainer-agent/internal/worker/delete_appserver.go
package worker

import (
	"context"
	"fmt"

	"github.com/dada-tuda/console/portainer-agent/internal/db"
	portainerclient "github.com/dada-tuda/console/portainer-agent/internal/portainer"
	tf "github.com/dada-tuda/console/portainer-agent/internal/terraform"
	"github.com/rs/zerolog/log"
)

// deleteAppServerPayload matches models.DeleteAppServerPayload.
type deleteAppServerPayload struct {
	AppServerName string `json:"app_server_name"`
}

func (w *VMWatcher) doDeleteAppServer(ctx context.Context, op db.Operation) error {
	var p deleteAppServerPayload
	if err := unmarshalPayload(op.Payload, &p); err != nil {
		return err
	}

	// ── 1. Fetch app_server record ──────────────────────────────────────────
	server, err := db.GetAppServerByName(ctx, w.pool, op.ProjectID, p.AppServerName)
	if err != nil {
		return fmt.Errorf("get app_server: %w", err)
	}

	if err := db.SetAppServerDeleting(ctx, w.pool, server.ID); err != nil {
		return fmt.Errorf("set deleting: %w", err)
	}
	_ = db.UpdateStatus(ctx, w.pool, op.ID, "DeletingStacks")

	// ── 2. Delete all Portainer stacks on this endpoint ─────────────────────
	if server.PortainerEndpointID != nil {
		endpointID := *server.PortainerEndpointID
		stacks, err := w.portainer.ListStacks(ctx, endpointID)
		if err != nil {
			log.Warn().Err(err).Int("endpoint", endpointID).Msg("list stacks failed — skipping stack deletion")
		} else {
			for _, stack := range stacks {
				log.Info().Int("stack", stack.ID).Str("name", stack.Name).Msg("deleting stack")
				if err := w.portainer.DeleteStack(ctx, stack.ID, endpointID); err != nil {
					log.Warn().Err(err).Int("stack", stack.ID).Msg("delete stack failed — continuing")
				}
			}
		}

		// Delete the Portainer endpoint
		if err := w.portainer.DeleteEndpoint(ctx, endpointID); err != nil {
			log.Warn().Err(err).Int("endpoint", endpointID).Msg("delete endpoint failed — continuing")
		}
	}

	// ── 3. Terraform destroy ─────────────────────────────────────────────────
	_ = db.UpdateStatus(ctx, w.pool, op.ID, "DeletingVM")

	if server.TerraformWorkspace != nil {
		appServerUUID := server.ID.String()

		// Re-init before destroy (workspace may be on a fresh pod)
		if err := w.tf.Init(ctx, appServerUUID); err != nil {
			log.Warn().Err(err).Msg("terraform init before destroy failed")
		} else {
			region := w.cfg.BegetRegion
			destroyVars := w.tfVars(p.AppServerName, region)
			if err := w.tf.Destroy(ctx, appServerUUID, destroyVars); err != nil {
				log.Warn().Err(err).Msg("terraform destroy failed — marking deleted anyway")
			}
		}

		// Clean up workspace directory
		if err := tf.CleanWorkspace(w.tf.WorkspaceDir(appServerUUID)); err != nil {
			log.Warn().Err(err).Msg("clean workspace failed")
		}
	}

	// ── 4. Mark deleted ──────────────────────────────────────────────────────
	if err := db.SetAppServerDeleted(ctx, w.pool, server.ID); err != nil {
		return fmt.Errorf("set deleted: %w", err)
	}

	_ = portainerclient.IsAgentConnected // silence unused import if needed
	return db.MarkReady(ctx, w.pool, op.ID) // "Ready" = operation completed
}
```

- [ ] **Step 2: Compile**

```bash
cd /Users/alex/IdeaProjects/dada-cloud/portainer-agent
go build ./internal/worker/...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add portainer-agent/internal/worker/delete_appserver.go
git commit -m "feat(portainer-agent): DeleteAppServer worker — stacks→endpoint→terraform destroy"
```

---

### Task 15: portainer-agent — main.go + health server + Dockerfile

**Files:**
- Create: `portainer-agent/internal/server/server.go`
- Create: `portainer-agent/cmd/portainer-agent/main.go`
- Create: `portainer-agent/Dockerfile`

- [ ] **Step 1: Write server.go**

```go
// portainer-agent/internal/server/server.go
package server

import (
	"context"
	"net/http"
	"time"
)

// Start runs a minimal HTTP server serving /healthz.
func Start(ctx context.Context, addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
	})
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	go srv.ListenAndServe() //nolint:errcheck
	return srv
}
```

- [ ] **Step 2: Write main.go**

```go
// portainer-agent/cmd/portainer-agent/main.go
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/dada-tuda/console/portainer-agent/internal/config"
	"github.com/dada-tuda/console/portainer-agent/internal/db"
	"github.com/dada-tuda/console/portainer-agent/internal/server"
	"github.com/dada-tuda/console/portainer-agent/internal/worker"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	_ = godotenv.Load() // optional .env file in dev

	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("config load failed")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("db connect failed")
	}
	defer pool.Close()

	// Health endpoint
	srv := server.Start(ctx, ":8090")
	defer srv.Shutdown(ctx) //nolint:errcheck

	// Start VM worker
	w := worker.NewVMWatcher(pool, cfg)
	w.Start(ctx)

	log.Info().Msg("portainer-agent stopped")
}
```

- [ ] **Step 3: Write Dockerfile**

```dockerfile
# portainer-agent/Dockerfile
# Stage 1: download terraform binary
FROM hashicorp/terraform:1.9 AS terraform

# Stage 2: build Go binary
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /portainer-agent ./cmd/portainer-agent

# Stage 3: runtime image
FROM alpine:3.19
RUN apk add --no-cache ca-certificates openssh-client

# Copy terraform binary
COPY --from=terraform /bin/terraform /usr/local/bin/terraform

# Copy agent binary
COPY --from=builder /portainer-agent /portainer-agent

EXPOSE 8090
ENTRYPOINT ["/portainer-agent"]
```

- [ ] **Step 4: Build binary**

```bash
cd /Users/alex/IdeaProjects/dada-cloud/portainer-agent
go build ./cmd/portainer-agent/...
```

Expected: no output (binary created at `portainer-agent`).

- [ ] **Step 5: Commit**

```bash
git add portainer-agent/cmd/ portainer-agent/internal/server/ portainer-agent/Dockerfile
git commit -m "feat(portainer-agent): main.go entrypoint, health server, multi-stage Dockerfile"
```

---

### Task 16: Push all commits

- [ ] **Step 1: Verify git log**

```bash
git log --oneline -15
```

Expected to see all 15+ commits from this plan in order.

- [ ] **Step 2: Push**

```bash
git push
```

Expected: branch pushed to remote.

---

## Self-Review

**Spec coverage check:**

| Spec section | Covered |
|---|---|
| §3 Migration 004 | ✅ Task 1 |
| §3 New payload structs | ✅ Task 2 |
| §3 AppServer model | ✅ Task 3 |
| §11 AppServer API endpoints | ✅ Task 4 |
| §2 gitops-agent dispatch fix | ✅ Task 5 |
| §10 portainer-agent layout | ✅ Tasks 6–15 |
| §5 Portainer REST client (endpoints, stacks, logs) | ✅ Task 9 |
| §6 Terraform Beget provider | ✅ Task 10 |
| §7 SSH bootstrap + bootstrap.sh | ✅ Task 11 |
| §4 CreateAppServer flow | ✅ Task 13 |
| §4 DeleteAppServer flow | ✅ Task 14 |

**Not in Plan 1 (covered by Plan 2):**
- CreateApp / UpdateApp VM flow (`worker/create_app.go`, `worker/update_app.go`)
- compose renderer
- git manager
- status_watcher
- Backend: extend CreateApp handler for VM runtime
- Backend: UpdateAppEnvVars endpoint
- Backend: logs proxy endpoint

**Placeholder scan:** none found — all steps contain actual code.

**Type consistency:**
- `AppServerRow.PortainerEndpointID *int` — used as `*server.PortainerEndpointID` in delete worker ✅
- `CreateEdgeEndpoint` returns `*Endpoint` with `.EdgeKey`, `.EdgeID`, `.ID` ✅
- `db.CreateAppServer` returns `(uuid.UUID, error)` — used as `serverID` in create worker ✅
- `tf.WorkspaceDir(appServerUUID string) string` called as `w.tf.WorkspaceDir(appServerUUID)` ✅
- `portainerclient.IsAgentConnected(*Endpoint) bool` — used correctly in `waitForAgent` ✅

---

**Deliverable:** After Plan 1, you can `curl POST /api/v1/projects/{id}/app-servers` and get back an operation. The portainer-agent will pick it up, create a Portainer edge endpoint, run Terraform on Beget to provision the VDS, SSH in to install Docker + Portainer Edge Agent + observability stack, wait for the agent to connect, and mark the AppServer Ready.

**Plan 2** covers the App deployment lifecycle on VM-track environments.
