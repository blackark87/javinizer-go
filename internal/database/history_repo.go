package database

import (
	"context"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

// HistoryRepository persists and queries operation history records.
type HistoryRepository struct {
	*BaseRepository[models.History, uint]
}

// NewHistoryRepository creates a HistoryRepository backed by the given database.
func NewHistoryRepository(db *DB) *HistoryRepository {
	return &HistoryRepository{
		BaseRepository: NewBaseRepository[models.History, uint](
			db, "history",
			func(h models.History) string { return fmt.Sprintf("%d", h.ID) },
			withDefaultOrder[models.History, uint]("created_at DESC"),
			WithNewEntity[models.History, uint](func() models.History { return models.History{} }),
		),
	}
}

// Create inserts a new history record.
func (r *HistoryRepository) Create(ctx context.Context, history *models.History) error {
	return r.BaseRepository.Create(ctx, history)
}

// FindByID returns the history record with the given primary key.
func (r *HistoryRepository) FindByID(ctx context.Context, id uint) (*models.History, error) {
	return r.BaseRepository.FindByID(ctx, id)
}

// FindByMovieID returns all history records for the given movie ID, newest first.
func (r *HistoryRepository) FindByMovieID(ctx context.Context, movieID string) ([]models.History, error) {
	var history []models.History
	err := r.GetDB().WithContext(ctx).Where("movie_id = ?", movieID).Order("created_at DESC").Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history for movie %s", movieID), err)
	}
	return history, nil
}

// FindLatestSuccessfulOperation returns the newest successful history entry
// for a movie and operation.
func (r *HistoryRepository) FindLatestSuccessfulOperation(ctx context.Context, movieID string, operation models.HistoryOperation) (*models.History, error) {
	var entry models.History
	err := r.GetDB().WithContext(ctx).
		Where("movie_id = ? AND operation = ? AND status = ?", movieID, operation, models.HistoryStatusSuccess).
		Order("created_at DESC, id DESC").First(&entry).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("latest %s history for movie %s", operation, movieID), err)
	}
	return &entry, nil
}

// ListByMovieID returns a paginated slice of history records for the given movie
// ID, ordered by most recent first. When limit is zero no pagination is applied,
// matching the semantics of BaseRepository.List.
func (r *HistoryRepository) ListByMovieID(ctx context.Context, movieID string, limit, offset int) ([]models.History, error) {
	var history []models.History
	query := r.GetDB().WithContext(ctx).Where("movie_id = ?", movieID).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit).Offset(offset)
	}
	err := query.Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history for movie %s", movieID), err)
	}
	return history, nil
}

// CountByMovieID returns the total number of history records for the given movie ID.
func (r *HistoryRepository) CountByMovieID(ctx context.Context, movieID string) (int64, error) {
	var count int64
	err := r.GetDB().WithContext(ctx).Model(&models.History{}).Where("movie_id = ?", movieID).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("history for movie %s", movieID), err)
	}
	return count, nil
}

// FindByOperation returns history records for the given operation, newest first, capped at limit when limit is positive.
func (r *HistoryRepository) FindByOperation(ctx context.Context, operation models.HistoryOperation, limit int) ([]models.History, error) {
	var history []models.History
	query := r.GetDB().WithContext(ctx).Where("operation = ?", operation).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history by operation %s", operation), err)
	}
	return history, nil
}

// ListByOperation returns a paginated slice of history records for the given
// operation, ordered by most recent first. When limit is zero no pagination is
// applied, matching the semantics of BaseRepository.List.
func (r *HistoryRepository) ListByOperation(ctx context.Context, operation models.HistoryOperation, limit, offset int) ([]models.History, error) {
	var history []models.History
	query := r.GetDB().WithContext(ctx).Where("operation = ?", operation).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit).Offset(offset)
	}
	err := query.Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history by operation %s", operation), err)
	}
	return history, nil
}

