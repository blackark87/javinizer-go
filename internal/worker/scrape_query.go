package worker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scanner"
)

type scrapeQueryResult struct {
	movieID             string
	rawFilenameQuery    string
	resolvedID          string
	matchResultPtr      *matcher.MatchResult
	matcherMissFallback bool
}

func resolveScrapeQuery(
	ctx context.Context,
	job *BatchJob,
	filePath string,
	fileIndex int,
	queryOverride string,
	registry *models.ScraperRegistry,
	fileMatcher *matcher.Matcher,
	startTime time.Time,
) (*scrapeQueryResult, *FileResult, error) {
	var movieID string
	var rawFilenameQuery string
	var matchResultPtr *matcher.MatchResult
	matcherMissFallback := false

	if queryOverride != "" {
		movieID = queryOverride
		matchResultPtr = nil
		logging.Debugf("[Batch %s] File %d: Using manual search query: %s", job.ID, fileIndex, movieID)
		if strings.HasPrefix(strings.ToLower(queryOverride), "http://") || strings.HasPrefix(strings.ToLower(queryOverride), "https://") {
			extractedID := extractIDFromURL(queryOverride, registry)
			if extractedID != "" {
				logging.Debugf("[Batch %s] File %d: URL detected, extracted ID: %s (using for movieID and fallback search)", job.ID, fileIndex, extractedID)
				movieID = extractedID
			}
		}
	} else {
		fileInfo := scanner.FileInfo{
			Path:      filePath,
			Name:      filepath.Base(filePath),
			Extension: filepath.Ext(filePath),
			Dir:       filepath.Dir(filePath),
		}
		rawFilenameQuery = strings.TrimSpace(strings.TrimSuffix(fileInfo.Name, fileInfo.Extension))

		matchResults := fileMatcher.Match([]scanner.FileInfo{fileInfo})
		if len(matchResults) == 0 {
			movieID = rawFilenameQuery
			if movieID == "" {
				errMsg := "could not extract movie ID from filename"
				logging.Debugf("[Batch %s] File %d: %s", job.ID, fileIndex, errMsg)
				return nil, &FileResult{
					FilePath:  filePath,
					Status:    JobStatusFailed,
					Error:     errMsg,
					StartedAt: startTime,
				}, errors.New(errMsg)
			}
			matcherMissFallback = true
			matchResultPtr = nil
			logging.Debugf("[Batch %s] File %d: Matcher could not extract ID, using raw filename query: %s",
				job.ID, fileIndex, movieID)
		} else {
			matchResultPtr = &matchResults[0]
			movieID = matchResultPtr.ID
			logging.Debugf("[Batch %s] File %d: Extracted movie ID: %s", job.ID, fileIndex, movieID)
		}
	}

	return &scrapeQueryResult{
		movieID:             movieID,
		rawFilenameQuery:    rawFilenameQuery,
		resolvedID:          "",
		matchResultPtr:      matchResultPtr,
		matcherMissFallback: matcherMissFallback,
	}, nil, nil
}

type scraperLoopOutcome struct {
	result  *models.ScraperResult
	failure *scraperFailure
	cancel  *FileResult
}

