package database

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm/clause"
)

type ActressTranslationRepository struct {
	db *DB
}

func NewActressTranslationRepository(db *DB) *ActressTranslationRepository {
	return &ActressTranslationRepository{db: db}
}

func (r *ActressTranslationRepository) Upsert(translation *models.ActressTranslation) error {
	if translation == nil {
		return ErrInvalidLookup
	}
	saveErr := retryOnLocked(func() error {
		return r.db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "actress_id"}, {Name: "language"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"name", "source_name", "settings_hash", "updated_at",
			}),
		}).Create(translation).Error
	})
	if saveErr != nil {
		return wrapDBErr("upsert", fmt.Sprintf("actress translation %d/%s", translation.ActressID, translation.Language), saveErr)
	}
	return nil
}

func (r *ActressTranslationRepository) FindByActressIDsAndLanguage(ids []uint, language string) (map[uint]models.ActressTranslation, error) {
	result := make(map[uint]models.ActressTranslation)
	if len(ids) == 0 {
		return result, nil
	}
	var rows []models.ActressTranslation
	if err := r.db.Where("actress_id IN ? AND language = ?", ids, language).Find(&rows).Error; err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress translations for %s", language), err)
	}
	for _, row := range rows {
		result[row.ActressID] = row
	}
	return result, nil
}
