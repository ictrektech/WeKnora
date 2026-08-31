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

func contractReviewError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrContractReviewNotFound):
		c.Error(apperrors.NewNotFoundError(err.Error()))
	case errors.Is(err, service.ErrContractReviewInvalidState), errors.Is(err, service.ErrContractReviewInvalidFile):
		c.Error(apperrors.NewBadRequestError(err.Error()))
	case errors.Is(err, service.ErrContractReviewModelMissing):
		c.Error(apperrors.NewBadRequestError(err.Error()).WithDetails("MODEL_NOT_CONFIGURED"))
	default:
		c.Error(apperrors.NewInternalServerError(err.Error()))
	}
}

func (h *ContractReviewHandler) List(c *gin.Context) {
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
	Title            string `json:"title"`
	PlaybookID       string `json:"playbook_id"`
	RepresentedParty string `json:"represented_party"`
	Archived         *bool  `json:"archived"`
}

func (h *ContractReviewHandler) Update(c *gin.Context) {
	userID, tenantID, ok := contractReviewContext(c)
	if !ok {
		return
	}
	var req contractReviewUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid request body"))
		return
	}
	r, err := h.service.Update(c.Request.Context(), tenantID, userID, c.Param("id"), req.Title, req.PlaybookID, req.RepresentedParty, req.Archived)
	if err != nil {
		contractReviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": r})
}

func (h *ContractReviewHandler) Delete(c *gin.Context) {
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

func (h *ContractReviewHandler) Start(c *gin.Context) { h.run(c, false) }
func (h *ContractReviewHandler) Retry(c *gin.Context) { h.run(c, true) }
func (h *ContractReviewHandler) run(c *gin.Context, retry bool) {
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
	c.JSON(http.StatusOK, gin.H{"success": true, "data": h.service.Playbooks()})
}

func (h *ContractReviewHandler) Events(c *gin.Context) {
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
