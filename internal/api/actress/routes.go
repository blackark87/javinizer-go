package actress

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
)

// RegisterRoutes registers the actress CRUD, search, merge, and import/export routes on the given protected router group.
func RegisterRoutes(protected *gin.RouterGroup, deps ActressDeps, runtimes ...*core.APIRuntime) {
	protected.GET("/actresses", listActresses(deps))
	protected.GET("/actresses/:id", getActress(deps))
	protected.POST("/actresses", createActress(deps))
	protected.PUT("/actresses/:id", updateActress(deps))
	protected.DELETE("/actresses/:id", deleteActress(deps))
	protected.GET("/actresses/search", searchActresses(deps))
	protected.GET("/actresses/alias-group", getAliasGroup(deps))
	protected.POST("/actresses/merge/preview", previewActressMerge(deps))
	protected.POST("/actresses/merge", mergeActresses(deps))
	protected.GET("/actresses/export", exportActresses(deps))
	protected.POST("/actresses/import", importActresses(deps))
	if len(runtimes) == 0 || runtimes[0] == nil {
		return
	}
	rt := runtimes[0]
	protected.POST("/actresses/resolve-alias-choice", resolveAliasChoice(rt))
	if rt.Deps() != nil && rt.Deps().CoreDeps != nil && rt.Deps().CoreDeps.DB != nil {
		actressRepo := database.NewActressRepository(rt.Deps().CoreDeps.DB)
		movieRepo := database.NewMovieRepository(rt.Deps().CoreDeps.DB)
		protected.POST("/actresses/bulk-delete", bulkDeleteActresses(actressRepo))
		protected.POST("/actresses/delete-all", deleteAllActresses(actressRepo))
		protected.GET("/actresses/:id/movies", listActressMovies(actressRepo, movieRepo))
	}
	protected.GET("/actresses/sync-candidates", listActressSyncCandidates(rt))
	protected.POST("/actresses/sync-jobs", createActressSyncJob(rt))
	protected.GET("/actresses/sync-jobs/active", listActiveActressSyncJobs(rt))
	protected.GET("/actresses/sync-jobs/:jobID", getActressSyncJob(rt))
	protected.GET("/actresses/sync-jobs/:jobID/tasks", listActressSyncJobTasks(rt))
	protected.POST("/actresses/sync-jobs/:jobID/cancel", cancelActressSyncJob(rt))
}
