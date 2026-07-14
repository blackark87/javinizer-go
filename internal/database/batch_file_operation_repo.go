package database

import (
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

type BatchFileOperationRepository struct {
	*BaseRepository[models.BatchFileOperation, uint]
}

func NewBatchFileOperationRepository(db *DB) *BatchFileOperationRepository {
	return &BatchFileOperationRepository{
		BaseRepository: NewBaseRepository[models.BatchFileOperation, uint](
			db, "batch file operation",
			func(op models.BatchFileOperation) string { return fmt.Sprintf("%d", op.ID) },
			WithNewEntity[models.BatchFileOperation, uint](func() models.BatchFileOperation { return models.BatchFileOperation{} }),
		),
	}
}

func (r *BatchFileOperationRepository) Create(op *models.BatchFileOperation) error {
	return r.BaseRepository.Create(op)
}

func (r *BatchFileOperationRepository) CreateBatch(ops []*models.BatchFileOperation) error {
	return r.GetDB().Transaction(func(tx *gorm.DB) error {
		for _, op := range ops {
			if err := tx.Create(op).Error; err != nil {
				return wrapDBErr("create", fmt.Sprintf("batch file operation %d", op.ID), err)
			}
		}
		return nil
	})
}

func (r *BatchFileOperationRepository) FindByID(id uint) (*models.BatchFileOperation, error) {
	return r.BaseRepository.FindByID(id)
}

func (r *BatchFileOperationRepository) FindByBatchJobID(batchJobID string) ([]models.BatchFileOperation, error) {
	var ops []models.BatchFileOperation
	err := r.GetDB().Where("batch_job_id = ?", batchJobID).Order("id ASC").Find(&ops).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("batch file operations for job %s", batchJobID), err)
	}
	return ops, nil
}

func (r *BatchFileOperationRepository) FindLatestAppliedByMovieID(movieID string) (*models.BatchFileOperation, error) {
	var op models.BatchFileOperation
	err := r.GetDB().Where("movie_id = ? AND revert_status = ? AND TRIM(COALESCE(nfo_path, '')) <> ''", movieID, models.RevertStatusApplied).
		Order("created_at DESC, id DESC").First(&op).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("latest NFO operation for movie %s", movieID), err)
	}
	return &op, nil
}

func (r *BatchFileOperationRepository) FindByBatchJobIDAndRevertStatus(batchJobID string, revertStatus string) ([]models.BatchFileOperation, error) {
	var ops []models.BatchFileOperation
	err := r.GetDB().Where("batch_job_id = ? AND revert_status = ?", batchJobID, revertStatus).Order("id ASC").Find(&ops).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("batch file operations for job %s with status %s", batchJobID, revertStatus), err)
	}
	return ops, nil
}

func (r *BatchFileOperationRepository) UpdateRevertStatus(id uint, status string) error {
	updates := map[string]interface{}{
		"revert_status": status,
		"updated_at":    time.Now().UTC(),
	}
	if status == models.RevertStatusReverted {
		updates["reverted_at"] = time.Now().UTC()
	}
	if err := r.GetDB().Model(&models.BatchFileOperation{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("batch file operation %d revert status", id), err)
	}
	return nil
}

func (r *BatchFileOperationRepository) GetOrganizedOriginalPaths(batchJobID string) (map[string]bool, error) {
	var ops []models.BatchFileOperation
	err := r.GetDB().Select("original_path").Where("batch_job_id = ? AND new_path != ''", batchJobID).Find(&ops).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("organized paths for job %s", batchJobID), err)
	}
	result := make(map[string]bool, len(ops))
	for _, op := range ops {
		result[op.OriginalPath] = true
	}
	return result, nil
}

func (r *BatchFileOperationRepository) CountByBatchJobID(batchJobID string) (int64, error) {
	var count int64
	err := r.GetDB().Model(&models.BatchFileOperation{}).Where("batch_job_id = ?", batchJobID).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("batch file operations for job %s", batchJobID), err)
	}
	return count, nil
}

func (r *BatchFileOperationRepository) CountByBatchJobIDAndRevertStatus(batchJobID string, status string) (int64, error) {
	var count int64
	err := r.GetDB().Model(&models.BatchFileOperation{}).Where("batch_job_id = ? AND revert_status = ?", batchJobID, status).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("batch file operations for job %s with status %s", batchJobID, status), err)
	}
	return count, nil
}

func (r *BatchFileOperationRepository) Update(op *models.BatchFileOperation) error {
	if err := r.GetDB().Save(op).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("batch file operation %d", op.ID), err)
	}
	return nil
}
