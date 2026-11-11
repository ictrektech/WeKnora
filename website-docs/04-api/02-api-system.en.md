# API Reference: System and Platform Management

Provides deployment-level system information and platform management interfaces, including global settings, task queues, platform API Keys, cross-tenant auditing, and user password reset. For feature descriptions, see [Platform Management and System Administrators](../03-features/20-platform-admin.md).

The entire `/system/admin/*` group is guarded by `SystemAdmin()`; platform API Keys are subdivided by capability (`system_settings_read/manage`, `system_runtime_read/manage`, `system_tenants_read/manage`, `system_audit_read`).

## System Information (/api/v1/system)

Handler: `internal/handler/system.go`. API key: `manage_vector_stores`/full. Responses in this group use the `{"code":0,"msg":"success","data":...}` wrapper.

### GET /api/v1/system/capabilities

Viewer+; readable with API Key. Returns `{code:0,data:{edition,capabilities}}`, with each capability providing supported/reason. The frontend controls menu entries based on the deployment edition, actually registered routes, and Docker switches; hiding a menu does not replace backend permission checks.

```bash
curl "$BASE/api/v1/system/capabilities" -H "Authorization: Bearer $TOKEN"
```

`settings.sandbox.host` in `capabilities` indicates whether the current deployment can use the local operating system sandbox; currently only the native macOS desktop application may report it as supported.

### POST /api/v1/system/host-project-dir

Purpose: pop up a system folder picker on the machine running WeKnora, for binding a new session to a local project directory (since v0.8.2, native desktop application only). Permission: Viewer+, JWT only; API Keys are always rejected. No request body.

Response: 200 `{"code":0,"msg":"success","data":{"dir":"/Users/me/project"}}`; when the user cancels the selection, `dir` is an empty string; non-desktop deployments return 404.

### GET /api/v1/system/info

Purpose: system version and engine information. Permission: Viewer+.

Response: 200 `{"code":0,"msg":"success","data":{version,edition,commit_id,build_time,go_version,keyword_index_engine,vector_store_engine,graph_database_engine,minio_enabled,db_version,started_at,uptime_seconds}}`

```bash
curl $BASE/api/v1/system/info -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/system/parser-engines

Purpose: parser engine list and DocReader connection status. Permission: Viewer+.

Response: 200 `{"code":0,"msg":"success","data":[...],"docreader_addr","docreader_transport","connected"}`

```bash
curl $BASE/api/v1/system/parser-engines -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/system/parser-engines/check

Purpose: probe a parser engine with the given configuration (`types.ParserEngineConfig` request body). Permission: Admin+.

Response: 200, same as above.

```bash
curl -X POST $BASE/api/v1/system/parser-engines/check -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{}'
```

### POST /api/v1/system/docreader/reconnect

Purpose: reconnect DocReader. Permission: Admin+. Request body: `{"addr":"host:port"}` (`binding:"required"`).

Response: 200 `{"code":0,"msg":"Connection successful",...,"connected":true}`

```bash
curl -X POST $BASE/api/v1/system/docreader/reconnect -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"addr":"docreader:50051"}'
```

### GET /api/v1/system/storage-engine-status

Purpose: object storage engine availability. Permission: Viewer+.

Response: 200 `{"code":0,"msg":"success","data":{"engines":[{name,allowed,available,description}],"allowed_providers":[...],"minio_env_available":bool}}`

