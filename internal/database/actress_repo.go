package database

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// ActressRepository persists and queries actress records, providing CRUD,
// lookup, search, and merge operations on top of BaseRepository.
type ActressRepository struct {
	*BaseRepository[models.Actress, uint]
	merger *actressMerger
}

// NewActressRepository constructs an ActressRepository backed by the given DB
// with the default sort order for listing actresses.
func NewActressRepository(db *DB) *ActressRepository {
	repo := &ActressRepository{
		BaseRepository: NewBaseRepository[models.Actress, uint](
			db, "actress",
			func(a models.Actress) string { return fmt.Sprintf("%d", a.ID) },
			withDefaultOrder[models.Actress, uint]("japanese_name ASC, last_name ASC, first_name ASC, id ASC"),
			WithNewEntity[models.Actress, uint](func() models.Actress { return models.Actress{} }),
		),
	}
	repo.merger = &actressMerger{repo: repo}
	return repo
}

// Create inserts a new actress record.
func (r *ActressRepository) Create(ctx context.Context, actress *models.Actress) error {
	return r.BaseRepository.Create(ctx, actress)
}

// Update saves all fields of the given actress record.
func (r *ActressRepository) Update(ctx context.Context, actress *models.Actress) error {
	if err := r.GetDB().WithContext(ctx).Save(actress).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("actress %s", actress.JapaneseName), err)
	}
	return nil
}

// RenameNameFields updates only the editable name columns (first_name,
// last_name, japanese_name) of the actress identified by id. It is used by the
// review-page edit path to apply an explicit actress rename without clobbering
// other columns (created_at, dmm_id, thumb_url, aliases) the way a full-row
// Save would. Callers should gate on a name-field change to avoid bumping
// updated_at for unedited actresses.
func (r *ActressRepository) RenameNameFields(ctx context.Context, id uint, firstName, lastName, japaneseName string) error {
	if id == 0 {
		return wrapDBErr("rename", "actress id 0", ErrInvalidLookup)
	}
	updates := map[string]interface{}{
		"first_name":    firstName,
		"last_name":     lastName,
		"japanese_name": japaneseName,
	}
	if err := r.GetDB().WithContext(ctx).Model(&models.Actress{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return wrapDBErr("rename", fmt.Sprintf("actress %d", id), err)
	}
	return nil
}

// FindByID loads an actress by its primary key.
func (r *ActressRepository) FindByID(ctx context.Context, id uint) (*models.Actress, error) {
	return r.BaseRepository.FindByID(ctx, id)
}

// Delete removes the actress with the given primary key.
func (r *ActressRepository) Delete(ctx context.Context, id uint) error {
	if id == 0 {
		return wrapDBErr("delete", "actress 0", ErrInvalidLookup)
	}
	err := retryOnLocked(func() error {
		return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := deleteActressRelationsTx(tx, []uint{id}); err != nil {
				return err
			}
			return tx.Delete(&models.Actress{}, id).Error
		})
	})
	return wrapDBErr("delete", fmt.Sprintf("actress %d", id), err)
}

func deleteActressRelationsTx(tx *gorm.DB, ids []uint) error {
	if err := tx.Exec("DELETE FROM movie_actresses WHERE actress_id IN ?", ids).Error; err != nil {
		return err
	}
	if tx.Migrator().HasTable(&models.ActressTranslation{}) {
		if err := tx.Where("actress_id IN ?", ids).Delete(&models.ActressTranslation{}).Error; err != nil {
			return err
		}
	}
	if tx.Migrator().HasTable(&models.ActressAlias{}) {
		if err := tx.Model(&models.ActressAlias{}).Where("alias_actress_id IN ?", ids).Update("alias_actress_id", 0).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.ActressAlias{}).Where("canonical_actress_id IN ?", ids).Update("canonical_actress_id", 0).Error; err != nil {
			return err
		}
	}
	return nil
}

// DeleteByIDs deletes actress rows and their movie associations atomically.
func (r *ActressRepository) DeleteByIDs(ctx context.Context, ids []uint) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var deleted int64
	err := retryOnLocked(func() error {
		return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := deleteActressRelationsTx(tx, ids); err != nil {
				return err
			}
			res := tx.Where("id IN ?", ids).Delete(&models.Actress{})
			if res.Error != nil {
				return res.Error
			}
			deleted = res.RowsAffected
			return nil
		})
	})
	if err != nil {
		return 0, wrapDBErr("delete", "actresses by ids", err)
	}
	return deleted, nil
}

// DeleteAll deletes every actress row and all movie associations atomically.
func (r *ActressRepository) DeleteAll(ctx context.Context) (int64, error) {
	var deleted int64
	err := retryOnLocked(func() error {
		return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var ids []uint
			if err := tx.Model(&models.Actress{}).Pluck("id", &ids).Error; err != nil {
				return err
			}
			if len(ids) > 0 {
				if err := deleteActressRelationsTx(tx, ids); err != nil {
					return err
				}
			}
			res := tx.Where("1 = 1").Delete(&models.Actress{})
			if res.Error != nil {
				return res.Error
			}
			deleted = res.RowsAffected
			return nil
		})
	})
	if err != nil {
		return 0, wrapDBErr("delete", "all actresses", err)
	}
	return deleted, nil
}

