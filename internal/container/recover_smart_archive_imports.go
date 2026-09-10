package container

import (
	"context"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// recoverPendingSmartArchiveImports re-arms durable archive import items only
// after the task handlers have been registered. It is intentionally
// best-effort: a Redis/SQLite outage must not prevent the HTTP server from
// starting, and the next restart/compensation pass can retry the wake-up.
func recoverPendingSmartArchiveImports(svc interfaces.SmartArchiveService) {
	if svc == nil {
		return
	}
	if err := svc.RecoverPendingImports(context.Background()); err != nil {
		logger.Warnf(context.Background(), "[SmartArchive] pending import recovery failed: %v", err)
	}
}
