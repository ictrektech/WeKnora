package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type SmartArchiveHandler struct {
	service interfaces.SmartArchiveService
}

func NewSmartArchiveHandler(s interfaces.SmartArchiveService) *SmartArchiveHandler {
	return &SmartArchiveHandler{service: s}
}

func archiveContext(c *gin.Context) (string, uint64, bool) { return favoriteContext(c) }
func archiveError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrArchiveNotFound):
		c.Error(apperrors.NewNotFoundError(err.Error()))
	case errors.Is(err, service.ErrArchivePermission):
		c.Error(apperrors.NewForbiddenError(err.Error()))
	case errors.Is(err, service.ErrArchiveInvalidFile), errors.Is(err, service.ErrArchiveInvalidState):
		c.Error(apperrors.NewBadRequestError(err.Error()))
	default:
		c.Error(apperrors.NewInternalServerError(err.Error()))
	}
}

func (h *SmartArchiveHandler) Settings(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	row, err := h.service.GetSettings(c.Request.Context(), tenant)
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}
func (h *SmartArchiveHandler) UpdateSettings(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	var input types.ArchiveSettings
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid settings body"))
		return
	}
	row, err := h.service.UpdateSettings(c.Request.Context(), tenant, &input)
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}

func (h *SmartArchiveHandler) Import(c *gin.Context) {
	user, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	form, err := c.MultipartForm()
	if err != nil {
		c.Error(apperrors.NewBadRequestError("files are required"))
		return
	}
	files := form.File["files"]
	if len(files) == 0 {
		files = form.File["file"]
	}
	uploads := make([]*types.ArchiveUpload, 0, len(files))
	closers := make([]io.Closer, 0, len(files))
	for _, header := range files {
		file, openErr := header.Open()
		if openErr != nil {
			archiveError(c, openErr)
			return
		}
		closers = append(closers, file)
		uploads = append(uploads, &types.ArchiveUpload{FileName: header.Filename, MimeType: header.Header.Get("Content-Type"), Size: header.Size, Reader: file})
	}
	for _, closer := range closers {
		defer closer.Close()
	}
	batch, err := h.service.Import(c.Request.Context(), tenant, user, uploads)
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": batch})
}
func (h *SmartArchiveHandler) Batch(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	row, err := h.service.GetBatch(c.Request.Context(), tenant, c.Param("id"))
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}
func (h *SmartArchiveHandler) BatchEvents(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	last := ""
	emit := func() bool {
		row, err := h.service.GetBatch(c.Request.Context(), tenant, c.Param("id"))
		if err != nil {
			return false
		}
		data, _ := json.Marshal(row)
		key := string(data)
		if key == last {
			return true
		}
		last = key
		_, _ = fmt.Fprintf(c.Writer, "event: progress\ndata: %s\n\n", data)
		c.Writer.Flush()
		return row.Status != "completed" && row.Status != "failed"
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
		}
	}
}

func (h *SmartArchiveHandler) ListDocuments(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	rows, err := h.service.ListDocuments(c.Request.Context(), tenant, c.Query("q"), c.Query("archived") == "true")
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}
func (h *SmartArchiveHandler) GetDocument(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	row, err := h.service.GetDocument(c.Request.Context(), tenant, c.Param("id"))
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}
func (h *SmartArchiveHandler) UpdateDocument(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid document body"))
		return
	}
	row, err := h.service.UpdateDocument(c.Request.Context(), tenant, c.Param("id"), body)
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}
func (h *SmartArchiveHandler) RetryExtraction(c *gin.Context) {
	user, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	row, err := h.service.RetryExtraction(c.Request.Context(), tenant, c.Param("id"), user)
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": row})
}
func (h *SmartArchiveHandler) CancelExtraction(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	row, err := h.service.CancelExtraction(c.Request.Context(), tenant, c.Param("id"))
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": row})
}
func (h *SmartArchiveHandler) RetryMirror(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	row, err := h.service.RetryMirror(c.Request.Context(), tenant, c.Param("id"))
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": row})
}
func (h *SmartArchiveHandler) ArchiveDocument(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	row, err := h.service.ArchiveDocument(c.Request.Context(), tenant, c.Param("id"), true)
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}
func (h *SmartArchiveHandler) RestoreDocument(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	row, err := h.service.ArchiveDocument(c.Request.Context(), tenant, c.Param("id"), false)
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}
func (h *SmartArchiveHandler) DeleteDocument(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	if err := h.service.DeleteDocument(c.Request.Context(), tenant, c.Param("id")); err != nil {
		archiveError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *SmartArchiveHandler) BatchArchiveDocuments(c *gin.Context) {
	h.batchDocumentAction(c, types.ArchiveBulkArchive)
}

func (h *SmartArchiveHandler) BatchRestoreDocuments(c *gin.Context) {
	h.batchDocumentAction(c, types.ArchiveBulkRestore)
}

func (h *SmartArchiveHandler) BatchDeleteDocuments(c *gin.Context) {
	h.batchDocumentAction(c, types.ArchiveBulkDelete)
}

func (h *SmartArchiveHandler) BatchPurgeDocuments(c *gin.Context) {
	h.batchDocumentAction(c, types.ArchiveBulkPurge)
}

func (h *SmartArchiveHandler) batchDocumentAction(c *gin.Context, action types.ArchiveBulkAction) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.IDs) == 0 {
		c.Error(apperrors.NewBadRequestError("document ids are required"))
		return
	}
	result, err := h.service.BatchDocumentAction(c.Request.Context(), tenant, body.IDs, action)
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
func (h *SmartArchiveHandler) Evidence(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	rows, err := h.service.ListEvidence(c.Request.Context(), tenant, c.Param("id"))
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}
func (h *SmartArchiveHandler) Preview(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	row, err := h.service.GetDocument(c.Request.Context(), tenant, c.Param("id"))
	if err != nil {
		archiveError(c, err)
		return
	}
	file, ext, err := h.service.OpenDocument(c.Request.Context(), tenant, c.Param("id"))
	if err != nil {
		archiveError(c, err)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		archiveError(c, err)
		return
	}
	mime := archivePreviewMIME(ext)
	c.Header("Content-Type", mime)
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=%q", strings.ReplaceAll(row.FileName, "\"", "")))
	http.ServeContent(c.Writer, c.Request, row.FileName, row.UpdatedAt, bytes.NewReader(data))
}

