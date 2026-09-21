package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type smartArchiveRepository struct{ db *gorm.DB }

// nullableTime accepts both database/sql time values and SQLite's textual
// representation returned by aggregate functions such as MIN(). PostgreSQL
// drivers return time.Time here, while mattn/go-sqlite3 returns a string for
// the same expression; keeping the scanner local avoids changing the query
// semantics for either backend.
type nullableTime struct {
	Time  time.Time
	Valid bool
}

func (t *nullableTime) Scan(value any) error {
	if value == nil {
		t.Time = time.Time{}
		t.Valid = false
		return nil
	}
	if parsed, ok := value.(time.Time); ok {
		t.Time = parsed
		t.Valid = true
		return nil
	}
	var raw string
	switch typed := value.(type) {
	case string:
		raw = typed
	case []byte:
		raw = string(typed)
	default:
		return fmt.Errorf("unsupported timestamp value %T", value)
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		parsed, err := time.Parse(layout, raw)
		if err == nil {
			t.Time = parsed
			t.Valid = true
			return nil
		}
	}
	return fmt.Errorf("cannot parse timestamp %q", raw)
}

func (t nullableTime) Value() (driver.Value, error) {
	if !t.Valid {
		return nil, nil
	}
	return t.Time, nil
}

func NewSmartArchiveRepository(db *gorm.DB) interfaces.ArchiveRepository {
	return &smartArchiveRepository{db: db}
}

func (r *smartArchiveRepository) GetSettings(ctx context.Context, tenantID uint64) (*types.ArchiveSettings, error) {
	var row types.ArchiveSettings
	err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).First(&row).Error
	return &row, err
}
func (r *smartArchiveRepository) SaveSettings(ctx context.Context, row *types.ArchiveSettings) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}}, UpdateAll: true}).Create(row).Error
}
func (r *smartArchiveRepository) CreateBatch(ctx context.Context, row *types.ArchiveImportBatch) error {
	return r.db.WithContext(ctx).Create(row).Error
}
func (r *smartArchiveRepository) GetBatch(ctx context.Context, tenantID uint64, id string) (*types.ArchiveImportBatch, error) {
	var row types.ArchiveImportBatch
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(&row).Error
	return &row, err
}
func (r *smartArchiveRepository) UpdateBatch(ctx context.Context, row *types.ArchiveImportBatch) error {
	return r.db.WithContext(ctx).Save(row).Error
}

func (r *smartArchiveRepository) CreateImportItem(ctx context.Context, row *types.ArchiveImportItem) error {
	if row == nil {
		return gorm.ErrInvalidData
	}
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *smartArchiveRepository) GetImportItem(ctx context.Context, tenantID uint64, id string) (*types.ArchiveImportItem, error) {
	var row types.ArchiveImportItem
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(&row).Error
	return &row, err
}

func (r *smartArchiveRepository) GetImportItemByFingerprint(ctx context.Context, tenantID uint64, fileHash, extractionVersion string) (*types.ArchiveImportItem, error) {
	var row types.ArchiveImportItem
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND file_hash = ? AND extraction_version = ?", tenantID, fileHash, extractionVersion).First(&row).Error
	return &row, err
}

func (r *smartArchiveRepository) UpdateImportItem(ctx context.Context, row *types.ArchiveImportItem) error {
	if row == nil {
		return gorm.ErrInvalidData
	}
	return r.db.WithContext(ctx).Save(row).Error
}

