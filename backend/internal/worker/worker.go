package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dada-tuda/console/backend/internal/config"
	"github.com/dada-tuda/console/backend/internal/gitwriter"
	"github.com/dada-tuda/console/backend/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

type Worker struct {
	pool         *pgxpool.Pool
	cfg          *config.Config
	gitWriter    *gitwriter.GitWriter
	pollInterval time.Duration
}

func New(pool *pgxpool.Pool, cfg *config.Config) *Worker {
	return &Worker{
		pool:         pool,
		cfg:          cfg,
		gitWriter:    gitwriter.New(cfg),
		pollInterval: 3 * time.Second,
	}
}

func (w *Worker) Start(ctx context.Context) {
	log.Info().Msg("worker started, poll interval 3s")
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("worker stopping")
			return
		case <-ticker.C:
			w.pollAndProcess(ctx)
		}
	}
}

// pollAndProcess claims one Queued operation using SELECT FOR UPDATE SKIP LOCKED
// and processes it. This ensures safe concurrent workers (though v1 uses one worker).
func (w *Worker) pollAndProcess(ctx context.Context) {
	// First, advance any Created operations to Queued
	w.advanceCreatedToQueued(ctx)

	// Claim and process one Queued operation
	op, err := w.claimNextOperation(ctx)
	if err != nil {
		log.Error().Err(err).Msg("claiming next operation")
		return
	}
	if op == nil {
		return // nothing to do
	}

	log.Info().Str("op_id", op.ID.String()).Str("action", op.Action).Msg("processing operation")
	if err := w.processOperation(ctx, op); err != nil {
		log.Error().Err(err).Str("op_id", op.ID.String()).Msg("operation failed")
		w.failOperation(ctx, op.ID, "PROCESSING_ERROR", err.Error())
	}
}

// advanceCreatedToQueued moves Created operations to Queued status.
// Created → Queued happens immediately (no validation needed for v1).
func (w *Worker) advanceCreatedToQueued(ctx context.Context) {
	_, err := w.pool.Exec(ctx,
		`UPDATE operations SET status = 'Queued', updated_at = NOW()
         WHERE status = 'Created'`)
	if err != nil {
		log.Error().Err(err).Msg("advancing Created to Queued")
	}
}

// claimNextOperation atomically claims one Queued operation.
func (w *Worker) claimNextOperation(ctx context.Context) (*models.Operation, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var op models.Operation
	// Nullable string columns must be scanned into *string to handle NULL.
	var gitCommit, gitPath, argoApp, errorCode, errorMessage *string
	err = tx.QueryRow(ctx, `
        SELECT o.id, o.actor_id, o.project_id, o.environment_id, o.action,
               o.resource_kind, o.resource_name, o.status, o.payload,
               o.validation_result, o.git_commit, o.git_path, o.argo_application,
               o.error_code, o.error_message, o.created_at, o.updated_at
        FROM operations o
        WHERE o.status = 'Queued'
        ORDER BY o.created_at ASC
        LIMIT 1
        FOR UPDATE SKIP LOCKED
    `).Scan(
		&op.ID, &op.ActorID, &op.ProjectID, &op.EnvironmentID, &op.Action,
		&op.ResourceKind, &op.ResourceName, &op.Status, &op.Payload,
		&op.ValidationResult, &gitCommit, &gitPath, &argoApp,
		&errorCode, &errorMessage, &op.CreatedAt, &op.UpdatedAt,
	)
	if gitCommit != nil {
		op.GitCommit = *gitCommit
	}
	if gitPath != nil {
		op.GitPath = *gitPath
	}
	if argoApp != nil {
		op.ArgoApplication = *argoApp
	}
	if errorCode != nil {
		op.ErrorCode = *errorCode
	}
	if errorMessage != nil {
		op.ErrorMessage = *errorMessage
	}
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("querying queued operation: %w", err)
	}

	// Mark as Rendering to prevent re-claim
	_, err = tx.Exec(ctx,
		"UPDATE operations SET status = 'Rendering', updated_at = NOW() WHERE id = $1",
		op.ID)
	if err != nil {
		return nil, fmt.Errorf("marking rendering: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim: %w", err)
	}

	op.Status = models.OperationStatusRendering
	return &op, nil
}

