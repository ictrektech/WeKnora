package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type vnoteImportUsers struct {
	interfaces.UserService
	bound bool
}

func (s *vnoteImportUsers) GetPlatformIdentityUser(_ context.Context, _ string, subject string) (*types.User, error) {
	if !s.bound || subject != "stable-subject" {
		return nil, errors.New("not bound")
	}
	return &types.User{ID: "bound-owner", TenantID: 7, IsActive: true}, nil
}
func (*vnoteImportUsers) BindPlatformIdentity(context.Context, string, string, string) error {
	panic("import must not create bindings")
}

type vnoteImportMembers struct{ interfaces.TenantMemberService }

func (*vnoteImportMembers) GetMembership(context.Context, string, uint64) (*types.TenantMember, error) {
	return &types.TenantMember{Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive}, nil
}

type vnoteImportTenants struct{ interfaces.TenantService }

func (*vnoteImportTenants) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return &types.Tenant{ID: 7}, nil
}

type vnoteImportRecorder struct {
	called    bool
	err       error
	caller    types.Caller
	principal types.Principal
}

func (s *vnoteImportRecorder) Import(ctx context.Context, snapshot types.VNoteSnapshot) (*service.VNoteImportResult, error) {
	s.called = true
	s.caller = types.CallerFromContext(ctx)
	s.principal, _ = types.PrincipalFromContext(ctx)
	return &service.VNoteImportResult{Accepted: true, KnowledgeBaseID: "private-kb", KnowledgeID: "doc", State: "pending"}, s.err
}

func TestVNoteImportRequiresStableBindingAndSameSourceSubject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer user-proof" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"sub":"stable-subject","enable":1}`))
	}))
	defer platform.Close()
	t.Setenv("HYBRAG_VOS_OIDC_USERINFO_URL", platform.URL)
	t.Setenv("HYBRAG_VOS_SSO_ENABLED", "true")
	for _, tc := range []struct {
		name      string
		bound     bool
		subject   string
		redirect  bool
		importErr error
		status    int
	}{
		{"owner", true, "stable-subject", false, nil, 202},
		{"missing_binding", false, "stable-subject", false, nil, 409},
		{"wrong_source_owner", true, "other-subject", false, nil, 403},
		{"redirect", true, "stable-subject", true, nil, 409},
		{"busy", true, "stable-subject", false, service.ErrVNoteImportBusy, 409},
		{"embedding unavailable", true, "stable-subject", false, service.ErrVNoteEmbeddingUnavailable, 422},
		{"unknown admission failure", true, "stable-subject", false, errors.New("enqueue outcome uncertain"), 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sourceCalls := 0
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sourceCalls++
				require.Equal(t, "/api/integrations/hybrag/redeem", r.URL.Path)
				require.Equal(t, "Bearer user-proof", r.Header.Get("Authorization"))
				if tc.redirect {
					http.Redirect(w, r, "/redirect", 302)
					return
				}
				_ = json.NewEncoder(w).Encode(types.VNoteSnapshot{Protocol: 1, Subject: tc.subject, RootID: "root", NoteID: "note", Markdown: "body"})
			}))
			defer source.Close()
			t.Setenv("HYBRAG_VNOTE_URL", source.URL)
			imports := &vnoteImportRecorder{err: tc.importErr}
			h := &VNoteImportHandler{imports: imports, users: &vnoteImportUsers{bound: tc.bound}, tenants: &vnoteImportTenants{}, members: &vnoteImportMembers{}}
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/vnote/import-note", strings.NewReader(`{"ticket":"one-use","user_id":"victim","tenant_id":999}`))
			ctx.Request.Header.Set("Authorization", "Bearer user-proof")
			ctx.Request.Header.Set("Content-Type", "application/json")
			h.ImportNote(ctx)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			if errors.Is(tc.importErr, service.ErrVNoteEmbeddingUnavailable) {
				var refusal map[string]any
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &refusal))
				require.Equal(t, "vnote_embedding_unavailable", refusal["code"])
				require.Equal(t, false, refusal["accepted"])
			} else if tc.status == 422 {
				require.NotContains(t, response.Body.String(), "\"accepted\":false")
			}
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			if tc.status == 202 || tc.importErr != nil {
				require.True(t, imports.called)
				require.Equal(t, "bound-owner", imports.caller.UserID)
				require.EqualValues(t, 7, imports.caller.TenantID)
				require.Equal(t, types.PrincipalWebUser, imports.principal.Type)
			} else {
				require.False(t, imports.called)
			}
			if !tc.bound {
				require.Zero(t, sourceCalls)
			} else {
				require.Equal(t, 1, sourceCalls)
			}
		})
	}
}

