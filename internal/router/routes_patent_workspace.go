package router

import (
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/handler/session"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

func patentWorkspaceEnabled() gin.HandlerFunc {
	return func(c *gin.Context) {
		tenant, ok := types.TenantInfoFromContext(c.Request.Context())
		if !ok || !tenant.PatentWorkspaceConfig.IsEnabled() {
			c.Error(apperrors.NewForbiddenError("patent workspace is disabled"))
			c.Abort()
			return
		}
		c.Next()
	}
}

func RegisterPatentWorkspaceRoutes(r *gin.RouterGroup, sessions *session.Handler, purge *handler.PatentWorkspaceDataHandler, g *rbacGuards) {
	r.POST("/patent-search/sessions", patentWorkspaceEnabled(), g.Viewer(), sessions.CreatePatentSearchSession)
	r.POST("/patent-writing/sessions", patentWorkspaceEnabled(), g.Viewer(), sessions.CreatePatentWritingSession)
	r.DELETE("/patent-workspace-data", g.Owner(), purge.DeleteTenantData)
	examples := r.Group("/patent-workspace/examples", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Next()
	}, g.Viewer(), patentWorkspaceEnabled(), func(c *gin.Context) {
		tenant, ok := types.TenantInfoFromContext(c.Request.Context())
		if !ok || !tenant.PatentWorkspaceConfig.AreExamplesEnabled() {
			c.Error(apperrors.NewForbiddenError("patent examples are disabled"))
			c.Abort()
			return
		}
		c.Next()
	})
	examples.GET("", handler.PatentExamples)
	examples.GET("/:example_id", handler.PatentExamples)
	examples.GET("/:example_id/documents/:document_id", handler.PatentExamples)
}
