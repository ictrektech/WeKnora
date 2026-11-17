package service

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
)

// hideUnpublishedVNoteHits runs before fusion, tracing and downstream rerank.
// The durable source pointer, not a vector store's enable flag, decides which
// physical generation can supply content to a caller.
func (s *knowledgeBaseService) hideUnpublishedVNoteHits(ctx context.Context, kbs []*types.KnowledgeBase, results []*types.RetrieveResult) error {
	hasVNote := false
	authorized := make(map[string]bool, len(kbs))
	for _, kb := range kbs {
		if kb != nil {
			authorized[kb.ID] = true
			if kb.VNoteRootID != "" {
				hasVNote = true
			}
		}
	}
	if !hasVNote {
		return nil
	}
	visibility, ok := s.kgRepo.(interface {
		VisibleKnowledgeScopes(context.Context, []string) (map[string]string, error)
	})
	if !ok {
		return errors.New("V-Note generation visibility is unavailable")
	}
	ids := make([]string, 0)
	seen := make(map[string]bool)
	for _, result := range results {
		if result == nil {
			continue
		}
		for _, hit := range result.Results {
			if hit != nil && hit.KnowledgeID != "" && !seen[hit.KnowledgeID] {
				seen[hit.KnowledgeID] = true
				ids = append(ids, hit.KnowledgeID)
			}
		}
	}
	scopes, err := visibility.VisibleKnowledgeScopes(ctx, ids)
	if err != nil {
		return err
	}
	for _, result := range results {
		if result == nil {
			continue
		}
		visible := result.Results[:0]
		for _, hit := range result.Results {
			if hit != nil && hit.KnowledgeID != "" && hit.KnowledgeBaseID != "" && scopes[hit.KnowledgeID] == hit.KnowledgeBaseID && authorized[hit.KnowledgeBaseID] {
				visible = append(visible, hit)
			}
		}
		result.Results = visible
	}
	return nil
}
