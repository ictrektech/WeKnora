# API Reference: Authentication and Users

Provides registration, login, token refresh, profile, and invitation handling endpoints. Authentication requirements vary by endpoint; public endpoints are marked in each entry.

Unless otherwise noted, endpoints in this group only require "logged in" (no role minimum) after the authentication middleware. See each entry for unauthenticated endpoints.

## Authentication (/api/v1/auth)

### POST /api/v1/auth/register

Purpose: Register a new user (self-serve registration mode). Unauthenticated. Handler: `internal/handler/auth.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `username` | string | Yes | Username, 2–50 characters |
| `email` | string | Yes | Email |
| `password` | string | Yes | Password (8–32 characters, letters + digits; complex mode additionally requires upper and lower case and special characters) |

Whether a personal space is automatically created is determined by the server's `auth.default_tenant_mode` and cannot be specified in the request body. Returns 403 in `invite_register` (invitation-only registration) and `invite_only` (registration disabled) modes.

Response: 201 `{"success":true,"message":"...","user":{User}}`

```bash
curl -X POST $BASE/api/v1/auth/register -H 'Content-Type: application/json' \
  -d '{"username":"alice","email":"a@ex.com","password":"secret123"}'
```

### POST /api/v1/auth/register-by-invite

Only `self_serve` and `invite_register` modes allow registration with a valid invitation link; `invite_only` mode returns 403.

Purpose: Register via an invitation/share link token and join a space. Unauthenticated, IP rate limit 30 requests/minute. Handler: `internal/handler/auth_register_by_invite.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `token` | string | Yes (`binding:"required"`) | Invitation token |
| `email` | string | Yes (`binding:"required,email"`) | Email |
| `username` | string | Yes (`binding:"required"`) | Username |
| `password` | string | Yes (`binding:"required,min=6"`) | Password (8–32 characters, letters + digits; complex mode additionally requires upper and lower case and special characters) |

Response: 201, same as Login (`user/active_tenant/memberships/token/refresh_token`).

```bash
curl -X POST $BASE/api/v1/auth/register-by-invite -H 'Content-Type: application/json' \
  -d '{"token":"<invite_token>","email":"a@ex.com","username":"alice","password":"secret123"}'
```

### POST /api/v1/auth/invitations/lookup

Purpose: Anonymously look up the space information corresponding to an invitation token (preview before registration). Unauthenticated, IP rate limited. Handler: `internal/handler/auth_register_by_invite.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `token` | string | Yes (`binding:"required"`) | Invitation token |

Response: 200 `{"success":true,"data":{"tenant_id","tenant_name","role","expires_at"}}`

```bash
curl -X POST $BASE/api/v1/auth/invitations/lookup -H 'Content-Type: application/json' -d '{"token":"<invite_token>"}'
```

### POST /api/v1/auth/login

Purpose: Email and password login. Unauthenticated. Handler: `internal/handler/auth.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `email` | string | Yes (`binding:"required"`) | Email |
| `password` | string | Yes (`binding:"required"`) | Password |

Response: 200 `{"success":true,"user":{...},"active_tenant":{...},"memberships":[...],"token":"...","refresh_token":"..."}`

```bash
curl -X POST $BASE/api/v1/auth/login -H 'Content-Type: application/json' -d '{"email":"a@ex.com","password":"secret123"}'
```

### POST /api/v1/auth/auto-setup

Purpose: Native desktop Lite automatic account creation, space creation, and login. Must carry the per-process random `X-WeKnora-Desktop-Token` provided by the desktop native bridge; anonymous HTTP requests are not accepted. Regular browsers use the registration/login endpoints. Handler: `internal/handler/auth.go`

Response: 200, same as Login; missing or incorrect desktop credentials return 401.

### GET /api/v1/auth/config

Purpose: Query authentication configuration such as registration mode. Unauthenticated. Handler: `internal/handler/auth.go`

Response: 200 `{"success":true,"registration_mode":"self_serve|invite_register|invite_only","complex_password_enabled":false}`

