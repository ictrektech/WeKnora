# API Reference: Sessions, Messages, and Chat

Create and manage sessions, read messages and temporary attachments, and obtain knowledge Q&A or agent answers via SSE.

Sessions are "user-private" resources; ownership is enforced inside the handler; the routing layer requires Viewer+. API keys: sessions/chat require the `chat` capability (or full-access); message search requires `message_history`; knowledge retrieval requires `retrieve`.


## Assistant Sessions, Permissions, and Background Tasks {#assistant-runtime}

Sessions with `kind=assistant` are used for the built-in assistant; assistant session history is managed separately from regular chat. Do not pass regular session IDs into the assistant resume entry point. Automation sessions are created by the system and associated with tasks.

Assistant chat requests use `assistant_access` (`read_only` / `confirm` / `auto_write`), subject to the space assistant upper limit; regular agents do not use this request field, and their configuration is controlled via `config.access_level`. Approval replies use `POST /api/v1/agent/prompts/:prompt_id`; querying and revoking available grants uses `/api/v1/agent/grants`. See [Execution Permissions](../03-features/28-agent-permissions.md).

| Method and Path | Purpose |
| --- | --- |
| `GET /api/v1/sessions/:id/background-work` | Query session background tasks |
| `GET /api/v1/sessions/:id/background-work/:work_id` | Query task status and output |
| `POST /api/v1/sessions/:session_id/background-work/:work_id/cancel` | Cancel the specified task |

The endpoints continue to check session access permissions. `work_id` is the background task identifier, not a terminal process ID, nor an automation run ID.

## Sessions (/api/v1/sessions)

### POST /api/v1/sessions

Purpose: Create a session. Handler: `internal/handler/session/handler.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `title` | string | No | Title |
| `kind` | string | No | Empty value (default) is a regular session; `assistant` is a WeKnora assistant session; other values return 400 |
| `description` | string | No | Description |
| `project_dir` | string | No | Desktop only: bind the session to a user-approved local project directory (absolute path, must exactly match the approved list), otherwise returns 400 |

Response: 201 `{"success":true,"data":{Session}}` (`id,kind,title,description,tenant_id,user_id,is_pinned,last_request_state,created_at,...`)

```bash
curl -X POST $BASE/api/v1/sessions -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' -d '{"title":"New Conversation"}'
```

### GET /api/v1/sessions

Purpose: List sessions. Handler: `internal/handler/session/handler.go`

| Query Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `page` / `page_size` | int | No | Pagination |
| `keyword` | string | No | Fuzzy title search |
| `kind` | string | No | Omitted or empty returns only regular sessions, applicable to all sources; `assistant` returns only the caller's own assistant sessions (administrators cannot cross users either), sorted by `updated_at DESC`; other values return 400 |
| `source` | string | No | Source filter (web/embed/api/feishu/wechat/slack/...) |
| `agent_id` | string | No | Filter by Agent (IM sessions) |

Response: 200 `{"success":true,"data":[SessionListItem],"total","page","page_size"}`

```bash
curl "$BASE/api/v1/sessions?page=1" -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/sessions/:id

Purpose: Session details.

Response: 200 `{"success":true,"data":{Session}}`

```bash
curl $BASE/api/v1/sessions/s-1 -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/sessions/:id

Purpose: Update a session (title/description/pin). Request body: `title`, `description`, `is_pinned` (all optional).

Response: 200 `{"success":true,"data":{Session}}`

```bash
curl -X PUT $BASE/api/v1/sessions/s-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"title":"Rename"}'
```

### DELETE /api/v1/sessions/:id

Purpose: Delete a session.

Response: 200 `{"success":true,"message":"Session deleted successfully"}`

```bash
curl -X DELETE $BASE/api/v1/sessions/s-1 -H "Authorization: Bearer $TOKEN"
```

### DELETE /api/v1/sessions/batch

Purpose: Batch delete sessions. `delete_all:true` preserves assistant sessions; deleting by specified IDs still follows the original ownership rules. Request body: `{"ids":["s-1"],"delete_all":false}` (choose one: `ids` or `delete_all:true`).

Response: 200 `{"success":true,"message":"Sessions deleted successfully"}`

```bash
curl -X DELETE $BASE/api/v1/sessions/batch -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"ids":["s-1","s-2"]}'
```

### DELETE /api/v1/sessions/:id/messages

Purpose: Clear session messages.

Response: 200 `{"success":true,"message":"Session messages cleared successfully"}`

```bash
curl -X DELETE $BASE/api/v1/sessions/s-1/messages -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/sessions/:session_id/generate_title

