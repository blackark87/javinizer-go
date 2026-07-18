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

// VerifiedActressResolution describes the atomic reconciliation of a positive
// DMM identity. DMM ID always wins over name; an exact Japanese-name match may
// only promote or merge rows that still have no positive DMM ID.
type VerifiedActressResolution struct {
	Actress        models.Actress
	MergedFromIDs  []uint
	UpdatedMovies  int
	Created        bool
	Promoted       bool
	Canonicalized  bool
	ProfileChanged bool
	NameChanged    bool
	AliasesAdded   []string
}

// ResolveVerifiedIdentity reconciles a positive DMM identity with existing
// name-only or duplicate actress rows in one transaction.
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
	if sourceID > 0 {
		var count int64
		if err := tx.Model(&models.Actress{}).Where("id = ?", sourceID).Count(&count).Error; err != nil {
			return nil, wrapDBErr("find", fmt.Sprintf("verified source actress %d", sourceID), err)
		}
		if count == 0 {
			return nil, ErrNotFound
		}
	}

	verified = sanitizedVerifiedActress(verified)
	idOwner, err := findActressByDMMIDTx(tx, verified.DMMID)
	if err != nil {
		return nil, err
	}
	nameOwners, err := findUnverifiedJapaneseNameOwnersTx(tx, verified.JapaneseName)
	if err != nil {
		return nil, err
	}

	canonical := idOwner
	if canonical == nil && len(nameOwners) > 0 {
		copyRow := nameOwners[0]
		canonical = &copyRow
	}

	created := false
	if canonical == nil {
		if !allowCreate {
			return nil, ErrNotFound
		}
		createdRow := verified
		createResult := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&createdRow)
		if createResult.Error != nil {
			if errors.Is(createResult.Error, gorm.ErrDuplicatedKey) {
				canonical, err = findActressByDMMIDTx(tx, verified.DMMID)
				if err != nil {
					return nil, err
				}
				if canonical != nil {
					goto resolvedCanonical
				}
			}
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

resolvedCanonical:
	before := *canonical
	promoted := !created && canonical.DMMID <= 0
	profileChanged, aliasesAdded := applyVerifiedActress(&verified, canonical)

	mergeRows := make([]models.Actress, 0, len(nameOwners))
	for _, row := range nameOwners {
		if row.ID != canonical.ID {
			mergeRows = append(mergeRows, row)
		}
	}
	updatedMovies := 0
	mergedIDs := make([]uint, 0, len(mergeRows))
	for _, mergeRow := range mergeRows {
		moved, moveErr := moveMovieAssociations(tx, mergeRow.ID, canonical.ID)
		if moveErr != nil {
			return nil, wrapDBErr("merge", fmt.Sprintf("verified actress associations %d to %d", mergeRow.ID, canonical.ID), moveErr)
		}
		updatedMovies += moved
		if err := moveActressTaskReferences(tx, mergeRow.ID, canonical.ID); err != nil {
			return nil, err
		}
		sameJapanese := normalizeExactActressName(mergeRow.JapaneseName) == normalizeExactActressName(canonical.JapaneseName)
		if err := mergeActressTranslationsTx(tx, mergeRow.ID, canonical.ID, sameJapanese); err != nil {
			return nil, err
		}
		if err := tx.Delete(&models.Actress{}, mergeRow.ID).Error; err != nil {
			return nil, wrapDBErr("delete", fmt.Sprintf("verified duplicate actress %d", mergeRow.ID), err)
		}
		mergedIDs = append(mergedIDs, mergeRow.ID)
	}

	aliasesChanged := before.Aliases != canonical.Aliases
	if profileChanged || aliasesChanged {
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
	}

	nameChanged := before.JapaneseName != canonical.JapaneseName || before.FirstName != canonical.FirstName || before.LastName != canonical.LastName
	if !created && nameChanged && tx.Migrator().HasTable(&models.ActressTranslation{}) {
		if err := tx.Where("actress_id = ?", canonical.ID).Delete(&models.ActressTranslation{}).Error; err != nil {
			return nil, wrapDBErr("delete", fmt.Sprintf("stale actress translations %d", canonical.ID), err)
		}
	}

	var loaded models.Actress
	if err := tx.First(&loaded, canonical.ID).Error; err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("verified canonical actress %d", canonical.ID), err)
	}
	return &VerifiedActressResolution{
		Actress: loaded, MergedFromIDs: mergedIDs, UpdatedMovies: updatedMovies,
		Created: created, Promoted: promoted, Canonicalized: created || promoted || len(mergedIDs) > 0,
		ProfileChanged: profileChanged, NameChanged: nameChanged, AliasesAdded: aliasesAdded,
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

func findUnverifiedJapaneseNameOwnersTx(tx *gorm.DB, name string) ([]models.Actress, error) {
	key := normalizeExactActressName(name)
	if key == "" {
		return nil, nil
	}
	var actresses []models.Actress
	if err := tx.Where("dmm_id <= 0").Order("id ASC").Find(&actresses).Error; err != nil {
		return nil, wrapDBErr("find", "unverified actress name owners", err)
	}
	owners := make([]models.Actress, 0)
	for _, actress := range actresses {
		if normalizeExactActressName(actress.JapaneseName) == key {
			owners = append(owners, actress)
		}
	}
	return owners, nil
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
	verified.Aliases = ""
	return verified
}

func applyVerifiedActress(verified, canonical *models.Actress) (bool, []string) {
	before := *canonical
	canonical.DMMID = verified.DMMID

	oldJapanese := strings.TrimSpace(canonical.JapaneseName)
	newJapanese := strings.TrimSpace(verified.JapaneseName)
	aliasesAdded := []string(nil)
	switch {
	case !isUsableVerifiedJapaneseName(oldJapanese) && isUsableVerifiedJapaneseName(newJapanese):
		canonical.JapaneseName = newJapanese
	case isMalformedCompositeActressName(oldJapanese) && isUsableVerifiedJapaneseName(newJapanese) &&
		strings.Contains(oldJapanese, newJapanese):
		// Older SougouWiki resolution accidentally persisted the text of a DMM
		// link that spanned readings and aliases. A clean verified DMM name is
		// authoritative and the polluted composite must not be retained as an
		// alias.
		canonical.JapaneseName = newJapanese
		canonical.FirstName = strings.TrimSpace(verified.FirstName)
		canonical.LastName = strings.TrimSpace(verified.LastName)
	case isUsableVerifiedJapaneseName(oldJapanese) && isUsableVerifiedJapaneseName(newJapanese) &&
		normalizeExactActressName(oldJapanese) != normalizeExactActressName(newJapanese):
		canonical.Aliases, aliasesAdded = mergeVerifiedAliasValues(canonical.Aliases, []string{newJapanese}, oldJapanese)
	}

	if !hasUsablePrimaryActressName(*canonical) {
		if value := strings.TrimSpace(verified.FirstName); value != "" && !models.IsUnknownActressName(value) {
			canonical.FirstName = value
		}
		if value := strings.TrimSpace(verified.LastName); value != "" && !models.IsUnknownActressName(value) {
			canonical.LastName = value
		}
	}
	if strings.TrimSpace(canonical.ThumbURL) == "" && strings.TrimSpace(verified.ThumbURL) != "" {
		canonical.ThumbURL = strings.TrimSpace(verified.ThumbURL)
	}

	profileChanged := before.DMMID != canonical.DMMID || before.JapaneseName != canonical.JapaneseName ||
		before.FirstName != canonical.FirstName || before.LastName != canonical.LastName || before.ThumbURL != canonical.ThumbURL
	return profileChanged, aliasesAdded
}

func isMalformedCompositeActressName(name string) bool {
	return strings.ContainsAny(name, "/／") || strings.ContainsAny(name, "(（")
}

func mergeVerifiedAliasValues(existing string, candidates []string, canonicalName string) (string, []string) {
	seen := make(map[string]struct{})
	merged := make([]string, 0)
	for _, value := range strings.Split(existing, "|") {
		value = strings.TrimSpace(value)
		key := normalizeExactActressName(value)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, value)
	}
	canonicalKey := normalizeExactActressName(canonicalName)
	added := make([]string, 0)
	for _, value := range filterVerifiedAliases(candidates) {
		key := normalizeExactActressName(value)
		if key == canonicalKey {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, value)
		added = append(added, value)
	}
	return strings.Join(merged, "|"), added
}