// processOperation routes an operation to the appropriate handler based on its action.
func (w *Worker) processOperation(ctx context.Context, op *models.Operation) error {
	switch op.Action {
	case "CreateServiceDatabase":
		return w.processCreateServiceDatabase(ctx, op)
	case "CreateApp":
		return w.processCreateApp(ctx, op)
	case "DeployImageVersion":
		return w.processDeployImageVersion(ctx, op)
	case "CreatePublicApi":
		return w.processCreatePublicApi(ctx, op)
	default:
		return fmt.Errorf("unknown action: %s", op.Action)
	}
}

func (w *Worker) processCreateServiceDatabase(ctx context.Context, op *models.Operation) error {
	// Parse payload
	var payload models.CreateServiceDatabasePayload
	if err := json.Unmarshal(op.Payload, &payload); err != nil {
		return fmt.Errorf("parsing payload: %w", err)
	}

	// Look up project and environment info for namespace/slug
	var projectName, envName, envNamespace string
	err := w.pool.QueryRow(ctx,
		`SELECT p.name, e.name, e.namespace
         FROM projects p JOIN environments e ON e.project_id = p.id
         WHERE p.id = $1 AND e.id = $2`,
		op.ProjectID, op.EnvironmentID,
	).Scan(&projectName, &envName, &envNamespace)
	if err != nil {
		return fmt.Errorf("fetching project/env: %w", err)
	}

	// Render manifest
	w.updateStatus(ctx, op.ID, models.OperationStatusRendering)
	spec := gitwriter.ServiceDatabaseSpec{
		Name:            payload.Name,
		Namespace:       envNamespace,
		ProjectSlug:     projectName,
		EnvSlug:         envName,
		AppRef:          payload.AppRef,
		Database:        payload.Database,
		BackupEnabled:   payload.BackupEnabled,
		BackupSchedule:  defaultIfEmpty(payload.BackupSchedule, "daily"),
		BackupRetention: defaultIfEmpty(payload.BackupRetention, "14d"),
		OperationID:     op.ID.String(),
	}
	yaml, err := gitwriter.RenderServiceDatabase(spec)
	if err != nil {
		return fmt.Errorf("rendering manifest: %w", err)
	}

	// Commit to Git
	w.updateStatus(ctx, op.ID, models.OperationStatusCommittingToGit)
	gitPath := gitwriter.ServiceDatabaseGitPath(projectName, envName, payload.AppRef)
	commitMsg := fmt.Sprintf("[DADA Console] Create ServiceDatabase %s for project %s\n\nOperation: %s\nActor: %s\nProject: %s\nEnvironment: %s\nResource: ServiceDatabase/%s/%s\n",
		payload.Name, projectName,
		op.ID, op.ActorID, projectName, envName,
		envName, payload.Name,
	)
	sha, err := w.gitWriter.CommitManifest(gitPath, yaml, commitMsg)
	if err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	// Update operation with git commit info
	_, err = w.pool.Exec(ctx,
		`UPDATE operations SET status = 'Committed', git_commit = $1, git_path = $2, updated_at = NOW() WHERE id = $3`,
		sha, gitPath, op.ID)
	if err != nil {
		return fmt.Errorf("updating committed status: %w", err)
	}
	log.Info().Str("op_id", op.ID.String()).Str("sha", sha[:8]).Str("path", gitPath).Msg("committed to git")

	// In DEV_MODE: simulate Argo + reconcile progression
	if w.cfg.DevMode {
		w.simulateArgoAndReconcile(ctx, op.ID, op.ProjectID, op.EnvironmentID, payload.Name)
	} else {
		// In prod: transition to WaitingForArgoSync, status watcher goroutine takes over
		w.updateStatus(ctx, op.ID, models.OperationStatusWaitingForArgoSync)
	}

	return nil
}

