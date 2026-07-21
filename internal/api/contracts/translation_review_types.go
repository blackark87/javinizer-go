package contracts

// TranslationReviewRequest requests a second-pass LLM review of one translated field.
type TranslationReviewRequest struct {
	Field string `json:"field" binding:"required,oneof=title description" example:"title"`
}

// TranslationReviewResponse returns the movie after the reviewed field is persisted.
type TranslationReviewResponse struct {
	Movie *MovieView `json:"movie"`
}