// FindByStatus returns history records with the given status, newest first, capped at limit when limit is positive.
func (r *HistoryRepository) FindByStatus(ctx context.Context, status models.HistoryStatus, limit int) ([]models.History, error) {
	var history []models.History
	query := r.GetDB().WithContext(ctx).Where("status = ?", status).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history by status %s", status), err)
	}
	return history, nil
}

// ListByStatus returns a paginated slice of history records for the given status,
// ordered by most recent first. When limit is zero no pagination is applied,
// matching the semantics of BaseRepository.List.
func (r *HistoryRepository) ListByStatus(ctx context.Context, status models.HistoryStatus, limit, offset int) ([]models.History, error) {
	var history []models.History
	query := r.GetDB().WithContext(ctx).Where("status = ?", status).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit).Offset(offset)
	}
	err := query.Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history by status %s", status), err)
	}
	return history, nil
}

// FindRecent returns the most recent history records up to limit.
func (r *HistoryRepository) FindRecent(ctx context.Context, limit int) ([]models.History, error) {
	var history []models.History
	err := r.GetDB().WithContext(ctx).Order("created_at DESC").Limit(limit).Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", "recent history", err)
	}
	return history, nil
}

// FindByDateRange returns history records created within the [start, end] range, newest first.
func (r *HistoryRepository) FindByDateRange(ctx context.Context, start, end time.Time) ([]models.History, error) {
	var history []models.History
	err := r.GetDB().WithContext(ctx).Where("datetime(created_at) BETWEEN datetime(?) AND datetime(?)", start.Format(sqliteTimeFormat), end.Format(sqliteTimeFormat)).Order("created_at DESC").Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", "history by date range", err)
	}
	return history, nil
}

// Count returns the total number of history records.
func (r *HistoryRepository) Count(ctx context.Context) (int64, error) {
	return r.BaseRepository.Count(ctx)
}

// CountByStatus returns the number of history records with the given status.
func (r *HistoryRepository) CountByStatus(ctx context.Context, status models.HistoryStatus) (int64, error) {
	var count int64
	err := r.GetDB().WithContext(ctx).Model(&models.History{}).Where("status = ?", status).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("history by status %s", status), err)
	}
	return count, nil
}

// CountByOperation returns the number of history records with the given operation.
func (r *HistoryRepository) CountByOperation(ctx context.Context, operation models.HistoryOperation) (int64, error) {
	var count int64
	err := r.GetDB().WithContext(ctx).Model(&models.History{}).Where("operation = ?", operation).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("history by operation %s", operation), err)
	}
	return count, nil
}

// Delete removes the history record with the given primary key.
func (r *HistoryRepository) Delete(ctx context.Context, id uint) error {
	return r.BaseRepository.Delete(ctx, id)
}

// DeleteByMovieID removes all history records for the given movie ID.
func (r *HistoryRepository) DeleteByMovieID(ctx context.Context, movieID string) error {
	if err := r.GetDB().WithContext(ctx).Where("movie_id = ?", movieID).Delete(&models.History{}).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("history for movie %s", movieID), err)
	}
	return nil
}

// DeleteOlderThan removes history records created before the given date.
func (r *HistoryRepository) DeleteOlderThan(ctx context.Context, date time.Time) error {
	if err := r.GetDB().WithContext(ctx).Where("datetime(created_at) < datetime(?)", date.UTC().Format(sqliteTimeFormat)).Delete(&models.History{}).Error; err != nil {
		return wrapDBErr("delete", "history older than date", err)
	}
	return nil
}

// List returns a paginated slice of history records ordered by created_at descending.
func (r *HistoryRepository) List(ctx context.Context, limit, offset int) ([]models.History, error) {
	return r.BaseRepository.List(ctx, limit, offset)
}

// FindByBatchJobID returns all history records for the given batch job ID, oldest first.
func (r *HistoryRepository) FindByBatchJobID(ctx context.Context, batchJobID string) ([]models.History, error) {
	var history []models.History
	err := r.GetDB().WithContext(ctx).Where("batch_job_id = ?", batchJobID).Order("created_at ASC").Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history for batch job %s", batchJobID), err)
	}
	return history, nil
}
