package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InsertCommit records a git commit pushed by the agent.
// operationID may be nil for commits from the git→DB sync path.
func InsertCommit(ctx context.Context, pool *pgxpool.Pool,
	sha, repoURL, branch, path, message, authorName, authorEmail string,
	operationID *uuid.UUID, source string,
) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO git_commits
			(sha, repo_url, branch, path, message, author_name, author_email, operation_id, source)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT DO NOTHING
	`, sha, repoURL, branch, path, message, authorName, authorEmail, operationID, source)
	if err != nil {
		return fmt.Errorf("insert git_commit: %w", err)
	}
	return nil
}
