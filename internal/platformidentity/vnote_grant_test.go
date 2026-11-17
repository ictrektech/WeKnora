package platformidentity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVNoteGrantStatusIsSubjectBoundAndNotAUserToken(t *testing.T) {
	body := `{"protocol":1,"active":true,"subject":"owner"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1000/oauth2/integration-grant/status", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		require.Empty(t, r.Header.Get("Authorization"))
		var input map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
		require.Equal(t, strings.Repeat("a", 64), input["secret"])
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	identity, err := VerifyVNoteGrant(context.Background(), server.URL+"/v1000/oauth2/userinfo", strings.Repeat("a", 64))
	require.NoError(t, err)
	require.Equal(t, "owner", identity.Subject)
	for _, invalid := range []string{`{"protocol":1,"active":false}`, `{"protocol":1,"active":true}`, `{"protocol":2,"active":true,"subject":"owner"}`, `{"protocol":1,"subject":"owner"}`, strings.Repeat("x", 4097)} {
		body = invalid
		_, err = VerifyVNoteGrant(context.Background(), server.URL+"/v1000/oauth2/userinfo", strings.Repeat("a", 64))
		require.Error(t, err)
	}
	_, err = VerifyVNoteGrant(context.Background(), server.URL+"/v1000/oauth2/userinfo", "user.jwt.token")
	require.Error(t, err)
}

func TestVNoteGrantRejectsRedirectAndUnsafeEndpoint(t *testing.T) {
	contacted := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { contacted = true }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	for _, endpoint := range []string{origin.URL + "/v1000/oauth2/userinfo", origin.URL + "/userinfo", origin.URL + "/v1000/oauth2/userinfo?q=secret"} {
		_, err := VerifyVNoteGrant(context.Background(), endpoint, strings.Repeat("a", 64))
		require.Error(t, err)
	}
	require.False(t, contacted)
}
