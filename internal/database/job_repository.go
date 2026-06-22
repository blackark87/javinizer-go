package database

import (
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

type JobRepository struct {
	*BaseRepository[models.Job, string]
}

func NewJobRepository(db *DB) *JobRepository {
	return &JobRepository{
		BaseRepository: NewBaseRepository[models.Job, string](
			db, "job",
			func(j models.Job) string { return j.ID },
			WithDefaultOrder[models.Job, string]("started_at DESC, id DESC"),
			WithNewEntity[models.Job, string](func() models.Job { return models.Job{} }),
		),
	}
}

func (r *JobRepository) Create(job *models.Job) error {
	return r.BaseRepository.Create(job)
}

func (r *JobRepository) Update(job *models.Job) error {
	if err := r.GetDB().Save(job).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("job %s", job.ID), err)
	}
	return nil
}

func (r *JobRepository) Upsert(job *models.Job) error {
	if err := r.GetDB().Save(job).Error; err != nil {
		return wrapDBErr("upsert", fmt.Sprintf("job %s", job.ID), err)
	}
	return nil
}

func (r *JobRepository) FindByID(id string) (*models.Job, error) {
	return r.BaseRepository.FindByID(id)
}

func (r *JobRepository) List() ([]models.Job, error) {
	return r.ListAll()
}

func (r *JobRepository) Delete(id string) error {
	return r.BaseRepository.Delete(id)
}

func (r *JobRepository) DeleteOrganizedOlderThan(date time.Time) error {
	if err := r.GetDB().Where("status = ? AND organized_at < ?", "organized", date).Delete(&models.Job{}).Error; err != nil {
		return wrapDBErr("delete", "organized jobs", err)
	}
	return nil
}