```bash
curl $BASE/api/v1/auth/config
```

### POST /api/v1/auth/switch-tenant

Purpose: Switch the current active space and issue new tokens. Requires login (can also be called without a space). Handler: `internal/handler/auth.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `tenant_id` | uint64 | Yes (`binding:"required"`) | Target space ID |
| `refresh_token` | string | No | Used to issue new tokens |

Response: 200, same as Login.

A successful switch writes the target space into the account-level "most recent active tenant" preference, so the next login (password/OIDC/device change) and refresh both return to that space. The refresh JWT does not contain `tenant_id`, so if writing the preference fails, the entire switch fails and no new token is issued. API clients no longer need to additionally send `PUT /auth/me/preferences`. The Web UI does not use this endpoint to switch spaces. A single switch changes the next landing point for all of that user's devices.

```bash
curl -X POST $BASE/api/v1/auth/switch-tenant -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"tenant_id":2}'
```

### GET /api/v1/auth/oidc/config

Purpose: Query whether OIDC is enabled and its display name. Unauthenticated. Handler: `internal/handler/auth.go`

Response: 200 `{"success":true,"enabled":bool,"provider_display_name":"..."}`

```bash
curl $BASE/api/v1/auth/oidc/config
```

### GET /api/v1/auth/oidc/start

No login required; directly returns 302 with a Location pointing to the IdP, usable for enterprise portal links. No need to first request a JSON authorization URL; the callback is constructed from the request origin as `/api/v1/auth/oidc/callback`. The login result after the callback is consistent with the original OIDC flow.

```bash
curl -i "$BASE/api/v1/auth/oidc/start"
```

### GET /api/v1/auth/oidc/url

Purpose: Obtain the OIDC authorization redirect URL. Unauthenticated. Handler: `internal/handler/auth.go`

| Query Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `redirect_uri` | string | Yes | Callback address |

Response: 200 `{"success":true,"authorization_url":"...","nonce":"..."}`

```bash
curl "$BASE/api/v1/auth/oidc/url?redirect_uri=https://app.example.com/callback"
```

### GET /api/v1/auth/oidc/callback

Purpose: OIDC authorization callback (entered via browser redirect). Unauthenticated. Handler: `internal/handler/auth.go`

Query parameters: `code`, `state`, `error`, `error_description` (all returned by the OIDC provider).

Response: 302 redirect to the frontend, carrying `#oidc_result=<base64url>` on success and `#oidc_error=...` on failure.

```bash
curl -i "$BASE/api/v1/auth/oidc/callback?code=xxx&state=yyy"
```

### POST /api/v1/auth/refresh

Purpose: Exchange a refresh token for new tokens. Unauthenticated. Handler: `internal/handler/auth.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `refreshToken` | string | Yes (`binding:"required"`) | Refresh token |

Response: 200 `{"success":true,"access_token":"...","refresh_token":"..."}`

```bash
curl -X POST $BASE/api/v1/auth/refresh -H 'Content-Type: application/json' -d '{"refreshToken":"<rt>"}'
```

### GET /api/v1/auth/validate

Purpose: Validate whether the current token is valid. Requires login (can be called without a space). Handler: `internal/handler/auth.go`

Response: 200 `{"success":true,"message":"Token is valid","user":{UserInfo}}`

```bash
curl $BASE/api/v1/auth/validate -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/auth/logout

Purpose: Log out (invalidate the current token). Requires login. No request body. Handler: `internal/handler/auth.go`

Response: 200 `{"success":true,"message":"Logout successful"}`

