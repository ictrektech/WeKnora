# API Reference: Tenants (Workspaces) and Members

Manage workspaces, members, invitations, API keys, and audit logs. Operations act on the currently active workspace; cross-workspace access is validated according to endpoint permissions.

All `/tenants/:id/*` routes mount `PathTenantMatch()` at the group level (`internal/middleware/access.go`): the `:id` in the URL must equal the currently active workspace (except for cross-workspace super admins), preventing unauthorized operations on other users' workspaces.

For tenant `memory_config` configuration fields and personal memory endpoints, see [Long-Term Memory API](02-api-memory.md). When a workspace administrator updates the configuration, they must submit the complete object to be retained.

## Workspace Lifecycle

### POST /api/v1/tenants

Purpose: Create a workspace (self-service creation of a new workspace; the caller automatically becomes Owner). Permissions: Any logged-in user (may have no workspace); API key must be a platform key with `system_tenants_manage`. Handler: `internal/handler/tenant.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | Yes (`binding:"required,min=1,max=128"`) | Workspace name |
| `description` | string | No (`binding:"max=512"`) | Description |

Cross-workspace super admins may submit a complete `types.Tenant` (including `storage_quota`, `status`, etc.).

Response: 201 `{"success":true,"data":{Tenant}}` (may include `api_key` when configuration allows). Returns 403 (code 2005) if self-service creation is disabled, and 429 if quota is exceeded.

```bash
curl -X POST $BASE/api/v1/tenants -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"My Workspace"}'
```

### GET /api/v1/tenants

Purpose: List workspaces I can access. Permissions: Logged in; API key requires `manage_tenant_settings` or full-access. Handler: `internal/handler/tenant.go`

Response: 200 `{"success":true,"data":{"items":[TenantResponse]}}`

```bash
curl $BASE/api/v1/tenants -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/tenants/all

Purpose: List all workspaces (cross-workspace super admin). Permissions: `CrossTenant()` (`CanAccessAllTenants` and the cluster has `EnableCrossTenantAccess` enabled); platform key requires `system_tenants_read|manage`. Handler: `internal/handler/tenant.go`

Response: 200 `{"success":true,"data":{"items":[TenantResponse]}}`

```bash
curl $BASE/api/v1/tenants/all -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/tenants/search

Purpose: Search workspaces by keyword (cross-workspace super admin). Permissions: Same as above. Handler: `internal/handler/tenant.go`

| Query Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `keyword` | string | No | Keyword |
| `tenant_id` | string | No | Exact workspace ID |
| `page` / `page_size` | int | No | Pagination (default 1/20, max 100) |

Response: 200 `{"success":true,"data":{"items":[...],"total","page","page_size"}}`

```bash
curl "$BASE/api/v1/tenants/search?keyword=demo&page=1" -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/tenants/:id

Purpose: Workspace details. Permissions: Viewer+; platform key requires `system_tenants_read|manage`. Handler: `internal/handler/tenant.go`

Response: 200 `{"success":true,"data":{TenantResponse}}`

```bash
curl $BASE/api/v1/tenants/1 -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/tenants/:id

Purpose: Update workspace configuration. Permissions: Owner; platform key requires `system_tenants_manage`. Handler: `internal/handler/tenant.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | *string | No (`binding:"omitempty,min=1,max=128"`) | New name |
| `description` | *string | No (`binding:"omitempty,max=512"`) | New description |

Response: 200 `{"success":true,"data":{TenantResponse}}`

```bash
curl -X PUT $BASE/api/v1/tenants/1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"New Name"}'
```

### DELETE /api/v1/tenants/:id

Purpose: Delete a workspace. Permissions: Owner; platform key requires `system_tenants_manage`. The workspace record and all membership relationships are soft-deleted, and members immediately lose access; data such as knowledge bases and models within the workspace is not immediately physically purged, and queued Wiki tasks for the deleted workspace no longer call models. Handler: `internal/handler/tenant.go`

Response: 200 `{"success":true,"message":"Workspace deleted successfully"}`

```bash
curl -X DELETE $BASE/api/v1/tenants/1 -H "Authorization: Bearer $TOKEN"
```

## Workspace KV Configuration

`:key` is the configuration key, not the workspace ID (the workspace is taken from the authentication context). Valid values: `web-search-config`, `prompt-templates`, `parser-engine-config`, `storage-engine-config`, `chat-history-config`, `retrieval-config`, `memory-config`, `assistant-config`, `default-settings`.

