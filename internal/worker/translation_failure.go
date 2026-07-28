package worker

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// IsTranslationFailure reports whether a failed result can be recovered from
// its retained movie and scraper-source metadata without scraping again.
func IsTranslationFailure(result *MovieResult) bool {
	if result == nil || result.Status != models.JobStatusFailed || result.Movie == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(result.Error))
	return strings.HasPrefix(message, "translation stage failed:") ||
		strings.HasPrefix(message, "translation failed:") ||
		strings.HasPrefix(message, "translation failed for ")
}
