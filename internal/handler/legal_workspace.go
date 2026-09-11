package handler

import (
	"net/http"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// LegalWorkspaceDataHandler exposes the Owner-only tenant purge. It is kept
// separate from ContractReviewHandler because the operation now spans more
// than contract-review rows.
type LegalWorkspaceDataHandler struct {
	service interfaces.LegalWorkspaceDataService
}

func NewLegalWorkspaceDataHandler(service interfaces.LegalWorkspaceDataService) *LegalWorkspaceDataHandler {
	return &LegalWorkspaceDataHandler{service: service}
}

func (h *LegalWorkspaceDataHandler) DeleteTenantData(c *gin.Context) {
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
