package worker

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/spf13/afero"
)

var (
	nfoActorBlockRE   = regexp.MustCompile(`(?is)[\t ]*<actor(?:\s[^>]*)?>.*?</actor>[\t ]*(?:\r?\n)?`)
	nfoClosingMovieRE = regexp.MustCompile(`(?i)</movie\s*>`)
)

func syncMovieNFO(
	ctx context.Context,
	movie *models.Movie,
	cfg *config.Config,
	historyRepo *database.HistoryRepository,
	batchRepo *database.BatchFileOperationRepository,
) (string, error) {
	if movie == nil || cfg == nil {
		return "", nil
	}
	path := discoverSafeNFOPath(ctx, movie, cfg.API.Security.AllowedDirectories, historyRepo, batchRepo)
	if path == "" {
		return "", nil
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read NFO: %w", err)
	}
	if !nfoClosingMovieRE.Match(original) {
		return "", fmt.Errorf("NFO does not contain a closing movie element")
	}
	if err := validateMovieNFOXML(original); err != nil {
		return "", fmt.Errorf("parse NFO: %w", err)
	}

	generator := nfo.NewGenerator(afero.NewOsFs(), nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)))
	generated := generator.MovieToNFO(movie, "")
	actorXML, err := marshalActorBlocks(generated.Actors)
	if err != nil {
		return "", err
	}
	updated := replaceNFOActorBlocks(original, actorXML)
	if bytes.Equal(original, updated) {
		return path, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat NFO: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".actress-sync-*.nfo")
	if err != nil {
		return "", fmt.Errorf("create temporary NFO: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("set temporary NFO permissions: %w", err)
	}
	if _, err := tmp.Write(updated); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write temporary NFO: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("sync temporary NFO: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temporary NFO: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("replace NFO: %w", err)
	}
	if historyRepo != nil {
		_ = historyRepo.Create(ctx, &models.History{
			MovieID: movie.ContentID, Operation: "nfo", OriginalPath: path, NewPath: path,
			Status: models.HistoryStatusSuccess, Metadata: `{"source":"actress_sync","actor_blocks_only":true}`,
		})
	}
	return path, nil
}

func validateMovieNFOXML(content []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	foundRoot := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if start, ok := token.(xml.StartElement); ok && !foundRoot {
			if !strings.EqualFold(start.Name.Local, "movie") {
				return fmt.Errorf("root element is %s, expected movie", start.Name.Local)
			}
			foundRoot = true
		}
	}
	if !foundRoot {
		return fmt.Errorf("movie root element is missing")
	}
	return nil
}

func marshalActorBlocks(actors []nfo.Actor) ([]byte, error) {
	if len(actors) == 0 {
		return nil, nil
	}
	var out bytes.Buffer
	for _, actor := range actors {
		encoded, err := xml.MarshalIndent(struct {
			XMLName xml.Name `xml:"actor"`
			Name    string   `xml:"name"`
			AltName string   `xml:"altname,omitempty"`
			Role    string   `xml:"role,omitempty"`
			Order   int      `xml:"order,omitempty"`
			Thumb   string   `xml:"thumb,omitempty"`
		}{Name: actor.Name, AltName: actor.AltName, Role: actor.Role, Order: actor.Order, Thumb: actor.Thumb}, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal NFO actress: %w", err)
		}
		out.WriteString("  ")
		out.Write(bytes.ReplaceAll(encoded, []byte("\n"), []byte("\n  ")))
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

func replaceNFOActorBlocks(original, actors []byte) []byte {
	locs := nfoActorBlockRE.FindAllIndex(original, -1)
	if len(locs) == 0 {
		closing := nfoClosingMovieRE.FindIndex(original)
		if closing == nil {
			return original
		}
		result := make([]byte, 0, len(original)+len(actors))
		result = append(result, original[:closing[0]]...)
		if len(result) > 0 && result[len(result)-1] != '\n' {
			result = append(result, '\n')
		}
		result = append(result, actors...)
		result = append(result, original[closing[0]:]...)
		return result
	}

	result := make([]byte, 0, len(original)+len(actors))
	last := 0
	for i, loc := range locs {
		result = append(result, original[last:loc[0]]...)
		if i == 0 {
			result = append(result, actors...)
		}
		last = loc[1]
	}
	result = append(result, original[last:]...)
	return result
}

func discoverSafeNFOPath(
	ctx context.Context,
	movie *models.Movie,
	allowedDirs []string,
	historyRepo *database.HistoryRepository,
	batchRepo *database.BatchFileOperationRepository,
) string {
	keys := []string{movie.ContentID}
	if strings.TrimSpace(movie.ID) != "" && movie.ID != movie.ContentID {
		keys = append(keys, movie.ID)
	}
	var candidates []string
	for _, key := range keys {
		if batchRepo != nil {
			if op, err := batchRepo.FindLatestAppliedByMovieID(ctx, key); err == nil {
				candidates = append(candidates, op.NFOPath)
			}
		}
		if historyRepo != nil {
			if entry, err := historyRepo.FindLatestSuccessfulOperation(ctx, key, models.HistoryOperation("nfo")); err == nil {
				candidates = append(candidates, entry.NewPath)
			}
		}
	}
	for _, candidate := range candidates {
		if validated, ok := validateExistingNFOPath(candidate, allowedDirs); ok {
			return validated
		}
	}
	return ""
}

func validateExistingNFOPath(path string, allowedDirs []string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" || len(allowedDirs) == 0 {
		return "", false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	for _, allowed := range allowedDirs {
		allowed = expandActressSyncHome(strings.TrimSpace(allowed))
		allowedAbs, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		allowedResolved, err := filepath.EvalSymlinks(allowedAbs)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(allowedResolved, resolved)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return resolved, true
		}
	}
	return "", false
}

func expandActressSyncHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~"+string(filepath.Separator)) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"+string(filepath.Separator)))
}