`assistant-config` is the workspace setting for the WeKnora assistant: `display_name` (the assistant name shown in the UI, up to 24 characters, empty means the default name) and `max_access_level` (the highest permission level members can select: `read_only` read-only, `confirm` confirm before modification, `auto_write` directly execute recoverable modifications, deletions still require confirmation; default `confirm`). Members select their own level in conversation requests via `assistant_access`, and the server truncates it according to the workspace cap.

`default-settings` is the workspace default model configuration; for fields and replacement semantics, see [the explanation below](#default-settings). The default search engine is still determined by the web search service's `is_default` (`PATCH /api/v1/web-search-providers/:id`).

### GET /api/v1/tenants/kv/:key

Purpose: Read workspace-level KV configuration. Permissions: Viewer+; API key requires `manage_tenant_settings` or full-access. Handler: `internal/handler/tenant.go`

Response: 200 `{"success":true,"data":{...corresponding configuration object...}}`

```bash
curl $BASE/api/v1/tenants/kv/retrieval-config -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/tenants/kv/:key

Purpose: Update workspace-level KV configuration. Permissions: Admin+; API key requires `manage_tenant_settings` or full-access. Request body: The configuration JSON object corresponding to `:key`. Handler: `internal/handler/tenant.go`

Response: 200 `{"success":true,"message":"Configuration updated"}`

```bash
curl -X PUT $BASE/api/v1/tenants/kv/web-search-config -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"enabled":true}'
```

### Workspace Default Models: GET / PUT /api/v1/tenants/kv/default-settings {#default-settings}

GET requires Viewer+, PUT requires Admin+; API key additionally requires `manage_tenant_settings` or full-access. The workspace is taken from the authentication context. The UI entry point is [Settings → Default Settings](../03-features/06-models.md#workspace-defaults).

| Field | Type | Model Requirement |
| --- | --- | --- |
| `chat_model_id` | string | Chat model `KnowledgeQA` |
| `utility_model_id` | string | `KnowledgeQA` used for short-text auxiliary tasks; follows the chat default when empty |
| `embedding_model_id` | string | Embedding model `Embedding` |
| `rerank_model_id` | string | Rerank model `Rerank` |
| `vlm_model_id` | string | `VLLM`, or a `KnowledgeQA` with vision capability enabled |
| `asr_model_id` | string | Speech recognition model `ASR` |

PUT **replaces the entire object**; all six items should be submitted; omitted fields are equivalent to empty strings. Model IDs are trimmed of leading and trailing whitespace; a newly selected non-empty ID must be available in the current workspace, enabled, and capability-matched, otherwise 400 is returned. Unchanged invalid IDs may be retained to facilitate item-by-item repair; clearing a field restores fallback behavior.

Models explicitly selected by agents, knowledge bases, or tasks take precedence over workspace defaults. Unset or invalid workspace defaults fall back to deployment defaults, then to the first available model; when `utility_model_id` is empty or invalid, it follows the chat default. The `default_for` in `GET /api/v1/models` indicates the workspace-specified purpose (e.g., `KnowledgeQA`, `VLLM`, `Utility`); `is_default` only indicates the default marker for the model's native type and cannot be used alone to identify auxiliary tasks or cross-type vision defaults. This does not override deployment configuration or model IDs saved by existing resources.

GET returns `{"success":true,"data":{...six configuration items...}}`, with unset values as empty strings. The PUT example below clears the workspace overrides and restores default fallback:

```bash
curl -X PUT "$BASE/api/v1/tenants/kv/default-settings" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"chat_model_id":"","utility_model_id":"","embedding_model_id":"","rerank_model_id":"","vlm_model_id":"","asr_model_id":""}'
```

A successful PUT returns `{"success":true,"data":{...the six saved configuration items...},"message":"Default settings updated successfully"}`. Implementation: `internal/handler/tenant_default_settings.go`, `internal/types/default_settings.go`.

## API Keys and API Principals

### GET /api/v1/tenants/:id/api-keys

Purpose: List workspace API keys (masked display). Permissions: Owner, JWT only (API keys are denied by default). Handler: `internal/handler/tenant.go`

Response: 200 `{"success":true,"data":[{id,scope_type,name,api_key(masked),full_access,knowledge_base_ids,capabilities,last_used_at,expires_at,created_at}]}`

```bash
curl $BASE/api/v1/tenants/1/api-keys -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/tenants/:id/api-keys

