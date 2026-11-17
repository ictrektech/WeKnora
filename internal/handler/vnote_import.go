package handler

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/platformidentity"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
)

type vnoteImporter interface {
	Import(context.Context, types.VNoteSnapshot) (*service.VNoteImportResult, error)
}

type VNoteImportHandler struct {
	imports vnoteImporter
	users   interfaces.UserService
	tenants interfaces.TenantService
	members interfaces.TenantMemberService
}

func NewVNoteImportHandler(imports *service.VNoteImportService, users interfaces.UserService, tenants interfaces.TenantService, members interfaces.TenantMemberService) *VNoteImportHandler {
	return &VNoteImportHandler{imports: imports, users: users, tenants: tenants, members: members}
}

func verifyVNoteIdentity(ctx context.Context, scheme, secret string) (*platformidentity.Identity, error) {
	if strings.EqualFold(scheme, "Integration") {
		return platformidentity.VerifyVNoteGrant(ctx, vosOIDCUserinfoURL(), secret)
	}
	if strings.EqualFold(scheme, "Bearer") {
		return platformidentity.VerifyVOS(ctx, vosOIDCUserinfoURL(), secret)
	}
	return nil, stderrors.New("unsupported integration authorization")
}

// Control accepts only a one-use source ticket, then redeems the authoritative
// snapshot under the same verified VOS user; client-supplied scope is ignored.
func (h *VNoteImportHandler) Control(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !vosSSOEnabled() {
		c.JSON(http.StatusForbidden, gin.H{"error": "VOS authentication is disabled"})
		return
	}
	auth := strings.Fields(c.GetHeader("Authorization"))
	if len(auth) != 2 || (!strings.EqualFold(auth[0], "Bearer") && !strings.EqualFold(auth[0], "Integration")) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "A VOS user proof is required"})
		return
	}
	identity, err := verifyVNoteIdentity(c.Request.Context(), auth[0], auth[1])
	delegated := strings.EqualFold(auth[0], "Integration")
	active := err == nil
	var inactiveBinding *service.VNoteDelegationBinding
	if err != nil && delegated {
		if lookup, ok := h.imports.(interface {
			VNoteDelegationBinding(context.Context, string) (*service.VNoteDelegationBinding, error)
		}); ok {
			inactiveBinding, err = lookup.VNoteDelegationBinding(c.Request.Context(), auth[1])
			if err == nil && inactiveBinding != nil {
				identity = &platformidentity.Identity{Subject: inactiveBinding.Subject}
			} else {
				err = stderrors.New("No matching integration binding")
			}
		}
	}
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Cannot verify the current VOS user"})
		return
	}
	identities, ok := h.users.(platformIdentityService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Identity binding is unavailable"})
		return
	}
	user, err := identities.GetPlatformIdentityUser(c.Request.Context(), vosOIDCUserinfoURL(), identity.Subject)
	if err != nil || user == nil || !user.IsActive || user.TenantID == 0 {
		c.JSON(http.StatusConflict, gin.H{"code": "identity_binding_required"})
		return
	}
	if inactiveBinding != nil && inactiveBinding.UserID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Integration owner changed"})
		return
	}
	membership, err := h.members.GetMembership(c.Request.Context(), user.ID, user.TenantID)
	if err != nil || membership == nil || membership.Status != types.TenantMemberStatusActive || membership.Role != types.TenantRoleOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "An owned personal workspace is required"})
		return
	}
	var input struct {
		Ticket string `json:"ticket" binding:"required,max=256"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32768)
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "A control ticket is required"})
		return
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("HYBRAG_VNOTE_URL")), "/")
	if base == "" {
		base = "http://v-note-backend:8787"
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "V-Note service address is invalid"})
		return
	}
	body, _ := json.Marshal(gin.H{"ticket": input.Ticket})
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, base+"/api/integrations/hybrag/redeem-control", bytes.NewReader(body))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "V-Note unavailable"})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", auth[0]+" "+auth[1])
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "V-Note unavailable"})
		return
	}
	defer resp.Body.Close()
	var snapshot struct {
		Protocol    int      `json:"protocol"`
		Subject     string   `json:"subject"`
		WorkspaceID string   `json:"workspaceId"`
		Version     int64    `json:"version"`
		Enabled     bool     `json:"enabled"`
		Roots       []string `json:"roots"`
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 32769))
	if err != nil || resp.StatusCode != http.StatusOK || len(payload) > 32768 || json.Unmarshal(payload, &snapshot) != nil || snapshot.Protocol != 1 || snapshot.Subject != identity.Subject {
		c.JSON(http.StatusConflict, gin.H{"error": "Invalid source control snapshot"})
		return
	}
	writer, ok := h.imports.(interface {
		ApplyControl(context.Context, string, string, int64, bool, []string) error
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Control unavailable"})
		return
	}
	ctx := types.WithCaller(c.Request.Context(), types.Caller{UserID: user.ID, TenantID: user.TenantID, Role: membership.Role})
	if delegated {
		if !active && (snapshot.Enabled || inactiveBinding == nil || snapshot.WorkspaceID != inactiveBinding.WorkspaceID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Inactive authorization can only disable its existing connection"})
			return
		}
		delegateWriter, ok := h.imports.(interface {
			ApplyDelegatedControl(context.Context, string, string, int64, bool, []string, string, bool) error
		})
		if !ok {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Background authorization unavailable"})
			return
		}
		if err := delegateWriter.ApplyDelegatedControl(ctx, snapshot.Subject, snapshot.WorkspaceID, snapshot.Version, snapshot.Enabled, snapshot.Roots, auth[1], active); err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "Delegated control rejected"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"version": snapshot.Version})
		return
	}
	if err := writer.ApplyControl(ctx, snapshot.Subject, snapshot.WorkspaceID, snapshot.Version, snapshot.Enabled, snapshot.Roots); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Control rejected"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"version": snapshot.Version})
}

func (h *VNoteImportHandler) ImportNote(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !vosSSOEnabled() {
		c.JSON(http.StatusForbidden, gin.H{"error": "VOS authentication is disabled"})
		return
	}
	authorization := strings.Fields(c.GetHeader("Authorization"))
	if len(authorization) != 2 || (!strings.EqualFold(authorization[0], "Bearer") && !strings.EqualFold(authorization[0], "Integration")) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "A VOS user proof is required"})
		return
	}
	identity, err := verifyVNoteIdentity(c.Request.Context(), authorization[0], authorization[1])
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Cannot verify the current VOS user"})
		return
	}
	identities, supported := h.users.(platformIdentityService)
	if !supported {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Identity binding is unavailable"})
		return
	}
	user, err := identities.GetPlatformIdentityUser(c.Request.Context(), vosOIDCUserinfoURL(), identity.Subject)
	if err != nil || user == nil || !user.IsActive || user.TenantID == 0 {
		c.JSON(http.StatusConflict, gin.H{"code": "identity_binding_required", "error": "Verify your HybRAG account binding before synchronizing notes"})
		return
	}
	membership, err := h.members.GetMembership(c.Request.Context(), user.ID, user.TenantID)
	if err != nil || membership == nil || membership.Status != types.TenantMemberStatusActive || membership.Role != types.TenantRoleOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "An owned personal workspace is required"})
		return
	}
	var input struct {
		Ticket string `json:"ticket" binding:"required,max=256"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32768)
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "A note ticket is required"})
		return
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("HYBRAG_VNOTE_URL")), "/")
	if base == "" {
		base = "http://v-note-backend:8787"
	}
	parsed, parseErr := url.Parse(base)
	if parseErr != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "V-Note service address is invalid"})
		return
	}
	body, _ := json.Marshal(gin.H{"ticket": input.Ticket})
	request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, base+"/api/integrations/hybrag/redeem", bytes.NewReader(body))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "V-Note service is unavailable"})
		return
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", authorization[0]+" "+authorization[1])
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "V-Note service is unavailable"})
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		c.JSON(http.StatusConflict, gin.H{"error": "V-Note rejected this note ticket; retry the source operation"})
		return
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1000001))
	var snapshot types.VNoteSnapshot
	if err != nil || len(payload) > 1000000 || json.Unmarshal(payload, &snapshot) != nil || snapshot.Subject != identity.Subject {
		c.JSON(http.StatusForbidden, gin.H{"error": "V-Note returned an invalid owner or snapshot"})
		return
	}
	tenant, err := h.tenants.GetTenantByID(c.Request.Context(), user.TenantID)
	if err != nil || tenant == nil || tenant.ID != user.TenantID {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Personal workspace is unavailable"})
		return
	}
	ctx := types.WithCaller(c.Request.Context(), types.Caller{UserID: user.ID, TenantID: user.TenantID, Role: membership.Role})
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: user.ID})
	ctx = types.WithExecutionTenant(ctx, user.TenantID)
	ctx = context.WithValue(ctx, types.UserIDContextKey, user.ID)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	if strings.EqualFold(authorization[0], "Integration") {
		ctx = types.WithVNoteDelegatedCredential(ctx, authorization[1])
	}
	result, err := h.imports.Import(ctx, snapshot)
	if stderrors.Is(err, service.ErrVNoteEmbeddingUnavailable) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "vnote_embedding_unavailable", "accepted": false, "error": "Configure an active default embedding model, then retry"})
		return
	}
	if stderrors.Is(err, service.ErrVNoteImportBusy) {
		c.JSON(http.StatusConflict, gin.H{"code": "vnote_busy", "error": "A V-Note note is being indexed. Retry after it finishes"})
		return
	}
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "The note could not be admitted; check model and storage settings or retry"})
		return
	}
	exposed := *result
	if logical := types.VNoteLogicalKnowledgeID(result.KnowledgeBaseID, snapshot.NoteID); logical != "" {
		exposed.KnowledgeID = logical
	}
	c.JSON(http.StatusAccepted, exposed)
}

