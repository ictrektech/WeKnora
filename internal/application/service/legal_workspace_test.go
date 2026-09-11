package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type legalPurgeContractStub struct {
	interfaces.ContractReviewService
	calls int
}

func (s *legalPurgeContractStub) DeleteTenantData(context.Context, uint64) error {
	s.calls++
	return nil
}

type legalPurgeSessionStub struct {
	interfaces.SessionService
	sessions []*types.Session
	calls    int
}

func (s *legalPurgeSessionStub) ListLegalAssistantSessions(context.Context, uint64) ([]*types.Session, error) {
	return s.sessions, nil
}

func (s *legalPurgeSessionStub) DeleteLegalAssistantSessions(context.Context, uint64) error {
	s.calls++
	return nil
}

type legalPurgeTemporaryDocumentStub struct {
	interfaces.TemporaryDocumentService
	documents []*types.TemporaryDocument
	deleted   []string
}

func (s *legalPurgeTemporaryDocumentStub) List(context.Context, uint64, string) ([]*types.TemporaryDocument, error) {
	return s.documents, nil
}

func (s *legalPurgeTemporaryDocumentStub) Delete(_ context.Context, _ uint64, _, documentID string) error {
	s.deleted = append(s.deleted, documentID)
	return nil
}

func TestLegalWorkspacePurgeCleansAttachmentsBeforeSessions(t *testing.T) {
	contract := &legalPurgeContractStub{}
	sessions := &legalPurgeSessionStub{sessions: []*types.Session{{ID: "legal-session"}}}
	documents := &legalPurgeTemporaryDocumentStub{documents: []*types.TemporaryDocument{{ID: "attachment"}}}

	purge := NewLegalWorkspaceDataService(contract, sessions, documents)
	if err := purge.DeleteTenantData(context.Background(), 7); err != nil {
		t.Fatalf("purge failed: %v", err)
	}
	if contract.calls != 1 || sessions.calls != 1 {
		t.Fatalf("purge calls = contract %d, sessions %d; want 1, 1", contract.calls, sessions.calls)
	}
	if len(documents.deleted) != 1 || documents.deleted[0] != "attachment" {
		t.Fatalf("deleted attachments = %#v; want [attachment]", documents.deleted)
	}
}
