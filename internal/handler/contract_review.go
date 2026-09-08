package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type ContractReviewHandler struct {
	service interfaces.ContractReviewService
}

func NewContractReviewHandler(service interfaces.ContractReviewService) *ContractReviewHandler {
	return &ContractReviewHandler{service: service}
}

func contractReviewContext(c *gin.Context) (string, uint64, bool) { return favoriteContext(c) }

// contractReviewAccessAllowed keeps the handler safe even when a caller uses
// a handler directly in a custom route. The normal router also installs the
// same gate at the route group, while a missing tenant object is left to the
// existing authentication/context checks for backwards-compatible tests and
// non-HTTP integrations.
func contractReviewAccessAllowed(c *gin.Context) bool {
	tenant, ok := types.TenantInfoFromContext(c.Request.Context())
	if ok && !tenant.LegalWorkspaceConfig.IsEnabled() {
		c.Error(apperrors.NewForbiddenError("legal workspace is disabled"))
		c.Abort()
		return false
	}
	return true
}

func contractReviewError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrContractReviewNotFound):
		c.Error(apperrors.NewNotFoundError(err.Error()))
	case errors.Is(err, service.ErrContractReviewInvalidState), errors.Is(err, service.ErrContractReviewInvalidFile):
		c.Error(apperrors.NewBadRequestError(err.Error()))
	case errors.Is(err, service.ErrContractReviewInvalidModel):
		c.Error(apperrors.NewBadRequestError(err.Error()).WithDetails("MODEL_NOT_AVAILABLE"))
	case errors.Is(err, service.ErrContractReviewModelMissing):
		c.Error(apperrors.NewBadRequestError(err.Error()).WithDetails("MODEL_NOT_CONFIGURED"))
	default:
		c.Error(apperrors.NewInternalServerError(err.Error()))
	}
}

