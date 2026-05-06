package worker

import (
	"context"
	"time"

	"github.com/dada-tuda/console/backend/internal/config"
	"github.com/dada-tuda/console/backend/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// Worker polls the database for queued operations and processes them sequentially.
type Worker struct {
	pool     *pgxpool.Pool
	cfg      *config.Config
	pollInterval time.Duration
}

// New creates a Worker with the given dependencies.
func New(pool *pgxpool.Pool, cfg *config.Config) *Worker {
	return &Worker{
		pool:         pool,
		cfg:          cfg,
		pollInterval: 5 * time.Second,
	}
}

// Start begins the worker polling loop. It blocks until ctx is cancelled.
func (w *Worker) Start(ctx context.Context) {
	log.Info().Msg("worker started")
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

// pollAndProcess fetches the next queued operation and processes it.
func (w *Worker) pollAndProcess(ctx context.Context) {
	// TODO: SELECT ... FOR UPDATE SKIP LOCKED to claim an operation
	// For now this is a no-op placeholder
	log.Debug().Msg("worker poll tick")
}

// processOperation drives a single operation through the GitOps pipeline.
func (w *Worker) processOperation(ctx context.Context, op *models.Operation) error {
	log.Info().Str("operation_id", op.ID.String()).Str("action", op.Action).Str("resource_kind", op.ResourceKind).Msg("processing operation")

	// Pipeline steps (to be implemented in Task 3+):
	// 1. Validate            → OperationStatusValidated
	// 2. Render manifest     → OperationStatusRendering
	// 3. Commit to Git       → OperationStatusCommittingToGit → OperationStatusCommitted
	// 4. Wait for Argo sync  → OperationStatusWaitingForArgoSync → OperationStatusSyncing
	// 5. Watch reconciliation → OperationStatusReconciling → OperationStatusReady

	// TODO: implement each step, updating op.Status in DB after each transition
	return nil
}