func archivePreviewMIME(ext string) string {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".pdf":
		return "application/pdf"
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func (h *SmartArchiveHandler) Customers(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	rows, err := h.service.ListCustomers(c.Request.Context(), tenant, c.Query("q"))
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}
func (h *SmartArchiveHandler) UpdateCustomer(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid customer body"))
		return
	}
	row, err := h.service.UpdateCustomer(c.Request.Context(), tenant, c.Param("id"), body)
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}
func (h *SmartArchiveHandler) Search(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	var req types.ArchiveSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid search body"))
		return
	}
	result, err := h.service.Search(c.Request.Context(), tenant, &req)
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *SmartArchiveHandler) Reminders(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	rows, err := h.service.ListReminders(c.Request.Context(), tenant, c.Query("status"))
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

func (h *SmartArchiveHandler) ReminderCandidates(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	status := c.Query("status")
	rows, err := h.service.ListReminderCandidates(c.Request.Context(), tenant, status)
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

func (h *SmartArchiveHandler) BatchIgnoreReminderCandidates(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid reminder candidate ids"))
		return
	}
	result, err := h.service.BatchIgnoreReminderCandidates(c.Request.Context(), tenant, body.IDs)
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *SmartArchiveHandler) CreateReminderFromCandidate(c *gin.Context) {
	user, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	var body struct {
		OffsetDays int    `json:"offset_days"`
		Time       string `json:"time"`
		AssigneeID string `json:"assignee_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid candidate reminder body"))
		return
	}
	row, err := h.service.CreateReminderFromCandidate(c.Request.Context(), tenant, user, c.Param("id"), body.OffsetDays, body.Time, body.AssigneeID)
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": row})
}
func (h *SmartArchiveHandler) CreateReminder(c *gin.Context) {
	user, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	var row types.ArchiveReminder
	if err := c.ShouldBindJSON(&row); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid reminder body"))
		return
	}
	created, err := h.service.CreateReminder(c.Request.Context(), tenant, user, &row)
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": created})
}
func (h *SmartArchiveHandler) UpdateReminder(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	var body struct {
		Status       types.ArchiveReminderStatus `json:"status"`
		SnoozedUntil *time.Time                  `json:"snoozed_until"`
		AssigneeID   string                      `json:"assignee_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid reminder body"))
		return
	}
	row, err := h.service.UpdateReminderStatus(c.Request.Context(), tenant, c.Param("id"), body.Status, body.SnoozedUntil, body.AssigneeID)
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}
func (h *SmartArchiveHandler) DeleteReminder(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	if err := h.service.DeleteReminder(c.Request.Context(), tenant, c.Param("id")); err != nil {
		archiveError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *SmartArchiveHandler) BatchDeleteReminders(c *gin.Context) {
	_, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid reminder ids"))
		return
	}
	result, err := h.service.BatchDeleteReminders(c.Request.Context(), tenant, body.IDs)
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
func (h *SmartArchiveHandler) Notifications(c *gin.Context) {
	user, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	rows, err := h.service.ListNotifications(c.Request.Context(), tenant, user, c.Query("unread") == "true")
	if err != nil {
		archiveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}
func (h *SmartArchiveHandler) MarkNotificationRead(c *gin.Context) {
	user, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	if err := h.service.MarkNotificationRead(c.Request.Context(), tenant, user, c.Param("id")); err != nil {
		archiveError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *SmartArchiveHandler) DeleteNotification(c *gin.Context) {
	user, tenant, ok := archiveContext(c)
	if !ok {
		return
	}
	if err := h.service.DeleteNotification(c.Request.Context(), tenant, user, c.Param("id")); err != nil {
		archiveError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
