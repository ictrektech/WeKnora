package router

import (
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/gin-gonic/gin"
)

func RegisterSmartArchiveRoutes(r *gin.RouterGroup, h *handler.SmartArchiveHandler, g *rbacGuards) {
	archive := r.Group("/archive", legalWorkspaceEnabled())
	archive.GET("/settings", g.Viewer(), h.Settings)
	archive.PATCH("/settings", g.Contributor(), h.UpdateSettings)
	archive.POST("/import-batches", g.Contributor(), h.Import)
	archive.GET("/import-batches/:id", g.Viewer(), h.Batch)
	archive.GET("/import-batches/:id/events", g.Viewer(), h.BatchEvents)
	archive.GET("/documents", g.Viewer(), h.ListDocuments)
	archive.POST("/documents/bulk/archive", g.Contributor(), h.BatchArchiveDocuments)
	archive.POST("/documents/bulk/restore", g.Admin(), h.BatchRestoreDocuments)
	archive.POST("/documents/bulk/delete", g.Admin(), h.BatchDeleteDocuments)
	archive.POST("/documents/bulk/purge", g.Admin(), h.BatchPurgeDocuments)
	archive.GET("/documents/:id", g.Viewer(), h.GetDocument)
	archive.PATCH("/documents/:id", g.Contributor(), h.UpdateDocument)
	archive.POST("/documents/:id/retry-extraction", g.Contributor(), h.RetryExtraction)
	archive.POST("/documents/:id/retry-mirror", g.Contributor(), h.RetryMirror)
	archive.POST("/documents/:id/archive", g.Contributor(), h.ArchiveDocument)
	archive.POST("/documents/:id/restore", g.Admin(), h.RestoreDocument)
	archive.DELETE("/documents/:id", g.Admin(), h.DeleteDocument)
	archive.GET("/documents/:id/evidence", g.Viewer(), h.Evidence)
	archive.GET("/documents/:id/preview", g.Viewer(), h.Preview)
	archive.GET("/customers", g.Viewer(), h.Customers)
	archive.PATCH("/customers/:id", g.Contributor(), h.UpdateCustomer)
	archive.POST("/search", g.Viewer(), h.Search)
	archive.GET("/reminders", g.Viewer(), h.Reminders)
	archive.GET("/reminder-candidates", g.Viewer(), h.ReminderCandidates)
	archive.POST("/reminder-candidates/bulk/ignore", g.Contributor(), h.BatchIgnoreReminderCandidates)
	archive.POST("/reminder-candidates/:id/create", g.Contributor(), h.CreateReminderFromCandidate)
	archive.POST("/reminders", g.Contributor(), h.CreateReminder)
	archive.PATCH("/reminders/:id", g.Contributor(), h.UpdateReminder)
	archive.DELETE("/reminders/:id", g.Contributor(), h.DeleteReminder)
	archive.POST("/reminders/bulk/delete", g.Contributor(), h.BatchDeleteReminders)
	archive.GET("/notifications", g.Viewer(), h.Notifications)
	archive.POST("/notifications/:id/read", g.Viewer(), h.MarkNotificationRead)
	archive.DELETE("/notifications/:id", g.Viewer(), h.DeleteNotification)
}
