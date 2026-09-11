package router

import (
	"github.com/Tencent/WeKnora/internal/handler/session"
	"github.com/gin-gonic/gin"
)

// RegisterLegalAssistantRoutes exposes the dedicated legal session creation
// surface to authenticated Web/JWT viewers. It is intentionally not wrapped
// in apiKeyGroup: legal sessions are a Web/JWT product surface, not an API-key
// chat capability.
func RegisterLegalAssistantRoutes(r *gin.RouterGroup, h *session.Handler, g *rbacGuards) {
	r.POST("/legal-assistant/sessions", legalWorkspaceEnabled(), g.Viewer(), h.CreateLegalAssistantSession)
}
