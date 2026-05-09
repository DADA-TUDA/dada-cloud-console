# v2 App Lifecycle — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `App` resource support (create, list) and `DeployImageVersion` (image update) as typed operations following the existing CreateServiceDatabase pattern end-to-end: backend handlers → worker YAML renderer → Git commit → frontend UI.

**Architecture:** New typed action types (`CreateApp`, `DeployImageVersion`) follow the exact same Operation model as `CreateServiceDatabase`. The worker renders an `App` CRD manifest and commits it to the same Git state repo. App spec is stored in `resource_snapshots.summary_json` so `DeployImageVersion` can re-render without reading Git. Frontend mirrors the databases page pattern.

**Tech Stack:** Go 1.22, Gin, pgx/v5, go-git, Next.js 14 App Router, TypeScript, Tailwind CSS

---

## Context: Where Each Piece Lives

```
backend/
  internal/
    gitwriter/
      renderer.go          ← ServiceDatabase renderer (COPY pattern for App)
      renderer_test.go     ← golden-file tests
    models/
      operation.go         ← payload structs (add CreateAppPayload, DeployImageVersionPayload)
    api/
      databases.go         ← COPY pattern for apps.go
      router.go            ← register new routes here
    worker/
      worker.go            ← add processCreateApp, processDeployImageVersion
  tests/golden/
    servicedatabase/       ← existing golden files
    app/                   ← CREATE this directory for App golden files
frontend/
  lib/
    types.ts               ← add App types
    api.ts                 ← add appsApi
  app/(console)/projects/[projectId]/
    page.tsx               ← add Apps quick-action card
    apps/
      page.tsx             ← CREATE: list + create form
      [appName]/
        page.tsx           ← CREATE: detail + image update
```

---

## Task 1: App YAML renderer + golden tests

**Files:**
- Create: `backend/internal/gitwriter/app_renderer.go`
- Create: `backend/internal/gitwriter/app_renderer_test.go`
- Create: `backend/tests/golden/app/basic.yaml`
- Create: `backend/tests/golden/app/custom-profile.yaml`

### Step 1: Create golden file — basic app

Create `backend/tests/golden/app/basic.yaml`:
```yaml
apiVersion: platform.dada-tuda.ru/v1alpha1
kind: App
metadata:
  name: codex-lb
  namespace: internal-prod
  labels:
    dada.io/project: internal
    dada.io/environment: prod
    dada.io/operation: op-test-1234
spec:
  project: internal
  image: ghcr.io/dada-tuda/codex-lb:1.14.2
  port: 8080
  replicas: 2
  profile: small
```

### Step 2: Create golden file — custom profile

Create `backend/tests/golden/app/custom-profile.yaml`:
```yaml
apiVersion: platform.dada-tuda.ru/v1alpha1
kind: App
metadata:
  name: profi-backend
  namespace: client-a-prod
  labels:
    dada.io/project: client-a
    dada.io/environment: prod
    dada.io/operation: op-test-5678
spec:
  project: client-a
  image: registry.dada-tuda.ru/profi-backend:2.3.1
  port: 3000
  replicas: 3
  profile: medium
```

### Step 3: Write the failing test

Create `backend/internal/gitwriter/app_renderer_test.go`:
```go
package gitwriter_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dada-tuda/console/backend/internal/gitwriter"
)

func appGoldenPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "tests", "golden", "app", name)
}

func readAppGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(appGoldenPath(name))
	if err != nil {
		t.Fatalf("reading golden file %s: %v", name, err)
	}
	return string(b)
}

func TestRenderApp_Basic(t *testing.T) {
	spec := gitwriter.AppSpec{
		Name:        "codex-lb",
		Namespace:   "internal-prod",
		ProjectSlug: "internal",
		EnvSlug:     "prod",
		Image:       "ghcr.io/dada-tuda/codex-lb:1.14.2",
		Port:        8080,
		Replicas:    2,
		Profile:     "small",
		OperationID: "op-test-1234",
	}
	got, err := gitwriter.RenderApp(spec)
	if err != nil {
		t.Fatalf("RenderApp: %v", err)
	}
	want := readAppGolden(t, "basic.yaml")
	if got != want {
		t.Errorf("rendered YAML does not match golden file basic.yaml\n\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestRenderApp_CustomProfile(t *testing.T) {
	spec := gitwriter.AppSpec{
		Name:        "profi-backend",
		Namespace:   "client-a-prod",
		ProjectSlug: "client-a",
		EnvSlug:     "prod",
		Image:       "registry.dada-tuda.ru/profi-backend:2.3.1",
		Port:        3000,
		Replicas:    3,
		Profile:     "medium",
		OperationID: "op-test-5678",
	}
	got, err := gitwriter.RenderApp(spec)
	if err != nil {
		t.Fatalf("RenderApp: %v", err)
	}
	want := readAppGolden(t, "custom-profile.yaml")
	if got != want {
		t.Errorf("rendered YAML does not match golden file custom-profile.yaml\n\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestAppGitPath(t *testing.T) {
	cases := []struct {
		project, env, app, want string
	}{
		{"internal", "prod", "codex-lb",
			"clusters/beget-prod/projects/internal/environments/prod/apps/codex-lb/app.yaml"},
		{"client-a", "prod", "profi-backend",
			"clusters/beget-prod/projects/client-a/environments/prod/apps/profi-backend/app.yaml"},
	}
	for _, tc := range cases {
		got := gitwriter.AppGitPath(tc.project, tc.env, tc.app)
		if got != tc.want {
			t.Errorf("AppGitPath(%q,%q,%q) = %q, want %q",
				tc.project, tc.env, tc.app, got, tc.want)
		}
	}
}
```

