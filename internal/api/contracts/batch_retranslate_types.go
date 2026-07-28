package contracts

// BatchRetranslateError describes a movie group that could not be retranslated.
type BatchRetranslateError struct {
	MovieID string `json:"movie_id"`
	Error   string `json:"error"`
}

// BatchRetranslateResponse summarizes a job-level retranslation request.
type BatchRetranslateResponse struct {
	JobID     string                  `json:"job_id"`
	Total     int                     `json:"total"`
	Succeeded int                     `json:"succeeded"`
	Failed    int                     `json:"failed"`
	Errors    []BatchRetranslateError `json:"errors,omitempty"`
}
