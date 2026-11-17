package repository

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type VNoteDelegation struct {
	UserID          string `gorm:"primaryKey;size:128"`
	WorkspaceID     string `gorm:"primaryKey;size:128"`
	TenantID        uint64 `gorm:"not null"`
	Subject         string `gorm:"size:255;not null"`
	SecretHash      string `json:"-" gorm:"size:64;not null;uniqueIndex"`
	SecretEncrypted string `json:"-" gorm:"type:text;not null"`
}

func (VNoteDelegation) TableName() string { return "vnote_delegations" }

var validVNoteSecret = regexp.MustCompile(`^[0-9a-f]{64}$`)
var ErrVNoteDelegation = errors.New("V-Note background authorization is unavailable")

func VNoteSecretHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func vnoteCredentialKey(row VNoteDelegation, raw string) ([]byte, error) {
	if len(raw) != 32 || raw == "your-32-byte-long-encryption-key!" {
		return nil, ErrVNoteDelegation
	}
	owner, _ := json.Marshal([]any{"weknora:vnote-delegation:v1", row.UserID, row.WorkspaceID, row.Subject, row.TenantID})
	mac := hmac.New(sha256.New, []byte(raw))
	_, _ = mac.Write(owner)
	return mac.Sum(nil), nil
}

func StoreVNoteDelegation(ctx context.Context, db *gorm.DB, row VNoteDelegation, secret string) error {
	if !validVNoteSecret.MatchString(secret) || row.UserID == "" || row.WorkspaceID == "" || row.Subject == "" || row.TenantID == 0 {
		return ErrVNoteDelegation
	}
	keys, err := utils.VNoteCredentialKeys()
	if err != nil {
		return ErrVNoteDelegation
	}
	key, err := vnoteCredentialKey(row, string(keys[0]))
	if err != nil {
		return err
	}
	row.SecretEncrypted, err = utils.EncryptAESGCM(secret, key)
	if err != nil || !strings.HasPrefix(row.SecretEncrypted, utils.EncPrefix) {
		return ErrVNoteDelegation
	}
	row.SecretHash = VNoteSecretHash(secret)
	return db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "workspace_id"}}, DoUpdates: clause.AssignmentColumns([]string{"tenant_id", "subject", "secret_hash", "secret_encrypted"})}).Create(&row).Error
}

func ReadVNoteDelegationSecret(row VNoteDelegation) (string, error) {
	if !strings.HasPrefix(row.SecretEncrypted, utils.EncPrefix) {
		return "", ErrVNoteDelegation
	}
	keys, err := utils.VNoteCredentialKeys()
	if err != nil {
		return "", ErrVNoteDelegation
	}
	for _, raw := range keys {
		key, err := vnoteCredentialKey(row, string(raw))
		if err != nil {
			continue
		}
		value, err := utils.DecryptAESGCM(row.SecretEncrypted, key)
		if err == nil && validVNoteSecret.MatchString(value) && VNoteSecretHash(value) == row.SecretHash {
			return value, nil
		}
	}
	return "", ErrVNoteDelegation
}

func (r *knowledgeRepository) VNoteDelegationForKnowledge(ctx context.Context, note *types.Knowledge) (*VNoteDelegation, error) {
	metadata := note.GetMetadata()
	var row VNoteDelegation
	err := r.db.WithContext(ctx).Table("vnote_delegations AS d").Select("d.*").
		Joins("JOIN knowledge_bases kb ON kb.id = ? AND kb.tenant_id=d.tenant_id AND kb.private_owner_id=d.user_id AND kb.v_note_root_id = ? AND kb.deleted_at IS NULL", note.KnowledgeBaseID, metadata["vnote_root_id"]).
		Joins("JOIN users u ON u.id=d.user_id AND u.tenant_id=d.tenant_id AND u.is_active = ?", true).
		Joins("JOIN tenant_members m ON m.user_id=u.id AND m.tenant_id=d.tenant_id AND m.status = ? AND m.role = ?", types.TenantMemberStatusActive, types.TenantRoleOwner).
		Where("d.user_id = ? AND d.workspace_id = ? AND d.subject = ? AND d.tenant_id = ?", metadata["vnote_owner_id"], metadata["vnote_workspace_id"], metadata["vnote_subject"], note.TenantID).Take(&row).Error
	if err != nil {
		return nil, ErrVNoteDelegation
	}
	return &row, nil
}

func (r *knowledgeRepository) CancelVNoteAuthorizationAttempt(ctx context.Context, note *types.Knowledge) error {
	attempt := note.GetMetadata()["vnote_attempt"]
	if attempt == "" {
		return ErrVNoteDelegation
	}
	if note.ParseStatus != types.ParseStatusPending && note.ParseStatus != types.ParseStatusProcessing {
		return ErrVNoteDelegation
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var generation types.VNoteImportGeneration
		if err := tx.Where("attempt = ? AND knowledge_id = ? AND status = ?", attempt, note.ID, "staged").Take(&generation).Error; err != nil {
			return err
		}
		// A request that observed pending cannot retire another worker that
		// claimed processing while its remote authorization check was in flight.
		result := tx.Model(&types.Knowledge{}).Where("id = ? AND tenant_id = ? AND parse_status = ?", note.ID, note.TenantID, note.ParseStatus).
			Where("EXISTS (SELECT 1 FROM vnote_import_gates g WHERE g.id = ? AND g.knowledge_id = ? AND g.attempt = ?)", "index", note.ID, attempt).
			Updates(map[string]any{"parse_status": types.ParseStatusCancelled, "error_message": "Background authorization or the source version changed; retry synchronization"})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrVNoteDelegation
		}
		return tx.Model(&types.VNoteImportGeneration{}).Where("attempt = ? AND status = ?", attempt, "staged").Update("status", "cancelled").Error
	})
}
