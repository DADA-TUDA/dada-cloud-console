package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// OperationStatus represents the lifecycle state of an async platform operation.
type OperationStatus string

const (
	OperationStatusCreated            OperationStatus = "Created"
	OperationStatusValidated          OperationStatus = "Validated"
	OperationStatusQueued             OperationStatus = "Queued"
	OperationStatusRendering          OperationStatus = "Rendering"
	OperationStatusCommittingToGit    OperationStatus = "CommittingToGit"
	OperationStatusCommitted          OperationStatus = "Committed"
	OperationStatusWaitingForArgoSync OperationStatus = "WaitingForArgoSync"
	OperationStatusSyncing            OperationStatus = "Syncing"
	OperationStatusReconciling        OperationStatus = "Reconciling"
	OperationStatusReady              OperationStatus = "Ready"
	OperationStatusFailed             OperationStatus = "Failed"
	OperationStatusCancelled          OperationStatus = "Cancelled"
	OperationStatusWaitingForApproval OperationStatus = "WaitingForApproval"
)

// CreateServiceDatabasePayload is the typed payload for CreateServiceDatabase operations.
type CreateServiceDatabasePayload struct {
	Name            string `json:"name"`
	Database        string `json:"database"`
	AppRef          string `json:"app_ref"`
	BackupEnabled   bool   `json:"backup_enabled"`
	BackupSchedule  string `json:"backup_schedule"`
	BackupRetention string `json:"backup_retention"`
}

// Operation represents an async, GitOps-backed platform operation.
// Field names and db tags mirror the operations table columns exactly.
type Operation struct {
	ID               uuid.UUID       `json:"id"                          db:"id"`
	ActorID          uuid.UUID       `json:"actor_id"                    db:"actor_id"`
	ProjectID        uuid.UUID       `json:"project_id"                  db:"project_id"`
	EnvironmentID    *uuid.UUID      `json:"environment_id,omitempty"    db:"environment_id"`
	Action           string          `json:"action"                      db:"action"`           // CreateServiceDatabase, CreateApp, etc.
	ResourceKind     string          `json:"resource_kind"               db:"resource_kind"`    // ServiceDatabase, App, ServiceEndpoint
	ResourceName     string          `json:"resource_name"               db:"resource_name"`
	Status           OperationStatus `json:"status"                      db:"status"`
	Payload          json.RawMessage `json:"payload"                     db:"payload"`
	ValidationResult json.RawMessage `json:"validation_result,omitempty" db:"validation_result"`
	GitCommit        string          `json:"git_commit,omitempty"        db:"git_commit"`
	GitPath          string          `json:"git_path,omitempty"          db:"git_path"`
	ArgoApplication  string          `json:"argo_application,omitempty"  db:"argo_application"`
	ErrorCode        string          `json:"error_code,omitempty"        db:"error_code"`
	ErrorMessage     string          `json:"error_message,omitempty"     db:"error_message"`
	CreatedAt        time.Time       `json:"created_at"                  db:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"                  db:"updated_at"`
}
