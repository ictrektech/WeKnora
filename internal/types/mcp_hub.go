package types

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrMCPHubSignInRequired means a request has no fresh VOS user identity.
	ErrMCPHubSignInRequired = errors.New("VOS sign-in is required to use MCPHub")
	// ErrMCPHubUnavailable means the platform service could not be reached.
	ErrMCPHubUnavailable = errors.New("MCPHub is temporarily unavailable")
)

// MCPHubAccess is a request-scoped function that verifies the VOS proof and
// returns the access token only after confirming it belongs to the local user.
type MCPHubAccess func(context.Context) (string, error)

func WithMCPHubAccess(ctx context.Context, access MCPHubAccess) context.Context {
	if access == nil {
		return ctx
	}
	return context.WithValue(ctx, MCPHubAccessContextKey, access)
}

func MCPHubAccessFromContext(ctx context.Context) MCPHubAccess {
	access, _ := ctx.Value(MCPHubAccessContextKey).(MCPHubAccess)
	return access
}

func MCPHubAccessToken(ctx context.Context) (string, error) {
	access := MCPHubAccessFromContext(ctx)
	if access == nil {
		return "", ErrMCPHubSignInRequired
	}
	return access(ctx)
}

// MCPHubServer is the subset of MCPHub's service directory HybRAG consumes.
type MCPHubServer struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Enabled     bool       `json:"enabled"`
	Status      string     `json:"status"`
	Tools       []*MCPTool `json:"tools"`
}

// MCPHubDiscoveryStatus reports a non-secret summary suitable for the UI.
type MCPHubDiscoveryStatus struct {
	Available     bool       `json:"available"`
	Authenticated bool       `json:"authenticated"`
	LastSyncedAt  *time.Time `json:"last_synced_at,omitempty"`
	ServiceCount  int        `json:"service_count"`
	Message       string     `json:"message,omitempty"`
}