func queryScrapers(
	ctx context.Context,
	job *BatchJob,
	filePath string,
	fileIndex int,
	query *scrapeQueryResult,
	queryOverride string,
	registry *models.ScraperRegistry,
	selectedScrapers []string,
	scraperPriorityOverride []string,
	cfg *config.Config,
	startTime time.Time,
) ([]*models.ScraperResult, []scraperFailure, string, *FileResult, error) {
	results := make([]*models.ScraperResult, 0)
	scraperFailures := make([]scraperFailure, 0)

	scrapersToUse, earlyResult := resolveScrapersToUse(ctx, job, fileIndex, query, registry, selectedScrapers, scraperPriorityOverride, cfg, startTime)
	if earlyResult != nil {
		return nil, nil, "", earlyResult, errors.New(earlyResult.Error)
	}
	regularScrapers, actressResolvers := partitionActressResolvers(scrapersToUse)

	for _, s := range regularScrapers {
		select {
		case <-ctx.Done():
			return nil, nil, "", newCancelledFileResult(filePath, query.movieID, startTime), ctx.Err()
		default:
		}

		outcome := processSingleScraper(ctx, job, fileIndex, query, queryOverride, s, startTime)
		if outcome.cancel != nil {
			return nil, nil, "", outcome.cancel, ctx.Err()
		}
		if outcome.result != nil {
			results = append(results, outcome.result)
		}
		if outcome.failure != nil {
			scraperFailures = append(scraperFailures, *outcome.failure)
		}

		// Early-stop: once enough results are collected and required fields are
		// covered, skip the remaining lower-priority scrapers.
		if shouldEarlyStop(cfg, results) {
			logging.Debugf("[Batch %s] File %d: Early-stop after %d results (min=%d, required fields covered), skipping remaining scrapers",
				job.ID, fileIndex, len(results), earlyStopMin(cfg))
			break
		}
	}

	if !needsActressResolution(results) {
		return results, scraperFailures, "", nil, nil
	}

	thumbnailResolver := findActressThumbnailResolver(registry)
	for _, scraper := range actressResolvers {
		if err := ctx.Err(); err != nil {
			return nil, nil, "", newCancelledFileResult(filePath, query.movieID, startTime), err
		}

		resolver := scraper.(models.ActressResolver)
		resolverResult, err := safeResolveActresses(ctx, resolver, query.movieID)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, "", newCancelledFileResult(filePath, query.movieID, startTime), ctx.Err()
			}
			logging.Warnf("[Batch %s] File %d: Actress verification failed (movie=%s resolver=%s): %v; regular scraper cast was preserved",
				job.ID, fileIndex, query.movieID, scraper.Name(), err)
			scraperFailures = append(scraperFailures, scraperFailure{Scraper: scraper.Name(), Err: err})
			continue
		}
		if !hasVerifiedActresses(resolverResult) {
			err := fmt.Errorf("actress resolver returned no verified DMM actresses")
			logging.Warnf("[Batch %s] File %d: Actress verification rejected (movie=%s resolver=%s): %v; regular scraper cast was preserved",
				job.ID, fileIndex, query.movieID, scraper.Name(), err)
			scraperFailures = append(scraperFailures, scraperFailure{Scraper: scraper.Name(), Err: err})
			continue
		}

		if thumbnailResolver != nil {
			for index := range resolverResult.Actresses {
				if resolverResult.Actresses[index].ThumbURL != "" {
					continue
				}
				resolverResult.Actresses[index].ThumbURL = safeResolveActressThumbnail(
					ctx, thumbnailResolver, resolverResult.Actresses[index],
				)
				if ctx.Err() != nil {
					return nil, nil, "", newCancelledFileResult(filePath, query.movieID, startTime), ctx.Err()
				}
			}
		}

		results = append(results, resolverResult)
		return results, scraperFailures, scraper.Name(), nil, nil
	}

	return results, scraperFailures, "", nil, nil
}

// needsActressResolution keeps SougouWiki as a fallback. Once any regular
// metadata provider supplies a positive DMM actress ID, its cast is retained
// and the movie is not sent to the resolver.
func needsActressResolution(results []*models.ScraperResult) bool {
	for _, result := range results {
		if result == nil {
			continue
		}
		for _, actress := range result.Actresses {
			if actress.DMMID > 0 {
				return false
			}
		}
	}
	return true
}

func partitionActressResolvers(scrapers []models.Scraper) ([]models.Scraper, []models.Scraper) {
	regular := make([]models.Scraper, 0, len(scrapers))
	resolvers := make([]models.Scraper, 0)
	for _, scraper := range scrapers {
		if _, ok := scraper.(models.ActressResolver); ok {
			resolvers = append(resolvers, scraper)
			continue
		}
		regular = append(regular, scraper)
	}
	return regular, resolvers
}