type vnoteStatusRecorder struct {
	vnoteImportRecorder
	called bool
}

func (s *vnoteStatusRecorder) Status(ctx context.Context, root, note string) (*service.VNoteImportResult, error) {
	s.called = true
	if root != "root" || note != "note" || types.CallerFromContext(ctx).UserID != "bound-owner" {
		return nil, errors.New("wrong caller")
	}
	return &service.VNoteImportResult{Accepted: true, State: "completed"}, nil
}
func TestVNoteStatusRequiresBoundVOSUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer user-proof" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"sub":"stable-subject","enable":1}`))
	}))
	defer platform.Close()
	t.Setenv("HYBRAG_VOS_OIDC_USERINFO_URL", platform.URL)
	t.Setenv("HYBRAG_VOS_SSO_ENABLED", "true")
	for _, bound := range []bool{false, true} {
		recorder := &vnoteStatusRecorder{}
		h := &VNoteImportHandler{imports: recorder, users: &vnoteImportUsers{bound: bound}, members: &vnoteImportMembers{}}
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/vnote/status", strings.NewReader(`{"rootId":"root","noteId":"note"}`))
		ctx.Request.Header.Set("Authorization", "Bearer user-proof")
		ctx.Request.Header.Set("Content-Type", "application/json")
		h.Status(ctx)
		if bound {
			require.Equal(t, 200, response.Code)
			require.Contains(t, response.Body.String(), `"state":"completed"`)
			require.True(t, recorder.called)
		} else {
			require.Equal(t, 409, response.Code)
			require.False(t, recorder.called)
		}
	}
}

type vnoteControlRecorder struct {
	vnoteImportRecorder
	called bool
}

type vnoteDelegatedControlRecorder struct {
	vnoteControlRecorder
	known     bool
	delegated bool
}

func (s *vnoteDelegatedControlRecorder) VNoteDelegationBinding(context.Context, string) (*service.VNoteDelegationBinding, error) {
	if !s.known {
		return nil, errors.New("unknown credential")
	}
	return &service.VNoteDelegationBinding{UserID: "bound-owner", WorkspaceID: "personal", Subject: "stable-subject"}, nil
}
func (s *vnoteDelegatedControlRecorder) ApplyDelegatedControl(ctx context.Context, subject, workspace string, version int64, enabled bool, roots []string, secret string, active bool) error {
	if types.CallerFromContext(ctx).UserID != "bound-owner" || subject != "stable-subject" || workspace != "personal" || (!active && enabled) {
		return errors.New("invalid delegated scope")
	}
	s.delegated = true
	return nil
}

