package access

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

func CanReadPrivateKB(ctx context.Context, kb *types.KnowledgeBase) bool {
	if kb == nil {
		return false
	}
	if kb.PrivateOwnerID == "" {
		return true
	}
	caller := types.CallerFromContext(ctx)
	if caller.UserID != kb.PrivateOwnerID || caller.TenantID != kb.TenantID || ctx.Value(types.SharedAgentGrantContextKey) != nil {
		return false
	}
	principal, present := types.PrincipalFromContext(ctx)
	if present && (principal.Type != types.PrincipalWebUser || principal.ID != kb.PrivateOwnerID) {
		return false
	}
	if kb.VNoteRootID != "" {
		if types.ActingViaFromContext(ctx) != "" {
			return false
		}
		if check := types.VNoteReadCheck(ctx); check != nil {
			return check(kb.PrivateOwnerID)
		}
	}
	return true
}

func CanLoadPrivateKB(ctx context.Context, kb *types.KnowledgeBase) bool {
	caller := types.CallerFromContext(ctx)
	_, hasPrincipal := types.PrincipalFromContext(ctx)
	_, hasKey := types.TenantAPIKeyScopeFromContext(ctx)
	if caller.UserID == "" && !hasPrincipal && !hasKey {
		return true
	}
	return CanReadPrivateKB(ctx, kb)
}

func FilterPrivateKBs(ctx context.Context, rows []*types.KnowledgeBase) []*types.KnowledgeBase {
	visible := make([]*types.KnowledgeBase, 0, len(rows))
	for _, kb := range rows {
		if CanLoadPrivateKB(ctx, kb) {
			visible = append(visible, kb)
		}
	}
	return visible
}
