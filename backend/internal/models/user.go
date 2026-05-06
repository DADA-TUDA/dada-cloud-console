package models

import (
	"time"

	"github.com/google/uuid"
)

// MemberRole defines the role a user holds within a specific project context.
// Roles are stored in project_members.role, not on the user row itself.
type MemberRole string

const (
	MemberRolePlatformAdmin MemberRole = "platform-admin"
	MemberRoleDeveloper     MemberRole = "developer"
	MemberRoleClientAdmin   MemberRole = "client-admin"
	MemberRoleClientViewer  MemberRole = "client-viewer"
)

// User represents an authenticated platform user.
// Role is not stored on the user row; it is resolved from project_members
// for a specific project context and populated at query time.
type User struct {
	ID           uuid.UUID `json:"id"           db:"id"`
	Username     string    `json:"username"     db:"username"`
	Email        string    `json:"email"        db:"email"`
	PasswordHash string    `json:"-"            db:"password_hash"`
	DisplayName  string    `json:"display_name" db:"display_name"`
	CreatedAt    time.Time `json:"created_at"   db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"   db:"updated_at"`
}

// ProjectMember links a user to a project with a specific role.
type ProjectMember struct {
	ID        uuid.UUID  `json:"id"         db:"id"`
	ProjectID uuid.UUID  `json:"project_id" db:"project_id"`
	UserID    uuid.UUID  `json:"user_id"    db:"user_id"`
	Role      MemberRole `json:"role"       db:"role"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
}
