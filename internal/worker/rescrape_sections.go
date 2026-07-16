package worker

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

var rescrapeSections = map[string]struct{}{
	"title":     {},
	"actresses": {},
	"genres":    {},
	"credits":   {},
	"rating":    {},
	"release":   {},
	"images":    {},
	"media":     {},
}

// rescrapeSectionSelected reports whether a section should be refreshed.
// An empty selection means a full rescrape, so every section is selected.
func rescrapeSectionSelected(selected []string, section string) bool {
	if len(selected) == 0 {
		return true
	}
	section = strings.ToLower(strings.TrimSpace(section))
	for _, value := range selected {
		if strings.ToLower(strings.TrimSpace(value)) == section {
			return true
		}
	}
	return false
}

// restoreUnselectedRescrapeSections keeps freshly scraped values only for the
// selected sections. Movie identity always remains the existing identity.
func restoreUnselectedRescrapeSections(newMovie, oldMovie *models.Movie, selected []string) {
	if newMovie == nil || oldMovie == nil || len(selected) == 0 {
		return
	}

	sel := make(map[string]bool, len(selected))
	for _, value := range selected {
		key := strings.ToLower(strings.TrimSpace(value))
		if _, ok := rescrapeSections[key]; ok {
			sel[key] = true
		}
	}

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
		newMovie.Poster = oldMovie.Poster.Clone()
	}
	if !sel["media"] {
		newMovie.Screenshots = oldMovie.Screenshots
		newMovie.TrailerURL = oldMovie.TrailerURL
	}

	restoreUnselectedRescrapeTranslations(newMovie, oldMovie, sel)
}

func restoreUnselectedRescrapeTranslations(newMovie, oldMovie *models.Movie, selected map[string]bool) {
	if selected["title"] || selected["credits"] || selected["actresses"] {
		oldByLanguage := make(map[string]*models.MovieTranslation, len(oldMovie.Translations))
		for i := range oldMovie.Translations {
			oldByLanguage[oldMovie.Translations[i].Language] = &oldMovie.Translations[i]
		}
		for i := range newMovie.Translations {
			translated := &newMovie.Translations[i]
			existing := oldByLanguage[translated.Language]
			if !selected["title"] {
				translated.Title, translated.OriginalTitle, translated.Description = pickRescrapeTranslation3(existing, func(old *models.MovieTranslation) (string, string, string) {
					return old.Title, old.OriginalTitle, old.Description
				})
			}
			if !selected["credits"] {
				translated.Director, translated.Maker, translated.Label, translated.Series = pickRescrapeTranslation4(existing, func(old *models.MovieTranslation) (string, string, string, string) {
					return old.Director, old.Maker, old.Label, old.Series
				})
			}
			if !selected["actresses"] {
				if existing != nil {
					translated.Actresses = existing.Actresses
				} else {
					translated.Actresses = nil
				}
			}
		}
		return
	}

	newMovie.Translations = oldMovie.Translations
}

func pickRescrapeTranslation3(old *models.MovieTranslation, pick func(*models.MovieTranslation) (string, string, string)) (string, string, string) {
	if old == nil {
		return "", "", ""
	}
	return pick(old)
}

func pickRescrapeTranslation4(old *models.MovieTranslation, pick func(*models.MovieTranslation) (string, string, string, string)) (string, string, string, string) {
	if old == nil {
		return "", "", "", ""
	}
	return pick(old)
}

func applyRescrapeSectionMask(newMovie, oldMovie *models.Movie, selected []string) bool {
	if len(selected) == 0 || newMovie == nil || oldMovie == nil {
		return false
	}
	restoreUnselectedRescrapeSections(newMovie, oldMovie, selected)
	return true
}
