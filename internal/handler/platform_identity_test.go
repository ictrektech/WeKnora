package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type identityBindingUserService struct {
	interfaces.UserService
	boundUser string
	subject   string
	conflict  bool
}

func TestPlatformIdentityCreationRequiresStrictMatchingHumanProof(t *testing.T) {
	for _, test := range []struct {
		name, response, subject string
		allowed                 bool
	}{
		{"verified", `{"sub":"stable","preferred_username":"alice","enable":true}`, "stable", true},
		{"name-only", `{"preferred_username":"alice","enable":true}`, "alice", false},
		{"other-subject", `{"sub":"other","enable":true}`, "stable", false},
		{"disabled", `{"sub":"stable","enable":false}`, "stable", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.response))
			}))
			defer server.Close()
			t.Setenv("HYBRAG_VOS_OIDC_USERINFO_URL", server.URL)
			request := &types.TrustedIdentityLoginRequest{PlatformID: server.URL, Subject: test.subject}
			enablePlatformIdentityCreation(context.Background(), "human-proof", request)
			require.Equal(t, test.allowed, request.BindOnCreate)
			require.Equal(t, test.subject, request.Subject)
		})
	}
}

func (service *identityBindingUserService) ValidateToken(_ context.Context, token string) (*types.User, uint64, error) {
	if token != "local-session" {
		return nil, 0, errors.New("invalid session")
	}
	return &types.User{ID: "current-user", IsActive: true, TenantID: 7}, 7, nil
}

func (service *identityBindingUserService) GetPlatformIdentityUser(context.Context, string, string) (*types.User, error) {
	panic("unexpected identity lookup")
}

func (service *identityBindingUserService) BindPlatformIdentity(_ context.Context, _ string, subject, userID string) error {
	if service.conflict {
		return types.ErrPlatformIdentityConflict
	}
	service.boundUser, service.subject = userID, subject
	return nil
}

func TestVOSIdentityBindingRequiresBothUserProofs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	platform := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer vos-proof" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = writer.Write([]byte(`{"sub":"stable-subject","enable":1}`))
	}))
	defer platform.Close()
	t.Setenv("HYBRAG_VOS_OIDC_USERINFO_URL", platform.URL)
	t.Setenv("HYBRAG_VOS_SSO_ENABLED", "true")
	for _, example := range []struct {
		name, bearer, body string
		conflict           bool
		status             int
	}{
		{"valid", "local-session", `{"access_token":"vos-proof","user_id":"someone-else"}`, false, 200},
		{"platform_bearer_only", "vos-proof", `{"access_token":"vos-proof"}`, false, 401},
		{"missing_platform_proof", "local-session", `{}`, false, 400},
		{"invalid_platform_proof", "local-session", `{"access_token":"wrong"}`, false, 401},
		{"conflict", "local-session", `{"access_token":"vos-proof"}`, true, 409},
	} {
		t.Run(example.name, func(t *testing.T) {
			service := &identityBindingUserService{conflict: example.conflict}
			handler := &AuthHandler{userService: service}
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/vos-identity", strings.NewReader(example.body))
			ctx.Request.Header.Set("Authorization", "Bearer "+example.bearer)
			ctx.Request.Header.Set("Content-Type", "application/json")
			handler.BindVOSIdentity(ctx)
			if example.status == 200 {
				require.Equal(t, "current-user", service.boundUser)
				require.Equal(t, "stable-subject", service.subject)
			} else {
				require.Empty(t, service.boundUser)
			}
			if example.status == 409 || example.status == 200 {
				require.Equal(t, example.status, response.Code)
			} else {
				require.NotEmpty(t, ctx.Errors)
				var appError *apperrors.AppError
				require.ErrorAs(t, ctx.Errors.Last().Err, &appError)
				require.Equal(t, example.status, appError.HTTPCode)
			}
		})
	}
}