Purpose: Generate a session title based on context messages. Handler: `internal/handler/session/title.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `messages` | []Message | Yes (`binding:"required"`) | Messages used as context |

Response: 200 `{"success":true,"data":"Generated title"}`

```bash
curl -X POST $BASE/api/v1/sessions/s-1/generate_title -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"messages":[{"role":"user","content":"Introduce the product"}]}'
```

### POST /api/v1/sessions/:session_id/stop

Purpose: Stop an answer that is being generated. Handler: `internal/handler/session/stream.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `message_id` | string | Yes (`binding:"required"`) | Assistant message ID |

Response: 200 `{"success":true,"message":"Generation stopped"}`

```bash
curl -X POST $BASE/api/v1/sessions/s-1/stop -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"message_id":"m-1"}'
```

When stopped, appended messages that have not yet been delivered to the agent (see [Appending Messages to a Running Answer](#steer) below) are discarded together and will not automatically start a new round.

### POST /api/v1/sessions/:session_id/fork

Purpose: Fork a new session from a historical message; the source session remains unchanged. Only the session owner can fork. Handler: `internal/handler/session/fork.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `message_id` | string | Yes | Fork point. User message: copies the history before it; the client typically pre-fills the question into the input box; assistant message: copies the history up to and including this answer |
| `title` | string | No | New session title, defaults to "Source Title (Branch)" |

Copied messages retain the original timeline and generated files; the new session records `parent_session_id` and `forked_from_message_id`. When the source session is bound to a sandbox, the system takes a snapshot of the sandbox, and the new session starts from the workspace state of the round corresponding to the fork point the first time it uses the sandbox. If the workspace cannot be carried over, the fork still succeeds but returns `degraded: true` and a reason:

| `reason` | Meaning |
| --- | --- |
| `NO_CHECKPOINT` | Rounds before the fork point have no workspace checkpoint |
| `SANDBOX_REPLACED` | The checkpoint belongs to an old sandbox that the session has since replaced |
| `SANDBOX_GONE` | The source session currently has no sandbox to snapshot |
| `SNAPSHOT_UNSUPPORTED` | The sandbox backend does not support snapshots or the snapshot failed |

Response: 200 `{"success":true,"data":{"session_id":"New session ID","degraded":false}}`. Returns 409 (`code: FORK_SOURCE_BUSY`) if the source session is generating or the fork point is an unfinished answer; returns 404 if the session or message does not exist; returns 400 if the fork point is not a user or assistant message.

```bash
curl -X POST $BASE/api/v1/sessions/s-1/fork -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"message_id":"m-3"}'
```

### POST /api/v1/sessions/:session_id/rewind

Purpose: Rewind the current session in place to a certain message. Only the session owner can rewind. Handler: `internal/handler/session/rewind.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `message_id` | string | Yes | Rewind point. User message: deletes it and all subsequent messages; assistant message: keeps this answer and deletes subsequent messages |

Generated files, follow-up suggestions, and chat history indexes of deleted messages are cleaned up as well. When the session is bound to a sandbox, the system also resets `/workspace` to the checkpoint of the last round in the retained history; if the workspace reset fails, the entire operation fails and messages are not deleted.

Response: 200 `{"success":true,"data":{"deleted_messages":N,"workspace_reset":true,"reason":""}}`. When `workspace_reset` is false, `reason` explains why only the conversation was rewound: `NO_SANDBOX` (session not bound to a sandbox) or `NO_CHECKPOINT` (retained history has no checkpoint).

| Status Code | `code` | Meaning |
| --- | --- | --- |
| 409 | `REWIND_SOURCE_BUSY` | The session is generating, or the rewind point is an unfinished answer |
| 409 | `REWIND_NO_CHECKPOINT` | The sandbox still exists, but no usable checkpoint can be found in the retained history; rejected to avoid files getting ahead of the conversation |
| 409 | `REWIND_SANDBOX_REPLACED` | The checkpoint belongs to a replaced old sandbox |
| 404 | — | Session or message does not exist |
| 500 | — | Workspace reset failed; can be retried |

```bash
curl -X POST $BASE/api/v1/sessions/s-1/rewind -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"message_id":"m-3"}'
```

### Appending Messages to a Running Answer (steer) {#steer}

During answer generation in intelligent reasoning mode, calling `agent-chat` again on the same session returns 409. In this case, the following endpoints can be used to queue new messages into the current round. Only the session owner can call them; quick Q&A has no execution loop to inject into and does not support appending. Handler: `internal/handler/session/steer.go`

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/api/v1/sessions/:session_id/steer` | Append a message |
| GET | `/api/v1/sessions/:id/steer` | List queued messages not yet delivered in the current round, used to restore the queue after a page refresh; returns `assistant_message_id` and `items[]` (`steer_id`, `content`, `delivery`, `mentioned_items`); `items` is empty when there is no running round |
| DELETE | `/api/v1/sessions/:id/steer/:steer_id` | Withdraw a queued message |
| POST | `/api/v1/sessions/:session_id/steer/:steer_id/inject` | Change an `after` message to `inject` |

POST request body:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `query` | string | Yes | Message content, up to 10000 characters |
| `delivery` | string | No | `after` (default): sent as the next round's question after the current round ends; `inject`: delivered to the agent at the next iteration boundary of the current round (including just before the final answer is about to be given), and the agent adjusts accordingly and continues |
| `mentioned_items` | []object | No | @-mention items, same format as chat requests |
| `steer_id` | string | No | Client-generated UUID, used for retry deduplication; the same ID with different content returns 409 |
| `expected_assistant_message_id` | string | No | The running assistant message ID the client sees; returns 409 if the run has switched, and the client should retry |
| `channel` | string | No | Source channel |

`status` in the response:

| `status` | Meaning |
| --- | --- |
| `queued` | Queued; returns `steer_id`, `delivery`, and `assistant_message_id` |
| `new_run` | There is currently no running round; the client should instead call `agent-chat` normally to send |
| `already_injected` | The message has already been delivered to the agent (may occur on retry, withdrawal, or change to inject); cannot be withdrawn |
| `deleted` / `gone` | DELETE only: withdrawn (`removed` indicates whether a message was actually deleted) / there is currently no running round |

At most 10 undelivered messages can be queued simultaneously per round; exceeding this returns 400. After the current round ends normally, the first undelivered message is started directly by the server as the next round's question, and the remaining messages are transferred into that round according to their original delivery mode; this round has no corresponding `agent-chat` connection, and the `assistant_message_id` returned by `GET /steer` can be used to call continue-stream to receive it. When the user stops generation, all undelivered messages are discarded. Delivered messages are written to session history and the client is notified via the `user_message_injected` event. Returns 503 if querying the run status fails; can be retried.

```bash
curl -X POST $BASE/api/v1/sessions/s-1/steer -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"query":"Only look at 2024 data","delivery":"inject"}'
```

### POST /api/v1/sessions/:session_id/pin and DELETE /api/v1/sessions/:id/pin

Purpose: Pin / unpin a session. No request body. Handler: `internal/handler/session/handler.go`

Response: 200 `{"success":true,"is_pinned":true|false}`

```bash
curl -X POST $BASE/api/v1/sessions/s-1/pin -H "Authorization: Bearer $TOKEN"
curl -X DELETE $BASE/api/v1/sessions/s-1/pin -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/sessions/continue-stream/:session_id