Purpose: Create a workspace API key (plaintext returned only once). Permissions: Owner, JWT only. Handler: `internal/handler/tenant.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | Yes | Key name |
| `full_access` | bool | No | Workspace full-access key (default false) |
| `knowledge_base_ids` | []string | No | KB allowlist (scoped key) |
| `capabilities` | []string | No | Capability list (see overview) |
| `expires_at_unix` | *int64 | No | Expiration timestamp |

Response: 201 `{"success":true,"data":{...,"api_key":"<plaintext>","token":"<plaintext>"}}`

```bash
curl -X POST $BASE/api/v1/tenants/1/api-keys -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"ingest-bot","capabilities":["ingest","retrieve"],"knowledge_base_ids":["kb-1"]}'
```

### PUT /api/v1/tenants/:id/api-keys/:key_id

Owner, JWT only. Updates an existing Key's name, full_access, knowledge_base_ids, capabilities, expires_at_unix; authorization fields are submitted as a complete configuration, not a PATCH that changes only one field. Omitting expires_at_unix or setting it to null clears the existing expiration time. After changing permissions, the same token is used; the new authorization takes effect on subsequent authentication, and the plaintext is not returned again.

Returns 200 `{success,data:APIKeyResponse}`, with the Key masked; invalid capabilities/knowledge base scope returns 400, and non-existent returns 404.

```bash
curl -X PUT "$BASE/api/v1/tenants/1/api-keys/5" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"search-bot","full_access":false,"knowledge_base_ids":["kb-1"],"capabilities":["retrieve"]}'
```

### DELETE /api/v1/tenants/:id/api-keys/:key_id

Purpose: Delete an API key. Permissions: Owner, JWT only. Path parameter: `key_id`.

Response: 200 `{"success":true}`

```bash
curl -X DELETE $BASE/api/v1/tenants/1/api-keys/5 -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/tenants/:id/api-principal-config

Purpose: Read the API external user principal configuration. Permissions: Owner, JWT only. Handler: `internal/handler/tenant.go`

Response: 200 `{"success":true,"data":{"mode":"tenant|direct|signed_token","direct_header_name","signed_token_header_name","require_direct_header","has_hmac_secret"}}`

```bash
curl $BASE/api/v1/tenants/1/api-principal-config -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/tenants/:id/api-principal-config

Purpose: Update the API external user principal configuration. Permissions: Owner, JWT only.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `mode` | string | Yes | `tenant` / `direct` / `signed_token` |
| `require_direct_header` | bool | No | Whether direct mode requires the Header |
| `hmac_secret` | *string | No | signed_token mode secret (pass `***` to retain the original value) |

Response: 200, same as GET.

```bash
curl -X PUT $BASE/api/v1/tenants/1/api-principal-config -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"mode":"signed_token","hmac_secret":"topsecret"}'
```

### POST /api/v1/tenants/:id/api-principal-test-token

Purpose: Issue an external user JWT for testing. Permissions: Owner, JWT only.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `external_user_id` | string | Yes | External user ID (≤128 characters) |
| `expires_in_seconds` | int | No | 1-3600, default 900 |

Response: 200 `{"success":true,"data":{"token","header_name","expires_in_seconds","expires_at_unix","external_user_id"}}`

```bash
curl -X POST $BASE/api/v1/tenants/1/api-principal-test-token -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"external_user_id":"u-123"}'
```

## Member Management (/tenants/:id/members)

Handler: `internal/handler/tenant_member.go`. API key requires `manage_members` or full-access.

### GET /api/v1/tenants/:id/members

Purpose: Member list. Permissions: Viewer+.

| Query Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `q` | string | No | Email/username filter |
| `page` / `page_size` | int | No | Pagination |

Response: 200 `{"success":true,"data":{"members":[{user_id,email,username,avatar,role,status,invited_by,joined_at}],"total","page","page_size"}}`

```bash
curl $BASE/api/v1/tenants/1/members -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/tenants/:id/members

Purpose: Add a member directly. Permissions: Owner.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `email` | string | Yes (`binding:"required,email"`) | Member email (must already be registered) |
| `role` | string | Yes (`binding:"required"`) | `owner/admin/contributor/viewer` |

Response: 201 `{"success":true,"data":{member object}}`

```bash
curl -X POST $BASE/api/v1/tenants/1/members -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"email":"b@ex.com","role":"contributor"}'
```

### PUT /api/v1/tenants/:id/members/:user_id

Purpose: Change a member's role. Permissions: Owner. Request body: `{"role":"admin"}` (`binding:"required"`).

Response: 200 `{"success":true}`