func hasVerifiedActresses(result *models.ScraperResult) bool {
	if result == nil || len(result.Actresses) == 0 {
		return false
	}
	for _, actress := range result.Actresses {
		hasName := strings.TrimSpace(actress.JapaneseName) != "" ||
			strings.TrimSpace(actress.FirstName) != "" ||
			strings.TrimSpace(actress.LastName) != ""
		if actress.DMMID <= 0 || !hasName ||
			models.IsUnknownActressFields(actress.LastName, actress.FirstName, actress.JapaneseName) ||
			models.IsDescriptiveNonName(actress.LastName, actress.FirstName, actress.JapaneseName) {
			return false
		}
	}
	return true
}

func findActressThumbnailResolver(registry *models.ScraperRegistry) models.ActressThumbnailResolver {
	if registry == nil {
		return nil
	}
	if dmmScraper, ok := registry.Get("dmm"); ok {
		if resolver, ok := dmmScraper.(models.ActressThumbnailResolver); ok {
			return resolver
		}
	}
	for _, scraper := range registry.GetAll() {
		if resolver, ok := scraper.(models.ActressThumbnailResolver); ok {
			return resolver
		}
	}
	return nil
}

func resolveScrapersToUse(
	ctx context.Context,
	job *BatchJob,
	fileIndex int,
	query *scrapeQueryResult,
	registry *models.ScraperRegistry,
	selectedScrapers []string,
	scraperPriorityOverride []string,
	cfg *config.Config,
	startTime time.Time,
) ([]models.Scraper, *FileResult) {
	var scraperNames []string
	if len(selectedScrapers) > 0 {
		scraperNames = selectedScrapers
		logging.Debugf("[Batch %s] File %d: Using custom scraper priority: %v", job.ID, fileIndex, selectedScrapers)
	} else if len(scraperPriorityOverride) > 0 {
		scraperNames = scraperPriorityOverride
		logging.Debugf("[Batch %s] File %d: Using priority override (URL-filtered): %v", job.ID, fileIndex, scraperPriorityOverride)
	} else if cfg != nil && len(cfg.Scrapers.Priority) > 0 {
		scraperNames = cfg.Scrapers.Priority
		logging.Debugf("[Batch %s] File %d: Using configured scraper priority: %v", job.ID, fileIndex, scraperNames)
	} else {
		logging.Debugf("[Batch %s] File %d: Using registry default scraper priority", job.ID, fileIndex)
	}

	priorityInput := query.movieID
	if query.rawFilenameQuery != "" {
		priorityInput = query.rawFilenameQuery
	}
	scrapersToUse := registry.GetByPriorityForInput(scraperNames, priorityInput)

	if query.matcherMissFallback {
		matchedScrapers := make([]models.Scraper, 0, len(scrapersToUse))
		for _, s := range scrapersToUse {
			if _, ok := resolveScraperQueryForInputs(s, query.rawFilenameQuery, query.movieID); ok {
				matchedScrapers = append(matchedScrapers, s)
			}
		}
		if len(matchedScrapers) == 0 {
			errMsg := fmt.Sprintf("No scraper query resolver matched filename input: %s", query.movieID)
			logging.Debugf("[Batch %s] File %d: %s", job.ID, fileIndex, errMsg)
			return nil, newFailedFileResult("", query.movieID, errMsg, startTime)
		}
		scrapersToUse = matchedScrapers
		logging.Debugf("[Batch %s] File %d: Routed filename input %s to resolver-matched scrapers: %d",
			job.ID, fileIndex, query.movieID, len(scrapersToUse))
	}
	logging.Debugf("[Batch %s] File %d: Resolved to %d scrapers", job.ID, fileIndex, len(scrapersToUse))
	return scrapersToUse, nil
}