### Step 4: Run test — verify it fails

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend && go test ./internal/gitwriter/... -v -run TestRenderApp
```
Expected: compile error — `gitwriter.AppSpec` undefined

### Step 5: Implement the renderer

Create `backend/internal/gitwriter/app_renderer.go`:
```go
package gitwriter

import (
	"bytes"
	"fmt"
	"text/template"
)

// AppSpec holds parameters for rendering an App manifest.
type AppSpec struct {
	Name        string
	Namespace   string
	ProjectSlug string
	EnvSlug     string
	Image       string
	Port        int
	Replicas    int
	Profile     string
	OperationID string
}

var appTemplate = template.Must(template.New("app").Parse(`apiVersion: platform.dada-tuda.ru/v1alpha1
kind: App
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
  labels:
    dada.io/project: {{ .ProjectSlug }}
    dada.io/environment: {{ .EnvSlug }}
    dada.io/operation: {{ .OperationID }}
spec:
  project: {{ .ProjectSlug }}
  image: {{ .Image }}
  port: {{ .Port }}
  replicas: {{ .Replicas }}
  profile: {{ .Profile }}
`))

// RenderApp generates the YAML manifest for an App CRD.
func RenderApp(spec AppSpec) (string, error) {
	var buf bytes.Buffer
	if err := appTemplate.Execute(&buf, spec); err != nil {
		return "", fmt.Errorf("rendering App template: %w", err)
	}
	return buf.String(), nil
}

// AppGitPath returns the canonical Git path for an App manifest.
// Format: clusters/beget-prod/projects/{project}/environments/{env}/apps/{name}/app.yaml
func AppGitPath(projectSlug, envSlug, appName string) string {
	return fmt.Sprintf("clusters/beget-prod/projects/%s/environments/%s/apps/%s/app.yaml",
		projectSlug, envSlug, appName)
}
```

### Step 6: Run tests — verify they pass

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend && go test ./internal/gitwriter/... -v
```
Expected: all 5 tests PASS (3 new + 3 existing ServiceDatabase tests)

### Step 7: Commit

```bash
git add backend/internal/gitwriter/app_renderer.go \
        backend/internal/gitwriter/app_renderer_test.go \
        backend/tests/golden/app/
git commit -m "feat(gitwriter): add App CRD renderer and golden tests"
```

---

## Task 2: App payload types + backend API handler

**Files:**
- Modify: `backend/internal/models/operation.go` (add 2 payload structs)
- Create: `backend/internal/api/apps.go`
- Modify: `backend/internal/api/router.go` (add 3 routes)
- Modify: `backend/internal/api/validate.go` (add image regex)

### Step 1: Write the failing test for the API handler

Create `backend/internal/api/apps_test.go`:
```go
package api_test

import (
	"testing"

	"github.com/dada-tuda/console/backend/internal/api"
)

func TestValidateImage(t *testing.T) {
	good := []string{
		"ghcr.io/dada-tuda/codex-lb:1.14.2",
		"registry.dada-tuda.ru/app:latest",
		"nginx:1.25",
		"my-app:v2.3.1-rc1",
	}
	bad := []string{
		"",
		"no-tag",
		"has space:v1",
		"UPPERCASE:v1",
	}
	for _, img := range good {
		if err := api.ValidateImage(img); err != nil {
			t.Errorf("expected %q to be valid, got: %v", img, err)
		}
	}
	for _, img := range bad {
		if err := api.ValidateImage(img); err == nil {
			t.Errorf("expected %q to be invalid", img)
		}
	}
}
```

### Step 2: Run test — verify it fails

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend && go test ./internal/api/... -v -run TestValidateImage
```
Expected: compile error — `api.ValidateImage` undefined

### Step 3: Add image validation + payload types

**Modify `backend/internal/api/validate.go`** — append after existing validators:
```go
var reImage = regexp.MustCompile(`^[a-z0-9][a-z0-9._\-/]*:[a-z0-9][a-z0-9._\-]*$`)

func ValidateImage(image string) error {
	if !reImage.MatchString(image) {
		return fmt.Errorf("image must be lowercase image:tag format")
	}
	return nil
}
```

**Modify `backend/internal/models/operation.go`** — append after `CreateServiceDatabasePayload`:
```go
// CreateAppPayload is the typed payload for CreateApp operations.
type CreateAppPayload struct {
	Name     string `json:"name"`
	Image    string `json:"image"`
	Port     int    `json:"port"`
	Replicas int    `json:"replicas"`
	Profile  string `json:"profile"`
}

// DeployImageVersionPayload is the typed payload for DeployImageVersion operations.
type DeployImageVersionPayload struct {
	AppName string `json:"app_name"`
	Image   string `json:"image"`
}
```

### Step 4: Run test — verify it passes

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend && go test ./internal/api/... -v -run TestValidateImage
```
Expected: PASS

### Step 5: Create the apps handler

Create `backend/internal/api/apps.go`:
```go
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

// ListApps returns all App resources in a project environment.
func (h *Handler) ListApps(c *gin.Context) {
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
		 WHERE project_id = $1 AND environment_id = $2 AND kind = 'App'
		 ORDER BY name`,
		projectID, envID,
	)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to query apps")
		return
	}
	defer rows.Close()

	var apps []models.ResourceSnapshot
	for rows.Next() {
		var rs models.ResourceSnapshot
		if err := rows.Scan(
			&rs.ID, &rs.ProjectID, &rs.EnvironmentID, &rs.Kind, &rs.Name,
			&rs.Phase, &rs.SummaryJSON, &rs.LastSyncedAt,
		); err != nil {
			respondError(c, http.StatusInternalServerError, "failed to scan app")
			return
		}
		apps = append(apps, rs)
	}
	if err := rows.Err(); err != nil {
		respondError(c, http.StatusInternalServerError, "error reading apps")
		return
	}
	if apps == nil {
		apps = []models.ResourceSnapshot{}
	}
	c.JSON(http.StatusOK, gin.H{"apps": apps})
}