```bash
curl -X PUT $BASE/api/v1/tenants/1/members/u-123 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"role":"admin"}'
```

### DELETE /api/v1/tenants/:id/members/:user_id

Purpose: Remove a member. Permissions: Owner.

Response: 200 `{"success":true}`

```bash
curl -X DELETE $BASE/api/v1/tenants/1/members/u-123 -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/tenants/:id/leave

Purpose: Leave the workspace (any member may leave on their own; the service layer rejects a leave that would result in the workspace having no Owner). Permissions: Viewer+, JWT only.

Response: 200 `{"success":true}`

```bash
curl -X POST $BASE/api/v1/tenants/1/leave -H "Authorization: Bearer $TOKEN"
```

## Workspace Invitations (/tenants/:id/invitations and invite-links)

Handler: `internal/handler/tenant_invitation.go`, `internal/handler/tenant_invite_link.go`. API key requires `manage_members` or full-access.

### GET /api/v1/tenants/:id/invitations

Purpose: Workspace invitation list. Permissions: Viewer+.

| Query Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `include_terminal` | bool | No | Include completed invitations |
| `page` / `page_size` | int | No | Pagination |

Response: 200 `{"success":true,"data":{"invitations":[{id,tenant_id,invitee_email,inviter_email,role,status,message,expires_at,is_share_link,accepted_count,...}],"total","page","page_size"}}`

```bash
curl $BASE/api/v1/tenants/1/invitations -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/tenants/:id/invitations

Purpose: Invite a member (the invitee is only added after confirming at `/me/invitations`). Permissions: Owner.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `email` | string | Yes (`binding:"required,email"`) | Invitee email |
| `role` | string | Yes (`binding:"required"`) | Role to grant |
| `message` | string | No | Additional message |

Response: 201 `{"success":true,"data":{TenantInvitationResponse}}`

```bash
curl -X POST $BASE/api/v1/tenants/1/invitations -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"email":"c@ex.com","role":"viewer"}'
```

### DELETE /api/v1/tenants/:id/invitations/:inv_id

Purpose: Revoke an invitation. Permissions: Owner.

Response: 200 `{"success":true}`

```bash
curl -X DELETE $BASE/api/v1/tenants/1/invitations/12 -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/tenants/:id/invite-links

Purpose: Create a share link (a multi-use registration invitation link). Permissions: Owner. Handler: `internal/handler/tenant_invite_link.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `role` | string | Yes (`binding:"required"`) | Role granted by the link |
| `message` | string | No | Additional message |

Response: 201 `{"success":true,"data":{id,token,invite_url,role,status,expires_at,is_share_link:true,accepted_count}}`

```bash
curl -X POST $BASE/api/v1/tenants/1/invite-links -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"role":"viewer"}'
```

## Audit Logs

Handler: `internal/handler/audit_log.go`. Cursor-based pagination.

### GET /api/v1/tenants/:id/audit-log

Purpose: Workspace audit log (including records of denied operations). Permissions: Admin+, JWT only.

| Query Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `after_id` | int | No | Cursor (the previous response's `next_cursor`) |
| `limit` | int | No | 1-100, default 50 |
| `action` | string | No | Filter by action (e.g., `rbac.member_added`) |
| `outcome` | string | No | `success` / `denied` |
| `actor` | string | No | Filter by actor user_id |

Response: 200 `{"success":true,"data":[AuditLog],"next_cursor":N}`

```bash
curl "$BASE/api/v1/tenants/1/audit-log?limit=50" -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledge-bases/:id/activity

Purpose: Activity stream for a single KB (read-only audit). Permissions: KB creator OR Admin+, and has read permission on the KB; JWT only. Query parameters are the same as above (`after_id/limit/action/outcome/actor`). Registered in `RegisterKnowledgeBaseActivityRoutes`.

Response: 200 `{"success":true,"data":[AuditLog],"next_cursor":N}`. `details` is the action payload; if the entry was triggered by an API Key, it includes `api_key_id` and `api_key_name` (name snapshot, no plaintext Key).

```bash
curl $BASE/api/v1/knowledge-bases/kb-1/activity -H "Authorization: Bearer $TOKEN"
```

## Implementation Reference

Route registration: `RegisterTenantRoutes` in `internal/router/routes_auth_tenant.go`. Handlers: `internal/handler/tenant.go`, `internal/handler/tenant_member.go`, `internal/handler/tenant_invitation.go`, `internal/handler/tenant_invite_link.go`, `internal/handler/audit_log.go`.
