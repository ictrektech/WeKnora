package repository

import (
	"encoding/hex"

	"github.com/Tencent/WeKnora/internal/types"
)

// Treat every remote field as untrusted. In particular an omitted/partial page
// must not look like a successful empty scan that deletes all retained copies.
func ValidateVNoteSourcePage(root types.VNoteSyncRoot, page types.VNoteSourcePage) error {
	if page.Protocol != 1 || page.Subject != root.Subject || page.WorkspaceID != root.WorkspaceID || page.RootID != root.RootID || page.ConfigVersion != root.ConfigVersion || page.SyncIdentity == "" || len(page.SyncIdentity) > 255 || page.GraphRevision < 0 || len(page.RootName) > 2048 {
		return ErrVNoteSyncStale
	}
	cursor, stamp := page.Cursor, page.CursorStamp
	notes := page.Events
	if root.Phase == "snapshot" {
		cursor, stamp = page.SnapshotCursor, page.SnapshotCursorStamp
		notes = page.Notes
	}
	if notes == nil {
		return ErrVNoteSyncStale
	}
	if cursor < 0 || cursor > 9007199254740991 || cursor == 0 && stamp != "" || cursor > 0 && !validVNoteStamp(stamp) {
		return ErrVNoteSyncStale
	}
	if root.Phase != "snapshot" && root.Phase != "changes" || len(notes) > 100 || root.Phase == "snapshot" && len(notes) > 50 {
		return ErrVNoteSyncStale
	}
	seen := make(map[string]bool, len(notes))
	lastBook, lastNote, lastSequence := root.AfterNotebookID, root.AfterNoteID, root.Cursor
	for _, note := range notes {
		if note.NoteID == "" || len(note.NoteID) > 128 || seen[note.NoteID] {
			return ErrVNoteSyncStale
		}
		seen[note.NoteID] = true
		if root.Phase == "changes" {
			if note.Sequence <= lastSequence || note.Sequence > cursor || note.Operation != "upsert" && note.Operation != "delete" {
				return ErrVNoteSyncStale
			}
			lastSequence = note.Sequence
		}
		if root.Phase == "snapshot" || note.Operation == "upsert" {
			if note.NotebookID == "" || len(note.NotebookID) > 128 || len(note.Title) > 8004 || len(note.SourceHash) > 65 || len(note.UpdatedAt) > 128 {
				return ErrVNoteSyncStale
			}
			if root.Phase == "snapshot" {
				if note.NotebookID < lastBook || note.NotebookID == lastBook && note.NoteID <= lastNote {
					return ErrVNoteSyncStale
				}
				lastBook, lastNote = note.NotebookID, note.NoteID
			}
		} else if note.NotebookID != "" || note.Title != "" || note.SourceHash != "" {
			return ErrVNoteSyncStale
		}
	}
	if root.Phase == "snapshot" && (page.AfterNotebookID != lastBook || page.AfterNoteID != lastNote || page.HasMore && len(notes) == 0) {
		return ErrVNoteSyncStale
	}
	if root.Phase == "changes" && (cursor < root.Cursor || page.HasMore && cursor == root.Cursor) {
		return ErrVNoteSyncStale
	}
	return nil
}

func validVNoteStamp(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
