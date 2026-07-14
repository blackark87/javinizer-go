package database

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// VerifiedActressResolution describes how a resolver-confirmed identity was
// reconciled with the existing actress table. A non-zero source ID is always
// reused or merged; it is never left behind as a duplicate row.
type VerifiedActressResolution struct {
	Actress       models.Actress
	MergedFromIDs []uint
	UpdatedMovies int
	Created       bool
	Canonicalized bool
}

// ResolveVerifiedIdentity reconciles a resolver-confirmed DMM identity with the
// existing actress table. The verified Japanese name is authoritative. Existing
// nickname/decorated rows become aliases and all of their movie associations are
// moved to the canonical row before the duplicate rows are deleted.
//
// When allowCreate is false, sourceID must identify an existing row and that row
// is canonicalized in place if no reusable row exists. When allowCreate is true,
// a new row is created only after both DMM-ID and exact-name lookups miss.
func (r *ActressRepository) ResolveVerifiedIdentity(sourceID uint, verified models.Actress, allowCreate bool) (*VerifiedActressResolution, error) {
	if verified.DMMID <= 0 {
		return nil, fmt.Errorf("verified actress requires a positive DMM ID")
	}
	if !hasVerifiedActressName(verified) {
		return nil, fmt.Errorf("verified actress requires a non-placeholder name")
	}

	var result *VerifiedActressResolution
	err := retryOnLocked(func() error {
		return r.GetDB().Transaction(func(tx *gorm.DB) error {
			resolved, resolveErr := r.resolveVerifiedIdentityTx(tx, sourceID, verified, allowCreate)
			if resolveErr != nil {
				return resolveErr
			}
			result = resolved
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *ActressRepository) resolveVerifiedIdentityTx(tx *gorm.DB, sourceID uint, verified models.Actress, allowCreate bool) (*VerifiedActressResolution, error) {
	var source *models.Actress
	sourceMissing := false
	if sourceID > 0 {
		var row models.Actress
		if err := tx.First(&row, sourceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				sourceMissing = true
			} else {
				return nil, wrapDBErr("find", fmt.Sprintf("verified source actress %d", sourceID), err)
			}
		} else {
			source = &row
		}
	}

	idOwner, err := findActressByDMMIDTx(tx, verified.DMMID)
	if err != nil {
		return nil, err
	}
	nameOwner, err := findVerifiedNameOwnerTx(tx, verified)
	if err != nil {
		return nil, err
	}
	if nameOwner != nil && nameOwner.DMMID > 0 && nameOwner.DMMID != verified.DMMID {
		return nil, &ActressDMMIDConflictError{
			IncomingDMMID: verified.DMMID,
			ExistingDMMID: nameOwner.DMMID,
			ExistingID:    nameOwner.ID,
		}
	}

	canonical := nameOwner
	if canonical == nil {
		canonical = idOwner
	}
	if canonical == nil {
		canonical = source
	}

	created := false
	if canonical == nil {
		if sourceMissing {
			return nil, ErrNotFound
		}
		if !allowCreate {
			return nil, ErrNotFound
		}
		createdRow := sanitizedVerifiedActress(verified)
		createResult := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&createdRow)
		if createResult.Error != nil {
			return nil, wrapDBErr("create", fmt.Sprintf("verified actress %d", verified.DMMID), createResult.Error)
		}
		if createResult.RowsAffected == 0 {
			canonical, err = findActressByDMMIDTx(tx, verified.DMMID)
			if err != nil {
				return nil, err
			}
			if canonical == nil {
				return nil, wrapDBErr("create", fmt.Sprintf("verified actress %d", verified.DMMID), gorm.ErrDuplicatedKey)
			}
		} else {
			canonical = &createdRow
			created = true
		}
	}
	if canonical.DMMID > 0 && canonical.DMMID != verified.DMMID {
		return nil, &ActressDMMIDConflictError{
			IncomingDMMID: verified.DMMID,
			ExistingDMMID: canonical.DMMID,
			ExistingID:    canonical.ID,
		}
	}

	mergeRows := uniqueActressRows(canonical.ID, idOwner, source)
	aliasCandidates := collectActressAliasCandidates(canonical)
	updatedMovies := 0
	mergedIDs := make([]uint, 0, len(mergeRows))
	for _, mergeRow := range mergeRows {
		aliasCandidates = append(aliasCandidates, collectActressAliasCandidates(mergeRow)...)
		if mergeRow.DMMID == verified.DMMID {
			temporaryID := -int(mergeRow.ID)
			if temporaryID == 0 {
				temporaryID = -1
			}
			if err := tx.Model(&models.Actress{}).Where("id = ?", mergeRow.ID).Update("dmm_id", temporaryID).Error; err != nil {
				return nil, wrapDBErr("update", fmt.Sprintf("temporary DMM ID for actress %d", mergeRow.ID), err)
			}
		}
		moved, moveErr := moveMovieAssociations(tx, mergeRow.ID, canonical.ID)
		if moveErr != nil {
			return nil, wrapDBErr("merge", fmt.Sprintf("verified actress associations %d to %d", mergeRow.ID, canonical.ID), moveErr)
		}
		updatedMovies += moved
		if err := moveActressTaskReferences(tx, mergeRow.ID, canonical.ID); err != nil {
			return nil, err
		}
		if err := tx.Delete(&models.Actress{}, mergeRow.ID).Error; err != nil {
			return nil, wrapDBErr("delete", fmt.Sprintf("verified duplicate actress %d", mergeRow.ID), err)
		}
		mergedIDs = append(mergedIDs, mergeRow.ID)
	}

	canonicalized := applyVerifiedActress(&verified, canonical, aliasCandidates)
	if err := tx.Model(&models.Actress{}).Where("id = ?", canonical.ID).Updates(map[string]interface{}{
		"dmm_id":        canonical.DMMID,
		"first_name":    canonical.FirstName,
		"last_name":     canonical.LastName,
		"japanese_name": canonical.JapaneseName,
		"thumb_url":     canonical.ThumbURL,
		"aliases":       canonical.Aliases,
		"updated_at":    time.Now().UTC(),
	}).Error; err != nil {
		return nil, wrapDBErr("update", fmt.Sprintf("verified canonical actress %d", canonical.ID), err)
	}

	if canonicalized && tx.Migrator().HasTable(&models.ActressTranslation{}) {
		if err := tx.Where("actress_id = ?", canonical.ID).Delete(&models.ActressTranslation{}).Error; err != nil {
			return nil, wrapDBErr("delete", fmt.Sprintf("stale actress translations %d", canonical.ID), err)
		}
	}
	if err := upsertActressAliases(tx, filterVerifiedAliases(aliasCandidates), canonicalActressName(canonical)); err != nil {
		return nil, wrapDBErr("merge", fmt.Sprintf("verified aliases for actress %d", canonical.ID), err)
	}

	var loaded models.Actress
	if err := tx.First(&loaded, canonical.ID).Error; err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("verified canonical actress %d", canonical.ID), err)
	}
	return &VerifiedActressResolution{
		Actress:       loaded,
		MergedFromIDs: mergedIDs,
		UpdatedMovies: updatedMovies,
		Created:       created,
		Canonicalized: canonicalized,
	}, nil
}