func (r *smartArchiveRepository) ListPendingImportItems(ctx context.Context, now time.Time, limit int) ([]*types.ArchiveImportItem, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []*types.ArchiveImportItem
	err := r.db.WithContext(ctx).Where("((status = ? AND (available_at IS NULL OR available_at <= ?)) OR (status = ? AND available_at IS NOT NULL AND available_at <= ?) OR (status = ? AND lease_until IS NOT NULL AND lease_until <= ?))", types.ArchiveImportItemQueued, now, types.ArchiveImportItemFailed, now, types.ArchiveImportItemProcessing, now).Order("COALESCE(available_at, created_at) ASC, created_at ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *smartArchiveRepository) ClaimImportItem(ctx context.Context, tenantID uint64, id, runID string, now time.Time) (*types.ArchiveImportItem, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if strings.TrimSpace(runID) == "" {
		runID = fmt.Sprintf("archive-import-%d", now.UnixNano())
	}
	leaseUntil := now.Add(5 * time.Minute)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item types.ArchiveImportItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, id).First(&item).Error; err != nil {
			return err
		}
		queuedOrFailed := item.Status == types.ArchiveImportItemQueued || item.Status == types.ArchiveImportItemFailed
		available := item.AvailableAt == nil || !item.AvailableAt.After(now)
		// A terminal failed item is represented by failed + NULL available_at;
		// unlike a queued item with no timestamp, it must never be reclaimed.
		if item.Status == types.ArchiveImportItemFailed && item.AvailableAt == nil {
			queuedOrFailed = false
		}
		leaseExpired := item.Status == types.ArchiveImportItemProcessing && item.LeaseUntil != nil && !item.LeaseUntil.After(now)
		if (!queuedOrFailed || !available) && !leaseExpired {
			return gorm.ErrRecordNotFound
		}
		if item.Status == types.ArchiveImportItemFailed && item.BatchID != "" {
			var batch types.ArchiveImportBatch
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, item.BatchID).First(&batch).Error; err != nil {
				return err
			}
			if item.FailureCounted && batch.Failed > 0 {
				batch.Failed--
			}
			setImportBatchStatus(&batch)
			if err := tx.Model(&types.ArchiveImportBatch{}).Where("tenant_id = ? AND id = ?", tenantID, item.BatchID).Updates(map[string]any{
				"completed": batch.Completed, "failed": batch.Failed, "status": batch.Status, "updated_at": now,
			}).Error; err != nil {
				return err
			}
		}
		itemUpdates := map[string]any{
			"status": types.ArchiveImportItemProcessing, "run_id": runID, "claimed_at": now, "lease_until": leaseUntil, "attempt_count": gorm.Expr("attempt_count + 1"), "updated_at": now,
		}
		// A counted terminal failure is removed from the batch failure tally as
		// soon as the retry lease is acquired. Keep the marker in sync so a
		// later successful completion does not subtract the same failure again.
		if item.Status == types.ArchiveImportItemFailed {
			itemUpdates["failure_counted"] = false
		}
		return tx.Model(&types.ArchiveImportItem{}).Where("tenant_id = ? AND id = ?", tenantID, id).Updates(itemUpdates).Error
	})
	if err != nil {
		return nil, err
	}
	return r.GetImportItem(ctx, tenantID, id)
}

func (r *smartArchiveRepository) CancelImportItem(ctx context.Context, tenantID uint64, itemID, errorMessage string) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item types.ArchiveImportItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, itemID).First(&item).Error; err != nil {
			return err
		}
		if item.Status == types.ArchiveImportItemCompleted {
			return gorm.ErrInvalidData
		}
		if item.Status == types.ArchiveImportItemCanceled {
			return nil
		}
		if err := tx.Model(&types.ArchiveImportItem{}).Where("tenant_id = ? AND id = ?", tenantID, itemID).Updates(map[string]any{
			"status": types.ArchiveImportItemCanceled, "error_message": errorMessage, "failure_counted": true,
			"available_at": nil, "run_id": "", "claimed_at": nil, "lease_until": nil, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		if item.BatchID == "" || item.FailureCounted {
			return nil
		}
		var batch types.ArchiveImportBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, item.BatchID).First(&batch).Error; err != nil {
			return err
		}
		batch.Failed++
		setImportBatchStatus(&batch)
		return tx.Model(&types.ArchiveImportBatch{}).Where("tenant_id = ? AND id = ?", tenantID, item.BatchID).Updates(map[string]any{
			"completed": batch.Completed, "failed": batch.Failed, "status": batch.Status, "updated_at": now,
		}).Error
	})
}

func (r *smartArchiveRepository) MarkImportItemCompleted(ctx context.Context, tenantID uint64, itemID, documentID string) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item types.ArchiveImportItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, itemID).First(&item).Error; err != nil {
			return err
		}
		if item.Status == types.ArchiveImportItemCompleted {
			return nil
		}
		if item.Status == types.ArchiveImportItemCanceled {
			return gorm.ErrInvalidData
		}
		updates := map[string]any{
			"status":          types.ArchiveImportItemCompleted,
			"document_id":     documentID,
			"completed_at":    now,
			"run_id":          "",
			"claimed_at":      nil,
			"lease_until":     nil,
			"error_message":   "",
			"failure_counted": false,
			"updated_at":      now,
		}
		if err := tx.Model(&types.ArchiveImportItem{}).Where("tenant_id = ? AND id = ?", tenantID, itemID).Updates(updates).Error; err != nil {
			return err
		}
		if item.BatchID == "" {
			return nil
		}
		var batch types.ArchiveImportBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, item.BatchID).First(&batch).Error; err != nil {
			return err
		}
		if item.FailureCounted && batch.Failed > 0 {
			batch.Failed--
		}
		batch.Completed++
		setImportBatchStatus(&batch)
		return tx.Model(&types.ArchiveImportBatch{}).Where("tenant_id = ? AND id = ?", tenantID, item.BatchID).Updates(map[string]any{
			"completed": batch.Completed, "failed": batch.Failed, "status": batch.Status, "updated_at": now,
		}).Error
	})
}