Purpose: Resume an active stream after disconnection (replay historical events + 100ms polling for new increments). Handler: `internal/handler/session/stream.go`

| Query Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `message_id` | string | Yes | Assistant message ID to resume |

Response: 200 SSE (`text/event-stream`, event format see the overview "Streaming Interface Protocol"). When replaying stored events, consecutive unfinished `answer`/`thinking`/`reflection` increments under the same event ID are merged into one frame; clients that accumulate content by event ID get the same text; the real-time push portion is not merged.

```bash
curl -N "$BASE/api/v1/sessions/continue-stream/s-1?message_id=m-1" -H "Authorization: Bearer $TOKEN"
```

## Sandbox Graphical Desktop {#sandbox-desktop}

These routes are registered by `internal/router/routes_chat.go` and have not yet entered Swagger. Only Cube/E2B desktop templates are supported; deployment and proxy requirements see [Sandbox Deployment](../06-development/04-sandbox-deployment.md).

### POST /api/v1/sessions/:session_id/sandbox/desktop-ticket

Issue a one-time WebSocket ticket valid for two minutes. Requires a valid login Bearer access token of the session owner; API Key alone cannot be used. The JWT is only placed in the authentication header of this POST.

```bash
curl -X POST "$BASE/api/v1/sessions/$SESSION_ID/sandbox/desktop-ticket" \
  -H "Authorization: Bearer $TOKEN"
```

Response: 200 `{"success":true,"data":{"ticket":"<opaque-ticket>","expires_in":120}}`.

### GET /api/v1/sessions/:id/sandbox/desktop

The WebSocket handshake uses `?ticket=<opaque-ticket>` and does not go through the normal JWT middleware. The ticket is bound to the user, space, session, and original access token, and becomes invalid after one use; used, expired, and unknown tickets are uniformly rejected. Proxy logs must not record the ticket query.

Only one relay is allowed per session at a time. States such as sandbox not bound, paused, desktop unsupported, or startup failure may complete the WebSocket upgrade first and then disconnect with close reasons such as `SANDBOX_NOT_BOUND`, `SANDBOX_PAUSED`, `DESKTOP_UNSUPPORTED`, `DESKTOP_START_FAILED`; the client should read the close reason.

