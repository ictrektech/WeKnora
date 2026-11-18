package middleware

import (
	"context"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/platformidentity"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

const VOSUserProofHeader = "X-VOS-User-Proof"

// A valid local JWT alone does not establish that its platform user remains
// enabled. This lazy request-scoped proof also backs delegated MCPHub calls;
// it is verified only when a protected feature is used.
func VNotePrivateReads(users interfaces.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
		proof := strings.TrimSpace(c.GetHeader(VOSUserProofHeader))
		// Keep proof only in this server-side check, never in downstream
		// handlers or reverse proxies that may forward request headers.
		c.Request.Header.Del(VOSUserProofHeader)
		ctx := c.Request.Context()
		// Trusted in-process dispatch may continue one or both checks from the
		// initiating request. Never replace a check that is already present.
		existingVNoteCheck := types.VNoteReadCheck(ctx)
		existingMCPHubAccess := types.MCPHubAccessFromContext(ctx)
		var once sync.Once
		verifiedUser := ""
		verifiedToken := ""
		verify := func(checkCtx context.Context) {
			once.Do(func() {
				if !vosSSOEnabledForMiddleware() || proof == "" || len(proof) > 16384 {
					return
				}
				identity, err := platformidentity.VerifyVOS(
					checkCtx, vosOIDCUserinfoURLForMiddleware(), proof,
				)
				if err != nil {
					return
				}
				store, ok := users.(interface {
					GetPlatformIdentityUser(context.Context, string, string) (*types.User, error)
				})
				if !ok {
					return
				}
				user, err := store.GetPlatformIdentityUser(
					checkCtx, vosOIDCUserinfoURLForMiddleware(), identity.Subject,
				)
				if err != nil || user == nil || !user.IsActive {
					return
				}
				verifiedUser = user.ID
				verifiedToken = proof
			})
		}
		check := func(owner string) bool {
			caller := types.CallerFromContext(ctx)
			if owner == "" || caller.UserID != owner {
				return false
			}
			verify(ctx)
			return verifiedUser == owner && verifiedUser == caller.UserID
		}
		hubAccess := func(checkCtx context.Context) (string, error) {
			caller := types.CallerFromContext(checkCtx)
			if caller.UserID == "" {
				return "", types.ErrMCPHubSignInRequired
			}
			verify(checkCtx)
			if verifiedUser != caller.UserID || verifiedToken == "" {
				return "", types.ErrMCPHubSignInRequired
			}
			return verifiedToken, nil
		}
		if existingVNoteCheck == nil {
			ctx = types.WithVNoteReadCheck(ctx, check)
		}
		if existingMCPHubAccess == nil {
			ctx = types.WithMCPHubAccess(ctx, hubAccess)
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
