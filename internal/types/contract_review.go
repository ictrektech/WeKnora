package types

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ContractReviewStatus string
type ContractReviewRiskLevel string
type ContractReviewParty string
type ContractReviewBulkAction string

const (
	ContractReviewStatusDraft            ContractReviewStatus = "draft"
	ContractReviewStatusUploading        ContractReviewStatus = "uploading"
	ContractReviewStatusReady            ContractReviewStatus = "ready"
	ContractReviewStatusAnalyzing        ContractReviewStatus = "analyzing"
	ContractReviewStatusReviewingClauses ContractReviewStatus = "reviewing_clauses"
	ContractReviewStatusCompleted        ContractReviewStatus = "completed"
	ContractReviewStatusFailed           ContractReviewStatus = "failed"

	ContractReviewRiskHigh   ContractReviewRiskLevel = "high"
	ContractReviewRiskMedium ContractReviewRiskLevel = "medium"
	ContractReviewRiskLow    ContractReviewRiskLevel = "low"

	ContractReviewPartyCustomer ContractReviewParty = "customer"
	ContractReviewPartyVendor   ContractReviewParty = "vendor"
	ContractReviewPartyNeutral  ContractReviewParty = "neutral"

	ContractReviewBulkArchive ContractReviewBulkAction = "archive"
	ContractReviewBulkRestore ContractReviewBulkAction = "restore"
	ContractReviewBulkDelete  ContractReviewBulkAction = "delete"
)

type ContractReviewBulkItem struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type ContractReviewBulkResult struct {
	Action    ContractReviewBulkAction `json:"action"`
	Requested int                      `json:"requested"`
	Succeeded int                      `json:"succeeded"`
	Failed    int                      `json:"failed"`
	Items     []ContractReviewBulkItem `json:"items"`
}

type ContractReview struct {
	ID               string                  `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID         uint64                  `json:"tenant_id" gorm:"not null;index"`
	UserID           string                  `json:"user_id" gorm:"type:varchar(64);not null;index"`
	Title            string                  `json:"title" gorm:"type:varchar(512);not null"`
	TitleCustomized  bool                    `json:"title_customized" gorm:"not null;default:false"`
	Status           ContractReviewStatus    `json:"status" gorm:"type:varchar(32);not null;index"`
	Progress         int                     `json:"progress" gorm:"not null;default:0"`
	PlaybookID       string                  `json:"playbook_id" gorm:"type:varchar(64);not null"`
	PlaybookVersion  string                  `json:"playbook_version" gorm:"type:varchar(32);not null"`
	RepresentedParty ContractReviewParty     `json:"represented_party" gorm:"type:varchar(16);not null"`
	ResourceRef      string                  `json:"-" gorm:"type:text"`
	FileName         string                  `json:"file_name" gorm:"type:varchar(1024);not null;default:''"`
	FileType         string                  `json:"file_type" gorm:"type:varchar(16);not null;default:''"`
	MimeType         string                  `json:"mime_type" gorm:"type:varchar(255);not null;default:''"`
	FileSize         int64                   `json:"file_size" gorm:"not null;default:0"`
	ExtractedContent string                  `json:"-" gorm:"type:text"`
	Metadata         JSON                    `json:"metadata" gorm:"type:jsonb;not null;default:'{}'"`
	Overview         JSON                    `json:"overview" gorm:"type:jsonb;not null;default:'{}'"`
	ErrorMessage     string                  `json:"error_message,omitempty" gorm:"type:text"`
	ArchivedAt       *time.Time              `json:"archived_at,omitempty" gorm:"index"`
	StartedAt        *time.Time              `json:"started_at,omitempty"`
	CompletedAt      *time.Time              `json:"completed_at,omitempty"`
	CreatedAt        time.Time               `json:"created_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
	DeletedAt        gorm.DeletedAt          `json:"-" gorm:"index"`
	Clauses          []*ContractReviewClause `json:"clauses,omitempty" gorm:"foreignKey:ReviewID"`
	Issues           []*ContractReviewIssue  `json:"issues,omitempty" gorm:"foreignKey:ReviewID"`
}

func (r *ContractReview) BeforeCreate(_ *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.Title == "" {
		r.Title = "Untitled Review"
	}
	if r.Status == "" {
		r.Status = ContractReviewStatusDraft
	}
	if r.PlaybookID == "" {
		r.PlaybookID = "general-contract-review"
	}
	if r.PlaybookVersion == "" {
		r.PlaybookVersion = "1.0"
	}
	if r.RepresentedParty == "" {
		r.RepresentedParty = ContractReviewPartyNeutral
	}
	if len(r.Metadata) == 0 {
		r.Metadata = JSON(`{}`)
	}
	if len(r.Overview) == 0 {
		r.Overview = JSON(`{}`)
	}
	return nil
}

type ContractReviewClause struct {
	ID           string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	ReviewID     string    `json:"review_id" gorm:"type:varchar(36);not null;index"`
	Sequence     int       `json:"sequence" gorm:"not null"`
	Title        string    `json:"title" gorm:"type:varchar(512);not null"`
	Excerpt      string    `json:"excerpt" gorm:"type:text"`
	SourceStart  int       `json:"source_start" gorm:"not null;default:0"`
	SourceEnd    int       `json:"source_end" gorm:"not null;default:0"`
	ReviewStatus string    `json:"review_status" gorm:"type:varchar(16);not null;default:'pending'"`
	IssueCount   int       `json:"issue_count" gorm:"not null;default:0"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (c *ContractReviewClause) BeforeCreate(_ *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.ReviewStatus == "" {
		c.ReviewStatus = "pending"
	}
	return nil
}

type ContractReviewIssue struct {
	ID            string                  `json:"id" gorm:"type:varchar(36);primaryKey"`
	ReviewID      string                  `json:"review_id" gorm:"type:varchar(36);not null;index"`
	ClauseID      string                  `json:"clause_id" gorm:"type:varchar(36);not null;index"`
	Fingerprint   string                  `json:"-" gorm:"type:varchar(64);not null;uniqueIndex"`
	Sequence      int                     `json:"sequence" gorm:"not null"`
	RiskLevel     ContractReviewRiskLevel `json:"risk_level" gorm:"type:varchar(16);not null"`
	Title         string                  `json:"title" gorm:"type:varchar(512);not null"`
	Explanation   string                  `json:"explanation" gorm:"type:text;not null"`
	OriginalQuote string                  `json:"original_quote" gorm:"type:text;not null"`
	Suggestion    string                  `json:"suggestion" gorm:"type:text;not null"`
	SourceStart   int                     `json:"source_start" gorm:"not null;default:0"`
	SourceEnd     int                     `json:"source_end" gorm:"not null;default:0"`
	CreatedAt     time.Time               `json:"created_at"`
	UpdatedAt     time.Time               `json:"updated_at"`
}

func (i *ContractReviewIssue) BeforeCreate(_ *gorm.DB) error {
	if i.ID == "" {
		i.ID = uuid.NewString()
	}
	return nil
}

type ContractReviewPlaybook struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

type ContractReviewTaskPayload struct {
	TenantID uint64 `json:"tenant_id"`
	UserID   string `json:"user_id"`
	ReviewID string `json:"review_id"`
}
