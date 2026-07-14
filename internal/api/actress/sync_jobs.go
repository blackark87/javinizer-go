package actress

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
)

type actressSyncJobResponse struct {
	Job models.ActressSyncJob `json:"job"`
}

type actressSyncJobsResponse struct {
	Jobs []models.ActressSyncJob `json:"jobs"`
}

type actressSyncTasksResponse struct {
	Tasks []models.ActressSyncTask `json:"tasks"`
	Total int                      `json:"total"`
}

// createActressSyncJob godoc
// @Summary Start a durable actress metadata sync job
// @Description Queue selected actresses or all actresses with missing metadata for background processing
// @Tags actress
// @Accept json
// @Produce json
// @Param request body worker.ActressSyncCreateRequest true "Sync scope"
// @Success 202 {object} actressSyncJobResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/actresses/sync-jobs [post]
func createActressSyncJob(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req worker.ActressSyncCreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request: " + err.Error()})
			return
		}
		if !req.Missing && len(req.ActressIDs) == 0 {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "actress_ids is required when missing is false"})
			return
		}
		manager := deps.EnsureActressSyncManager()
		if manager == nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "actress sync manager is unavailable"})
			return
		}
		job, err := manager.CreateJob(c.Request.Context(), req)
		if err != nil {
			status := http.StatusInternalServerError
			if strings.Contains(err.Error(), "no actresses") || database.IsNotFound(err) {
				status = http.StatusBadRequest
			}
			c.JSON(status, ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, actressSyncJobResponse{Job: *job})
	}
}

// listActiveActressSyncJobs godoc
// @Summary List active actress sync jobs
// @Tags actress
// @Produce json
// @Success 200 {object} actressSyncJobsResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/actresses/sync-jobs/active [get]
func listActiveActressSyncJobs(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		manager := deps.EnsureActressSyncManager()
		if manager == nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "actress sync manager is unavailable"})
			return
		}
		jobs, err := manager.ListActiveJobs()
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, actressSyncJobsResponse{Jobs: jobs})
	}
}

// getActressSyncJob godoc
// @Summary Get actress sync job progress
// @Tags actress
// @Produce json
// @Param jobID path string true "Actress sync job ID"
// @Success 200 {object} actressSyncJobResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/actresses/sync-jobs/{jobID} [get]
func getActressSyncJob(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		manager := deps.EnsureActressSyncManager()
		if manager == nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "actress sync manager is unavailable"})
			return
		}
		job, err := manager.GetJob(c.Param("jobID"))
		if err != nil {
			writeActressSyncJobError(c, err)
			return
		}
		c.JSON(http.StatusOK, actressSyncJobResponse{Job: *job})
	}
}

// listActressSyncJobTasks godoc
// @Summary List detailed actress sync job tasks
// @Tags actress
// @Produce json
// @Param jobID path string true "Actress sync job ID"
// @Success 200 {object} actressSyncTasksResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/actresses/sync-jobs/{jobID}/tasks [get]
func listActressSyncJobTasks(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		manager := deps.EnsureActressSyncManager()
		if manager == nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "actress sync manager is unavailable"})
			return
		}
		tasks, err := manager.ListTasks(c.Param("jobID"))
		if err != nil {
			writeActressSyncJobError(c, err)
			return
		}
		c.JSON(http.StatusOK, actressSyncTasksResponse{Tasks: tasks, Total: len(tasks)})
	}
}

// cancelActressSyncJob godoc
// @Summary Stop an actress sync job after currently running items
// @Tags actress
// @Produce json
// @Param jobID path string true "Actress sync job ID"
// @Success 200 {object} actressSyncJobResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/actresses/sync-jobs/{jobID}/cancel [post]
func cancelActressSyncJob(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		manager := deps.EnsureActressSyncManager()
		if manager == nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "actress sync manager is unavailable"})
			return
		}
		jobID := c.Param("jobID")
		if err := manager.CancelJob(jobID); err != nil {
			writeActressSyncJobError(c, err)
			return
		}
		job, err := manager.GetJob(jobID)
		if err != nil {
			writeActressSyncJobError(c, err)
			return
		}
		c.JSON(http.StatusOK, actressSyncJobResponse{Job: *job})
	}
}

func writeActressSyncJobError(c *gin.Context, err error) {
	if database.IsNotFound(err) {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
}
