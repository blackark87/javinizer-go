package database

import (
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

type HistoryRepository struct {
	*BaseRepository[models.History, uint]
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

// HistoryStatsAggregate contains grouped counts used by dashboard and history stats APIs.
type HistoryStatsAggregate struct {
	Total          int64
	ByStatus       map[string]int64
	ByOperation    map[string]int64
	RecentByStatus map[string]int64
}

type groupedCount struct {
	Key   string `gorm:"column:key"`
	Count int64  `gorm:"column:count"`
}

// StatsAggregate returns total history count, grouped status counts, grouped
// operation counts, and grouped status counts for records created since
// recentSince. The grouped queries avoid repeated per-status and per-operation
// count calls during dashboard initialization.
func (r *HistoryRepository) StatsAggregate(recentSince time.Time) (*HistoryStatsAggregate, error) {
	stats := &HistoryStatsAggregate{
		ByStatus:       make(map[string]int64),
		ByOperation:    make(map[string]int64),
		RecentByStatus: make(map[string]int64),
	}

	if err := r.GetDB().Model(&models.History{}).Count(&stats.Total).Error; err != nil {
		return nil, wrapDBErr("count", "history", err)
	}

	var statusCounts []groupedCount
	if err := r.GetDB().Model(&models.History{}).
		Select("status AS key, COUNT(*) AS count").
		Group("status").
		Scan(&statusCounts).Error; err != nil {
		return nil, wrapDBErr("count", "history by status", err)
	}
	for _, row := range statusCounts {
		stats.ByStatus[row.Key] = row.Count
	}

	var operationCounts []groupedCount
	if err := r.GetDB().Model(&models.History{}).
		Select("operation AS key, COUNT(*) AS count").
		Group("operation").
		Scan(&operationCounts).Error; err != nil {
		return nil, wrapDBErr("count", "history by operation", err)
	}
	for _, row := range operationCounts {
		stats.ByOperation[row.Key] = row.Count
	}

	var recentStatusCounts []groupedCount
	if err := r.GetDB().Model(&models.History{}).
		Select("status AS key, COUNT(*) AS count").
		Where("datetime(created_at) >= datetime(?)", recentSince.UTC().Format(SqliteTimeFormat)).
		Group("status").
		Scan(&recentStatusCounts).Error; err != nil {
		return nil, wrapDBErr("count", "recent history by status", err)
	}
	for _, row := range recentStatusCounts {
		stats.RecentByStatus[row.Key] = row.Count
	}

	return stats, nil
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
