package actress

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

func RegisterRoutes(protected *gin.RouterGroup, deps *core.ServerDependencies) {
	protected.GET("/actresses", listActresses(deps.ActressRepo))
	protected.GET("/actresses/sync-candidates", listActressSyncCandidates(deps.ActressRepo))
	protected.POST("/actresses/sync-jobs", createActressSyncJob(deps))
	protected.GET("/actresses/sync-jobs/active", listActiveActressSyncJobs(deps))
	protected.GET("/actresses/sync-jobs/:jobID", getActressSyncJob(deps))
	protected.GET("/actresses/sync-jobs/:jobID/tasks", listActressSyncJobTasks(deps))
	protected.POST("/actresses/sync-jobs/:jobID/cancel", cancelActressSyncJob(deps))
	protected.GET("/actresses/:id", getActress(deps.ActressRepo))
	protected.GET("/actresses/:id/movies", listActressMovies(deps.ActressRepo, deps.MovieRepo))
	protected.POST("/actresses/:id/sync", syncActress(deps))
	protected.POST("/actresses", createActress(deps.ActressRepo))
	protected.PUT("/actresses/:id", updateActress(deps.ActressRepo))
	protected.DELETE("/actresses/:id", deleteActress(deps.ActressRepo))
	protected.POST("/actresses/bulk-delete", bulkDeleteActresses(deps.ActressRepo))
	protected.POST("/actresses/delete-all", deleteAllActresses(deps.ActressRepo))
	protected.GET("/actresses/search", searchActresses(deps.ActressRepo))
	protected.POST("/actresses/merge/preview", previewActressMerge(deps.ActressRepo))
	protected.POST("/actresses/merge", mergeActresses(deps.ActressRepo))
	protected.GET("/actresses/export", exportActresses(deps.ActressRepo))
	protected.POST("/actresses/import", importActresses(deps.ActressRepo))
}
