package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/mcphub"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

type mcpHubService struct {
	client    interfaces.MCPHubClient
	hubClient *mcphub.Client
	repo      interfaces.MCPServiceRepository
	approvals interfaces.MCPToolApprovalService
	refreshMu sync.Mutex
	now       func() time.Time
}

// NewMCPHubService reconciles the current VOS user's MCPHub directory into a
// workspace without replacing administrator choices.
func NewMCPHubService(
	client *mcphub.Client,
	repo interfaces.MCPServiceRepository,
	approvals interfaces.MCPToolApprovalService,
) interfaces.MCPHubService {
	return &mcpHubService{
		client: client, hubClient: client, repo: repo, approvals: approvals, now: time.Now,
	}
}

func (s *mcpHubService) Refresh(
	ctx context.Context,
	tenantID uint64,
) (*types.MCPHubDiscoveryStatus, error) {
	status := &types.MCPHubDiscoveryStatus{}
	if tenantID == 0 {
		return status, errors.New("workspace context is required")
	}
	if s == nil || s.client == nil || s.repo == nil || s.hubClient == nil {
		status.Message = types.ErrMCPHubUnavailable.Error()
		return status, types.ErrMCPHubUnavailable
	}

	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	token, err := types.MCPHubAccessToken(ctx)
	if err != nil {
		status.Message = types.ErrMCPHubSignInRequired.Error()
		return status, types.ErrMCPHubSignInRequired
	}
	status.Authenticated = true

	servers, err := s.client.ListServers(ctx, token)
	if err != nil {
		status.Message = types.ErrMCPHubUnavailable.Error()
		return status, err
	}
	status.Available = true
	status.ServiceCount = len(servers)
	now := s.now().UTC()
	status.LastSyncedAt = &now

	services, err := s.repo.List(ctx, tenantID)
	if err != nil {
		return status, fmt.Errorf("list MCP services: %w", err)
	}
	byExternalID := make(map[string]*types.MCPService)
	names := make(map[string]struct{})
	for _, service := range services {
		if service == nil {
			continue
		}
		names[service.Name] = struct{}{}
		if service.Source == types.MCPServiceSourceMCPHub && service.ExternalID != "" {
			byExternalID[service.ExternalID] = service
		}
	}

	seen := make(map[string]struct{}, len(servers))
	for _, remote := range servers {
		if remote == nil || strings.TrimSpace(remote.Name) == "" {
			continue
		}
		seen[remote.Name] = struct{}{}
		serviceURL, urlErr := s.hubClient.ServiceURL(remote.Name)
		if urlErr != nil {
			return status, urlErr
		}
		service := byExternalID[remote.Name]
		isNew := service == nil
		if service == nil {
			name := s.uniqueName(remote.Name, names)
			names[name] = struct{}{}
			service = &types.MCPService{
				ID:        newMCPServiceID(),
				TenantID:  tenantID,
				Name:      name,
				Enabled:   false,
				CreatedAt: now,
			}
			service.Description = strings.TrimSpace(remote.Description)
			service.TransportType = types.MCPTransportHTTPStreamable
			service.URL = &serviceURL
			service.AuthConfig = &types.MCPAuthConfig{AuthType: types.MCPAuthVOS}
			service.AdvancedConfig = types.GetDefaultAdvancedConfig()
			service.Source = types.MCPServiceSourceMCPHub
			service.ExternalID = remote.Name
			service.HubAvailable = remote.Enabled
			service.HubLastSeenAt = &now
			service.UpdatedAt = now
			if err := s.repo.Create(ctx, service); err != nil {
				return status, fmt.Errorf("import MCPHub service %q: %w", remote.Name, err)
			}
			byExternalID[remote.Name] = service
		} else if mcpHubConnectionChanged(service, remote, serviceURL) {
			service.Description = strings.TrimSpace(remote.Description)
			service.TransportType = types.MCPTransportHTTPStreamable
			service.URL = &serviceURL
			service.AuthConfig = &types.MCPAuthConfig{AuthType: types.MCPAuthVOS}
			service.Source = types.MCPServiceSourceMCPHub
			service.ExternalID = remote.Name
			service.HubAvailable = remote.Enabled
			service.HubLastSeenAt = &now
			service.UpdatedAt = now
			if err := s.repo.Update(ctx, service); err != nil {
				return status, fmt.Errorf("update MCPHub service %q: %w", remote.Name, err)
			}
		}
		if err := s.EnsureToolPolicies(ctx, tenantID, service.ID, remote.Tools, isNew); err != nil {
			return status, err
		}
	}

	for externalID, service := range byExternalID {
		if _, exists := seen[externalID]; exists || !service.HubAvailable {
			continue
		}
		service.HubAvailable = false
		service.UpdatedAt = now
		if err := s.repo.Update(ctx, service); err != nil {
			return status, fmt.Errorf("mark MCPHub service %q unavailable: %w", externalID, err)
		}
	}
	return status, nil
}

