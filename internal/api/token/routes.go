package token

import (
	"github.com/gin-gonic/gin"
)

// RegisterRoutes registers the API token management routes on the given router groups.
func RegisterRoutes(protected *gin.RouterGroup, writeProtected *gin.RouterGroup, svc *TokenService) {
	protected.GET("/tokens", listTokens(svc))
	writeProtected.POST("/tokens", createToken(svc))
	writeProtected.DELETE("/tokens/:id", revokeToken(svc))
	writeProtected.POST("/tokens/:id/regenerate", regenerateToken(svc))
}
