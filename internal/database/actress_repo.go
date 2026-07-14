package database

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// ActressDMMIDConflictError is returned when an exact-name match points at a
// different verified DMM identity. Callers must report the conflict instead of
// merging or creating a duplicate row.
type ActressDMMIDConflictError struct {
	IncomingDMMID int
	ExistingDMMID int
	ExistingID    uint
}

func (e *ActressDMMIDConflictError) Error() string {
	return fmt.Sprintf("DMM ID %d conflicts with actress %d (DMM ID %d)", e.IncomingDMMID, e.ExistingID, e.ExistingDMMID)
}

func AsActressDMMIDConflict(err error) (*ActressDMMIDConflictError, bool) {
	var conflict *ActressDMMIDConflictError
	return conflict, errors.As(err, &conflict)
}

type ActressRepository struct {
	*BaseRepository[models.Actress, uint]
}

var (
	ErrActressMergeSameID           = errors.New("target_id and source_id must be different")
	ErrActressMergeInvalidID        = errors.New("target_id and source_id must be greater than 0")
	ErrActressMergeInvalidField     = errors.New("invalid merge field")
	ErrActressMergeInvalidDecision  = errors.New("invalid merge resolution")
	ErrActressMergeUniqueConstraint = errors.New("merge would violate unique constraints")
)

type ActressMergeConflict struct {
	Field             string      `json:"field"`
	TargetValue       interface{} `json:"target_value,omitempty"`
	SourceValue       interface{} `json:"source_value,omitempty"`
	DefaultResolution string      `json:"default_resolution"`
}

type ActressMergePreview struct {
	Target             models.Actress                  `json:"target"`
	Source             models.Actress                  `json:"source"`
	ProposedMerged     models.Actress                  `json:"proposed_merged"`
	Conflicts          []ActressMergeConflict          `json:"conflicts"`
	DefaultResolutions map[string]string               `json:"default_resolutions"`
	ConflictByField    map[string]ActressMergeConflict `json:"-"`
}

type ActressMergeResult struct {
	MergedActress     models.Actress `json:"merged_actress"`
	MergedFromID      uint           `json:"merged_from_id"`
	UpdatedMovies     int            `json:"updated_movies"`
	ConflictsResolved int            `json:"conflicts_resolved"`
	AliasesAdded      int            `json:"aliases_added"`
}

func NewActressRepository(db *DB) *ActressRepository {
	return &ActressRepository{
		BaseRepository: NewBaseRepository[models.Actress, uint](
			db, "actress",
			func(a models.Actress) string { return fmt.Sprintf("%d", a.ID) },
			WithDefaultOrder[models.Actress, uint]("japanese_name ASC, last_name ASC, first_name ASC, id ASC"),
			WithNewEntity[models.Actress, uint](func() models.Actress { return models.Actress{} }),
		),
	}
}

func (r *ActressRepository) Create(actress *models.Actress) error {
	return r.BaseRepository.Create(actress)
}

func (r *ActressRepository) Update(actress *models.Actress) error {
	if err := r.GetDB().Save(actress).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("actress %s", actress.JapaneseName), err)
	}
	return nil
}

func (r *ActressRepository) FindByID(id uint) (*models.Actress, error) {
	return r.BaseRepository.FindByID(id)
}

func (r *ActressRepository) Delete(id uint) error {
	return r.BaseRepository.Delete(id)
}