type createAppRequest struct {
	Name     string `json:"name"`
	Image    string `json:"image"`
	Port     int    `json:"port"`
	Replicas int    `json:"replicas"`
	Profile  string `json:"profile"`
}

// CreateApp enqueues a CreateApp operation.
func (h *Handler) CreateApp(c *gin.Context) {
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

	var req createAppRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	// Defaults
	if req.Port == 0 {
		req.Port = 8080
	}
	if req.Replicas == 0 {
		req.Replicas = 2
	}
	if req.Profile == "" {
		req.Profile = "small"
	}

	// Validate
	if req.Name == "" {
		respondError(c, http.StatusBadRequest, "name is required")
		return
	}
	if req.Image == "" {
		respondError(c, http.StatusBadRequest, "image is required")
		return
	}
	if err := validateKubeName(req.Name); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := ValidateImage(req.Image); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.Port < 1 || req.Port > 65535 {
		respondError(c, http.StatusBadRequest, "port must be between 1 and 65535")
		return
	}
	if req.Replicas < 1 || req.Replicas > 10 {
		respondError(c, http.StatusBadRequest, "replicas must be between 1 and 10")
		return
	}
	validProfiles := map[string]bool{"small": true, "medium": true, "large": true}
	if !validProfiles[req.Profile] {
		respondError(c, http.StatusBadRequest, "profile must be small, medium, or large")
		return
	}

	// Check name uniqueness
	var existing int
	err = h.pool.QueryRow(c.Request.Context(),
		`SELECT COUNT(*) FROM resource_snapshots
		 WHERE project_id = $1 AND environment_id = $2 AND kind = 'App' AND name = $3`,
		projectID, envID, req.Name,
	).Scan(&existing)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to check name uniqueness")
		return
	}
	if existing > 0 {
		respondError(c, http.StatusConflict, "an app with that name already exists in this environment")
		return
	}

	payload := models.CreateAppPayload{
		Name:     req.Name,
		Image:    req.Image,
		Port:     req.Port,
		Replicas: req.Replicas,
		Profile:  req.Profile,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to marshal payload")
		return
	}

	var op models.Operation
	err = h.pool.QueryRow(c.Request.Context(),
		`INSERT INTO operations (actor_id, project_id, environment_id, action, resource_kind, resource_name, status, payload)
		 VALUES ($1, $2, $3, 'CreateApp', 'App', $4, 'Created', $5)
		 RETURNING id, actor_id, project_id, environment_id, action, resource_kind, resource_name,
		           status, payload, validation_result, git_commit, git_path, argo_application,
		           error_code, error_message, created_at, updated_at`,
		claims.UserID, projectID, envID, req.Name, payloadBytes,
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
		 VALUES ($1, $2, $3, 'CreateApp', 'App', $4, $5)`,
		claims.UserID, projectID, op.ID, req.Name, auditMeta,
	)

	c.JSON(http.StatusAccepted, gin.H{
		"operation": op,
		"message":   "App creation queued",
	})
}

type updateAppImageRequest struct {
	Image string `json:"image"`
}

// UpdateAppImage enqueues a DeployImageVersion operation.
func (h *Handler) UpdateAppImage(c *gin.Context) {
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

	var req updateAppImageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.Image == "" {
		respondError(c, http.StatusBadRequest, "image is required")
		return
	}
	if err := ValidateImage(req.Image); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	// Verify app exists
	var appCount int
	err = h.pool.QueryRow(c.Request.Context(),
		`SELECT COUNT(*) FROM resource_snapshots
		 WHERE project_id = $1 AND environment_id = $2 AND kind = 'App' AND name = $3`,
		projectID, envID, appName,
	).Scan(&appCount)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to verify app")
		return
	}
	if appCount == 0 {
		respondNotFound(c)
		return
	}

	payload := models.DeployImageVersionPayload{
		AppName: appName,
		Image:   req.Image,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to marshal payload")
		return
	}

	var op models.Operation
	err = h.pool.QueryRow(c.Request.Context(),
		`INSERT INTO operations (actor_id, project_id, environment_id, action, resource_kind, resource_name, status, payload)
		 VALUES ($1, $2, $3, 'DeployImageVersion', 'App', $4, 'Created', $5)
		 RETURNING id, actor_id, project_id, environment_id, action, resource_kind, resource_name,
		           status, payload, validation_result, git_commit, git_path, argo_application,
		           error_code, error_message, created_at, updated_at`,
		claims.UserID, projectID, envID, appName, payloadBytes,
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
		 VALUES ($1, $2, $3, 'DeployImageVersion', 'App', $4, $5)`,
		claims.UserID, projectID, op.ID, appName, auditMeta,
	)

	c.JSON(http.StatusAccepted, gin.H{
		"operation": op,
		"message":   "Image update queued",
	})
}
```

### Step 6: Register routes in router.go

**Modify `backend/internal/api/router.go`** — add inside the `api` group after the databases block:
```go
		// Apps
		api.GET("/projects/:projectId/environments/:envId/apps", h.ListApps)
		api.POST("/projects/:projectId/environments/:envId/apps", h.CreateApp)
		api.PATCH("/projects/:projectId/environments/:envId/apps/:appName/image", h.UpdateAppImage)