// Count returns the total number of actress records.
func (r *ActressRepository) Count(ctx context.Context) (int64, error) {
	return r.BaseRepository.Count(ctx)
}

// FindByDMMID loads the actress with the given DMM identifier, returning
// ErrNotFound when the id is zero and ErrInvalidLookup when negative.
func (r *ActressRepository) FindByDMMID(ctx context.Context, dmmID int) (*models.Actress, error) {
	if dmmID < 0 {
		return nil, wrapDBErr("find", fmt.Sprintf("actress by dmm_id %d", dmmID), ErrInvalidLookup)
	}
	if dmmID == 0 {
		return nil, wrapDBErr("find", fmt.Sprintf("actress by dmm_id %d", dmmID), ErrNotFound)
	}
	var actress models.Actress
	err := r.GetDB().WithContext(ctx).First(&actress, "dmm_id = ?", dmmID).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress by dmm_id %d", dmmID), err)
	}
	return &actress, nil
}

// FindByJapaneseName loads the first actress matching the given Japanese name,
// preferring higher DMM ids when duplicates exist.
func (r *ActressRepository) FindByJapaneseName(ctx context.Context, name string) (*models.Actress, error) {
	var actress models.Actress
	err := r.GetDB().WithContext(ctx).Order("dmm_id DESC, id ASC").First(&actress, "japanese_name = ?", name).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress %s", name), err)
	}
	return &actress, nil
}

// FindByFirstNameLastName loads the first actress matching the given first and
// last name, preferring higher DMM ids when duplicates exist.
func (r *ActressRepository) FindByFirstNameLastName(ctx context.Context, firstName, lastName string) (*models.Actress, error) {
	var actress models.Actress
	err := r.GetDB().WithContext(ctx).Order("dmm_id DESC, id ASC").First(&actress, "first_name = ? AND last_name = ?", firstName, lastName).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress %s %s", lastName, firstName), err)
	}
	return &actress, nil
}

// FindByJapaneseNameAndDMMID loads an actress by Japanese name and DMM id,
// falling back to whichever identifier is provided when only one is set.
func (r *ActressRepository) FindByJapaneseNameAndDMMID(ctx context.Context, name string, dmmID int) (*models.Actress, error) {
	var actress models.Actress
	if name != "" && dmmID > 0 {
		err := r.GetDB().WithContext(ctx).First(&actress, "japanese_name = ? AND dmm_id = ?", name, dmmID).Error
		if err != nil {
			return nil, wrapDBErr("find", fmt.Sprintf("actress %s dmm_id %d", name, dmmID), err)
		}
		return &actress, nil
	} else if name != "" {
		return r.FindByJapaneseName(ctx, name)
	} else if dmmID > 0 {
		return r.FindByDMMID(ctx, dmmID)
	}
	return nil, wrapDBErr("find", "actress by japanese_name and dmm_id", ErrInvalidLookup)
}

// ListAll returns every actress record in the default sort order.
func (r *ActressRepository) ListAll(ctx context.Context) ([]models.Actress, error) {
	return r.BaseRepository.ListAll(ctx)
}

// ListMissingMetadataIDs returns stable actress IDs missing a verified DMM ID
// or profile thumbnail. It is used to seed explicit actress-sync jobs.
func (r *ActressRepository) ListMissingMetadataIDs() ([]uint, error) {
	actresses, err := r.ListMissingMetadata()
	if err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(actresses))
	for _, actress := range actresses {
		ids = append(ids, actress.ID)
	}
	return ids, nil
}

// ListMissingMetadata returns actresses without a verified DMM ID or thumbnail.
func (r *ActressRepository) ListMissingMetadata() ([]models.Actress, error) {
	var actresses []models.Actress
	err := r.GetDB().WithContext(context.Background()).
		Where("dmm_id <= 0 OR TRIM(COALESCE(thumb_url, '')) = ''").
		Order("id ASC").Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("find", "actresses missing metadata", err)
	}
	return actresses, nil
}