func isUsableVerifiedJapaneseName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && !models.IsUnknownActressName(name) && !models.IsDescriptiveNonName("", "", name)
}

func hasUsablePrimaryActressName(actress models.Actress) bool {
	first := strings.TrimSpace(actress.FirstName)
	last := strings.TrimSpace(actress.LastName)
	if first == "" && last == "" {
		return false
	}
	return !models.IsUnknownActressName(first) && !models.IsUnknownActressName(last) &&
		!models.IsUnknownActressName(strings.TrimSpace(last+" "+first)) &&
		!models.IsDescriptiveNonName(last, first, "")
}

func mergeActressTranslationsTx(tx *gorm.DB, sourceID, targetID uint, preserve bool) error {
	if !tx.Migrator().HasTable(&models.ActressTranslation{}) {
		return nil
	}
	if preserve {
		var translations []models.ActressTranslation
		if err := tx.Where("actress_id = ?", sourceID).Find(&translations).Error; err != nil {
			return wrapDBErr("find", fmt.Sprintf("actress translations %d", sourceID), err)
		}
		for _, record := range translations {
			record.ID = 0
			record.ActressID = targetID
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "actress_id"}, {Name: "language"}}, DoNothing: true,
			}).Create(&record).Error; err != nil {
				return wrapDBErr("merge", fmt.Sprintf("actress translations %d to %d", sourceID, targetID), err)
			}
		}
	}
	if err := tx.Where("actress_id = ?", sourceID).Delete(&models.ActressTranslation{}).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("actress translations %d", sourceID), err)
	}
	return nil
}

func filterVerifiedAliases(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := normalizeExactActressName(value)
		if key == "" || models.IsUnknownActressName(value) || models.IsDescriptiveNonName("", "", value) {
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