```bash
curl -X POST $BASE/api/v1/auth/logout -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/auth/me

Purpose: Query the current caller's identity (user/space/memberships/capabilities). Requires login; API key also works (policy `apiKeyAny()`, any valid key). Handler: `internal/handler/auth.go`

Response: 200 `{"success":true,"data":{"user":{UserInfo},"tenant":{TenantResponse},"memberships":[...],"tenant_required":bool,"capabilities":{"can_create_tenant":bool,"auto_accept_invitation":bool}}}`

```bash
curl $BASE/api/v1/auth/me -H "X-API-Key: $API_KEY"
```

### PUT /api/v1/auth/me/preferences

Purpose: Update personal preferences (most recent active space). Requires login. Handler: `internal/handler/auth.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `last_active_tenant_id` | *uint64 | No | A positive integer sets/replaces; `0` clears (next login returns to home); omitted means no change. On success, `POST /auth/switch-tenant` writes the same field server-side. |

Response: 200 `{"success":true,"data":{UserPreferences}}`

```bash
curl -X PUT $BASE/api/v1/auth/me/preferences -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"last_active_tenant_id":2}'
```

The password policy is provided by `GET /auth/config`. The special character set allowed in complex mode is `!@#$%^&*()_+-=[]{}|;:,.<>?`; the complete validation rules cannot be derived solely from the length tags in the struct binding.

### POST /api/v1/auth/change-password

Purpose: Change password. Requires login. Handler: `internal/handler/auth.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `old_password` | string | Yes (`binding:"required"`) | Old password |
| `new_password` | string | Yes (`binding:"required"`) | New password (8–32 characters, letters + digits; complex mode additionally requires upper and lower case and special characters) |

Response: 200 `{"success":true,"message":"Password changed successfully"}`

```bash
curl -X POST $BASE/api/v1/auth/change-password -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"old_password":"old","new_password":"NewPass123!"}'
```

Changing the password requires providing the correct old password, and the new password cannot be the same as the old one. On success, all sessions are revoked and re-login is required. The default policy is 8–32 characters, letters and digits; complex mode requires upper and lower case letters, digits, and special characters. Error details may be `invalid_old_password`, `password_policy`, `same_password`, all returning 400.

### POST /api/v1/me/invitations/accept-by-token

Requires login; only operates on the current user, and new users without a space can also call it. Request `{"token":"<invite-token>"}`; a valid share invitation makes the current user join the corresponding space. Invalid, expired, or revoked links return 410; an empty token returns 400. The success response is `{success:true,data:{membership:{tenant_id,role,status,joined_at},tenant_name}}`, and the frontend switches spaces accordingly.

```bash
curl -X POST "$BASE/api/v1/me/invitations/accept-by-token" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"token":"<invite-token>"}'
```

## My Invitations (/api/v1/me/invitations)

The service layer guarantees "only the invitee can accept/decline"; no role minimum (new users without a space can also use it). Handler: `internal/handler/tenant_invitation.go`

### GET /api/v1/me/invitations

Purpose: List invitations sent to me.

| Query Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `include_terminal` | bool | No | When `true`, includes completed invitations |

Response: 200 `{"success":true,"data":{"invitations":[TenantInvitationResponse],"total":N}}`

```bash
curl $BASE/api/v1/me/invitations -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/me/invitations/pending-count

Purpose: Pending invitation count (lightweight polling).

Response: 200 `{"success":true,"data":{"pending_count":N}}`

```bash
curl $BASE/api/v1/me/invitations/pending-count -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/me/invitations/:inv_id/accept

Purpose: Accept an invitation and write the membership. Path parameter: `inv_id` invitation ID. No request body.

Response: 200 `{"success":true,"data":{"membership":{"tenant_id","role","status","joined_at"}}}`

```bash
curl -X POST $BASE/api/v1/me/invitations/12/accept -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/me/invitations/:inv_id/decline

Purpose: Decline an invitation. Path parameter: `inv_id`. No request body.

Response: 200 `{"success":true}`

```bash
curl -X POST $BASE/api/v1/me/invitations/12/decline -H "Authorization: Bearer $TOKEN"
```

## Implementation Reference

Route registration: `RegisterAuthRoutes` and `RegisterMyInvitationRoutes` in `internal/router/routes_auth_tenant.go`. Handlers: `internal/handler/auth.go`, `internal/handler/auth_register_by_invite.go`, `internal/handler/tenant_invitation.go`.
