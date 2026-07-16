package version

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

// RegisterRoutes wires the version status and check endpoints onto the protected router group.
func RegisterRoutes(protected *gin.RouterGroup, deps *core.APIDeps) {
	protected.GET("/version", versionStatus(deps.CoreDeps))
	protected.POST("/version/check", versionCheck(deps.CoreDeps))
}
