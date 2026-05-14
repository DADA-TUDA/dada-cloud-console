package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Project holds the project catalog fields used by gitops-agent.
type Project struct {
	ID                 uuid.UUID
	Name               string
	DisplayName        string
	OwnerType          string
	DefaultEnvironment string
	Quotas             json.RawMessage
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// ListProjects returns all projects from the catalog.
func ListProjects(ctx context.Context, pool *pgxpool.Pool) ([]Project, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, name, display_name, owner_type, default_environment, quotas, created_at, updated_at
		FROM projects
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("query projects: %w", err)
	}
	defer rows.Close()

	var result []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(
			&p.ID, &p.Name, &p.DisplayName, &p.OwnerType, &p.DefaultEnvironment,
			&p.Quotas, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// UpsertProject stores a project catalog row, preferring values from git.
func UpsertProject(ctx context.Context, pool *pgxpool.Pool,
	name, displayName, ownerType, defaultEnvironment string,
	quotas json.RawMessage,
) error {
	if quotas == nil {
		quotas = json.RawMessage(`{}`)
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO projects
			(name, display_name, owner_type, default_environment, quotas)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (name) DO UPDATE
		SET display_name         = EXCLUDED.display_name,
		    owner_type           = EXCLUDED.owner_type,
		    default_environment   = EXCLUDED.default_environment,
		    quotas                = EXCLUDED.quotas,
		    updated_at            = NOW()
	`, name, displayName, ownerType, defaultEnvironment, quotas)
	if err != nil {
		return fmt.Errorf("upsert project: %w", err)
	}
	return nil
}
