package handler

import (
	"context"
	stderrors "errors"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/platformidentity"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

type platformIdentityService interface {
	GetPlatformIdentityUser(context.Context, string, string) (*types.User, error)
	BindPlatformIdentity(context.Context, string, string, string) error
}

// Legacy SSO may return display names as subjects. Only the strict human
// userinfo proof can authorize creating a durable platform identity binding.
func enablePlatformIdentityCreation(ctx context.Context, token string, request *types.TrustedIdentityLoginRequest) {
	identity, err := platformidentity.VerifyVOS(ctx, vosOIDCUserinfoURL(), token)
	if err == nil && request.Subject == identity.Subject {
		request.BindOnCreate = true
	}
}

func (h *AuthHandler) BindVOSIdentity(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !vosSSOEnabled() {
		c.Error(errors.NewForbiddenError("VOS authentication is disabled"))
		return
	}
	authorization := strings.Fields(c.GetHeader("Authorization"))
	if len(authorization) != 2 || !strings.EqualFold(authorization[0], "Bearer") {
		c.Error(errors.NewUnauthorizedError("A HybRAG user session is required"))
		return
	}
	user, _, err := h.userService.ValidateToken(c.Request.Context(), authorization[1])
	if err != nil || user == nil || !user.IsActive {
		c.Error(errors.NewUnauthorizedError("A valid HybRAG user session is required"))
		return
	}
	var input struct {
		AccessToken string `json:"access_token" binding:"required,max=16384"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32768)
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errors.NewBadRequestError("A VOS user access token is required"))
		return
	}
	identity, err := platformidentity.VerifyVOS(c.Request.Context(), vosOIDCUserinfoURL(), input.AccessToken)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("Cannot verify the current VOS user"))
		return
	}
	identities, supported := h.userService.(platformIdentityService)
	if !supported {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "Identity binding is unavailable"})
		return
	}
	if err := identities.BindPlatformIdentity(c.Request.Context(), vosOIDCUserinfoURL(), identity.Subject, user.ID); err != nil {
		if stderrors.Is(err, types.ErrPlatformIdentityConflict) {
			c.JSON(http.StatusConflict, gin.H{"success": false, "code": "identity_conflict", "message": "This VOS identity or HybRAG account is already linked"})
			return
		}
		c.Error(errors.NewInternalServerError("Cannot save the VOS identity binding"))
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"bound": true}})
}
