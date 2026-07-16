package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// MovieRepository persists movies and their associated records using a GORM
// base repository plus a dedicated upserter for insert-or-update semantics.
type MovieRepository struct {
	*BaseRepository[models.Movie, string]
	upserter *MovieUpserter
}

// NewMovieRepository returns a MovieRepository backed by the given database.
func NewMovieRepository(db *DB) *MovieRepository {
	repo := &MovieRepository{
		BaseRepository: NewBaseRepository[models.Movie, string](
			db, "movie",
			func(m models.Movie) string { return movieEntityID(&m) },
			WithNewEntity[models.Movie, string](func() models.Movie { return models.Movie{} }),
		),
	}
	repo.upserter = NewMovieUpserter(repo)
	return repo
}

func movieEntityID(movie *models.Movie) string {
	if movie.ContentID != "" {
		return movie.ContentID
	}
	return movie.ID
}

// Create inserts a new movie into the database.
func (r *MovieRepository) Create(ctx context.Context, movie *models.Movie) error {
	return r.BaseRepository.Create(ctx, movie)
}

// Update saves all fields of an existing movie.
func (r *MovieRepository) Update(ctx context.Context, movie *models.Movie) error {
	if err := r.GetDB().WithContext(ctx).Save(movie).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("movie %s", movieEntityID(movie)), err)
	}
	return nil
}

// Upsert inserts or updates a movie, returning the persisted record.
func (r *MovieRepository) Upsert(ctx context.Context, movie *models.Movie) (*models.Movie, error) {
	return r.upserter.Upsert(ctx, movie)
}

// UpsertWithTranslations upserts a movie together with its genre and actress translations.
func (r *MovieRepository) UpsertWithTranslations(ctx context.Context, movie *models.Movie, genreTranslations []models.GenreTranslationData, actressTranslations []models.ActressTranslationData) (*models.Movie, error) {
	return r.upserter.UpsertWithTranslations(ctx, movie, genreTranslations, actressTranslations)
}

// FindByID loads a movie by its primary id, preloading actresses, genres, and translations.
func (r *MovieRepository) FindByID(ctx context.Context, id string) (*models.Movie, error) {
	var movie models.Movie
	err := r.GetDB().WithContext(ctx).Preload("Actresses").Preload("Genres").Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).First(&movie, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find movie by id %s: %w", id, ErrNotFound)
		}
		return nil, wrapDBErr("find", fmt.Sprintf("movie by id %s", id), err)
	}
	return &movie, nil
}

// FindByContentID loads a movie by its content_id, preloading actresses, genres, and translations.
func (r *MovieRepository) FindByContentID(ctx context.Context, contentID string) (*models.Movie, error) {
	var movie models.Movie
	err := r.GetDB().WithContext(ctx).Preload("Actresses").Preload("Genres").Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).First(&movie, "content_id = ?", contentID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find movie %s: %w", contentID, ErrNotFound)
		}
		return nil, wrapDBErr("find", fmt.Sprintf("movie %s", contentID), err)
	}
	return &movie, nil
}

// Delete removes a movie and its associated actresses, genres, translations, and tags.
func (r *MovieRepository) Delete(ctx context.Context, id string) error {
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var movie models.Movie
		if err := tx.Model(&models.Movie{}).
			Select("content_id").
			Where("id = ?", id).
			First(&movie).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return wrapDBErr("find", fmt.Sprintf("movie for delete %s", id), err)
		}

		if movie.ContentID == "" {
			return nil
		}

		stub := &models.Movie{ContentID: movie.ContentID}
		if err := tx.Model(stub).Association("Actresses").Clear(); err != nil {
			return wrapDBErr("clear", fmt.Sprintf("actresses for movie %s", movie.ContentID), err)
		}
		if err := tx.Model(stub).Association("Genres").Clear(); err != nil {
			return wrapDBErr("clear", fmt.Sprintf("genres for movie %s", movie.ContentID), err)
		}

		if err := tx.Delete(&models.MovieTranslation{}, "movie_id = ?", movie.ContentID).Error; err != nil {
			return wrapDBErr("delete", fmt.Sprintf("translations for movie %s", movie.ContentID), err)
		}

		if err := tx.Delete(&models.MovieTag{}, "movie_id = ?", movie.ContentID).Error; err != nil {
			return wrapDBErr("delete", fmt.Sprintf("tags for movie %s", movie.ContentID), err)
		}

		if err := tx.Delete(&models.Movie{}, "content_id = ?", movie.ContentID).Error; err != nil {
			return wrapDBErr("delete", fmt.Sprintf("movie %s", movie.ContentID), err)
		}
		return nil
	})
}