// DeleteByIDs deletes the given actress rows together with their movie
// associations, so no orphaned movie_actresses join rows remain.
func (r *ActressRepository) DeleteByIDs(ids []uint) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var deleted int64
	err := retryOnLocked(func() error {
		return r.GetDB().Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("DELETE FROM movie_actresses WHERE actress_id IN ?", ids).Error; err != nil {
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

// DeleteAll deletes every actress row and all movie associations.
func (r *ActressRepository) DeleteAll() (int64, error) {
	var deleted int64
	err := retryOnLocked(func() error {
		return r.GetDB().Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("DELETE FROM movie_actresses").Error; err != nil {
				return err
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

func (r *ActressRepository) Count() (int64, error) {
	return r.BaseRepository.Count()
}

func (r *ActressRepository) FindByDMMID(dmmID int) (*models.Actress, error) {
	if dmmID < 0 {
		return nil, wrapDBErr("find", fmt.Sprintf("actress by dmm_id %d", dmmID), ErrInvalidLookup)
	}
	if dmmID == 0 {
		return nil, wrapDBErr("find", fmt.Sprintf("actress by dmm_id %d", dmmID), ErrNotFound)
	}
	var actress models.Actress
	err := r.GetDB().First(&actress, "dmm_id = ?", dmmID).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress by dmm_id %d", dmmID), err)
	}
	return &actress, nil
}

func (r *ActressRepository) FindByJapaneseName(name string) (*models.Actress, error) {
	var actress models.Actress
	err := r.GetDB().Order("dmm_id DESC, id ASC").First(&actress, "japanese_name = ?", name).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress %s", name), err)
	}
	return &actress, nil
}

func (r *ActressRepository) FindByFirstNameLastName(firstName, lastName string) (*models.Actress, error) {
	var actress models.Actress
	err := r.GetDB().Order("dmm_id DESC, id ASC").First(&actress, "first_name = ? AND last_name = ?", firstName, lastName).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress %s %s", lastName, firstName), err)
	}
	return &actress, nil
}

func (r *ActressRepository) FindByJapaneseNameAndDMMID(name string, dmmID int) (*models.Actress, error) {
	var actress models.Actress
	if name != "" && dmmID > 0 {
		err := r.GetDB().First(&actress, "japanese_name = ? AND dmm_id = ?", name, dmmID).Error
		if err != nil {
			return nil, wrapDBErr("find", fmt.Sprintf("actress %s dmm_id %d", name, dmmID), err)
		}
		return &actress, nil
	} else if name != "" {
		return r.FindByJapaneseName(name)
	} else if dmmID > 0 {
		return r.FindByDMMID(dmmID)
	}
	return nil, wrapDBErr("find", "actress by japanese_name and dmm_id", ErrInvalidLookup)
}

func (r *ActressRepository) ListAll() ([]models.Actress, error) {
	return r.BaseRepository.ListAll()
}

// ListMissingMetadataIDs returns actresses that are missing either a verified
// DMM ID or a profile thumbnail. IDs are stable-sorted so callers can process
// the result deterministically.
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

// ListMissingMetadata returns the actress rows used by sync so the UI can show
// a stable name even when an individual sync request fails.
func (r *ActressRepository) ListMissingMetadata() ([]models.Actress, error) {
	var actresses []models.Actress
	if err := r.GetDB().
		Where("dmm_id <= 0 OR TRIM(COALESCE(thumb_url, '')) = ''").
		Order("id ASC").
		Find(&actresses).Error; err != nil {
		return nil, wrapDBErr("find", "actresses missing metadata", err)
	}
	return actresses, nil
}

func (r *ActressRepository) FindOrCreate(actress *models.Actress) error {
	return retryOnLocked(func() error { return r.findOrCreateOnce(actress) })
}

func (r *ActressRepository) findOrCreateOnce(actress *models.Actress) error {
	if actress.DMMID > 0 {
		existing, err := r.FindByDMMID(actress.DMMID)
		if err == nil {
			return r.reuseActressWithBackfill(actress, existing)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) && !errors.Is(err, ErrNotFound) {
			return err
		}
	}

	if existing, err := r.findExactReusableActress(*actress); err != nil {
		return err
	} else if existing != nil {
		return r.reuseActressWithBackfill(actress, existing)
	}

	if err := r.Create(actress); err != nil {
		// Verified Unknown/movie tasks can resolve the same performer at the same
		// time. The DMM unique index chooses the winner; all other workers then
		// reuse and backfill that canonical row.
		if actress.DMMID > 0 && strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			existing, findErr := r.FindByDMMID(actress.DMMID)
			if findErr != nil {
				return findErr
			}
			actress.ID = 0
			return r.reuseActressWithBackfill(actress, existing)
		}
		return err
	}
	return nil
}

func (r *ActressRepository) reuseActressWithBackfill(incoming, existing *models.Actress) error {
	if incoming.DMMID > 0 && existing.DMMID > 0 && incoming.DMMID != existing.DMMID {
		return &ActressDMMIDConflictError{IncomingDMMID: incoming.DMMID, ExistingDMMID: existing.DMMID, ExistingID: existing.ID}
	}
	changed := false
	if incoming.DMMID > 0 && existing.DMMID <= 0 {
		existing.DMMID = incoming.DMMID
		changed = true
	}
	if incoming.ThumbURL != "" && existing.ThumbURL == "" {
		existing.ThumbURL = incoming.ThumbURL
		changed = true
	}
	if incoming.JapaneseName != "" && (existing.JapaneseName == "" || models.IsUnknownActressName(existing.JapaneseName)) {
		existing.JapaneseName = incoming.JapaneseName
		changed = true
	}
	incomingHasHangul := containsHangul(incoming.FirstName) || containsHangul(incoming.LastName)
	existingHasHangul := containsHangul(existing.FirstName) || containsHangul(existing.LastName)
	if incoming.FirstName != "" && (existing.FirstName == "" || models.IsUnknownActressName(existing.FirstName) || (incomingHasHangul && !existingHasHangul)) {
		existing.FirstName = incoming.FirstName
		changed = true
	}
	if incoming.LastName != "" && (existing.LastName == "" || models.IsUnknownActressName(existing.LastName) || (incomingHasHangul && !existingHasHangul)) {
		existing.LastName = incoming.LastName
		changed = true
	}
	if changed {
		if err := r.Update(existing); err != nil {
			return err
		}
	}
	*incoming = *existing
	return nil
}

// findExactReusableActress applies the non-fuzzy identity order used by actress
// sync: Japanese name/aliases first, then normalized romanized or Hangul names.
func (r *ActressRepository) findExactReusableActress(incoming models.Actress) (*models.Actress, error) {
	var actresses []models.Actress
	if err := r.GetDB().Order("dmm_id DESC, id ASC").Find(&actresses).Error; err != nil {
		return nil, wrapDBErr("find", "exact reusable actress", err)
	}

	incomingJapanese := exactActressAliasKeys(incoming.JapaneseName, incoming.Aliases)
	if len(incomingJapanese) > 0 {
		for i := range actresses {
			if exactKeySetsIntersect(incomingJapanese, exactActressAliasKeys(actresses[i].JapaneseName, actresses[i].Aliases)) {
				return &actresses[i], nil
			}
		}
	}

	incomingPrimary := exactActressPrimaryKeys(incoming.FirstName, incoming.LastName)
	if len(incomingPrimary) > 0 {
		for i := range actresses {
			if exactKeySetsIntersect(incomingPrimary, exactActressPrimaryKeys(actresses[i].FirstName, actresses[i].LastName)) {
				return &actresses[i], nil
			}
		}
	}
	return nil, nil
}

func exactActressAliasKeys(japaneseName, aliases string) map[string]struct{} {
	keys := make(map[string]struct{})
	for _, value := range append([]string{japaneseName}, strings.Split(aliases, "|")...) {
		if key := normalizeExactActressName(value); key != "" {
			keys[key] = struct{}{}
		}
	}
	return keys
}

func exactActressPrimaryKeys(firstName, lastName string) map[string]struct{} {
	keys := make(map[string]struct{})
	for _, value := range []string{strings.TrimSpace(firstName + " " + lastName), strings.TrimSpace(lastName + " " + firstName)} {
		if key := normalizeExactActressName(value); key != "" && !models.IsUnknownActressName(value) {
			keys[key] = struct{}{}
		}
	}
	return keys
}

func normalizeExactActressName(value string) string {
	var normalized strings.Builder
	for _, char := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(char) || unicode.IsNumber(char) {
			normalized.WriteRune(char)
		}
	}
	return normalized.String()
}

func exactKeySetsIntersect(left, right map[string]struct{}) bool {
	for key := range left {
		if _, exists := right[key]; exists {
			return true
		}
	}
	return false
}

func (r *ActressRepository) List(limit, offset int) ([]models.Actress, error) {
	return r.BaseRepository.List(limit, offset)
}

func (r *ActressRepository) ListSorted(limit, offset int, sortBy, sortOrder string) ([]models.Actress, error) {
	var actresses []models.Actress

	sortBy, sortOrder, err := normalizeActressSort(sortBy, sortOrder)
	if err != nil {
		return nil, err
	}
	dbq := r.GetDB().DB
	for _, clause := range actressOrderClauses(sortBy, sortOrder) {
		dbq = dbq.Order(clause)
	}

	err = dbq.Limit(limit).Offset(offset).Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("find", "actresses", err)
	}
	return actresses, nil
}

func (r *ActressRepository) SearchPaged(query string, limit, offset int) ([]models.Actress, error) {
	var actresses []models.Actress

	searchPattern := "%" + query + "%"
	err := r.GetDB().Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
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

func (r *ActressRepository) SearchPagedSorted(query string, limit, offset int, sortBy, sortOrder string) ([]models.Actress, error) {
	var actresses []models.Actress

	sortBy, sortOrder, err := normalizeActressSort(sortBy, sortOrder)
	if err != nil {
		return nil, err
	}
	searchPattern := "%" + query + "%"

	dbq := r.GetDB().Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
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

func (r *ActressRepository) CountSearch(query string) (int64, error) {
	var count int64
	searchPattern := "%" + query + "%"
	err := r.GetDB().Model(&models.Actress{}).
		Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
			searchPattern, searchPattern, searchPattern).
		Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", "search actresses", err)
	}
	return count, nil
}

func (r *ActressRepository) Search(query string) ([]models.Actress, error) {
	var actresses []models.Actress

	if query == "" {
		err := r.GetDB().Limit(100).Order("japanese_name ASC, last_name ASC, first_name ASC").Find(&actresses).Error
		if err != nil {
			return nil, wrapDBErr("find", "actresses", err)
		}
		return actresses, nil
	}

	searchPattern := "%" + query + "%"
	err := r.GetDB().Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
		searchPattern, searchPattern, searchPattern).
		Order("japanese_name ASC, last_name ASC, first_name ASC").
		Limit(50).
		Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("search", "actresses", err)
	}
	return actresses, nil
}