// FindOrCreate returns the existing actress with the given Japanese name, or
// creates a new record when none is found.
func (r *ActressRepository) FindOrCreate(ctx context.Context, actress *models.Actress) error {
	if actress == nil {
		return wrapDBErr("find or create", "nil actress", ErrInvalidLookup)
	}

	// Keep each retry independent: a failed SQLite INSERT can still mutate the
	// model's primary key, which must not leak into the next attempt.
	incoming := *actress
	var resolved models.Actress
	err := retryOnLocked(func() error {
		candidate := incoming
		candidate.ID = 0
		db := r.GetDB().WithContext(ctx)

		var existing models.Actress
		var found bool
		var findErr error
		switch {
		case candidate.DMMID > 0:
			existing, found, findErr = lookupActressByDMMID(db, &candidate)
		case candidate.JapaneseName != "":
			existing, found, findErr = lookupActressByJapaneseName(db, &candidate)
		}
		if findErr != nil {
			return findErr
		}
		if found {
			resolved = existing
			return nil
		}

		if createErr := db.Create(&candidate).Error; createErr != nil {
			if isDuplicateKey(createErr) && candidate.DMMID > 0 {
				existing, found, findErr = lookupActressByDMMID(db, &candidate)
				if findErr != nil {
					return findErr
				}
				if found {
					resolved = existing
					return nil
				}
			}
			return createErr
		}
		resolved = candidate
		return nil
	})
	if err != nil {
		return wrapDBErr("find or create", "actress", err)
	}
	*actress = resolved
	return nil
}

// List returns a page of actresses limited by limit and offset.
func (r *ActressRepository) List(ctx context.Context, limit, offset int) ([]models.Actress, error) {
	return r.BaseRepository.List(ctx, limit, offset)
}

// ListSorted returns a page of actresses ordered by the validated sortBy and
// sortOrder columns.
func (r *ActressRepository) ListSorted(ctx context.Context, limit, offset int, sortBy, sortOrder string) ([]models.Actress, error) {
	var actresses []models.Actress

	sortBy, sortOrder, err := normalizeActressSort(sortBy, sortOrder)
	if err != nil {
		return nil, err
	}
	dbq := r.GetDB().WithContext(ctx)
	for _, clause := range actressOrderClauses(sortBy, sortOrder) {
		dbq = dbq.Order(clause)
	}

	err = dbq.Limit(limit).Offset(offset).Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("find", "actresses", err)
	}
	return actresses, nil
}

// SearchPaged returns a page of actresses whose names match the query, ordered
// by the default sort.
func (r *ActressRepository) SearchPaged(ctx context.Context, query string, limit, offset int) ([]models.Actress, error) {
	var actresses []models.Actress

	searchPattern := "%" + query + "%"
	err := r.GetDB().WithContext(ctx).Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
		searchPattern, searchPattern, searchPattern).
		Order("japanese_name ASC, last_name ASC, first_name ASC, id ASC").
		Limit(limit).
		Offset(offset).
		Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("search", "actresses", err)
	}
	return actresses, nil
}

// SearchPagedSorted returns a page of actresses matching the query, ordered by
// the validated sortBy and sortOrder columns.
func (r *ActressRepository) SearchPagedSorted(ctx context.Context, query string, limit, offset int, sortBy, sortOrder string) ([]models.Actress, error) {
	var actresses []models.Actress

	sortBy, sortOrder, err := normalizeActressSort(sortBy, sortOrder)
	if err != nil {
		return nil, err
	}
	searchPattern := "%" + query + "%"

	dbq := r.GetDB().WithContext(ctx).Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
		searchPattern, searchPattern, searchPattern)
	for _, clause := range actressOrderClauses(sortBy, sortOrder) {
		dbq = dbq.Order(clause)
	}

	err = dbq.Limit(limit).Offset(offset).Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("search", "actresses", err)
	}
	return actresses, nil
}

// CountSearch returns the number of actresses whose names match the query.
func (r *ActressRepository) CountSearch(ctx context.Context, query string) (int64, error) {
	var count int64
	searchPattern := "%" + query + "%"
	err := r.GetDB().WithContext(ctx).Model(&models.Actress{}).
		Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
			searchPattern, searchPattern, searchPattern).
		Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", "search actresses", err)
	}
	return count, nil
}

// Search returns up to 50 actresses matching the query, or up to 100 when
// the query is empty.
func (r *ActressRepository) Search(ctx context.Context, query string) ([]models.Actress, error) {
	var actresses []models.Actress

	if query == "" {
		err := r.GetDB().WithContext(ctx).Limit(100).Order("japanese_name ASC, last_name ASC, first_name ASC").Find(&actresses).Error
		if err != nil {
			return nil, wrapDBErr("find", "actresses", err)
		}
		return actresses, nil
	}

	searchPattern := "%" + query + "%"
	err := r.GetDB().WithContext(ctx).Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
		searchPattern, searchPattern, searchPattern).
		Order("japanese_name ASC, last_name ASC, first_name ASC").
		Limit(50).
		Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("search", "actresses", err)
	}
	return actresses, nil
}

// PreviewMerge computes a non-persistent preview of merging the source
// actress into the target actress.
func (r *ActressRepository) PreviewMerge(ctx context.Context, targetID, sourceID uint) (*ActressMergePreview, error) {
	return r.merger.PreviewMerge(ctx, targetID, sourceID)
}

// Merge computes a merge plan for the source actress into the target and
// executes it within a transaction.
func (r *ActressRepository) Merge(ctx context.Context, targetID, sourceID uint, resolutions map[string]string) (*ActressMergeResult, error) {
	return r.merger.Merge(ctx, targetID, sourceID, resolutions, r.GetDB())
}
