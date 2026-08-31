package router

import (
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

func RegisterContractReviewRoutes(r *gin.RouterGroup, h *handler.ContractReviewHandler, g *rbacGuards) {
	r.GET("/contract-review-playbooks", g.Viewer(), h.Playbooks)
	reviews := r.Group("/contract-reviews")
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
		reviews.POST("/:id/start", g.Viewer(), h.Start)
		reviews.POST("/:id/retry", g.Viewer(), h.Retry)
		reviews.GET("/:id/events", g.Viewer(), h.Events)
	}
}