// Status verifies the same stable platform identity as import; it exposes no note body.
func (h *VNoteImportHandler) Status(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !vosSSOEnabled() {
		c.JSON(http.StatusForbidden, gin.H{"error": "VOS authentication is disabled"})
		return
	}
	authorization := strings.Fields(c.GetHeader("Authorization"))
	if len(authorization) != 2 || (!strings.EqualFold(authorization[0], "Bearer") && !strings.EqualFold(authorization[0], "Integration")) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "A VOS user proof is required"})
		return
	}
	identity, err := verifyVNoteIdentity(c.Request.Context(), authorization[0], authorization[1])
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Cannot verify the current VOS user"})
		return
	}
	identities, ok := h.users.(platformIdentityService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Identity binding is unavailable"})
		return
	}
	user, err := identities.GetPlatformIdentityUser(c.Request.Context(), vosOIDCUserinfoURL(), identity.Subject)
	if err != nil || user == nil || !user.IsActive || user.TenantID == 0 {
		c.JSON(http.StatusConflict, gin.H{"code": "identity_binding_required", "error": "Verify your HybRAG account binding"})
		return
	}
	membership, err := h.members.GetMembership(c.Request.Context(), user.ID, user.TenantID)
	if err != nil || membership == nil || membership.Status != types.TenantMemberStatusActive || membership.Role != types.TenantRoleOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "An owned personal workspace is required"})
		return
	}
	var input struct {
		RootID          string   `json:"rootId" binding:"max=128"`
		NoteID          string   `json:"noteId" binding:"max=128"`
		SyncWorkspaceID string   `json:"syncWorkspaceId" binding:"max=128"`
		SyncRoots       []string `json:"syncRoots" binding:"max=100,dive,max=128"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32768)
	if c.ShouldBindJSON(&input) != nil || (input.RootID == "") != (input.NoteID == "") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid scope"})
		return
	}
	ctx := types.WithCaller(c.Request.Context(), types.Caller{UserID: user.ID, TenantID: user.TenantID, Role: membership.Role})
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: user.ID})
	ctx = types.WithExecutionTenant(ctx, user.TenantID)
	if input.SyncWorkspaceID != "" {
		if input.RootID != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid scope"})
			return
		}
		reader, ok := h.imports.(interface {
			SynchronizationStatus(context.Context, string, string, []string) ([]service.VNoteSyncStatus, error)
		})
		if !ok {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Synchronization status is unavailable"})
			return
		}
		runs, err := reader.SynchronizationStatus(ctx, identity.Subject, input.SyncWorkspaceID, input.SyncRoots)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Synchronization status is unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"connected": true, "syncRuns": runs})
		return
	}
	if input.RootID == "" {
		capability := 0
		if _, err := utils.VNoteCredentialKeys(); err == nil {
			capability = 1
		}
		c.JSON(http.StatusOK, gin.H{"connected": true, "backgroundAuthorization": capability, "backgroundSync": 1})
		return
	}
	reader, ok := h.imports.(interface {
		Status(context.Context, string, string) (*service.VNoteImportResult, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Status is unavailable"})
		return
	}
	result, err := reader.Status(ctx, input.RootID, input.NoteID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Status is unavailable"})
		return
	}
	exposed := *result
	if exposed.KnowledgeID != "" {
		if logical := types.VNoteLogicalKnowledgeID(exposed.KnowledgeBaseID, input.NoteID); logical != "" {
			exposed.KnowledgeID = logical
		}
	}
	c.JSON(http.StatusOK, gin.H{"connected": true, "result": exposed})
}
