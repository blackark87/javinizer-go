package downloader

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMetadataReuseDownloader(fs afero.Fs) *Downloader {
	return NewDownloader(nil, fs, &Config{
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat:     "<ID>-poster.jpg",
			FanartFormat:     "<ID>-fanart.jpg",
			TrailerFormat:    "<ID>-trailer.mp4",
			ScreenshotFolder: "extrafanart",
		},
	}, nil)
}

func TestReuseExistingMetadata_MovesConfiguredFiles(t *testing.T) {
	fs := afero.NewMemMapFs()
	d := newMetadataReuseDownloader(fs)
	sourceDir := "/library/old"
	targetDir := "/library/new/ABC-123"

	files := map[string]string{
		"ABC-123-poster.jpg":  "poster",
		"ABC-123-fanart.jpg":  "fanart",
		"ABC-123-trailer.mp4": "trailer",
	}
	for name, content := range files {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(sourceDir, name), []byte(content), 0644))
	}

	results := d.ReuseExistingMetadata(context.Background(), ReuseExistingMetadataCmd{
		Movie:      &models.Movie{ID: "ABC-123"},
		SourceDir:  sourceDir,
		TargetDir:  targetDir,
		SourcePath: filepath.Join(sourceDir, "ABC-123.mp4"),
		MoveFiles:  true,
	})

	require.Len(t, results, 3)
	for _, result := range results {
		assert.True(t, result.Moved)
		assert.False(t, result.Copied)
		assert.NoError(t, result.Error)
		_, err := fs.Stat(result.OriginalPath)
		assert.ErrorIs(t, err, os.ErrNotExist)
	}
	for name, content := range files {
		data, err := afero.ReadFile(fs, filepath.Join(targetDir, name))
		require.NoError(t, err)
		assert.Equal(t, content, string(data))
	}
}

func TestReuseExistingMetadata_UsesLegacyCoverAsFanart(t *testing.T) {
	fs := afero.NewMemMapFs()
	d := newMetadataReuseDownloader(fs)
	sourceDir := "/library/old"
	targetDir := "/library/new/ABC-123"
	legacyCover := filepath.Join(sourceDir, "ABC-123-cover.jpg")
	require.NoError(t, afero.WriteFile(fs, legacyCover, []byte("cover"), 0644))

	results := d.ReuseExistingMetadata(context.Background(), ReuseExistingMetadataCmd{
		Movie:      &models.Movie{ID: "ABC-123"},
		SourceDir:  sourceDir,
		TargetDir:  targetDir,
		SourcePath: filepath.Join(sourceDir, "ABC-123.mp4"),
		MoveFiles:  true,
	})

	require.Len(t, results, 1)
	assert.Equal(t, MediaTypeCover, results[0].Type)
	assert.Equal(t, legacyCover, results[0].OriginalPath)
	assert.Equal(t, filepath.Join(targetDir, "ABC-123-fanart.jpg"), results[0].NewPath)
	assert.True(t, results[0].Moved)
	data, err := afero.ReadFile(fs, results[0].NewPath)
	require.NoError(t, err)
	assert.Equal(t, "cover", string(data))
}

