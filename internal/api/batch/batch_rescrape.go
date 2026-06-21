package batch

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
)

const bulkRescrapeWorkers = 5
const bulkRescrapeMaxMovies = 100

func batchRescrapeMovies(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")

		var req BulkRescrapeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}

		if len(req.MovieIDs) == 0 {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "movie_ids is required and must not be empty"})
			return
		}

		if len(req.MovieIDs) > bulkRescrapeMaxMovies {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: fmt.Sprintf("movie_ids must not exceed %d items", bulkRescrapeMaxMovies)})
			return
		}

		rescrapeReq := &BatchRescrapeRequest{
			Force:            req.Force,
			SelectedScrapers: req.SelectedScrapers,
			Preset:           req.Preset,
			ScalarStrategy:   req.ScalarStrategy,
			ArrayStrategy:    req.ArrayStrategy,
		}

		if httpStatus, errMsg := validateRescrapeRequest(rescrapeReq); errMsg != "" {
			writeErrorResponse(c, httpStatus, false, errMsg)
			return
		}

		job, ok := deps.JobQueue.GetJobPointer(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "Job not found"})
			return
		}

		if isGone, httpStatus, errMsg := validateJobState(job); errMsg != "" {
			writeErrorResponse(c, httpStatus, isGone, errMsg)
			return
		}

		cfg := deps.GetConfig()

		job.Lock()
		if job.IsDeleted() {
			job.Unlock()
			writeErrorResponse(c, http.StatusGone, true, "Job has been deleted")
			return
		}
		job.Unlock()

		logging.Infof("Bulk rescrape request for job %s: %d movies, scrapers=%v, force=%v",
			jobID, len(req.MovieIDs), req.SelectedScrapers, req.Force)

		// Return immediately — actual work runs in background to avoid broken-pipe
		// errors when proxies time out long-running HTTP connections.
		c.JSON(http.StatusAccepted, gin.H{"message": "Bulk rescrape started"})

		// Copy values that the goroutine needs; do NOT capture c (Gin recycles it).
		movieIDs := make([]string, len(req.MovieIDs))
		copy(movieIDs, req.MovieIDs)
		rescrapeReqCopy := *rescrapeReq

		go func() {
			ctx := context.Background()

			type movieResult struct {
				movieID string
				result  *BulkRescrapeMovieResult
			}

			var mu sync.Mutex
			var completedCount int

			movieChan := make(chan string, len(movieIDs))
			resultChan := make(chan movieResult, len(movieIDs))

			workerCount := bulkRescrapeWorkers
			if workerCount > len(movieIDs) {
				workerCount = len(movieIDs)
			}

			var wg sync.WaitGroup
			for i := 0; i < workerCount; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for movieID := range movieChan {
						r := processBulkRescrapeMovie(ctx, jobID, movieID, job, &rescrapeReqCopy, deps, cfg)
						resultChan <- movieResult{movieID: movieID, result: r}
					}
				}()
			}

			for _, id := range movieIDs {
				movieChan <- id
			}
			close(movieChan)

			go func() {
				wg.Wait()
				close(resultChan)
			}()

			succeeded := 0
			failed := 0
			for mr := range resultChan {
				mu.Lock()
				completedCount++
				if mr.result.Status == "success" {
					succeeded++
				} else {
					failed++
				}
				broadcastProgress(&ws.ProgressMessage{
					JobID:    jobID,
					FilePath: mr.movieID,
					Status:   mr.result.Status,
					Message:  fmt.Sprintf("Rescrape %s: %s", mr.movieID, mr.result.Status),
					Error:    mr.result.Error,
					Progress: float64(completedCount) / float64(len(movieIDs)) * 100,
				})
				mu.Unlock()
			}

			deps.JobQueue.PersistJob(job)
			logging.Infof("Bulk rescrape complete for job %s: %d succeeded, %d failed", jobID, succeeded, failed)
		}()
	}
}

func processBulkRescrapeMovie(ctx context.Context, jobID string, movieID string, job *worker.BatchJob, req *BatchRescrapeRequest, deps *ServerDependencies, cfg *config.Config) *BulkRescrapeMovieResult {
	lookup, httpStatus, errMsg := findFileForMovieID(job, movieID)
	if errMsg != "" {
		return &BulkRescrapeMovieResult{
			MovieID: movieID,
			Status:  "failed",
			Error:   fmt.Sprintf("Movie lookup failed: %s (HTTP %d)", errMsg, httpStatus),
		}
	}

	params, _ := resolveScrapeParams(req, movieID, deps)

	result, err := executeRescrape(ctx, params, job, lookup.foundFilePath, deps, req, cfg)
	if err != nil {
		return &BulkRescrapeMovieResult{
			MovieID: movieID,
			Status:  "failed",
			Error:   fmt.Sprintf("Rescrape failed: %v", err),
		}
	}

	if result == nil {
		return &BulkRescrapeMovieResult{
			MovieID: movieID,
			Status:  "failed",
			Error:   "Rescrape produced no result",
		}
	}

	if result.Status != worker.JobStatusCompleted {
		errorMsg := "Unknown error"
		if result.Error != "" {
			errorMsg = result.Error
		}
		return &BulkRescrapeMovieResult{
			MovieID: movieID,
			Status:  "failed",
			Error:   fmt.Sprintf("Rescrape failed: %s", errorMsg),
		}
	}

	var movie *models.Movie
	if result.Data != nil {
		if m, ok := result.Data.(*models.Movie); ok {
			movie = m
		}
	}

	updateRes := validateAndUpdateResult(job, result, lookup.foundFilePath, lookup.capturedRevision, movie, lookup.oldMovieID, cfg, jobID)
	if updateRes.shouldAbort {
		cleanupPosterPaths(updateRes.posterPaths)
		return &BulkRescrapeMovieResult{
			MovieID: movieID,
			Status:  "failed",
			Error:   updateRes.errorMessage,
		}
	}

	cleanupPosterPaths(updateRes.posterPaths)

	return &BulkRescrapeMovieResult{
		MovieID: movieID,
		Status:  "success",
		Movie:   movie,
	}
}
