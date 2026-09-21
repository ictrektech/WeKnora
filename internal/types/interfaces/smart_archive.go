package interfaces

import (
	"context"
	"io"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
)

// ArchiveRepository is the tenant-scoped persistence boundary for Smart
// Archive. Import item terminal methods update the item and its batch counters
// in one transaction; callers must not maintain those counters themselves.
type ArchiveRepository interface {
	GetSettings(context.Context, uint64) (*types.ArchiveSettings, error)
	SaveSettings(context.Context, *types.ArchiveSettings) error
	CreateBatch(context.Context, *types.ArchiveImportBatch) error
	GetBatch(context.Context, uint64, string) (*types.ArchiveImportBatch, error)
	UpdateBatch(context.Context, *types.ArchiveImportBatch) error

	CreateImportItem(context.Context, *types.ArchiveImportItem) error
	GetImportItem(context.Context, uint64, string) (*types.ArchiveImportItem, error)
	GetImportItemByFingerprint(context.Context, uint64, string, string) (*types.ArchiveImportItem, error)
	UpdateImportItem(context.Context, *types.ArchiveImportItem) error
	ListPendingImportItems(context.Context, time.Time, int) ([]*types.ArchiveImportItem, error)
	ClaimImportItem(context.Context, uint64, string, string, time.Time) (*types.ArchiveImportItem, error)
	// MarkImportItemCompleted and MarkImportItemFailed atomically change item
	// state and reconcile ArchiveImportBatch.Completed/Failed and Status.
	MarkImportItemCompleted(context.Context, uint64, string, string) error
	MarkImportItemFailed(context.Context, uint64, string, string, *time.Time) error

	CreateDocument(context.Context, *types.ArchiveDocument) error
	GetDocument(context.Context, uint64, string) (*types.ArchiveDocument, error)
	ListDocuments(context.Context, uint64, string, bool) ([]*types.ArchiveDocument, error)
	UpdateDocument(context.Context, *types.ArchiveDocument) error
	DeleteDocument(context.Context, uint64, string) error
	FindDocumentByHash(context.Context, uint64, string, string) (*types.ArchiveDocument, error)

	CreateCustomer(context.Context, *types.ArchiveCustomer) error
	FindCustomer(context.Context, uint64, string) (*types.ArchiveCustomer, error)
	ListCustomers(context.Context, uint64, string) ([]*types.ArchiveCustomer, error)
	UpdateCustomer(context.Context, *types.ArchiveCustomer) error

	CreateDocumentLink(context.Context, *types.ArchiveDocumentLink) error
	ListDocumentLinks(context.Context, uint64, string) ([]*types.ArchiveDocumentLink, error)
	ReplaceEvidence(context.Context, uint64, string, []*types.ArchiveFieldEvidence) error
	ListEvidence(context.Context, uint64, string) ([]*types.ArchiveFieldEvidence, error)

	CreateReminder(context.Context, *types.ArchiveReminder) error
	GetReminder(context.Context, uint64, string) (*types.ArchiveReminder, error)
	ListReminders(context.Context, uint64, string) ([]*types.ArchiveReminder, error)
	UpdateReminder(context.Context, *types.ArchiveReminder) error
	DeleteReminder(context.Context, uint64, string) error
	ListDueReminders(context.Context, int) ([]*types.ArchiveReminder, error)
	CreateOccurrence(context.Context, *types.ArchiveReminderOccurrence) (bool, error)
	NextReminderWakeAt(context.Context) (*time.Time, error)
	DeliverReminder(context.Context, *types.ArchiveReminder, *types.ArchiveReminderOccurrence, *types.ArchiveNotification) error

	ListTrashedDocuments(context.Context) ([]*types.ArchiveDocument, error)
	ClaimMirrorDocument(context.Context, uint64, string) (*types.ArchiveDocument, error)
	ListPendingMirrorDocuments(context.Context, time.Time, int) ([]*types.ArchiveDocument, error)
	HardDeleteDocument(context.Context, uint64, string) error
	CreateNotification(context.Context, *types.ArchiveNotification) error
	ListNotifications(context.Context, uint64, string, bool) ([]*types.ArchiveNotification, error)
	MarkNotificationRead(context.Context, uint64, string, string) error
	DeleteNotification(context.Context, uint64, string, string) error
	DeleteReminderDeliveryArtifacts(context.Context, uint64, string) error

	ListReminderCandidates(context.Context, uint64, string) ([]*types.ArchiveReminderCandidate, error)
	GetReminderCandidate(context.Context, uint64, string) (*types.ArchiveReminderCandidate, error)
	UpsertReminderCandidate(context.Context, *types.ArchiveReminderCandidate) error
	UpdateReminderCandidate(context.Context, *types.ArchiveReminderCandidate) error
	IgnoreReminderCandidate(context.Context, uint64, string) error
	CreateReminderFromCandidate(context.Context, *types.ArchiveReminderCandidate, *types.ArchiveReminder) error

	Search(context.Context, uint64, *types.ArchiveSearchRequest) (*types.ArchiveSearchResponse, error)
}

