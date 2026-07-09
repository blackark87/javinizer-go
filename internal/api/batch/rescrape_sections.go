package batch

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// rescrapeSections is the set of selectable metadata sections for a section-limited
// re-scrape. ID/ContentID are intentionally not a section — a re-scrape never changes
// the movie identity when sections are limited.
var rescrapeSections = map[string]struct{}{
	"title":     {}, // Title, DisplayTitle, OriginalTitle, Description
	"actresses": {}, // Actresses
	"genres":    {}, // Genres
	"credits":   {}, // Director, Maker, Label, Series
	"rating":    {}, // RatingScore, RatingVotes
	"release":   {}, // ReleaseDate, ReleaseYear, Runtime
	"images":    {}, // PosterURL, CoverURL, CroppedPosterURL, ShouldCropPoster
	"media":     {}, // Screenshots, TrailerURL
}

// restoreUnselectedSections keeps the freshly-scraped values only for the selected
// sections; every other section's fields are restored from the previous movie so a
// re-scrape can update just the chosen parts. ID/ContentID are always preserved.
// A nil/empty selected list means "full rescrape" and is a no-op (caller should not
// invoke it in that case).
func restoreUnselectedSections(newMovie, oldMovie *models.Movie, selected []string) {
	if newMovie == nil || oldMovie == nil || len(selected) == 0 {
		return
	}

	sel := make(map[string]bool, len(selected))
	for _, s := range selected {
		key := strings.ToLower(strings.TrimSpace(s))
		if _, ok := rescrapeSections[key]; ok {
			sel[key] = true
		}
	}

	// Identity is never taken from a section-limited rescrape.
	newMovie.ID = oldMovie.ID
	newMovie.ContentID = oldMovie.ContentID

	if !sel["title"] {
		newMovie.Title = oldMovie.Title
		newMovie.DisplayTitle = oldMovie.DisplayTitle
		newMovie.OriginalTitle = oldMovie.OriginalTitle
		newMovie.Description = oldMovie.Description
	}
	if !sel["actresses"] {
		newMovie.Actresses = oldMovie.Actresses
	}
	if !sel["genres"] {
		newMovie.Genres = oldMovie.Genres
	}
	if !sel["credits"] {
		newMovie.Director = oldMovie.Director
		newMovie.Maker = oldMovie.Maker
		newMovie.Label = oldMovie.Label
		newMovie.Series = oldMovie.Series
	}
	if !sel["rating"] {
		newMovie.RatingScore = oldMovie.RatingScore
		newMovie.RatingVotes = oldMovie.RatingVotes
	}
	if !sel["release"] {
		newMovie.ReleaseDate = oldMovie.ReleaseDate
		newMovie.ReleaseYear = oldMovie.ReleaseYear
		newMovie.Runtime = oldMovie.Runtime
	}
	if !sel["images"] {
		newMovie.PosterURL = oldMovie.PosterURL
		newMovie.CoverURL = oldMovie.CoverURL
		newMovie.CroppedPosterURL = oldMovie.CroppedPosterURL
		newMovie.ShouldCropPoster = oldMovie.ShouldCropPoster
	}
	if !sel["media"] {
		newMovie.Screenshots = oldMovie.Screenshots
		newMovie.TrailerURL = oldMovie.TrailerURL
	}
}