func (r *smartArchiveRepository) MarkImportItemFailed(ctx context.Context, tenantID uint64, itemID, errorMessage string, retryAt *time.Time) error {
	now := time.Now().UTC()
	// A nil retryAt is terminal. Leaving AvailableAt NULL keeps startup
	// recovery from resurrecting an item whose retry budget is exhausted.
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item types.ArchiveImportItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, itemID).First(&item).Error; err != nil {
			return err
		}
		if item.Status == types.ArchiveImportItemCompleted {
			return nil
		}
		if item.Status == types.ArchiveImportItemCanceled {
			return gorm.ErrInvalidData
		}
		updates := map[string]any{
			"status":          types.ArchiveImportItemFailed,
			"error_message":   errorMessage,
			"failure_counted": true,
			"available_at":    retryAt,
			"run_id":          "",
			"claimed_at":      nil,
			"lease_until":     nil,
			"updated_at":      now,
		}
		if err := tx.Model(&types.ArchiveImportItem{}).Where("tenant_id = ? AND id = ?", tenantID, itemID).Updates(updates).Error; err != nil {
			return err
		}
		if item.BatchID == "" || (item.Status == types.ArchiveImportItemFailed && item.FailureCounted) {
			return nil
		}
		var batch types.ArchiveImportBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, item.BatchID).First(&batch).Error; err != nil {
			return err
		}
		batch.Failed++
		setImportBatchStatus(&batch)
		return tx.Model(&types.ArchiveImportBatch{}).Where("tenant_id = ? AND id = ?", tenantID, item.BatchID).Updates(map[string]any{
			"completed": batch.Completed, "failed": batch.Failed, "status": batch.Status, "updated_at": now,
		}).Error
	})
}

func setImportBatchStatus(batch *types.ArchiveImportBatch) {
	if batch == nil || batch.Total <= 0 || batch.Completed+batch.Failed < batch.Total {
		if batch != nil {
			batch.Status = types.ArchiveImportBatchProcessing
		}
		return
	}
	if batch.Completed == 0 && batch.Failed > 0 {
		batch.Status = types.ArchiveImportBatchFailed
		return
	}
	batch.Status = types.ArchiveImportBatchCompleted
}
func (r *smartArchiveRepository) CreateDocument(ctx context.Context, row *types.ArchiveDocument) error {
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *smartArchiveRepository) GetDocument(ctx context.Context, tenantID uint64, id string) (*types.ArchiveDocument, error) {
	var row types.ArchiveDocument
	if err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(&row).Error; err != nil {
		return &row, err
	}
	return r.hydrateDocument(ctx, &row)
}

func (r *smartArchiveRepository) hydrateDocument(ctx context.Context, row *types.ArchiveDocument) (*types.ArchiveDocument, error) {
	if row.CustomerID != "" {
		var customer types.ArchiveCustomer
		if err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", row.TenantID, row.CustomerID).First(&customer).Error; err == nil {
			row.Customer = &customer
		}
	}
	var err error
	row.Links, err = r.ListDocumentLinks(ctx, row.TenantID, row.ID)
	if err != nil {
		return row, err
	}
	row.Evidence, err = r.ListEvidence(ctx, row.TenantID, row.ID)
	return row, err
}

func (r *smartArchiveRepository) ListDocuments(ctx context.Context, tenantID uint64, keyword string, includeArchived bool) ([]*types.ArchiveDocument, error) {
	var rows []*types.ArchiveDocument
	q := r.db.WithContext(ctx).Where("tenant_id = ? AND trashed_at IS NULL", tenantID)
	if includeArchived {
		q = q.Where("archived_at IS NOT NULL")
	} else {
		q = q.Where("archived_at IS NULL")
	}
	if strings.TrimSpace(keyword) != "" {
		like := "%" + strings.TrimSpace(keyword) + "%"
		q = q.Where("title LIKE ? OR file_name LIKE ? OR agreement_number LIKE ? OR extracted_text LIKE ?", like, like, like, like)
	}
	if err := q.Order("updated_at DESC").Limit(500).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if _, err := r.hydrateDocument(ctx, row); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func (r *smartArchiveRepository) UpdateDocument(ctx context.Context, row *types.ArchiveDocument) error {
	return r.db.WithContext(ctx).Omit("Customer", "Links", "Evidence").Save(row).Error
}
func (r *smartArchiveRepository) DeleteDocument(ctx context.Context, tenantID uint64, id string) error {
	return r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).Delete(&types.ArchiveDocument{}).Error
}
func (r *smartArchiveRepository) FindDocumentByHash(ctx context.Context, tenantID uint64, hash, extractionVersion string) (*types.ArchiveDocument, error) {
	var row types.ArchiveDocument
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND file_hash = ? AND extraction_version = ? AND trashed_at IS NULL", tenantID, hash, extractionVersion).First(&row).Error
	return &row, err
}