```

### Step 7: Verify compilation

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend && go build ./...
```
Expected: no errors

### Step 8: Run all backend tests

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend && go test ./...
```
Expected: all PASS

### Step 9: Commit

```bash
git add backend/internal/models/operation.go \
        backend/internal/api/apps.go \
        backend/internal/api/apps_test.go \
        backend/internal/api/validate.go \
        backend/internal/api/router.go
git commit -m "feat(api): add CreateApp and DeployImageVersion typed actions"
```

---

## Task 3: Worker — processCreateApp

**Files:**
- Modify: `backend/internal/worker/worker.go`

### Step 1: Add `processCreateApp` method and wire the switch

**In `worker.go`**, update the `processOperation` switch:
```go
func (w *Worker) processOperation(ctx context.Context, op *models.Operation) error {
	switch op.Action {
	case "CreateServiceDatabase":
		return w.processCreateServiceDatabase(ctx, op)
	case "CreateApp":
		return w.processCreateApp(ctx, op)
	case "DeployImageVersion":
		return w.processDeployImageVersion(ctx, op)
	default:
		return fmt.Errorf("unknown action: %s", op.Action)
	}
}
```

**Add method at bottom of `worker.go`**:
```go
func (w *Worker) processCreateApp(ctx context.Context, op *models.Operation) error {
	var payload models.CreateAppPayload
	if err := json.Unmarshal(op.Payload, &payload); err != nil {
		return fmt.Errorf("parsing CreateApp payload: %w", err)
	}

	var projectName, envName, envNamespace string
	err := w.pool.QueryRow(ctx,
		`SELECT p.name, e.name, e.namespace
		 FROM projects p JOIN environments e ON e.project_id = p.id
		 WHERE p.id = $1 AND e.id = $2`,
		op.ProjectID, op.EnvironmentID,
	).Scan(&projectName, &envName, &envNamespace)
	if err != nil {
		return fmt.Errorf("fetching project/env for CreateApp: %w", err)
	}

	w.updateStatus(ctx, op.ID, models.OperationStatusRendering)
	spec := gitwriter.AppSpec{
		Name:        payload.Name,
		Namespace:   envNamespace,
		ProjectSlug: projectName,
		EnvSlug:     envName,
		Image:       payload.Image,
		Port:        payload.Port,
		Replicas:    payload.Replicas,
		Profile:     payload.Profile,
		OperationID: op.ID.String(),
	}
	yaml, err := gitwriter.RenderApp(spec)
	if err != nil {
		return fmt.Errorf("rendering App manifest: %w", err)
	}

	w.updateStatus(ctx, op.ID, models.OperationStatusCommittingToGit)
	gitPath := gitwriter.AppGitPath(projectName, envName, payload.Name)
	commitMsg := fmt.Sprintf(
		"[DADA Console] Create App %s for project %s\n\nOperation: %s\nActor: %s\nProject: %s\nEnvironment: %s\nResource: App/%s/%s\n",
		payload.Name, projectName,
		op.ID, op.ActorID, projectName, envName,
		envName, payload.Name,
	)
	sha, err := w.gitWriter.CommitManifest(gitPath, yaml, commitMsg)
	if err != nil {
		return fmt.Errorf("git commit for CreateApp: %w", err)
	}

	_, err = w.pool.Exec(ctx,
		`UPDATE operations SET status = 'Committed', git_commit = $1, git_path = $2, updated_at = NOW() WHERE id = $3`,
		sha, gitPath, op.ID)
	if err != nil {
		return fmt.Errorf("updating committed status for CreateApp: %w", err)
	}

	// Store spec in resource_snapshots so DeployImageVersion can re-render without reading git
	summaryJSON, _ := json.Marshal(map[string]interface{}{
		"image":    payload.Image,
		"port":     payload.Port,
		"replicas": payload.Replicas,
		"profile":  payload.Profile,
		"status":   "Pending",
		"message":  "App created by DADA Console",
	})
	var envIDVal interface{} = nil
	if op.EnvironmentID != nil {
		envIDVal = *op.EnvironmentID
	}
	_, err = w.pool.Exec(ctx, `
		INSERT INTO resource_snapshots (project_id, environment_id, kind, name, phase, summary_json)
		VALUES ($1, $2, 'App', $3, 'Pending', $4)
		ON CONFLICT (project_id, environment_id, kind, name)
		DO UPDATE SET phase = 'Pending', summary_json = EXCLUDED.summary_json, last_synced_at = NOW()
	`, op.ProjectID, envIDVal, payload.Name, summaryJSON)
	if err != nil {
		log.Error().Err(err).Msg("creating App resource snapshot")
	}

	if w.cfg.DevMode {
		w.simulateAppArgoAndReconcile(ctx, op.ID, op.ProjectID, op.EnvironmentID, payload.Name, summaryJSON)
	} else {
		w.updateStatus(ctx, op.ID, models.OperationStatusWaitingForArgoSync)
	}

	return nil
}

