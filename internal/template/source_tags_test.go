package template

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/mediainfo"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceAndMediaInfoTags(t *testing.T) {
	ctx := &Context{ID: "ABC-123"}
	ctx.SetSourceFile("/library/collection/movie/ABC-123.mkv", "", "")
	ctx.SetMediaInfo(&mediainfo.VideoInfo{Width: 3840, Height: 1920})

	assert.Equal(t, "/library/collection/movie/ABC-123.mkv", ctx.SourcePath)
	assert.Equal(t, "/library/collection/movie", ctx.SourceDir)
	assert.Equal(t, "movie", ctx.SourceFolder)
	assert.Equal(t, "collection", ctx.SourceParent)
	assert.Equal(t, "ABC-123.mkv", ctx.SourceFile)
	assert.Equal(t, "ABC-123", ctx.SourceFilename)
	assert.Equal(t, ".mkv", ctx.SourceExtension)
	assert.Equal(t, ctx.SourcePath, ctx.VideoFilePath)

	got, err := NewEngine().Execute(
		"<SOURCEPATH>|<SOURCEDIR>|<SOURCEFOLDER>|<SOURCEPARENT>|<SOURCEFILE>|<SOURCEFILENAME>|<SOURCEEXT>|<RESOLUTION>|<IF:VR>VR</IF>",
		ctx,
	)
	require.NoError(t, err)
	assert.Equal(t, "/library/collection/movie/ABC-123.mkv|/library/collection/movie|movie|collection|ABC-123.mkv|ABC-123|.mkv|4K|VR", got)
}

func TestContextCloneDeepCopiesTranslatedActresses(t *testing.T) {
	ctx := &Context{Translations: map[string]models.MovieTranslation{
		"ko": {Language: "ko", Actresses: []string{"배우 한글명"}},
	}}

	clone := ctx.Clone()
	translation := clone.Translations["ko"]
	translation.Actresses[0] = "변경됨"
	clone.Translations["ko"] = translation

	assert.Equal(t, "배우 한글명", ctx.Translations["ko"].Actresses[0])
}