func (s *mcpHubService) FilterAccessible(
	ctx context.Context,
	tenantID uint64,
	services []*types.MCPService,
) ([]*types.MCPService, *types.MCPHubDiscoveryStatus) {
	hasHub := false
	for _, service := range services {
		if service != nil && service.Source == types.MCPServiceSourceMCPHub {
			hasHub = true
			break
		}
	}
	if !hasHub {
		return services, nil
	}

	status := &types.MCPHubDiscoveryStatus{}
	if s == nil || s.client == nil {
		status.Message = types.ErrMCPHubUnavailable.Error()
		return withoutMCPHubServices(services), status
	}
	token, err := types.MCPHubAccessToken(ctx)
	if err != nil {
		status.Message = types.ErrMCPHubSignInRequired.Error()
		return withoutMCPHubServices(services), status
	}
	status.Authenticated = true
	servers, err := s.client.ListServers(ctx, token)
	if err != nil {
		status.Message = types.ErrMCPHubUnavailable.Error()
		return withoutMCPHubServices(services), status
	}
	status.Available = true
	status.ServiceCount = len(servers)
	now := s.now().UTC()
	status.LastSyncedAt = &now
	allowed := make(map[string]*types.MCPHubServer, len(servers))
	for _, server := range servers {
		if server != nil && server.Enabled {
			allowed[server.Name] = server
		}
	}
	out := make([]*types.MCPService, 0, len(services))
	for _, service := range services {
		if service == nil {
			continue
		}
		if service.Source != types.MCPServiceSourceMCPHub {
			out = append(out, service)
			continue
		}
		if server, ok := allowed[service.ExternalID]; ok {
			out = append(out, service)
			if err := s.EnsureToolPolicies(ctx, tenantID, service.ID, server.Tools, false); err != nil {
				logger.Warnf(ctx, "Failed to initialize MCPHub tools for %s: %v", service.ID, err)
			}
		}
	}
	return out, status
}

func (s *mcpHubService) EnsureToolPolicies(
	ctx context.Context,
	tenantID uint64,
	serviceID string,
	tools []*types.MCPTool,
	defaultEnabled bool,
) error {
	if s == nil || s.approvals == nil || serviceID == "" || len(tools) == 0 {
		return nil
	}
	rows, err := s.approvals.ListByService(ctx, tenantID, serviceID)
	if err != nil {
		return fmt.Errorf("list MCPHub tool policies: %w", err)
	}
	known := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row != nil {
			known[row.ToolName] = struct{}{}
		}
	}
	names := make(map[string]struct{}, len(tools))
	ordered := make([]string, 0, len(tools))
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		toolName := strings.TrimSpace(tool.Name)
		if toolName == "" {
			continue
		}
		if _, duplicate := names[toolName]; duplicate {
			continue
		}
		names[toolName] = struct{}{}
		ordered = append(ordered, toolName)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		if _, exists := known[name]; exists {
			continue
		}
		enabled := defaultEnabled
		if err := s.approvals.SetPolicy(ctx, tenantID, serviceID, name, types.MCPToolPolicyPatch{
			Enabled: &enabled,
		}); err != nil {
			return fmt.Errorf("initialize MCPHub tool policy %q: %w", name, err)
		}
	}
	return nil
}

func (s *mcpHubService) uniqueName(wanted string, existing map[string]struct{}) string {
	if _, exists := existing[wanted]; !exists {
		return wanted
	}
	base := wanted + " (MCPHub)"
	if _, exists := existing[base]; !exists {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s (%d)", base, i)
		if _, exists := existing[candidate]; !exists {
			return candidate
		}
	}
}

func mcpHubConnectionChanged(
	service *types.MCPService,
	remote *types.MCPHubServer,
	serviceURL string,
) bool {
	currentURL := ""
	if service.URL != nil {
		currentURL = strings.TrimSpace(*service.URL)
	}
	return currentURL != serviceURL ||
		strings.TrimSpace(service.Description) != strings.TrimSpace(remote.Description) ||
		service.TransportType != types.MCPTransportHTTPStreamable ||
		service.AuthConfig == nil ||
		service.AuthConfig.AuthType != types.MCPAuthVOS ||
		len(service.Headers) != 0 ||
		service.Source != types.MCPServiceSourceMCPHub ||
		service.ExternalID != remote.Name ||
		service.HubAvailable != remote.Enabled ||
		service.HubLastSeenAt == nil
}

func withoutMCPHubServices(services []*types.MCPService) []*types.MCPService {
	out := make([]*types.MCPService, 0, len(services))
	for _, service := range services {
		if service != nil && service.Source != types.MCPServiceSourceMCPHub {
			out = append(out, service)
		}
	}
	return out
}

func newMCPServiceID() string {
	return uuid.NewString()
}

var _ interfaces.MCPHubService = (*mcpHubService)(nil)
