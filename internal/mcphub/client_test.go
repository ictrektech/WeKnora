package mcphub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientListsPerUserServers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/app/com.ictrek.mcphub/api/servers", r.URL.Path)
		require.Equal(t, "Bearer vos-user-token", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": []any{map[string]any{
				"name": "search", "enabled": true, "status": "connected",
				"config": map[string]any{"description": "Search tools"},
				"tools": []any{
					map[string]any{
						"name": "search-query", "description": "Run a query",
						"inputSchema": map[string]any{"type": "object"},
					},
					map[string]any{
						"name": "read", "description": "Read data",
						"inputSchema": map[string]any{"type": "object"},
					},
				},
			}},
		})
	}))
	t.Cleanup(server.Close)

	client, err := NewClientWithBaseURL(server.URL + "/app/com.ictrek.mcphub")
	require.NoError(t, err)
	servers, err := client.ListServers(context.Background(), "vos-user-token")
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, "search", servers[0].Name)
	require.Equal(t, "Search tools", servers[0].Description)
	require.Len(t, servers[0].Tools, 2)
	require.Equal(t, "query", servers[0].Tools[0].Name)
	require.Equal(t, "read", servers[0].Tools[1].Name)
}

func TestClientRejectsRedirectAndDoesNotLeakToken(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("redirect target must not be contacted")
	}))
	t.Cleanup(target.Close)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)

	client, err := NewClientWithBaseURL(server.URL)
	require.NoError(t, err)
	_, err = client.ListServers(context.Background(), "secret-token")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret-token")
}

func TestManagedURLMatchesOnlyConfiguredHubBase(t *testing.T) {
	client, err := NewClientWithBaseURL("http://mcphub:3000/app/com.ictrek.mcphub")
	require.NoError(t, err)
	require.True(t, client.IsManagedURL("http://mcphub:3000/app/com.ictrek.mcphub/mcp/search"))
	require.False(t, client.IsManagedURL("http://mcphub:3000/other/mcp/search"))
	require.False(t, client.IsManagedURL("http://mcphub.example.com/app/com.ictrek.mcphub/mcp/search"))
	require.False(t, client.IsManagedURL("http://mcphub:3000/app/com.ictrek.mcphub/mcp/../other"))
}
