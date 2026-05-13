# v2.2 Domain Management — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `PublicApi` Crossplane CRD support — per-domain creation for an App. One domain = one `PublicApi` resource. Route is always `/` → `/**` (domain-based routing). Backend follows CreateApp pattern end-to-end: renderer → worker → API → frontend domain list on App detail page.

**Architecture:** New typed action `CreatePublicApi` follows the Operation model. Worker renders `PublicApi` manifest and commits to Git state repo. `upstream.serviceName` = appName, `servicePort` = from App snapshot. `dns.target` = cluster LB IP from config. PublicApi name = FQDN with dots replaced by dashes.

**Tech Stack:** Go 1.22, Gin, pgx/v5, go-git, Next.js 14 App Router, TypeScript, Tailwind CSS

---

## Context: Where Each Piece Lives

```
backend/
  internal/
    config/
      config.go              ← add ClusterLBIP field
    gitwriter/
      publicapi_renderer.go  ← CREATE
      publicapi_renderer_test.go ← CREATE
    models/
      operation.go           ← add CreatePublicApiPayload
    api/
      endpoints.go           ← CREATE (list + create handlers)
      router.go              ← register routes
  tests/golden/
    publicapi/               ← CREATE
      basic.yaml
      no-auth-no-swagger.yaml
frontend/
  lib/
    types.ts                 ← add PublicApi types
    api.ts                   ← add endpointsApi
  app/(console)/projects/[projectId]/apps/[appName]/
    page.tsx                 ← add Domains section + Create Domain modal
```

---

## Task 1: Config — ClusterLBIP

**Files:**
- Modify: `backend/internal/config/config.go`

### Step 1: Add ClusterLBIP to Config struct and Load()

```go
ClusterLBIP string
```

In `Load()`:
```go
ClusterLBIP: getEnv("CLUSTER_LB_IP", "93.189.231.60"),
```

### Step 2: Verify compilation

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend && go build ./...
```

### Step 3: Commit

```bash
git add backend/internal/config/config.go
git commit -m "feat(config): add CLUSTER_LB_IP for PublicApi DNS target"
```

---

## Task 2: PublicApi YAML renderer + golden tests

**Files:**
- Create: `backend/internal/gitwriter/publicapi_renderer.go`
- Create: `backend/internal/gitwriter/publicapi_renderer_test.go`
- Create: `backend/tests/golden/publicapi/basic.yaml`
- Create: `backend/tests/golden/publicapi/no-auth-no-swagger.yaml`

### Step 1: Create golden file — basic (platform-jwt auth + swagger)

Create `backend/tests/golden/publicapi/basic.yaml`:
```yaml
apiVersion: platform.dada-tuda.ru/v1alpha1
kind: PublicApi
metadata:
  name: api-myservice-ru
  namespace: client-a-prod
  labels:
    dada.io/project: client-a
    dada.io/environment: prod
    dada.io/operation: op-test-1234