func (r *smartArchiveRepository) CreateCustomer(ctx context.Context, row *types.ArchiveCustomer) error {
	return r.db.WithContext(ctx).Create(row).Error
}
func (r *smartArchiveRepository) FindCustomer(ctx context.Context, tenantID uint64, normalized string) (*types.ArchiveCustomer, error) {
	var row types.ArchiveCustomer
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND normalized = ?", tenantID, normalized).First(&row).Error
	return &row, err
}
func (r *smartArchiveRepository) ListCustomers(ctx context.Context, tenantID uint64, keyword string) ([]*types.ArchiveCustomer, error) {
	var rows []*types.ArchiveCustomer
	q := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if strings.TrimSpace(keyword) != "" {
		like := "%" + strings.TrimSpace(keyword) + "%"
		q = q.Where("name LIKE ? OR normalized LIKE ?", like, like)
	}
	return rows, q.Order("updated_at DESC").Limit(500).Find(&rows).Error
}
func (r *smartArchiveRepository) UpdateCustomer(ctx context.Context, row *types.ArchiveCustomer) error {
	return r.db.WithContext(ctx).Save(row).Error
}

func (r *smartArchiveRepository) CreateDocumentLink(ctx context.Context, row *types.ArchiveDocumentLink) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error
}
func (r *smartArchiveRepository) ListDocumentLinks(ctx context.Context, tenantID uint64, documentID string) ([]*types.ArchiveDocumentLink, error) {
	var rows []*types.ArchiveDocumentLink
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND (from_document_id = ? OR to_document_id = ?)", tenantID, documentID, documentID).Order("created_at ASC").Find(&rows).Error
	return rows, err
}

func (r *smartArchiveRepository) ReplaceEvidence(ctx context.Context, tenantID uint64, documentID string, rows []*types.ArchiveFieldEvidence) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tenant_id = ? AND document_id = ?", tenantID, documentID).Delete(&types.ArchiveFieldEvidence{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	})
}
func (r *smartArchiveRepository) ListEvidence(ctx context.Context, tenantID uint64, documentID string) ([]*types.ArchiveFieldEvidence, error) {
	var rows []*types.ArchiveFieldEvidence
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND document_id = ?", tenantID, documentID).Order("created_at ASC").Find(&rows).Error
	return rows, err
}

func (r *smartArchiveRepository) CreateReminder(ctx context.Context, row *types.ArchiveReminder) error {
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *smartArchiveRepository) ListReminderCandidates(ctx context.Context, tenantID uint64, status string) ([]*types.ArchiveReminderCandidate, error) {
	var rows []*types.ArchiveReminderCandidate
	q := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Order("event_at ASC, created_at ASC").Limit(500).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.DocumentID == "" {
			continue
		}
		var document types.ArchiveDocument
		if err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, row.DocumentID).First(&document).Error; err == nil {
			row.Document = &document
			if row.DocumentTitle == "" {
				row.DocumentTitle = document.Title
			}
		}
	}
	return rows, nil
}

func (r *smartArchiveRepository) GetReminderCandidate(ctx context.Context, tenantID uint64, id string) (*types.ArchiveReminderCandidate, error) {
	var row types.ArchiveReminderCandidate
	if err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(&row).Error; err != nil {
		return &row, err
	}
	rows, err := r.ListReminderCandidates(ctx, tenantID, "")
	if err != nil {
		return &row, err
	}
	for _, candidate := range rows {
		if candidate.ID == row.ID {
			row.Document = candidate.Document
			break
		}
	}
	return &row, nil
}

func (r *smartArchiveRepository) UpsertReminderCandidate(ctx context.Context, row *types.ArchiveReminderCandidate) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "fingerprint"}},
		// Preserve created/superseded status on re-import. A fingerprint match
		// is the same suggestion, so extraction may refresh evidence but must not
		// silently reopen a user's already-created reminder.
		DoUpdates: clause.AssignmentColumns([]string{"document_title", "customer_id", "assignee_id", "source_field", "event_at", "suggested_offset_days", "title", "description", "confidence", "quote", "locator", "rule", "needs_review", "updated_at"}),
	}).Create(row).Error
}

func (r *smartArchiveRepository) UpdateReminderCandidate(ctx context.Context, row *types.ArchiveReminderCandidate) error {
	return r.db.WithContext(ctx).Save(row).Error
}

