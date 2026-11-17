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
// enabled. This lazy request-scoped proof is independent of background grants:
// revoking synchronization still permits the active owner to read retained copies.
func VNotePrivateReads(users interfaces.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
		proof := strings.TrimSpace(c.GetHeader(VOSUserProofHeader))
		// Keep proof only in this server-side check, never in downstream
		// handlers or reverse proxies that may forward request headers.
		c.Request.Header.Del(VOSUserProofHeader)
		ctx := c.Request.Context()
		// Trusted in-process dispatch may continue the initiating web request's
		// server-side check. An incoming HTTP request cannot manufacture this context.
		if _, internal := types.InternalDispatchFromContext(ctx); internal && types.VNoteReadCheck(ctx) != nil {
			c.Next()
			return
		}
		var once sync.Once
		verifiedUser := ""
		check := func(owner string) bool {
			caller := types.CallerFromContext(ctx)
			if owner == "" || caller.UserID != owner {
				return false
			}
			once.Do(func() {
				if !vosSSOEnabledForMiddleware() || proof == "" || len(proof) > 16384 {
					return
				}
				identity, err := platformidentity.VerifyVOS(ctx, vosOIDCUserinfoURLForMiddleware(), proof)
				if err != nil {
					return
				}
				store, ok := users.(interface {
					GetPlatformIdentityUser(context.Context, string, string) (*types.User, error)
				})
				if !ok {
					return
				}
				user, err := store.GetPlatformIdentityUser(ctx, vosOIDCUserinfoURLForMiddleware(), identity.Subject)
				if err != nil || user == nil || !user.IsActive || user.ID != caller.UserID || user.TenantID != caller.TenantID {
					return
				}
				verifiedUser = user.ID
			})
			return verifiedUser == owner
		}
		c.Request = c.Request.WithContext(types.WithVNoteReadCheck(ctx, check))
		c.Next()
	}
}
