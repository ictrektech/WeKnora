package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDocumentParseArtifactRepositoryUpsertScopesAndDeletes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&types.DocumentParseArtifact{}))
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_test_document_parse_artifact_fingerprint
		ON document_parse_artifacts(tenant_id, file_hash, parser_version)`).Error)
	repo := NewDocumentParseArtifactRepository(db)
	ctx := context.Background()

	artifact := &types.DocumentParseArtifact{
		TenantID:         1,
		SourceDocumentID: "source-1",
		FileHash:         "hash-1",
		FileName:         "contract.pdf",
		FileType:         ".pdf",
		MarkdownContent:  "# first",
	}
	require.NoError(t, repo.Upsert(ctx, artifact))
	firstID := artifact.ID
	require.NotEmpty(t, firstID)
	require.Equal(t, types.DocumentParseArtifactVersion, artifact.ParserVersion)

	refresh := &types.DocumentParseArtifact{
		TenantID:         1,
		SourceDocumentID: "source-2",
		FileHash:         "hash-1",
		FileName:         "renamed.pdf",
		FileType:         ".pdf",
		ParserVersion:    types.DocumentParseArtifactVersion,
		MarkdownContent:  "# refreshed",
	}
	require.NoError(t, repo.Upsert(ctx, refresh))
	require.Equal(t, firstID, refresh.ID)
	got, err := repo.GetByFingerprint(ctx, 1, "hash-1", types.DocumentParseArtifactVersion)
	require.NoError(t, err)
	require.Equal(t, "# refreshed", got.MarkdownContent)
	require.Equal(t, "source-2", got.SourceDocumentID)

	_, err = repo.GetByID(ctx, 2, firstID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.NoError(t, repo.DeleteBySourceDocument(ctx, 1, "source-2"))
	_, err = repo.GetByFingerprint(ctx, 1, "hash-1", types.DocumentParseArtifactVersion)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	// Empty source IDs are intentionally a no-op, so a caller cannot erase all
	// artifacts by passing an omitted optional identifier.
	require.NoError(t, repo.DeleteBySourceDocument(ctx, 1, ""))
}
