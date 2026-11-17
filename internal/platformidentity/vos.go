package platformidentity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Identity struct {
	Subject  string
	Username string
}

func VerifyVOS(ctx context.Context, endpoint, token string) (*Identity, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("invalid VOS identity endpoint")
	}
	if token == "" || len(token) > 16384 || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("invalid VOS token")
	}
	if segments := strings.Split(token, "."); len(segments) == 3 {
		payload, decodeErr := base64.RawURLEncoding.DecodeString(segments[1])
		var claims struct {
			Application string `json:"application"`
		}
		if decodeErr == nil && json.Unmarshal(payload, &claims) == nil && (claims.Application == "app" || claims.Application == "platform") {
			return nil, errors.New("application service credentials cannot authorize a user")
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("cannot create VOS identity request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("VOS identity service is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("VOS identity verification was rejected")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(body) > 65536 {
		return nil, errors.New("invalid VOS identity response")
	}
	var claims struct {
		Subject  string          `json:"sub"`
		Username string          `json:"preferred_username"`
		Enabled  json.RawMessage `json:"enable"`
	}
	if json.Unmarshal(body, &claims) != nil || strings.TrimSpace(claims.Subject) == "" || len(claims.Subject) > 255 {
		return nil, errors.New("VOS stable user subject is missing")
	}
	if string(claims.Enabled) != "true" && string(claims.Enabled) != "1" {
		return nil, errors.New("VOS user is disabled or status is unknown")
	}
	return &Identity{Subject: strings.TrimSpace(claims.Subject), Username: strings.TrimSpace(claims.Username)}, nil
}
