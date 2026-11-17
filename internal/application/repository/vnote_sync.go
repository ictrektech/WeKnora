package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrVNoteSyncStale = errors.New("V-Note synchronization ownership or configuration changed")

func VNoteSyncRootID(userID, workspaceID, rootID string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("vnote-sync\x00"+userID+"\x00"+workspaceID+"\x00"+rootID)).String()
}

func vnoteKBID(root types.VNoteSyncRoot) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("vnote-kb\x00"+root.UserID+"\x00"+root.RootID)).String()
}

// EnsureVNoteSyncRoot is called only for a locally registered delegation. New
// configurations rebuild their snapshot; retained searchable copies survive.
func EnsureVNoteSyncRoot(ctx context.Context, db *gorm.DB, binding VNoteDelegation, rootID string, version int64) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var control VNoteControl
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id=? AND workspace_id=?", binding.UserID, binding.WorkspaceID).Take(&control).Error; err != nil {
			return err
		}
		allowed, err := VNoteControlAllows(ctx, tx, binding.UserID, binding.WorkspaceID, binding.Subject, rootID, version)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrVNoteSyncStale
		}
		now := time.Now().UTC()
		root := types.VNoteSyncRoot{ID: VNoteSyncRootID(binding.UserID, binding.WorkspaceID, rootID), UserID: binding.UserID, WorkspaceID: binding.WorkspaceID, Subject: binding.Subject, TenantID: binding.TenantID, RootID: rootID, ConfigVersion: version, Phase: "snapshot", ScanID: uuid.NewString(), LeaseUntil: now, NextPollAt: now, NextMetadataAt: now}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&root).Error; err != nil {
			return err
		}
		result := tx.Model(&types.VNoteSyncRoot{}).Where("id=? AND subject=? AND tenant_id=? AND config_version<?", root.ID, binding.Subject, binding.TenantID, version).
			Updates(map[string]any{"config_version": version, "phase": "snapshot", "scan_id": root.ScanID, "sync_identity": "", "graph_revision": 0, "cursor": 0, "cursor_stamp": "", "after_notebook_id": "", "after_note_id": "", "lease_token": "", "lease_until": now, "next_poll_at": now, "next_metadata_at": now, "failures": 0, "error_code": ""})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			return tx.Model(&types.VNoteSyncNote{}).Where("sync_root_id=? AND status<>?", root.ID, "done").Updates(map[string]any{"status": gorm.Expr("CASE WHEN status IN ('indexing','blocked') THEN status ELSE 'pending' END"), "next_attempt_at": now, "revision": gorm.Expr("revision+1")}).Error
		}
		return nil
	})
}

// A database lease coordinates metadata readers across replicas. The separate
// import gate owns expensive processing and is never released by this lease.
func ClaimVNoteSyncRoot(ctx context.Context, db *gorm.DB, id string, now time.Time) (*types.VNoteSyncRoot, error) {
	now = now.UTC()
	token := uuid.NewString()
	result := db.WithContext(ctx).Model(&types.VNoteSyncRoot{}).Where("id=? AND lease_until<=? AND next_poll_at<=?", id, now, now).
		Updates(map[string]any{"lease_token": token, "lease_until": now.Add(time.Minute)})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrVNoteSyncStale
	}
	var root types.VNoteSyncRoot
	err := db.WithContext(ctx).Where("id=? AND lease_token=?", id, token).Take(&root).Error
	return &root, err
}

func lockedVNoteSyncRoot(ctx context.Context, tx *gorm.DB, claim types.VNoteSyncRoot) (*types.VNoteSyncRoot, error) {
	var control VNoteControl
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id=? AND workspace_id=?", claim.UserID, claim.WorkspaceID).Take(&control).Error; err != nil {
		return nil, err
	}
	allowed, err := VNoteControlAllows(ctx, tx, claim.UserID, claim.WorkspaceID, claim.Subject, claim.RootID, claim.ConfigVersion)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrVNoteSyncStale
	}
	var root types.VNoteSyncRoot
	err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND lease_token=? AND lease_until>? AND config_version=? AND scan_id=?", claim.ID, claim.LeaseToken, time.Now().UTC(), claim.ConfigVersion, claim.ScanID).Take(&root).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrVNoteSyncStale
	}
	return &root, err
}