### POST /api/v1/sessions/:session_id/sandbox/desktop/activity

The session owner reports keyboard/mouse activity; responds 200 `{"success":true}`. Used for renewal only when the server-side RFB parser is degraded; ignored when the parser is normal, and sandbox lifetime should not be extended by polling.

## Session Attachments (Temporary Documents)

Handler: `internal/handler/session/temporary_document.go`

### POST /api/v1/sessions/:session_id/attachments

Purpose: Upload and save a session attachment. After saving, it is immediately queued for background parsing; upload completion does not mean parsing is complete. Quick Q&A waits for unfinished parsing after the answer stream is established; intelligent reasoning reads the body on demand via `read_file`, waiting for documents that are not ready only at read time. Individually uploaded images wait for readiness first when image upload is enabled. Multipart fields: `file` (required), `agent_id` (optional, determines the parsing engine/ASR model), `parser_engine` (optional; ignored when using a shared agent, determined by the agent's parsing rules).

Response: 202 `{"success":true,"data":{TemporaryDocument}}` (`id,session_id,file_name,file_type,file_size,status(uploaded/processing/ready/failed),resource_ref,...`)

```bash
curl -X POST $BASE/api/v1/sessions/s-1/attachments -H "Authorization: Bearer $TOKEN" -F 'file=@notes.pdf'
```

`uploaded` is a stored state that is ready to send; clients should not force waiting until `ready` before allowing questions. Attachments referenced by valid session messages (including forks) are not subject to the default 24-hour TTL cleanup; only unreferenced expired files are cleaned up by the background.

### GET /api/v1/sessions/:id/attachments

Purpose: List attachments.

Response: 200 `{"success":true,"data":[TemporaryDocument]}`

```bash
curl $BASE/api/v1/sessions/s-1/attachments -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/sessions/:id/attachments/:attachment_id

Purpose: Attachment details (including parsing status).

Response: 200 `{"success":true,"data":{TemporaryDocument}}`

```bash
curl $BASE/api/v1/sessions/s-1/attachments/a-1 -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/sessions/:id/attachments/:attachment_id/preview

Purpose: Preview the original attachment file.

Response: 200 file stream (`Content-Disposition: inline|attachment`, `Cache-Control: private`).

```bash
curl $BASE/api/v1/sessions/s-1/attachments/a-1/preview -H "Authorization: Bearer $TOKEN" -o preview.pdf
```

### DELETE /api/v1/sessions/:id/attachments/:attachment_id

Purpose: Delete an attachment.

Response: 204 No Content

```bash
curl -X DELETE $BASE/api/v1/sessions/s-1/attachments/a-1 -H "Authorization: Bearer $TOKEN"
```

## Answer Suggestions (Suggestions)

Handler: `internal/handler/message_suggestion.go`

### GET /api/v1/sessions/:id/messages/:message_id/suggestions

Purpose: Read the follow-up suggestions for an assistant message.

Response: 200 `{"success":true,"data":{MessageSuggestionSet}}` (`status(generating/ready/suppressed/failed),questions:[{id,text,category,source,knowledge_base_ids}],allow_regenerate,...`)

```bash
curl $BASE/api/v1/sessions/s-1/messages/m-1/suggestions -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/sessions/:session_id/messages/:message_id/suggestions

Purpose: Ensure suggestions are generated (idempotent trigger). Request body: `{"regenerate":true}` (optional, forces regeneration).

Response: 200 (ready) or 202 (generating) `{"success":true,"data":{MessageSuggestionSet|null}}`

```bash
curl -X POST $BASE/api/v1/sessions/s-1/messages/m-1/suggestions -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{}'
```

### POST /api/v1/sessions/:session_id/suggestion-events

Purpose: Report suggestion interaction events (tracking).

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `suggestion_set_id` | string | Yes (`binding:"required"`) | Suggestion set ID |
| `question_id` | string | No | Required for click/regenerate |
| `event_type` | string | Yes (`binding:"required"`) | `impression/click/dismiss/regenerate` |

Response: 204 No Content

```bash
curl -X POST $BASE/api/v1/sessions/s-1/suggestion-events -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"suggestion_set_id":"ss-1","event_type":"impression"}'
```

## Chat and Retrieval

Handler: `internal/handler/session/qa.go`. API key: chat requires `chat`/full; `knowledge-search` requires `retrieve`/full.

External systems calling RAG Q&A typically follow two steps:

1. `POST /api/v1/sessions` to create a session, saving the `data.id` from the response.
2. `POST /api/v1/knowledge-chat/:session_id` to initiate streaming Q&A, using the value returned in step 1 for `:session_id` in the path; the request body specifies the knowledge bases to retrieve via `knowledge_base_ids`.

Authentication can be either of two options:

- Server-side integrations are recommended to use `X-API-Key: <api_key>`. A key used for RAG Q&A needs at least the `chat` capability; if sessionless retrieval is also needed, add `retrieve`.
- Logged-in user calls can use `Authorization: Bearer <access_token>`, with the token from `/auth/login`, OIDC, or VOS token exchange.

### POST /api/v1/knowledge-chat/:session_id

Purpose: Knowledge base Q&A (SSE streaming).

Request body (shared by KnowledgeQA/AgentQA):

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `query` | string | Usually required | User question; when carrying non-empty inline `images[].data` or `attachment_uploads[].data`, text may be omitted and the system generates a default question. Passing only `attachment_ids` or image URLs cannot omit text; illegal characters or empty-content requests return 400 |
| `knowledge_base_ids` | []string | No | Knowledge bases to retrieve |
| `knowledge_ids` | []string | No | Restrict to knowledge files |
| `agent_enabled` | bool | No | Whether to enable Agent mode |
| `agent_id` | string | No | Custom Agent ID |
| `agent_source_tenant_id` | uint64 | No | Source space of a shared agent, used to distinguish when shared agents with the same name come from multiple spaces |
| `reasoning_effort` | string | No | Thinking intensity for this round: `off`, `auto`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max` (`none`/`false` treated as `off`, `true`/`on` treated as `auto`); when omitted, the agent configuration is used. Applies only to this round and does not modify the agent; if the model does not support the selected level, it is automatically adjusted to a nearby level. Illegal values return 400 |
| `web_search_enabled` | bool | No | Web search; only takes effect when the agent itself has web search enabled |
| `local_browser_enabled` | bool | No | Allow use of the connected local browser for this round; can only be used for `agent-chat` of intelligent reasoning agents, otherwise returns 400. See [Local Browser](../05-clients/09-local-browser.md) |
| `summary_model_id` | string | No | Summary model; ignored when using a shared agent, always uses the model configured on the agent |
| `mcp_service_ids` | []string | No | @-mentioned MCP services |
| `skill_names` | []string | No | @-mentioned skills |
| `tag_ids` | []string | No | Tag filter |
| `mentioned_items` | []object | No | @-mention items (type/kb_id/kb_name/service_id/skill_name) |
| `disable_title` | bool | No | Disable automatic title |
| `images` | []object | No | Images (`data` base64 / `url` / `caption`) |
| `attachment_uploads` | []object | No | Inline attachments (`data` base64, `file_name`, `file_size`) |
| `attachment_ids` | []string | No | IDs of already-uploaded session attachments |
| `channel` | string | No | Source channel |
| `suggestion_attribution` | object | No | Attribution information for a clicked suggestion |
| `question_origin` | object | No | Where the user-clicked suggested question came from: `knowledge_base_id`, optional `knowledge_id`. Used only in intelligent reasoning mode; the agent retrieves from this source first; ignored when the source is not within this round's retrieval scope, and does not expand the scope |
| `page_context` | object | No | Current page: `knowledge_base_id`, optional `knowledge_id`, `wiki_slug`, `wiki_issue_ids` (at most 20 items retained), or `agent_id`. Knowledge bases must belong to this round's retrieval scope; agents are validated independently and must be visible to the current tenant (built-in agents supported). Invalid parts are discarded and do not expand retrieval permissions |
| `assistant_access` | string | No | WeKnora assistant permission level: `read_only`, `confirm`, `auto_write`, default `confirm`, subject to the space upper limit |

