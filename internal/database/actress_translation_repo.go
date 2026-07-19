package database

import (
	"context"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ActressTranslationRepository persists localized name translations for actresses.
type ActressTranslationRepository struct {
	db *DB
}

func newActressTranslationRepository(db *DB) *ActressTranslationRepository {
	return &ActressTranslationRepository{db: db}
}

// NewActressTranslationRepository exposes the translation repository to the
// actress-sync workflow while the regular repository bag continues to use the
// narrow interface.
func NewActressTranslationRepository(db *DB) *ActressTranslationRepository {
	return newActressTranslationRepository(db)
}

func actressTranslationEntityID(actressID uint, language string) string {
	return fmt.Sprintf("actress translation %d/%s", actressID, language)
}

// Upsert inserts the actress translation or updates the existing record matched by actress and language.
func (r *ActressTranslationRepository) Upsert(ctx context.Context, translation *models.ActressTranslation) error {
	if translation == nil {
		return wrapDBErr("upsert", "nil actress translation", ErrInvalidLookup)
	}
	incoming := *translation
	var persisted models.ActressTranslation
	err := retryOnLocked(func() error {
		candidate := incoming
		if upsertErr := r.UpsertTx(r.db.WithContext(ctx), &candidate); upsertErr != nil {
			return upsertErr
		}
		persisted = candidate
		return nil
	})
	if err != nil {
		return err
	}
	*translation = persisted
	return nil
}

// UpsertTx inserts or updates the actress translation within the provided transaction.
func (r *ActressTranslationRepository) UpsertTx(tx *gorm.DB, translation *models.ActressTranslation) error {
	incoming := *translation
	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "actress_id"}, {Name: "language"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"first_name", "last_name", "japanese_name", "display_name",
			"source_name", "settings_hash", "updated_at",
		}),
	}).Create(&incoming).Error; err != nil {
		if !isDuplicateKey(err) {
			return wrapDBErr("upsert", actressTranslationEntityID(translation.ActressID, translation.Language), err)
		}
		updates := map[string]any{
			"first_name": incoming.FirstName, "last_name": incoming.LastName,
			"japanese_name": incoming.JapaneseName, "display_name": incoming.DisplayName,
			"source_name": incoming.SourceName, "settings_hash": incoming.SettingsHash,
			"updated_at": time.Now(),
		}
		if updateErr := tx.Model(&models.ActressTranslation{}).
			Where("actress_id = ? AND language = ?", incoming.ActressID, incoming.Language).
			Updates(updates).Error; updateErr != nil {
			return wrapDBErr("update", actressTranslationEntityID(translation.ActressID, translation.Language), updateErr)
		}
	}
	if err := tx.First(translation, "actress_id = ? AND language = ?", incoming.ActressID, incoming.Language).Error; err != nil {
		return wrapDBErr("reload", actressTranslationEntityID(incoming.ActressID, incoming.Language), err)
	}
	return nil
}

// FindByActressAndLanguage returns the translation for the given actress in the given language.
func (r *ActressTranslationRepository) FindByActressAndLanguage(ctx context.Context, actressID uint, language string) (*models.ActressTranslation, error) {
	var translation models.ActressTranslation
	err := r.db.WithContext(ctx).First(&translation, "actress_id = ? AND language = ?", actressID, language).Error
	if err != nil {
		return nil, wrapDBErr("find", actressTranslationEntityID(actressID, language), err)
	}
	return &translation, nil
}

// FindAllByActress returns every translation stored for the given actress.
func (r *ActressTranslationRepository) FindAllByActress(ctx context.Context, actressID uint) ([]models.ActressTranslation, error) {
	var translations []models.ActressTranslation
	err := r.db.WithContext(ctx).Where("actress_id = ?", actressID).Find(&translations).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress translations for actress %d", actressID), err)
	}
	return translations, nil
}

// FindByActressIDsAndLanguage returns translations grouped by actress ID for the given actress IDs and language.
func (r *ActressTranslationRepository) FindByActressIDsAndLanguage(ctx context.Context, actressIDs []uint, language string) (map[uint][]models.ActressTranslation, error) {
	if len(actressIDs) == 0 {
		return make(map[uint][]models.ActressTranslation), nil
	}
	var translations []models.ActressTranslation
	if err := r.db.WithContext(ctx).Where("actress_id IN ? AND language = ?", actressIDs, language).Find(&translations).Error; err != nil {
		return nil, wrapDBErr("find", "actress translations batch", err)
	}
	result := make(map[uint][]models.ActressTranslation, len(actressIDs))
	for _, t := range translations {
		result[t.ActressID] = append(result[t.ActressID], t)
	}
	return result, nil
}

// Delete removes the translation for the given actress in the given language.
func (r *ActressTranslationRepository) Delete(ctx context.Context, actressID uint, language string) error {
	if err := r.db.WithContext(ctx).Delete(&models.ActressTranslation{}, "actress_id = ? AND language = ?", actressID, language).Error; err != nil {
		return wrapDBErr("delete", actressTranslationEntityID(actressID, language), err)
	}
	return nil
}
