package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// GenreTranslationRepository persists and queries genre translation records.
type GenreTranslationRepository struct {
	db *DB
}

func newGenreTranslationRepository(db *DB) *GenreTranslationRepository {
	return &GenreTranslationRepository{db: db}
}

func genreTranslationEntityID(genreID uint, language string) string {
	return fmt.Sprintf("genre translation %d/%s", genreID, language)
}

// Upsert inserts or updates the given genre translation using the repository's default context.
func (r *GenreTranslationRepository) Upsert(ctx context.Context, translation *models.GenreTranslation) error {
	return r.UpsertTx(r.db.WithContext(ctx), translation)
}

// UpsertTx inserts or updates the given genre translation within the provided transaction, tolerating duplicate-key races.
func (r *GenreTranslationRepository) UpsertTx(tx *gorm.DB, translation *models.GenreTranslation) error {
	var existing models.GenreTranslation
	err := tx.First(&existing, "genre_id = ? AND language = ?", translation.GenreID, translation.Language).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return wrapDBErr("find", genreTranslationEntityID(translation.GenreID, translation.Language), err)
		}
		if err := tx.Create(translation).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				loadErr := tx.First(&existing, "genre_id = ? AND language = ?", translation.GenreID, translation.Language).Error
				if loadErr == nil {
					translation.ID = existing.ID
					translation.CreatedAt = existing.CreatedAt
					if saveErr := tx.Save(translation).Error; saveErr != nil {
						return wrapDBErr("update", genreTranslationEntityID(translation.GenreID, translation.Language), saveErr)
					}
					return nil
				}
				return fmt.Errorf("create duplicate key, then reload also failed: create=%w, reload=%w", err, loadErr)
			}
			return wrapDBErr("create", genreTranslationEntityID(translation.GenreID, translation.Language), err)
		}
		return nil
	}
	translation.ID = existing.ID
	translation.CreatedAt = existing.CreatedAt
	if err := tx.Save(translation).Error; err != nil {
		return wrapDBErr("update", genreTranslationEntityID(translation.GenreID, translation.Language), err)
	}
	return nil
}

// FindByGenreAndLanguage returns the translation for the given genre and language.
func (r *GenreTranslationRepository) FindByGenreAndLanguage(ctx context.Context, genreID uint, language string) (*models.GenreTranslation, error) {
	var translation models.GenreTranslation
	err := r.db.WithContext(ctx).First(&translation, "genre_id = ? AND language = ?", genreID, language).Error
	if err != nil {
		return nil, wrapDBErr("find", genreTranslationEntityID(genreID, language), err)
	}
	return &translation, nil
}

// FindAllByGenre returns all translations stored for the given genre.
func (r *GenreTranslationRepository) FindAllByGenre(ctx context.Context, genreID uint) ([]models.GenreTranslation, error) {
	var translations []models.GenreTranslation
	err := r.db.WithContext(ctx).Where("genre_id = ?", genreID).Find(&translations).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("genre translations for genre %d", genreID), err)
	}
	return translations, nil
}

// FindByGenreIDsAndLanguage returns translations for the given genre IDs in the specified language, grouped by genre ID.
func (r *GenreTranslationRepository) FindByGenreIDsAndLanguage(ctx context.Context, genreIDs []uint, language string) (map[uint][]models.GenreTranslation, error) {
	if len(genreIDs) == 0 {
		return make(map[uint][]models.GenreTranslation), nil
	}
	var translations []models.GenreTranslation
	if err := r.db.WithContext(ctx).Where("genre_id IN ? AND language = ?", genreIDs, language).Find(&translations).Error; err != nil {
		return nil, wrapDBErr("find", "genre translations batch", err)
	}
	result := make(map[uint][]models.GenreTranslation, len(genreIDs))
	for _, t := range translations {
		result[t.GenreID] = append(result[t.GenreID], t)
	}
	return result, nil
}

// Delete removes the translation for the given genre and language.
func (r *GenreTranslationRepository) Delete(ctx context.Context, genreID uint, language string) error {
	if err := r.db.WithContext(ctx).Delete(&models.GenreTranslation{}, "genre_id = ? AND language = ?", genreID, language).Error; err != nil {
		return wrapDBErr("delete", genreTranslationEntityID(genreID, language), err)
	}
	return nil
}