func (w *Worker) simulateAppArgoAndReconcile(ctx context.Context, opID, projectID uuid.UUID, environmentID *uuid.UUID, appName string, specJSON []byte) {
	steps := []struct {
		status models.OperationStatus
		delay  time.Duration
	}{
		{models.OperationStatusWaitingForArgoSync, 2 * time.Second},
		{models.OperationStatusSyncing, 3 * time.Second},
		{models.OperationStatusReconciling, 4 * time.Second},
		{models.OperationStatusReady, 0},
	}
	for _, step := range steps {
		time.Sleep(step.delay)
		w.updateStatus(ctx, opID, step.status)
	}

	// Update snapshot to Ready
	var spec map[string]interface{}
	_ = json.Unmarshal(specJSON, &spec)
	spec["status"] = "Ready"
	spec["message"] = "App provisioned by DADA Console"
	readyJSON, _ := json.Marshal(spec)

	var envIDVal interface{} = nil
	if environmentID != nil {
		envIDVal = *environmentID
	}
	_, err := w.pool.Exec(ctx, `
		UPDATE resource_snapshots SET phase = 'Ready', summary_json = $1, last_synced_at = NOW()
		WHERE project_id = $2 AND environment_id = $3 AND kind = 'App' AND name = $4
	`, readyJSON, projectID, envIDVal, appName)
	if err != nil {
		log.Error().Err(err).Msg("updating App resource snapshot to Ready")
	}
}
```

### Step 2: Verify compilation

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend && go build ./...
```
Expected: no errors

### Step 3: Commit

```bash
git add backend/internal/worker/worker.go
git commit -m "feat(worker): add processCreateApp with Git commit and status simulation"
```

---

## Task 4: Worker — processDeployImageVersion

**Files:**
- Modify: `backend/internal/worker/worker.go`

### Step 1: Add `processDeployImageVersion` at bottom of `worker.go`

```go
func (w *Worker) processDeployImageVersion(ctx context.Context, op *models.Operation) error {
	var payload models.DeployImageVersionPayload
	if err := json.Unmarshal(op.Payload, &payload); err != nil {
		return fmt.Errorf("parsing DeployImageVersion payload: %w", err)
	}

	var projectName, envName, envNamespace string
	err := w.pool.QueryRow(ctx,
		`SELECT p.name, e.name, e.namespace
		 FROM projects p JOIN environments e ON e.project_id = p.id
		 WHERE p.id = $1 AND e.id = $2`,
		op.ProjectID, op.EnvironmentID,
	).Scan(&projectName, &envName, &envNamespace)
	if err != nil {
		return fmt.Errorf("fetching project/env for DeployImageVersion: %w", err)
	}

	// Read current app spec from resource_snapshots
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
		return fmt.Errorf("loading app spec from snapshot: %w", err)
	}

	var currentSpec map[string]interface{}
	if err := json.Unmarshal(summaryRaw, &currentSpec); err != nil {
		return fmt.Errorf("parsing app spec from snapshot: %w", err)
	}

	portVal, _ := currentSpec["port"].(float64)
	replicasVal, _ := currentSpec["replicas"].(float64)
	profileVal, _ := currentSpec["profile"].(string)
	if portVal == 0 {
		portVal = 8080
	}
	if replicasVal == 0 {
		replicasVal = 2
	}
	if profileVal == "" {
		profileVal = "small"
	}

	w.updateStatus(ctx, op.ID, models.OperationStatusRendering)
	spec := gitwriter.AppSpec{
		Name:        payload.AppName,
		Namespace:   envNamespace,
		ProjectSlug: projectName,
		EnvSlug:     envName,
		Image:       payload.Image,
		Port:        int(portVal),
		Replicas:    int(replicasVal),
		Profile:     profileVal,
		OperationID: op.ID.String(),
	}
	yaml, err := gitwriter.RenderApp(spec)
	if err != nil {
		return fmt.Errorf("rendering App manifest for deploy: %w", err)
	}

	w.updateStatus(ctx, op.ID, models.OperationStatusCommittingToGit)
	gitPath := gitwriter.AppGitPath(projectName, envName, payload.AppName)
	commitMsg := fmt.Sprintf(
		"[DADA Console] Deploy image %s for app %s in project %s\n\nOperation: %s\nActor: %s\nProject: %s\nEnvironment: %s\nResource: App/%s/%s\n",
		payload.Image, payload.AppName, projectName,
		op.ID, op.ActorID, projectName, envName,
		envName, payload.AppName,
	)
	sha, err := w.gitWriter.CommitManifest(gitPath, yaml, commitMsg)
	if err != nil {
		return fmt.Errorf("git commit for DeployImageVersion: %w", err)
	}

	_, err = w.pool.Exec(ctx,
		`UPDATE operations SET status = 'Committed', git_commit = $1, git_path = $2, updated_at = NOW() WHERE id = $3`,
		sha, gitPath, op.ID)
	if err != nil {
		return fmt.Errorf("updating committed status for DeployImageVersion: %w", err)
	}

	// Update snapshot with new image
	currentSpec["image"] = payload.Image
	currentSpec["status"] = "Pending"
	currentSpec["message"] = "Image update in progress"
	updatedJSON, _ := json.Marshal(currentSpec)
	_, _ = w.pool.Exec(ctx, `
		UPDATE resource_snapshots SET phase = 'Pending', summary_json = $1, last_synced_at = NOW()
		WHERE project_id = $2 AND environment_id = $3 AND kind = 'App' AND name = $4
	`, updatedJSON, op.ProjectID, envIDVal, payload.AppName)

	if w.cfg.DevMode {
		w.simulateAppArgoAndReconcile(ctx, op.ID, op.ProjectID, op.EnvironmentID, payload.AppName, updatedJSON)
	} else {
		w.updateStatus(ctx, op.ID, models.OperationStatusWaitingForArgoSync)
	}

	return nil
}
```

