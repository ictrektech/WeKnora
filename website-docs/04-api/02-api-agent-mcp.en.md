# API Reference: Agents, MCP, and Skills

Manage agents, MCP services and their credentials, skills, and resource favorites. An agent's tool scope and invocation approval configuration are maintained through this group of endpoints.

## Agent (/api/v1/agents)

Read: Viewer+ (API key `read_agents`/`manage_agents`/`chat`/full); write: creator OR Admin+ (API key `manage_agents`/full); built-in agents (`is_builtin=true`) are always Admin+.

### GET /api/v1/agents/placeholders

Purpose: prompt placeholder definitions (must be registered before `/:id`). Permission: Viewer+.

Response: 200 `{"success":true,"data":{"all":{...},"system_prompt":{...},"agent_system_prompt":{...},"context_template":{...},"rewrite_system_prompt":{...},"rewrite_prompt":{...},"fallback_prompt":{...}}}`

```bash
curl $BASE/api/v1/agents/placeholders -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/agents/type-presets

Purpose: intelligent reasoning agent type presets (rag-qa / wiki-qa / hybrid / custom, etc.). Permission: Viewer+.

Response: 200 `{"success":true,"data":[{type,system_prompt,allowed_tools,kb_compatibility}]}`

```bash
curl $BASE/api/v1/agents/type-presets -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/agents/tools

Purpose: retrieve the catalog of built-in tools available to agents. Permission: Viewer+. Response `{"success":true,"data":{"tools":[...],"default_tools":[...],"switch_managed_tools":[...]}}`; each tool carries `requirement.any_of` / `all_of` knowledge base capability requirements, which the editor uses to determine whether it is selectable.

When creating or updating, an unknown tool name returns 400; already-stored unknown entries are retained, and the top-level `allowed_tools_warnings.kept_unknown` lists these names. Tools managed by independent switches and legacy skill tools with no successor are removed from the list, and `allowed_tools_warnings.dropped` returns the removed entries. Legacy retrieval tool names are mapped to successor tools, and duplicates are deduplicated.

### POST /api/v1/agents

Purpose: create a custom agent. Permission: Contributor+.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | Yes (`binding:"required"`) | Name |
| `description` | string | No | Description |
| `avatar` | string | No | Avatar/emoji |
| `config` | object | No | Agent configuration (`types.CustomAgentConfig`, see below) |

Main `config` fields: `agent_mode` (`quick-answer`/`smart-reasoning`, defaults to `smart-reasoning` when omitted), `agent_type` (`rag-qa/wiki-qa/hybrid-rag-wiki/data-analysis/custom`), `system_prompt`, `model_id`, `temperature` (0-2, invalid returns code 2103), `max_iterations` (1-20, invalid returns code 2102), `allowed_tools` (unset/null uses default tools; explicit `[]` selects no built-in tools; web search, memory, MCP, and sandbox are registered via independent configuration), `mcp_selection_mode`/`mcp_services`, `skills_selection_mode`, `kb_selection_mode`/`knowledge_bases`, `web_search_enabled`, `question_suggestions`, etc. (see `internal/types/custom_agent.go` for the full definition).

Response: 201 `{"success":true,"data":{id,name,description,avatar,is_builtin,created_by,config,creator_name,...}}`

```bash
curl -X POST $BASE/api/v1/agents -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"After-sales Assistant","config":{"agent_mode":"quick-answer","kb_selection_mode":"selected","knowledge_bases":["kb-1"]}}'
```

### GET /api/v1/agents

Purpose: agent list (including built-in). Permission: Viewer+. Query parameter: `creator` (`mine`/`others`, optional).

Response: 200 `{"success":true,"data":[Agent],"disabled_own_agent_ids":[...]}`

```bash
curl $BASE/api/v1/agents -H "X-API-Key: $API_KEY"
```

### GET /api/v1/agents/:id

Purpose: agent details. Permission: Viewer+.

