# API Reference: Long-Term Memory

Manage the current caller's long-term memory, topics, and document preferences, as well as space-level memory configuration. Paths use the `/api/v1` prefix.

All personal endpoints require Viewer+ and the API Key must be full-access. The scope is determined from the credentials; an arbitrary `subject_id` is not accepted. In the examples, `$BASE` is the service address and `$TOKEN` is the current user's Bearer token.

## Space Configuration and Request Toggles

Space configuration uses `GET/PUT /tenants/kv/memory-config`, not the tenant name/description update endpoints. Reading requires Viewer+; writing requires Admin+; the API Key requires manage_tenant_settings or full-access. The response is `{success,data:MemoryConfig}`; PUT passes the configuration object directly. The personal `PUT /memory/settings` cannot replace the space toggle.

```bash
curl -X PUT "$BASE/api/v1/tenants/kv/memory-config" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"enabled":true,"write_mode":"explicit_only","max_items":200}'
```

`memory_config` fields: `enabled`, `write_mode` (explicit_only/auto), `extract_model_id`, `max_items`, `extract_delay_seconds`, `extract_min_interval_seconds`, `extract_instructions`, `interest_threshold`, `embedding_model_id`, `vector_recall`, `retrieval_conditioning`. For semantics, see [Long-Term Memory](../03-features/23-memory.md). When updating, submit the complete configuration object with the fields to retain.

If `CustomAgentConfig.memory_enabled` is omitted, it inherits the space setting; false prohibits this agent from using memory. IM/Embed use the configuration of the bound agent; the current channel structure has no independent memory_enabled field.

## Personal Settings

| Method | Path | Request / Response |
| --- | --- | --- |
| GET | `/memory/settings` | `{success,data:{workspace_enabled,user_enabled,effective,write_mode,item_count,max_items}}` |
| PUT | `/memory/settings` | `{"enabled":true}`, enabled is required; returns the updated settings |

`effective` indicates the combined result of the space and personal toggles; a single chat is also constrained by the agent toggle.

```bash
curl -X PUT "$BASE/api/v1/memory/settings" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"enabled":true}'
```

## Items

| Method | Path | Request / Response |
| --- | --- | --- |
| GET | `/memory/items` | Optional status, limit, offset; `{success,data:[MemoryItem],total}` |
| POST | `/memory/items` | `{kind,content,importance}`; 200 `{success,data:MemoryItem}` |
| PUT | `/memory/items/:id` | `{content,importance}`; 200 `{success,data:MemoryItem}` |
| DELETE | `/memory/items/:id` | 200 `{"success":true}` |
| POST | `/memory/items/:id/confirm` | 200 `{success,data:MemoryItem}`; returns 409 when the inference has become invalid or its basis has been modified |
| POST | `/memory/items/:id/reject` | 200 `{"success":true}` |
| DELETE | `/memory/items` | Clears the current identity; 200 `{success,removed}` |

`status` can be active, pending, superseded, or archived; omitting it applies no filter. `limit` defaults to 50, with a valid range of 1–200; out-of-range values fall back to 50. `offset` defaults to 0; negative values are reset to zero. kind is profile/preference/fact/task/interest; content is a short memory, up to 300 characters; importance is used for importance ordering.

MemoryItem includes `id`, `kind`, `content`, `topic`, `importance`, `origin`, `status`, `source_session_id`, `source_message_id`, `expires_at`, `superseded_by`, and creation/modification times. pending items do not participate in prompts; after editing, they are treated as manually maintained.

Used to modify a pending inference for an existing memory; the old item remains in effect until confirmation. Confirmation activates the inference and replaces the old item in the same transaction. Inferences that have become invalid, have expired, or whose basis has been modified/deleted cannot be confirmed and return 409; the client should refresh the list. When the content is almost entirely sensitive information such as credentials, creation or editing returns 400.

```bash
curl -X POST "$BASE/api/v1/memory/items" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"kind":"preference","content":"Lead with the conclusion, then explain the reasoning","importance":3}'

curl "$BASE/api/v1/memory/items?status=pending&limit=50&offset=0" \
  -H "Authorization: Bearer $TOKEN"

curl -X POST "$BASE/api/v1/memory/items/item-1/confirm" \
  -H "Authorization: Bearer $TOKEN"
```

## Topics and Document Preferences

| Method | Path | Description |
| --- | --- | --- |
| GET | `/memory/topics` | Topics currently being tracked but not yet promoted; limit/offset same as items, returns data and total |
| POST | `/memory/topics/:id/promote` | Manually promote to long-term interest; returns `{success,data:MemoryItem}` |
| DELETE | `/memory/topics/:id` | Stop tracking the topic; returns success |
| GET | `/memory/documents` | Document preferences; limit/offset same as items, returns data and total |
| DELETE | `/memory/documents/:id` | Delete the preference record; returns success, does not delete the knowledge base document |

Topic fields include id/topic/aliases/hits/threshold/last_seen_at; Document fields include id/knowledge_id/knowledge_base_id/title/hits/last_used_at. Deletion uses the preference record id.

