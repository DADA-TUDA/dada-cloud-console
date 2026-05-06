package api

import (
	"github.com/dada-tuda/console/backend/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Handler holds shared dependencies for all API handlers.
type Handler struct {
	pool *pgxpool.Pool
	cfg  *config.Config
}

// NewHandler constructs a Handler with the given dependencies.
func NewHandler(pool *pgxpool.Pool, cfg *config.Config) *Handler {
	return &Handler{pool: pool, cfg: cfg}
}
