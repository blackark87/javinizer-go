package database

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// filterIdentifiableActresses removes actresses from the list that have no
// identifying information and canonicalizes promotional blurbs to Unknown.
// This is the final persistence boundary: even if an upstream scraper or
// translation path misses its cleanup, descriptive text must never create an
// actress row.
func filterIdentifiableActresses(actresses []models.Actress) []models.Actress {
	if len(actresses) == 0 {
		return actresses
	}

	filtered := make([]models.Actress, 0, len(actresses))
	for _, actress := range actresses {
		if models.IsDescriptiveNonName(actress.LastName, actress.FirstName, actress.JapaneseName) {
			actress = models.Actress{
				FirstName:    models.UnknownActressName,
				JapaneseName: models.UnknownActressName,
			}
		}
		if actress.DMMID != 0 ||
			strings.TrimSpace(actress.JapaneseName) != "" ||
			strings.TrimSpace(actress.FirstName) != "" ||
			strings.TrimSpace(actress.LastName) != "" {
			filtered = append(filtered, actress)
		}
	}

	return filtered
}
