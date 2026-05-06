package api

import (
	"net/http"

	"github.com/dada-tuda/console/backend/internal/auth"
	"github.com/dada-tuda/console/backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// GetOperation returns the current state of an async platform operation.
func (h *Handler) GetOperation(c *gin.Context) {
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
	operationID, err := uuid.Parse(c.Param("operationId"))
	if err != nil {
		respondNotFound(c)
		return
	}

	// Verify project membership (404 to avoid enumeration)
	_, err = h.getUserProjectRole(c.Request.Context(), claims.UserID, projectID)
	if err == pgx.ErrNoRows {
		respondNotFound(c)
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to check project membership")
		return
	}

	var op models.Operation
	err = h.pool.QueryRow(c.Request.Context(),
		`SELECT id, actor_id, project_id, environment_id, action, resource_kind, resource_name,
		        status, payload, validation_result, git_commit, git_path, argo_application,
		        error_code, error_message, created_at, updated_at
		 FROM operations WHERE id = $1 AND project_id = $2`,
		operationID, projectID,
	).Scan(
		&op.ID, &op.ActorID, &op.ProjectID, &op.EnvironmentID,
		&op.Action, &op.ResourceKind, &op.ResourceName,
		&op.Status, &op.Payload, &op.ValidationResult,
		&op.GitCommit, &op.GitPath, &op.ArgoApplication,
		&op.ErrorCode, &op.ErrorMessage, &op.CreatedAt, &op.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		respondNotFound(c)
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to fetch operation")
		return
	}

	c.JSON(http.StatusOK, gin.H{"operation": op})
}

// RetryOperation re-queues a failed operation for another processing attempt.
func (h *Handler) RetryOperation(c *gin.Context) {
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
	operationID, err := uuid.Parse(c.Param("operationId"))
	if err != nil {
		respondNotFound(c)
		return
	}

	// Verify project membership
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

	// Fetch current status
	var currentStatus models.OperationStatus
	err = h.pool.QueryRow(c.Request.Context(),
		`SELECT status FROM operations WHERE id = $1 AND project_id = $2`,
		operationID, projectID,
	).Scan(&currentStatus)
	if err == pgx.ErrNoRows {
		respondNotFound(c)
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to fetch operation")
		return
	}

	if currentStatus != models.OperationStatusFailed {
		respondError(c, http.StatusConflict, "only failed operations can be retried")
		return
	}

	// Reset to Queued
	var op models.Operation
	err = h.pool.QueryRow(c.Request.Context(),
		`UPDATE operations
		 SET status = 'Queued', updated_at = NOW()
		 WHERE id = $1 AND project_id = $2
		 RETURNING id, actor_id, project_id, environment_id, action, resource_kind, resource_name,
		           status, payload, validation_result, git_commit, git_path, argo_application,
		           error_code, error_message, created_at, updated_at`,
		operationID, projectID,
	).Scan(
		&op.ID, &op.ActorID, &op.ProjectID, &op.EnvironmentID,
		&op.Action, &op.ResourceKind, &op.ResourceName,
		&op.Status, &op.Payload, &op.ValidationResult,
		&op.GitCommit, &op.GitPath, &op.ArgoApplication,
		&op.ErrorCode, &op.ErrorMessage, &op.CreatedAt, &op.UpdatedAt,
	)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to retry operation")
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"operation": op})
}
