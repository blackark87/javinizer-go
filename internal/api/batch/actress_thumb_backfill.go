package batch

import (
	"context"
	"strings"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// backfillActressThumb fills an empty display thumbnail from the actress DB.
// It never overwrites a scraper-provided thumbnail or mutates database state.
func backfillActressThumb(actress *models.Actress, repo database.ActressRepositoryInterface) {
	if actress == nil || repo == nil || strings.TrimSpace(actress.ThumbURL) != "" ||
		models.IsUnknownActressFields(actress.LastName, actress.FirstName, actress.JapaneseName) {
		return
	}
	ctx := context.Background()
	var found *models.Actress
	if actress.DMMID > 0 {
		found, _ = repo.FindByDMMID(ctx, actress.DMMID)
	}
	if found == nil && strings.TrimSpace(actress.JapaneseName) != "" {
		found, _ = repo.FindByJapaneseName(ctx, strings.TrimSpace(actress.JapaneseName))
	}
	if found == nil && (strings.TrimSpace(actress.FirstName) != "" || strings.TrimSpace(actress.LastName) != "") {
		found, _ = repo.FindByFirstNameLastName(ctx, actress.FirstName, actress.LastName)
	}
	if found != nil && strings.TrimSpace(found.ThumbURL) != "" {
		actress.ThumbURL = found.ThumbURL
	}
}

func movieWithBackfilledActressThumbs(movie *models.Movie, repos ...database.ActressRepositoryInterface) *models.Movie {
	if movie == nil || len(repos) == 0 || repos[0] == nil || len(movie.Actresses) == 0 {
		return movie
	}
	copy := movie.Clone()
	for i := range copy.Actresses {
		backfillActressThumb(&copy.Actresses[i], repos[0])
	}
	return copy
}