Response: 200 `{"success":true,"data":{Agent}}`

```bash
curl $BASE/api/v1/agents/agent-1 -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/agents/:id

Purpose: update an agent. Permission: creator OR Admin+. Request body: `name/description/avatar/config`. `name` is required; omitting `avatar` keeps the original avatar; `config` is replaced as a whole, and omitted configuration items do not retain their original values—use `PATCH` to change only individual settings. When the agent has been shared to the organization, any knowledge base newly added to the knowledge base scope must be one the caller can share (knowledge base creator or Admin+; changing to `all` requires Admin+), otherwise 403.

Response: 200 `{"success":true,"data":{Agent}}`

```bash
curl -X PUT $BASE/api/v1/agents/agent-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"Customer Service Assistant","description":"Updated description","config":{...}}'
```

### PATCH /api/v1/agents/:id

Purpose: partially update an agent, modifying only the fields present in the request while all other configuration (including the avatar) remains unchanged. Permission is the same as `PUT`: creator OR Admin+; API key `manage_agents`/full. Reuses `PUT` validation (including the organization-shared knowledge base scope rule above), and additionally validates skill names.

Request body fields (all optional):

| Field | Type | Description |
|------|------|------|
| `name` | string | Name, cannot be blank |
| `description` | string | Description |
| `agent_mode` | string | `quick-answer` / `smart-reasoning`. MCP and skills only run under `smart-reasoning`; switching to this mode enables multi-turn conversation |
| `system_prompt` | string | Full system prompt (replaced as a whole) |
| `model_id` | string | Chat model ID |
| `kb_selection_mode` | string | `all` / `selected` / `none` |
| `knowledge_bases` | string[] | Selected knowledge base IDs, replaced as a whole; if `kb_selection_mode` is not also passed, the mode switches to `selected` |
| `mcp_selection_mode` | string | `all` / `selected` / `none` |
| `mcp_services` | string[] | Selected MCP service IDs, replaced as a whole; if `mcp_selection_mode` is not also passed, the mode switches to `selected` |
| `skills_selection_mode` | string | `all` / `selected` / `none`; `all` and `none` clear the selected skill names |
| `selected_skills` | string[] | Up to 50 skill names (not IDs), replaced as a whole; if `skills_selection_mode` is not also passed, the mode switches to `selected` |
| `web_search_enabled` | bool | Whether to enable web search |
| `multi_turn_enabled` | bool | Whether to enable multi-turn conversation |

Skill names come from `GET /api/v1/skills?sandbox_config_id=<agent.config.sandbox_config_id>` and only take effect in `smart-reasoning` mode. When the mode is `selected` after PATCH, all names must be skills currently available in that workspace and that sandbox; unavailable or unknown names return 400 and list the names. When no sandbox is bound, setting skills to `all` or `selected` via PATCH (including passing only `selected_skills`) returns 400; a sandbox must first be selected in the agent editor.

Response: 200 `{"success":true,"data":{Agent}}`; 400 when `agent_mode` or a selection mode has an invalid value, the name is empty, the skill list exceeds 50 items, or the above skill validation fails; 404 when the agent does not exist.

```bash
curl -X PATCH $BASE/api/v1/agents/agent-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"model_id":"model-2","knowledge_bases":["kb-1","kb-2"]}'
```

### DELETE /api/v1/agents/:id

Purpose: delete an agent. Permission: creator OR Admin+.

Response: 200 `{"success":true,"message":"Agent deleted successfully"}`

```bash
curl -X DELETE $BASE/api/v1/agents/agent-1 -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/agents/:id/copy

Purpose: copy an agent (the copy belongs to the caller). Permission: Contributor+. No request body.

Response: 201 `{"success":true,"data":{new Agent}}`

