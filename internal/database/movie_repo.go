package database

import (
	"errors"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

type MovieRepository struct {
	*BaseRepository[models.Movie, string]
}

func NewMovieRepository(db *DB) *MovieRepository {
	return &MovieRepository{
		BaseRepository: NewBaseRepository[models.Movie, string](
			db, "movie",
			func(m models.Movie) string { return movieEntityID(&m) },
			WithNewEntity[models.Movie, string](func() models.Movie { return models.Movie{} }),
		),
	}
}

func movieEntityID(movie *models.Movie) string {
	if movie.ContentID != "" {
		return movie.ContentID
	}
	return movie.ID
}

func (r *MovieRepository) Create(movie *models.Movie) error {
	return r.BaseRepository.Create(movie)
}

func (r *MovieRepository) Update(movie *models.Movie) error {
	if err := r.GetDB().Save(movie).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("movie %s", movieEntityID(movie)), err)
	}
	return nil
}

func (r *MovieRepository) Upsert(movie *models.Movie) (*models.Movie, error) {
	var result *models.Movie
	movie.Actresses = filterIdentifiableActresses(movie.Actresses)
	savedTranslations := make([]models.MovieTranslation, len(movie.Translations))
	copy(savedTranslations, movie.Translations)
	savedActresses := make([]models.Actress, len(movie.Actresses))
	copy(savedActresses, movie.Actresses)
	savedGenres := make([]models.Genre, len(movie.Genres))
	copy(savedGenres, movie.Genres)
	savedContentID := movie.ContentID
	savedCreatedAt := movie.CreatedAt
	err := retryOnLocked(func() error {
		movie.Translations = make([]models.MovieTranslation, len(savedTranslations))
		copy(movie.Translations, savedTranslations)
		movie.Actresses = make([]models.Actress, len(savedActresses))
		copy(movie.Actresses, savedActresses)
		movie.Genres = make([]models.Genre, len(savedGenres))
		copy(movie.Genres, savedGenres)
		movie.ContentID = savedContentID
		movie.CreatedAt = savedCreatedAt
		return r.GetDB().Transaction(func(tx *gorm.DB) error {
			if strings.TrimSpace(movie.ContentID) == "" {
				if strings.TrimSpace(movie.ID) == "" {
					return fmt.Errorf("content_id is required when using ContentID as primary key")
				}
				movie.ContentID = strings.ToLower(strings.ReplaceAll(movie.ID, "-", ""))
			}

			var existing models.Movie
			var existingFound bool
			if movie.ContentID != "" {
				err := tx.Select("content_id", "created_at").First(&existing, "content_id = ?", movie.ContentID).Error
				if err == nil {
					existingFound = true
				} else if !errors.Is(err, gorm.ErrRecordNotFound) {
					return wrapDBErr("find", fmt.Sprintf("movie %s", movie.ContentID), err)
				}
			}
			if !existingFound && movie.ID != "" {
				err := tx.Select("content_id", "created_at").First(&existing, "id = ?", movie.ID).Error
				if err == nil {
					existingFound = true
					movie.ContentID = existing.ContentID
				} else if !errors.Is(err, gorm.ErrRecordNotFound) {
					return wrapDBErr("find", fmt.Sprintf("movie %s", movie.ID), err)
				}
			}

			if !existingFound {
				if err := tx.Omit("Actresses", "Genres", "Translations").Create(movie).Error; err != nil {
					if errors.Is(err, gorm.ErrDuplicatedKey) {
						var existingMovie models.Movie
						loadErr := tx.Select("created_at").First(&existingMovie, "content_id = ?", movie.ContentID).Error
						if loadErr != nil {
							if !errors.Is(loadErr, gorm.ErrRecordNotFound) {
								return wrapDBErr("find duplicate", fmt.Sprintf("movie %s", movie.ContentID), loadErr)
							}
						} else {
							movie.CreatedAt = existingMovie.CreatedAt
						}
						if err := r.saveMovieWithAssociations(tx, movie); err != nil {
							return wrapDBErr("save duplicate", fmt.Sprintf("movie %s", movie.ContentID), err)
						}
						var loaded models.Movie
						if err := tx.Preload("Actresses").Preload("Genres").Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).First(&loaded, "content_id = ?", movie.ContentID).Error; err != nil {
							return wrapDBErr("reload", fmt.Sprintf("movie %s", movie.ContentID), err)
						}
						result = &loaded
						return nil
					}
					return wrapDBErr("create", fmt.Sprintf("movie %s", movie.ContentID), err)
				}
			} else {
				movie.CreatedAt = existing.CreatedAt
			}

			if err := r.ensureGenresExistTx(tx, movie.Genres); err != nil {
				return wrapDBErr("ensure genres", fmt.Sprintf("for movie %s", movie.ContentID), err)
			}
			if err := r.ensureActressesExistTx(tx, movie.Actresses); err != nil {
				return wrapDBErr("ensure actresses", fmt.Sprintf("for movie %s", movie.ContentID), err)
			}

			translations := movie.Translations
			movie.Translations = nil
			if err := upsertMovieCore(tx, r.GetDB(), movie, translations); err != nil {
				return wrapDBErr("save", fmt.Sprintf("movie %s", movie.ContentID), err)
			}

			var loaded models.Movie
			if err := tx.Preload("Actresses").Preload("Genres").Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).First(&loaded, "content_id = ?", movie.ContentID).Error; err != nil {
				return wrapDBErr("reload", fmt.Sprintf("movie %s", movie.ContentID), err)
			}
			result = &loaded
			return nil
		})
	})
	return result, err
}

