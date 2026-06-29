package file

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

// RegisterRoutes registers the protected file-system routes on the given router group.
func RegisterRoutes(protected *gin.RouterGroup, rt *core.APIRuntime) {
	protected.GET("/cwd", getCurrentWorkingDirectory(rt))
	protected.POST("/scan", scanDirectory(rt))
	protected.POST("/browse", browseDirectory(rt))
	protected.POST("/browse/autocomplete", autocompletePath(rt))
}