func upsertVNoteDesired(tx *gorm.DB, root types.VNoteSyncRoot, note types.VNoteSourceNote, operation string) error {
	ready := time.Now().UTC()
	if operation == "upsert" {
		// Recent edits settle before model admission. Old snapshot notes remain
		// immediately eligible; missing/future clocks wait at most one minute.
		quiet := ready.Add(time.Minute)
		if updated, err := time.Parse(time.RFC3339Nano, note.UpdatedAt); err == nil {
			candidate := updated.UTC().Add(time.Minute)
			if candidate.Before(quiet) {
				quiet = candidate
			}
		}
		if quiet.After(ready) {
			ready = quiet
		}
	}
	row := types.VNoteSyncNote{ID: types.VNoteLogicalKnowledgeID(vnoteKBID(root), note.NoteID), SyncRootID: root.ID, NoteID: note.NoteID, SourceHash: note.SourceHash, Title: note.Title, Operation: operation, Revision: 1, ScanID: root.ScanID, Status: "pending", NextAttemptAt: ready}
	changed := "(vnote_sync_notes.operation<>excluded.operation OR vnote_sync_notes.source_hash<>excluded.source_hash OR vnote_sync_notes.title<>excluded.title)"
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "sync_root_id"}, {Name: "note_id"}}, DoUpdates: clause.Assignments(map[string]any{
		"source_hash": row.SourceHash, "title": row.Title, "operation": operation, "scan_id": root.ScanID,
		"revision":        gorm.Expr("CASE WHEN " + changed + " THEN vnote_sync_notes.revision+1 ELSE vnote_sync_notes.revision END"),
		"status":          gorm.Expr("CASE WHEN " + changed + " THEN 'pending' ELSE vnote_sync_notes.status END"),
		"error_code":      gorm.Expr("CASE WHEN " + changed + " THEN '' ELSE vnote_sync_notes.error_code END"),
		"next_attempt_at": gorm.Expr("CASE WHEN " + changed + " THEN excluded.next_attempt_at ELSE vnote_sync_notes.next_attempt_at END"),
	})}).Create(&row).Error
}

// StoreVNoteSourcePage commits desired versions and their cursor together.
// Failed/partial snapshots cannot generate absence tombstones.
func StoreVNoteSourcePage(ctx context.Context, db *gorm.DB, claim types.VNoteSyncRoot, page types.VNoteSourcePage) error {
	if page.Protocol != 1 || page.Subject != claim.Subject || page.WorkspaceID != claim.WorkspaceID || page.RootID != claim.RootID || page.ConfigVersion != claim.ConfigVersion {
		return ErrVNoteSyncStale
	}
	if err := ValidateVNoteSourcePage(claim, page); err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		root, err := lockedVNoteSyncRoot(ctx, tx, claim)
		if err != nil {
			return err
		}
		if root.Phase != claim.Phase || root.Cursor != claim.Cursor || root.CursorStamp != claim.CursorStamp || root.AfterNotebookID != claim.AfterNotebookID || root.AfterNoteID != claim.AfterNoteID {
			return ErrVNoteSyncStale
		}
		updates := map[string]any{"root_name": page.RootName, "failures": 0, "error_code": ""}
		if root.Phase == "snapshot" {
			if root.SyncIdentity != "" && (root.SyncIdentity != page.SyncIdentity || root.GraphRevision != page.GraphRevision || root.Cursor != page.SnapshotCursor || root.CursorStamp != page.SnapshotCursorStamp) {
				return ErrVNoteSyncStale
			}
			for _, note := range page.Notes {
				if err := upsertVNoteDesired(tx, *root, note, "upsert"); err != nil {
					return err
				}
			}
			updates["sync_identity"], updates["graph_revision"] = page.SyncIdentity, page.GraphRevision
			updates["cursor"], updates["cursor_stamp"] = page.SnapshotCursor, page.SnapshotCursorStamp
			updates["after_notebook_id"], updates["after_note_id"] = page.AfterNotebookID, page.AfterNoteID
			if !page.HasMore {
				// Include copies created by the earlier manual importer, not just notes
				// already known to this queue. Extract only their source IDs, never bodies.
				extraction := "k.metadata->>'vnote_note_id'"
				workspace := "k.metadata->>'vnote_workspace_id'"
				if tx.Dialector.Name() == "sqlite" {
					extraction = "json_extract(k.metadata,'$.vnote_note_id')"
					workspace = "json_extract(k.metadata,'$.vnote_workspace_id')"
				}
				err := tx.Exec(`INSERT INTO vnote_sync_notes(id,sync_root_id,note_id,source_hash,title,operation,revision,scan_id,status,knowledge_id,attempt,next_attempt_at,error_code)
     SELECT s.id,?,`+extraction+`,'','','delete',1,'','pending','','',?,'' FROM vnote_import_states s
     JOIN knowledges k ON k.id=s.knowledge_id AND k.knowledge_base_id=s.knowledge_base_id AND k.tenant_id=? AND k.deleted_at IS NULL
     WHERE s.user_id=? AND s.knowledge_base_id=? AND `+workspace+`=? AND COALESCE(`+extraction+`,'')<>''
     ON CONFLICT DO NOTHING`, root.ID, time.Now().UTC(), root.TenantID, root.UserID, vnoteKBID(*root), root.WorkspaceID).Error
				if err != nil {
					return err
				}
				if err := tx.Model(&types.VNoteSyncNote{}).Where("sync_root_id=? AND scan_id<>? AND operation<>?", root.ID, root.ScanID, "delete").
					Updates(map[string]any{"operation": "delete", "status": "pending", "revision": gorm.Expr("revision+1"), "source_hash": "", "title": "", "error_code": "", "next_attempt_at": time.Now().UTC()}).Error; err != nil {
					return err
				}
				updates["phase"] = "changes"
			}
		} else {
			if page.Rescan || page.SyncIdentity != root.SyncIdentity || page.GraphRevision != root.GraphRevision {
				return ErrVNoteSyncStale
			}
			if page.Cursor < root.Cursor {
				return ErrVNoteSyncStale
			}
			for _, note := range page.Events {
				if err := upsertVNoteDesired(tx, *root, note, note.Operation); err != nil {
					return err
				}
			}
			updates["cursor"], updates["cursor_stamp"] = page.Cursor, page.CursorStamp
			if page.RootName == "" {
				delete(updates, "root_name")
			}
		}
		// Notebook renames are metadata changes and require no document rebuild.
		if page.RootName != "" {
			name := types.VNoteKnowledgeBaseName(page.RootName)
			if err := tx.Model(&types.KnowledgeBase{}).Where("id=? AND tenant_id=? AND private_owner_id=? AND v_note_root_id=? AND name<>?", vnoteKBID(*root), root.TenantID, root.UserID, root.RootID, name).Update("name", name).Error; err != nil {
				return err
			}
		}
		return tx.Model(&types.VNoteSyncRoot{}).Where("id=? AND lease_token=?", root.ID, root.LeaseToken).Updates(updates).Error
	})
}