### Step 2: Verify compilation

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend && go build ./...
```
Expected: no errors

### Step 3: Run all tests

```bash
cd /Users/alex/IdeaProjects/dada-cloud/backend && go test ./...
```
Expected: all PASS

### Step 4: Commit

```bash
git add backend/internal/worker/worker.go
git commit -m "feat(worker): add processDeployImageVersion — re-renders App manifest with new image"
```

---

## Task 5: Frontend — types and API client

**Files:**
- Modify: `frontend/lib/types.ts`
- Modify: `frontend/lib/api.ts`

### Step 1: Add App types to `types.ts`

Append at end of `frontend/lib/types.ts`:
```typescript
export interface AppSummary {
  image: string;
  port: number;
  replicas: number;
  profile: string;
  status: string;
  message: string;
}

export interface AppSnapshot extends ResourceSnapshot {
  summary_json: AppSummary;
}

export interface AppsResponse {
  apps: ResourceSnapshot[];
}

export interface CreateAppResponse {
  operation: Operation;
  message: string;
}

export interface DeployImageResponse {
  operation: Operation;
  message: string;
}
```

### Step 2: Add appsApi to `api.ts`

Append at end of `frontend/lib/api.ts`, also add the new types to the import at the top:

**Update top import:**
```typescript
import type {
  LoginResponse,
  User,
  ProjectsResponse,
  ProjectDetailResponse,
  OperationsResponse,
  Operation,
  DatabasesResponse,
  CreateDatabaseResponse,
  AppsResponse,
  CreateAppResponse,
  DeployImageResponse,
} from "./types";
```

**Append at end:**
```typescript
export const appsApi = {
  list: (projectId: string, envId: string) =>
    apiFetch<AppsResponse>(`/api/v1/projects/${projectId}/environments/${envId}/apps`),

  create: (projectId: string, envId: string, data: {
    name: string;
    image: string;
    port: number;
    replicas: number;
    profile: string;
  }) =>
    apiFetch<CreateAppResponse>(`/api/v1/projects/${projectId}/environments/${envId}/apps`, {
      method: "POST",
      body: data,
    }),

  updateImage: (projectId: string, envId: string, appName: string, image: string) =>
    apiFetch<DeployImageResponse>(
      `/api/v1/projects/${projectId}/environments/${envId}/apps/${appName}/image`,
      { method: "PATCH", body: { image } }
    ),
};
```

### Step 3: Verify TypeScript compiles

```bash
cd /Users/alex/IdeaProjects/dada-cloud/frontend && npx tsc --noEmit
```
Expected: no errors

### Step 4: Commit

```bash
git add frontend/lib/types.ts frontend/lib/api.ts
git commit -m "feat(frontend): add App types and appsApi client"
```

---

## Task 6: Frontend — Apps list + create form page

**Files:**
- Create: `frontend/app/(console)/projects/[projectId]/apps/page.tsx`
- Modify: `frontend/app/(console)/projects/[projectId]/page.tsx` (add Apps quick-action card)

### Step 1: Create apps page

Create `frontend/app/(console)/projects/[projectId]/apps/page.tsx`:

Pattern is identical to `databases/page.tsx` — same env tabs, same create modal, adapted for App fields.

```typescript
"use client";
import { useEffect, useState, FormEvent } from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { projectsApi, appsApi } from "@/lib/api";
import type { Environment, ResourceSnapshot } from "@/lib/types";
import { Modal } from "@/components/ui/modal";
import { Spinner } from "@/components/ui/spinner";

function timeAgo(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffSecs = Math.floor(diffMs / 1000);
  if (diffSecs < 60) return `${diffSecs}s ago`;
  const diffMins = Math.floor(diffSecs / 60);
  if (diffMins < 60) return `${diffMins}m ago`;
  const diffHours = Math.floor(diffMins / 60);
  if (diffHours < 24) return `${diffHours}h ago`;
  const diffDays = Math.floor(diffHours / 24);
  return `${diffDays}d ago`;
}

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

interface CreateAppForm {
  name: string;
  image: string;
  port: number;
  replicas: number;
  profile: string;
}

