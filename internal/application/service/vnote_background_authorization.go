package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/platformidentity"
	"github.com/Tencent/WeKnora/internal/types"
)

// Checks are performed at task admission, each external index batch, and final
// publication, never inferred from a browser session or cached positive result.
func (s *knowledgeService) checkVNoteBackgroundAuthorization(ctx context.Context, note *types.Knowledge) error {
	meta := note.GetMetadata()
	if meta["vnote_authorization"] != "delegated" {
		return nil
	}
	reader, ok := s.repo.(interface {
		VNoteDelegationForKnowledge(context.Context, *types.Knowledge) (*repository.VNoteDelegation, error)
	})
	if !ok {
		return repository.ErrVNoteDelegation
	}
	binding, err := reader.VNoteDelegationForKnowledge(ctx, note)
	if err != nil {
		return err
	}
	secret, err := repository.ReadVNoteDelegationSecret(*binding)
	if err != nil {
		return err
	}
	endpoint := strings.TrimSpace(os.Getenv("HYBRAG_VOS_OIDC_USERINFO_URL"))
	if endpoint == "" {
		endpoint = strings.TrimSpace(os.Getenv("WEKNORA_VOS_OIDC_USERINFO_URL"))
	}
	if endpoint == "" {
		endpoint = "http://172.17.0.1:8105/v1000/oauth2/userinfo"
	}
	identity, err := platformidentity.VerifyVNoteGrant(ctx, endpoint, secret)
	if err != nil || identity.Subject != binding.Subject {
		return repository.ErrVNoteDelegation
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("HYBRAG_VNOTE_URL")), "/")
	if base == "" {
		base = "http://v-note-backend:8787"
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return repository.ErrVNoteDelegation
	}
	version, err := strconv.ParseInt(meta["vnote_config_version"], 10, 64)
	if err != nil {
		return repository.ErrVNoteDelegation
	}
	// Admission can be delayed after enqueue. Check the window again before
	// claiming a scheduled task; an already processing document may finish.
	if meta["vnote_scheduled"] == "true" && note.ParseStatus == types.ParseStatusPending {
		var plan vnoteSyncPlan
		if err := vnoteSourceRequest(ctx, secret, "plan", map[string]any{"rootId": meta["vnote_root_id"], "configVersion": version}, &plan); err != nil {
			return repository.ErrVNoteDelegation
		}
		if plan.Protocol != 1 || plan.Subject != binding.Subject || plan.WorkspaceID != binding.WorkspaceID || plan.RootID != meta["vnote_root_id"] || plan.ConfigVersion != version || plan.Schedule == nil || plan.Paused || plan.RootMissing {
			return repository.ErrVNoteDelegation
		}
		allowed, _, err := plan.Schedule.window(time.Now().UTC())
		if err != nil || !allowed {
			return repository.ErrVNoteDelegation
		}
	}
	body, _ := json.Marshal(map[string]any{"rootId": meta["vnote_root_id"], "noteId": meta["vnote_note_id"], "configVersion": version, "sourceHash": meta["vnote_source_hash"], "sourceTitle": meta["vnote_source_title"]})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/integrations/hybrag/authorize", bytes.NewReader(body))
	if err != nil {
		return repository.ErrVNoteDelegation
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Integration "+secret)
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return repository.ErrVNoteDelegation
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	var result struct {
		Protocol      int    `json:"protocol"`
		Authorized    bool   `json:"authorized"`
		Subject       string `json:"subject"`
		WorkspaceID   string `json:"workspaceId"`
		ConfigVersion int64  `json:"configVersion"`
		RootID        string `json:"rootId"`
		NoteID        string `json:"noteId"`
	}
	if err != nil || resp.StatusCode != http.StatusOK || len(raw) > 4096 || json.Unmarshal(raw, &result) != nil || result.Protocol != 1 || !result.Authorized || result.Subject != binding.Subject || result.WorkspaceID != binding.WorkspaceID || result.ConfigVersion != version || result.RootID != meta["vnote_root_id"] || result.NoteID != meta["vnote_note_id"] {
		return repository.ErrVNoteDelegation
	}
	return nil
}

// Only called at a boundary where this worker has no outstanding external
// request. An unknown model timeout must retain the global processing slot.
func (s *knowledgeService) stopUnauthorizedVNoteBoundary(ctx context.Context, note *types.Knowledge) error {
	if err := s.checkVNoteBackgroundAuthorization(ctx, note); err != nil {
		if retirer, ok := s.repo.(interface {
			CancelVNoteAuthorizationAttempt(context.Context, *types.Knowledge) error
		}); ok {
			cleanupCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer stop()
			if cancelErr := retirer.CancelVNoteAuthorizationAttempt(cleanupCtx, note); cancelErr != nil {
				return errors.Join(err, cancelErr)
			}
		}
		return err
	}
	return nil
}
