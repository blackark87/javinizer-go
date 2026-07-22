package contracts

// TranslationReviewRequest requests a fresh translation followed by a second-pass LLM review.
type TranslationReviewRequest struct {
	Field string `json:"field" binding:"required,oneof=title description" example:"title"`
}

// TranslationReviewResponse returns the movie and whether the reviewed value changed.
type TranslationReviewResponse struct {
	Movie   *MovieView `json:"movie"`
	Changed bool       `json:"changed"`
}