```bash
curl -X POST $BASE/api/v1/agents/agent-1/copy -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/agents/:id/suggested-questions

Purpose: agent starter suggested questions (registered outside the group to avoid conflicting with `/agents/:id/shares`). Permission: Viewer+; API key `read_agents`/`manage_agents`/`chat`/full.

| Query parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `knowledge_base_ids` | string | No | Comma-separated KBs |
| `knowledge_ids` | string | No | Comma-separated knowledge IDs |
| `tag_scopes` | string | No | Tag scopes as a JSON array |
| `limit` | int | No | Maximum 30 |

Response: 200 `{"success":true,"data":{"questions":[{question,source,knowledge_base_id}]}}`

```bash
curl "$BASE/api/v1/agents/agent-1/suggested-questions?limit=6" -H "X-API-Key: $API_KEY"
```

## MCP Services (/api/v1/mcp-services)

Space-level external tool service integration. Read: Viewer+; write/test/approval policy: Admin+. API key: `manage_mcp_services`/full. Handler: `internal/handler/mcp_service.go`

### POST /api/v1/mcp-services

Purpose: create an MCP service. Permission: Admin+.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | Yes | Name |
| `description` | string | No | Legacy description, retained for compatibility; the management UI uniformly edits `usage_instructions` |
| `usage_instructions` | string | No | Service purpose, applicable scenarios, and key constraints, based on which the model determines when to use the service; the management UI requires this in the second step |
| `enabled` | bool | No | Enabled |
| `transport_type` | string | Yes | `sse` / `http-streamable`; `stdio` is rejected for security reasons |
| `url` | *string | No | Service URL (SSE/HTTP) |
| `headers` | map[string]string | No | HTTP headers |
| `auth_config` | object | No | `auth_type` (`api_key/bearer/oauth`), `api_key_header`, `custom_headers`, `scopes`, `auth_server_metadata_url`, `client_id` (OAuth pre-registered client; leave empty for dynamic client registration; empty string clears it) (secrets and `client_secret` go through the credentials sub-resource and can be submitted with the request body at creation time) |
| `advanced_config` | object | No | `{timeout,retry_count,retry_delay}`, defaults to 30 seconds / 3 times / 1 second; when `timeout` is greater than 60 seconds, it also extends the wait window for a single agent invocation of that service's tools |
| `stdio_config` / `env_vars` | object | No | Retained only for compatibility with legacy data; stdio is disabled and has no effect |

Response: 200 `{"success":true,"data":{MCPServiceResponse}}` (includes `credentials:{api_key:{configured},token:{configured},client_secret:{configured}}`; services with a synced tool catalog also carry `catalog:{tool_count,stale,synced_at}`)

```bash
curl -X POST $BASE/api/v1/mcp-services -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"github","transport_type":"sse","url":"https://mcp.example.com/sse"}'
```

### GET /api/v1/mcp-services

Purpose: MCP service list. Permission: Viewer+. Response: 200 `{"success":true,"data":[MCPServiceResponse]}`

| Query parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `agent_id` | string | No | When passed together with `agent_source_tenant_id`, lists the MCP services that the shared agent can @-mention |
| `agent_source_tenant_id` | int | No | The source space ID of the shared agent, consistent with the parameter of the same name in chat requests |

When both parameters are passed, the shared agent path is used: only services that the agent specified in `selected` mode in the source space and that are enabled are returned (`all`/`none` modes return an empty list), and each item contains only the ID, name, description, usage instructions, transport type, enabled status, and tool catalog summary, without connection details such as URL, headers, or auth configuration; if the caller is not authorized to use the agent, 403 is returned. When only one or neither is passed, the caller's own space services are listed.

```bash
curl $BASE/api/v1/mcp-services -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/mcp-services/:id

Purpose: details. Permission: Viewer+. Response: 200 `{"success":true,"data":{MCPServiceResponse}}`