spec:
  upstream:
    serviceName: profi-backend
    servicePort: 3000
  route:
    prefix: /
    pathPattern: /**
    stripPrefix: false
  auth:
    enabled: true
    scheme: platform-jwt
    scopes:
      - api.read
      - api.write
  swagger:
    enabled: true
    path: /v3/api-docs
    title: My Service API
  dns:
    enabled: true
    fqdn: api.myservice.ru
    recordType: A
    target: 93.189.231.60
```

### Step 2: Create golden file — no auth, no swagger

Create `backend/tests/golden/publicapi/no-auth-no-swagger.yaml`:
```yaml
apiVersion: platform.dada-tuda.ru/v1alpha1
kind: PublicApi
metadata:
  name: app-internal-ru
  namespace: internal-prod
  labels:
    dada.io/project: internal
    dada.io/environment: prod
    dada.io/operation: op-test-5678
spec:
  upstream:
    serviceName: codex-lb
    servicePort: 8080
  route:
    prefix: /
    pathPattern: /**
    stripPrefix: false
  auth:
    enabled: false
    scheme: none
  swagger:
    enabled: false
    path: /v3/api-docs
    title: codex-lb
  dns:
    enabled: true
    fqdn: app.internal.ru
    recordType: A
    target: 93.189.231.60
```

### Step 3: Write the failing test

Create `backend/internal/gitwriter/publicapi_renderer_test.go`:
```go
package gitwriter_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dada-tuda/console/backend/internal/gitwriter"
)

func publicapiGoldenPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "tests", "golden", "publicapi", name)
}

func readPublicapiGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(publicapiGoldenPath(name))
	if err != nil {
		t.Fatalf("reading golden file %s: %v", name, err)
	}
	return string(b)
}

func TestRenderPublicApi_Basic(t *testing.T) {
	spec := gitwriter.PublicApiSpec{
		Name:        "api-myservice-ru",
		Namespace:   "client-a-prod",
		ProjectSlug: "client-a",
		EnvSlug:     "prod",
		ServiceName: "profi-backend",
		ServicePort: 3000,
		FQDN:        "api.myservice.ru",
		LBTarget:    "93.189.231.60",
		AuthEnabled: true,
		AuthScheme:  "platform-jwt",
		AuthScopes:  []string{"api.read", "api.write"},
		SwaggerEnabled: true,
		SwaggerPath:    "/v3/api-docs",
		SwaggerTitle:   "My Service API",
		OperationID: "op-test-1234",
	}
	got, err := gitwriter.RenderPublicApi(spec)
	if err != nil {
		t.Fatalf("RenderPublicApi: %v", err)
	}
	want := readPublicapiGolden(t, "basic.yaml")
	if got != want {
		t.Errorf("rendered YAML does not match basic.yaml\n\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestRenderPublicApi_NoAuthNoSwagger(t *testing.T) {
	spec := gitwriter.PublicApiSpec{
		Name:        "app-internal-ru",
		Namespace:   "internal-prod",
		ProjectSlug: "internal",
		EnvSlug:     "prod",
		ServiceName: "codex-lb",
		ServicePort: 8080,
		FQDN:        "app.internal.ru",
		LBTarget:    "93.189.231.60",
		AuthEnabled: false,
		AuthScheme:  "none",
		AuthScopes:  nil,
		SwaggerEnabled: false,
		SwaggerPath:    "/v3/api-docs",
		SwaggerTitle:   "codex-lb",
		OperationID: "op-test-5678",
	}
	got, err := gitwriter.RenderPublicApi(spec)
	if err != nil {
		t.Fatalf("RenderPublicApi: %v", err)
	}
	want := readPublicapiGolden(t, "no-auth-no-swagger.yaml")
	if got != want {
		t.Errorf("rendered YAML does not match no-auth-no-swagger.yaml\n\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestPublicApiGitPath(t *testing.T) {
	cases := []struct {
		project, env, app, name, want string
	}{
		{"client-a", "prod", "profi-backend", "api-myservice-ru",
			"clusters/beget-prod/projects/client-a/environments/prod/apps/profi-backend/publicapi-api-myservice-ru.yaml"},
		{"internal", "prod", "codex-lb", "app-internal-ru",
			"clusters/beget-prod/projects/internal/environments/prod/apps/codex-lb/publicapi-app-internal-ru.yaml"},
	}
	for _, tc := range cases {
		got := gitwriter.PublicApiGitPath(tc.project, tc.env, tc.app, tc.name)
		if got != tc.want {
			t.Errorf("PublicApiGitPath(%q,%q,%q,%q) = %q, want %q",
				tc.project, tc.env, tc.app, tc.name, got, tc.want)
		}
	}
}

func TestFQDNToName(t *testing.T) {
	cases := []struct{ fqdn, want string }{
		{"api.myservice.ru", "api-myservice-ru"},
		{"app.internal.ru", "app-internal-ru"},
		{"payments.dada-tuda.ru", "payments-dada-tuda-ru"},
	}
	for _, tc := range cases {
		got := gitwriter.FQDNToName(tc.fqdn)
		if got != tc.want {
			t.Errorf("FQDNToName(%q) = %q, want %q", tc.fqdn, got, tc.want)
		}
	}
}
```

### Step 4: Run test — verify it fails

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend && go test ./internal/gitwriter/... -v -run TestRenderPublicApi
```
Expected: compile error — `gitwriter.PublicApiSpec` undefined

### Step 5: Implement the renderer

Create `backend/internal/gitwriter/publicapi_renderer.go`:
```go
package gitwriter

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// PublicApiSpec holds parameters for rendering a PublicApi manifest.
type PublicApiSpec struct {
	Name        string
	Namespace   string
	ProjectSlug string
	EnvSlug     string
	ServiceName string
	ServicePort int
	FQDN        string
	LBTarget    string
	AuthEnabled    bool
	AuthScheme     string
	AuthScopes     []string
	SwaggerEnabled bool
	SwaggerPath    string
	SwaggerTitle   string
	OperationID string
}

var publicApiTemplate = template.Must(template.New("publicapi").Parse(`apiVersion: platform.dada-tuda.ru/v1alpha1
kind: PublicApi
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
  labels:
    dada.io/project: {{ .ProjectSlug }}
    dada.io/environment: {{ .EnvSlug }}
    dada.io/operation: {{ .OperationID }}
spec:
  upstream:
    serviceName: {{ .ServiceName }}
    servicePort: {{ .ServicePort }}
  route:
    prefix: /
    pathPattern: /**
    stripPrefix: false
  auth:
    enabled: {{ .AuthEnabled }}
    scheme: {{ .AuthScheme }}{{ if and .AuthEnabled .AuthScopes }}
    scopes:{{ range .AuthScopes }}
      - {{ . }}{{ end }}{{ end }}
  swagger:
    enabled: {{ .SwaggerEnabled }}
    path: {{ .SwaggerPath }}
    title: {{ .SwaggerTitle }}
  dns:
    enabled: true
    fqdn: {{ .FQDN }}
    recordType: A
    target: {{ .LBTarget }}
`))

// RenderPublicApi generates the YAML manifest for a PublicApi CRD.
func RenderPublicApi(spec PublicApiSpec) (string, error) {
	var buf bytes.Buffer
	if err := publicApiTemplate.Execute(&buf, spec); err != nil {
		return "", fmt.Errorf("rendering PublicApi template: %w", err)
	}
	return buf.String(), nil
}

// PublicApiGitPath returns the canonical Git path for a PublicApi manifest.
func PublicApiGitPath(projectSlug, envSlug, appName, publicApiName string) string {
	return fmt.Sprintf("clusters/beget-prod/projects/%s/environments/%s/apps/%s/publicapi-%s.yaml",
		projectSlug, envSlug, appName, publicApiName)
}

// FQDNToName converts a FQDN to a valid Kubernetes resource name.
// Example: "api.myservice.ru" → "api-myservice-ru"
func FQDNToName(fqdn string) string {
	return strings.ReplaceAll(fqdn, ".", "-")
}
```

### Step 6: Run tests — verify they pass

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend && go test ./internal/gitwriter/... -v
```
Expected: all tests PASS

### Step 7: Commit

```bash
git add backend/internal/gitwriter/publicapi_renderer.go \
        backend/internal/gitwriter/publicapi_renderer_test.go \
        backend/tests/golden/publicapi/
git commit -m "feat(gitwriter): add PublicApi CRD renderer and golden tests"
```

---

## Task 3: Payload type + API handler

**Files:**
- Modify: `backend/internal/models/operation.go`
- Create: `backend/internal/api/endpoints.go`
- Modify: `backend/internal/api/router.go`

### Step 1: Add payload type to operation.go

Append after `DeployImageVersionPayload`:
```go
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
```

### Step 2: Create endpoints.go

Create `backend/internal/api/endpoints.go`:
```go
package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dada-tuda/console/backend/internal/auth"
	"github.com/dada-tuda/console/backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ListEndpoints returns all PublicApi resources for an app in a project environment.
func (h *Handler) ListEndpoints(c *gin.Context) {
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
	envID, err := uuid.Parse(c.Param("envId"))
	if err != nil {
		respondNotFound(c)
		return
	}
	appName := c.Param("appName")

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
		`SELECT id, project_id, environment_id, kind, name, phase, summary_json, last_synced_at
		 FROM resource_snapshots
		 WHERE project_id = $1 AND environment_id = $2 AND kind = 'PublicApi'
		   AND summary_json->>'app_name' = $3
		 ORDER BY name`,
		projectID, envID, appName,
	)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to query endpoints")
		return
	}
	defer rows.Close()

	var endpoints []models.ResourceSnapshot
	for rows.Next() {
		var rs models.ResourceSnapshot
		if err := rows.Scan(
			&rs.ID, &rs.ProjectID, &rs.EnvironmentID, &rs.Kind, &rs.Name,
			&rs.Phase, &rs.SummaryJSON, &rs.LastSyncedAt,
		); err != nil {
			respondError(c, http.StatusInternalServerError, "failed to scan endpoint")
			return
		}
		endpoints = append(endpoints, rs)
	}
	if endpoints == nil {
		endpoints = []models.ResourceSnapshot{}
	}
	c.JSON(http.StatusOK, gin.H{"endpoints": endpoints})
}

type createEndpointRequest struct {
	FQDN           string   `json:"fqdn"`
	AuthEnabled    bool     `json:"auth_enabled"`
	AuthScheme     string   `json:"auth_scheme"`
	AuthScopes     []string `json:"auth_scopes"`
	SwaggerEnabled bool     `json:"swagger_enabled"`
	SwaggerPath    string   `json:"swagger_path"`
	SwaggerTitle   string   `json:"swagger_title"`
}

// CreateEndpoint enqueues a CreatePublicApi operation.
func (h *Handler) CreateEndpoint(c *gin.Context) {
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
	envID, err := uuid.Parse(c.Param("envId"))
	if err != nil {
		respondNotFound(c)
		return
	}
	appName := c.Param("appName")

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

	var req createEndpointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	// Validate FQDN
	if req.FQDN == "" {
		respondError(c, http.StatusBadRequest, "fqdn is required")
		return
	}
	if !strings.Contains(req.FQDN, ".") {
		respondError(c, http.StatusBadRequest, "fqdn must be a valid domain name")
		return
	}

	// Defaults
	if req.AuthScheme == "" {
		req.AuthScheme = "none"
		req.AuthEnabled = false
	}
	if req.SwaggerPath == "" {
		req.SwaggerPath = "/v3/api-docs"
	}
	if req.SwaggerTitle == "" {
		req.SwaggerTitle = appName
	}

	validSchemes := map[string]bool{"none": true, "platform-jwt": true, "api-key": true, "internal": true}
	if !validSchemes[req.AuthScheme] {
		respondError(c, http.StatusBadRequest, "auth_scheme must be none, platform-jwt, api-key, or internal")
		return
	}

	// Verify app exists
	var appCount int
	if err := h.pool.QueryRow(c.Request.Context(),
		`SELECT COUNT(*) FROM resource_snapshots
		 WHERE project_id = $1 AND environment_id = $2 AND kind = 'App' AND name = $3`,
		projectID, envID, appName,
	).Scan(&appCount); err != nil {
		respondError(c, http.StatusInternalServerError, "failed to verify app")
		return
	}
	if appCount == 0 {
		respondError(c, http.StatusNotFound, "app not found")
		return
	}

	// Derive resource name from FQDN
	publicApiName := strings.ReplaceAll(req.FQDN, ".", "-")

	// Check uniqueness
	var existing int
	if err := h.pool.QueryRow(c.Request.Context(),
		`SELECT COUNT(*) FROM resource_snapshots
		 WHERE project_id = $1 AND environment_id = $2 AND kind = 'PublicApi' AND name = $3`,
		projectID, envID, publicApiName,
	).Scan(&existing); err != nil {
		respondError(c, http.StatusInternalServerError, "failed to check uniqueness")
		return
	}
	if existing > 0 {
		respondError(c, http.StatusConflict, "a domain with that FQDN already exists in this environment")
		return
	}

	payload := models.CreatePublicApiPayload{
		AppName:        appName,
		PublicApiName:  publicApiName,
		FQDN:           req.FQDN,
		AuthEnabled:    req.AuthEnabled,
		AuthScheme:     req.AuthScheme,
		AuthScopes:     req.AuthScopes,
		SwaggerEnabled: req.SwaggerEnabled,
		SwaggerPath:    req.SwaggerPath,
		SwaggerTitle:   req.SwaggerTitle,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to marshal payload")
		return
	}

	var op models.Operation
	err = h.pool.QueryRow(c.Request.Context(),
		`INSERT INTO operations (actor_id, project_id, environment_id, action, resource_kind, resource_name, status, payload)
		 VALUES ($1, $2, $3, 'CreatePublicApi', 'PublicApi', $4, 'Created', $5)
		 RETURNING id, actor_id, project_id, environment_id, action, resource_kind, resource_name,
		           status, payload, validation_result, git_commit, git_path, argo_application,
		           error_code, error_message, created_at, updated_at`,
		claims.UserID, projectID, envID, publicApiName, payloadBytes,
	).Scan(
		&op.ID, &op.ActorID, &op.ProjectID, &op.EnvironmentID,
		&op.Action, &op.ResourceKind, &op.ResourceName,
		&op.Status, &op.Payload, &op.ValidationResult,
		&op.GitCommit, &op.GitPath, &op.ArgoApplication,
		&op.ErrorCode, &op.ErrorMessage, &op.CreatedAt, &op.UpdatedAt,
	)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to create operation")
		return
	}

	auditMeta, _ := json.Marshal(payload)
	_, _ = h.pool.Exec(c.Request.Context(),
		`INSERT INTO audit_events (actor_id, project_id, operation_id, action, resource_kind, resource_name, metadata)
		 VALUES ($1, $2, $3, 'CreatePublicApi', 'PublicApi', $4, $5)`,
		claims.UserID, projectID, op.ID, publicApiName, auditMeta,
	)

	c.JSON(http.StatusAccepted, gin.H{
		"operation": op,
		"message":   "Domain registration queued",
	})
}
```

### Step 3: Register routes in router.go

Add inside the `api` group after the apps block:
```go
		// Endpoints (PublicApi)
		api.GET("/projects/:projectId/environments/:envId/apps/:appName/endpoints", h.ListEndpoints)
		api.POST("/projects/:projectId/environments/:envId/apps/:appName/endpoints", h.CreateEndpoint)
```

### Step 4: Verify compilation

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend && go build ./...
```

### Step 5: Run all tests

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend && go test ./...
```

### Step 6: Commit

```bash
git add backend/internal/models/operation.go \
        backend/internal/api/endpoints.go \
        backend/internal/api/router.go
git commit -m "feat(api): add CreatePublicApi typed action and endpoints handler"
```

---

## Task 4: Worker — processCreatePublicApi

**Files:**
- Modify: `backend/internal/worker/worker.go`

### Step 1: Wire switch and add method

In `processOperation` switch, add:
```go
case "CreatePublicApi":
    return w.processCreatePublicApi(ctx, op)
```

Add method at bottom of `worker.go`:
```go
func (w *Worker) processCreatePublicApi(ctx context.Context, op *models.Operation) error {
	var payload models.CreatePublicApiPayload
	if err := json.Unmarshal(op.Payload, &payload); err != nil {
		return fmt.Errorf("parsing CreatePublicApi payload: %w", err)
	}

	var projectName, envName, envNamespace string
	err := w.pool.QueryRow(ctx,
		`SELECT p.name, e.name, e.namespace
		 FROM projects p JOIN environments e ON e.project_id = p.id
		 WHERE p.id = $1 AND e.id = $2`,
		op.ProjectID, op.EnvironmentID,
	).Scan(&projectName, &envName, &envNamespace)
	if err != nil {
		return fmt.Errorf("fetching project/env for CreatePublicApi: %w", err)
	}

	// Read app port from snapshot
	var summaryRaw []byte
	var envIDVal interface{} = nil
	if op.EnvironmentID != nil {
		envIDVal = *op.EnvironmentID
	}
	err = w.pool.QueryRow(ctx,
		`SELECT summary_json FROM resource_snapshots
		 WHERE project_id = $1 AND environment_id = $2 AND kind = 'App' AND name = $3`,
		op.ProjectID, envIDVal, payload.AppName,
	).Scan(&summaryRaw)
	if err != nil {
		return fmt.Errorf("loading app snapshot for CreatePublicApi: %w", err)
	}
	var appSpec map[string]interface{}
	if err := json.Unmarshal(summaryRaw, &appSpec); err != nil {
		return fmt.Errorf("parsing app snapshot: %w", err)
	}
	portVal, _ := appSpec["port"].(float64)
	if portVal == 0 {
		portVal = 8080
	}

	w.updateStatus(ctx, op.ID, models.OperationStatusRendering)
	spec := gitwriter.PublicApiSpec{
		Name:           payload.PublicApiName,
		Namespace:      envNamespace,
		ProjectSlug:    projectName,
		EnvSlug:        envName,
		ServiceName:    payload.AppName,
		ServicePort:    int(portVal),
		FQDN:           payload.FQDN,
		LBTarget:       w.cfg.ClusterLBIP,
		AuthEnabled:    payload.AuthEnabled,
		AuthScheme:     payload.AuthScheme,
		AuthScopes:     payload.AuthScopes,
		SwaggerEnabled: payload.SwaggerEnabled,
		SwaggerPath:    payload.SwaggerPath,
		SwaggerTitle:   payload.SwaggerTitle,
		OperationID:    op.ID.String(),
	}
	yaml, err := gitwriter.RenderPublicApi(spec)
	if err != nil {
		return fmt.Errorf("rendering PublicApi manifest: %w", err)
	}

	w.updateStatus(ctx, op.ID, models.OperationStatusCommittingToGit)
	gitPath := gitwriter.PublicApiGitPath(projectName, envName, payload.AppName, payload.PublicApiName)
	commitMsg := fmt.Sprintf(
		"[DADA Console] Register domain %s for app %s in project %s\n\nOperation: %s\nActor: %s\nProject: %s\nEnvironment: %s\nResource: PublicApi/%s\n",
		payload.FQDN, payload.AppName, projectName,
		op.ID, op.ActorID, projectName, envName, payload.PublicApiName,
	)
	sha, err := w.gitWriter.CommitManifest(gitPath, yaml, commitMsg)
	if err != nil {
		return fmt.Errorf("git commit for CreatePublicApi: %w", err)
	}

	_, err = w.pool.Exec(ctx,
		`UPDATE operations SET status = 'Committed', git_commit = $1, git_path = $2, updated_at = NOW() WHERE id = $3`,
		sha, gitPath, op.ID)
	if err != nil {
		return fmt.Errorf("updating committed status for CreatePublicApi: %w", err)
	}

	summaryJSON, _ := json.Marshal(map[string]interface{}{
		"app_name":        payload.AppName,
		"fqdn":            payload.FQDN,
		"auth_enabled":    payload.AuthEnabled,
		"auth_scheme":     payload.AuthScheme,
		"swagger_enabled": payload.SwaggerEnabled,
		"status":          "Pending",
		"message":         "Domain registration in progress",
	})
	_, err = w.pool.Exec(ctx, `
		INSERT INTO resource_snapshots (project_id, environment_id, kind, name, phase, summary_json)
		VALUES ($1, $2, 'PublicApi', $3, 'Pending', $4)
		ON CONFLICT (project_id, environment_id, kind, name)
		DO UPDATE SET phase = 'Pending', summary_json = EXCLUDED.summary_json, last_synced_at = NOW()
	`, op.ProjectID, envIDVal, payload.PublicApiName, summaryJSON)
	if err != nil {
		log.Error().Err(err).Msg("creating PublicApi resource snapshot")
	}

	if w.cfg.DevMode {
		w.simulatePublicApiReady(ctx, op.ID, op.ProjectID, op.EnvironmentID, payload.PublicApiName, summaryJSON)
	} else {
		w.updateStatus(ctx, op.ID, models.OperationStatusWaitingForArgoSync)
	}

	return nil
}

func (w *Worker) simulatePublicApiReady(ctx context.Context, opID, projectID uuid.UUID, environmentID *uuid.UUID, name string, specJSON []byte) {
	steps := []struct {
		status models.OperationStatus
		delay  time.Duration
	}{
		{models.OperationStatusWaitingForArgoSync, 2 * time.Second},
		{models.OperationStatusSyncing, 3 * time.Second},
		{models.OperationStatusReconciling, 2 * time.Second},
		{models.OperationStatusReady, 0},
	}
	for _, step := range steps {
		time.Sleep(step.delay)
		w.updateStatus(ctx, opID, step.status)
	}

	var spec map[string]interface{}
	_ = json.Unmarshal(specJSON, &spec)
	spec["status"] = "Ready"
	spec["message"] = "Domain registered by DADA Console"
	readyJSON, _ := json.Marshal(spec)

	var envIDVal interface{} = nil
	if environmentID != nil {
		envIDVal = *environmentID
	}
	_, err := w.pool.Exec(ctx,
		`UPDATE resource_snapshots SET phase = 'Ready', summary_json = $1, last_synced_at = NOW()
		 WHERE project_id = $2 AND environment_id = $3 AND kind = 'PublicApi' AND name = $4`,
		readyJSON, projectID, envIDVal, name)
	if err != nil {
		log.Error().Err(err).Msg("updating PublicApi snapshot to Ready")
	}
}
```

### Step 2: Verify compilation

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend && go build ./...
```

### Step 3: Run all tests

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend && go test ./...
```

### Step 4: Commit

```bash
git add backend/internal/worker/worker.go
git commit -m "feat(worker): add processCreatePublicApi with Git commit and status simulation"
```

---

## Task 5: Frontend — types and API client

**Files:**
- Modify: `frontend/lib/types.ts`
- Modify: `frontend/lib/api.ts`

### Step 1: Add types to types.ts

Append:
```typescript
export interface EndpointSummary {
  app_name: string;
  fqdn: string;
  auth_enabled: boolean;
  auth_scheme: string;
  swagger_enabled: boolean;
  status: string;
  message: string;
}

export interface EndpointsResponse {
  endpoints: ResourceSnapshot[];
}

export interface CreateEndpointResponse {
  operation: Operation;
  message: string;
}
```

### Step 2: Add endpointsApi to api.ts

Add to top import: `EndpointsResponse`, `CreateEndpointResponse`

Append:
```typescript
export const endpointsApi = {
  list: (projectId: string, envId: string, appName: string) =>
    apiFetch<EndpointsResponse>(
      `/api/v1/projects/${projectId}/environments/${envId}/apps/${appName}/endpoints`
    ),

  create: (
    projectId: string,
    envId: string,
    appName: string,
    data: {
      fqdn: string;
      auth_enabled: boolean;
      auth_scheme: string;
      auth_scopes: string[];
      swagger_enabled: boolean;
      swagger_path: string;
      swagger_title: string;
    }
  ) =>
    apiFetch<CreateEndpointResponse>(
      `/api/v1/projects/${projectId}/environments/${envId}/apps/${appName}/endpoints`,
      { method: "POST", body: data }
    ),
};
```

### Step 3: TypeScript check

```bash
cd /Users/alex/IdeaProjects/dada-cloud/frontend && npx tsc --noEmit
```

### Step 4: Commit

```bash
git add frontend/lib/types.ts frontend/lib/api.ts
git commit -m "feat(frontend): add endpoint types and endpointsApi client"
```

---

## Task 6: Frontend — Domains section on App detail page

**Files:**
- Modify: `frontend/app/(console)/projects/[projectId]/apps/[appName]/page.tsx`

Add a "Domains" section below the spec cards, with a list of existing domains and an "Add Domain" button that opens a modal.

The modal form fields:
- FQDN (text input, required)
- Auth scheme (select: none / platform-jwt / api-key / internal)
- Auth scopes (textarea, shown only if scheme ≠ none, comma-separated)
- Swagger enabled (checkbox)
- Swagger path (text, shown if swagger enabled, default `/v3/api-docs`)
- Swagger title (text, shown if swagger enabled)

After submit: redirect to operations page with highlight.

### Step 1: Update page.tsx

Replace the current `frontend/app/(console)/projects/[projectId]/apps/[appName]/page.tsx` with the updated version that includes:

1. Import `endpointsApi` and new types
2. State for endpoints list + loading
3. State for domain modal form
4. `useEffect` to load endpoints alongside app data
5. Domains section with list cards + "Add Domain" button
6. Create Domain modal with the form

```typescript
"use client";
import { useEffect, useState, FormEvent } from "react";
import { useParams, useSearchParams, useRouter } from "next/navigation";
import Link from "next/link";
import { appsApi, endpointsApi } from "@/lib/api";
import type { ResourceSnapshot } from "@/lib/types";
import { Modal } from "@/components/ui/modal";
import { Spinner } from "@/components/ui/spinner";

function PhaseBadge({ phase }: { phase?: string }) {
  const p = phase ?? "";
  const isReady = p.toLowerCase() === "ready";
  return (
    <span className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ${
      isReady ? "bg-green-100 text-green-700" : "bg-yellow-100 text-yellow-700"
    }`}>
      {p || "Unknown"}
    </span>
  );
}

interface DomainForm {
  fqdn: string;
  auth_enabled: boolean;
  auth_scheme: string;
  auth_scopes: string;
  swagger_enabled: boolean;
  swagger_path: string;
  swagger_title: string;
}

const defaultDomainForm: DomainForm = {
  fqdn: "",
  auth_enabled: false,
  auth_scheme: "none",
  auth_scopes: "",
  swagger_enabled: false,
  swagger_path: "/v3/api-docs",
  swagger_title: "",
};

export default function AppDetailPage() {
  const params = useParams<{ projectId: string; appName: string }>();
  const searchParams = useSearchParams();
  const router = useRouter();
  const { projectId, appName } = params;
  const envId = searchParams.get("envId") ?? "";

  const [app, setApp] = useState<ResourceSnapshot | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [endpoints, setEndpoints] = useState<ResourceSnapshot[]>([]);
  const [isLoadingEndpoints, setIsLoadingEndpoints] = useState(true);

  const [isImageModalOpen, setIsImageModalOpen] = useState(false);
  const [newImage, setNewImage] = useState("");
  const [isImageSubmitting, setIsImageSubmitting] = useState(false);
  const [imageSubmitError, setImageSubmitError] = useState<string | null>(null);

  const [isDomainModalOpen, setIsDomainModalOpen] = useState(false);
  const [domainForm, setDomainForm] = useState<DomainForm>(defaultDomainForm);
  const [isDomainSubmitting, setIsDomainSubmitting] = useState(false);
  const [domainSubmitError, setDomainSubmitError] = useState<string | null>(null);

  useEffect(() => {
    if (!envId) return;
    appsApi
      .list(projectId, envId)
      .then((data) => {
        const found = (data.apps ?? []).find((a) => a.name === appName);
        if (!found) setError("App not found");
        else setApp(found);
      })
      .catch((err) => setError(err instanceof Error ? err.message : "Failed to load app"))
      .finally(() => setIsLoading(false));

    endpointsApi
      .list(projectId, envId, appName)
      .then((data) => setEndpoints(data.endpoints ?? []))
      .catch(() => setEndpoints([]))
      .finally(() => setIsLoadingEndpoints(false));
  }, [projectId, appName, envId]);

  async function handleImageUpdate(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setImageSubmitError(null);
    setIsImageSubmitting(true);
    try {
      const result = await appsApi.updateImage(projectId, envId, appName, newImage);
      setIsImageModalOpen(false);
      setNewImage("");
      const opId = result.operation?.id;
      setTimeout(() => {
        router.push(`/projects/${projectId}/operations${opId ? `?highlight=${opId}` : ""}`);
      }, 1500);
    } catch (err) {
      setImageSubmitError(err instanceof Error ? err.message : "Failed to update image");
    } finally {
      setIsImageSubmitting(false);
    }
  }

  async function handleDomainCreate(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setDomainSubmitError(null);
    setIsDomainSubmitting(true);
    try {
      const scopes = domainForm.auth_scopes
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      const result = await endpointsApi.create(projectId, envId, appName, {
        fqdn: domainForm.fqdn,
        auth_enabled: domainForm.auth_scheme !== "none",
        auth_scheme: domainForm.auth_scheme,
        auth_scopes: scopes,
        swagger_enabled: domainForm.swagger_enabled,
        swagger_path: domainForm.swagger_path || "/v3/api-docs",
        swagger_title: domainForm.swagger_title || appName,
      });
      setIsDomainModalOpen(false);
      setDomainForm(defaultDomainForm);
      const opId = result.operation?.id;
      setTimeout(() => {
        router.push(`/projects/${projectId}/operations${opId ? `?highlight=${opId}` : ""}`);
      }, 1500);
    } catch (err) {
      setDomainSubmitError(err instanceof Error ? err.message : "Failed to register domain");
    } finally {
      setIsDomainSubmitting(false);
    }
  }

  if (isLoading) {
    return <div className="flex h-64 items-center justify-center"><Spinner size="lg" /></div>;
  }
  if (error || !app) {
    return (
      <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
        {error ?? "App not found"}
      </div>
    );
  }

  const summary = app.summary_json as { image?: string; port?: number; replicas?: number; profile?: string };

  return (
    <div>
      {/* Header */}
      <div className="mb-8 flex items-start justify-between">
        <div>
          <div className="flex items-center gap-2 text-sm text-gray-500">
            <Link href="/projects" className="hover:text-gray-700">Projects</Link>
            <span>/</span>
            <Link href={`/projects/${projectId}`} className="hover:text-gray-700">Overview</Link>
            <span>/</span>
            <Link href={`/projects/${projectId}/apps`} className="hover:text-gray-700">Applications</Link>
            <span>/</span>
            <span className="text-gray-900 font-mono">{appName}</span>
          </div>
          <div className="mt-2 flex items-center gap-3">
            <h1 className="text-2xl font-bold text-gray-900 font-mono">{appName}</h1>
            <PhaseBadge phase={app.phase} />
          </div>
        </div>
        <button
          onClick={() => { setNewImage(summary.image ?? ""); setIsImageModalOpen(true); }}
          className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 transition-colors"
        >
          <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
          </svg>
          Deploy Image
        </button>
      </div>

      {/* Spec cards */}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {[
          { label: "Image", value: summary.image ?? "—", mono: true },
          { label: "Profile", value: summary.profile ?? "small" },
          { label: "Replicas", value: String(summary.replicas ?? 2) },
          { label: "Port", value: String(summary.port ?? 8080) },
        ].map(({ label, value, mono }) => (
          <div key={label} className="rounded-xl border border-gray-200 bg-white p-5 shadow-sm">
            <p className="text-xs font-semibold uppercase tracking-wide text-gray-400">{label}</p>
            <p className={`mt-1 text-sm font-medium text-gray-900 truncate ${mono ? "font-mono" : ""}`}>{value}</p>
          </div>
        ))}
      </div>

      {/* Domains section */}
      <div className="mt-10">
        <div className="mb-4 flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold text-gray-900">Domains</h2>
            <p className="text-sm text-gray-400">Public endpoints via gateway + DNS</p>
          </div>
          <button
            onClick={() => { setDomainForm({ ...defaultDomainForm, swagger_title: appName }); setIsDomainModalOpen(true); }}
            className="inline-flex items-center gap-2 rounded-lg border border-gray-200 bg-white px-3 py-1.5 text-sm font-medium text-gray-700 hover:border-blue-300 hover:text-blue-600 transition-colors shadow-sm"
          >
            <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
            </svg>
            Add Domain
          </button>
        </div>

        {isLoadingEndpoints ? (
          <div className="flex h-20 items-center justify-center"><Spinner /></div>
        ) : endpoints.length === 0 ? (
          <div className="flex flex-col items-center justify-center rounded-xl border border-dashed border-gray-300 bg-gray-50 py-10">
            <svg className="mb-2 h-8 w-8 text-gray-300" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9" />
            </svg>
            <p className="text-sm text-gray-400">No domains yet</p>
            <button
              onClick={() => { setDomainForm({ ...defaultDomainForm, swagger_title: appName }); setIsDomainModalOpen(true); }}
              className="mt-2 text-sm text-blue-600 hover:text-blue-700"
            >
              Add first domain →
            </button>
          </div>
        ) : (
          <div className="space-y-3">
            {endpoints.map((ep) => {
              const epSummary = ep.summary_json as { fqdn?: string; auth_scheme?: string; swagger_enabled?: boolean };
              return (
                <div key={ep.id} className="flex items-center justify-between rounded-xl border border-gray-200 bg-white px-5 py-4 shadow-sm">
                  <div className="flex items-center gap-4">
                    <svg className="h-5 w-5 text-gray-400 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9" />
                    </svg>
                    <div>
                      <p className="font-mono text-sm font-medium text-gray-900">{epSummary.fqdn ?? ep.name}</p>
                      <p className="text-xs text-gray-400">
                        auth: {epSummary.auth_scheme ?? "none"}
                        {epSummary.swagger_enabled && " · swagger"}
                      </p>
                    </div>
                  </div>
                  <PhaseBadge phase={ep.phase} />
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Deploy Image Modal */}
      <Modal
        isOpen={isImageModalOpen}
        onClose={() => { setIsImageModalOpen(false); setImageSubmitError(null); }}
        title="Deploy New Image"
      >
        <form onSubmit={handleImageUpdate} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700">New Image Tag</label>
            <input
              type="text"
              required
              value={newImage}
              onChange={(e) => setNewImage(e.target.value)}
              placeholder="ghcr.io/org/service:v2.0.0"
              className="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm font-mono text-gray-900 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
            <p className="mt-1 text-xs text-gray-400">Current: <span className="font-mono">{summary.image ?? "—"}</span></p>
          </div>
          {imageSubmitError && (
            <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{imageSubmitError}</div>
          )}
          <div className="flex justify-end gap-3 pt-2">
            <button type="button" onClick={() => { setIsImageModalOpen(false); setImageSubmitError(null); }}
              className="rounded-lg px-4 py-2 text-sm font-medium text-gray-600 hover:bg-gray-100 transition-colors">
              Cancel
            </button>
            <button type="submit" disabled={isImageSubmitting}
              className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50 transition-colors">
              {isImageSubmitting ? <><Spinner size="sm" /> Deploying...</> : "Deploy"}
            </button>
          </div>
        </form>
      </Modal>

      {/* Add Domain Modal */}
      <Modal
        isOpen={isDomainModalOpen}
        onClose={() => { setIsDomainModalOpen(false); setDomainSubmitError(null); }}
        title="Add Domain"
      >
        <form onSubmit={handleDomainCreate} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700">FQDN</label>
            <input
              type="text"
              required
              value={domainForm.fqdn}
              onChange={(e) => setDomainForm((f) => ({ ...f, fqdn: e.target.value }))}
              placeholder="api.myservice.ru"
              className="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm font-mono text-gray-900 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700">Auth Scheme</label>
            <select
              value={domainForm.auth_scheme}
              onChange={(e) => setDomainForm((f) => ({ ...f, auth_scheme: e.target.value, auth_enabled: e.target.value !== "none" }))}
              className="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm text-gray-900 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            >
              <option value="none">none — public access</option>
              <option value="platform-jwt">platform-jwt</option>
              <option value="api-key">api-key</option>
              <option value="internal">internal</option>
            </select>
          </div>

          {domainForm.auth_scheme !== "none" && (
            <div>
              <label className="block text-sm font-medium text-gray-700">
                Scopes <span className="font-normal text-gray-400">(comma-separated)</span>
              </label>
              <input
                type="text"
                value={domainForm.auth_scopes}
                onChange={(e) => setDomainForm((f) => ({ ...f, auth_scopes: e.target.value }))}
                placeholder="api.read, api.write"
                className="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm font-mono text-gray-900 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
              />
            </div>
          )}

          <div className="flex items-center gap-3">
            <input
              type="checkbox"
              id="swagger-enabled"
              checked={domainForm.swagger_enabled}
              onChange={(e) => setDomainForm((f) => ({ ...f, swagger_enabled: e.target.checked }))}
              className="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
            />
            <label htmlFor="swagger-enabled" className="text-sm font-medium text-gray-700">Enable Swagger / OpenAPI</label>
          </div>

          {domainForm.swagger_enabled && (
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-sm font-medium text-gray-700">API Docs Path</label>
                <input
                  type="text"
                  value={domainForm.swagger_path}
                  onChange={(e) => setDomainForm((f) => ({ ...f, swagger_path: e.target.value }))}
                  placeholder="/v3/api-docs"
                  className="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm font-mono text-gray-900 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700">API Title</label>
                <input
                  type="text"
                  value={domainForm.swagger_title}
                  onChange={(e) => setDomainForm((f) => ({ ...f, swagger_title: e.target.value }))}
                  placeholder={appName}
                  className="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm text-gray-900 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
                />
              </div>
            </div>
          )}

          {domainSubmitError && (
            <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{domainSubmitError}</div>
          )}

          <div className="flex justify-end gap-3 pt-2">
            <button type="button" onClick={() => { setIsDomainModalOpen(false); setDomainSubmitError(null); }}
              className="rounded-lg px-4 py-2 text-sm font-medium text-gray-600 hover:bg-gray-100 transition-colors">
              Cancel
            </button>
            <button type="submit" disabled={isDomainSubmitting}
              className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50 transition-colors">
              {isDomainSubmitting ? <><Spinner size="sm" /> Registering...</> : "Add Domain"}
            </button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
```

### Step 2: TypeScript check

```bash
cd /Users/alex/IdeaProjects/dada-cloud/frontend && npx tsc --noEmit
```

### Step 3: Commit

```bash
git add "frontend/app/(console)/projects/[projectId]/apps/[appName]/page.tsx"
git commit -m "feat(frontend): add Domains section with Add Domain modal on app detail page"
```

---

## Final verification

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend && go test ./... -v
cd /Users/alex/IdeaProjects/dada-cloud/frontend && npx tsc --noEmit
cd /Users/alex/IdeaProjects/dada-cloud/backend && go build ./...
```

---

## Decision Log

| Decision | Alternative | Reason |
|----------|-------------|--------|
| PublicApi name = FQDN dots→dashes | UUID or user-chosen name | Stable, readable, idempotent; DNS domain already unique |
| Route always / + /** | User-configurable route | Domain-based routing, not path-based; simplifies UX |
| Domains on App detail page, not separate page | Separate /endpoints page | PublicApi always owned by one App; no reason for top-level list |
| LBTarget from config (CLUSTER_LB_IP) | Hardcoded | Allows overriding per environment |
| Auth scopes as comma-separated input | Multi-select | Simpler form; scopes are free-form |
| swagger_title defaults to appName | Empty | UX default; gateway needs a title |