```bash
curl "$BASE/api/v1/memory/topics" -H "Authorization: Bearer $TOKEN"
curl -X POST "$BASE/api/v1/memory/topics/topic-1/promote" \
  -H "Authorization: Bearer $TOKEN"
curl -X DELETE "$BASE/api/v1/memory/documents/affinity-1" \
  -H "Authorization: Bearer $TOKEN"
```

## Export and Immediate Consolidation

`GET /memory/export` returns `{success,total,truncated,data}` with the download filename `weknora-memories.json`. A maximum of 20,000 items are exported; check truncated when the limit is reached.

`POST /memory/consolidate` returns `{success,data:{merged,demoted,expired,reviewed,candidates,skipped?}}`, immediately merging near-synonymous items and archiving expired items. When there are no changes, skipped explains the reason.

```bash
curl "$BASE/api/v1/memory/export" -H "Authorization: Bearer $TOKEN" \
  -o weknora-memories.json
curl -X POST "$BASE/api/v1/memory/consolidate" -H "Authorization: Bearer $TOKEN"
```

Returns 400 when parameters are invalid, the content is sensitive information, or memory is not enabled; returns 404 when no item for the current identity is found; returns 409 on confirmation conflicts; returns 401/403 when authentication/permissions are not satisfied. Personal endpoints do not accept a subject parameter. Implementation: `internal/handler/memory.go`, `internal/router/routes_memory.go`.

## Managing Memory for Identities Without an Account

The following endpoints use the Bearer login credentials of an Admin/Owner of the current workspace; API Keys cannot access them. Even when the tenant RBAC feature flag is disabled, the administrator role check remains in effect. Administrators can only manage identities in the workspace of the current caller and cannot switch tenants via request parameters. When memory is disabled for the space or identity, administrators can still view, clear, and adjust the toggle.

The `subject_id` in the path is the memory space UUID (`id`) returned by the list, not the `subject_id` storage identifier such as `web_user:...` or `im_user:...`. Item IDs must also belong to the identity corresponding to that UUID; UUIDs/items that do not exist or belong to other workspaces return 404.

| Method | Path | Request / Response |
| --- | --- | --- |
| GET | `/admin/memory/subjects` | Optional limit, offset; `{success,data:[MemorySubjectSummary],total}` |
| GET | `/admin/memory/subjects/:subject_id/items` | Optional status, limit, offset; `{success,data:[MemoryItem],total}`, supports viewing pending |
| POST | `/admin/memory/subjects/:subject_id/items/:id/confirm` | 200 `{success,data:MemoryItem}`; returns 409 for invalid or expired inferences |
| POST | `/admin/memory/subjects/:subject_id/items/:id/reject` | 200 `{"success":true}`, rejects a pending inference |
| DELETE | `/admin/memory/subjects/:subject_id/items` | 200 `{success,removed}`, clears the identity's memory and derived materials |
| PUT | `/admin/memory/subjects/:subject_id/settings` | `{"enabled":false}`, enabled is required; 200 `{"success":true}` |

Pagination and status values follow the personal items endpoints. `MemorySubjectSummary` includes `id`, `subject_id`, `principal_type`, `enabled`, `item_count`, `active_count`, `pending_count`, `last_activity_at`. Counts are computed from current items; the list does not return memory body text, resident profile blocks, background task queues, or extraction status.

```bash
curl "$BASE/api/v1/admin/memory/subjects?limit=50" \
  -H "Authorization: Bearer $TOKEN"

# Replace SUBJECT_UUID with the id returned in the previous step.
curl "$BASE/api/v1/admin/memory/subjects/$SUBJECT_UUID/items?status=pending" \
  -H "Authorization: Bearer $TOKEN"

curl -X PUT "$BASE/api/v1/admin/memory/subjects/$SUBJECT_UUID/settings" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"enabled":false}'
```

Administrator viewing of a specified identity's items and successful confirmations, rejections, clears, and toggle changes are written to the existing audit log, with event names beginning with `memory.`; the log records the operator, target UUID, and operation metadata, and does not copy memory content. Audit storage failures are recorded in the service log and do not cause completed operations to fail.

`max_items` separately limits the number of active and pending items per identity (default 200 each, configured maximum 2000). When writing a new inference, the oldest pending items over the limit are archived within the same identity transaction; active facts are not evicted due to pending items exceeding the limit. After lowering the configuration, existing pending items converge to the limit on the next write; there is no additional scheduled cleanup process.

Clearing deletes memory and derived materials, switches the write generation, and advances the extraction history watermark to the time of clearing, all within the same principal transaction. Memory extraction, consolidation, and profile rebuilding that have already started cannot write back under the old generation; subsequent new messages in the same session will also not re-extract history from before the clear, nor use that history as prior context for the extraction model. Existing newer cursors remain monotonically advancing.

Clearing preserves the identity and its enabled setting, does not delete chat messages, and does not retract content already sent to the model or client. New memory operations started after clearing can still write normally; to continuously stop automatic extraction, first set the identity's enabled to false, then perform the clear. Channel- or agent-level extraction policy inheritance is outside the scope of these endpoints.