```bash
curl $BASE/api/v1/mcp-services/mcp-1 -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/mcp-services/:id

Purpose: partial update (map semantics; `auth_config` must not carry api_key/token). Permission: Admin+. Fields are the same as creation (all optional).

When submitting `usage_instructions`, it must be a string that is non-empty after trimming leading and trailing whitespace, with a maximum length of 16000 characters. The field may be omitted when only modifying the connection or enabled status, in which case the original value remains unchanged.

Response: 200 `{"success":true,"data":{MCPServiceResponse}}`

```bash
curl -X PUT $BASE/api/v1/mcp-services/mcp-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"enabled":false}'
```

### POST /api/v1/mcp-services/:id/usage-instructions/generate

Purpose: generate concise usage instructions based on a synced, non-expired MCP tool catalog. Permission: Admin+; API key requires `manage_mcp_services` or full.

Request: `{"language":"zh-CN"}`. Supports `zh-CN`, `en-US`, `ja-JP`, `ko-KR`, `ru-RU`; defaults to Chinese.

Prefers the space's default available chat model, otherwise uses the first available chat model. Input includes the service name, server-side description, and the names and descriptions of enabled tools; OAuth catalogs use the current user's authorization scope. It does not connect to MCP, invoke tools, or automatically save the generated result.

Response: 200 `{"success":true,"data":{"usage_instructions":"Query remote logs by module and time range; when a query ID already exists, read the corresponding logs."}}`. The generation target is a brief 2–3 sentence description, up to 500 characters; users can edit it and save via PUT. Returns 400 when the catalog is not synced, is expired, has no enabled tools, or no chat model is available.

### DELETE /api/v1/mcp-services/:id

Purpose: delete. Permission: Admin+. Response: 200 `{"success":true,"message":"MCP service deleted successfully"}`

```bash
curl -X DELETE $BASE/api/v1/mcp-services/mcp-1 -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/mcp-services/:id/test

Purpose: connection test (probes the external service). Permission: Admin+. Response: 200 `{"success":true,"data":{"success","message","oauth_required","tools":[...],"resources":[...]}}`

```bash
curl -X POST $BASE/api/v1/mcp-services/mcp-1/test -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/mcp-services/:id/metadata

Purpose: read the persisted tool catalog without connecting upstream. Permission: Viewer+; OAuth catalogs are isolated by the currently effective authorization subject.

Response: 200 `{"success":true,"data":null}` indicates not synced; when synced, `data` is a catalog snapshot containing server information, instructions, tools, and sync time. Snapshots after connection configuration changes are marked `stale:true` and cannot be used to load runtime tools.

```bash
curl $BASE/api/v1/mcp-services/mcp-1/metadata -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/mcp-services/:id/metadata/refresh

Purpose: explicitly connect upstream, fully fetch, and atomically update the tool catalog. Static auth catalogs require Admin+; OAuth users can sync their own catalog (Viewer+). API Key requires MCP management capability.

The response is the updated catalog snapshot. On failure, the original snapshot is retained; if the connection changes during refresh, 409 is returned; if the catalog is invalid/too large or upstream sync fails, 400 is returned; if the metadata store is unavailable, 503 is returned. Manual usage instructions and per-tool enable/approval policies are not overwritten.

```bash
curl -X POST $BASE/api/v1/mcp-services/mcp-1/metadata/refresh -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/mcp-services/:id/tools

Purpose: tool list. Permission: Viewer+. Response: 200 `{"success":true,"data":[{name,description,inputSchema,require_approval}]}`

```bash
curl $BASE/api/v1/mcp-services/mcp-1/tools -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/mcp-services/:id/resources

Purpose: resource list. Permission: Viewer+. Response: 200 `{"success":true,"data":[{uri,name,description,mimeType}]}`