```bash
curl $BASE/api/v1/system/storage-engine-status -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/system/storage-engine-check

Purpose: validate storage configuration (probed after SSRF protection). Permission: Admin+. Request body: `provider` (required, `minio/cos/tos/s3/oss/ks3/obs`) + the corresponding `minio|cos|tos|s3|oss|ks3|obs` configuration object.

Response: 200 `{"code":0,"data":{"ok","message","bucket_created"}}`

```bash
curl -X POST $BASE/api/v1/system/storage-engine-check -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"provider":"minio","minio":{"endpoint":"minio:9000"}}'
```

## System Administration (/api/v1/system/admin, SystemAdmin only)

The group is mounted with the `SystemAdmin()` guard (always enforced, unaffected by EnableRBAC); platform API keys require the corresponding `system_*` capability. Read endpoints in this group mostly return raw rows/arrays (no wrapper). Handler: `internal/handler/system.go`, `internal/handler/audit_log.go`.

### POST /api/v1/system/admin/promote

Purpose: grant SystemAdmin. Request body: `user_id` (UUID, preferred) or `email` (one of the two).

Response: 200 `UserInfo` (raw object).

```bash
curl -X POST $BASE/api/v1/system/admin/promote -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"email":"admin@ex.com"}'
```

### POST /api/v1/system/admin/revoke

Purpose: revoke SystemAdmin. Request body: `{"user_id":"..."}` (`binding:"required"`).

Response: 200 `UserInfo`

```bash
curl -X POST $BASE/api/v1/system/admin/revoke -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"user_id":"u-1"}'
```

### GET /api/v1/system/admin/list

Purpose: SystemAdmin list. Query parameters: `offset` (default 0), `limit` (default 50, max 200).

Response: 200 `{"total":N,"admins":[UserInfo]}`

```bash
curl $BASE/api/v1/system/admin/list -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/system/admin/users/create

System administrators only; this endpoint is not available to platform API Keys. Request fields: username (2–50 characters), email (valid email), password (optional or null to auto-generate). An explicit empty string is still subject to password policy validation and is not treated as auto-generation.

| HTTP status | Response and meaning |
| --- | --- |
| 201 | `{user:UserInfo,generated_password?}`, newly created; the password is returned only when auto-generated |
| 200 | `{user:UserInfo}`, identity already exists; account and password are not modified |
| 400 | Parameters or password policy not satisfied |
| 409 | Email and username correspond to different identities |

This is a raw response object, with no success/data wrapper and no idempotent field. Use the HTTP status to distinguish newly created from existing accounts. Tenant assignment follows auth.default_tenant_mode.

```bash
curl -i -X POST "$BASE/api/v1/system/admin/users/create" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"username":"alice","email":"alice@example.com"}'
```

### POST /api/v1/system/admin/users/reset-password

Purpose: reset a user's password. Request body: `email` (`binding:"required,email"`), `new_password` (`binding:"required"`).

Response: 200 `{"message":"Password reset successfully"}`

```bash
curl -X POST $BASE/api/v1/system/admin/users/reset-password -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"email":"a@ex.com","new_password":"newpass1"}'
```

### GET /api/v1/system/admin/api-keys

Purpose: platform API key list (masked).

Response: 200 `{"success":true,"data":[{id,name,api_key,capabilities,expires_at_unix,...}]}`

```bash
curl $BASE/api/v1/system/admin/api-keys -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/system/admin/api-keys

Purpose: create a platform API key (plaintext returned only once). Request body: `name` (non-empty), `capabilities` (`system_*` list, required), `expires_at_unix` (optional, must be a future time).

Response: 201 `{"success":true,"data":{...,"api_key":"<plaintext>","token":"<plaintext>"}}`

```bash
curl -X POST $BASE/api/v1/system/admin/api-keys -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"ops","capabilities":["system_tenants_read"]}'
```

### DELETE /api/v1/system/admin/api-keys/:key_id

Purpose: delete a platform API key.

Response: 200 `{"success":true}`

```bash
curl -X DELETE $BASE/api/v1/system/admin/api-keys/3 -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/system/admin/settings and GET /api/v1/system/admin/settings/:key

Purpose: platform runtime settings list / single item (platform key requires `system_settings_read|manage`).

Response: 200 `[SystemSetting]` / `SystemSetting` (raw, no wrapper; fields: `key,value,value_type,description,last_modified_by,last_modified_at`).