func TestReuseExistingMetadata_CopyModePreservesSource(t *testing.T) {
	fs := afero.NewMemMapFs()
	d := newMetadataReuseDownloader(fs)
	sourceDir := "/library/old"
	targetDir := "/library/new/ABC-123"
	sourcePoster := filepath.Join(sourceDir, "ABC-123-poster.jpg")
	require.NoError(t, afero.WriteFile(fs, sourcePoster, []byte("poster"), 0644))

	results := d.ReuseExistingMetadata(context.Background(), ReuseExistingMetadataCmd{
		Movie:      &models.Movie{ID: "ABC-123"},
		SourceDir:  sourceDir,
		TargetDir:  targetDir,
		SourcePath: filepath.Join(sourceDir, "ABC-123.mp4"),
		MoveFiles:  false,
	})

	require.Len(t, results, 1)
	assert.False(t, results[0].Moved)
	assert.True(t, results[0].Copied)
	_, err := fs.Stat(sourcePoster)
	require.NoError(t, err)
	data, err := afero.ReadFile(fs, filepath.Join(targetDir, "ABC-123-poster.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "poster", string(data))
}

func TestReuseExistingMetadata_DoesNotUseGenericFiles(t *testing.T) {
	fs := afero.NewMemMapFs()
	d := newMetadataReuseDownloader(fs)
	sourceDir := "/library/actress"
	targetDir := "/library/new/ABC-123"
	require.NoError(t, afero.WriteFile(fs, filepath.Join(sourceDir, "OTHER-999.mp4"), []byte("video"), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(sourceDir, "poster.jpg"), []byte("wrong poster"), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(sourceDir, "fanart.jpg"), []byte("wrong fanart"), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(sourceDir, "trailer.mp4"), []byte("wrong trailer"), 0644))

	results := d.ReuseExistingMetadata(context.Background(), ReuseExistingMetadataCmd{
		Movie:      &models.Movie{ID: "ABC-123"},
		SourceDir:  sourceDir,
		TargetDir:  targetDir,
		SourcePath: filepath.Join(sourceDir, "ABC-123.mp4"),
		MoveFiles:  true,
	})

	assert.Empty(t, results)
	_, err := fs.Stat(filepath.Join(sourceDir, "poster.jpg"))
	require.NoError(t, err)
}

func TestReuseExistingMetadata_FindsOldTitleBasedName(t *testing.T) {
	fs := afero.NewMemMapFs()
	d := newMetadataReuseDownloader(fs)
	sourceDir := "/library/old"
	targetDir := "/library/new/ABC-123"
	oldPoster := filepath.Join(sourceDir, "ABC-123 - 이전 제목-poster.jpg")
	require.NoError(t, afero.WriteFile(fs, oldPoster, []byte("poster"), 0644))

	results := d.ReuseExistingMetadata(context.Background(), ReuseExistingMetadataCmd{
		Movie:      &models.Movie{ID: "ABC-123", Title: "새 제목"},
		SourceDir:  sourceDir,
		TargetDir:  targetDir,
		SourcePath: filepath.Join(sourceDir, "ABC-123.mp4"),
		MoveFiles:  true,
	})

	require.Len(t, results, 1)
	assert.Equal(t, oldPoster, results[0].OriginalPath)
	assert.Equal(t, filepath.Join(targetDir, "ABC-123-poster.jpg"), results[0].NewPath)
	assert.True(t, results[0].Moved)
}

func TestReuseExistingMetadata_DestinationWinsWithoutOverwriting(t *testing.T) {
	fs := afero.NewMemMapFs()
	d := newMetadataReuseDownloader(fs)
	sourceDir := "/library/old"
	targetDir := "/library/new/ABC-123"
	sourcePoster := filepath.Join(sourceDir, "ABC-123-poster.jpg")
	targetPoster := filepath.Join(targetDir, "ABC-123-poster.jpg")
	require.NoError(t, afero.WriteFile(fs, sourcePoster, []byte("source"), 0644))
	require.NoError(t, afero.WriteFile(fs, targetPoster, []byte("destination"), 0644))

	results := d.ReuseExistingMetadata(context.Background(), ReuseExistingMetadataCmd{
		Movie:      &models.Movie{ID: "ABC-123"},
		SourceDir:  sourceDir,
		TargetDir:  targetDir,
		SourcePath: filepath.Join(sourceDir, "ABC-123.mp4"),
		MoveFiles:  true,
	})

	assert.Empty(t, results)
	sourceData, err := afero.ReadFile(fs, sourcePoster)
	require.NoError(t, err)
	assert.Equal(t, "source", string(sourceData))
	targetData, err := afero.ReadFile(fs, targetPoster)
	require.NoError(t, err)
	assert.Equal(t, "destination", string(targetData))
}

func TestReuseExistingMetadata_MovesExtrafanartWithoutOverwriting(t *testing.T) {
	fs := afero.NewMemMapFs()
	d := newMetadataReuseDownloader(fs)
	sourceDir := "/library/old"
	targetDir := "/library/new/ABC-123"
	sourceExtrafanartDir := filepath.Join(sourceDir, "extrafanart")
	targetExtrafanartDir := filepath.Join(targetDir, "extrafanart")

	sourceFanart1 := filepath.Join(sourceExtrafanartDir, "fanart1.jpg")
	sourceFanart2 := filepath.Join(sourceExtrafanartDir, "fanart2.jpg")
	targetFanart2 := filepath.Join(targetExtrafanartDir, "fanart2.jpg")
	require.NoError(t, afero.WriteFile(fs, sourceFanart1, []byte("source-1"), 0644))
	require.NoError(t, afero.WriteFile(fs, sourceFanart2, []byte("source-2"), 0644))
	require.NoError(t, afero.WriteFile(fs, targetFanart2, []byte("destination-2"), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(sourceExtrafanartDir, "@eaDir", "thumbnail.jpg"), []byte("thumbnail"), 0644))

	results := d.ReuseExistingMetadata(context.Background(), ReuseExistingMetadataCmd{
		Movie:      &models.Movie{ID: "ABC-123"},
		SourceDir:  sourceDir,
		TargetDir:  targetDir,
		SourcePath: filepath.Join(sourceDir, "ABC-123.mp4"),
		MoveFiles:  true,
	})

	require.Len(t, results, 1)
	assert.Equal(t, MediaTypeExtrafanart, results[0].Type)
	assert.Equal(t, sourceFanart1, results[0].OriginalPath)
	assert.Equal(t, filepath.Join(targetExtrafanartDir, "fanart1.jpg"), results[0].NewPath)
	assert.True(t, results[0].Moved)
	assert.False(t, results[0].Copied)
	assert.NoError(t, results[0].Error)

	_, err := fs.Stat(sourceFanart1)
	assert.ErrorIs(t, err, os.ErrNotExist)
	sourceFanart2Data, err := afero.ReadFile(fs, sourceFanart2)
	require.NoError(t, err)
	assert.Equal(t, "source-2", string(sourceFanart2Data))
	targetFanart2Data, err := afero.ReadFile(fs, targetFanart2)
	require.NoError(t, err)
	assert.Equal(t, "destination-2", string(targetFanart2Data))
	_, err = fs.Stat(filepath.Join(targetExtrafanartDir, "@eaDir", "thumbnail.jpg"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestReuseExistingMetadata_CopiesExtrafanartAndPreservesSource(t *testing.T) {
	fs := afero.NewMemMapFs()
	d := newMetadataReuseDownloader(fs)
	sourceDir := "/library/old"
	targetDir := "/library/new/ABC-123"
	sourceScreenshot := filepath.Join(sourceDir, "extrafanart", "fanart1.jpg")
	targetScreenshot := filepath.Join(targetDir, "extrafanart", "fanart1.jpg")
	require.NoError(t, afero.WriteFile(fs, sourceScreenshot, []byte("screenshot"), 0644))

	results := d.ReuseExistingMetadata(context.Background(), ReuseExistingMetadataCmd{
		Movie:      &models.Movie{ID: "ABC-123"},
		SourceDir:  sourceDir,
		TargetDir:  targetDir,
		SourcePath: filepath.Join(sourceDir, "ABC-123.mp4"),
		MoveFiles:  false,
	})

	require.Len(t, results, 1)
	assert.Equal(t, MediaTypeExtrafanart, results[0].Type)
	assert.False(t, results[0].Moved)
	assert.True(t, results[0].Copied)
	assert.NoError(t, results[0].Error)

	sourceData, err := afero.ReadFile(fs, sourceScreenshot)
	require.NoError(t, err)
	assert.Equal(t, "screenshot", string(sourceData))
	targetData, err := afero.ReadFile(fs, targetScreenshot)
	require.NoError(t, err)
	assert.Equal(t, "screenshot", string(targetData))
}
