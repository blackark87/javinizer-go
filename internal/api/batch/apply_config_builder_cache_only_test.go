package batch

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusUsesCacheOnly(t *testing.T) {
	assert.False(t, statusUsesCacheOnly(nil))
	assert.False(t, statusUsesCacheOnly(&worker.BatchJobStatus{}))
	assert.False(t, statusUsesCacheOnly(&worker.BatchJobStatus{
		Results: map[string]*worker.MovieResult{
			"/media/ordinary.mp4": {CacheOnly: false},
		},
	}))
	assert.True(t, statusUsesCacheOnly(&worker.BatchJobStatus{
		Results: map[string]*worker.MovieResult{
			"/media/FWAY-088.mp4": {CacheOnly: true},
		},
	}))
}

func TestResolveApplyConfig_CacheOnlyDisablesDownloads(t *testing.T) {
	rt := core.NewAPIRuntime(nil)
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	job := &stubControlledJob{status: &worker.BatchJobStatus{
		Results: map[string]*worker.MovieResult{
			"/media/FWAY-088.mp4": {CacheOnly: true},
		},
	}}

	organizeOpts, err := resolveOrganizeApplyConfig(
		core.NewSnapshotForTesting(rt, core.APIConfig{}),
		factory,
		job,
		contracts.OrganizeRequest{OperationMode: string(operationmode.OperationModeInPlace)},
	)
	require.NoError(t, err)
	assert.False(t, organizeOpts.Download)

	updateOpts, err := resolveUpdateApplyConfig(
		core.NewSnapshotForTesting(rt, core.APIConfig{}),
		factory,
		job,
		contracts.UpdateRequest{},
	)
	require.NoError(t, err)
	assert.False(t, updateOpts.Download)
}
