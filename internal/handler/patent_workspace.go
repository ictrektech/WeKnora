package handler

import (
	"net/http"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// PatentWorkspaceDataHandler exposes the Owner-only patent session purge.
type PatentWorkspaceDataHandler struct {
	service interfaces.PatentWorkspaceDataService
}

func NewPatentWorkspaceDataHandler(service interfaces.PatentWorkspaceDataService) *PatentWorkspaceDataHandler {
	return &PatentWorkspaceDataHandler{service: service}
}

func (h *PatentWorkspaceDataHandler) DeleteTenantData(c *gin.Context) {
	tenantIDValue, ok := c.Get(types.TenantIDContextKey.String())
	if !ok {
		c.Error(apperrors.NewUnauthorizedError("Unauthorized"))
		return
	}
	tenantID, ok := tenantIDValue.(uint64)
	if !ok || tenantID == 0 {
		c.Error(apperrors.NewUnauthorizedError("Unauthorized"))
		return
	}
	if err := h.service.DeleteTenantData(c.Request.Context(), tenantID); err != nil {
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}
	c.Status(http.StatusNoContent)
}