func (r *MovieRepository) saveMovieWithAssociations(tx *gorm.DB, movie *models.Movie) error {
	if err := r.ensureGenresExistTx(tx, movie.Genres); err != nil {
		return fmt.Errorf("save associations for movie %s: ensure genres: %w", movie.ContentID, err)
	}
	if err := r.ensureActressesExistTx(tx, movie.Actresses); err != nil {
		return fmt.Errorf("save associations for movie %s: ensure actresses: %w", movie.ContentID, err)
	}

	translations := movie.Translations
	movie.Translations = nil
	if err := upsertMovieCore(tx, r.GetDB(), movie, translations); err != nil {
		return fmt.Errorf("save associations for movie %s: upsert core: %w", movie.ContentID, err)
	}
	return nil
}

func (r *MovieRepository) ensureGenresExistTx(tx *gorm.DB, genres []models.Genre) error {
	if len(genres) == 0 {
		return nil
	}

	names := make([]string, len(genres))
	for i, g := range genres {
		names[i] = g.Name
	}

	var existingGenres []models.Genre
	if err := tx.Where("name IN ?", names).Find(&existingGenres).Error; err != nil {
		return err
	}

	existingByName := make(map[string]models.Genre, len(existingGenres))
	for _, g := range existingGenres {
		existingByName[g.Name] = g
	}

	for i := range genres {
		if found, ok := existingByName[genres[i].Name]; ok {
			genres[i] = found
			continue
		}

		if err := raceRetryCreate(tx, &genres[i], func(tx *gorm.DB) error {
			var found models.Genre
			if err := tx.Where("name = ?", genres[i].Name).First(&found).Error; err != nil {
				return err
			}
			genres[i] = found
			return nil
		}); err != nil {
			return err
		}
	}

	return nil
}

func (r *MovieRepository) mergeActressData(existing *models.Actress, new models.Actress) bool {
	needsUpdate := false

	if new.ThumbURL != "" && existing.ThumbURL == "" {
		existing.ThumbURL = new.ThumbURL
		needsUpdate = true
	}

	if new.FirstName != "" && existing.FirstName == "" {
		existing.FirstName = new.FirstName
		needsUpdate = true
	}
	if new.LastName != "" && existing.LastName == "" {
		existing.LastName = new.LastName
		needsUpdate = true
	}

	// A translated (Hangul) name upgrades an existing untranslated row: rows
	// written before translation ran (or while it failed) keep romaji forever
	// otherwise, since the fill-empty rules above never overwrite. The reverse
	// direction never applies — a failed translation must not downgrade Hangul.
	newHasHangul := containsHangul(new.FirstName) || containsHangul(new.LastName)
	existingHasHangul := containsHangul(existing.FirstName) || containsHangul(existing.LastName)
	if newHasHangul && !existingHasHangul {
		if existing.FirstName != new.FirstName || existing.LastName != new.LastName {
			existing.FirstName = new.FirstName
			existing.LastName = new.LastName
			needsUpdate = true
		}
	}

	return needsUpdate
}

// containsHangul reports whether s contains at least one Hangul syllable.
func containsHangul(s string) bool {
	for _, r := range s {
		if r >= 0xAC00 && r <= 0xD7A3 {
			return true
		}
	}
	return false
}