export default function AppsPage() {
  const params = useParams<{ projectId: string }>();
  const projectId = params.projectId;
  const router = useRouter();

  const [environments, setEnvironments] = useState<Environment[]>([]);
  const [selectedEnvId, setSelectedEnvId] = useState<string>("");
  const [apps, setApps] = useState<ResourceSnapshot[]>([]);
  const [isLoadingEnvs, setIsLoadingEnvs] = useState(true);
  const [isLoadingApps, setIsLoadingApps] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [isModalOpen, setIsModalOpen] = useState(false);
  const [form, setForm] = useState<CreateAppForm>({
    name: "",
    image: "",
    port: 8080,
    replicas: 2,
    profile: "small",
  });
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  useEffect(() => {
    projectsApi
      .get(projectId)
      .then((data) => {
        const envs = data.environments ?? [];
        setEnvironments(envs);
        if (envs.length > 0) setSelectedEnvId(envs[0].id);
        else setIsLoadingApps(false);
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Failed to load project");
        setIsLoadingApps(false);
      })
      .finally(() => setIsLoadingEnvs(false));
  }, [projectId]);

  useEffect(() => {
    if (!selectedEnvId) return;
    setIsLoadingApps(true);
    appsApi
      .list(projectId, selectedEnvId)
      .then((data) => setApps(data.apps ?? []))
      .catch((err) => setError(err instanceof Error ? err.message : "Failed to load apps"))
      .finally(() => setIsLoadingApps(false));
  }, [projectId, selectedEnvId]);

  function handleFormChange(field: keyof CreateAppForm, value: string | number) {
    setForm((prev) => ({ ...prev, [field]: value }));
  }

  function handleEnvironmentChange(envId: string) {
    setIsLoadingApps(true);
    setError(null);
    setSelectedEnvId(envId);
  }

  async function handleSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setSubmitError(null);
    setIsSubmitting(true);
    try {
      const result = await appsApi.create(projectId, selectedEnvId, {
        name: form.name,
        image: form.image,
        port: form.port,
        replicas: form.replicas,
        profile: form.profile,
      });
      setIsModalOpen(false);
      setForm({ name: "", image: "", port: 8080, replicas: 2, profile: "small" });
      const opId = result.operation?.id;
      setTimeout(() => {
        router.push(`/projects/${projectId}/operations${opId ? `?highlight=${opId}` : ""}`);
      }, 2000);
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : "Failed to create app");
    } finally {
      setIsSubmitting(false);
    }
  }

  const selectedEnv = environments.find((e) => e.id === selectedEnvId);

  if (isLoadingEnvs) {
    return <div className="flex h-64 items-center justify-center"><Spinner size="lg" /></div>;
  }

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
            <span className="text-gray-900">Applications</span>
          </div>
          <h1 className="mt-2 text-2xl font-bold text-gray-900">Applications</h1>
          <p className="mt-0.5 text-sm text-gray-500">Managed application workloads</p>
        </div>
        <button
          onClick={() => setIsModalOpen(true)}
          disabled={!selectedEnvId}
          className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50 transition-colors"
        >
          <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          Create App
        </button>
      </div>

      {error && (
        <div className="mb-6 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>
      )}

      {/* Environment tabs */}
      {environments.length > 0 && (
        <div className="mb-6 flex gap-1 rounded-lg border border-gray-200 bg-gray-50 p-1 w-fit">
          {environments.map((env) => (
            <button
              key={env.id}
              onClick={() => handleEnvironmentChange(env.id)}
              className={`rounded-md px-4 py-1.5 text-sm font-medium transition-colors ${
                selectedEnvId === env.id ? "bg-white text-gray-900 shadow-sm" : "text-gray-500 hover:text-gray-700"
              }`}
            >
              {env.name}
            </button>
          ))}
        </div>
      )}

      {/* App list */}
      {isLoadingApps ? (
        <div className="flex h-40 items-center justify-center"><Spinner /></div>
      ) : apps.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-xl border border-dashed border-gray-300 bg-gray-50 py-16">
          <svg className="mb-3 h-12 w-12 text-gray-300" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M5 12h14M12 5l7 7-7 7" />
          </svg>
          <p className="text-sm font-medium text-gray-500">No apps in {selectedEnv?.name ?? "this environment"}</p>
          <button onClick={() => setIsModalOpen(true)} className="mt-4 text-sm text-blue-600 hover:text-blue-700">
            Create your first app →
          </button>
        </div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {apps.map((app) => {
            const summary = app.summary_json as { image?: string; profile?: string; replicas?: number };
            return (
              <Link
                key={app.id}
                href={`/projects/${projectId}/apps/${app.name}?envId=${selectedEnvId}`}
                className="block rounded-xl border border-gray-200 bg-white p-5 shadow-sm hover:border-blue-200 hover:shadow-md transition-all"
              >
                <div className="mb-3 flex items-start justify-between">
                  <div>
                    <p className="font-mono text-sm font-semibold text-gray-900">{app.name}</p>
                    <p className="mt-0.5 text-xs text-gray-400 font-mono truncate max-w-[180px]">{summary.image ?? "—"}</p>
                  </div>
                  <PhaseBadge phase={app.phase} />
                </div>
                <div className="flex items-center gap-3 text-xs text-gray-400">
                  <span>{summary.profile ?? "small"}</span>
                  <span>·</span>
                  <span>{summary.replicas ?? 2} replicas</span>
                  <span>·</span>
                  <span>{timeAgo(app.last_synced_at)}</span>
                </div>
              </Link>
            );
          })}
        </div>
      )}

      {/* Create App Modal */}
      <Modal
        isOpen={isModalOpen}
        onClose={() => { setIsModalOpen(false); setSubmitError(null); }}
        title="Create Application"
      >
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700">
              App Name <span className="text-gray-400 font-normal">(Kubernetes resource name)</span>
            </label>
            <input
              type="text"
              required
              value={form.name}
              onChange={(e) => handleFormChange("name", e.target.value)}
              placeholder="my-service"
              pattern="[a-z0-9-]+"
              title="Lowercase letters, numbers, and hyphens only"
              className="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm text-gray-900 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700">Image</label>
            <input
              type="text"
              required
              value={form.image}
              onChange={(e) => handleFormChange("image", e.target.value)}
              placeholder="ghcr.io/org/service:v1.0.0"
              className="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm font-mono text-gray-900 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
          </div>

          <div className="grid grid-cols-3 gap-3">
            <div>
              <label className="block text-sm font-medium text-gray-700">Port</label>
              <input
                type="number"
                min={1}
                max={65535}
                value={form.port}
                onChange={(e) => handleFormChange("port", parseInt(e.target.value, 10) || 8080)}
                className="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm text-gray-900 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">Replicas</label>
              <input
                type="number"
                min={1}
                max={10}
                value={form.replicas}
                onChange={(e) => handleFormChange("replicas", parseInt(e.target.value, 10) || 2)}
                className="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm text-gray-900 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">Profile</label>
              <select
                value={form.profile}
                onChange={(e) => handleFormChange("profile", e.target.value)}
                className="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm text-gray-900 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
              >
                <option value="small">Small</option>
                <option value="medium">Medium</option>
                <option value="large">Large</option>
              </select>
            </div>
          </div>

          {submitError && (
            <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{submitError}</div>
          )}

          <div className="flex justify-end gap-3 pt-2">
            <button
              type="button"
              onClick={() => { setIsModalOpen(false); setSubmitError(null); }}
              className="rounded-lg px-4 py-2 text-sm font-medium text-gray-600 hover:bg-gray-100 transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={isSubmitting}
              className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50 transition-colors"
            >
              {isSubmitting ? <><Spinner size="sm" /> Creating...</> : "Create App"}
            </button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
