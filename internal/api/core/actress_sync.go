package core

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// EnsureActressSyncManager lazily creates the durable actress metadata worker
// and binds its config/registry reads to the hot-reloadable CoreDeps snapshot.
func (r *APIRuntime) EnsureActressSyncManager() *worker.ActressSyncManager {
	if r == nil || r.deps == nil || r.deps.CoreDeps == nil || r.deps.CoreDeps.DB == nil {
		return nil
	}
	r.actressSyncMu.Lock()
	defer r.actressSyncMu.Unlock()
	if r.actressSyncManager != nil {
		return r.actressSyncManager
	}

	db := r.deps.CoreDeps.DB
	r.actressSyncManager = worker.NewActressSyncManager(worker.ActressSyncManagerDeps{
		DB:              db,
		ActressRepo:     database.NewActressRepository(db),
		MovieRepo:       database.NewMovieRepository(db),
		HistoryRepo:     database.NewHistoryRepository(db),
		BatchFileOpRepo: database.NewBatchFileOperationRepository(db),
		GetConfig:       r.deps.CoreDeps.GetConfig,
		GetRegistry:     r.deps.CoreDeps.GetRegistry,
	})
	return r.actressSyncManager
}

func (r *APIRuntime) queueMissingActressTranslations(ctx context.Context, actressIDs []uint) error {
	manager := r.EnsureActressSyncManager()
	if manager == nil {
		return fmt.Errorf("actress sync manager is unavailable")
	}
	return manager.QueueMissingTranslations(ctx, actressIDs)
}

func (r *APIRuntime) stopActressSyncManager() {
	if r == nil {
		return
	}
	r.actressSyncMu.Lock()
	manager := r.actressSyncManager
	r.actressSyncManager = nil
	r.actressSyncMu.Unlock()
	if manager != nil {
		manager.Stop()
	}
}
