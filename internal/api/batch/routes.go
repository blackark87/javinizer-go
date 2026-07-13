package batch

import "github.com/gin-gonic/gin"

func RegisterRoutes(protected *gin.RouterGroup, deps *ServerDependencies) {
	protected.GET("/batch", listBatchJobs(deps))
	protected.POST("/batch/scrape", batchScrape(deps))
	protected.GET("/batch/:id", getBatchJob(deps))
	protected.GET("/batch/:id/results/:resultId", getBatchJobResult(deps))
	protected.DELETE("/batch/:id", deleteBatchJob(deps))
	protected.POST("/batch/:id/cancel", cancelBatchJob(deps))
	protected.PATCH("/batch/:id/results/:resultId", updateBatchMovie(deps))
	protected.POST("/batch/:id/movies/batch-exclude", batchExcludeMovies(deps))
	protected.POST("/batch/:id/movies/batch-rescrape", batchRescrapeMovies(deps))
	protected.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(deps))
	protected.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(deps))
	protected.POST("/batch/:id/results/:resultId/exclude", excludeBatchMovie(deps))
	protected.POST("/batch/:id/results/:resultId/preview", previewOrganize(deps))
	protected.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(deps))
	protected.POST("/batch/:id/organize", organizeJob(deps))
	protected.POST("/batch/:id/update", updateBatchJob(deps))
}
