package types

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// DocumentParseArtifact is the durable, format-neutral output of a document
// reader/OCR pass. Importers may hand the artifact to downstream consumers
// such as knowledge indexing and domain extraction without parsing the source
// bytes again.
type DocumentParseArtifact struct {
	ID               string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID         uint64    `json:"tenant_id" gorm:"not null;index"`
	SourceDocumentID string    `json:"source_document_id" gorm:"type:varchar(36);not null;default:'';index"`
	FileHash         string    `json:"file_hash" gorm:"type:varchar(64);not null;uniqueIndex:idx_document_parse_artifacts_fingerprint,priority:1"`
	FileName         string    `json:"file_name" gorm:"type:varchar(1024);not null;default:''"`
	FileType         string    `json:"file_type" gorm:"type:varchar(32);not null;default:''"`
	ParserVersion    string    `json:"parser_version" gorm:"type:varchar(64);not null;uniqueIndex:idx_document_parse_artifacts_fingerprint,priority:2"`
	MarkdownContent  string    `json:"markdown_content" gorm:"type:text;not null"`
	Result           JSON      `json:"result" gorm:"type:jsonb;not null;default:'{}'"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (a *DocumentParseArtifact) BeforeCreate(_ *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	if a.ParserVersion == "" {
		a.ParserVersion = DocumentParseArtifactVersion
	}
	return nil
}

const DocumentParseArtifactVersion = "unified-v1"
