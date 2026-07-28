package worker

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsTranslationFailure(t *testing.T) {
	movie := &models.Movie{ID: "IPX-535"}
	tests := []struct {
		name   string
		result *MovieResult
		want   bool
	}{
		{
			name: "deferred translation failure",
			result: &MovieResult{
				Status: models.JobStatusFailed,
				Error:  "translation stage failed: invalid output",
				Movie:  movie,
			},
			want: true,
		},
		{
			name: "legacy cached translation failure",
			result: &MovieResult{
				Status: models.JobStatusFailed,
				Error:  "translation failed for cached IPX-535: provider unavailable",
				Movie:  movie,
			},
			want: true,
		},
		{
			name: "translation persistence failure is not recoverable",
			result: &MovieResult{
				Status: models.JobStatusFailed,
				Error:  "translation checkpoint persistence failed",
				Movie:  movie,
			},
		},
		{
			name: "scrape failure",
			result: &MovieResult{
				Status: models.JobStatusFailed,
				Error:  "scrape failed: no metadata",
				Movie:  movie,
			},
		},
		{
			name: "missing retained movie",
			result: &MovieResult{
				Status: models.JobStatusFailed,
				Error:  "translation stage failed: invalid output",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsTranslationFailure(tc.result))
		})
	}
}

func TestJobEditorResolveTranslationFailure(t *testing.T) {
	store := NewJobStore(nil, nil, nil, "", nil, nil)
	job := store.CreateJobBatch([]string{"/media/IPX-535.mp4"})
	resultID := "translation-failure"
	warning := "old warning"
	job.results.UpdateFileResult("/media/IPX-535.mp4", &MovieResult{
		ResultID: resultID,
		FileMatchInfo: models.FileMatchInfo{
			Path:    "/media/IPX-535.mp4",
			MovieID: "IPX-535",
		},
		Status:             models.JobStatusFailed,
		Error:              "translation stage failed: invalid title",
		Movie:              &models.Movie{ID: "IPX-535", Title: "번역 제목"},
		OrchestrationState: models.OrchestrationState{TranslationWarning: &warning},
		StartedAt:          time.Now(),
	})

	controlled, ok := store.GetBatchJob(job.GetID())
	require.True(t, ok)
	updated, err := controlled.ResolveTranslationFailure(resultID)

	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, models.JobStatusCompleted, updated.Status)
	assert.Empty(t, updated.Error)
	assert.Nil(t, updated.TranslationWarning)
	status := controlled.GetStatus()
	assert.Equal(t, 1, status.Completed)
	assert.Zero(t, status.Failed)

	_, err = controlled.ResolveTranslationFailure(resultID)
	assert.ErrorContains(t, err, "not a recoverable translation failure")
}
