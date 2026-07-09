package models

// ScrapeCandidate is a lightweight per-scraper summary retained so the UI can let the
// user pick which provider's result to use when providers disagree on the movie.
type ScrapeCandidate struct {
	Source string `json:"source"`
	// MovieID is the scraper-reported id for this candidate.
	MovieID string `json:"movie_id,omitempty"`
	// Title is the display title — translated when translation is enabled, otherwise
	// the scraper's original title. OriginalTitle always holds the untranslated title.
	Title         string `json:"title,omitempty"`
	OriginalTitle string `json:"original_title,omitempty"`
	ActressCount  int    `json:"actress_count"`
	PosterURL     string `json:"poster_url,omitempty"`
}
