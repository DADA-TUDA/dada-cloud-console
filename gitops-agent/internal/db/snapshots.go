package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UpsertSnapshot upserts a resource_snapshots row using LWW:
// the update is skipped if the existing last_synced_at is newer than syncedAt.
func UpsertSnapshot(ctx context.Context, pool *pgxpool.Pool,
	projectID uuid.UUID, environmentID *uuid.UUID,
	kind, name, phase string,
	summaryJSON json.RawMessage,
	syncedAt time.Time,
) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO resource_snapshots
			(project_id, environment_id, kind, name, phase, summary_json, last_synced_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (project_id, environment_id, kind, name) DO UPDATE
		SET phase          = EXCLUDED.phase,
		    summary_json   = EXCLUDED.summary_json,
		    last_synced_at = EXCLUDED.last_synced_at
		WHERE resource_snapshots.last_synced_at < EXCLUDED.last_synced_at
	`, projectID, environmentID, kind, name, phase, summaryJSON, syncedAt)
	if err != nil {
		return fmt.Errorf("upsert snapshot: %w", err)
	}
	return nil
}
