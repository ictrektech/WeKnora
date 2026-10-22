package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type patentSessionPurger interface {
	ListPatentSessions(context.Context, uint64) ([]*types.Session, error)
	DeletePatentSessions(context.Context, uint64, []*types.Session) error
}

type patentWorkspaceDataService struct {
	sessions  interfaces.SessionService
	documents interfaces.TemporaryDocumentService
	messages  interfaces.MessageRepository
	files     interfaces.FileService
	catalog   interfaces.ResourceCatalog
}

func NewPatentWorkspaceDataService(sessions interfaces.SessionService, documents interfaces.TemporaryDocumentService, messages interfaces.MessageRepository, files interfaces.FileService, catalog interfaces.ResourceCatalog) interfaces.PatentWorkspaceDataService {
	return &patentWorkspaceDataService{sessions: sessions, documents: documents, messages: messages, files: files, catalog: catalog}
}

func (s *patentWorkspaceDataService) DeleteTenantData(ctx context.Context, tenantID uint64) error {
	purger, ok := s.sessions.(patentSessionPurger)
	if tenantID == 0 || !ok || s.documents == nil {
		return errors.New("patent workspace purge is not configured or workspace id is invalid")
	}
	sessions, err := purger.ListPatentSessions(ctx, tenantID)
	if err != nil {
		return err
	}
	// Keep the session discoverable until all attachment storage has been released.
	for _, session := range sessions {
		documents, err := s.documents.List(ctx, tenantID, session.ID)
		if err != nil {
			return err
		}
		for _, document := range documents {
			if document == nil {
				continue
			}
			if err := s.documents.Delete(ctx, tenantID, session.ID, document.ID); err != nil {
				return fmt.Errorf("delete patent attachment %q: %w", document.ID, err)
			}
		}
	}
	if err := s.releaseMessageResources(ctx, sessions); err != nil {
		return err
	}
	return purger.DeletePatentSessions(ctx, tenantID, sessions)
}

func (s *patentWorkspaceDataService) releaseMessageResources(ctx context.Context, sessions []*types.Session) error {
	if s.messages == nil || s.files == nil || s.catalog == nil {
		return errors.New("patent resource cleanup is not configured")
	}
	owners := make(map[string]map[string]bool)
	// Tombstones retain URLs for retry if releasing a binding or deleting a blob fails.
	for _, session := range sessions {
		for page := 1; ; page++ {
			var messages []*types.Message
			var err error
			if reader, ok := s.messages.(interface {
				GetMessagesForWorkspacePurge(context.Context, string, int, int) ([]*types.Message, error)
			}); ok {
				messages, err = reader.GetMessagesForWorkspacePurge(ctx, session.ID, page, 100)
			} else {
				messages, err = s.messages.GetMessagesBySession(ctx, session.ID, page, 100)
			}
			if err != nil {
				return err
			}
			var refs []types.ArtifactRef
			for _, message := range messages {
				resources, err := s.catalog.ListReferencesByOwner(ctx, types.ResourceOwnerMessage, message.ID)
				if err != nil {
					return err
				}
				for index, artifact := range message.Artifacts {
					resources = append(resources, artifact.URL)
					refs = append(refs, types.ArtifactRef{MessageID: message.ID, Position: index})
				}
				for _, resource := range resources {
					if resource == "" {
						continue
					}
					if owners[resource] == nil {
						owners[resource] = make(map[string]bool)
					}
					owners[resource][message.ID] = true
				}
			}
			if _, err := s.messages.SoftDeleteSessionArtifacts(ctx, session.ID, refs, time.Now()); err != nil {
				return err
			}
			if len(messages) < 100 {
				break
			}
		}
	}
	for resource, messages := range owners {
		remaining := int64(0)
		for messageID := range messages {
			count, err := s.catalog.Release(ctx, resource, types.ResourceOwnerMessage, messageID)
			if errors.Is(err, ErrResourceNotFound) {
				remaining = 1
				break
			}
			if err != nil {
				return err
			}
			remaining = count
		}
		if remaining > 0 {
			continue
		}
		live, err := s.messages.CountLiveArtifactsByURL(ctx, resource)
		if err != nil {
			return err
		}
		if live > 0 {
			continue
		}
		if err := deleteFileIfPresent(ctx, s.files, s.catalog, resource); err != nil {
			return err
		}
	}
	return nil
}

// A retry can encounter a blob deleted before a later storage operation failed.
func deleteFileIfPresent(ctx context.Context, files interfaces.FileService, catalog interfaces.ResourceCatalog, ref string) error {
	err := files.DeleteFile(ctx, ref)
	if errors.Is(err, ErrResourceNotFound) {
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		if _, registered := types.ParseResourcePath(ref); registered && catalog != nil {
			return catalog.MarkDeleted(ctx, ref)
		}
		return nil
	}
	return err
}
