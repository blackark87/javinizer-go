package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplaceNFOActorBlocksPreservesAllOtherXML(t *testing.T) {
	original := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<movie custom="yes">
  <title>Original title</title>
	  <customtag foo="bar"><nested>keep me</nested></customtag>
  <actor>
    <name>Old One</name>
    <role>Self</role>
  </actor>
  <userrating>8.5</userrating>
  <actor><name>Old Two</name></actor>
	  <uniqueid type="custom">abc</uniqueid>
</movie>`)
	require.NoError(t, validateMovieNFOXML(original))
	actors, err := marshalActorBlocks([]nfo.Actor{{Name: "伊藤舞雪", AltName: "이토 마유키", Thumb: "https://example.com/actress.jpg"}})
	require.NoError(t, err)
	updated := replaceNFOActorBlocks(original, actors)
	result := string(updated)

	assert.Contains(t, result, `<movie custom="yes">`)
	assert.Contains(t, result, `<customtag foo="bar"><nested>keep me</nested></customtag>`)
	assert.Contains(t, result, `<userrating>8.5</userrating>`)
	assert.Contains(t, result, `<uniqueid type="custom">abc</uniqueid>`)
	assert.NotContains(t, result, "Old One")
	assert.NotContains(t, result, "Old Two")
	assert.Contains(t, result, "伊藤舞雪")
	assert.Contains(t, result, "이토 마유키")
	assert.Equal(t, 1, strings.Count(result, "<actor>"))
	require.NoError(t, validateMovieNFOXML(updated))
}

func TestReplaceNFOActorBlocksAddsActorsWithoutCreatingOrRewritingOtherMetadata(t *testing.T) {
	original := []byte("<movie>\n  <title>Keep</title>\n  <custom>unchanged</custom>\n</movie>\n")
	actors, err := marshalActorBlocks([]nfo.Actor{{Name: "Yui Hatano"}})
	require.NoError(t, err)
	updated := string(replaceNFOActorBlocks(original, actors))
	assert.Contains(t, updated, "  <title>Keep</title>\n  <custom>unchanged</custom>\n")
	assert.Contains(t, updated, "<name>Yui Hatano</name>")
	require.NoError(t, validateMovieNFOXML([]byte(updated)))
}

func TestValidateMovieNFOXMLRejectsMalformedOrWrongRoot(t *testing.T) {
	assert.Error(t, validateMovieNFOXML([]byte("<movie><title>broken</movie>")))
	assert.Error(t, validateMovieNFOXML([]byte("<tvshow></tvshow>")))
}

func TestValidateExistingNFOPathRejectsSymlinkEscape(t *testing.T) {
	allowed := filepath.Join(t.TempDir(), "allowed")
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.MkdirAll(allowed, 0o755))
	require.NoError(t, os.MkdirAll(outside, 0o755))
	insideNFO := filepath.Join(allowed, "movie.nfo")
	outsideNFO := filepath.Join(outside, "movie.nfo")
	require.NoError(t, os.WriteFile(insideNFO, []byte("<movie></movie>"), 0o644))
	require.NoError(t, os.WriteFile(outsideNFO, []byte("<movie></movie>"), 0o644))

	validated, ok := validateExistingNFOPath(insideNFO, []string{allowed})
	assert.True(t, ok)
	expectedInside, err := filepath.EvalSymlinks(insideNFO)
	require.NoError(t, err)
	assert.Equal(t, expectedInside, validated)

	symlink := filepath.Join(allowed, "escape.nfo")
	require.NoError(t, os.Symlink(outsideNFO, symlink))
	_, ok = validateExistingNFOPath(symlink, []string{allowed})
	assert.False(t, ok)
}
