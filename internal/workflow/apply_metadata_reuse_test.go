package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyOrchestrator_ReusesMetadataBeforeDownload(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourceDir := "/library/old/actress/year"
	sourceVideo := filepath.Join(sourceDir, "ABC-123.mp4")
	require.NoError(t, afero.WriteFile(fs, sourceVideo, []byte("video"), 0644))

	reusedFiles := map[string]string{
		"ABC-123-poster.jpg":  "poster",
		"ABC-123-fanart.jpg":  "fanart",
		"ABC-123-trailer.mp4": "trailer",
	}
	for name, content := range reusedFiles {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(sourceDir, name), []byte(content), 0644))
	}
	reusedExtrafanart := map[string]string{
		"fanart1.jpg": "screenshot-1",
		"fanart2.jpg": "screenshot-2",
	}
	for name, content := range reusedExtrafanart {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(sourceDir, "extrafanart", name), []byte(content), 0644))
	}

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("downloaded"))
	}))
	defer server.Close()

	mediaFormats := organizer.MediaFormatConfig{
		PosterFormat:     "<ID>-poster.jpg",
		FanartFormat:     "<ID>-fanart.jpg",
		TrailerFormat:    "<ID>-trailer.mp4",
		ScreenshotFormat: "fanart<INDEX>.jpg",
		ScreenshotFolder: "extrafanart",
	}
	org := organizer.NewOrganizer(fs, &organizer.Config{
		FolderFormat:      "<ID>",
		FileFormat:        "<ID>",
		RenameFile:        true,
		OperationMode:     operationmode.OperationModeOrganize,
		MediaFormatConfig: mediaFormats,
	}, nil, nil)
	dl := downloader.NewDownloader(server.Client(), fs, &downloader.Config{
		MediaFormatConfig:   mediaFormats,
		DownloadCover:       true,
		DownloadPoster:      true,
		DownloadTrailer:     true,
		DownloadExtrafanart: true,
	}, nil)
	orch := newApplyOrchestrator(
		fs,
		org,
		dl,
		nil,
		nil,
		ApplyConfig{},
		nil,
		noOpRevertLog{},
		nil,
		nil,
	)

	movie := &models.Movie{
		ID:         "ABC-123",
		Title:      "Test",
		TrailerURL: server.URL + "/trailer.mp4",
		Screenshots: []string{
			server.URL + "/fanart1.jpg",
			server.URL + "/fanart2.jpg",
		},
		Poster: models.PosterState{
			CoverURL:         server.URL + "/cover.jpg",
			PosterURL:        server.URL + "/poster.jpg",
			ShouldCropPoster: false,
		},
	}
	result, err := orch.Execute(context.Background(), ApplyCmd{
		Movie: movie,
		Match: models.FileMatchInfo{
			Path:      sourceVideo,
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
			MovieID:   "ABC-123",
		},
		DestPath: "/library/new",
		Organize: OrganizeOptions{
			MoveFiles: true,
		},
		Download:      true,
		OperationMode: operationmode.OperationModeOrganize,
	}, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Zero(t, requests.Load(), "existing metadata should prevent every network download")
	assert.True(t, result.Steps.Downloaded)
	assert.Empty(t, result.DownloadPaths, "reused files are tracked separately from newly downloaded files")
	require.Len(t, result.ReusedMetadataMoves, 5)

	targetDir := "/library/new/ABC-123"
	for name, content := range reusedFiles {
		sourcePath := filepath.Join(sourceDir, name)
		_, statErr := fs.Stat(sourcePath)
		assert.Error(t, statErr)

		data, readErr := afero.ReadFile(fs, filepath.Join(targetDir, name))
		require.NoError(t, readErr)
		assert.Equal(t, content, string(data))
	}
	for name, content := range reusedExtrafanart {
		sourcePath := filepath.Join(sourceDir, "extrafanart", name)
		_, statErr := fs.Stat(sourcePath)
		assert.Error(t, statErr)

		data, readErr := afero.ReadFile(fs, filepath.Join(targetDir, "extrafanart", name))
		require.NoError(t, readErr)
		assert.Equal(t, content, string(data))
	}
	_, statErr := fs.Stat(filepath.Join(sourceDir, "extrafanart"))
	assert.Error(t, statErr, "empty source extrafanart directory should be removed in move mode")
}

func TestBuildApplyGeneratedFilesJSON_TracksReusedMetadata(t *testing.T) {
	result := &ApplyResult{
		NFOPath:       "/dest/ABC-123.nfo",
		DownloadPaths: []string{"/dest/downloaded.jpg"},
		ReusedMetadataMoves: []models.FileMove{
			{OriginalPath: "/source/ABC-123-poster.jpg", NewPath: "/dest/ABC-123-poster.jpg"},
		},
		ReusedMetadataCopies: []string{"/dest/ABC-123-trailer.mp4"},
	}

	raw := buildApplyGeneratedFilesJSON(logging.GlobalLogger(), result, nil)
	var generated models.GeneratedFilesJSON
	require.NoError(t, json.Unmarshal([]byte(raw), &generated))
	assert.Equal(t, []string{
		"/dest/ABC-123.nfo",
		"/dest/downloaded.jpg",
		"/dest/ABC-123-trailer.mp4",
	}, generated.Delete)
	assert.Equal(t, []models.FileMove{
		{OriginalPath: "/source/ABC-123-poster.jpg", NewPath: "/dest/ABC-123-poster.jpg"},
	}, generated.MoveBack)
}