func findActressByDMMIDTx(tx *gorm.DB, dmmID int) (*models.Actress, error) {
	var actress models.Actress
	err := tx.Where("dmm_id = ?", dmmID).First(&actress).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress by verified DMM ID %d", dmmID), err)
	}
	return &actress, nil
}

func findVerifiedNameOwnerTx(tx *gorm.DB, verified models.Actress) (*models.Actress, error) {
	var actresses []models.Actress
	if err := tx.Order("dmm_id DESC, id ASC").Find(&actresses).Error; err != nil {
		return nil, wrapDBErr("find", "verified actress name owner", err)
	}
	// Prefer the row whose canonical field itself matches the verified identity.
	// A polluted nickname row may already have the real name in its aliases and
	// own the DMM ID; selecting it first would leave the actual canonical row as
	// a duplicate.
	verifiedJapaneseName := normalizeExactActressName(verified.JapaneseName)
	if verifiedJapaneseName != "" {
		for i := range actresses {
			if normalizeExactActressName(actresses[i].JapaneseName) == verifiedJapaneseName {
				return &actresses[i], nil
			}
		}
	}
	verifiedPrimary := exactActressPrimaryKeys(verified.FirstName, verified.LastName)
	if len(verifiedPrimary) > 0 {
		for i := range actresses {
			if exactKeySetsIntersect(verifiedPrimary, exactActressPrimaryKeys(actresses[i].FirstName, actresses[i].LastName)) {
				return &actresses[i], nil
			}
		}
	}
	verifiedJapanese := exactActressAliasKeys(verified.JapaneseName, verified.Aliases)
	for i := range actresses {
		if exactKeySetsIntersect(verifiedJapanese, exactActressAliasKeys(actresses[i].JapaneseName, actresses[i].Aliases)) {
			return &actresses[i], nil
		}
	}
	return nil, nil
}