The selected `reasoning_effort` is written to the session's `last_request_state.reasoning_effort`, and the frontend restores it accordingly when the session is reopened.

Response: 200 SSE stream, `event: message` + `data: StreamResponse` (see overview), ending with a `complete` event. When the answer is truncated due to the model's single-output limit, the `answer` event carries `data.truncated: true`, and content generated before truncation is retained as usual.

```bash
curl -N -X POST $BASE/api/v1/knowledge-chat/s-1 -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"query":"What is the refund policy?","knowledge_base_ids":["kb-1"]}'
```

Notes:

- `knowledge_base_ids` is the recommended field, supporting one or more knowledge base IDs; when omitted, the current Agent/session default knowledge scope determines it.
- `knowledge_ids` is used to further restrict retrieval to certain document IDs.
- If the API Key was created with a knowledge base whitelist bound, the `knowledge_base_ids` in the request must be within the whitelist.
- The response is SSE, not one-shot JSON. The client needs to continuously read `data:` until it receives `response_type:"complete"`.

### POST /api/v1/agent-chat/:session_id

Purpose: Agent Q&A (SSE streaming, including events such as `thinking/tool_call/tool_result/tool_approval_required/mcp_oauth_required`). Request body same as above.

- Tool execution failures are returned as `tool_result` events (`data.success: false`, `data.error` is the reason), and the agent continues processing; the `error` event only indicates that the entire round failed.
- When the agent receives an appended message mid-way, it emits `user_message_injected`; during command execution, `command_output` updates the tool card output.
- Returns 409 if an intelligent reasoning answer is already being generated in the same session; in this case, use the [steer endpoint](#steer) to append messages instead.

```bash
curl -N -X POST $BASE/api/v1/agent-chat/s-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"query":"Analyze last quarter data","agent_id":"agent-1"}'
```

### POST /api/v1/knowledge-search

Purpose: Sessionless knowledge retrieval (non-streaming), the preferred endpoint for external systems to obtain retrieval results. Uses the same retrieval pipeline as in-product Q&A (recall → rerank → merge → truncate), with ranking consistent with page Q&A. For how to choose between this and `hybrid-search`, see [How to Choose a Retrieval API](./01-api-overview.md#retrieval-api). Handler: `SearchKnowledge` in `internal/handler/session/qa.go`.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `query` | string | Yes (`binding:"required"`) | Query |
| `knowledge_base_id` | string | No | Single KB (legacy compatible) |
| `knowledge_base_ids` | []string | No | Multiple KBs |
| `knowledge_ids` | []string | No | Specified files, unioned with `knowledge_base_id(s)` (see note below) |
| `tag_ids` | []string | No | Tag filter |
| `mentioned_items` | []object | No | Tag mentions with KB scope |
| `vector_threshold` / `keyword_threshold` | float | No | Recall thresholds; when omitted, the space retrieval configuration is used (default 0.15 / 0.3) |
| `match_count` | int | No | Number of results to return, max 200; when omitted, the space-configured `rerank_top_k` is used (default 10). Recall depth is automatically increased to be no less than this |
| `disable_keywords_match` / `disable_vector_match` | bool | No | Disable one recall path; returning 400 if both are `true` |
| `parent_slot_aggregation` | bool | No | Multiple child chunk hits under the same parent chunk occupy only 1 top-k slot: returns the parent chunk body, `sub_chunk_id` retains the hit child chunk, and the score is the highest score among this group of child chunks. Default `false`, does not affect normal chunks |
| `rerank` | object | No | Override rerank settings; `{"enabled":false}` disables rerank. Fields see [rerank object](./01-api-overview.md#retrieval-api). When `rerank.top_k` is also given, it takes precedence over `match_count` |

Omitted fields all use the space's retrieval configuration (`GET /tenants/kv/retrieval-config`); behavior is the same as before when no new fields are passed.

The retrieval scope is the union of `knowledge_base_id(s)` and `knowledge_ids`, consistent with the semantics of in-product @ mentions: each knowledge base is retrieved in full, files in `knowledge_ids` that belong to these knowledge bases are already covered and no longer separately restricted; files belonging to other knowledge bases are additionally included in retrieval. **When you only want to search within specified files, pass only `knowledge_ids` and do not also pass the knowledge bases they belong to**, otherwise the entire knowledge base will be retrieved.

Response: 200 `{"success":true,"data":[SearchResult],"meta":{"rerank":{...}}}`. `SearchResult` contains `id,content,knowledge_id,knowledge_title,score,chunk_type,knowledge_base_id,...`; results that have gone through rerank have `metadata` carrying `model_score` and `base_score`. When `data` is empty, check `meta.rerank.outcome` to determine the reason, see [meta.rerank diagnostics](./01-api-overview.md#retrieval-api).

```bash
curl -X POST $BASE/api/v1/knowledge-search -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' -d '{"query":"Deployment requirements","knowledge_base_ids":["kb-1"]}'

# Vector recall only, take 5 results, and disable rerank
curl -X POST $BASE/api/v1/knowledge-search -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"query":"Deployment requirements","knowledge_base_ids":["kb-1"],"disable_keywords_match":true,"match_count":5,"rerank":{"enabled":false}}'

# Search only within specified files (do not pass knowledge_base_ids)
curl -X POST $BASE/api/v1/knowledge-search -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' -d '{"query":"Deployment requirements","knowledge_ids":["k-1","k-2"]}'
```

## Messages (/api/v1/messages)

Handler: `internal/handler/message.go`

### POST /api/v1/messages/search

Purpose: Chat history search. Permissions: Viewer+; API key `message_history`/full.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `query` | string | Yes (`binding:"required"`) | Query |
| `mode` | string | No | `keyword/vector/hybrid` (default hybrid) |
| `limit` | int | No | Default 20 |
| `session_ids` | []string | No | Restrict to sessions |

Response: 200 `{"success":true,"data":{"total":N,"results":[{session_id,message_id,role,content,created_at,score}]}}`

```bash
curl -X POST $BASE/api/v1/messages/search -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"query":"quote"}'
```

### GET /api/v1/messages/chat-history-stats

Purpose: Chat history index statistics. Permissions: Viewer+; API key `message_history`/full.

Response: 200 `{"success":true,"data":{enabled,embedding_model_id,knowledge_base_id,knowledge_base_name,indexed_message_count,has_indexed_messages}}`.

Empty model and knowledge-base fields are omitted. Disabling indexing retains existing entries, including entries pending cleanup. A failed statistics lookup is never treated as an empty index.

```bash
curl $BASE/api/v1/messages/chat-history-stats -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/messages/chat-history-index/clear

Clear the message search index for **all members of the current workspace**. Requires Admin+; API keys need `manage_tenant_settings` or full access. `message_history` and `chat` do not grant this permission. No request parameters; callers cannot select a different workspace or knowledge base.

The operation first pauses automatic message indexing, then synchronously removes the indexed knowledge entries, chunks and retrieval vectors. Original messages, conversations and ordinary knowledge bases are retained. Keyword search remains available. Clearing the index cannot be undone.

Returns 200 with the same data shape as the statistics endpoint: `enabled=false`, `indexed_message_count=0`, `has_indexed_messages=false`, and no old `knowledge_base_id`. You may then change the embedding model and re-enable indexing. Re-enabling creates a new index for future conversations; old messages are not automatically indexed again.

If cleanup fails after pausing, indexing stays paused and the old knowledge-base binding is retained for retry. Refresh configuration and statistics before retrying; a timeout or error does not mean cleanup completed. Changing the embedding model while entries remain returns 409, even when indexing is disabled. The server timeout is five minutes; proxies and clients must allow enough time. Repeated requests do not delete original messages.

```bash
curl -X POST $BASE/api/v1/messages/chat-history-index/clear \
  -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/messages/:session_id/load

Purpose: Load session messages (time cursor backward pagination). Permissions: Viewer+; API key `chat`/full.

| Query Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `limit` | int | No | Default 20 |
| `before_time` | string | No | RFC3339/RFC3339Nano timestamp |

Response: 200 `{"success":true,"data":[Message]}` (`id,session_id,role,content,is_completed,images,attachments,agent_steps,...`)

```bash
curl "$BASE/api/v1/messages/s-1/load?limit=20" -H "X-API-Key: $API_KEY"
```

### DELETE /api/v1/messages/:session_id/:id

Purpose: Delete a single message. Permissions: Viewer+ (handler verifies session ownership); API key `chat`/full.

Response: 200 `{"success":true,"message":"Message deleted successfully"}`

```bash
curl -X DELETE $BASE/api/v1/messages/s-1/m-1 -H "Authorization: Bearer $TOKEN"
```

## Session Generated Files

The following endpoints require Viewer+, API Key with chat or full-access, and verify by session ownership. Nonexistent or inaccessible sessions return 404.

| Method | Path | Response |
| --- | --- | --- |
| GET | `/api/v1/sessions/:id/artifacts` | 200 `{success:true,data:[Artifact]}`, aggregates session files |
| GET | `/api/v1/sessions/:id/messages/:message_id/artifacts` | Same as above, only this message's files |
| GET | `/api/v1/sessions/:id/messages/:message_id/artifacts/:index/download` | 200 file stream, Content-Disposition: attachment |
| DELETE | `/api/v1/sessions/:id/messages/:message_id/artifacts/:index` | 200 `{success:true,data:{file_name,deleted}}`, deletes the file |

Artifact fields: index, handle (optional resource:// reference), file_name, file_type, file_size, source_path, mod_time, created_at. The response does not return the underlying object storage URL. Download index starts from 0 and must use the index of the corresponding message list; you cannot use the session aggregate index to directly construct a message download URL. Illegal index returns 400; out-of-range or nonexistent file returns 404.

```bash
curl "$BASE/api/v1/sessions/session-1/messages/message-1/artifacts" \
  -H "Authorization: Bearer $TOKEN"
curl "$BASE/api/v1/sessions/session-1/messages/message-1/artifacts/0/download" \
  -H "Authorization: Bearer $TOKEN" -o result.pdf
```

### Deleting Generated Files

Deletion reclaims the bytes in object storage and is **irreversible**. Unlike download, deletion is only open to the session owner: read-only access obtained through a shared agent can download files but cannot delete them — sessions that do not belong to you are uniformly treated as 404 (not distinguishing between "nonexistent" and "no permission", consistent with the rest of the session endpoints). Deleted files return 404, and repeated deletion also returns 404.

Bytes are actually reclaimed only when there are no other holders: the same file being stored in a knowledge base, re-referenced by a subsequent answer, or the session it resides in being forked into a copy will all keep it retained. Reclamation failure does not affect the deletion result (the endpoint still returns 200), and the file has disappeared from all lists.

After deletion, the file no longer appears in list endpoints or the artifact library, but its **position in the message is retained**: `index` is the download address, and if subsequent files shift forward in sequence, existing download links would point to the wrong file. For the same reason, a file with the same name in the sandbox will not be re-collected in the next round — its mtime has not changed due to the user's deletion.

| Parameter | Description |
| --- | --- |
| `all_versions` | Also delete all historical versions with the same `source_path` in this session. Boolean, default `false` |

```bash
curl -X DELETE "$BASE/api/v1/sessions/session-1/messages/message-1/artifacts/0" \
  -H "Authorization: Bearer $TOKEN"
```

### Cross-Session Artifact List

`GET /api/v1/artifacts` lists all generated files in the current user's web conversations, used by the "Artifacts" page in the homepage sidebar. The scope is consistent with the session list's `source=web`: the user's own sessions and historically unowned tenant-level web sessions; IM channels, web widgets (embed), and API Key sessions are all excluded, even if the IM session has no owner in the database. Files with the same `source_path` in the same session are treated as multiple versions of the same file, and only the latest version is returned, with `version_count` giving the number of versions. Files in deleted sessions or messages are not returned. Permission requirements are the same as the table above.

| Parameter | Description |
| --- | --- |
| `keyword` | Filter by file name, case-insensitive |
| `file_types` | Comma-separated extensions, such as `.pdf,.pptx` (leading dot optional) |
| `page` / `page_size` | Pagination, `page_size` max 100, default 20 |

Response `{success, data:[LibraryArtifact], total, page, page_size}`, sorted by generation time descending. LibraryArtifact fields: session_id, session_title, message_id, index, handle (optional), file_name, file_type, file_size, source_path, created_at, version_count. When downloading, use the session_id, message_id, and index within to call the download endpoint in the table above.

```bash
curl "$BASE/api/v1/artifacts?file_types=.pptx,.pdf&keyword=report&page=1&page_size=30" \
  -H "Authorization: Bearer $TOKEN"
```

`DELETE /api/v1/artifacts` deletes a file in the artifact library, located by the query parameters `session_id`, `message_id`, `index`, with semantics consistent with the in-session deletion above. The only difference is that when `all_versions` is omitted it is treated as `true` (when explicitly passed, the parsing rules of the two endpoints are the same): a row in the artifact library represents a file (`version_count` gives the number of versions) rather than a single generation, and deleting only the latest version would leave the row in the list showing the previous version. Passing `all_versions=false` deletes only the current version.

```bash
curl -X DELETE "$BASE/api/v1/artifacts?session_id=session-1&message_id=message-1&index=0" \
  -H "Authorization: Bearer $TOKEN"
```

### Image and File References in Answers

`GET /api/v1/sessions/:id/messages/:message_id/files?file_path=...` is a message-level authenticated proxy. file_path passes the resource handle referenced by the message or a supported storage reference, and the client should URL-encode it. The backend verifies message access rights, the binding of the resource to the message, and the current access rights of the knowledge base/shared Agent; arbitrary file paths cannot be accessed by virtue of the session ID. After authorization is revoked, old message references are also rejected. Applicable to shared Agents, organization shared libraries answering images and message artifacts; see [File Access](../03-features/21-file-access.md) for details.

### Per-Round Usage

Messages return persisted usage, and the Agent completion event carries turn_usage; it includes the aggregated results of model calls for each purpose in this round. Tool calls themselves do not all produce tokens; usage is based on what the provider returns or what the backend has collected. Fields see [Observability](../03-features/16-observability.md).

The completion event in agent mode carries in `data.model_id` the ID of the conversation model actually run in this round (the model that produced this round's usage), which can be used to obtain model information via the model endpoint and convert usage by unit price. The `model_id` in the message record is the model bound at request time; the two differ when the model specified in the request is invalid and falls back to the agent model. The completion event of quick Q&A does not yet carry this field.

## Implementation Reference

Route registration: `RegisterSessionRoutes`, `RegisterChatRoutes`, `RegisterMessageRoutes` in `internal/router/routes_chat.go` (called by `internal/router/router.go`). Handlers: `internal/handler/session/` (handler.go, qa.go, stream.go, title.go, temporary_document.go, fork.go, rewind.go, steer.go, artifact_*.go), `internal/handler/message.go`, `internal/handler/message_suggestion.go`.
