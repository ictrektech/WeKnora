package types

import (
	"context"
	"strings"
	"time"
)

type vnoteScheduledKey struct{}

func WithVNoteScheduledOperation(ctx context.Context) context.Context {
	return context.WithValue(ctx, vnoteScheduledKey{}, true)
}
func VNoteScheduledOperation(ctx context.Context) bool {
	value, _ := ctx.Value(vnoteScheduledKey{}).(bool)
	return value
}

func VNoteKnowledgeBaseName(rootName string) string {
	name := []rune("V-Note / " + strings.TrimSpace(rootName))
	if len(name) > 255 {
		name = name[:255]
	}
	return string(name)
}

// Persisted bookmarks and desired state contain metadata only, never note bodies.
type VNoteSyncRoot struct {
	ID              string `gorm:"primaryKey;size:36"`
	UserID          string
	WorkspaceID     string
	Subject         string
	TenantID        uint64
	RootID          string
	ConfigVersion   int64
	RootName        string
	Phase           string
	ScanID          string
	SyncIdentity    string
	GraphRevision   int64
	Cursor          int64
	CursorStamp     string
	AfterNotebookID string
	AfterNoteID     string
	LeaseToken      string
	LeaseUntil      time.Time
	NextPollAt      time.Time
	NextMetadataAt  time.Time
	Failures        int
	ErrorCode       string
}

func (VNoteSyncRoot) TableName() string { return "vnote_sync_roots" }

type VNoteSyncNote struct {
	ID            string `gorm:"primaryKey;size:36"`
	SyncRootID    string `gorm:"uniqueIndex:idx_vnote_sync_note"`
	NoteID        string `gorm:"uniqueIndex:idx_vnote_sync_note"`
	SourceHash    string
	Title         string
	Operation     string
	Revision      int64
	ScanID        string
	Status        string
	KnowledgeID   string
	Attempt       string
	NextAttemptAt time.Time
	ErrorCode     string
}

func (VNoteSyncNote) TableName() string { return "vnote_sync_notes" }

type VNoteSourceNote struct {
	NoteID     string `json:"noteId"`
	NotebookID string `json:"notebookId"`
	Title      string `json:"title"`
	SourceHash string `json:"sourceHash"`
	UpdatedAt  string `json:"updatedAt"`
	Operation  string `json:"operation,omitempty"`
	Sequence   int64  `json:"sequence,omitempty"`
}
type VNoteSourcePage struct {
	Protocol            int               `json:"protocol"`
	Subject             string            `json:"subject"`
	WorkspaceID         string            `json:"workspaceId"`
	RootID              string            `json:"rootId"`
	ConfigVersion       int64             `json:"configVersion"`
	RootName            string            `json:"rootName"`
	SyncIdentity        string            `json:"syncIdentity"`
	GraphRevision       int64             `json:"graphRevision"`
	SnapshotCursor      int64             `json:"snapshotCursor"`
	SnapshotCursorStamp string            `json:"snapshotCursorStamp"`
	Cursor              int64             `json:"cursor"`
	CursorStamp         string            `json:"cursorStamp"`
	AfterNotebookID     string            `json:"afterNotebookId"`
	AfterNoteID         string            `json:"afterNoteId"`
	HasMore             bool              `json:"hasMore"`
	Rescan              bool              `json:"rescan"`
	Notes               []VNoteSourceNote `json:"notes"`
	Events              []VNoteSourceNote `json:"events"`
}
