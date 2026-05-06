package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Project represents a tenant project / namespace boundary in the platform.
type Project struct {
	ID                 uuid.UUID       `json:"id"                   db:"id"`
	Name               string          `json:"name"                 db:"name"`           // slug: internal, client-a
	DisplayName        string          `json:"display_name"         db:"display_name"`
	OwnerType          string          `json:"owner_type"           db:"owner_type"`     // team | client
	OwnerID            *uuid.UUID      `json:"owner_id,omitempty"   db:"owner_id"`
	DefaultEnvironment string          `json:"default_environment"  db:"default_environment"`
	Quotas             json.RawMessage `json:"quotas"               db:"quotas"`
	CreatedAt          time.Time       `json:"created_at"           db:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"           db:"updated_at"`
}

// EnvironmentType constrains environment kinds.
type EnvironmentType string

const (
	EnvironmentTypeDev  EnvironmentType = "dev"
	EnvironmentTypeProd EnvironmentType = "prod"
)

// Environment represents a deployment environment within a project (e.g. dev, prod).
type Environment struct {
	ID        uuid.UUID       `json:"id"         db:"id"`
	ProjectID uuid.UUID       `json:"project_id" db:"project_id"`
	Name      string          `json:"name"       db:"name"`      // dev, prod
	Namespace string          `json:"namespace"  db:"namespace"` // k8s namespace: internal-prod
	Type      EnvironmentType `json:"type"       db:"type"`
	CreatedAt time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt time.Time       `json:"updated_at" db:"updated_at"`
}
