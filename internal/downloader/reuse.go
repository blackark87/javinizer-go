package downloader

import (
	"context"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
)

// ReuseExistingMetadataCmd describes metadata files that can be reused while
// organizing a video into a different directory.
type ReuseExistingMetadataCmd struct {
	Movie      *models.Movie
	SourceDir  string
	TargetDir  string
	SourcePath string
	Multipart  *MultipartInfo
	MoveFiles  bool
}

// MetadataReuseResult describes one metadata file moved or copied into the
// organized destination. Cover images are represented by MediaTypeCover and
// stored at the configured fanart path, matching the downloader contract.
// Existing extrafanart files are reported individually so move/copy operations
// remain reversible.
type MetadataReuseResult struct {
	Type         MediaType
	OriginalPath string
	NewPath      string
	Moved        bool
	Copied       bool
	Error        error
}

// ExistingMetadataReuser is an optional downloader capability used by the
// workflow before it attempts network downloads.
type ExistingMetadataReuser interface {
	ReuseExistingMetadata(ctx context.Context, cmd ReuseExistingMetadataCmd) []MetadataReuseResult
}

var _ ExistingMetadataReuser = (*Downloader)(nil)

type reusableMetadataTarget struct {
	mediaType MediaType
	target    string
	roles     []string
}

// ReuseExistingMetadata moves existing poster, fanart/cover, trailer, and
// extrafanart files from the video's source directory into the organized
// destination. Copy/link organization modes copy the metadata instead,
// preserving the source tree.
//
// Exact configured filenames are preferred. Legacy "*-cover.jpg" files are
// accepted as the source for the configured fanart destination. A candidate
// must contain the source basename or movie ID, preventing generic files from a
// shared actress folder being reused for the wrong movie.
func (d *Downloader) ReuseExistingMetadata(ctx context.Context, cmd ReuseExistingMetadataCmd) []MetadataReuseResult {
	if d == nil || d.fs == nil || cmd.Movie == nil {
		return nil
	}
	if strings.TrimSpace(cmd.SourceDir) == "" || strings.TrimSpace(cmd.TargetDir) == "" {
		return nil
	}

	sourceDir := filepath.Clean(cmd.SourceDir)
	targetDir := filepath.Clean(cmd.TargetDir)
	if sourceDir == targetDir {
		return nil
	}

	tmplCtx := d.buildTemplateContext(cmd.Movie, cmd.Multipart, cmd.SourcePath)
	posterPath := d.pathResolver.ResolvePosterPath(cmd.Movie, nil, true, tmplCtx, targetDir)
	fanartPath := d.pathResolver.ResolveFanartPath(cmd.Movie, nil, true, tmplCtx, targetDir)

	// Preserve an existing trailer even if the latest scrape no longer has a
	// trailer URL. The configured output name still defaults to .mp4.
	trailerMovie := *cmd.Movie
	if trailerMovie.TrailerURL == "" {
		trailerMovie.TrailerURL = "trailer.mp4"
	}
	trailerPath := d.pathResolver.ResolveTrailerPath(&trailerMovie, true, tmplCtx, targetDir)

	targets := []reusableMetadataTarget{
		{mediaType: MediaTypePoster, target: posterPath, roles: []string{"poster"}},
		{mediaType: MediaTypeCover, target: fanartPath, roles: []string{"fanart", "cover"}},
		{mediaType: MediaTypeTrailer, target: trailerPath, roles: []string{"trailer"}},
	}

	results := make([]MetadataReuseResult, 0, len(targets))

	for _, item := range targets {
		if err := ctx.Err(); err != nil {
			results = append(results, MetadataReuseResult{
				Type:    item.mediaType,
				NewPath: item.target,
				Error:   err,
			})
			break
		}
		if item.target == "" {
			continue
		}
		if _, err := d.fs.Stat(item.target); err == nil {
			continue
		}

		sourcePath := d.findReusableMetadataSource(
			sourceDir,
			filepath.Base(item.target),
			cmd.Movie.ID,
			cmd.SourcePath,
			item.roles,
		)
		if sourcePath == "" {
			continue
		}

		result := MetadataReuseResult{
			Type:         item.mediaType,
			OriginalPath: sourcePath,
			NewPath:      item.target,
		}
		var err error
		if cmd.MoveFiles {
			err = fsutil.MoveFileFs(d.fs, sourcePath, item.target)
			result.Moved = err == nil
		} else {
			err = fsutil.CopyFileFs(d.fs, sourcePath, item.target)
			result.Copied = err == nil
		}
		if err != nil {
			result.Error = err
		}
		results = append(results, result)
	}

	if ctx.Err() == nil {
		results = append(results, d.reuseExistingExtrafanart(ctx, sourceDir, targetDir, cmd.MoveFiles)...)
	}

	return results
}