func uniqueActressRows(canonicalID uint, rows ...*models.Actress) []*models.Actress {
	seen := map[uint]struct{}{canonicalID: {}}
	result := make([]*models.Actress, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.ID == 0 {
			continue
		}
		if _, exists := seen[row.ID]; exists {
			continue
		}
		seen[row.ID] = struct{}{}
		copyRow := *row
		result = append(result, &copyRow)
	}
	return result
}

func hasVerifiedActressName(actress models.Actress) bool {
	if models.IsUnknownActressFields(actress.LastName, actress.FirstName, actress.JapaneseName) ||
		models.IsDescriptiveNonName(actress.LastName, actress.FirstName, actress.JapaneseName) {
		return false
	}
	return strings.TrimSpace(actress.JapaneseName) != "" || strings.TrimSpace(actress.FullName()) != ""
}

func sanitizedVerifiedActress(verified models.Actress) models.Actress {
	verified.ID = 0
	verified.CreatedAt = time.Time{}
	verified.UpdatedAt = time.Time{}
	verified.JapaneseName = strings.TrimSpace(verified.JapaneseName)
	verified.FirstName = strings.TrimSpace(verified.FirstName)
	verified.LastName = strings.TrimSpace(verified.LastName)
	verified.ThumbURL = strings.TrimSpace(verified.ThumbURL)
	return verified
}

func applyVerifiedActress(verified, canonical *models.Actress, aliasCandidates []string) bool {
	oldJapanese := strings.TrimSpace(canonical.JapaneseName)
	newJapanese := strings.TrimSpace(verified.JapaneseName)
	identityChanged := newJapanese != "" && normalizeExactActressName(oldJapanese) != normalizeExactActressName(newJapanese)
	if oldJapanese != "" && identityChanged {
		aliasCandidates = append(aliasCandidates, oldJapanese)
	}
	if identityChanged {
		if fullName := strings.TrimSpace(canonical.FullName()); fullName != "" && !models.IsUnknownActressName(fullName) {
			aliasCandidates = append(aliasCandidates, fullName)
		}
	}

	canonical.DMMID = verified.DMMID
	if newJapanese != "" {
		canonical.JapaneseName = newJapanese
	}
	if identityChanged {
		canonical.FirstName = strings.TrimSpace(verified.FirstName)
		canonical.LastName = strings.TrimSpace(verified.LastName)
	} else {
		if strings.TrimSpace(verified.FirstName) != "" && (canonical.FirstName == "" || (containsHangul(verified.FirstName) && !containsHangul(canonical.FirstName))) {
			canonical.FirstName = strings.TrimSpace(verified.FirstName)
		}
		if strings.TrimSpace(verified.LastName) != "" && (canonical.LastName == "" || (containsHangul(verified.LastName) && !containsHangul(canonical.LastName))) {
			canonical.LastName = strings.TrimSpace(verified.LastName)
		}
	}
	if strings.TrimSpace(verified.ThumbURL) != "" && (canonical.ThumbURL == "" || identityChanged) {
		canonical.ThumbURL = strings.TrimSpace(verified.ThumbURL)
	}
	canonical.Aliases, _, _ = mergeAliasValues(canonical.Aliases, filterVerifiedAliases(aliasCandidates), canonicalActressName(canonical))
	return identityChanged
}

func filterVerifiedAliases(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := normalizeExactActressName(value)
		if key == "" || models.IsUnknownActressName(value) {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func moveActressTaskReferences(tx *gorm.DB, sourceID, targetID uint) error {
	if !tx.Migrator().HasTable(&models.ActressSyncTask{}) {
		return nil
	}
	if err := tx.Model(&models.ActressSyncTask{}).Where("actress_id = ?", sourceID).Update("actress_id", targetID).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("actress sync task references %d to %d", sourceID, targetID), err)
	}
	return nil
}