func processSingleScraper(
	ctx context.Context,
	job *BatchJob,
	fileIndex int,
	query *scrapeQueryResult,
	queryOverride string,
	scraper models.Scraper,
	startTime time.Time,
) scraperLoopOutcome {
	movieID := query.movieID
	resolvedID := query.resolvedID
	rawFilenameQuery := query.rawFilenameQuery

	if queryOverride != "" {
		if handler, ok := scraper.(models.URLHandler); ok && handler.CanHandleURL(queryOverride) {
			if directScraper, ok := scraper.(models.DirectURLScraper); ok {
				logging.Debugf("[Batch %s] File %d: Trying direct URL scrape for %s",
					job.ID, fileIndex, scraper.Name())
				scraperResult, err := safeScrapeURL(ctx, directScraper, queryOverride)
				if err == nil {
					logging.Debugf("[Batch %s] File %d: Direct URL scrape succeeded for %s",
						job.ID, fileIndex, scraper.Name())
					return scraperLoopOutcome{result: scraperResult}
				}
				logDirectURLFailure(job, fileIndex, scraper.Name(), err)
			}
		}
	}

	scraperQuery := resolvedID
	if mappedQuery, ok := resolveScraperQueryForInputs(scraper, rawFilenameQuery, movieID, resolvedID); ok {
		scraperQuery = mappedQuery
	}
	if scraperQuery != movieID {
		logging.Debugf("[Batch %s] File %d: Scraper %s using resolvedID %q instead of movieID %q",
			job.ID, fileIndex, scraper.Name(), scraperQuery, movieID)
	}

	logging.Debugf("[Batch %s] File %d: Querying scraper %s for %s", job.ID, fileIndex, scraper.Name(), scraperQuery)
	scraperResult, err := safeSearch(ctx, scraper, scraperQuery)
	if err != nil {
		return handleScraperError(ctx, job, fileIndex, query, queryOverride, scraper, scraperQuery, err, startTime)
	}

	logging.Debugf("[Batch %s] File %d: Scraper %s returned: Title=%s, Language=%s, Actresses=%d, Genres=%d",
		job.ID, fileIndex, scraper.Name(), scraperResult.Title, scraperResult.Language,
		len(scraperResult.Actresses), len(scraperResult.Genres))
	return scraperLoopOutcome{result: scraperResult}
}

func logDirectURLFailure(job *BatchJob, fileIndex int, scraperName string, err error) {
	if scraperErr, ok := models.AsScraperError(err); ok {
		if scraperErr.Kind == models.ScraperErrorKindNotFound {
			logging.Debugf("[Batch %s] File %d: Direct URL not found, falling back to ID search", job.ID, fileIndex)
		} else {
			logging.Debugf("[Batch %s] File %d: Direct URL scrape failed (%s), falling back to ID search",
				job.ID, fileIndex, scraperErr.Kind)
		}
	} else {
		logging.Debugf("[Batch %s] File %d: Direct URL scrape failed: %v, falling back to ID search",
			job.ID, fileIndex, err)
	}
}

func handleScraperError(
	ctx context.Context,
	job *BatchJob,
	fileIndex int,
	query *scrapeQueryResult,
	queryOverride string,
	scraper models.Scraper,
	scraperQuery string,
	scraperErr error,
	startTime time.Time,
) scraperLoopOutcome {
	movieID := query.movieID
	if scraperErr == ctx.Err() {
		logging.Debugf("[Batch %s] File %d: Context cancelled during scraper %s", job.ID, fileIndex, scraper.Name())
		return scraperLoopOutcome{cancel: newCancelledFileResult("", movieID, startTime)}
	}

	logging.Debugf("[Batch %s] File %d: Scraper %s failed: %v", job.ID, fileIndex, scraper.Name(), scraperErr)

	if scraperQuery != movieID && queryOverride == "" {
		logging.Debugf("[Batch %s] File %d: Retrying scraper %s with original ID: %s",
			job.ID, fileIndex, scraper.Name(), movieID)
		retryResult, retryErr := safeSearch(ctx, scraper, movieID)
		if retryErr != nil {
			if retryErr == ctx.Err() {
				logging.Debugf("[Batch %s] File %d: Context cancelled during scraper %s retry", job.ID, fileIndex, scraper.Name())
				return scraperLoopOutcome{cancel: newCancelledFileResult("", movieID, startTime)}
			}
			logging.Debugf("[Batch %s] File %d: Scraper %s failed with original ID: %v",
				job.ID, fileIndex, scraper.Name(), retryErr)
			return scraperLoopOutcome{failure: &scraperFailure{Scraper: scraper.Name(), Err: retryErr}}
		}
		return scraperLoopOutcome{result: retryResult}
	}

	return scraperLoopOutcome{failure: &scraperFailure{Scraper: scraper.Name(), Err: scraperErr}}
}