```bash
curl $BASE/api/v1/mcp-services/mcp-1/resources -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/mcp-services/:id/credentials

Purpose: set secrets (`api_key`/`token`/`client_secret`, pointer fields; omitted values are retained). Permission: Admin+. Handler: `internal/handler/mcp_credentials.go`

Response: 200 `{"success":true,"data":{"fields":{"api_key":{"configured"},"token":{"configured"},"client_secret":{"configured"}}}}`

```bash
curl -X PUT $BASE/api/v1/mcp-services/mcp-1/credentials -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"token":"ghp_..."}'
```

### DELETE /api/v1/mcp-services/:id/credentials/:field

Purpose: delete a credential field (`api_key`, `token`, or `client_secret`). Permission: Admin+. Response: 204.

```bash
curl -X DELETE $BASE/api/v1/mcp-services/mcp-1/credentials/token -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/mcp-services/:id/tool-approvals

Purpose: list of tool enable/disable and manual approval policies. Permission: Viewer+. Response: 200 `{"success":true,"data":[{service_id,tool_name,require_approval,enabled,...}]}`

```bash
curl $BASE/api/v1/mcp-services/mcp-1/tool-approvals -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/mcp-services/:id/tool-approvals/:tool_name

Purpose: update a tool's enabled (enable/disable) and require_approval (manual approval). Permission: Admin+. At least one of the two must be provided; omitted fields retain their original values; with no record, the tool is enabled by default and does not require approval.

Response: 200 `{"success":true}`

```bash
curl -X PUT $BASE/api/v1/mcp-services/mcp-1/tool-approvals/create_issue \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"require_approval":true}'
```

## MCP OAuth

Handler: `internal/handler/mcp_oauth.go`

### GET /api/v1/mcp-oauth/callback

Purpose: third-party OAuth authorization callback (unauthenticated; authenticated via a single-use `state` parameter; registered outside the `/mcp-services` group). Query parameters: `code`, `state`, `error`.

Response: 302 redirect to the frontend (success `#mcp_oauth_result=success`, failure `#mcp_oauth_error=<code>`).

```bash
curl -i "$BASE/api/v1/mcp-oauth/callback?code=xxx&state=yyy"
```

### POST /api/v1/mcp-services/:id/oauth/authorize-url

Purpose: generate a user-level authorization URL. Permission: Viewer+.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `redirect_uri` | string | Yes | Backend callback URL (absolute address) |
| `frontend_redirect` | string | No | Frontend redirect after callback (default `/`) |

Response: 200 `{"success":true,"data":{"authorization_url","authorization_attempt"}}`

```bash
curl -X POST $BASE/api/v1/mcp-services/mcp-1/oauth/authorize-url -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"redirect_uri":"'$BASE'/api/v1/mcp-oauth/callback"}'
```

### POST /api/v1/mcp-services/:id/oauth/assertion

Purpose: obtain an OAuth token for oneself via JWT Bearer authorization (RFC 7523), without a browser redirect. Suitable for scenarios where WeKnora is deployed behind another application that handles login and calls WeKnora on behalf of the user: that application issues an assertion representing the current user (such as the ID-JAG used by MCP Enterprise-Managed Authorization), and WeKnora submits it as-is to the authorization server, which validates it. Uses the OAuth client already registered for the service (no dynamic client registration is performed; returns 400 if no client exists yet). The obtained token is the same as the token saved by the authorization callback, and subsequent refresh, status query, and revocation remain unchanged. Permission: Viewer+.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `assertion` | string | Yes | JWT assertion submitted to the authorization server (request body limit 16 KiB) |

Response: 200, with `data` the same as `oauth/status`. When the authorization server rejects the assertion (4xx, or a 2xx response body containing an OAuth `error`), returns 400 with the OAuth error code; when the authorization server returns 5xx or a network error occurs, returns 500.

```bash
curl -X POST $BASE/api/v1/mcp-services/mcp-1/oauth/assertion -H "X-API-Key: $API_KEY" \
  -H "X-External-User-ID: user-1" -H 'Content-Type: application/json' -d '{"assertion":"'$ASSERTION'"}'
```

### GET /api/v1/mcp-services/:id/oauth/status

