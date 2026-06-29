package realtime

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

// RegisterRoutes registers the realtime WebSocket routes on the given router.
func RegisterRoutes(router *gin.Engine, rt *core.APIRuntime, authMiddleware gin.HandlerFunc) {
	router.GET("/ws/progress", authMiddleware, handleWebSocket(rt))
}
