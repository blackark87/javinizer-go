package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartApply_ResumeAllowsCancelledJob(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-779"}},
	}
	job := newBatchJob([]string{"/source/IPX-779.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       wf,
			BatchCfg: BatchJobConfig{MaxWorkers: 1},
		},
	})
	job.results.UpdateFileResult("/source/IPX-779.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-779.mp4", MovieID: "IPX-779"},
		Status:        models.JobStatusCancelled,
		Movie:         &models.Movie{ID: "IPX-779"},
	})
	job.controller.SetJobStatus(models.JobStatusCancelled)

	err := job.Controller().StartApply(context.Background(), ApplyPhaseConfig{
		Destination: "/output",
		Resume:      true,
	})
	require.NoError(t, err)
	require.NoError(t, job.Controller().Wait())
	assert.Equal(t, 1, wf.getApplyCalled())
	assert.Equal(t, models.JobStatusOrganized, job.lifecycle.GetJobStatus())
}

func TestStartApply_RejectsCancelledJobWithoutResume(t *testing.T) {
	job := newBatchJob(nil, &JobConfig{
		BatchJobDeps: BatchJobDeps{WF: &stubApplyWorkflow{}},
	})
	job.controller.SetJobStatus(models.JobStatusCancelled)

	err := job.Controller().StartApply(context.Background(), ApplyPhaseConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected status")
}
