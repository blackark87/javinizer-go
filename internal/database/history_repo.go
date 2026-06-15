package database

import (
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

type HistoryRepository struct {
	*BaseRepository[models.History, uint]
}

// HistoryFilter contains optional filters for querying history records.
type HistoryFilter struct {
	Operation string
	Status    string
	MovieID   string
}

func NewHistoryRepository(db *DB) *HistoryRepository {
	return &HistoryRepository{
		BaseRepository: NewBaseRepository[models.History, uint](
			db, "history",
			func(h models.History) string { return fmt.Sprintf("%d", h.ID) },
			WithDefaultOrder[models.History, uint]("created_at DESC"),
			WithNewEntity[models.History, uint](func() models.History { return models.History{} }),
		),
	}
}

func (r *HistoryRepository) Create(history *models.History) error {
	return r.BaseRepository.Create(history)
}

func (r *HistoryRepository) FindByID(id uint) (*models.History, error) {
	return r.BaseRepository.FindByID(id)
}

func (r *HistoryRepository) FindByMovieID(movieID string) ([]models.History, error) {
	var history []models.History
	err := r.GetDB().Where("movie_id = ?", movieID).Order("created_at DESC").Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history for movie %s", movieID), err)
	}
	return history, nil
}

func (r *HistoryRepository) FindByOperation(operation string, limit int) ([]models.History, error) {
	var history []models.History
	query := r.GetDB().Where("operation = ?", operation).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history by operation %s", operation), err)
	}
	return history, nil
}

func (r *HistoryRepository) FindByStatus(status string, limit int) ([]models.History, error) {
	var history []models.History
	query := r.GetDB().Where("status = ?", status).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history by status %s", status), err)
	}
	return history, nil
}

func (r *HistoryRepository) FindRecent(limit int) ([]models.History, error) {
	var history []models.History
	err := r.GetDB().Order("created_at DESC").Limit(limit).Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", "recent history", err)
	}
	return history, nil
}

func (r *HistoryRepository) FindByDateRange(start, end time.Time) ([]models.History, error) {
	var history []models.History
	err := r.GetDB().Where("datetime(created_at) BETWEEN datetime(?) AND datetime(?)", start.Format(SqliteTimeFormat), end.Format(SqliteTimeFormat)).Order("created_at DESC").Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", "history by date range", err)
	}
	return history, nil
}

func (r *HistoryRepository) Count() (int64, error) {
	return r.BaseRepository.Count()
}

func (r *HistoryRepository) applyFilter(filter HistoryFilter) *gorm.DB {
	query := r.GetDB().Model(&models.History{})
	if filter.Operation != "" {
		query = query.Where("operation = ?", filter.Operation)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.MovieID != "" {
		query = query.Where("movie_id = ?", filter.MovieID)
	}
	return query
}

func (r *HistoryRepository) ListFiltered(filter HistoryFilter, limit int, offset int) ([]models.History, error) {
	var history []models.History
	query := r.applyFilter(filter).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit).Offset(offset)
	}
	err := query.Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", "filtered history", err)
	}
	return history, nil
}

func (r *HistoryRepository) CountFiltered(filter HistoryFilter) (int64, error) {
	var count int64
	err := r.applyFilter(filter).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", "filtered history", err)
	}
	return count, nil
}

func (r *HistoryRepository) CountByStatus(status string) (int64, error) {
	var count int64
	err := r.GetDB().Model(&models.History{}).Where("status = ?", status).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("history by status %s", status), err)
	}
	return count, nil
}

func (r *HistoryRepository) CountByOperation(operation string) (int64, error) {
	var count int64
	err := r.GetDB().Model(&models.History{}).Where("operation = ?", operation).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("history by operation %s", operation), err)
	}
	return count, nil
}

func (r *HistoryRepository) Delete(id uint) error {
	return r.BaseRepository.Delete(id)
}

func (r *HistoryRepository) DeleteByMovieID(movieID string) error {
	if err := r.GetDB().Where("movie_id = ?", movieID).Delete(&models.History{}).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("history for movie %s", movieID), err)
	}
	return nil
}

func (r *HistoryRepository) DeleteOlderThan(date time.Time) error {
	if err := r.GetDB().Where("datetime(created_at) < datetime(?)", date.Format(SqliteTimeFormat)).Delete(&models.History{}).Error; err != nil {
		return wrapDBErr("delete", "history older than date", err)
	}
	return nil
}

func (r *HistoryRepository) List(limit, offset int) ([]models.History, error) {
	return r.BaseRepository.List(limit, offset)
}

func (r *HistoryRepository) FindByBatchJobID(batchJobID string) ([]models.History, error) {
	var history []models.History
	err := r.GetDB().Where("batch_job_id = ?", batchJobID).Order("created_at ASC").Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history for batch job %s", batchJobID), err)
	}
	return history, nil
}