```

### Step 2: Add Apps card to project overview page

**Modify `frontend/app/(console)/projects/[projectId]/page.tsx`** — add Apps link in the quick actions grid, between Databases and Operations cards:
```typescript
        <Link
          href={`/projects/${projectId}/apps`}
          className="group flex items-center gap-4 rounded-xl border border-gray-200 bg-white p-5 shadow-sm hover:border-blue-200 hover:shadow-md transition-all"
        >
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-green-100 text-green-600 group-hover:bg-green-600 group-hover:text-white transition-colors">
            <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 12h14M12 5l7 7-7 7" />
            </svg>
          </div>
          <div>
            <p className="text-sm font-semibold text-gray-900">Applications</p>
            <p className="text-xs text-gray-400">Manage app workloads</p>
          </div>
        </Link>
```

### Step 3: TypeScript check

```bash
cd /Users/alex/IdeaProjects/dada-cloud/frontend && npx tsc --noEmit
```
Expected: no errors

### Step 4: Commit

```bash
git add frontend/app/\(console\)/projects/\[projectId\]/apps/page.tsx \
        frontend/app/\(console\)/projects/\[projectId\]/page.tsx
git commit -m "feat(frontend): add apps list page with create form and project overview card"
```

---

## Task 7: Frontend — App detail page with image update

**Files:**
- Create: `frontend/app/(console)/projects/[projectId]/apps/[appName]/page.tsx`

### Step 1: Create app detail page

Create `frontend/app/(console)/projects/[projectId]/apps/[appName]/page.tsx`:

```typescript
"use client";
import { useEffect, useState, FormEvent } from "react";
import { useParams, useSearchParams, useRouter } from "next/navigation";
import Link from "next/link";
import { appsApi } from "@/lib/api";
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

export default function AppDetailPage() {
  const params = useParams<{ projectId: string; appName: string }>();
  const searchParams = useSearchParams();
  const router = useRouter();
  const { projectId, appName } = params;
  const envId = searchParams.get("envId") ?? "";

  const [app, setApp] = useState<ResourceSnapshot | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [isModalOpen, setIsModalOpen] = useState(false);
  const [newImage, setNewImage] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  useEffect(() => {
    if (!envId) return;
    appsApi
      .list(projectId, envId)
      .then((data) => {
        const found = (data.apps ?? []).find((a) => a.name === appName);
        if (!found) {
          setError("App not found");
        } else {
          setApp(found);
        }
      })
      .catch((err) => setError(err instanceof Error ? err.message : "Failed to load app"))
      .finally(() => setIsLoading(false));
  }, [projectId, appName, envId]);

  async function handleImageUpdate(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setSubmitError(null);
    setIsSubmitting(true);
    try {
      const result = await appsApi.updateImage(projectId, envId, appName, newImage);
      setIsModalOpen(false);
      setNewImage("");
      const opId = result.operation?.id;
      setTimeout(() => {
        router.push(`/projects/${projectId}/operations${opId ? `?highlight=${opId}` : ""}`);
      }, 1500);
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : "Failed to update image");
    } finally {
      setIsSubmitting(false);
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
          onClick={() => { setNewImage(summary.image ?? ""); setIsModalOpen(true); }}
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

      {/* Deploy Image Modal */}
      <Modal
        isOpen={isModalOpen}
        onClose={() => { setIsModalOpen(false); setSubmitError(null); }}
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

          {submitError && (
            <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{submitError}</div>
          )}

          <div className="flex justify-end gap-3 pt-2">
            <button
              type="button"
              onClick={() => { setIsModalOpen(false); setSubmitError(null); }}
              className="rounded-lg px-4 py-2 text-sm font-medium text-gray-600 hover:bg-gray-100 transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={isSubmitting}
              className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50 transition-colors"
            >
              {isSubmitting ? <><Spinner size="sm" /> Deploying...</> : "Deploy"}
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
Expected: no errors

### Step 3: Commit

```bash
git add "frontend/app/(console)/projects/[projectId]/apps/[appName]/page.tsx"
git commit -m "feat(frontend): add app detail page with Deploy Image modal"
```

---

## Final verification

```bash
# Backend: all tests pass
cd /Users/alex/IdeaProjects/dada-cloud/backend && go test ./... -v

# Frontend: TypeScript clean
cd /Users/alex/IdeaProjects/dada-cloud/frontend && npx tsc --noEmit

# Backend: binary builds
cd /Users/alex/IdeaProjects/dada-cloud/backend && go build ./...
```

---

## Decision Log

| Decision | Alternative | Reason |
|----------|------------|--------|
| Store App spec in `resource_snapshots.summary_json` | Read git file on deploy | Avoids git read dependency; simpler worker; consistent with existing snapshot pattern |
| `envId` passed as query param on app detail page | Store env in URL slug | Avoids breaking change to existing routing; simpler for now |
| Re-render full manifest on `DeployImageVersion` | Patch only the image field in git | Idempotent — same spec always produces same output; safe for concurrent deploys |
| Validate image with regex (not registry API) | Check registry reachability | v2 scope only; registry validation is out of scope and adds latency |
| Default port=8080, replicas=2, profile=small | Require all fields | Good UX — most apps are small services on 8080 |
