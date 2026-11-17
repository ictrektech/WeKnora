package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"gorm.io/gorm"
)

// VNoteControl is a complete source configuration snapshot, not a credential.
// Its caller must independently authenticate and bind the source identity.
type VNoteControl struct {
	UserID      string `gorm:"primaryKey;size:128"`
	WorkspaceID string `gorm:"primaryKey;size:128"`
	Subject     string `gorm:"size:255;not null"`
	Version     int64  `gorm:"not null"`
	Enabled     bool   `gorm:"not null"`
	RootsJSON   string `gorm:"type:text;not null"`
}

func (VNoteControl) TableName() string { return "vnote_controls" }

var ErrVNoteControlConflict = errors.New("V-Note control identity or version conflict")
var ErrVNoteControlStale = errors.New("V-Note control is older than the applied version")

// ApplyVNoteControl advances a complete snapshot monotonically. Duplicate
// delivery is safe, while conflicting data for the same version is rejected.
func ApplyVNoteControl(ctx context.Context, db *gorm.DB, userID, workspaceID, subject string, version int64, enabled bool, roots []string) error {
	if userID == "" || len(userID) > 128 || workspaceID == "" || len(workspaceID) > 128 || subject == "" || len(subject) > 255 || version < 1 || len(roots) > 100 || (enabled && len(roots) == 0) {
		return ErrVNoteControlConflict
	}
	sorted := append([]string(nil), roots...)
	sort.Strings(sorted)
	for i, root := range sorted {
		if root == "" || len(root) > 128 || (i > 0 && sorted[i-1] == root) {
			return ErrVNoteControlConflict
		}
	}
	if sorted == nil {
		sorted = []string{}
	}
	encoded, err := json.Marshal(sorted)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`INSERT INTO vnote_controls (user_id, workspace_id, subject, version, enabled, roots_json)
          VALUES (?, ?, ?, ?, ?, ?)
          ON CONFLICT (user_id, workspace_id) DO UPDATE SET version = excluded.version,
          enabled = excluded.enabled, roots_json = excluded.roots_json
          WHERE vnote_controls.subject = excluded.subject AND vnote_controls.version < excluded.version`,
			userID, workspaceID, subject, version, enabled, string(encoded))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 1 {
			return nil
		}
		var existing VNoteControl
		if err := tx.Where("user_id = ? AND workspace_id = ?", userID, workspaceID).First(&existing).Error; err != nil {
			return err
		}
		if existing.Subject != subject || existing.Version == version && (existing.Enabled != enabled || existing.RootsJSON != string(encoded)) {
			return ErrVNoteControlConflict
		}
		if existing.Version > version {
			return ErrVNoteControlStale
		}
		return nil
	})
}

// VNoteControlAllows fails closed for a missing, stale, disabled, or differently
// bound snapshot. Callers must also validate any descendant-specific scope.
func VNoteControlAllows(ctx context.Context, db *gorm.DB, userID, workspaceID, subject, rootID string, version int64) (bool, error) {
	var control VNoteControl
	err := db.WithContext(ctx).Where("user_id = ? AND workspace_id = ? AND subject = ? AND version = ? AND enabled = ?", userID, workspaceID, subject, version, true).First(&control).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var roots []string
	if err := json.Unmarshal([]byte(control.RootsJSON), &roots); err != nil {
		return false, err
	}
	for _, root := range roots {
		if root == rootID && rootID != "" {
			return true, nil
		}
	}
	return false, nil
}
