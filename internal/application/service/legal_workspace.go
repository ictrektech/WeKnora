package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type legalWorkspaceDataService struct {
	contractReview interfaces.ContractReviewService
	sessions       interfaces.SessionService
	temporaryDocs  interfaces.TemporaryDocumentService
}

// NewLegalWorkspaceDataService composes the existing contract-review purge
// with the legal-assistant session purge. Keeping this coordinator separate
// avoids widening the contract-review service's constructor and preserves its
// existing unit-test doubles.
func NewLegalWorkspaceDataService(
	contractReview interfaces.ContractReviewService,
	sessions interfaces.SessionService,
	temporaryDocs interfaces.TemporaryDocumentService,
) interfaces.LegalWorkspaceDataService {
	return &legalWorkspaceDataService{
		contractReview: contractReview,
		sessions:       sessions,
		temporaryDocs:  temporaryDocs,
	}
}

type legalSessionLister interface {
	ListLegalAssistantSessions(context.Context, uint64) ([]*types.Session, error)
}

func (s *legalWorkspaceDataService) deleteTemporaryDocuments(ctx context.Context, tenantID uint64) error {
	lister, ok := s.sessions.(legalSessionLister)
	if !ok {
		return errors.New("legal session list capability is not configured")
	}
	if s.temporaryDocs == nil {
		return errors.New("temporary document service is not configured")
	}
	sessions, err := lister.ListLegalAssistantSessions(ctx, tenantID)
	if err != nil {
		return err
	}
	var joined error
	for _, session := range sessions {
		if session == nil {
			continue
		}
		documents, err := s.temporaryDocs.List(ctx, tenantID, session.ID)
		if err != nil {
			joined = errors.Join(joined, fmt.Errorf("list attachments for legal session %q: %w", session.ID, err))
			continue
		}
		for _, document := range documents {
			if document == nil {
				continue
			}
			if err := s.temporaryDocs.Delete(ctx, tenantID, session.ID, document.ID); err != nil {
				joined = errors.Join(joined, fmt.Errorf("delete attachment %q: %w", document.ID, err))
			}
		}
	}
	return joined
}

func (s *legalWorkspaceDataService) DeleteTenantData(ctx context.Context, tenantID uint64) error {
	if tenantID == 0 {
		return errors.New("workspace id is required")
	}
	if s.contractReview == nil || s.sessions == nil {
		return errors.New("legal workspace data service is not configured")
	}
	// Each component is idempotent at the database layer. Run both operations
	// even when the first one fails so a transient contract-storage failure does
	// not prevent legal-assistant rows from being purged on the same request.
	var joined error
	if err := s.contractReview.DeleteTenantData(ctx, tenantID); err != nil {
		joined = errors.Join(joined, fmt.Errorf("delete contract review data: %w", err))
	}
	// Temporary documents own physical attachment bytes. Do this before the
	// session rows are soft-deleted so a provider failure leaves the legal
	// session discoverable for a later retry instead of orphaning its files.
	if err := s.deleteTemporaryDocuments(ctx, tenantID); err != nil {
		return errors.Join(joined, fmt.Errorf("delete legal assistant attachments: %w", err))
	}
	if err := s.sessions.DeleteLegalAssistantSessions(ctx, tenantID); err != nil {
		joined = errors.Join(joined, fmt.Errorf("delete legal assistant sessions: %w", err))
	}
	return joined
}
