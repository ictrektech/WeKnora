package types

import (
	"io"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Smart Archive is intentionally separate from chat and Contract Review. The
// rows below are the durable source of truth for historical documents,
// extracted facts, entity links and reminders.
type ArchiveDocumentType string
type ArchiveBusinessType string
type ArchiveExtractionStatus string
type ArchiveLinkStatus string
type ArchiveReminderType string
type ArchiveReminderStatus string
type ArchiveReminderCandidateStatus string
type ArchiveReminderOccurrenceStatus string
type ArchiveSourceLocatorKind string
type ArchiveBulkAction string
type ArchiveMirrorStatus string

const ArchiveDefaultExtractionVersion = "1.0"

// ManagedSmartArchiveKnowledgeBaseMarker is kept in the description of the
// system-owned document KB. The regular knowledge-base service uses it to
// prevent a normal KB edit/delete request from breaking the archive's source
// of truth; archive imports remain the only supported write path.
const ManagedSmartArchiveKnowledgeBaseMarker = "[weknora-managed-smart-archive]"

const (
	ArchiveDocumentContract      ArchiveDocumentType = "contract"
	ArchiveDocumentLoanAgreement ArchiveDocumentType = "loan_agreement"
	ArchiveDocumentOutbound      ArchiveDocumentType = "outbound_order"
	ArchiveDocumentReturn        ArchiveDocumentType = "return_order"
	ArchiveDocumentRenewal       ArchiveDocumentType = "renewal"
	ArchiveDocumentPayment       ArchiveDocumentType = "payment"
	ArchiveDocumentDelivery      ArchiveDocumentType = "delivery"
	ArchiveDocumentOther         ArchiveDocumentType = "other"

	ArchiveBusinessLoan  ArchiveBusinessType = "loan"
	ArchiveBusinessSale  ArchiveBusinessType = "sale"
	ArchiveBusinessOther ArchiveBusinessType = "other"

	ArchiveExtractionUploading  ArchiveExtractionStatus = "uploading"
	ArchiveExtractionParsing    ArchiveExtractionStatus = "parsing"
	ArchiveExtractionExtracting ArchiveExtractionStatus = "extracting"
	ArchiveExtractionLinking    ArchiveExtractionStatus = "linking"
	ArchiveExtractionReview     ArchiveExtractionStatus = "needs_review"
	ArchiveExtractionCompleted  ArchiveExtractionStatus = "completed"
	ArchiveExtractionFailed     ArchiveExtractionStatus = "failed"

	ArchiveLinkPending   ArchiveLinkStatus = "pending"
	ArchiveLinkConfirmed ArchiveLinkStatus = "confirmed"
	ArchiveLinkRejected  ArchiveLinkStatus = "rejected"

	ArchiveReminderExpiry        ArchiveReminderType = "contract_expiry"
	ArchiveReminderReturn        ArchiveReminderType = "asset_return"
	ArchiveReminderPayment       ArchiveReminderType = "payment"
	ArchiveReminderDelivery      ArchiveReminderType = "delivery"
	ArchiveReminderRenewal       ArchiveReminderType = "renewal"
	ArchiveReminderMissingReturn ArchiveReminderType = "missing_return"

	ArchiveReminderDraft    ArchiveReminderStatus = "draft"
	ArchiveReminderActive   ArchiveReminderStatus = "active"
	ArchiveReminderSnoozed  ArchiveReminderStatus = "snoozed"
	ArchiveReminderHandled  ArchiveReminderStatus = "handled"
	ArchiveReminderCanceled ArchiveReminderStatus = "canceled"

	ArchiveReminderCandidatePending    ArchiveReminderCandidateStatus = "pending"
	ArchiveReminderCandidateCreated    ArchiveReminderCandidateStatus = "created"
	ArchiveReminderCandidateSuperseded ArchiveReminderCandidateStatus = "superseded"
	ArchiveReminderCandidateIgnored    ArchiveReminderCandidateStatus = "ignored"

	ArchiveOccurrencePending ArchiveReminderOccurrenceStatus = "pending"
	ArchiveOccurrenceSent    ArchiveReminderOccurrenceStatus = "sent"
	ArchiveOccurrenceSkipped ArchiveReminderOccurrenceStatus = "skipped"
	ArchiveOccurrenceFailed  ArchiveReminderOccurrenceStatus = "failed"

	ArchiveLocatorPDFPage     ArchiveSourceLocatorKind = "pdf_page"
	ArchiveLocatorDocx        ArchiveSourceLocatorKind = "docx_paragraph"
	ArchiveLocatorSpreadsheet ArchiveSourceLocatorKind = "spreadsheet_cell"
	// ArchiveLocatorImage identifies OCR evidence extracted from a standalone
	// image. Coordinates are optional; the archive currently guarantees only
	// the page and character range when the OCR provider does not return boxes.
	ArchiveLocatorImage ArchiveSourceLocatorKind = "image"
	ArchiveLocatorText  ArchiveSourceLocatorKind = "text"

	ArchiveBulkArchive ArchiveBulkAction = "archive"
	ArchiveBulkRestore ArchiveBulkAction = "restore"
	ArchiveBulkDelete  ArchiveBulkAction = "delete"
	ArchiveBulkPurge   ArchiveBulkAction = "purge"
	ArchiveBulkIgnore  ArchiveBulkAction = "ignore"

	ArchiveMirrorNotStarted ArchiveMirrorStatus = "not_started"
	ArchiveMirrorPending    ArchiveMirrorStatus = "pending"
	ArchiveMirrorProcessing ArchiveMirrorStatus = "processing"
	ArchiveMirrorSubmitted  ArchiveMirrorStatus = "submitted"
	ArchiveMirrorFailed     ArchiveMirrorStatus = "failed"
)

type ArchiveSettings struct {
	ID                     string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID               uint64    `json:"tenant_id" gorm:"not null;uniqueIndex"`
	ManagedKnowledgeBaseID string    `json:"managed_knowledge_base_id" gorm:"type:varchar(36);not null;default:''"`
	Timezone               string    `json:"timezone" gorm:"type:varchar(64);not null;default:'Asia/Shanghai'"`
	ExtractionModelID      string    `json:"extraction_model_id" gorm:"type:varchar(128);not null;default:''"`
	ExtractionVersion      string    `json:"extraction_version" gorm:"type:varchar(32);not null;default:'1.0'"`
	TrashRetentionDays     int       `json:"trash_retention_days" gorm:"not null;default:30"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

func (s *ArchiveSettings) BeforeCreate(_ *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	if s.Timezone == "" {
		s.Timezone = "Asia/Shanghai"
	}
	if s.ExtractionVersion == "" {
		s.ExtractionVersion = "1.0"
	}
	if s.TrashRetentionDays <= 0 {
		s.TrashRetentionDays = 30
	}
	return nil
}

type ArchiveImportBatch struct {
	ID        string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID  uint64    `json:"tenant_id" gorm:"not null;index"`
	UserID    string    `json:"user_id" gorm:"type:varchar(64);not null;index"`
	Total     int       `json:"total" gorm:"not null;default:0"`
	Completed int       `json:"completed" gorm:"not null;default:0"`
	Failed    int       `json:"failed" gorm:"not null;default:0"`
	Status    string    `json:"status" gorm:"type:varchar(24);not null;default:'processing';index"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (b *ArchiveImportBatch) BeforeCreate(_ *gorm.DB) error {
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	return nil
}

type ArchiveImportItemStatus string

const (
	ArchiveImportItemQueued     ArchiveImportItemStatus = "queued"
	ArchiveImportItemProcessing ArchiveImportItemStatus = "processing"
	ArchiveImportItemCompleted  ArchiveImportItemStatus = "completed"
	ArchiveImportItemFailed     ArchiveImportItemStatus = "failed"
	ArchiveImportItemCanceled   ArchiveImportItemStatus = "canceled"

	ArchiveImportBatchProcessing = "processing"
	ArchiveImportBatchCompleted  = "completed"
	ArchiveImportBatchFailed     = "failed"
)

// ArchiveImportItem is the durable unit of work behind an import batch.
// Its fingerprint is tenant-scoped and includes the parser/extraction
// version so a parser upgrade can intentionally reprocess the same bytes.
type ArchiveImportItem struct {
	ID                string                  `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID          uint64                  `json:"tenant_id" gorm:"not null;index"`
	BatchID           string                  `json:"batch_id" gorm:"type:varchar(36);not null;index"`
	DocumentID        string                  `json:"document_id,omitempty" gorm:"type:varchar(36);index"`
	FileName          string                  `json:"file_name" gorm:"type:varchar(1024);not null"`
	FileType          string                  `json:"file_type" gorm:"type:varchar(16);not null;default:''"`
	MimeType          string                  `json:"mime_type" gorm:"type:varchar(128);not null;default:''"`
	FileSize          int64                   `json:"file_size" gorm:"not null;default:0"`
	FileHash          string                  `json:"file_hash" gorm:"type:varchar(64);not null"`
	FilePath          string                  `json:"-" gorm:"type:text;not null"`
	ExtractionVersion string                  `json:"extraction_version" gorm:"type:varchar(32);not null;default:'1.0'"`
	Status            ArchiveImportItemStatus `json:"status" gorm:"type:varchar(24);not null;default:'queued';index"`
	AttemptCount      int                     `json:"attempt_count" gorm:"not null;default:0"`
	FailureCounted    bool                    `json:"-" gorm:"not null;default:false"`
	RunID             string                  `json:"run_id,omitempty" gorm:"type:varchar(64);not null;default:'';index"`
	AvailableAt       *time.Time              `json:"available_at,omitempty" gorm:"index"`
	ClaimedAt         *time.Time              `json:"claimed_at,omitempty"`
	LeaseUntil        *time.Time              `json:"lease_until,omitempty" gorm:"index"`
	CompletedAt       *time.Time              `json:"completed_at,omitempty"`
	ErrorMessage      string                  `json:"error_message,omitempty" gorm:"type:text"`
	CreatedAt         time.Time               `json:"created_at"`
	UpdatedAt         time.Time               `json:"updated_at"`
}

func (i *ArchiveImportItem) BeforeCreate(_ *gorm.DB) error {
	if i.ID == "" {
		i.ID = uuid.NewString()
	}
	if i.Status == "" {
		i.Status = ArchiveImportItemQueued
	}
	if i.ExtractionVersion == "" {
		i.ExtractionVersion = ArchiveDefaultExtractionVersion
	}
	return nil
}

// ArchiveImportTaskPayload is the JSON body enqueued on QueueDefault.
// ItemID is the durable idempotency key; RunID identifies one worker lease.
type ArchiveImportTaskPayload struct {
	TenantID          uint64 `json:"tenant_id"`
	BatchID           string `json:"batch_id"`
	ItemID            string `json:"item_id"`
	RunID             string `json:"run_id"`
	FileHash          string `json:"file_hash,omitempty"`
	ExtractionVersion string `json:"extraction_version,omitempty"`
}

// ArchiveMirrorTaskPayload carries only durable identifiers. The archive row
// and parse artifact remain the source of truth; the worker creates or
// re-submits the derived managed-knowledge mirror from those records.
type ArchiveMirrorTaskPayload struct {
	TenantID   uint64 `json:"tenant_id"`
	DocumentID string `json:"document_id"`
}

type ArchiveDocument struct {
	ID                 string                  `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID           uint64                  `json:"tenant_id" gorm:"not null;index"`
	ImportBatchID      string                  `json:"import_batch_id" gorm:"type:varchar(36);index"`
	KnowledgeID        string                  `json:"knowledge_id" gorm:"type:varchar(36);index"`
	Title              string                  `json:"title" gorm:"type:varchar(512);not null"`
	FileName           string                  `json:"file_name" gorm:"type:varchar(1024);not null"`
	FileType           string                  `json:"file_type" gorm:"type:varchar(16);not null"`
	FileSize           int64                   `json:"file_size" gorm:"not null;default:0"`
	FileHash           string                  `json:"file_hash" gorm:"type:varchar(64);not null;index"`
	FilePath           string                  `json:"-" gorm:"type:text;not null"`
	DocumentType       ArchiveDocumentType     `json:"document_type" gorm:"type:varchar(32);not null;default:'other';index"`
	BusinessType       ArchiveBusinessType     `json:"business_type" gorm:"type:varchar(16);not null;default:'other';index"`
	CustomerID         string                  `json:"customer_id" gorm:"type:varchar(36);index"`
	AgreementNumber    string                  `json:"agreement_number" gorm:"type:varchar(256);index"`
	SignedAt           *time.Time              `json:"signed_at,omitempty"`
	EffectiveAt        *time.Time              `json:"effective_at,omitempty"`
	ExpiresAt          *time.Time              `json:"expires_at,omitempty"`
	ReturnDueAt        *time.Time              `json:"return_due_at,omitempty" gorm:"-"`
	ReturnedAt         *time.Time              `json:"returned_at,omitempty"`
	RenewedAt          *time.Time              `json:"renewed_at,omitempty"`
	Amount             float64                 `json:"amount"`
	Currency           string                  `json:"currency" gorm:"type:varchar(16)"`
	ExtractedText      string                  `json:"-" gorm:"type:text"`
	ExtractedFields    JSON                    `json:"extracted_fields" gorm:"type:json"`
	Metadata           JSON                    `json:"metadata" gorm:"type:json"`
	ExtractionStatus   ArchiveExtractionStatus `json:"extraction_status" gorm:"type:varchar(24);not null;default:'uploading';index"`
	ExtractionVersion  string                  `json:"extraction_version" gorm:"type:varchar(32);not null;default:'1.0'"`
	ErrorMessage       string                  `json:"error_message,omitempty" gorm:"type:text"`
	MirrorStatus       ArchiveMirrorStatus     `json:"mirror_status" gorm:"type:varchar(24);not null;default:'not_started';index"`
	MirrorErrorMessage string                  `json:"mirror_error_message,omitempty" gorm:"type:text"`
	ArchivedAt         *time.Time              `json:"archived_at,omitempty" gorm:"index"`
	TrashedAt          *time.Time              `json:"trashed_at,omitempty" gorm:"index"`
	CreatedBy          string                  `json:"created_by" gorm:"type:varchar(64);not null;index"`
	CreatedAt          time.Time               `json:"created_at"`
	UpdatedAt          time.Time               `json:"updated_at"`
	Customer           *ArchiveCustomer        `json:"customer,omitempty" gorm:"-"`
	Links              []*ArchiveDocumentLink  `json:"links,omitempty" gorm:"-"`
	Evidence           []*ArchiveFieldEvidence `json:"evidence,omitempty" gorm:"-"`
}

func (d *ArchiveDocument) BeforeCreate(_ *gorm.DB) error {
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	if d.DocumentType == "" {
		d.DocumentType = ArchiveDocumentOther
	}
	if d.BusinessType == "" {
		d.BusinessType = ArchiveBusinessOther
	}
	if d.ExtractionStatus == "" {
		d.ExtractionStatus = ArchiveExtractionUploading
	}
	if d.MirrorStatus == "" {
		d.MirrorStatus = ArchiveMirrorNotStarted
	}
	if d.ExtractionVersion == "" {
		d.ExtractionVersion = "1.0"
	}
	if len(d.ExtractedFields) == 0 {
		d.ExtractedFields = JSON(`{}`)
	}
	if len(d.Metadata) == 0 {
		d.Metadata = JSON(`{}`)
	}
	return nil
}

type ArchiveCustomer struct {
	ID         string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID   uint64    `json:"tenant_id" gorm:"not null;index"`
	Name       string    `json:"name" gorm:"type:varchar(512);not null"`
	Normalized string    `json:"normalized" gorm:"type:varchar(512);not null"`
	Aliases    JSON      `json:"aliases" gorm:"type:json"`
	Notes      string    `json:"notes" gorm:"type:text"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (c *ArchiveCustomer) BeforeCreate(_ *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if len(c.Aliases) == 0 {
		c.Aliases = JSON(`[]`)
	}
	return nil
}

type ArchiveDocumentLink struct {
	ID             string            `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID       uint64            `json:"tenant_id" gorm:"not null;index"`
	FromDocumentID string            `json:"from_document_id" gorm:"type:varchar(36);not null;index"`
	ToDocumentID   string            `json:"to_document_id" gorm:"type:varchar(36);not null;index"`
	Relation       string            `json:"relation" gorm:"type:varchar(32);not null"`
	LinkStatus     ArchiveLinkStatus `json:"link_status" gorm:"type:varchar(16);not null;default:'pending'"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

func (l *ArchiveDocumentLink) BeforeCreate(_ *gorm.DB) error {
	if l.ID == "" {
		l.ID = uuid.NewString()
	}
	if l.LinkStatus == "" {
		l.LinkStatus = ArchiveLinkPending
	}
	return nil
}

type ArchiveFieldEvidence struct {
	ID          string                   `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID    uint64                   `json:"tenant_id" gorm:"not null;index"`
	DocumentID  string                   `json:"document_id" gorm:"type:varchar(36);not null;index"`
	KnowledgeID string                   `json:"knowledge_id,omitempty" gorm:"type:varchar(36);index"`
	ChunkID     string                   `json:"chunk_id,omitempty" gorm:"type:varchar(36);index"`
	FieldName   string                   `json:"field_name" gorm:"type:varchar(128);not null;index"`
	Value       string                   `json:"value" gorm:"type:text;not null"`
	Confidence  float64                  `json:"confidence" gorm:"not null;default:0"`
	Quote       string                   `json:"quote" gorm:"type:text;not null"`
	LocatorKind ArchiveSourceLocatorKind `json:"locator_kind" gorm:"type:varchar(32);not null;default:'text'"`
	Locator     JSON                     `json:"locator" gorm:"type:json;not null"`
	SourceStart int                      `json:"source_start" gorm:"not null;default:0"`
	SourceEnd   int                      `json:"source_end" gorm:"not null;default:0"`
	IsManual    bool                     `json:"is_manual" gorm:"not null;default:false"`
	CreatedAt   time.Time                `json:"created_at"`
	UpdatedAt   time.Time                `json:"updated_at"`
}

// TableName keeps GORM aligned with the singular table name used by both the
// PostgreSQL and SQLite smart-archive migrations. Without this override GORM
// pluralizes Evidence as "evidences" and every hydrated document fails.
func (ArchiveFieldEvidence) TableName() string { return "archive_field_evidence" }

func (e *ArchiveFieldEvidence) BeforeCreate(_ *gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.LocatorKind == "" {
		e.LocatorKind = ArchiveLocatorText
	}
	if len(e.Locator) == 0 {
		e.Locator = JSON(`{}`)
	}
	return nil
}

type ArchiveReminder struct {
	ID               string                `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID         uint64                `json:"tenant_id" gorm:"not null;index"`
	DocumentID       string                `json:"document_id" gorm:"type:varchar(36);index"`
	CustomerID       string                `json:"customer_id" gorm:"type:varchar(36);index"`
	AssigneeID       string                `json:"assignee_id" gorm:"type:varchar(64);not null;index"`
	Type             ArchiveReminderType   `json:"type" gorm:"type:varchar(32);not null;index"`
	Title            string                `json:"title" gorm:"type:varchar(512);not null"`
	Description      string                `json:"description" gorm:"type:text"`
	Rule             JSON                  `json:"rule" gorm:"type:json;not null"`
	Status           ArchiveReminderStatus `json:"status" gorm:"type:varchar(16);not null;default:'draft';index"`
	Confidence       float64               `json:"confidence" gorm:"not null;default:0"`
	DueAt            *time.Time            `json:"due_at,omitempty" gorm:"index"`
	SnoozedUntil     *time.Time            `json:"snoozed_until,omitempty"`
	LastOccurrenceAt *time.Time            `json:"last_occurrence_at,omitempty"`
	CreatedBy        string                `json:"created_by" gorm:"type:varchar(64);not null"`
	CreatedAt        time.Time             `json:"created_at"`
	UpdatedAt        time.Time             `json:"updated_at"`
}

// ArchiveReminderCandidate is an extracted reminder suggestion. Candidates
// are never scanned by the scheduler; a user must explicitly create a formal
// ArchiveReminder from one after confirming its rule and assignee.
type ArchiveReminderCandidate struct {
	ID                  string                         `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID            uint64                         `json:"tenant_id" gorm:"not null;index"`
	DocumentID          string                         `json:"document_id" gorm:"type:varchar(36);not null;index"`
	DocumentTitle       string                         `json:"document_title" gorm:"type:varchar(512);not null;default:''"`
	CustomerID          string                         `json:"customer_id" gorm:"type:varchar(36);index"`
	AssigneeID          string                         `json:"assignee_id" gorm:"type:varchar(64);not null;default:''"`
	Type                ArchiveReminderType            `json:"type" gorm:"type:varchar(32);not null;index"`
	SourceField         string                         `json:"source_field" gorm:"type:varchar(128);not null"`
	EventAt             time.Time                      `json:"event_at" gorm:"not null;index"`
	SuggestedOffsetDays int                            `json:"suggested_offset_days" gorm:"not null;default:0"`
	Title               string                         `json:"title" gorm:"type:varchar(512);not null"`
	Description         string                         `json:"description" gorm:"type:text"`
	Confidence          float64                        `json:"confidence" gorm:"not null;default:0"`
	Quote               string                         `json:"quote" gorm:"type:text"`
	Locator             JSON                           `json:"locator" gorm:"type:json;not null"`
	Rule                JSON                           `json:"rule" gorm:"type:json;not null"`
	NeedsReview         bool                           `json:"needs_review" gorm:"not null;default:false"`
	Status              ArchiveReminderCandidateStatus `json:"status" gorm:"type:varchar(16);not null;default:'pending';index"`
	ReminderID          string                         `json:"reminder_id,omitempty" gorm:"type:varchar(36);index"`
	Fingerprint         string                         `json:"fingerprint" gorm:"type:varchar(160);not null;uniqueIndex"`
	CreatedBy           string                         `json:"created_by" gorm:"type:varchar(64);not null"`
	CreatedAt           time.Time                      `json:"created_at"`
	UpdatedAt           time.Time                      `json:"updated_at"`
	Document            *ArchiveDocument               `json:"document,omitempty" gorm:"-"`
}

func (c *ArchiveReminderCandidate) BeforeCreate(_ *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.Status == "" {
		c.Status = ArchiveReminderCandidatePending
	}
	if len(c.Locator) == 0 {
		c.Locator = JSON(`{}`)
	}
	if len(c.Rule) == 0 {
		c.Rule = JSON(`{}`)
	}
	return nil
}

func (r *ArchiveReminder) BeforeCreate(_ *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.Status == "" {
		r.Status = ArchiveReminderDraft
	}
	if len(r.Rule) == 0 {
		r.Rule = JSON(`{}`)
	}
	return nil
}

type ArchiveReminderOccurrence struct {
	ID          string                          `json:"id" gorm:"type:varchar(36);primaryKey"`
	ReminderID  string                          `json:"reminder_id" gorm:"type:varchar(36);not null;index"`
	TenantID    uint64                          `json:"tenant_id" gorm:"not null;index"`
	Fingerprint string                          `json:"fingerprint" gorm:"type:varchar(128);not null;uniqueIndex"`
	DueAt       time.Time                       `json:"due_at"`
	Status      ArchiveReminderOccurrenceStatus `json:"status" gorm:"type:varchar(16);not null;default:'pending'"`
	CreatedAt   time.Time                       `json:"created_at"`
}

func (o *ArchiveReminderOccurrence) BeforeCreate(_ *gorm.DB) error {
	if o.ID == "" {
		o.ID = uuid.NewString()
	}
	if o.Status == "" {
		o.Status = ArchiveOccurrencePending
	}
	return nil
}

type ArchiveNotification struct {
	ID         string `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID   uint64 `json:"tenant_id" gorm:"not null;index"`
	UserID     string `json:"user_id" gorm:"type:varchar(64);not null;index"`
	ReminderID string `json:"reminder_id" gorm:"type:varchar(36);index"`
	// OccurrenceID binds the notification to the durable one-shot delivery
	// record.  It is intentionally separate from ReminderID because a reminder
	// may be delivered again after a future reschedule.  The unique database
	// index makes delivery idempotent across restarts and multiple instances.
	OccurrenceID string     `json:"occurrence_id,omitempty" gorm:"type:varchar(36);index"`
	Title        string     `json:"title" gorm:"type:varchar(512);not null"`
	Body         string     `json:"body" gorm:"type:text"`
	ReadAt       *time.Time `json:"read_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at" gorm:"index"`
}

// ArchiveBulkActionResult reports every requested item independently. A batch
// is intentionally not all-or-nothing: an item that is already deleted, in an
// incompatible state, or not visible to the tenant must not prevent the
// remaining safe operations from completing, and the UI can show the exact
// failures instead of silently dropping them.
type ArchiveBulkActionItem struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type ArchiveBulkActionResult struct {
	Action    ArchiveBulkAction       `json:"action"`
	Requested int                     `json:"requested"`
	Succeeded int                     `json:"succeeded"`
	Failed    int                     `json:"failed"`
	Items     []ArchiveBulkActionItem `json:"items"`
}

func (n *ArchiveNotification) BeforeCreate(_ *gorm.DB) error {
	if n.ID == "" {
		n.ID = uuid.NewString()
	}
	return nil
}

// ArchiveUpload is a transport-neutral upload payload. The handler owns the
// multipart file and closes it after the service has copied the bytes.
type ArchiveUpload struct {
	FileName string
	MimeType string
	Size     int64
	Reader   io.Reader
}

type ArchiveSearchFilters struct {
	DocumentType       string                    `json:"document_type,omitempty"`
	BusinessType       string                    `json:"business_type,omitempty"`
	CustomerID         string                    `json:"customer_id,omitempty"`
	Model              string                    `json:"model,omitempty"`
	SerialNumber       string                    `json:"serial_number,omitempty"`
	AgreementNumber    string                    `json:"agreement_number,omitempty"`
	From               *time.Time                `json:"from,omitempty"`
	To                 *time.Time                `json:"to,omitempty"`
	ImportedFrom       *time.Time                `json:"imported_from,omitempty"`
	ImportedTo         *time.Time                `json:"imported_to,omitempty"`
	ExtractionStatuses []ArchiveExtractionStatus `json:"extraction_statuses,omitempty"`
	Archived           *bool                     `json:"archived,omitempty"`
}

type ArchiveSearchRequest struct {
	Query    string               `json:"query"`
	Filters  ArchiveSearchFilters `json:"filters"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

type ArchiveSearchCitation struct {
	DocumentID string `json:"document_id"`
	FieldName  string `json:"field_name,omitempty"`
	Quote      string `json:"quote"`
	Locator    JSON   `json:"locator"`
}

type ArchiveSearchResponse struct {
	Answer    string                  `json:"answer"`
	Documents []*ArchiveDocument      `json:"documents"`
	Customers []*ArchiveCustomer      `json:"customers"`
	Citations []ArchiveSearchCitation `json:"citations"`
	Total     int64                   `json:"total"`
}
