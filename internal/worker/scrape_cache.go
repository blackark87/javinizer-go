package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	httpclientiface "github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
)

func handleCacheHit(
	ctx context.Context,
	job *BatchJob,
	filePath string,
	fileIndex int,
	movieID string,
	matchResultPtr *matcher.MatchResult,
	movieRepo *database.MovieRepository,
	actressRepo *database.ActressRepository,
	registry *models.ScraperRegistry,
	httpClient httpclientiface.HTTPClient,
	userAgent string,
	referer string,
	processedMovieIDs map[string]bool,
	cfg *config.Config,
	updateMode bool,
	scalarStrategy string,
	arrayStrategy string,
	startTime time.Time,
) (*models.Movie, *FileResult, error) {
	cached, err := movieRepo.FindByID(movieID)
	if err != nil {
		if !database.IsNotFound(err) {
			return nil, nil, fmt.Errorf("cache lookup failed for %s: %w", movieID, err)
		}
		logging.Debugf("[Batch %s] File %d: %s not found in cache, will scrape", job.ID, fileIndex, movieID)
		return nil, nil, nil
	}

	logging.Debugf("[Batch %s] File %d: Found %s in cache (Title=%s, Maker=%s)",
		job.ID, fileIndex, movieID, cached.Title, cached.Maker)
	if verified, verifyErr := verifyCachedActresses(ctx, job, fileIndex, cached, movieRepo, actressRepo, registry, cfg); verifyErr != nil {
		return nil, nil, verifyErr
	} else if verified != nil {
		cached = verified
	}

	var posterErr *string
	if httpClient != nil {
		posterErr = generateCachedPoster(ctx, job, fileIndex, cached, httpClient, userAgent, referer, processedMovieIDs, cfg)
	}

	movieToReturn := cached
	fieldSources := buildFieldSourcesFromCachedMovie(cached)
	actressSources := buildActressSourcesFromCachedMovie(cached)
	displayTitleApplied := false

	if updateMode && cfg != nil {
		movieToReturn, fieldSources, actressSources, displayTitleApplied = mergeCachedNFO(
			ctx, job, fileIndex, filePath, cached, matchResultPtr, cfg,
			scalarStrategy, arrayStrategy, fieldSources, actressSources,
		)
	}

	if actressRepo != nil {
		if enriched := EnrichActressesFromDB(movieToReturn, actressRepo, cfg); enriched > 0 {
			logging.Debugf("[Batch %s] File %d: Enriched %d actresses from database after cache hit", job.ID, fileIndex, enriched)
		}
	}

	if !displayTitleApplied {
		applyDisplayTitleWithSource(ctx, job, cfg, movieToReturn, cached, filePath)
	}

	now := time.Now()
	fileResult := &FileResult{
		FilePath:       filePath,
		MovieID:        movieID,
		Status:         JobStatusCompleted,
		Data:           movieToReturn,
		FieldSources:   fieldSources,
		ActressSources: actressSources,
		PosterError:    posterErr,
		StartedAt:      startTime,
		EndedAt:        &now,
	}

	if matchResultPtr != nil {
		fileResult.IsMultiPart = matchResultPtr.IsMultiPart
		fileResult.PartNumber = matchResultPtr.PartNumber
		fileResult.PartSuffix = matchResultPtr.PartSuffix
	}

	return movieToReturn, fileResult, nil
}