// simulateArgoAndReconcile simulates the Argo CD → K8s reconcile flow in dev mode.
// It advances the operation through: Committed → WaitingForArgoSync → Syncing → Reconciling → Ready
// and creates a ResourceSnapshot when Ready.
func (w *Worker) simulateArgoAndReconcile(ctx context.Context, opID, projectID uuid.UUID, environmentID *uuid.UUID, resourceName string) {
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
		log.Info().Str("op_id", opID.String()).Str("status", string(step.status)).Msg("operation status advanced")
	}

	// Create ResourceSnapshot when Ready
	var envIDVal interface{} = nil
	if environmentID != nil {
		envIDVal = *environmentID
	}
	_, err := w.pool.Exec(ctx, `
        INSERT INTO resource_snapshots (project_id, environment_id, kind, name, phase, summary_json)
        VALUES ($1, $2, 'ServiceDatabase', $3, 'Ready', '{"status":"Ready","message":"Database provisioned by DADA Console"}')
        ON CONFLICT (project_id, environment_id, kind, name)
        DO UPDATE SET phase = 'Ready', summary_json = EXCLUDED.summary_json, last_synced_at = NOW()
    `, projectID, envIDVal, resourceName)
	if err != nil {
		log.Error().Err(err).Msg("creating resource snapshot")
	} else {
		log.Info().Str("resource", resourceName).Msg("resource snapshot created: Ready")
	}
}

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
	log.Info().Str("op_id", op.ID.String()).Str("sha", sha[:8]).Str("path", gitPath).Msg("committed App to git")

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
		// Return error: DeployImageVersion depends on this snapshot existing to re-render.
		return fmt.Errorf("upserting App resource snapshot: %w", err)
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
		log.Info().Str("op_id", opID.String()).Str("status", string(step.status)).Msg("app operation status advanced")
	}

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
	log.Info().Str("op_id", op.ID.String()).Str("sha", sha[:8]).Str("path", gitPath).Msg("committed deploy to git")

	// Update snapshot with new image, phase=Pending
	currentSpec["image"] = payload.Image
	currentSpec["status"] = "Pending"
	currentSpec["message"] = "Image update in progress"
	updatedJSON, _ := json.Marshal(currentSpec)
	if _, err = w.pool.Exec(ctx, `
		UPDATE resource_snapshots SET phase = 'Pending', summary_json = $1, last_synced_at = NOW()
		WHERE project_id = $2 AND environment_id = $3 AND kind = 'App' AND name = $4
	`, updatedJSON, op.ProjectID, envIDVal, payload.AppName); err != nil {
		log.Error().Err(err).Str("app", payload.AppName).Msg("updating App snapshot for DeployImageVersion")
	}

	if w.cfg.DevMode {
		w.simulateAppArgoAndReconcile(ctx, op.ID, op.ProjectID, op.EnvironmentID, payload.AppName, updatedJSON)
	} else {
		w.updateStatus(ctx, op.ID, models.OperationStatusWaitingForArgoSync)
	}
	return nil
}

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

	var envIDVal interface{} = nil
	if op.EnvironmentID != nil {
		envIDVal = *op.EnvironmentID
	}

	// Read app port from snapshot
	var summaryRaw []byte
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

func (w *Worker) updateStatus(ctx context.Context, opID uuid.UUID, status models.OperationStatus) {
	_, err := w.pool.Exec(ctx,
		"UPDATE operations SET status = $1, updated_at = NOW() WHERE id = $2",
		string(status), opID)
	if err != nil {
		log.Error().Err(err).Str("op_id", opID.String()).Str("status", string(status)).Msg("updating status")
	}
}

func (w *Worker) failOperation(ctx context.Context, opID uuid.UUID, code, message string) {
	_, err := w.pool.Exec(ctx,
		"UPDATE operations SET status = 'Failed', error_code = $1, error_message = $2, updated_at = NOW() WHERE id = $3",
		code, message, opID)
	if err != nil {
		log.Error().Err(err).Msg("marking operation failed")
	}
}

func defaultIfEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