Purpose: query one's own authorization status. Permission: Viewer+. Query parameter: `authorization_attempt` (optional).

Response: 200 `{"success":true,"data":{"authorized","state":"authorized|pending","refresh_available","expires_at"}}`

```bash
curl $BASE/api/v1/mcp-services/mcp-1/oauth/status -H "Authorization: Bearer $TOKEN"
```

### DELETE /api/v1/mcp-services/:id/oauth/token

Purpose: revoke one's own OAuth token. Permission: Viewer+. Response: 204.

```bash
curl -X DELETE $BASE/api/v1/mcp-services/mcp-1/oauth/token -H "Authorization: Bearer $TOKEN"
```

## MCP Server Endpoints (/api/v1/mcp-endpoints) {#mcp-server-endpoints}

Manage the MCP endpoints published externally by the current space; external MCP clients connect to `/mcp/:endpoint_id`. Read: Viewer+; write: Admin+. API key: `manage_channels`/full. Handler: `internal/handler/mcp_endpoint.go`. For purpose and tool descriptions, see [MCP Integration](../03-features/08-mcp.md#供外部客户端调用).

| Method | Path | Description |
| --- | --- | --- |
| GET | `/mcp-endpoints` | Endpoint list (without tokens) |
| GET | `/mcp-endpoints/tools` | Tool catalog: `{groups,tools:[{name,group,destructive}],default_tools}` |
| POST | `/mcp-endpoints` | Create; 201, response includes a one-time `token` |
| GET | `/mcp-endpoints/:endpoint_id` | Details |
| PUT | `/mcp-endpoints/:endpoint_id` | Partial update; omitted fields retain their original values |
| DELETE | `/mcp-endpoints/:endpoint_id` | Delete; clients using the endpoint are immediately invalidated |
| POST | `/mcp-endpoints/:endpoint_id/rotate-token` | Rotate the token; response includes the new `token`, and the old token is immediately invalidated |

Request fields (same for create and update, all optional):

| Field | Type | Description |
| --- | --- | --- |
| `name` | string | Name, required on creation |
| `description` | string | Description |
| `enabled` | bool | Defaults to true; when disabled, connections return 403 |
| `knowledge_base_ids` | string[] | Accessible knowledge bases; an empty array means all in the space |
| `tools` | string[] | Exposed tools, at least one; if omitted on creation, `default_tools` (all read-only tools) is used |
| `default_agent_id` | string | Agent used by `ask`; empty means the built-in quick answer; internal built-in agents are not selectable |
| `rate_limit_per_minute` | int | Maximum tool calls per minute; 0 or omitted means 60, maximum 6000 |

Response `data` is `{id,tenant_id,name,description,enabled,token_hint,knowledge_base_ids,tools,default_agent_id,rate_limit_per_minute,path,last_used_at,created_at,updated_at}`, with an additional `token` on creation and rotation. When called with a restricted API Key, the capabilities required by the endpoint's knowledge bases and tools must not exceed the scope of that Key, otherwise 403.

```bash
curl -X POST $BASE/api/v1/mcp-endpoints -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Product Docs Assistant","knowledge_base_ids":["kb-1"],"tools":["search_knowledge","read_document","ask"]}'
```

## Agent Runtime Interaction (/api/v1/agent)

Manual approval and OAuth resumption during conversations; permissions are all Viewer+ (only the person who initiated the session has the context), and API keys are denied by default.

### POST /api/v1/agent/tool-approvals/:pending_id

Purpose: adjudicate a pending tool invocation approval. Handler: `ResolveToolApproval` in `internal/handler/mcp_service.go`.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `decision` | string | Yes (`binding:"required"`) | `approve` / `reject` |
| `modified_args` | JSON | No | Modified tool arguments |
| `reason` | string | No | Reason |

Response: 200 `{"success":true}`

```bash
curl -X POST $BASE/api/v1/agent/tool-approvals/p-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"decision":"approve"}'
```

### POST /api/v1/agent/mcp-oauth-resolutions/:pending_id

Purpose: resume an agent run paused due to MCP OAuth. Handler: `internal/handler/mcp_oauth.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `service_id` | string | Yes (`binding:"required"`) | MCP service ID |
| `decision` | string | No | `authorize` (default) / `cancel` |

Response: 200 `{"success":true}`

```bash
curl -X POST $BASE/api/v1/agent/mcp-oauth-resolutions/p-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"service_id":"mcp-1"}'
```

### POST /api/v1/agent/mcp-oauth-resolutions/:pending_id/cancel

Purpose: cancel a paused OAuth flow. No request body.

Response: 200 `{"success":true}`

```bash
curl -X POST $BASE/api/v1/agent/mcp-oauth-resolutions/p-1/cancel -H "Authorization: Bearer $TOKEN"
```

## Skills, Sandbox, and Personal Variables

`GET /api/v1/skills?sandbox_config_id=...` returns the names/descriptions of skills available under the specified configuration and skills_available; when `agent_id` + `agent_source_tenant_id` are passed, it returns results based on the shared agent's source space and its sandbox configuration. See [Sandbox and Skills API](02-api-sandbox-skills.md#沙箱内技能) for details. For the complete interfaces for catalog inclusion, installation, templates, progress, files, and personal variables, see [Sandbox and Skills API](02-api-sandbox-skills.md).

The agent config adds `sandbox_config_id`; together with skills_selection_mode and selected_skills, it determines the available skills. Shell/file tools are registered according to backend capabilities; the legacy read_skill / execute_skill_script are no longer registered.

## Long-Term Memory

When the agent config's `memory_enabled` is nil, it inherits from the space; false disables memory read/write for this agent. For personal management, topic/document preferences, export, and immediate organization, see [Long-Term Memory API](02-api-memory.md); for usage steps, see [Cross-Session Long-Term Memory](../03-features/23-memory.md).

## User Favorites (/api/v1/user/favorites)

Stored per user (not per resource creator); permissions are all Viewer+, JWT only (API keys are denied by default). Handler: `internal/handler/user_resource_favorite.go`

### GET /api/v1/user/favorites

Purpose: favorites list. Query parameter: `type` (required, `kb` or `agent`).

Response: 200 `{"success":true,"data":[{type,id,created_at}]}`

```bash
curl "$BASE/api/v1/user/favorites?type=kb" -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/user/favorites

