package router

import (
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// legalWorkspaceEnabled gates the contract review surface without changing
// the underlying data. Missing legacy config is enabled by types' default.
func legalWorkspaceEnabled() gin.HandlerFunc {
	return func(c *gin.Context) {
		tenant, ok := types.TenantInfoFromContext(c.Request.Context())
		if ok && !tenant.LegalWorkspaceConfig.IsEnabled() {
			c.Error(apperrors.NewForbiddenError("legal workspace is disabled"))
			c.Abort()
			return
		}
		c.Next()
	}
}

func RegisterContractReviewRoutes(r *gin.RouterGroup, h *handler.ContractReviewHandler, g *rbacGuards) {
	r.GET("/contract-review-playbooks", legalWorkspaceEnabled(), g.Viewer(), h.Playbooks)
	// This is deliberately outside the gated review group. Owners must be able
	// to purge data even after disabling the workspace, and the purge is never
	// implied by changing the access switch.
	r.DELETE("/legal-workspace-data", g.Owner(), h.DeleteTenantData)
	reviews := r.Group("/contract-reviews", legalWorkspaceEnabled())
	{
		reviews.GET("", g.Viewer(), h.List)
		reviews.POST("", g.Viewer(), h.Create)
		reviews.POST("/bulk/archive", g.Viewer(), h.BulkAction(types.ContractReviewBulkArchive))
		reviews.POST("/bulk/restore", g.Viewer(), h.BulkAction(types.ContractReviewBulkRestore))
		reviews.POST("/bulk/delete", g.Viewer(), h.BulkAction(types.ContractReviewBulkDelete))
		reviews.GET("/:id", g.Viewer(), h.Get)
		reviews.PATCH("/:id", g.Viewer(), h.Update)
		reviews.DELETE("/:id", g.Viewer(), h.Delete)
		reviews.POST("/:id/document", g.Viewer(), h.Upload)
		reviews.GET("/:id/document/preview", g.Viewer(), h.Preview)
		reviews.GET("/:id/document/locator", g.Viewer(), h.Locator)
		reviews.POST("/:id/start", g.Viewer(), h.Start)
		reviews.POST("/:id/retry", g.Viewer(), h.Retry)
		reviews.POST("/:id/cancel", g.Viewer(), h.Cancel)
		reviews.GET("/:id/events", g.Viewer(), h.Events)
	}
}