func (r *ActressRepository) loadPair(targetID, sourceID uint) (*models.Actress, *models.Actress, error) {
	if targetID == 0 || sourceID == 0 {
		return nil, nil, ErrActressMergeInvalidID
	}
	if targetID == sourceID {
		return nil, nil, ErrActressMergeSameID
	}

	target, err := r.FindByID(targetID)
	if err != nil {
		return nil, nil, err
	}
	source, err := r.FindByID(sourceID)
	if err != nil {
		return nil, nil, err
	}
	return target, source, nil
}

func (r *ActressRepository) PreviewMerge(targetID, sourceID uint) (*ActressMergePreview, error) {
	target, source, err := r.loadPair(targetID, sourceID)
	if err != nil {
		return nil, err
	}

	conflicts := buildActressMergeConflicts(target, source)
	defaultResolutions := defaultResolutionsFromConflicts(conflicts)
	merged, err := mergeActressValues(target, source, defaultResolutions)
	if err != nil {
		return nil, err
	}

	canonicalName := canonicalActressName(&merged)
	merged.Aliases, _, _ = mergeAliasValues(target.Aliases, collectActressAliasCandidates(source), canonicalName)

	byField := make(map[string]ActressMergeConflict, len(conflicts))
	for _, conflict := range conflicts {
		byField[conflict.Field] = conflict
	}

	return &ActressMergePreview{
		Target:             *target,
		Source:             *source,
		ProposedMerged:     merged,
		Conflicts:          conflicts,
		DefaultResolutions: defaultResolutions,
		ConflictByField:    byField,
	}, nil
}