Purpose: add a favorite. Request body: `{"type":"kb|agent","id":"<resource ID>"}` (both required).

Response: 200 `{"success":true}`

```bash
curl -X POST $BASE/api/v1/user/favorites -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"type":"kb","id":"kb-1"}'
```

### DELETE /api/v1/user/favorites/:type/:id

Purpose: remove a favorite. Path parameters: `type`, `id`.

Response: 200 `{"success":true}`

```bash
curl -X DELETE $BASE/api/v1/user/favorites/kb/kb-1 -H "Authorization: Bearer $TOKEN"
```

## Implementation Reference

Route registration: invoked by `internal/router/router.go`; `RegisterCustomAgentRoutes`, `RegisterSkillRoutes`, and `RegisterUserFavoriteRoutes` are defined in `routes_agent.go`; `RegisterMCPServiceRoutes` (including MCP OAuth and `/agent` runtime interaction) is in `routes_infra.go`; `RegisterMCPEndpointRoutes` and the public `/mcp/:endpoint_id` are in `routes_mcp_endpoint.go`. Handlers: `internal/handler/custom_agent.go`, `internal/handler/mcp_service.go`, `internal/handler/mcp_credentials.go`, `internal/handler/mcp_oauth.go`, `internal/handler/mcp_endpoint.go`, `internal/handler/skill_handler.go`, `internal/handler/user_resource_favorite.go`.