func (r *MovieRepository) ensureActressesExistTx(tx *gorm.DB, actresses []models.Actress) error {
	if len(actresses) == 0 {
		return nil
	}

	type actressGroup struct {
		index int
		act   *models.Actress
	}

	var dmmGroup []actressGroup
	var jpGroup []actressGroup
	var nameGroup []actressGroup

	for i := range actresses {
		a := &actresses[i]
		if a.DMMID > 0 {
			dmmGroup = append(dmmGroup, actressGroup{index: i, act: a})
		} else if a.JapaneseName != "" {
			jpGroup = append(jpGroup, actressGroup{index: i, act: a})
		} else if a.FirstName != "" || a.LastName != "" {
			nameGroup = append(nameGroup, actressGroup{index: i, act: a})
		}
	}

	if len(dmmGroup) > 0 {
		actressRepo := NewActressRepository(r.GetDB())
		for _, g := range dmmGroup {
			resolution, err := actressRepo.resolveVerifiedIdentityTx(tx, 0, *g.act, true)
			if err != nil {
				return err
			}
			actresses[g.index] = resolution.Actress
		}
	}

	if len(jpGroup) > 0 {
		var found []models.Actress
		if err := tx.Where("dmm_id <= 0").Order("id ASC").Find(&found).Error; err != nil {
			return err
		}
		byJPName := make(map[string]models.Actress, len(found))
		for _, a := range found {
			key := normalizeExactActressName(a.JapaneseName)
			if key != "" {
				if _, exists := byJPName[key]; !exists {
					byJPName[key] = a
				}
			}
		}
		for _, g := range jpGroup {
			key := normalizeExactActressName(g.act.JapaneseName)
			if existing, ok := byJPName[key]; ok {
				if r.mergeActressData(&existing, *g.act) {
					if err := tx.Save(&existing).Error; err != nil {
						return err
					}
				}
				actresses[g.index] = existing
			} else {
				if err := raceRetryCreate(tx, g.act, func(tx *gorm.DB) error {
					owners, err := findUnverifiedJapaneseNameOwnersTx(tx, g.act.JapaneseName)
					if err != nil {
						return err
					}
					if len(owners) == 0 {
						return gorm.ErrRecordNotFound
					}
					found := owners[0]
					if r.mergeActressData(&found, *g.act) {
						if err := tx.Save(&found).Error; err != nil {
							return err
						}
					}
					actresses[g.index] = found
					return nil
				}); err != nil {
					return err
				}
				byJPName[key] = *g.act
			}
		}
	}

	for _, g := range nameGroup {
		a := g.act
		existing, err := findUnverifiedPrimaryNameTx(tx, a.FirstName, a.LastName)
		if err != nil {
			return err
		}
		if existing != nil {
			if r.mergeActressData(existing, *a) {
				if err := tx.Save(existing).Error; err != nil {
					return err
				}
			}
			actresses[g.index] = *existing
		} else {
			if err := raceRetryCreate(tx, a, func(tx *gorm.DB) error {
				found, findErr := findUnverifiedPrimaryNameTx(tx, a.FirstName, a.LastName)
				if findErr != nil {
					return findErr
				}
				if found == nil {
					return gorm.ErrRecordNotFound
				}
				if r.mergeActressData(found, *a) {
					if saveErr := tx.Save(found).Error; saveErr != nil {
						return saveErr
					}
				}
				actresses[g.index] = *found
				return nil
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

func findUnverifiedPrimaryNameTx(tx *gorm.DB, firstName, lastName string) (*models.Actress, error) {
	firstKey := normalizeExactActressName(firstName)
	lastKey := normalizeExactActressName(lastName)
	if firstKey == "" && lastKey == "" {
		return nil, nil
	}
	var actresses []models.Actress
	if err := tx.Where("dmm_id <= 0").Order("id ASC").Find(&actresses).Error; err != nil {
		return nil, err
	}
	for i := range actresses {
		if firstKey != "" && normalizeExactActressName(actresses[i].FirstName) != firstKey {
			continue
		}
		if lastKey != "" && normalizeExactActressName(actresses[i].LastName) != lastKey {
			continue
		}
		return &actresses[i], nil
	}
	return nil, nil
}

func (r *MovieRepository) FindByID(id string) (*models.Movie, error) {
	var movie models.Movie
	err := r.GetDB().Preload("Actresses").Preload("Genres").Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).First(&movie, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find movie by id %s: %w", id, ErrNotFound)
		}
		return nil, wrapDBErr("find", fmt.Sprintf("movie by id %s", id), err)
	}
	return &movie, nil
}

func (r *MovieRepository) FindByContentID(contentID string) (*models.Movie, error) {
	var movie models.Movie
	err := r.GetDB().Preload("Actresses").Preload("Genres").Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).First(&movie, "content_id = ?", contentID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find movie %s: %w", contentID, ErrNotFound)
		}
		return nil, wrapDBErr("find", fmt.Sprintf("movie %s", contentID), err)
	}
	return &movie, nil
}

func (r *MovieRepository) Delete(id string) error {
	return r.GetDB().Transaction(func(tx *gorm.DB) error {
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

func (r *MovieRepository) List(limit, offset int) ([]models.Movie, error) {
	var movies []models.Movie
	err := r.GetDB().Preload("Actresses").Preload("Genres").Limit(limit).Offset(offset).Find(&movies).Error
	if err != nil {
		return nil, wrapDBErr("find", "movies", err)
	}
	return movies, nil
}

func (r *MovieRepository) ListByActressID(actressID uint, limit, offset int) ([]models.Movie, error) {
	var movies []models.Movie
	query := r.GetDB().
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

func (r *MovieRepository) CountByActressID(actressID uint) (int64, error) {
	var count int64
	err := r.GetDB().Model(&models.Movie{}).
		Joins("JOIN movie_actresses ON movie_actresses.movie_content_id = movies.content_id").
		Where("movie_actresses.actress_id = ?", actressID).
		Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("movies for actress %d", actressID), err)
	}
	return count, nil
}

// ReplaceActressForMovie atomically removes one association and adds verified
// replacements while preserving every other actress already linked to the movie.
func (r *MovieRepository) ReplaceActressForMovie(movieContentID string, sourceActressID uint, replacements []models.Actress) error {
	return retryOnLocked(func() error {
		return r.GetDB().Transaction(func(tx *gorm.DB) error {
			if err := r.ensureActressesExistTx(tx, replacements); err != nil {
				return err
			}
			if err := tx.Exec("DELETE FROM movie_actresses WHERE movie_content_id = ? AND actress_id = ?", movieContentID, sourceActressID).Error; err != nil {
				return err
			}
			for _, actress := range replacements {
				if err := tx.Exec("INSERT OR IGNORE INTO movie_actresses(movie_content_id, actress_id) VALUES (?, ?)", movieContentID, actress.ID).Error; err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// ReplaceUnverifiedActressesForMovie removes every DMM-ID-less cast mapping for
// one movie, preserves positive-DMM mappings, and adds the resolver-verified
// cast atomically. It returns the removed actress IDs for orphan cleanup.
func (r *MovieRepository) ReplaceUnverifiedActressesForMovie(movieContentID string, replacements []models.Actress) ([]uint, error) {
	var removedIDs []uint
	err := retryOnLocked(func() error {
		return r.GetDB().Transaction(func(tx *gorm.DB) error {
			if err := tx.Table("actresses AS actress").
				Select("actress.id").
				Joins("JOIN movie_actresses AS mapping ON mapping.actress_id = actress.id").
				Where("mapping.movie_content_id = ? AND actress.dmm_id <= 0", movieContentID).
				Order("actress.id ASC").Pluck("actress.id", &removedIDs).Error; err != nil {
				return err
			}
			if err := r.ensureActressesExistTx(tx, replacements); err != nil {
				return err
			}
			if len(removedIDs) > 0 {
				if err := tx.Exec("DELETE FROM movie_actresses WHERE movie_content_id = ? AND actress_id IN ?", movieContentID, removedIDs).Error; err != nil {
					return err
				}
			}
			for _, actress := range replacements {
				if err := tx.Exec("INSERT OR IGNORE INTO movie_actresses(movie_content_id, actress_id) VALUES (?, ?)", movieContentID, actress.ID).Error; err != nil {
					return err
				}
			}
			return nil
		})
	})
	return removedIDs, err
}

// ReassignActressAssociations moves all movie links to an already-existing
// canonical actress without deleting either actress row.
func (r *MovieRepository) ReassignActressAssociations(sourceActressID, targetActressID uint) (int64, error) {
	if sourceActressID == 0 || targetActressID == 0 || sourceActressID == targetActressID {
		return 0, ErrInvalidLookup
	}
	var moved int64
	err := retryOnLocked(func() error {
		return r.GetDB().Transaction(func(tx *gorm.DB) error {
			var count int64
			if err := tx.Table("movie_actresses").Where("actress_id = ?", sourceActressID).Count(&count).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT OR IGNORE INTO movie_actresses(movie_content_id, actress_id)
				SELECT movie_content_id, ? FROM movie_actresses WHERE actress_id = ?`, targetActressID, sourceActressID).Error; err != nil {
				return err
			}
			if err := tx.Exec("DELETE FROM movie_actresses WHERE actress_id = ?", sourceActressID).Error; err != nil {
				return err
			}
			moved = count
			return nil
		})
	})
	return moved, err
}