func (r *ActressRepository) Merge(targetID, sourceID uint, resolutions map[string]string) (*ActressMergeResult, error) {
	preview, err := r.PreviewMerge(targetID, sourceID)
	if err != nil {
		return nil, err
	}

	normalizedResolutions, err := normalizeMergeResolutions(resolutions)
	if err != nil {
		return nil, err
	}
	for _, conflict := range preview.Conflicts {
		if _, exists := normalizedResolutions[conflict.Field]; !exists {
			normalizedResolutions[conflict.Field] = MergeResolutionTarget
		}
	}

	merged, err := mergeActressValues(&preview.Target, &preview.Source, normalizedResolutions)
	if err != nil {
		return nil, err
	}

	canonicalName := canonicalActressName(&merged)
	aliasesAdded := 0
	sourceCandidates := collectActressAliasCandidates(&preview.Source)
	merged.Aliases, aliasesAdded, _ = mergeAliasValues(
		preview.Target.Aliases,
		sourceCandidates,
		canonicalName,
	)
	sourceAliasUpserts := sourceAliasesForUpsert(sourceCandidates, canonicalName)

	updatedMovies := 0
	conflictsResolved := len(preview.Conflicts)
	err = r.GetDB().Transaction(func(tx *gorm.DB) error {
		if merged.DMMID > 0 {
			var existing models.Actress
			checkErr := tx.Where("dmm_id = ? AND id NOT IN ?", merged.DMMID, []uint{targetID, sourceID}).First(&existing).Error
			if checkErr == nil {
				return fmt.Errorf("%w: dmm_id %d is already used by actress #%d", ErrActressMergeUniqueConstraint, merged.DMMID, existing.ID)
			}
			if checkErr != nil && !errors.Is(checkErr, gorm.ErrRecordNotFound) {
				return wrapDBErr("find", fmt.Sprintf("actress by dmm_id %d for merge", merged.DMMID), checkErr)
			}
		}

		if merged.DMMID > 0 && merged.DMMID == preview.Source.DMMID && preview.Target.DMMID != preview.Source.DMMID {
			tempDMMID := -int(sourceID)
			if tempDMMID == 0 {
				tempDMMID = -1
			}
			if err := tx.Model(&models.Actress{}).Where("id = ?", sourceID).Update("dmm_id", tempDMMID).Error; err != nil {
				return wrapDBErr("update", fmt.Sprintf("merge actress %d temp dmm_id", sourceID), err)
			}
		}

		if err := tx.Model(&models.Actress{}).Where("id = ?", targetID).Updates(map[string]interface{}{
			"dmm_id":        merged.DMMID,
			"first_name":    merged.FirstName,
			"last_name":     merged.LastName,
			"japanese_name": merged.JapaneseName,
			"thumb_url":     merged.ThumbURL,
			"aliases":       merged.Aliases,
			"updated_at":    time.Now().UTC(),
		}).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrActressMergeUniqueConstraint
			}
			return wrapDBErr("update", fmt.Sprintf("merge actress %d", targetID), err)
		}

		var moveErr error
		updatedMovies, moveErr = moveMovieAssociations(tx, sourceID, targetID)
		if moveErr != nil {
			return wrapDBErr("merge", fmt.Sprintf("actress movie associations from %d to %d", sourceID, targetID), moveErr)
		}

		if err := upsertActressAliases(tx, sourceAliasUpserts, canonicalName); err != nil {
			return wrapDBErr("merge", fmt.Sprintf("actress aliases for %s", canonicalName), err)
		}

		if err := tx.Delete(&models.Actress{}, sourceID).Error; err != nil {
			return wrapDBErr("delete", fmt.Sprintf("merge source actress %d", sourceID), err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	mergedRecord, err := r.FindByID(targetID)
	if err != nil {
		return nil, err
	}

	return &ActressMergeResult{
		MergedActress:     *mergedRecord,
		MergedFromID:      sourceID,
		UpdatedMovies:     updatedMovies,
		ConflictsResolved: conflictsResolved,
		AliasesAdded:      aliasesAdded,
	}, nil
}