func (h *ContractReviewHandler) List(c *gin.Context) {
	if !contractReviewAccessAllowed(c) {
		return
	}
	userID, tenantID, ok := contractReviewContext(c)
	if !ok {
		return
	}
	rows, err := h.service.List(c.Request.Context(), tenantID, userID, c.Query("archived") == "true")
	if err != nil {
		contractReviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

func (h *ContractReviewHandler) Create(c *gin.Context) {
	if !contractReviewAccessAllowed(c) {
		return
	}
	userID, tenantID, ok := contractReviewContext(c)
	if !ok {
		return
	}
	r, err := h.service.Create(c.Request.Context(), tenantID, userID)
	if err != nil {
		contractReviewError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": r})
}

func (h *ContractReviewHandler) Get(c *gin.Context) {
	if !contractReviewAccessAllowed(c) {
		return
	}
	userID, tenantID, ok := contractReviewContext(c)
	if !ok {
		return
	}
	r, err := h.service.Get(c.Request.Context(), tenantID, userID, c.Param("id"))
	if err != nil {
		contractReviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": r})
}

type contractReviewUpdateRequest struct {
	Title            string  `json:"title"`
	PlaybookID       string  `json:"playbook_id"`
	RepresentedParty string  `json:"represented_party"`
	ModelID          *string `json:"model_id"`
	Archived         *bool   `json:"archived"`
}

func (h *ContractReviewHandler) Update(c *gin.Context) {
	if !contractReviewAccessAllowed(c) {
		return
	}
	userID, tenantID, ok := contractReviewContext(c)
	if !ok {
		return
	}
	var req contractReviewUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid request body"))
		return
	}
	r, err := h.service.Update(c.Request.Context(), tenantID, userID, c.Param("id"), req.Title, req.PlaybookID, req.RepresentedParty, req.ModelID, req.Archived)
	if err != nil {
		contractReviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": r})
}

func (h *ContractReviewHandler) Delete(c *gin.Context) {
	if !contractReviewAccessAllowed(c) {
		return
	}
	userID, tenantID, ok := contractReviewContext(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), tenantID, userID, c.Param("id")); err != nil {
		contractReviewError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type contractReviewBulkRequest struct {
	IDs []string `json:"ids"`
}

func (h *ContractReviewHandler) BulkAction(action types.ContractReviewBulkAction) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !contractReviewAccessAllowed(c) {
			return
		}
		userID, tenantID, ok := contractReviewContext(c)
		if !ok {
			return
		}
		var req contractReviewBulkRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.Error(apperrors.NewBadRequestError("invalid request body"))
			return
		}
		result, err := h.service.BulkAction(c.Request.Context(), tenantID, userID, req.IDs, action)
		if err != nil {
			if strings.Contains(err.Error(), "bulk action") {
				c.Error(apperrors.NewBadRequestError(err.Error()))
			} else {
				contractReviewError(c, err)
			}
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
	}
}

func (h *ContractReviewHandler) Upload(c *gin.Context) {
	if !contractReviewAccessAllowed(c) {
		return
	}
	userID, tenantID, ok := contractReviewContext(c)
	if !ok {
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		c.Error(apperrors.NewBadRequestError("contract file is required"))
		return
	}
	f, err := file.Open()
	if err != nil {
		contractReviewError(c, err)
		return
	}
	defer f.Close()
	r, err := h.service.Upload(c.Request.Context(), tenantID, userID, c.Param("id"), file.Filename, file.Header.Get("Content-Type"), file.Size, f)
	if err != nil {
		contractReviewError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": r})
}

func (h *ContractReviewHandler) Preview(c *gin.Context) {
	if !contractReviewAccessAllowed(c) {
		return
	}
	userID, tenantID, ok := contractReviewContext(c)
	if !ok {
		return
	}
	r, f, err := h.service.OpenDocument(c.Request.Context(), tenantID, userID, c.Param("id"))
	if err != nil {
		contractReviewError(c, err)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		contractReviewError(c, err)
		return
	}
	mime := r.MimeType
	if mime == "" {
		if r.FileType == ".pdf" {
			mime = "application/pdf"
		} else {
			mime = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		}
	}
	c.Header("Content-Type", mime)
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=%q", strings.ReplaceAll(r.FileName, "\"", "")))
	http.ServeContent(c.Writer, c.Request, r.FileName, r.UpdatedAt, bytes.NewReader(data))
}

// Locator returns the parser-produced source units used as the stable
// identity for evidence. It intentionally does not attempt to reconstruct
// locations from the rendered PDF/DOCX when a legacy review has no locator.
func (h *ContractReviewHandler) Locator(c *gin.Context) {
	if !contractReviewAccessAllowed(c) {
		return
	}
	userID, tenantID, ok := contractReviewContext(c)
	if !ok {
		return
	}
	r, err := h.service.Get(c.Request.Context(), tenantID, userID, c.Param("id"))
	if err != nil {
		contractReviewError(c, err)
		return
	}
	var locator map[string]any
	if len(r.Locator) > 0 {
		if err := json.Unmarshal(r.Locator, &locator); err != nil {
			locator = nil
		}
	}
	if locator == nil {
		locator = map[string]any{}
	}
	if !locatorHasUnits(locator) {
		if raw, exists := locator["source_units_json"]; exists {
			locator["units"] = raw
		} else {
			var metadata map[string]any
			if json.Unmarshal(r.Metadata, &metadata) == nil {
				if raw, exists := metadata["source_units_json"]; exists {
					locator["units"] = raw
				}
			}
		}
	}
	if !locatorHasUnits(locator) {
		locator["units"] = []any{}
	}
	locator["review_id"] = r.ID
	if r.SourceRevision != "" {
		locator["source_revision"] = r.SourceRevision
	}
	if r.SourceTextHash != "" {
		locator["source_text_hash"] = r.SourceTextHash
	} else if r.SourceHash != "" {
		locator["source_text_hash"] = r.SourceHash
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": locator})
}

func locatorHasUnits(locator map[string]any) bool {
	raw, ok := locator["units"]
	if !ok {
		return false
	}
	if units, ok := raw.([]any); ok {
		return len(units) > 0
	}
	if encoded, ok := raw.(string); ok {
		var units []any
		return json.Unmarshal([]byte(encoded), &units) == nil && len(units) > 0
	}
	return false
}

func (h *ContractReviewHandler) Start(c *gin.Context) { h.run(c, false) }
func (h *ContractReviewHandler) Retry(c *gin.Context) { h.run(c, true) }
func (h *ContractReviewHandler) run(c *gin.Context, retry bool) {
	if !contractReviewAccessAllowed(c) {
		return
	}
	userID, tenantID, ok := contractReviewContext(c)
	if !ok {
		return
	}
	var r *types.ContractReview
	var err error
	if retry {
		r, err = h.service.Retry(c.Request.Context(), tenantID, userID, c.Param("id"))
	} else {
		r, err = h.service.Start(c.Request.Context(), tenantID, userID, c.Param("id"))
	}
	if err != nil {
		contractReviewError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": r})
}

func (h *ContractReviewHandler) Playbooks(c *gin.Context) {
	if !contractReviewAccessAllowed(c) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": h.service.Playbooks()})
}

// DeleteTenantData permanently removes all legal workspace data belonging to
// the active tenant. The route is Owner-gated and intentionally independent
// from the enable/disable access switch.
func (h *ContractReviewHandler) DeleteTenantData(c *gin.Context) {
	_, tenantID, ok := contractReviewContext(c)
	if !ok {
		return
	}
	if err := h.service.DeleteTenantData(c.Request.Context(), tenantID); err != nil {
		contractReviewError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *ContractReviewHandler) Events(c *gin.Context) {
	if !contractReviewAccessAllowed(c) {
		return
	}
	userID, tenantID, ok := contractReviewContext(c)
	if !ok {
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	lastHash := ""
	eventID := 0
	ticker := time.NewTicker(time.Second)
	heartbeat := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	defer heartbeat.Stop()
	emit := func() bool {
		r, err := h.service.Get(c.Request.Context(), tenantID, userID, c.Param("id"))
		if err != nil {
			return false
		}
		data, _ := json.Marshal(r)
		sum := fmt.Sprintf("%x", sha256.Sum256(data))
		if sum == lastHash {
			return true
		}
		lastHash = sum
		eventID++
		_, _ = fmt.Fprintf(c.Writer, "id: %s\nevent: snapshot\ndata: %s\n\n", strconv.Itoa(eventID), data)
		c.Writer.Flush()
		return true
	}
	if !emit() {
		return
	}
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			if !emit() {
				return
			}
		case <-heartbeat.C:
			c.SSEvent("heartbeat", gin.H{"at": time.Now().Unix()})
			c.Writer.Flush()
		}
	}
}
