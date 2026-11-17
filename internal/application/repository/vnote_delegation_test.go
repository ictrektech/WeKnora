package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestVNoteDelegationEncryptionRequiresKeyAndExactOwner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&VNoteDelegation{}))
	row := VNoteDelegation{UserID: "alice", WorkspaceID: "space", TenantID: 7, Subject: "subject"}
	secret := strings.Repeat("a", 64)
	t.Setenv("SYSTEM_AES_KEY", "")
	require.Error(t, StoreVNoteDelegation(context.Background(), db, row, secret))
	t.Setenv("SYSTEM_AES_KEY", strings.Repeat("k", 32))
	require.NoError(t, StoreVNoteDelegation(context.Background(), db, row, secret))
	require.NoError(t, db.First(&row).Error)
	require.NotContains(t, row.SecretEncrypted, secret)
	exposed, err := json.Marshal(row)
	require.NoError(t, err)
	require.NotContains(t, string(exposed), row.SecretEncrypted)
	value, err := ReadVNoteDelegationSecret(row)
	require.NoError(t, err)
	require.Equal(t, secret, value)
	changed := row
	changed.UserID = "bob"
	_, err = ReadVNoteDelegationSecret(changed)
	require.Error(t, err)
	t.Setenv("SYSTEM_AES_KEY_PREVIOUS", strings.Repeat("k", 32))
	t.Setenv("SYSTEM_AES_KEY", strings.Repeat("n", 32))
	value, err = ReadVNoteDelegationSecret(row)
	require.NoError(t, err)
	require.Equal(t, secret, value)
	row.SecretEncrypted = secret
	_, err = ReadVNoteDelegationSecret(row)
	require.Error(t, err, "plaintext is never a stored credential")
}

func TestVNoteDelegationLookupRequiresOwnedManagedKBAndActiveMembership(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&VNoteDelegation{}, &types.User{}, &types.TenantMember{}, &types.KnowledgeBase{}))
	require.NoError(t, db.Create(&types.User{ID: "alice", Username: "alice", Email: "alice@test", TenantID: 7, IsActive: true}).Error)
	member := types.TenantMember{UserID: "alice", TenantID: 7, Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive}
	require.NoError(t, db.Create(&member).Error)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb", TenantID: 7, PrivateOwnerID: "alice", VNoteRootID: "root"}).Error)
	t.Setenv("SYSTEM_AES_KEY", strings.Repeat("k", 32))
	require.NoError(t, StoreVNoteDelegation(context.Background(), db, VNoteDelegation{UserID: "alice", WorkspaceID: "space", TenantID: 7, Subject: "subject"}, strings.Repeat("a", 64)))
	note := &types.Knowledge{TenantID: 7, KnowledgeBaseID: "kb", Metadata: []byte(`{"vnote_owner_id":"alice","vnote_workspace_id":"space","vnote_subject":"subject","vnote_root_id":"root"}`)}
	repo := &knowledgeRepository{db: db}
	_, err = repo.VNoteDelegationForKnowledge(context.Background(), note)
	require.NoError(t, err)
	note.KnowledgeBaseID = "ordinary-kb"
	_, err = repo.VNoteDelegationForKnowledge(context.Background(), note)
	require.Error(t, err)
	note.KnowledgeBaseID = "kb"
	require.NoError(t, db.Model(&member).Update("role", types.TenantRoleContributor).Error)
	_, err = repo.VNoteDelegationForKnowledge(context.Background(), note)
	require.Error(t, err)
}
