# VOS API contracts, keys and lifecycle

## Entry and complete field reference

Open **Settings → API documentation** for the existing full chapter library, then **Full contract / 完整字段** for searchable native operation parameters, body types, response schemas and shared definitions. Production disables `/swagger` when `GIN_MODE=release`; the shipped documentation uses the version-matched schema bundled at build time instead of requiring an administrator to enable development mode. The catalogue below includes every operation declared in that native Swagger specification; application policy and live routes remain authoritative for newly introduced or intentionally open-ended integration payloads. Do not treat a generic provider/tool payload as a new universally fixed schema.

VOS base is normally `https://<VOS_HOST>:<VOS_PORT>/app/com.ictrek.hybrag/api/v1`. Direct API default mapping, when installed, is `http://<HOST>:29081/api/v1`, frontend29080; actual installation parameters can change or omit these mappings. Backend native listen8080 is not a universal externally reachable port. The API Integration settings show the correct installed address. Never use a developer SSH alias as an API hostname.

## User identity and access scope

External callers may use `X-API-Key: $HYBRAG_KEY` or an authorized VOS/OAuth user token in `Authorization: Bearer $VOS_ACCESS_TOKEN`. The app verifies/maps the user and applies membership, capability and knowledge-base scope. OAuth password grant is not supported: use the existing authorization-code/Fastpath flow or the authorized VOS login endpoint for command-line access. Do not put passwords, tokens or returned secrets in logs.

A personal Key belongs to the user, not to all workspace data. Regular users may select `retrieve`, `chat`, `ingest`, with allowed knowledge-base IDs; they cannot obtain full workspace/platform permissions merely by owning a personal space. Platform administrators with `CanAccessAllTenants=true` may create workspace-level keys. API Key routes are deny-by-default unless an explicit policy grants them; a full-access workspace Key is not an unrestricted platform administrator. System-scoped keys and MCP endpoint tokens have separate scopes, endpoints and permission requirements.

HybRAG Keys are not V-Scheduler inference keys or upstream vendor credentials. Configuring an upstream once in Scheduler does not remove HybRAG's own KB/session authorization.

## Creating, editing and revoking user/workspace Keys

Use the current authorized user session/VOS Bearer for management. Path `id` is the numeric workspace ID; `key_id` is the numeric key record ID, not the secret. Do not copy another tenant's ID.

| Method/path | Request | Success |
| --- | --- | --- |
| GET `/tenants/{id}/api-keys` | Current user and workspace scope | 200 `{success:true,data:KeyRecord[]}` |
| POST `/tenants/{id}/api-keys` | JSON `name:string,full_access:boolean,knowledge_base_ids:string[],capabilities:string[],expires_at_unix?:integer` | 201 `{success:true,data:KeyRecord & {token:string}}` |
| PUT `/tenants/{id}/api-keys/{key_id}` | Same configurable fields as creation | 200 `{success:true,data:KeyRecord}` |
| DELETE `/tenants/{id}/api-keys/{key_id}` | No body | 200 `{success:true}` (not204) |

KeyRecord fields: `id:integer,scope_type:string,owner_user_id?:string,name:string,api_key:string,full_access:boolean,knowledge_base_ids:string[],capabilities:string[],last_used_at?:date-time,expires_at?:date-time,created_at:date-time`. Creation returns the generated secret in both `data.token` and `data.api_key`; current authorized list/update responses may also return the stored `api_key` after decryption. **This app is not a one-time-secret-only API**. Protect all management responses; legacy records lacking a retained secret may show an empty value. Encryption at rest does not make the returned secret safe to log.

Creation requires a nonblank name, valid capabilities and, for personal data capabilities, a nonempty allowed KB scope. `expires_at_unix` is Unix **seconds**, must be future on creation; returned date-time fields use their serialized timestamp format. PUT is a full configuration update, not a partial patch: send the complete desired name/access/scope/capability settings; omitted boolean/arrays decode as false/empty and can change authorization. `full_access=true` clears fine-grained capabilities and KB scope and requires eligible administrator authority. PUT changes configuration, not the token value. Rotate by creating a replacement and revoking the old Key. DELETE marks revocation, so it no longer authenticates; it is not a model-unload operation.

## Retrieval, ingestion and conversation lifecycle

For read-only integration, create `retrieve`/`chat` within selected KBs, call `/auth/me` to verify the effective principal, then list visible `/knowledge-bases?creator=all`. Use `knowledge_base_ids` for KBs and `knowledge_ids` for document IDs; do not substitute `document_ids`. `POST /sessions` returns `data.id`; reuse that session for a multi-turn topic and create another for a new topic. `POST /knowledge-chat/{session_id}` accepts JSON `query`, optional `knowledge_base_ids`, `knowledge_ids`, and appropriate channel. Its SSE `data` frames are native `StreamResponse` records; `response_type:"complete"` ends the turn. Errors can occur after HTTP200, so inspect stream records and persist successful final state.

Sessionless `/knowledge-search` retrieves within the allowed scope. Upload/import/manual knowledge creation returns a knowledge/document ID, not proof that parsing/indexing has completed. Poll the matching knowledge/task status. A duplicate upload/URL can return409 with an existing Knowledge in data; do not blindly enqueue duplicate ingestion after a timeout. Stop/delete operations have distinct ownership and side effects; deleting a document is not a generic way to cancel a conversation or unload Scheduler weights. Use the documented task/conversation operations for their actual scope.

```bash
BASE='https://<VOS_HOST>:<VOS_PORT>/app/com.ictrek.hybrag/api/v1'
curl --fail-with-body -H "X-API-Key: $HYBRAG_KEY" "$BASE/auth/me"
curl --fail-with-body -H "X-API-Key: $HYBRAG_KEY" -H 'Content-Type: application/json' \
  -d '{"query":"Find relevant records","knowledge_base_ids":["<kb_id>"]}' "$BASE/knowledge-search"
```

## Payloads, files, pagination and errors

Most JSON success responses use `{success:true,data:...}`, with per-endpoint `total/page/page_size`. Some system endpoints return raw rows/arrays or `{code:0,msg:"success",data:...}`; media, export, SSE and MCP routes have their own content types. Read the operation schema and Content-Type, not one assumed universal envelope. Cursor pagination (audit/runtime/wiki) differs from page pagination; use each response's next_cursor/has_more fields.

Application errors commonly use `{success:false,error:{code,message,details}}`; middleware authentication/RBAC can return `{error:string,code?:string}` and proxy failures can be non-JSON. Permission403 can be tenant, KB scope, route policy or expired/revoked Key; do not “fix” it by selecting an unrelated model. Retry bounded transient failures after restoring dependency/capacity, not permanent validation/auth errors. Queries and uploads have different timeout/idempotency behavior; inspect existing task state before retrying a write.

`/r/:token` and signed file URLs grant only their encoded resource, with expiry; they do not grant broad API access. MCP endpoints use their own issued/rotatable endpoint token and selected tools/KB scope. Admin-only diagnostics and native desktop/dialog actions are not generally callable by scoped API Keys. The existing chapter library documents these specific flows. Regional network restrictions can block foreign API providers even while Scheduler/HybRAG local discovery succeeds; verify upstream connectivity separately from registration and user authorization.
