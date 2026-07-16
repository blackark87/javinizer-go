package aggregator

import (
	"strings"
	"unicode"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
)

func cleanActressInfoName(info *models.ActressInfo) {
	translation.CleanActressInfo(info)
}

func dropRedundantUnknowns(actresses []models.Actress) []models.Actress {
	hasReal := false
	for _, actress := range actresses {
		if !models.IsUnknownActressFields(actress.LastName, actress.FirstName, actress.JapaneseName) {
			hasReal = true
			break
		}
	}
	if !hasReal {
		return actresses
	}
	filtered := actresses[:0]
	for _, actress := range actresses {
		if !models.IsUnknownActressFields(actress.LastName, actress.FirstName, actress.JapaneseName) {
			filtered = append(filtered, actress)
		}
	}
	return filtered
}

func actressHasUsableReading(actress models.Actress) bool {
	if strings.Contains(strings.ToLower(actress.ThumbURL), "/actjpgs/") {
		return true
	}
	name := strings.TrimSpace(actress.LastName + " " + actress.FirstName)
	if name == "" {
		return false
	}
	hasLatin := false
	for _, r := range name {
		if unicode.In(r, unicode.Hangul) {
			return true
		}
		if unicode.Is(unicode.Latin, r) {
			hasLatin = true
		}
	}
	return hasLatin
}

// enrichActressReadings fills only missing fields from a stored actress.  This
// makes DMM's authoritative romaji available to Korean translation without
// overwriting data supplied by the active scrape.
func (a *Aggregator) enrichActressReadings(actresses []models.Actress) {
	if a == nil || a.actressLookupRepo == nil {
		return
	}
	for i := range actresses {
		actress := &actresses[i]
		if models.IsUnknownActressFields(actress.LastName, actress.FirstName, actress.JapaneseName) || actressHasUsableReading(*actress) {
			continue
		}

		var stored *models.Actress
		var err error
		if actress.DMMID > 0 {
			stored, err = a.actressLookupRepo.FindByDMMID(actress.DMMID)
		} else if name := strings.TrimSpace(actress.JapaneseName); name != "" {
			stored, err = a.actressLookupRepo.FindUnverifiedByJapaneseName(name)
		}
		if err != nil || stored == nil {
			continue
		}
		if actress.FirstName == "" {
			actress.FirstName = stored.FirstName
		}
		if actress.LastName == "" {
			actress.LastName = stored.LastName
		}
		if actress.ThumbURL == "" {
			actress.ThumbURL = stored.ThumbURL
		}
	}
}