// List returns a page of movies with actresses and genres preloaded.
func (r *MovieRepository) List(ctx context.Context, limit, offset int) ([]models.Movie, error) {
	var movies []models.Movie
	err := r.GetDB().WithContext(ctx).Preload("Actresses").Preload("Genres").Limit(limit).Offset(offset).Find(&movies).Error
	if err != nil {
		return nil, wrapDBErr("find", "movies", err)
	}
	return movies, nil
}

// ListByActressID returns movies linked to an actress with the associations
// needed by actress-sync refreshes preloaded.
func (r *MovieRepository) ListByActressID(ctx context.Context, actressID uint, limit, offset int) ([]models.Movie, error) {
	var movies []models.Movie
	query := r.GetDB().WithContext(ctx).
		Preload("Actresses").
		Preload("Genres").
		Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).
		Joins("JOIN movie_actresses ON movie_actresses.movie_content_id = movies.content_id").
		Where("movie_actresses.actress_id = ?", actressID).
		Order("movies.release_date DESC, movies.content_id ASC")
	if limit > 0 {
		query = query.Limit(limit).Offset(offset)
	}
	if err := query.Find(&movies).Error; err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("movies for actress %d", actressID), err)
	}
	return movies, nil
}

// CountByActressID returns the number of movies linked to an actress.
func (r *MovieRepository) CountByActressID(ctx context.Context, actressID uint) (int64, error) {
	var count int64
	err := r.GetDB().WithContext(ctx).Model(&models.Movie{}).
		Joins("JOIN movie_actresses ON movie_actresses.movie_content_id = movies.content_id").
		Where("movie_actresses.actress_id = ?", actressID).
		Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("movies for actress %d", actressID), err)
	}
	return count, nil
}

// ReplaceUnverifiedActressesForMovie atomically removes DMM-ID-less cast
// mappings, preserves verified mappings, and adds resolver-verified actresses.
// It returns removed actress IDs so callers can clean up true orphans.
func (r *MovieRepository) ReplaceUnverifiedActressesForMovie(ctx context.Context, movieContentID string, replacements []models.Actress) ([]uint, error) {
	var removedIDs []uint
	err := retryOnLocked(func() error {
		return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Table("actresses AS actress").
				Select("actress.id").
				Joins("JOIN movie_actresses AS mapping ON mapping.actress_id = actress.id").
				Where("mapping.movie_content_id = ? AND actress.dmm_id <= 0", movieContentID).
				Order("actress.id ASC").Pluck("actress.id", &removedIDs).Error; err != nil {
				return err
			}
			if len(removedIDs) > 0 {
				if err := tx.Exec("DELETE FROM movie_actresses WHERE movie_content_id = ? AND actress_id IN ?", movieContentID, removedIDs).Error; err != nil {
					return err
				}
			}
			for _, actress := range replacements {
				if actress.ID == 0 {
					return ErrInvalidLookup
				}
				if err := tx.Exec("INSERT OR IGNORE INTO movie_actresses(movie_content_id, actress_id) VALUES (?, ?)", movieContentID, actress.ID).Error; err != nil {
					return err
				}
			}
			return nil
		})
	})
	if err != nil {
		return nil, wrapDBErr("replace", fmt.Sprintf("unverified actresses for movie %s", movieContentID), err)
	}
	return removedIDs, nil
}