// IgnoreReminderCandidate changes only a pending candidate. Keeping the
// status predicate in the update makes a concurrent create/ignore request
// safe: once another request has acted on the candidate, this operation no
// longer reports success.
func (r *smartArchiveRepository) IgnoreReminderCandidate(ctx context.Context, tenantID uint64, id string) error {
	result := r.db.WithContext(ctx).
		Model(&types.ArchiveReminderCandidate{}).
		Where("tenant_id = ? AND id = ? AND status = ?", tenantID, id, types.ArchiveReminderCandidatePending).
		Updates(map[string]any{"status": types.ArchiveReminderCandidateIgnored, "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *smartArchiveRepository) CreateReminderFromCandidate(ctx context.Context, candidate *types.ArchiveReminderCandidate, reminder *types.ArchiveReminder) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current types.ArchiveReminderCandidate
		// Serialize candidate confirmations. Without a row lock two browser
		// clicks (or two app instances) could both observe `pending`, create
		// separate formal reminders, and only then mark the candidate created.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", candidate.TenantID, candidate.ID).First(&current).Error; err != nil {
			return err
		}
		if current.Status == types.ArchiveReminderCandidateCreated && current.ReminderID != "" {
			return gorm.ErrDuplicatedKey
		}
		if err := tx.Create(reminder).Error; err != nil {
			return err
		}
		current.Status = types.ArchiveReminderCandidateCreated
		current.ReminderID = reminder.ID
		return tx.Save(&current).Error
	})
}
func (r *smartArchiveRepository) GetReminder(ctx context.Context, tenantID uint64, id string) (*types.ArchiveReminder, error) {
	var row types.ArchiveReminder
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(&row).Error
	return &row, err
}
func (r *smartArchiveRepository) ListReminders(ctx context.Context, tenantID uint64, status string) ([]*types.ArchiveReminder, error) {
	var rows []*types.ArchiveReminder
	q := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	return rows, q.Order("COALESCE(due_at, created_at) ASC").Limit(500).Find(&rows).Error
}
func (r *smartArchiveRepository) UpdateReminder(ctx context.Context, row *types.ArchiveReminder) error {
	return r.db.WithContext(ctx).Save(row).Error
}
func (r *smartArchiveRepository) DeleteReminder(ctx context.Context, tenantID uint64, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Notifications and occurrences are delivery artifacts of the reminder,
		// not independent history once the reminder itself is deleted. Remove
		// them explicitly so SQLite and PostgreSQL have identical behavior even
		// when foreign-key enforcement differs between deployments.
		if err := deleteReminderDeliveryArtifacts(tx, tenantID, id); err != nil {
			return err
		}
		result := tx.Where("tenant_id = ? AND id = ?", tenantID, id).Delete(&types.ArchiveReminder{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}
func (r *smartArchiveRepository) ListDueReminders(ctx context.Context, limit int) ([]*types.ArchiveReminder, error) {
	var rows []*types.ArchiveReminder
	return rows, r.db.WithContext(ctx).Where("((status = ? AND (snoozed_until IS NULL OR snoozed_until <= CURRENT_TIMESTAMP)) OR (status = ? AND snoozed_until IS NOT NULL AND snoozed_until <= CURRENT_TIMESTAMP)) AND due_at IS NOT NULL AND due_at <= CURRENT_TIMESTAMP AND (last_occurrence_at IS NULL OR last_occurrence_at < due_at)", types.ArchiveReminderActive, types.ArchiveReminderSnoozed).Order("due_at ASC").Limit(limit).Find(&rows).Error
}
func (r *smartArchiveRepository) CreateOccurrence(ctx context.Context, row *types.ArchiveReminderOccurrence) (bool, error) {
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "fingerprint"}}, DoNothing: true}).Create(row)
	return result.RowsAffected > 0, result.Error
}

// NextReminderWakeAt returns the earliest durable wake-up point. Active
// reminders wake at due_at; snoozed reminders wake at snoozed_until. The
// scheduler still performs a bounded compensation scan, so this optimization
// cannot make a reminder disappear if a timer or wake signal is lost.
func (r *smartArchiveRepository) NextReminderWakeAt(ctx context.Context) (*time.Time, error) {
	type minTime struct {
		At nullableTime `gorm:"column:at"`
	}
	var active, snoozed minTime
	if err := r.db.WithContext(ctx).Model(&types.ArchiveReminder{}).
		Select("MIN(due_at) AS at").
		Where("status = ? AND due_at IS NOT NULL", types.ArchiveReminderActive).
		Scan(&active).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Model(&types.ArchiveReminder{}).
		Select("MIN(snoozed_until) AS at").
		Where("status = ? AND snoozed_until IS NOT NULL", types.ArchiveReminderSnoozed).
		Scan(&snoozed).Error; err != nil {
		return nil, err
	}
	var next *time.Time
	if active.At.Valid {
		value := active.At.Time
		next = &value
	}
	if snoozed.At.Valid && (next == nil || snoozed.At.Time.Before(*next)) {
		value := snoozed.At.Time
		next = &value
	}
	return next, nil
}