func TestVNoteInactiveGrantCanDeliverOnlyItsOwnDisableControl(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name                   string
		active, enabled, known bool
		workspace              string
		status                 int
	}{
		{"active consent", true, true, false, "personal", 200},
		{"inactive deny", false, false, true, "personal", 200},
		{"inactive cannot enable", false, true, true, "personal", 403},
		{"unknown credential", false, false, false, "personal", 401},
		{"other workspace", false, false, true, "other-space", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"protocol": 1, "active": tc.active, "subject": "stable-subject"})
			}))
			defer platform.Close()
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "Integration "+strings.Repeat("a", 64), r.Header.Get("Authorization"))
				_ = json.NewEncoder(w).Encode(map[string]any{"protocol": 1, "subject": "stable-subject", "workspaceId": tc.workspace, "version": 3, "enabled": tc.enabled, "roots": []string{"root"}})
			}))
			defer source.Close()
			t.Setenv("HYBRAG_VOS_SSO_ENABLED", "true")
			t.Setenv("HYBRAG_VOS_OIDC_USERINFO_URL", platform.URL+"/v1000/oauth2/userinfo")
			t.Setenv("HYBRAG_VNOTE_URL", source.URL)
			recorder := &vnoteDelegatedControlRecorder{known: tc.known}
			h := &VNoteImportHandler{imports: recorder, users: &vnoteImportUsers{bound: true}, members: &vnoteImportMembers{}}
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/control", strings.NewReader(`{"ticket":"one-use","enabled":true}`))
			ctx.Request.Header.Set("Authorization", "Integration "+strings.Repeat("a", 64))
			ctx.Request.Header.Set("Content-Type", "application/json")
			h.Control(ctx)
			require.Equal(t, tc.status, response.Code)
			require.Equal(t, tc.status == 200, recorder.delegated)
			require.NotContains(t, response.Body.String(), strings.Repeat("a", 64))
			if !tc.active {
				statusResponse := httptest.NewRecorder()
				statusCtx, _ := gin.CreateTestContext(statusResponse)
				statusCtx.Request = httptest.NewRequest(http.MethodPost, "/status", strings.NewReader(`{}`))
				statusCtx.Request.Header.Set("Authorization", "Integration "+strings.Repeat("a", 64))
				statusCtx.Request.Header.Set("Content-Type", "application/json")
				h.Status(statusCtx)
				require.Equal(t, http.StatusUnauthorized, statusResponse.Code, "inactive credentials cannot read status or documents")
			}
		})
	}
}

func (s *vnoteControlRecorder) ApplyControl(ctx context.Context, subject, workspace string, version int64, enabled bool, roots []string) error {
	s.called = true
	if types.CallerFromContext(ctx).UserID != "bound-owner" || subject != "stable-subject" || workspace != "personal" || version != 3 || enabled || len(roots) != 1 || roots[0] != "root" {
		return errors.New("wrong control scope")
	}
	return nil
}

func TestVNoteControlRequiresBoundIdentityAndSourceSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"sub":"stable-subject","enable":1}`))
	}))
	defer platform.Close()
	t.Setenv("HYBRAG_VOS_OIDC_USERINFO_URL", platform.URL)
	t.Setenv("HYBRAG_VOS_SSO_ENABLED", "true")
	for _, tc := range []struct {
		name, subject string
		bound         bool
		status        int
	}{
		{"valid", "stable-subject", true, 200}, {"other subject", "other", true, 409}, {"unbound", "stable-subject", false, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, "/api/integrations/hybrag/redeem-control", r.URL.Path)
				require.Equal(t, "Bearer user-proof", r.Header.Get("Authorization"))
				_ = json.NewEncoder(w).Encode(map[string]any{"protocol": 1, "subject": tc.subject, "workspaceId": "personal", "version": 3, "enabled": false, "roots": []string{"root"}})
			}))
			defer source.Close()
			t.Setenv("HYBRAG_VNOTE_URL", source.URL)
			recorder := &vnoteControlRecorder{}
			h := &VNoteImportHandler{imports: recorder, users: &vnoteImportUsers{bound: tc.bound}, members: &vnoteImportMembers{}}
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/control", strings.NewReader(`{"ticket":"one-use","version":999,"enabled":true}`))
			ctx.Request.Header.Set("Authorization", "Bearer user-proof")
			ctx.Request.Header.Set("Content-Type", "application/json")
			h.Control(ctx)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			require.Equal(t, tc.status == 200, recorder.called)
			if !tc.bound {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
		})
	}
}
