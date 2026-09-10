package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type smartArchiveRepositoryFixture struct {
	db   *gorm.DB
	repo interfaces.ArchiveRepository
}

func newSmartArchiveRepositoryFixture(t *testing.T) smartArchiveRepositoryFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&types.ArchiveSettings{},
		&types.ArchiveImportBatch{},
		&types.ArchiveDocument{},
		&types.ArchiveImportItem{},
		&types.ArchiveCustomer{},
		&types.ArchiveDocumentLink{},
		&types.ArchiveFieldEvidence{},
		&types.ArchiveReminder{},
		&types.ArchiveReminderOccurrence{},
		&types.ArchiveNotification{},
		&types.ArchiveReminderCandidate{},
	))
	// AutoMigrate cannot express the active-row partial index, while the
	// migration deliberately uses it to allow a trashed copy to be reimported.
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_test_archive_document_fingerprint
		ON archive_documents(tenant_id, file_hash, extraction_version)
		WHERE trashed_at IS NULL`).Error)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_test_archive_import_item_fingerprint
		ON archive_import_items(tenant_id, file_hash, extraction_version)`).Error)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_test_archive_notification_occurrence
		ON archive_notifications(occurrence_id)
		WHERE occurrence_id IS NOT NULL AND occurrence_id <> ''`).Error)
	return smartArchiveRepositoryFixture{db: db, repo: NewSmartArchiveRepository(db)}
}

func newArchiveDocument(tenantID uint64, hash, version string) *types.ArchiveDocument {
	return &types.ArchiveDocument{
		TenantID:          tenantID,
		Title:             "document-" + hash,
		FileName:          "document.pdf",
		FileType:          ".pdf",
		FileHash:          hash,
		ExtractionVersion: version,
		FilePath:          "/tmp/" + hash,
		CreatedBy:         "user-1",
	}
}

func TestSmartArchiveRepositoryImportItemFingerprintAndTenantScope(t *testing.T) {
	f := newSmartArchiveRepositoryFixture(t)
	ctx := context.Background()
	batch := &types.ArchiveImportBatch{TenantID: 1, UserID: "user-1", Total: 3}
	require.NoError(t, f.repo.CreateBatch(ctx, batch))

	item := &types.ArchiveImportItem{
		TenantID:          1,
		BatchID:           batch.ID,
		FileName:          "contract.pdf",
		FileHash:          "same-bytes",
		ExtractionVersion: "v1",
		FilePath:          "/tmp/contract.pdf",
	}
	require.NoError(t, f.repo.CreateImportItem(ctx, item))
	loaded, err := f.repo.GetImportItemByFingerprint(ctx, 1, "same-bytes", "v1")
	require.NoError(t, err)
	require.Equal(t, item.ID, loaded.ID)

	duplicate := *item
	duplicate.ID = "duplicate"
	require.Error(t, f.repo.CreateImportItem(ctx, &duplicate), "the durable queue fingerprint must be unique per tenant and extraction version")

	otherVersion := *item
	otherVersion.ID = "other-version"
	otherVersion.ExtractionVersion = "v2"
	require.NoError(t, f.repo.CreateImportItem(ctx, &otherVersion))
	otherTenant := *item
	otherTenant.ID = "other-tenant"
	otherTenant.TenantID = 2
	require.NoError(t, f.repo.CreateImportItem(ctx, &otherTenant))

	require.NoError(t, f.repo.UpdateImportItem(ctx, func() *types.ArchiveImportItem {
		item.ErrorMessage = "transient"
		return item
	}()))
	updated, err := f.repo.GetImportItem(ctx, 1, item.ID)
	require.NoError(t, err)
	require.Equal(t, "transient", updated.ErrorMessage)
	_, err = f.repo.GetImportItem(ctx, 2, item.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestSmartArchiveRepositoryClaimsAndCompletesImportAtomically(t *testing.T) {
	f := newSmartArchiveRepositoryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	batch := &types.ArchiveImportBatch{TenantID: 1, UserID: "user-1", Total: 1}
	require.NoError(t, f.repo.CreateBatch(ctx, batch))
	item := &types.ArchiveImportItem{
		TenantID:          1,
		BatchID:           batch.ID,
		FileName:          "contract.pdf",
		FileHash:          "claim-bytes",
		ExtractionVersion: "v1",
		FilePath:          "/tmp/contract.pdf",
		AvailableAt:       func() *time.Time { value := now.Add(-time.Minute); return &value }(),
	}
	require.NoError(t, f.repo.CreateImportItem(ctx, item))
	pending, err := f.repo.ListPendingImportItems(ctx, now, 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)

	claimed, err := f.repo.ClaimImportItem(ctx, 1, item.ID, "run-1", now)
	require.NoError(t, err)
	require.Equal(t, types.ArchiveImportItemProcessing, claimed.Status)
	require.Equal(t, 1, claimed.AttemptCount)
	require.Equal(t, "run-1", claimed.RunID)
	_, err = f.repo.ClaimImportItem(ctx, 1, item.ID, "run-2", now)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	require.NoError(t, f.repo.MarkImportItemCompleted(ctx, 1, item.ID, "document-1"))
	require.NoError(t, f.repo.MarkImportItemCompleted(ctx, 1, item.ID, "document-1"), "completion must be idempotent")
	completed, err := f.repo.GetImportItem(ctx, 1, item.ID)
	require.NoError(t, err)
	require.Equal(t, types.ArchiveImportItemCompleted, completed.Status)
	require.Equal(t, "document-1", completed.DocumentID)
	require.Empty(t, completed.RunID)
	gotBatch, err := f.repo.GetBatch(ctx, 1, batch.ID)
	require.NoError(t, err)
	require.Equal(t, 1, gotBatch.Completed)
	require.Zero(t, gotBatch.Failed)
	require.Equal(t, types.ArchiveImportBatchCompleted, gotBatch.Status)
}

func TestSmartArchiveRepositoryFailureRetryReconcilesBatchCounters(t *testing.T) {
	f := newSmartArchiveRepositoryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	retryAt := now.Add(time.Minute)
	batch := &types.ArchiveImportBatch{TenantID: 1, UserID: "user-1", Total: 1}
	require.NoError(t, f.repo.CreateBatch(ctx, batch))
	item := &types.ArchiveImportItem{
		TenantID:          1,
		BatchID:           batch.ID,
		FileName:          "contract.pdf",
		FileHash:          "retry-bytes",
		ExtractionVersion: "v1",
		FilePath:          "/tmp/contract.pdf",
		AvailableAt:       &now,
	}
	require.NoError(t, f.repo.CreateImportItem(ctx, item))
	_, err := f.repo.ClaimImportItem(ctx, 1, item.ID, "run-1", now)
	require.NoError(t, err)
	require.NoError(t, f.repo.MarkImportItemFailed(ctx, 1, item.ID, "parser unavailable", &retryAt))
	failedBatch, err := f.repo.GetBatch(ctx, 1, batch.ID)
	require.NoError(t, err)
	require.Equal(t, 1, failedBatch.Failed)
	require.Equal(t, types.ArchiveImportBatchFailed, failedBatch.Status)
	pending, err := f.repo.ListPendingImportItems(ctx, now, 10)
	require.NoError(t, err)
	require.Empty(t, pending)
	pending, err = f.repo.ListPendingImportItems(ctx, retryAt, 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)

	_, err = f.repo.ClaimImportItem(ctx, 1, item.ID, "run-2", retryAt)
	require.NoError(t, err)
	require.NoError(t, f.repo.MarkImportItemCompleted(ctx, 1, item.ID, "document-2"))
	completedBatch, err := f.repo.GetBatch(ctx, 1, batch.ID)
	require.NoError(t, err)
	require.Equal(t, 1, completedBatch.Completed)
	require.Zero(t, completedBatch.Failed)
	require.Equal(t, types.ArchiveImportBatchCompleted, completedBatch.Status)
}

func TestSmartArchiveRepositoryTerminalFailureIsNotRecovered(t *testing.T) {
	f := newSmartArchiveRepositoryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	batch := &types.ArchiveImportBatch{TenantID: 1, UserID: "user-1", Total: 1}
	require.NoError(t, f.repo.CreateBatch(ctx, batch))
	item := &types.ArchiveImportItem{
		TenantID: 1, BatchID: batch.ID, FileName: "terminal.pdf",
		FileHash: "terminal-bytes", ExtractionVersion: "v1", FilePath: "/tmp/terminal.pdf",
		AvailableAt: &now,
	}
	require.NoError(t, f.repo.CreateImportItem(ctx, item))
	_, err := f.repo.ClaimImportItem(ctx, 1, item.ID, "run-terminal", now)
	require.NoError(t, err)
	require.NoError(t, f.repo.MarkImportItemFailed(ctx, 1, item.ID, "permanent", nil))
	pending, err := f.repo.ListPendingImportItems(ctx, now.Add(time.Hour), 10)
	require.NoError(t, err)
	require.Empty(t, pending)
	_, err = f.repo.ClaimImportItem(ctx, 1, item.ID, "run-again", now.Add(time.Hour))
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestSmartArchiveSearchUsesNormalizedExtractedAssetFields(t *testing.T) {
	f := newSmartArchiveRepositoryFixture(t)
	ctx := context.Background()
	doc := newArchiveDocument(1, "field-search", "v1")
	doc.ExtractedFields = types.JSON(`{"asset_model":"H100","serial_number":"SN-42"}`)
	require.NoError(t, f.repo.CreateDocument(ctx, doc))

	result, err := f.repo.Search(ctx, 1, &types.ArchiveSearchRequest{
		Filters: types.ArchiveSearchFilters{Model: "H100", SerialNumber: "SN-42"},
		Page:    1, PageSize: 20,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Total)
	require.Len(t, result.Documents, 1)
	require.Equal(t, doc.ID, result.Documents[0].ID)
}

func TestSmartArchiveRepositoryRetryDoesNotDoubleDecrementOtherFailures(t *testing.T) {
	f := newSmartArchiveRepositoryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	retryAt := now.Add(time.Minute)
	batch := &types.ArchiveImportBatch{TenantID: 1, UserID: "user-1", Total: 2}
	require.NoError(t, f.repo.CreateBatch(ctx, batch))
	items := []*types.ArchiveImportItem{
		{TenantID: 1, BatchID: batch.ID, FileName: "one.pdf", FileHash: "retry-one", ExtractionVersion: "v1", FilePath: "/tmp/one.pdf", AvailableAt: &now},
		{TenantID: 1, BatchID: batch.ID, FileName: "two.pdf", FileHash: "retry-two", ExtractionVersion: "v1", FilePath: "/tmp/two.pdf", AvailableAt: &now},
	}
	for index, item := range items {
		require.NoError(t, f.repo.CreateImportItem(ctx, item))
		_, err := f.repo.ClaimImportItem(ctx, 1, item.ID, "run-"+string(rune('1'+index)), now)
		require.NoError(t, err)
		require.NoError(t, f.repo.MarkImportItemFailed(ctx, 1, item.ID, "temporary", &retryAt))
	}
	failedBatch, err := f.repo.GetBatch(ctx, 1, batch.ID)
	require.NoError(t, err)
	require.Equal(t, 2, failedBatch.Failed)

	_, err = f.repo.ClaimImportItem(ctx, 1, items[0].ID, "run-retry-1", retryAt)
	require.NoError(t, err)
	require.NoError(t, f.repo.MarkImportItemCompleted(ctx, 1, items[0].ID, "document-1"))
	partiallyDone, err := f.repo.GetBatch(ctx, 1, batch.ID)
	require.NoError(t, err)
	require.Equal(t, 1, partiallyDone.Completed)
	require.Equal(t, 1, partiallyDone.Failed)

	_, err = f.repo.ClaimImportItem(ctx, 1, items[1].ID, "run-retry-2", retryAt)
	require.NoError(t, err)
	require.NoError(t, f.repo.MarkImportItemCompleted(ctx, 1, items[1].ID, "document-2"))
	done, err := f.repo.GetBatch(ctx, 1, batch.ID)
	require.NoError(t, err)
	require.Equal(t, 2, done.Completed)
	require.Zero(t, done.Failed)
}

func TestSmartArchiveRepositoryDocumentFingerprintAndHydration(t *testing.T) {
	f := newSmartArchiveRepositoryFixture(t)
	ctx := context.Background()
	docV1 := newArchiveDocument(1, "document-bytes", "v1")
	docV2 := newArchiveDocument(1, "document-bytes", "v2")
	require.NoError(t, f.repo.CreateDocument(ctx, docV1))
	require.Error(t, f.repo.CreateDocument(ctx, newArchiveDocument(1, "document-bytes", "v1")))
	require.NoError(t, f.repo.CreateDocument(ctx, docV2), "a parser version is part of the document fingerprint")

	customer := &types.ArchiveCustomer{TenantID: 1, Name: "Acme", Normalized: "acme"}
	require.NoError(t, f.repo.CreateCustomer(ctx, customer))
	docV1.CustomerID = customer.ID
	require.NoError(t, f.repo.UpdateDocument(ctx, docV1))
	require.NoError(t, f.repo.CreateDocumentLink(ctx, &types.ArchiveDocumentLink{TenantID: 1, FromDocumentID: docV1.ID, ToDocumentID: docV2.ID, Relation: "same_agreement"}))
	require.NoError(t, f.repo.ReplaceEvidence(ctx, 1, docV1.ID, []*types.ArchiveFieldEvidence{{TenantID: 1, DocumentID: docV1.ID, FieldName: "agreement_number", Value: "A-1", Quote: "A-1"}}))

	found, err := f.repo.FindDocumentByHash(ctx, 1, "document-bytes", "v2")
	require.NoError(t, err)
	require.Equal(t, docV2.ID, found.ID)
	hydrated, err := f.repo.GetDocument(ctx, 1, docV1.ID)
	require.NoError(t, err)
	require.Equal(t, customer.ID, hydrated.Customer.ID)
	require.Len(t, hydrated.Links, 1)
	require.Len(t, hydrated.Evidence, 1)
	_, err = f.repo.FindDocumentByHash(ctx, 2, "document-bytes", "v1")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}
