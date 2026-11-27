package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (h *SystemHandler) GetDocReaderRouting(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	router, available := h.documentReader.(*docparser.ComputeRouter)
	if !available {
		c.JSON(http.StatusOK, gin.H{"available": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"available": true, "config": router.Config()})
}

func (h *SystemHandler) SaveDocReaderRouting(c *gin.Context) {
	router, available := h.documentReader.(*docparser.ComputeRouter)
	if !available {
		c.JSON(http.StatusNotFound, gin.H{"message": "DocReader routing is unavailable"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	var config docparser.ComputeRoutingConfig
	if c.ShouldBindJSON(&config) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid DocReader routing configuration"})
		return
	}
	switch err := router.Save(config); {
	case errors.Is(err, docparser.ErrInvalidComputeRouting):
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
	case errors.Is(err, docparser.ErrComputeRoutingConflict):
		c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
	case err != nil:
		logger.Errorf(c.Request.Context(), "Could not persist DocReader routing: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Could not save DocReader routing"})
	default:
		c.JSON(http.StatusOK, gin.H{"available": true, "config": router.Config()})
	}
}

func (h *SystemHandler) ProbeDocReaderNode(c *gin.Context) {
	router, available := h.documentReader.(*docparser.ComputeRouter)
	if !available {
		c.JSON(http.StatusNotFound, gin.H{"message": "DocReader routing is unavailable"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<10)
	var node docparser.ComputeNode
	if c.ShouldBindJSON(&node) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid DocReader node configuration"})
		return
	}
	info, err := router.Probe(c.Request.Context(), node)
	switch {
	case errors.Is(err, docparser.ErrInvalidComputeRouting):
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
	case status.Code(err) == codes.Unauthenticated || status.Code(err) == codes.PermissionDenied:
		c.JSON(http.StatusBadGateway, gin.H{"message": "DocReader rejected the node credential"})
	case err != nil:
		c.JSON(http.StatusBadGateway, gin.H{"message": "DocReader probe failed. Check the VOS HTTPS address, certificate and service status."})
	default:
		c.JSON(http.StatusOK, info)
	}
}

func (h *SystemHandler) PairDocReaderNode(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	router, available := h.documentReader.(*docparser.ComputeRouter)
	if !available {
		c.JSON(http.StatusNotFound, gin.H{"message": "DocReader routing is unavailable"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	var req struct {
		Code          string `json:"code"`
		Revision      uint64 `json:"revision"`
		EnableIfEmpty bool   `json:"enable_if_empty"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid DocReader access code request"})
		return
	}
	info, err := router.PairNode(c.Request.Context(), req.Code, req.Revision, req.EnableIfEmpty)
	switch {
	case errors.Is(err, docparser.ErrInvalidComputeRouting):
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
	case errors.Is(err, docparser.ErrComputeRoutingConflict):
		c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
	case status.Code(err) == codes.Unauthenticated || status.Code(err) == codes.PermissionDenied:
		c.JSON(http.StatusBadGateway, gin.H{"message": "The node is not sharing DocReader, or the access code expired or was already used"})
	case err != nil:
		c.JSON(http.StatusBadGateway, gin.H{"message": "Could not pair or save the node. Check the remote VOS HTTPS address and generate a new code."})
	default:
		c.JSON(http.StatusOK, gin.H{"config": router.Config(), "node": info})
	}
}

func (h *SystemHandler) docReaderSharing() *docparser.ComputeSharing {
	if routing, ok := h.documentReader.(*docparser.ComputeRouter); ok {
		return routing.Sharing
	}
	return nil
}

func (h *SystemHandler) ListComputeTextModels(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	router, available := h.documentReader.(*docparser.ComputeRouter)
	if !available {
		c.JSON(http.StatusOK, gin.H{"available": false, "nodes": []docparser.ComputeTextNodeModels{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"available": true, "nodes": router.ListDiscoveredTextModels()})
}

func (h *SystemHandler) GetDocReaderSharing(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	sharing := h.docReaderSharing()
	if sharing == nil {
		c.JSON(http.StatusOK, gin.H{"available": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"available": true, "sharing": sharing.Status()})
}

func (h *SystemHandler) UpdateDocReaderSharing(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	sharing := h.docReaderSharing()
	if sharing == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "DocReader sharing is unavailable"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	var req struct {
		Enabled            *bool     `json:"enabled"`
		Revoke             bool      `json:"revoke"`
		DocumentProcessing *bool     `json:"document_processing"`
		DocumentModelID    *string   `json:"document_model_id"`
		TextInference      *bool     `json:"text_inference"`
		TextModelIDs       *[]string `json:"text_model_ids"`
	}
	if c.ShouldBindJSON(&req) != nil ||
		(req.Enabled == nil && !req.Revoke && req.DocumentProcessing == nil && req.DocumentModelID == nil &&
			req.TextInference == nil && req.TextModelIDs == nil) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Specify a sharing change"})
		return
	}
	if req.DocumentProcessing != nil && *req.DocumentProcessing && req.DocumentModelID == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": docparser.ErrComputeDocumentModelRequired.Error()})
		return
	}
	ctx := c.Request.Context()
	var binding *docparser.ComputeDocumentModel
	if req.DocumentModelID != nil {
		tenantID, ok := types.TenantIDFromContext(ctx)
		modelID := strings.TrimSpace(*req.DocumentModelID)
		if !ok || tenantID == 0 || modelID == "" || len(modelID) > types.ModelIDMaxLen || h.modelSvc == nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Select a vision model in the current workspace"})
			return
		}
		selected := docparser.ComputeDocumentModel{TenantID: tenantID, ModelID: modelID}
		if _, err := service.ResolveDocumentComputeVisionModel(ctx, h.modelSvc, selected); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "The selected vision model is unavailable"})
			return
		}
		binding = &selected
	}
	var textBindings []docparser.ComputeTextModel
	if req.TextModelIDs != nil {
		tenantID, ok := types.TenantIDFromContext(ctx)
		if !ok || tenantID == 0 || h.modelSvc == nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Select chat models in the current workspace"})
			return
		}
		textBindings = make([]docparser.ComputeTextModel, 0, len(*req.TextModelIDs))
		seen := make(map[string]bool, len(*req.TextModelIDs))
		for _, rawID := range *req.TextModelIDs {
			modelID := strings.TrimSpace(rawID)
			if modelID == "" || len(modelID) > types.ModelIDMaxLen {
				c.JSON(http.StatusBadRequest, gin.H{"message": "Select valid chat models in the current workspace"})
				return
			}
			if seen[modelID] {
				continue
			}
			seen[modelID] = true
			selected := docparser.ComputeTextModel{TenantID: tenantID, ModelID: modelID}
			if _, err := service.ResolveComputeTextModel(ctx, h.modelSvc, selected); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": "One or more selected chat models are unavailable"})
				return
			}
			textBindings = append(textBindings, selected)
		}
	}
	err := sharing.UpdateSharing(req.Enabled, req.DocumentProcessing, binding, req.TextInference, textBindings, req.Revoke)
	if errors.Is(err, docparser.ErrComputeDocumentModelRequired) || errors.Is(err, docparser.ErrComputeTextModelRequired) {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err != nil {
		logger.Errorf(c.Request.Context(), "Could not persist DocReader sharing: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Could not save DocReader sharing"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"available": true, "sharing": sharing.Status()})
}

func (h *SystemHandler) IssueDocReaderPairingCode(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	sharing := h.docReaderSharing()
	if sharing == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "DocReader sharing is unavailable"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<10)
	var req struct {
		Origin string `json:"origin"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid DocReader access code request"})
		return
	}
	code, err := sharing.IssuePairingCode(c.Request.Context(), req.Origin)
	switch {
	case errors.Is(err, docparser.ErrInvalidComputeRouting):
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
	case status.Code(err) == codes.FailedPrecondition:
		c.JSON(http.StatusConflict, gin.H{"message": "Enable DocReader sharing and ensure space for another paired node"})
	case err != nil:
		c.JSON(http.StatusBadGateway, gin.H{"message": "Could not inspect the local VOS HTTPS gateway"})
	default:
		c.JSON(http.StatusOK, code)
	}
}
