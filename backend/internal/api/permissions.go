package api

import (
	"context"

	"github.com/dada-tuda/console/backend/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// getUserProjectRole returns the user's role in a project, or error if not a member.
func (h *Handler) getUserProjectRole(ctx context.Context, userID, projectID uuid.UUID) (models.MemberRole, error) {
	var role models.MemberRole
	err := h.pool.QueryRow(ctx,
		"SELECT role FROM project_members WHERE user_id = $1 AND project_id = $2",
		userID, projectID,
	).Scan(&role)
	if err == pgx.ErrNoRows {
		return "", pgx.ErrNoRows
	}
	return role, err
}

// canWrite returns true for roles that can create/modify resources.
func canWrite(role models.MemberRole) bool {
	return role == models.MemberRolePlatformAdmin ||
		role == models.MemberRoleDeveloper ||
		role == models.MemberRoleClientAdmin
}