func (d *Downloader) reuseExistingExtrafanart(
	ctx context.Context,
	sourceDir string,
	targetDir string,
	moveFiles bool,
) []MetadataReuseResult {
	screenshotFolder := strings.TrimSpace(d.config.ScreenshotFolder)
	if screenshotFolder == "" {
		return nil
	}

	sourceExtrafanartDir := filepath.Join(sourceDir, screenshotFolder)
	targetExtrafanartDir := filepath.Join(targetDir, screenshotFolder)
	entries, err := afero.ReadDir(d.fs, sourceExtrafanartDir)
	if err != nil {
		return nil
	}

	results := make([]MetadataReuseResult, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err := ctx.Err(); err != nil {
			results = append(results, MetadataReuseResult{
				Type:         MediaTypeExtrafanart,
				OriginalPath: filepath.Join(sourceExtrafanartDir, entry.Name()),
				NewPath:      filepath.Join(targetExtrafanartDir, entry.Name()),
				Error:        err,
			})
			break
		}

		sourcePath := filepath.Join(sourceExtrafanartDir, entry.Name())
		targetPath := filepath.Join(targetExtrafanartDir, entry.Name())
		if _, err := d.fs.Stat(targetPath); err == nil {
			continue
		}

		result := MetadataReuseResult{
			Type:         MediaTypeExtrafanart,
			OriginalPath: sourcePath,
			NewPath:      targetPath,
		}
		if moveFiles {
			err = fsutil.MoveFileFs(d.fs, sourcePath, targetPath)
			result.Moved = err == nil
		} else {
			err = fsutil.CopyFileFs(d.fs, sourcePath, targetPath)
			result.Copied = err == nil
		}
		if err != nil {
			result.Error = err
		}
		results = append(results, result)
	}
	if moveFiles {
		// Remove the old extrafanart directory only when every direct file was
		// moved and no skipped files or nested directories remain. Revert can
		// recreate it from the individual move records.
		_ = d.fs.Remove(sourceExtrafanartDir)
	}

	return results
}

func (d *Downloader) findReusableMetadataSource(
	sourceDir string,
	targetName string,
	movieID string,
	sourcePath string,
	roles []string,
) string {
	entries, err := afero.ReadDir(d.fs, sourceDir)
	if err != nil {
		return ""
	}

	byLowerName := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		byLowerName[strings.ToLower(entry.Name())] = entry.Name()
	}

	targetExt := strings.ToLower(filepath.Ext(targetName))
	sourceBase := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
	identities := []string{sourceBase, movieID}

	candidates := []string{targetName}
	for _, identity := range identities {
		if identity == "" {
			continue
		}
		for _, role := range roles {
			candidates = append(candidates, identity+"-"+role+targetExt)
		}
	}
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		key := strings.ToLower(candidate)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if actualName, ok := byLowerName[key]; ok {
			return filepath.Join(sourceDir, actualName)
		}
	}

	for _, entry := range entries {
		if entry.IsDir() || !sameReusableMediaExtension(entry.Name(), targetExt) {
			continue
		}
		stem := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		for _, role := range roles {
			prefix, ok := trimReusableMediaRole(stem, role)
			if !ok {
				continue
			}
			if hasReusableMediaIdentityPrefix(prefix, sourceBase) ||
				hasReusableMediaIdentityPrefix(prefix, movieID) {
				return filepath.Join(sourceDir, entry.Name())
			}
		}
	}

	return ""
}

func sameReusableMediaExtension(name, targetExt string) bool {
	sourceExt := strings.ToLower(filepath.Ext(name))
	if sourceExt == targetExt {
		return true
	}
	// JPEG files may use either common extension without changing the content.
	return (sourceExt == ".jpg" || sourceExt == ".jpeg") &&
		(targetExt == ".jpg" || targetExt == ".jpeg")
}

func trimReusableMediaRole(stem, role string) (string, bool) {
	lowerStem := strings.ToLower(stem)
	for _, separator := range []string{"-", "_", " "} {
		suffix := separator + role
		if strings.HasSuffix(lowerStem, suffix) {
			return stem[:len(stem)-len(suffix)], true
		}
	}
	return "", false
}

func hasReusableMediaIdentityPrefix(value, identity string) bool {
	value = strings.TrimSpace(value)
	identity = strings.TrimSpace(identity)
	if value == "" || identity == "" {
		return false
	}
	if strings.EqualFold(value, identity) {
		return true
	}
	if len(value) <= len(identity) || !strings.EqualFold(value[:len(identity)], identity) {
		return false
	}
	next, _ := utf8.DecodeRuneInString(value[len(identity):])
	return !unicode.IsLetter(next) && !unicode.IsDigit(next)
}
