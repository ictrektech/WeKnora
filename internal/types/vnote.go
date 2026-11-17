package types

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// VNoteLogicalKnowledgeID remains stable while physical indexed generations change.
func VNoteLogicalKnowledgeID(kbID, noteID string) string {
	if kbID == "" || noteID == "" {
		return ""
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(kbID+"\x00"+noteID)).String()
}

type vnoteAdmissionKey struct{}
type vnoteGuardKey struct{}
type vnoteDelegatedKey struct{}
type vnoteReadCheckKey struct{}
type vnoteRootScopeKey struct{}

// VNoteRootScope is server-only authority for metadata provisioning. It must
// match the delivered control while the repository holds that control's lock.
type VNoteRootScope struct {
	UserID, WorkspaceID, Subject, RootID string
	TenantID                             uint64
	ConfigVersion                        int64
}

func WithVNoteRootScope(ctx context.Context, scope VNoteRootScope) context.Context {
	return context.WithValue(ctx, vnoteRootScopeKey{}, scope)
}

func VNoteRootScopeFromContext(ctx context.Context) (VNoteRootScope, bool) {
	scope, ok := ctx.Value(vnoteRootScopeKey{}).(VNoteRootScope)
	return scope, ok
}

// This server-only check is installed on authenticated HTTP requests. Internal
// ingestion contexts retain their separate admission/processing authorization.
func WithVNoteReadCheck(ctx context.Context, check func(string) bool) context.Context {
	return context.WithValue(ctx, vnoteReadCheckKey{}, check)
}
func VNoteReadCheck(ctx context.Context) func(string) bool {
	check, _ := ctx.Value(vnoteReadCheckKey{}).(func(string) bool)
	return check
}

// Only integration handlers can attach this server-side authorization scope.
func WithVNoteDelegatedCredential(ctx context.Context, secret string) context.Context {
	return context.WithValue(ctx, vnoteDelegatedKey{}, secret)
}
func VNoteDelegatedCredential(ctx context.Context) string {
	value, _ := ctx.Value(vnoteDelegatedKey{}).(string)
	return value
}

func WithVNoteAdmission(ctx context.Context, attempt string) context.Context {
	return context.WithValue(ctx, vnoteAdmissionKey{}, attempt)
}
func VNoteAdmissionFromContext(ctx context.Context) string {
	attempt, _ := ctx.Value(vnoteAdmissionKey{}).(string)
	return attempt
}

// WithVNoteWriteGuard attaches an authorization check to an indexing attempt.
// It is re-evaluated after model calls and before writing each batch.
func WithVNoteWriteGuard(ctx context.Context, check func(context.Context) error) context.Context {
	return context.WithValue(ctx, vnoteGuardKey{}, check)
}

func CheckVNoteWriteGuard(ctx context.Context) error {
	if check, ok := ctx.Value(vnoteGuardKey{}).(func(context.Context) error); ok {
		return check(ctx)
	}
	return nil
}

type VNoteSnapshot struct {
	Protocol      int    `json:"protocol"`
	Subject       string `json:"subject"`
	WorkspaceID   string `json:"workspaceId"`
	ConfigVersion int64  `json:"configVersion"`
	RootID        string `json:"rootId"`
	RootName      string `json:"rootName"`
	NoteID        string `json:"noteId"`
	NotebookID    string `json:"notebookId"`
	Title         string `json:"title"`
	Markdown      string `json:"markdown"`
	SourceHash    string `json:"sourceHash"`
	UpdatedAt     string `json:"updatedAt"`
}

type VNoteImportState struct {
	ID              string `gorm:"primaryKey;size:36"`
	UserID          string `gorm:"size:128"`
	KnowledgeBaseID string `gorm:"size:36"`
	KnowledgeID     string `gorm:"size:36"`
	ContentHash     string `gorm:"size:64"`
}

func (VNoteImportState) TableName() string { return "vnote_import_states" }

// VNoteImportGeneration is an unpublished physical document until the source
// state points at its knowledge ID. The source state is the visibility authority.
type VNoteImportGeneration struct {
	Attempt             string `gorm:"primaryKey;size:36"`
	StateID             string `gorm:"size:36;not null;index:idx_vnote_generation_state"`
	KnowledgeID         string `gorm:"size:36;not null;uniqueIndex"`
	ExpectedKnowledgeID string `gorm:"size:36;not null;default:''"`
	UserID              string `gorm:"size:128;not null"`
	WorkspaceID         string `gorm:"size:128;not null"`
	Subject             string `gorm:"size:255;not null"`
	RootID              string `gorm:"size:128;not null"`
	ConfigVersion       int64  `gorm:"not null"`
	ContentHash         string `gorm:"size:64;not null"`
	Status              string `gorm:"size:16;not null;index:idx_vnote_generation_state"`
}

func (VNoteImportGeneration) TableName() string { return "vnote_import_generations" }

type VNoteImportGate struct {
	ID          string `gorm:"primaryKey;size:32"`
	Token       string `gorm:"size:36"`
	LeaseUntil  time.Time
	KnowledgeID string `gorm:"size:36"`
	TenantID    uint64
	ContentHash string `gorm:"size:64"`
	Attempt     string `gorm:"size:36"`
}

func (VNoteImportGate) TableName() string { return "vnote_import_gates" }
