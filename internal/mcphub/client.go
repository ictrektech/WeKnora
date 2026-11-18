package mcphub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const (
	defaultBaseURL = "http://mcphub:3000/app/com.ictrek.mcphub"
	maxBodyBytes   = 8 << 20
	maxServices    = 5000
)

// Client reads the per-user service directory exposed by the MCPHub VOS app.
type Client struct {
	baseURL          *url.URL
	configurationErr error
	httpClient       *http.Client
}

// NewClient builds the fixed, deployment-scoped MCPHub client. The base URL is
// never accepted from a user request.
func NewClient() *Client {
	raw := strings.TrimSpace(os.Getenv("MCPHUB_BASE_URL"))
	if raw == "" {
		raw = defaultBaseURL
	}
	baseURL, err := normalizeBaseURL(raw)
	if err != nil {
		return &Client{configurationErr: fmt.Errorf("invalid MCPHUB_BASE_URL: %w", err)}
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 8 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// NewClientWithBaseURL is used by tests and local integrations.
func NewClientWithBaseURL(raw string) (*Client, error) {
	baseURL, err := normalizeBaseURL(raw)
	if err != nil {
		return nil, err
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 8 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func normalizeBaseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || parsed.Host == "" || parsed.User != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("expected an http(s) URL without credentials, query or fragment")
	}
	return parsed, nil
}

// ListServers returns only the directory fields MCPHub already filters for the
// supplied VOS user.
func (c *Client) ListServers(ctx context.Context, accessToken string) ([]*types.MCPHubServer, error) {
	if c == nil || c.baseURL == nil {
		if c != nil && c.configurationErr != nil {
			return nil, fmt.Errorf("%w: %v", types.ErrMCPHubUnavailable, c.configurationErr)
		}
		return nil, types.ErrMCPHubUnavailable
	}
	if strings.TrimSpace(accessToken) == "" || len(accessToken) > 16384 ||
		strings.ContainsAny(accessToken, "\r\n") {
		return nil, types.ErrMCPHubSignInRequired
	}

	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/api/servers"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: create request", types.ErrMCPHubUnavailable)
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", types.ErrMCPHubUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, types.ErrMCPHubSignInRequired
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", types.ErrMCPHubUnavailable, response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read response", types.ErrMCPHubUnavailable)
	}
	if len(body) > maxBodyBytes {
		return nil, fmt.Errorf("%w: response is too large", types.ErrMCPHubUnavailable)
	}

	var payload struct {
		Success bool `json:"success"`
		Data    []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Enabled     *bool  `json:"enabled"`
			Status      string `json:"status"`
			Tools       []struct {
				Name        string                    `json:"name"`
				Description string                    `json:"description"`
				InputSchema json.RawMessage           `json:"inputSchema"`
				Annotations *types.MCPToolAnnotations `json:"annotations"`
			} `json:"tools"`
			Config struct {
				Description string `json:"description"`
			} `json:"config"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: invalid response", types.ErrMCPHubUnavailable)
	}
	if !payload.Success {
		return nil, fmt.Errorf("%w: request rejected", types.ErrMCPHubUnavailable)
	}
	if len(payload.Data) > maxServices {
		return nil, fmt.Errorf("%w: too many services", types.ErrMCPHubUnavailable)
	}

	out := make([]*types.MCPHubServer, 0, len(payload.Data))
	for _, remote := range payload.Data {
		name := strings.TrimSpace(remote.Name)
		if name == "" || len(name) > 255 || strings.ContainsAny(name, "\r\n") {
			continue
		}
		enabled := true
		if remote.Enabled != nil {
			enabled = *remote.Enabled
		}
		description := strings.TrimSpace(remote.Description)
		if description == "" {
			description = strings.TrimSpace(remote.Config.Description)
		}
		tools := make([]*types.MCPTool, 0, len(remote.Tools))
		seenTools := make(map[string]struct{}, len(remote.Tools))
		for _, remoteTool := range remote.Tools {
			toolName := strings.TrimSpace(remoteTool.Name)
			// MCPHub names tools with the server prefix to keep its combined
			// directory unambiguous. The per-server MCP endpoint exposes the
			// original tool names, which are also the names persisted by tool
			// policy and directory discovery. Normalize at ingestion so both
			// representations cannot diverge.
			toolName = strings.TrimPrefix(toolName, name+"-")
			if toolName == "" || len(toolName) > 512 {
				continue
			}
			if _, exists := seenTools[toolName]; exists {
				continue
			}
			seenTools[toolName] = struct{}{}
			tools = append(tools, &types.MCPTool{
				Name:        toolName,
				Description: remoteTool.Description,
				InputSchema: remoteTool.InputSchema,
				Annotations: remoteTool.Annotations,
			})
		}
		out = append(out, &types.MCPHubServer{
			Name: name, Description: description, Enabled: enabled,
			Status: remote.Status, Tools: tools,
		})
	}
	return out, nil
}

// ServiceURL returns the Streamable HTTP endpoint for one Hub service.
func (c *Client) ServiceURL(name string) (string, error) {
	if c == nil || c.baseURL == nil {
		return "", types.ErrMCPHubUnavailable
	}
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "\r\n") {
		return "", errors.New("invalid MCPHub service name")
	}
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/mcp/" + url.PathEscape(name)
	return endpoint.String(), nil
}

// IsManagedURL verifies that a persisted Hub URL still belongs to the fixed
// deployment endpoint. This is the explicit exception to private-network SSRF
// blocking for managed services.
func (c *Client) IsManagedURL(raw string) bool {
	if c == nil || c.baseURL == nil {
		return false
	}
	candidate, err := url.Parse(raw)
	if err != nil || candidate.User != nil || candidate.RawQuery != "" || candidate.Fragment != "" {
		return false
	}
	if path.Clean(candidate.Path) != candidate.Path {
		return false
	}
	return candidate.Scheme == c.baseURL.Scheme &&
		candidate.Host == c.baseURL.Host &&
		strings.HasPrefix(candidate.Path, strings.TrimRight(c.baseURL.Path, "/")+"/mcp/")
}

// HTTPClient returns a bounded client suitable for managed internal HTTP
// traffic. Redirects are rejected so the delegated bearer never follows an
// unexpected Location header.
func (c *Client) HTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

var _ interfaces.MCPHubClient = (*Client)(nil)
