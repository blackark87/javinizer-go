package contracts

// BatchRetranslateStatus describes the lifecycle of a background job-level
// retranslation.
type BatchRetranslateStatus string

const (
	BatchRetranslateStatusRunning   BatchRetranslateStatus = "running"
	BatchRetranslateStatusCompleted BatchRetranslateStatus = "completed"
	BatchRetranslateStatusCancelled BatchRetranslateStatus = "cancelled"
	BatchRetranslateStatusFailed    BatchRetranslateStatus = "failed"
)

// BatchRetranslateError describes a movie group that could not be retranslated.
type BatchRetranslateError struct {
	MovieID string `json:"movie_id"`
	Error   string `json:"error"`
}

// BatchRetranslateResponse summarizes a background job-level retranslation.
type BatchRetranslateResponse struct {
	JobID     string                  `json:"job_id"`
	Status    BatchRetranslateStatus  `json:"status"`
	Total     int                     `json:"total"`
	Processed int                     `json:"processed"`
	Succeeded int                     `json:"succeeded"`
	Failed    int                     `json:"failed"`
	Errors    []BatchRetranslateError `json:"errors,omitempty"`
}
