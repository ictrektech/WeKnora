package handler

import (
	"net/http"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/patentexamples"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// PatentExamples serves embedded recordings, with no connection to sessions, retrieval or models.
func PatentExamples(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	catalog, err := patentexamples.Load()
	if err != nil {
		c.Error(apperrors.NewInternalServerError("patent examples are unavailable"))
		return
	}
	id := c.Param("example_id")
	if id == "" {
		mode := types.WorkspaceMode(c.Query("workspace_mode"))
		if mode != "" && !mode.IsPatent() {
			c.Error(apperrors.NewBadRequestError("invalid patent workspace mode"))
			return
		}
		entries := make([]patentexamples.Example, 0)
		for _, example := range catalog.Examples {
			if mode == "" || example.Mode == mode {
				example.Messages = nil
				entries = append(entries, example)
			}
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "data": patentexamples.Catalog{Version: catalog.Version, Examples: entries}})
		return
	}
	example, err := catalog.Find(id)
	if err != nil {
		c.Error(apperrors.NewNotFoundError("patent example not found"))
		return
	}
	if docID := c.Param("document_id"); docID != "" {
		doc, content, err := example.ReadDocument(docID)
		if err != nil {
			c.Error(apperrors.NewNotFoundError("example document not found"))
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"id": doc.ID, "filename": doc.Filename, "content": content, "document": doc}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": example})
}