// DeliverReminder atomically records one occurrence, creates its in-app
// notification, and advances the reminder cursor. This is the reliability
// boundary for the scheduler: a database failure rolls back the occurrence,
// so the next compensation scan retries instead of losing the notification.
// The occurrence fingerprint and notification occurrence_id make concurrent
// workers and process restarts idempotent.
func (r *smartArchiveRepository) DeliverReminder(ctx context.Context, reminder *types.ArchiveReminder, occurrence *types.ArchiveReminderOccurrence, notification *types.ArchiveNotification) error {
	if reminder == nil || occurrence == nil || notification == nil {
		return gorm.ErrInvalidData
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var currentReminder types.ArchiveReminder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND id = ?", reminder.TenantID, reminder.ID).
			First(&currentReminder).Error; err != nil {
			return err
		}
		if currentReminder.Status != types.ArchiveReminderActive && currentReminder.Status != types.ArchiveReminderSnoozed {
			return nil
		}

		var currentOccurrence types.ArchiveReminderOccurrence
		err := tx.Where("fingerprint = ?", occurrence.Fingerprint).First(&currentOccurrence).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			currentOccurrence = *occurrence
			if err := tx.Create(&currentOccurrence).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if occurrence.Status == types.ArchiveOccurrenceSkipped && currentOccurrence.Status == types.ArchiveOccurrencePending {
			if err := tx.Model(&types.ArchiveReminderOccurrence{}).
				Where("id = ?", currentOccurrence.ID).
				Update("status", types.ArchiveOccurrenceSkipped).Error; err != nil {
				return err
			}
			currentOccurrence.Status = types.ArchiveOccurrenceSkipped
		}

		switch currentOccurrence.Status {
		case types.ArchiveOccurrencePending:
			notification.OccurrenceID = currentOccurrence.ID
			// A conflict means another worker already committed the same
			// notification. It is still safe to mark this occurrence sent.
			// Use a constraint-agnostic conflict target so this works with the
			// partial unique index used by both PostgreSQL and SQLite (empty
			// legacy occurrence IDs are intentionally excluded from that index).
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(notification).Error; err != nil {
				return err
			}
			if err := tx.Model(&types.ArchiveReminderOccurrence{}).
				Where("id = ?", currentOccurrence.ID).
				Update("status", types.ArchiveOccurrenceSent).Error; err != nil {
				return err
			}
		case types.ArchiveOccurrenceSent, types.ArchiveOccurrenceSkipped:
			// Already durably delivered (or intentionally skipped).
		default:
			return gorm.ErrInvalidData
		}

		now := time.Now().UTC()
		updates := map[string]any{"last_occurrence_at": now}
		if currentReminder.Status == types.ArchiveReminderSnoozed && currentReminder.SnoozedUntil != nil && !currentReminder.SnoozedUntil.After(now) {
			updates["status"] = types.ArchiveReminderActive
			updates["snoozed_until"] = nil
		}
		return tx.Model(&types.ArchiveReminder{}).
			Where("tenant_id = ? AND id = ?", currentReminder.TenantID, currentReminder.ID).
			Updates(updates).Error
	})
}
func (r *smartArchiveRepository) ListTrashedDocuments(ctx context.Context) ([]*types.ArchiveDocument, error) {
	var rows []*types.ArchiveDocument
	err := r.db.WithContext(ctx).Where("trashed_at IS NOT NULL").Order("trashed_at ASC").Limit(1000).Find(&rows).Error
	return rows, err
}

