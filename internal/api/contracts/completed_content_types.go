package contracts

import "github.com/javinizer/javinizer-go/internal/models"

// CompletedContentItem represents one movie with its currently applied
// organized destination paths.
type CompletedContentItem struct {
	MovieID                  string           `json:"movie_id" example:"MIUM-985"`
	ContentID                string           `json:"content_id,omitempty" example:"300mium00985"`
	DisplayTitle             string           `json:"display_title,omitempty"`
	Title                    string           `json:"title,omitempty"`
	OriginalTitle            string           `json:"original_title,omitempty"`
	PosterURL                string           `json:"poster_url,omitempty"`
	CoverURL                 string           `json:"cover_url,omitempty"`
	CroppedPosterURL         string           `json:"cropped_poster_url,omitempty"`
	OriginalPosterURL        string           `json:"original_poster_url,omitempty"`
	OriginalCroppedPosterURL string           `json:"original_cropped_poster_url,omitempty"`
	OriginalCoverURL         string           `json:"original_cover_url,omitempty"`
	Actresses                []models.Actress `json:"actresses"`
	Paths                    []string         `json:"paths" example:"/media/MIUM-985/MIUM-985.mp4"`
	LatestJobID              string           `json:"latest_job_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	OrganizedAt              string           `json:"organized_at" example:"2026-07-26T12:00:00Z"`
}

// CompletedContentListResponse is a paginated completed-content response.
type CompletedContentListResponse struct {
	Contents []CompletedContentItem `json:"contents"`
	Total    int64                  `json:"total" example:"42"`
	Limit    int                    `json:"limit" example:"20"`
	Offset   int                    `json:"offset" example:"0"`
}
