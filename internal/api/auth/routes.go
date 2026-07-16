package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

// RequireTokenOrSession returns a gin handler that accepts either a valid API token or an authenticated session.
func RequireTokenOrSession(deps *core.APIDeps) gin.HandlerFunc {
	rt := core.NewAPIRuntime(deps)
	return requireTokenOrSession(rt)
}

// RegisterPublicRoutes mounts the unauthenticated auth endpoints (status, setup, login, logout) on v1.
func RegisterPublicRoutes(v1 *gin.RouterGroup, rt *core.APIRuntime) {
	v1.GET("/auth/status", getAuthStatus(rt))
	v1.POST("/auth/setup", setupAuth(rt))
	v1.POST("/auth/login", loginAuth(rt))
	v1.POST("/auth/logout", logoutAuth(rt))
}
