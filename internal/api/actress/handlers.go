package actress

import "github.com/javinizer/javinizer-go/internal/api/contracts"

// ErrorResponse keeps custom actress endpoints aligned with the shared API
// response contract while preserving their existing Swagger names.
type ErrorResponse = contracts.ErrorResponse

// models.Actress handlers were split by concern into:
// - handlers_actress_crud.go
// - handlers_actress_merge.go
// - handlers_actress_search.go
