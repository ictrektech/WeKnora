package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestRecentDocumentDirectoryFollowsRetrievalScope(t *testing.T) {
	for _, tc := range []struct {
		name    string
		targets types.SearchTargets
		want    []string
	}{
		{"legacy", nil, []string{"chosen", "other-case"}},
		{"whole base", types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "base"}}, []string{"chosen", "other-case"}},
		{"one file", types.SearchTargets{{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "base", KnowledgeIDs: []string{"chosen"}}}, []string{"chosen"}},
		{"other base", types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "elsewhere"}}, nil},
		{"unresolved FAQ tags", types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "base", TagIDs: []string{"tag"}}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &agent.KnowledgeBaseInfo{
				ID: "base", Description: "Workspace-wide case descriptions", Profile: &types.KnowledgeBaseProfile{},
				RecentDocs: []agent.RecentDocInfo{{KnowledgeID: "chosen"}, {KnowledgeID: "other-case"}},
			}
			scopeKnowledgeBasePrompt(tc.targets, info)
			var ids []string
			for _, doc := range info.RecentDocs {
				ids = append(ids, doc.KnowledgeID)
			}
			require.Equal(t, tc.want, ids)
			if tc.name == "legacy" || tc.name == "whole base" {
				require.NotEmpty(t, info.Description)
				require.NotNil(t, info.Profile)
			} else {
				require.Empty(t, info.Description)
				require.Nil(t, info.Profile, "a whole-base profile must not introduce other case facts")
			}
		})
	}
}

func TestPatentKnowledgeScopeRequiresExplicitSelection(t *testing.T) {
	for _, mode := range []types.WorkspaceMode{types.WorkspaceModePatentSearch, types.WorkspaceModePatentWriting} {
		for _, selection := range []string{"empty", "base", "file", "tag", "suggestion"} {
			t.Run(string(mode)+"/"+selection, func(t *testing.T) {
				// A saved, pre-upgrade agent still has all-workspace retrieval enabled.
				req := &types.QARequest{
					Session: &types.Session{TenantID: 1, WorkspaceMode: mode},
					CustomAgent: &types.CustomAgent{TenantID: 1, Config: types.CustomAgentConfig{
						KBSelectionMode: "all", RetrieveKBOnlyWhenMentioned: false,
					}},
				}
				switch selection {
				case "base":
					req.KnowledgeBaseIDs = []string{"chosen-base"}
				case "file":
					req.KnowledgeIDs = []string{"chosen-file"}
				case "tag":
					req.TagScopes = []types.TagScope{{KnowledgeBaseID: "chosen-base", TagIDs: []string{"tag"}}}
				case "suggestion":
					req.QuestionOrigin = &types.QuestionOrigin{KnowledgeBaseID: "unselected-base", KnowledgeID: "other-case"}
				}
				// No KB service: implicit all-workspace resolution would panic here.
				kbIDs, docIDs, err := (&sessionService{}).resolveKnowledgeBases(t.Context(), req)
				require.NoError(t, err)
				require.Equal(t, req.KnowledgeBaseIDs, kbIDs)
				require.Equal(t, req.KnowledgeIDs, docIDs)
				require.False(t, req.CustomAgent.Config.RetrieveKBOnlyWhenMentioned, "do not mutate saved agent configuration")
			})
		}
	}
}

func TestNonPatentKnowledgeScopePreservesAgentDefaults(t *testing.T) {
	for _, mode := range []types.WorkspaceMode{types.WorkspaceModePlatform, types.WorkspaceModeLegalAssistant} {
		req := &types.QARequest{
			Session: &types.Session{TenantID: 1, WorkspaceMode: mode},
			CustomAgent: &types.CustomAgent{TenantID: 1, Config: types.CustomAgentConfig{
				KBSelectionMode: "selected", KnowledgeBases: []string{"default-base"},
			}},
		}
		kbIDs, _, err := (&sessionService{}).resolveKnowledgeBases(context.Background(), req)
		require.NoError(t, err)
		require.Equal(t, []string{"default-base"}, kbIDs)
	}
}

func TestPatentExplicitScopeStillChecksAuthorization(t *testing.T) {
	ctx := types.WithTenantAPIKeyScope(t.Context(), types.TenantAPIKeyScope{
		KnowledgeBaseIDs: types.StringArray{"allowed-base"},
	})
	for _, mode := range []types.WorkspaceMode{types.WorkspaceModePatentSearch, types.WorkspaceModePatentWriting} {
		_, _, err := (&sessionService{}).resolveKnowledgeBases(ctx, &types.QARequest{
			Session:          &types.Session{TenantID: 1, WorkspaceMode: mode},
			KnowledgeBaseIDs: []string{"blocked-base"},
		})
		require.Error(t, err)
	}
}
