package service

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// RecoverVNotePending repairs delivery only. A persistent processing/finalizing
// row is never replayed, even if its queue task disappeared. The worker's atomic
// pending-to-processing claim arbitrates concurrent original/recovered delivery.
func (s *knowledgeService) RecoverVNotePending(ctx context.Context, id, attempt string) error {
	caller := types.CallerFromContext(ctx)
	if caller.UserID == "" || caller.TenantID == 0 || caller.Role != types.TenantRoleOwner || id == "" || attempt == "" {
		return access.ErrForbidden
	}
	taskID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(id+attempt)).String()
	var archived bool
	if inspector, ok := s.taskInspector.(interfaces.RuntimeTaskInspector); ok {
		task, supported, err := inspector.GetRuntimeTask(ctx, types.QueueDefault, taskID)
		if err != nil && !(supported && errors.Is(err, asynq.ErrTaskNotFound)) {
			return err
		}
		if supported && err == nil && task == nil {
			return errors.New("V-Note queue inspection returned no state")
		}
		if supported && task != nil {
			if task.ID != taskID || task.Type != types.TypeManualProcess || task.TenantID != caller.TenantID || task.KnowledgeID != id {
				return access.ErrForbidden
			}
			switch task.State {
			case types.RuntimeTaskPending, types.RuntimeTaskActive, types.RuntimeTaskScheduled, types.RuntimeTaskRetry:
				// Legitimate queue delay is not a lost task. Do not scan queues,
				// duplicate it, or override retry/scheduling backoff.
				return nil
			case types.RuntimeTaskArchived:
				archived = true
			default:
				return errors.New("V-Note pending task requires queue diagnosis")
			}
		}
	}
	ctx = types.WithVNoteAdmission(ctx, attempt)
	note, err := s.repo.GetKnowledgeByID(ctx, caller.TenantID, id)
	if err != nil {
		return err
	}
	if note.ParseStatus != types.ParseStatusPending {
		return nil
	}
	meta := note.GetMetadata()
	if note.Channel != "vnote" || meta["vnote_attempt"] != attempt || meta["vnote_owner_id"] != caller.UserID || meta["vnote_authorization"] != "delegated" {
		return access.ErrForbidden
	}
	checker, ok := s.repo.(interface {
		CheckVNotePending(context.Context, string, string, string) error
	})
	if !ok {
		return errors.New("V-Note pending admission check is unavailable")
	}
	if err := checker.CheckVNotePending(ctx, id, meta["vnote_content_hash"], attempt); err != nil {
		return err
	}
	if err := s.checkVNoteBackgroundAuthorization(ctx, note); err != nil {
		return err
	}
	if archived {
		// Restore the exact retained payload/ID; no new attempt or model slot.
		inspector := s.taskInspector.(interfaces.RuntimeTaskInspector)
		supported, err := inspector.RunRuntimeTask(ctx, types.QueueDefault, taskID)
		if !supported && err == nil {
			return errors.New("V-Note queue recovery is unavailable")
		}
		return err
	}
	manual, err := note.ManualMetadata()
	if err != nil || manual == nil || manual.Content == "" || s.task == nil {
		return errors.New("V-Note pending payload is unavailable")
	}
	_, err = s.enqueueManualProcessing(ctx, note, manual.Content, true)
	return err
}
