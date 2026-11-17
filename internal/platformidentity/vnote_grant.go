package platformidentity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var vnoteGrantSecret = regexp.MustCompile(`^[0-9a-f]{64}$`)

// VerifyVNoteGrant accepts only the narrow, opaque platform credential. It is
// deliberately separate from ordinary user and API authentication.
func VerifyVNoteGrant(ctx context.Context, userinfoURL, secret string) (*Identity, error) {
	endpoint, err := url.Parse(userinfoURL)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" ||
		(endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Path != "/v1000/oauth2/userinfo" || !vnoteGrantSecret.MatchString(secret) {
		return nil, errors.New("invalid VOS integration grant configuration")
	}
	endpoint.Path = "/v1000/oauth2/integration-grant/status"
	body, _ := json.Marshal(map[string]string{"secret": secret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("cannot check VOS integration grant")
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("VOS integration authorization is unavailable")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	var result struct {
		Protocol int    `json:"protocol"`
		Active   *bool  `json:"active"`
		Subject  string `json:"subject"`
	}
	if err != nil || resp.StatusCode != http.StatusOK || len(raw) > 4096 || json.Unmarshal(raw, &result) != nil ||
		result.Protocol != 1 || result.Active == nil || !*result.Active || strings.TrimSpace(result.Subject) == "" || len(result.Subject) > 255 {
		return nil, errors.New("VOS integration authorization is inactive or unknown")
	}
	return &Identity{Subject: result.Subject}, nil
}
