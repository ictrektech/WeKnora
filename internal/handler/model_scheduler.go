package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/Tencent/WeKnora/internal/schedulergateway"
	"github.com/gin-gonic/gin"
)

// ListSchedulerModels exposes only model IDs, never the app binding credential.
// The current Scheduler supports /v1/models for ordinary model selectors.
func (h *ModelHandler) ListSchedulerModels(c *gin.Context) {
	if schedulergateway.Backend() != "scheduler" {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": []string{}, "enabled": false})
		return
	}
	client := schedulergateway.Client("", schedulergateway.Gateway, "", &http.Client{Timeout: 15 * time.Second})
	request, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, schedulergateway.Gateway+"/models", nil)
	response, err := client.Do(request)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "V-Scheduler model discovery unavailable; check the gateway binding"})
		return
	}
	defer response.Body.Close()
	var catalog struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&catalog) != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": "V-Scheduler model discovery failed"})
		return
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, model := range catalog.Data {
		if model.ID != "" && !seen[model.ID] {
			ids = append(ids, model.ID)
			seen[model.ID] = true
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": ids, "enabled": true, "base_url": schedulergateway.Gateway})
}
