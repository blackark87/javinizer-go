package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/mediainfo"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestApply_OrganizeRegeneratesNFOWithCurrentDisplayTitle(t *testing.T) {
	releaseDate := time.Date(2026, 1, 13, 0, 0, 0, 0, time.UTC)
	engine := workflowPreloadedMediaEngine{
		EngineInterface: template.NewEngine(),
		info:            &mediainfo.VideoInfo{Width: 1920, Height: 1080},
	}
	f := newFixture(t).
		withDisplayTitle("<IF:RELEASEDATE>[<RELEASEDATE:YYYY>]<ELSE>[<YEAR>]</IF><IF:RESOLUTION>[<RESOLUTION>]</IF><TITLE>").
		withTemplateEngine(engine).
		withOrganizer().
		withNFOGenerator().
		withSourceFile("/source/FWAY-088.mp4")

	const oldNFO = `<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>【1080p】MONSTER 세토 칸나</title>
  <originaltitle>MONSTER 瀬戸環奈</originaltitle>
  <sorttitle>FWAY-088</sorttitle>
  <id>FWAY-088</id>
  <year>2026</year>
  <releasedate>2026-01-13</releasedate>
</movie>`
	require.NoError(t, afero.WriteFile(f.fs, "/source/FWAY-088.nfo", []byte(oldNFO), 0o644))

	movie := &models.Movie{
		ID:            "FWAY-088",
		Title:         "MONSTER 세토 칸나",
		OriginalTitle: "MONSTER 瀬戸環奈",
		ReleaseDate:   &releaseDate,
	}
	match := models.FileMatchInfo{
		Path:      "/source/FWAY-088.mp4",
		MovieID:   "FWAY-088",
		Name:      "FWAY-088.mp4",
		Extension: ".mp4",
	}

	result, err := f.build().Apply(context.Background(), ApplyCmd{
		Movie:           movie,
		DisplayTitleSrc: movie,
		Match:           match,
		DestPath:        "/dest",
		Organize:        OrganizeOptions{MoveFiles: true},
		Merge:           MergeOptions{ForceOverwrite: true},
		GenerateNFO:     true,
		Download:        false,
	}, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "[2026][1080p]MONSTER 세토 칸나", result.Movie.DisplayTitle)
	require.NotEmpty(t, result.NFOPath)
	written, err := afero.ReadFile(f.fs, result.NFOPath)
	require.NoError(t, err)
	require.Contains(t, string(written), "<title>[2026][1080p]MONSTER 세토 칸나</title>")
	require.NotContains(t, string(written), "<title>【1080p】MONSTER 세토 칸나</title>")
}