```bash
curl $BASE/api/v1/system/admin/settings -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/system/admin/settings/:key

Purpose: update a setting (platform key requires `system_settings_manage`). Request body: `{"value":<any JSON, validated against the registry type>}` (required).

Response: 200 `SystemSetting`

```bash
curl -X PUT $BASE/api/v1/system/admin/settings/default_storage_quota -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"value":10737418240}'
```

### DELETE /api/v1/system/admin/settings/:key

Purpose: restore a setting to its default value.

Response: 200 `{"success":true}`

```bash
curl -X DELETE $BASE/api/v1/system/admin/settings/default_storage_quota -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/system/admin/runtime/documents

System administrators read persistent progress by document. Platform API Key requires `system_runtime_read`. Query parameters: `status` (default `unfinished`; options `all`, `failed`, `completed`), `page` (1–10000), `page_size` (1–100, default 25), `kb_id`, `search` (title, filename, or knowledge base name, max 200 characters).

Returns `data.documents`, `total`, `page`, `page_size`. Each document includes the current `attempt`, `ingestion_status`, `wiki_status`, `wiki_pending`, `wiki_retries`, `failure_count`, `can_start`, and stage metadata `stages`. `ingestion_status=ready` means primary ingestion has completed; `wiki_status` separately indicates the subsequent Wiki status. Historical failure counts are independent of the current status.

### GET /api/v1/system/admin/runtime/documents/:knowledge_id

Permissions same as the list endpoint. Returns `data.document`, `failures`, and `has_more_failures`. Failure records are in reverse chronological order, up to 50 entries; error previews are up to 2048 characters. Original task payloads and stage inputs/outputs are not returned. Returns 404 if the document does not exist or has been deleted.

### POST /api/v1/system/admin/runtime/documents/wiki-start

System administrators start existing claimable Wiki to-dos; platform API Key requires `system_runtime_manage`. A single request is `{"knowledge_id":"doc-id","count":1}`; a batch request is `{"count":10,"knowledge_base_id":"kb-id","search":"optional search"}`, with count ranging from 1–100.

Only schedules documents that have completed primary ingestion and have Wiki enabled, ordered by the earliest to-do ID, scanning at most 1000 candidates per call; skips documents that are processing or already have the same trigger task. Does not create new document to-dos, reset failure counts, or remove failure history. Returns `data.requested`, `started` (number of documents accepted this time), `already_queued` (number of existing trigger tasks found during candidate scanning). The count only limits this submission; existing automatic queues continue to run.

If the queue becomes unavailable mid-way, returns 503, with `data` retaining the number already accepted. Stable task IDs prevent duplicate requests from creating the same trigger task; document completion is read via the progress endpoint. Starting remains subject to the original knowledge base and model concurrency limits; successful enqueue operations are written to the system audit log.

### GET /api/v1/system/admin/runtime/queues

Purpose: asynq queue depth and concurrency status (Lite mode returns `available:false`; platform key requires `system_runtime_read|manage`).

Response: 200 `{"available",upstream_concurrency,parse_concurrency,wiki_concurrency,pools,queues,model_limiter_available,models,timestamp}`

```bash
curl $BASE/api/v1/system/admin/runtime/queues -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/system/admin/runtime/queues/:queue/tasks

Purpose: queue task list. Query parameters: `state` (`pending/active/scheduled/retry/archived/completed`), `cursor`, `page_size` (default 20, max 100).

Response: 200 `{"available","tasks":[RuntimeTaskInfo],"page_size","has_more","next_cursor"}`

```bash
curl "$BASE/api/v1/system/admin/runtime/queues/default/tasks?state=pending" -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/system/admin/runtime/queues/:queue/tasks/:task_id/actions/:action

Purpose: task operations (`action` ∈ `cancel/run_now/delete`; platform key requires `system_runtime_manage`).

Response: 200 `{"success":true}`