type SmartArchiveService interface {
	GetSettings(context.Context, uint64) (*types.ArchiveSettings, error)
	UpdateSettings(context.Context, uint64, *types.ArchiveSettings) (*types.ArchiveSettings, error)
	Import(context.Context, uint64, string, []*types.ArchiveUpload) (*types.ArchiveImportBatch, error)
	GetBatch(context.Context, uint64, string) (*types.ArchiveImportBatch, error)
	GetDocument(context.Context, uint64, string) (*types.ArchiveDocument, error)
	ListDocuments(context.Context, uint64, string, bool) ([]*types.ArchiveDocument, error)
	UpdateDocument(context.Context, uint64, string, map[string]any) (*types.ArchiveDocument, error)
	RetryExtraction(context.Context, uint64, string, string) (*types.ArchiveDocument, error)
	RetryMirror(context.Context, uint64, string) (*types.ArchiveDocument, error)
	ArchiveDocument(context.Context, uint64, string, bool) (*types.ArchiveDocument, error)
	DeleteDocument(context.Context, uint64, string) error
	BatchDocumentAction(context.Context, uint64, []string, types.ArchiveBulkAction) (*types.ArchiveBulkActionResult, error)
	OpenDocument(context.Context, uint64, string) (io.ReadCloser, string, error)
	ListCustomers(context.Context, uint64, string) ([]*types.ArchiveCustomer, error)
	UpdateCustomer(context.Context, uint64, string, map[string]any) (*types.ArchiveCustomer, error)
	ListEvidence(context.Context, uint64, string) ([]*types.ArchiveFieldEvidence, error)
	Search(context.Context, uint64, *types.ArchiveSearchRequest) (*types.ArchiveSearchResponse, error)
	ListReminders(context.Context, uint64, string) ([]*types.ArchiveReminder, error)
	CreateReminder(context.Context, uint64, string, *types.ArchiveReminder) (*types.ArchiveReminder, error)
	UpdateReminderStatus(context.Context, uint64, string, types.ArchiveReminderStatus, *time.Time, string) (*types.ArchiveReminder, error)
	DeleteReminder(context.Context, uint64, string) error
	BatchDeleteReminders(context.Context, uint64, []string) (*types.ArchiveBulkActionResult, error)
	ListReminderCandidates(context.Context, uint64, string) ([]*types.ArchiveReminderCandidate, error)
	CreateReminderFromCandidate(context.Context, uint64, string, string, int, string, string) (*types.ArchiveReminder, error)
	BatchIgnoreReminderCandidates(context.Context, uint64, []string) (*types.ArchiveBulkActionResult, error)
	ListNotifications(context.Context, uint64, string, bool) ([]*types.ArchiveNotification, error)
	MarkNotificationRead(context.Context, uint64, string, string) error
	DeleteNotification(context.Context, uint64, string, string) error
	RunDueReminders(context.Context) error
	NextReminderWakeAt(context.Context) (*time.Time, error)
	ReminderWakeups() <-chan struct{}

	// ProcessDocument consumes one durable ArchiveImportTaskPayload from an
	// asynq task. RecoverPendingImports re-enqueues queued and expired leases at
	// startup; both operations are tenant-aware and idempotent at repository
	// boundaries.
	ProcessDocument(context.Context, *asynq.Task) error
	// ProcessMirror consumes one durable ArchiveMirrorTaskPayload. It only
	// creates or re-submits the derived managed-knowledge mirror.
	ProcessMirror(context.Context, *asynq.Task) error
	RecoverPendingImports(context.Context) error
	RecoverPendingMirrors(context.Context) error
}