// Reset preserves the queue and visible documents. Only a complete new scan
// may reconcile absence after cursor loss, restore, or a hierarchy change.
func ResetVNoteSyncSnapshot(ctx context.Context, db *gorm.DB, claim types.VNoteSyncRoot) error {
	return db.WithContext(ctx).Model(&types.VNoteSyncRoot{}).Where("id=? AND lease_token=? AND config_version=?", claim.ID, claim.LeaseToken, claim.ConfigVersion).
		Updates(map[string]any{"phase": "snapshot", "scan_id": uuid.NewString(), "sync_identity": "", "graph_revision": 0, "cursor": 0, "cursor_stamp": "", "after_notebook_id": "", "after_note_id": ""}).Error
}

// Deletion switches visibility only. Physical indexes are reclaimed separately.
// Never remove or terminalize an in-flight external model request to free a slot.
func ApplyVNoteSourceDeletion(ctx context.Context, db *gorm.DB, claim types.VNoteSyncRoot, note types.VNoteSyncNote) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		root, err := lockedVNoteSyncRoot(ctx, tx, claim)
		if err != nil {
			return err
		}
		var desired types.VNoteSyncNote
		if err := tx.Where("id=? AND sync_root_id=? AND revision=? AND operation=?", note.ID, root.ID, note.Revision, "delete").Take(&desired).Error; err != nil {
			return ErrVNoteSyncStale
		}
		var active int64
		if err := tx.Table("vnote_import_gates g").Joins("JOIN vnote_import_generations gen ON gen.attempt=g.attempt AND gen.knowledge_id=g.knowledge_id").
			Joins("JOIN knowledges k ON k.id=gen.knowledge_id AND k.tenant_id=g.tenant_id").Where("g.id=? AND gen.state_id=? AND gen.user_id=? AND k.parse_status IN ?", "index", note.ID, root.UserID, []string{types.ParseStatusPending, types.ParseStatusProcessing, types.ParseStatusFinalizing}).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return ErrVNoteSyncStale
		}
		if err := tx.Model(&types.VNoteImportState{}).Where("id=? AND user_id=? AND knowledge_base_id=?", note.ID, root.UserID, vnoteKBID(*root)).Updates(map[string]any{"knowledge_id": "", "content_hash": ""}).Error; err != nil {
			return err
		}
		return tx.Model(&types.VNoteSyncNote{}).Where("id=? AND revision=?", note.ID, note.Revision).Updates(map[string]any{"status": "done", "knowledge_id": "", "attempt": "", "error_code": ""}).Error
	})
}

// Result updates use both root ownership and desired revision. A completed old
// attempt cannot mark a newer edit as synchronized.
func UpdateVNoteSyncNote(ctx context.Context, db *gorm.DB, claim types.VNoteSyncRoot, note types.VNoteSyncNote, status, knowledgeID, attempt, errorCode string, retryAt time.Time) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockedVNoteSyncRoot(ctx, tx, claim); err != nil {
			return err
		}
		result := tx.Model(&types.VNoteSyncNote{}).Where("id=? AND sync_root_id=? AND revision=? AND operation=? AND source_hash=? AND title=?", note.ID, claim.ID, note.Revision, note.Operation, note.SourceHash, note.Title).
			Updates(map[string]any{"status": status, "knowledge_id": knowledgeID, "attempt": attempt, "error_code": errorCode, "next_attempt_at": retryAt.UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrVNoteSyncStale
		}
		return nil
	})
}