```bash
curl -X POST $BASE/api/v1/system/admin/runtime/queues/default/tasks/t-1/actions/cancel \
  -H "Authorization: Bearer $TOKEN"
```

### DELETE /api/v1/system/admin/runtime/queues/:queue/archived

Purpose: clear archived tasks.

Response: 200 `{"success":true,"deleted":N}`

```bash
curl -X DELETE $BASE/api/v1/system/admin/runtime/queues/default/archived -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/system/admin/tenants/apply-default-storage-quota

Purpose: batch-write the current default storage quota to all tenants (platform key requires `system_tenants_manage`). No request body.

Response: 200 `{"affected":N,"quota_bytes":N,"quota_gb":N}`

```bash
curl -X POST $BASE/api/v1/system/admin/tenants/apply-default-storage-quota -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/system/admin/audit-log

Purpose: platform-level audit log (tenant_id=0 rows; platform key requires `system_audit_read`). Query parameters same as tenant audit (`after_id/limit/action/outcome/actor`). Handler: `internal/handler/audit_log.go`

Response: 200 `{"success":true,"data":[AuditLog],"next_cursor":N}`

```bash
curl $BASE/api/v1/system/admin/audit-log -H "Authorization: Bearer $TOKEN"
```

## Implementation Reference

Route registration: `RegisterSystemAdminRoutes` and `RegisterSystemRoutes` in `internal/router/routes_auth_tenant.go`. Handler: `internal/handler/system.go`, `internal/handler/audit_log.go`.


## Platform User Management

The following endpoints are accessible only to system administrator user sessions; API Keys are not allowed.

### GET /api/v1/system/admin/users

List users across the entire deployment with pagination. Query parameter `offset` defaults to 0; `limit` defaults to 50, max 200. The response is an unwrapped object `{"users":[UserInfo],"offset":0,"limit":50,"has_more":false}`; passwords or hashes are not returned.

### POST /api/v1/system/admin/users/status

Request body `{"user_id":"target user ID","is_active":false}`, both fields required. Deactivation revokes the target user's existing sessions; you cannot deactivate yourself or the last enabled system administrator (400); returns 404 if the user does not exist. On success returns `{"message":"User status updated","user":UserInfo,"is_active":false}`. Account creation and password reset still use `/users/create` and `/users/reset-password` above.

## Model Catalog (/api/v1/system/admin/model-catalog)

Accessible only to system administrator user sessions; API Keys are not allowed for this group of endpoints.

| Method and path | Purpose |
| --- | --- |
| `GET /system/admin/model-catalog` | Returns the current `version`, `baseline`, administrator `overlay`, the 20 most recent historical versions, and the `builtin` / `deployment` / `effective` catalogs |
| `POST /system/admin/model-catalog/preview` | Validates the overlay document and returns the candidate catalog (only `effective` and the normalized `overlay`; `history` / `builtin` / `deployment` are `null`), without persisting or publishing |
| `PUT /system/admin/model-catalog` | Validates, saves a new version, and publishes it; auditing records only version metadata |

Preview and publish use the same request body:

```json
{
  "version": 0,
  "baseline": "deployment baseline identifier returned by GET",
  "overlay": {
    "providers": {
      "openai": {
        "models": [{"id": "gpt-5", "context_window": 128000}]
      }
    }
  }
}
```

The response is an unwrapped catalog state object. Each vendor entry additionally carries `model_thinking_levels` (the levels selectable after enabling thinking, listed by chat model id, merging vendor mappings and protocol capabilities) and `vendor_thinking_levels` (the vendor default levels used by models without individually configured levels). Invalid documents return 400; stale versions or a deployment baseline mismatch with the requesting instance return 409. Publishing persists first, then switches this instance; other instances sync within about 5 seconds. To roll back, publish again using a historical `overlay` with the current `version` / `baseline`. For complete rules and limitations, see [Model Management](../03-features/06-models.md#系统管理员维护模型目录).