func (r *smartArchiveRepository) ClaimMirrorDocument(ctx context.Context, tenantID uint64, id, runID string, now time.Time) (*types.ArchiveDocument, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, gorm.ErrInvalidData
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	leaseUntil := now.Add(11 * time.Minute)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var doc types.ArchiveDocument
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ? AND trashed_at IS NULL", tenantID, id).First(&doc).Error; err != nil {
			return err
		}
		pending := doc.MirrorStatus == types.ArchiveMirrorPending
		leaseExpired := doc.MirrorStatus == types.ArchiveMirrorProcessing && (doc.MirrorLeaseUntil == nil || !doc.MirrorLeaseUntil.After(now))
		if !pending && !leaseExpired {
			return gorm.ErrRecordNotFound
		}
		return tx.Model(&types.ArchiveDocument{}).Where("tenant_id = ? AND id = ?", tenantID, id).Updates(map[string]any{
			"mirror_status":        types.ArchiveMirrorProcessing,
			"mirror_error_message": "",
			"mirror_run_id":        runID,
			"mirror_lease_until":   leaseUntil,
			"updated_at":           now,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return r.GetDocument(ctx, tenantID, id)
}

func (r *smartArchiveRepository) CompleteMirrorDocument(ctx context.Context, tenantID uint64, id, runID, knowledgeID string) error {
	result := r.db.WithContext(ctx).Model(&types.ArchiveDocument{}).
		Where("tenant_id = ? AND id = ? AND mirror_status = ? AND mirror_run_id = ?", tenantID, id, types.ArchiveMirrorProcessing, runID).
		Updates(map[string]any{
			"knowledge_id":         knowledgeID,
			"mirror_status":        types.ArchiveMirrorSubmitted,
			"mirror_error_message": "",
			"mirror_run_id":        "",
			"mirror_lease_until":   nil,
			"updated_at":           time.Now().UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *smartArchiveRepository) FailMirrorDocument(ctx context.Context, tenantID uint64, id, runID, errorMessage string, retry bool) error {
	status := types.ArchiveMirrorFailed
	storedRunID := ""
	if retry {
		status = types.ArchiveMirrorPending
		storedRunID = runID
	}
	result := r.db.WithContext(ctx).Model(&types.ArchiveDocument{}).
		Where("tenant_id = ? AND id = ? AND mirror_status = ? AND mirror_run_id = ?", tenantID, id, types.ArchiveMirrorProcessing, runID).
		Updates(map[string]any{
			"mirror_status":        status,
			"mirror_error_message": errorMessage,
			"mirror_run_id":        storedRunID,
			"mirror_lease_until":   nil,
			"updated_at":           time.Now().UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *smartArchiveRepository) RetryMirrorDocument(ctx context.Context, tenantID uint64, id string) error {
	result := r.db.WithContext(ctx).Model(&types.ArchiveDocument{}).
		Where("tenant_id = ? AND id = ? AND trashed_at IS NULL AND mirror_status = ?", tenantID, id, types.ArchiveMirrorFailed).
		Updates(map[string]any{
			"mirror_status":        types.ArchiveMirrorPending,
			"mirror_error_message": "",
			"mirror_run_id":        "",
			"mirror_lease_until":   nil,
			"updated_at":           time.Now().UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrInvalidData
	}
	return nil
}

func (r *smartArchiveRepository) RollbackMirrorRetry(ctx context.Context, tenantID uint64, id, errorMessage string) error {
	result := r.db.WithContext(ctx).Model(&types.ArchiveDocument{}).
		Where("tenant_id = ? AND id = ? AND mirror_status = ? AND mirror_run_id = '' AND mirror_lease_until IS NULL", tenantID, id, types.ArchiveMirrorPending).
		Updates(map[string]any{
			"mirror_status":        types.ArchiveMirrorFailed,
			"mirror_error_message": errorMessage,
			"updated_at":           time.Now().UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *smartArchiveRepository) ListPendingMirrorDocuments(ctx context.Context, _ time.Time, limit int) ([]*types.ArchiveDocument, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []*types.ArchiveDocument
	err := r.db.WithContext(ctx).
		Where("trashed_at IS NULL AND mirror_status IN ?", []types.ArchiveMirrorStatus{types.ArchiveMirrorPending, types.ArchiveMirrorProcessing}).
		Order("updated_at ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *smartArchiveRepository) HardDeleteDocument(ctx context.Context, tenantID uint64, id string) error {
	return r.db.WithContext(ctx).Unscoped().Where("tenant_id = ? AND id = ?", tenantID, id).Delete(&types.ArchiveDocument{}).Error
}
func (r *smartArchiveRepository) CreateNotification(ctx context.Context, row *types.ArchiveNotification) error {
	return r.db.WithContext(ctx).Create(row).Error
}
func (r *smartArchiveRepository) ListNotifications(ctx context.Context, tenantID uint64, userID string, unread bool) ([]*types.ArchiveNotification, error) {
	var rows []*types.ArchiveNotification
	q := r.db.WithContext(ctx).Where("tenant_id = ? AND user_id = ?", tenantID, userID)
	if unread {
		q = q.Where("read_at IS NULL")
	}
	return rows, q.Order("created_at DESC").Limit(200).Find(&rows).Error
}
func (r *smartArchiveRepository) MarkNotificationRead(ctx context.Context, tenantID uint64, userID, id string) error {
	now := gorm.Expr("CURRENT_TIMESTAMP")
	return r.db.WithContext(ctx).Model(&types.ArchiveNotification{}).Where("tenant_id = ? AND user_id = ? AND id = ?", tenantID, userID, id).Update("read_at", now).Error
}

func (r *smartArchiveRepository) DeleteNotification(ctx context.Context, tenantID uint64, userID, id string) error {
	result := r.db.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND id = ?", tenantID, userID, id).Delete(&types.ArchiveNotification{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func deleteReminderDeliveryArtifacts(db *gorm.DB, tenantID uint64, reminderID string) error {
	if err := db.Where("tenant_id = ? AND reminder_id = ?", tenantID, reminderID).Delete(&types.ArchiveNotification{}).Error; err != nil {
		return err
	}
	return db.Where("tenant_id = ? AND reminder_id = ?", tenantID, reminderID).Delete(&types.ArchiveReminderOccurrence{}).Error
}

func (r *smartArchiveRepository) DeleteReminderDeliveryArtifacts(ctx context.Context, tenantID uint64, reminderID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return deleteReminderDeliveryArtifacts(tx, tenantID, reminderID)
	})
}

func (r *smartArchiveRepository) Search(ctx context.Context, tenantID uint64, req *types.ArchiveSearchRequest) (*types.ArchiveSearchResponse, error) {
	if req == nil {
		req = &types.ArchiveSearchRequest{}
	}
	q := r.db.WithContext(ctx).Model(&types.ArchiveDocument{}).Where("archive_documents.tenant_id = ? AND archive_documents.trashed_at IS NULL", tenantID)
	if req.Filters.DocumentType != "" {
		q = q.Where("document_type = ?", req.Filters.DocumentType)
	}
	if req.Filters.BusinessType != "" {
		q = q.Where("business_type = ?", req.Filters.BusinessType)
	}
	if req.Filters.CustomerID != "" {
		q = q.Where("customer_id = ?", req.Filters.CustomerID)
	}
	// Asset tables were retired from the Smart Archive schema. Model and
	// serial-number filters now resolve against the normalized extracted-field
	// JSON, which keeps these search dimensions available without reintroducing
	// a second asset source of truth. CAST/LOWER works for both PostgreSQL JSONB
	// and SQLite TEXT/JSON storage.
	if req.Filters.Model != "" {
		model := strings.ToLower(strings.TrimSpace(req.Filters.Model))
		like := "%\"asset_model\"%" + model + "%"
		q = q.Where("LOWER(CAST(extracted_fields AS TEXT)) LIKE ?", like)
	}
	if req.Filters.SerialNumber != "" {
		serial := strings.ToLower(strings.TrimSpace(req.Filters.SerialNumber))
		like := "%\"serial_number\"%" + serial + "%"
		q = q.Where("LOWER(CAST(extracted_fields AS TEXT)) LIKE ?", like)
	}
	if req.Filters.AgreementNumber != "" {
		q = q.Where("agreement_number = ?", req.Filters.AgreementNumber)
	}
	if req.Filters.From != nil {
		q = q.Where("COALESCE(effective_at, created_at) >= ?", req.Filters.From)
	}
	if req.Filters.To != nil {
		q = q.Where("COALESCE(effective_at, created_at) <= ?", req.Filters.To)
	}
	if req.Filters.ImportedFrom != nil {
		q = q.Where("archive_documents.created_at >= ?", req.Filters.ImportedFrom)
	}
	if req.Filters.ImportedTo != nil {
		q = q.Where("archive_documents.created_at <= ?", req.Filters.ImportedTo)
	}
	if len(req.Filters.ExtractionStatuses) > 0 {
		q = q.Where("archive_documents.extraction_status IN ?", req.Filters.ExtractionStatuses)
	}
	if req.Filters.Archived != nil {
		if *req.Filters.Archived {
			q = q.Where("archive_documents.archived_at IS NOT NULL")
		} else {
			q = q.Where("archive_documents.archived_at IS NULL")
		}
	}
	query := strings.TrimSpace(req.Query)
	if query != "" {
		terms := []string{query}
		for _, term := range strings.FieldsFunc(query, func(r rune) bool {
			return unicode.IsSpace(r) || strings.ContainsRune("，。,.、;；:：!?！？()（）[]【】{}\"“”'‘’", r)
		}) {
			term = strings.TrimSpace(term)
			if len([]rune(term)) >= 2 && term != query {
				terms = append(terms, term)
			}
		}
		parts := make([]string, 0, len(terms))
		args := make([]any, 0, len(terms)*4)
		for _, term := range terms {
			like := "%" + term + "%"
			parts = append(parts, "(archive_documents.title LIKE ? OR archive_documents.file_name LIKE ? OR archive_documents.agreement_number LIKE ? OR archive_documents.extracted_text LIKE ?)")
			args = append(args, like, like, like, like)
		}
		q = q.Where("("+strings.Join(parts, " OR ")+")", args...)
	}
	var total int64
	countQ := q.Session(&gorm.Session{})
	if err := countQ.Count(&total).Error; err != nil {
		return nil, err
	}
	page, size := req.Page, req.PageSize
	if page < 1 {
		page = 1
	}
	if size <= 0 || size > 100 {
		size = 30
	}
	var docs []*types.ArchiveDocument
	if err := q.Select("archive_documents.*").Order("archive_documents.updated_at DESC").Offset((page - 1) * size).Limit(size).Find(&docs).Error; err != nil {
		return nil, err
	}
	for _, doc := range docs {
		if _, err := r.hydrateDocument(ctx, doc); err != nil {
			return nil, err
		}
	}
	customers, err := r.ListCustomers(ctx, tenantID, query)
	if err != nil {
		return nil, err
	}
	answer := "未找到匹配的档案。"
	if total > 0 {
		answer = "检索到 " + fmtInt(total) + " 份档案。"
	}
	resp := &types.ArchiveSearchResponse{Answer: answer, Documents: docs, Customers: customers, Total: total, Citations: []types.ArchiveSearchCitation{}}
	for _, doc := range docs {
		for _, ev := range doc.Evidence {
			if len(resp.Citations) >= 20 {
				break
			}
			resp.Citations = append(resp.Citations, types.ArchiveSearchCitation{DocumentID: doc.ID, FieldName: ev.FieldName, Quote: ev.Quote, Locator: ev.Locator})
		}
	}
	return resp, nil
}

func fmtInt(value int64) string { return strconv.FormatInt(value, 10) }
