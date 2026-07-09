package models

// ScrapeCandidate is a lightweight per-scraper summary retained so the UI can let the
// user pick which provider's result to use when providers disagree on the movie.
type ScrapeCandidate struct {
	Source       string `json:"source"`
	MovieID      string `json:"movie_id,omitempty"`
	Title        string `json:"title,omitempty"`
	ActressCount int    `json:"actress_count"`
	PosterURL    string `json:"poster_url,omitempty"`
}
