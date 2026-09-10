package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

type documentParseArtifactRepository struct{ db *gorm.DB }

func NewDocumentParseArtifactRepository(db *gorm.DB) interfaces.DocumentParseArtifactRepository {
	return &documentParseArtifactRepository{db: db}
}

func (r *documentParseArtifactRepository) GetByID(ctx context.Context, tenantID uint64, id string) (*types.DocumentParseArtifact, error) {
	var row types.DocumentParseArtifact
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(&row).Error
	return &row, err
}

func (r *documentParseArtifactRepository) GetByFingerprint(ctx context.Context, tenantID uint64, fileHash, parserVersion string) (*types.DocumentParseArtifact, error) {
	var row types.DocumentParseArtifact
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND file_hash = ? AND parser_version = ?", tenantID, fileHash, parserVersion).First(&row).Error
	return &row, err
}

func (r *documentParseArtifactRepository) Upsert(ctx context.Context, row *types.DocumentParseArtifact) error {
	if row == nil {
		return errors.New("document parse artifact is nil")
	}
	if row.ParserVersion == "" {
		row.ParserVersion = types.DocumentParseArtifactVersion
	}
	if row.Result == nil {
		row.Result = types.JSON(`{}`)
	}
	// Use an explicit lookup/update rather than relying on a dialect-specific
	// ON CONFLICT target. Some existing SQLite databases were created before
	// the composite unique index was introduced; the read-then-write path keeps
	// those upgrades compatible while the migration still adds the index for
	// new installations.
	var existing types.DocumentParseArtifact
	lookupErr := r.db.WithContext(ctx).Where("tenant_id = ? AND file_hash = ? AND parser_version = ?", row.TenantID, row.FileHash, row.ParserVersion).First(&existing).Error
	if lookupErr == nil {
		if err := r.db.WithContext(ctx).Model(&existing).Updates(map[string]any{
			"source_document_id": row.SourceDocumentID,
			"file_name":          row.FileName,
			"file_type":          row.FileType,
			"markdown_content":   row.MarkdownContent,
			"result":             row.Result,
			"updated_at":         gorm.Expr("CURRENT_TIMESTAMP"),
		}).Error; err != nil {
			return err
		}
		*row = existing
		row.SourceDocumentID, row.FileName, row.FileType, row.MarkdownContent, row.Result = existing.SourceDocumentID, existing.FileName, existing.FileType, existing.MarkdownContent, existing.Result
		return r.db.WithContext(ctx).First(row, "id = ?", existing.ID).Error
	}
	if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return lookupErr
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		// A concurrent writer may have won the fingerprint race. Reload it and
		// apply the same update, preserving one stable artifact ID.
		if existingErr := r.db.WithContext(ctx).Where("tenant_id = ? AND file_hash = ? AND parser_version = ?", row.TenantID, row.FileHash, row.ParserVersion).First(&existing).Error; existingErr != nil {
			return err
		}
		if updateErr := r.db.WithContext(ctx).Model(&existing).Updates(map[string]any{
			"source_document_id": row.SourceDocumentID,
			"file_name":          row.FileName,
			"file_type":          row.FileType,
			"markdown_content":   row.MarkdownContent,
			"result":             row.Result,
			"updated_at":         gorm.Expr("CURRENT_TIMESTAMP"),
		}).Error; updateErr != nil {
			return updateErr
		}
		*row = existing
		return nil
	}
	// A successful insert already has the durable ID. Reloading by the unique
	// fingerprint is useful for drivers that populate default timestamps only
	// on the database side.
	var stored types.DocumentParseArtifact
	if err := r.db.WithContext(ctx).Where("tenant_id = ? AND file_hash = ? AND parser_version = ?", row.TenantID, row.FileHash, row.ParserVersion).First(&stored).Error; err != nil {
		return err
	}
	*row = stored
	return nil
}

func (r *documentParseArtifactRepository) DeleteBySourceDocument(ctx context.Context, tenantID uint64, sourceDocumentID string) error {
	if strings.TrimSpace(sourceDocumentID) == "" {
		return nil
	}
	return r.db.WithContext(ctx).
		Where("tenant_id = ? AND source_document_id = ?", tenantID, sourceDocumentID).
		Delete(&types.DocumentParseArtifact{}).Error
}
