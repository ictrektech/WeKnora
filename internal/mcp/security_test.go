package mcp

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestValidateServiceOutboundURLsRejectsOAuthMetadataSSRF(t *testing.T) {
	serviceURL := "https://example.com/mcp"
	err := ValidateServiceOutboundURLs(&types.MCPService{
		URL: &serviceURL,
		AuthConfig: &types.MCPAuthConfig{
			AuthServerMetadataURL: "http://169.254.169.254/latest/meta-data",
		},
	})
	if err == nil {
		t.Fatal("expected OAuth metadata URL to be rejected")
	}
}

func TestValidateServiceOutboundURLsAllowsOnlyManagedMCPHubURL(t *testing.T) {
	t.Setenv("MCPHUB_BASE_URL", "http://mcphub:3000/app/com.ictrek.mcphub")
	t.Run("managed", func(t *testing.T) {
		managed := "http://mcphub:3000/app/com.ictrek.mcphub/mcp/search"
		err := ValidateServiceOutboundURLs(&types.MCPService{
			Source: types.MCPServiceSourceMCPHub,
			URL:    &managed,
		})
		if err != nil {
			t.Fatalf("expected managed MCPHub URL to be allowed: %v", err)
		}
	})
	t.Run("arbitrary private target", func(t *testing.T) {
		target := "http://169.254.169.254/latest/meta-data"
		err := ValidateServiceOutboundURLs(&types.MCPService{
			Source: types.MCPServiceSourceMCPHub,
			URL:    &target,
		})
		if err == nil {
			t.Fatal("expected unmatched managed URL to be rejected")
		}
	})
}

func TestNewMCPClientRejectsStoredUnsafeURL(t *testing.T) {
	serviceURL := "http://127.0.0.1:8080/mcp"
	_, err := NewMCPClient(&ClientConfig{Service: &types.MCPService{
		URL:           &serviceURL,
		TransportType: types.MCPTransportHTTPStreamable,
	}})
	if err == nil {
		t.Fatal("expected stored unsafe MCP URL to be rejected at client construction")
	}
}