func verifyCachedActresses(
	ctx context.Context,
	job *BatchJob,
	fileIndex int,
	cached *models.Movie,
	movieRepo *database.MovieRepository,
	actressRepo *database.ActressRepository,
	registry *models.ScraperRegistry,
	cfg *config.Config,
) (*models.Movie, error) {
	if cached == nil || registry == nil {
		return nil, nil
	}
	priority := []string(nil)
	if cfg != nil {
		priority = cfg.Scrapers.Priority
	}
	_, resolvers := partitionActressResolvers(registry.GetByPriority(priority))
	if len(resolvers) == 0 {
		return nil, nil
	}
	queryID := cached.ID
	if queryID == "" {
		queryID = cached.ContentID
	}
	for _, scraper := range resolvers {
		resolver := scraper.(models.ActressResolver)
		resolved, err := safeResolveActresses(ctx, resolver, queryID)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			logging.Warnf(
				"[Batch %s] File %d: Cached actress verification failed (movie=%s resolver=%s existing_cast=%s): %v; cached mappings were preserved",
				job.ID, fileIndex, queryID, scraper.Name(), cachedActressSummary(cached.Actresses), err,
			)
			continue
		}
		if !hasVerifiedActresses(resolved) {
			logging.Warnf(
				"[Batch %s] File %d: Cached actress verification returned no canonical cast (movie=%s resolver=%s existing_cast=%s); cached mappings were preserved",
				job.ID, fileIndex, queryID, scraper.Name(), cachedActressSummary(cached.Actresses),
			)
			continue
		}

		thumbnailResolver := findActressThumbnailResolver(registry)
		verifiedMovie := *cached
		verifiedMovie.Actresses = make([]models.Actress, 0, len(resolved.Actresses))
		for _, info := range resolved.Actresses {
			if info.ThumbURL == "" && thumbnailResolver != nil {
				info.ThumbURL = safeResolveActressThumbnail(ctx, thumbnailResolver, info)
			}
			verifiedMovie.Actresses = append(verifiedMovie.Actresses, actressModelFromInfo(info))
		}
		identityRepo := actressRepo
		if identityRepo == nil {
			identityRepo = database.NewActressRepository(movieRepo.GetDB())
		}
		if err := reconcileVerifiedMovieActresses(&verifiedMovie, identityRepo); err != nil {
			return nil, fmt.Errorf("cached actress verification failed for %s via %s: %w", queryID, scraper.Name(), err)
		}
		saved, err := movieRepo.Upsert(&verifiedMovie)
		if err != nil {
			return nil, fmt.Errorf("save verified cached actresses for %s: %w", queryID, err)
		}
		return saved, nil
	}
	return nil, nil
}

func cachedActressSummary(actresses []models.Actress) string {
	if len(actresses) == 0 {
		return "[]"
	}
	values := make([]string, 0, len(actresses))
	for _, actress := range actresses {
		values = append(values, fmt.Sprintf("#%d:%s(dmm=%d)", actress.ID, actress.FullName(), actress.DMMID))
	}
	return "[" + strings.Join(values, ", ") + "]"
}

func generateCachedPoster(
	ctx context.Context,
	job *BatchJob,
	fileIndex int,
	cached *models.Movie,
	httpClient httpclientiface.HTTPClient,
	userAgent string,
	referer string,
	processedMovieIDs map[string]bool,
	cfg *config.Config,
) *string {
	shouldGenerate := true

	if processedMovieIDs != nil {
		processedMovieIDsMutex.Lock()
		shouldGenerate = !processedMovieIDs[cached.ID]
		if shouldGenerate {
			processedMovieIDs[cached.ID] = true
		}
		processedMovieIDsMutex.Unlock()
	}

	if shouldGenerate {
		tempPosterURL, err := GenerateTempPoster(ctx, job.ID, cached, httpClient, userAgent, referer, downloader.ResolveMediaReferer, cfg.System.TempDir)
		if err != nil {
			logging.Warnf("[Batch %s] File %d: Failed to create temp poster for cached movie: %v", job.ID, fileIndex, err)
			errMsg := err.Error()
			return &errMsg
		}
		cached.CroppedPosterURL = tempPosterURL
		return nil
	}

	tempPosterPath := filepath.Join(cfg.System.TempDir, "posters", job.ID, cached.ID+".jpg")
	if _, err := os.Stat(tempPosterPath); err != nil {
		logging.Debugf("[Batch %s] File %d: Temp poster missing for %s, regenerating", job.ID, fileIndex, cached.ID)
		tempPosterURL, err := GenerateTempPoster(ctx, job.ID, cached, httpClient, userAgent, referer, downloader.ResolveMediaReferer, cfg.System.TempDir)
		if err != nil {
			logging.Warnf("[Batch %s] File %d: Failed to regenerate temp poster: %v", job.ID, fileIndex, err)
			errMsg := err.Error()
			return &errMsg
		}
		cached.CroppedPosterURL = tempPosterURL
	} else {
		cached.CroppedPosterURL = fmt.Sprintf("/api/v1/temp/posters/%s/%s.jpg", job.ID, cached.ID)
	}
	return nil
}

func clearCacheIfForced(
	job *BatchJob,
	fileIndex int,
	movieID string,
	force bool,
	movieRepo *database.MovieRepository,
) {
	if !force {
		return
	}
	logging.Debugf("[Batch %s] File %d: Force refresh enabled, clearing cache for %s", job.ID, fileIndex, movieID)
	if err := movieRepo.Delete(movieID); err != nil {
		logging.Debugf("[Batch %s] File %d: Cache delete failed (movie may not exist): %v", job.ID, fileIndex, err)
	}
}
