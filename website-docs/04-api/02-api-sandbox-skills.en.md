# API Reference: Sandboxes, Skills, and Personal Variables

Manage sandbox configurations, skill catalogs, installation tasks, session interactive terminals, and personal environment variables. All paths are prefixed with `/api/v1`; in the examples, `$BASE` is the service address and `$TOKEN` is the current user's Bearer token. For UI operation steps, see [Skill Catalog and Sandboxes](../03-features/22-skills-sandbox.md).

## Permissions

| Resource | Read | Write / Check |
| --- | --- | --- |
| Sandbox configuration list/detail | Viewer+; API Key must be full-access | Admin+; API Key must be full-access |
| Sandbox instance inventory, installed skills, and files | Admin+ | Admin+; API Key must be full-access |
| Skill catalog list | Viewer+, JWT | Catalog add/delete, install, and package file browsing require Admin+; API Keys in the write group must be full-access |
| `/skills` available skills | Viewer+, JWT | Read-only, query parameter sandbox_config_id; for shared agents also pass agent_id and agent_source_tenant_id |
| Session interactive terminal | Session owner, logged-in JWT only | Establish WebSocket after obtaining ticket |
| `/me/env-vars*` | The currently logged-in caller | Modify only your own; identity is derived server-side; no additional admin threshold, uses Bearer JWT |

Cross-workspace IDs do not grant access. The personal variables API cannot replace the workspace skill management API.

## Sandbox Configurations

| Method | Path | Request / Response |
| --- | --- | --- |
| GET | `/sandbox-configs` | 200 `{success,data:[ConfigResponse],workspace_scripts_disabled}` |
| POST | `/sandbox-configs` | `{name,description?,config}`; 201 `{success,data:ConfigResponse}` |
| GET | `/sandbox-configs/:id` | 200 `{success,data:ConfigResponse}` |
| PUT | `/sandbox-configs/:id` | `{name,description?,config}`, name required; 200 same as detail |
| DELETE | `/sandbox-configs/:id` | Optional `force=true`; 200 `{success:true}` |
| GET | `/sandbox-configs/:id/sandboxes` | 200 `{success,data:SandboxInventory}`, includes occupancy and associated agents |
| PUT | `/sandbox-configs/workspace-policy` | `{"scripts_disabled":true}`; returns success/workspace_scripts_disabled |
| POST | `/sandbox-configs/templates/query` | `{config,config_id?,ensure_standard?,replace_standard?,ensure_desktop?,replace_desktop?}`; queries remote templates, can create/replace standard templates or desktop templates; replace requires config_id |

ConfigResponse is `{id,name,description,sandbox_type,config,created_at,updated_at}`, with credentials redacted. When editing, updates are based on the saved configuration; redacted placeholder values retain the old credentials, and `skill_image` is maintained by the installation service and cannot be replaced by clients.

### config Fields

| Field | Type | Description |
| --- | --- | --- |
| `sandbox_type` | string | docker/cube/e2b; local has been removed; the host of Lite desktop cannot be saved as a configuration |
| `default_timeout_sec` | int | Execution timeout; 0 uses the built-in default |
| `terminal_idle_disconnect_sec` | int | How long terminal/desktop disconnects after inactivity; 0 means the default 900 seconds; effective range is 60 seconds–24 hours |
| `desktop_enabled` | bool | Declares that the selected template is a Cube/E2B desktop template; cannot be switched after skills are installed |
| `allow_private_endpoints` | bool | Allows private network cluster addresses; does not allow link-local/cloud metadata |
| `env_vars` | map[string]string | Environment for this sandbox configuration; values are stored encrypted |
| `skill_rollout` | string | next_turn (default)/new_session; has no effect when `allocation_mode=user`; existing shared machines always retain the current image |
| `allocation_mode` | string | session (default, one per session)/user (shared by the same user); only affects first allocation, does not migrate existing sessions |
| `network` | object | Cube/E2B network policy, see below |
| `cube` / `e2b` / `docker` | object | Connection configuration corresponding to sandbox_type |
| `volume_mount` | object | Optional volume configuration; whether it takes effect is determined by backend capabilities; the presence of the field alone cannot be taken as support |
| `skill_image` | object | Snapshot information maintained by the installation service, read-only |

volume_mount fields include enabled, mount_path, provider, volume_id, volume_name; volume_owner_fingerprint is managed by the server. Whether a volume is mounted is determined jointly by the volume configuration and backend support capabilities; when editing, retain the ownership information returned by the server.

Backend configuration:

| Backend | Fields |
| --- | --- |
| cube | api_url, proxy_url, sandbox_domain, template_id; api_key as required by the cluster; http_timeout_sec, cube_sandbox_ttl_seconds, dns_servers |
| e2b | api_key, template_id required; api_url, sandbox_domain, proxy_url for self-hosting; http_timeout_sec, e2b_sandbox_ttl_seconds |
| docker | image required; host (empty means local socket), tls_cert_path (required for TCP), cpu_limit, memory_limit_mb, pids_limit, network_mode (bridge/none), runtime, idle_ttl_seconds, http_timeout_sec |

```bash
curl -X POST "$BASE/api/v1/sandbox-configs" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"E2B Work Environment","config":{"sandbox_type":"e2b","e2b":{"api_key":"<e2b-key>","template_id":"<template-id>"}}}'

curl "$BASE/api/v1/sandbox-configs/cfg-1/sandboxes" \
  -H "Authorization: Bearer $TOKEN"
```

Changing the backend identity and deleting a configuration will check remote instances. The error.code of a 409 can be `sandboxes_still_live`, `sandbox_inventory_unverifiable`, `skill_snapshot_release_failed`, etc.; 423 means the configuration is being modified by another request. `force=true` only allows deletion when inventory cannot be verified, and does not skip confirmed active/paused instances. The occupancy data in the response is used to locate related sessions and agents.

### Network Policy

`network`:

| Field | Description |
| --- | --- |
| `deny_egress_by_default` | false allows egress by default; true denies by default |
| `allow_out` | IPv4/CIDR/domain or single-label wildcard domain; domains are used in default-deny mode |
| `deny_out` | IPv4/CIDR deny list |
| `cube_rules` | Cube name/scheme/sni/host/methods/path/deny/audit/inject rules; order matters |
| `e2b_host_rules` | E2B host/headers rules; host must also be listed in allow_out |
| `allow_public_inbound` | Compatible with legacy input, cleared on save; inbound always requires credentials |

`cube_rules[].inject` is `[{header,secret,format}]`, `e2b_host_rules[].headers` is header→secret; secret fields are stored encrypted and redacted. Docker uses `docker.network_mode` and does not support these L7 rules.

```json
{
  "deny_egress_by_default": true,
  "allow_out": ["pypi.org", "files.pythonhosted.org"],
  "deny_out": []
}
```

### POST /system/sandbox-check

Request `{config,config_id?,deep?}`; config_id can be used to restore saved redacted credentials. `deep=false` performs a connection check; true also executes a temporary script, and remote backends will actually create and destroy a sandbox, which may incur backend usage.

A successful probe returns `{success:true,data:{ok,provider,checks,capabilities}}`; a failed probe may also be HTTP 200, so you should check data.ok; parameter errors return 400 with code/msg. Each check contains name/ok/message/reason/latency_ms; `ok=null` means skipped. For example, when egress is denied by default, the external network probe is skipped by policy, which is not the same as passing external network access verification.

```bash
curl -X POST "$BASE/api/v1/system/sandbox-check" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"config_id":"cfg-1","deep":true}'
```

## Skill Catalog

| Method | Path | Request / Response |
| --- | --- | --- |
| GET | `/skills/catalog` | 200 `{success,data:[CatalogItem]}`; CatalogItem contains `installations:[{sandbox_config_id,status,enabled,version,bundle_sha256,served?,...}]` |
| POST | `/skills/catalog` | multipart `file` or JSON `{"source":"@owner/slug"}`; 201 `{success,data:{id,name,version,description}}` |
| POST | `/skills/catalog/:id/install` | `{"sandbox_config_ids":["cfg-1","cfg-2"]}`; 202 `{success,data:{installs,errors?}}` |
| GET | `/skills/catalog/:id/files` | 200 `{success,data:[FileEntry]}` |
| GET | `/skills/catalog/:id/files/content?path=SKILL.md` | 200 `{success,data:FileContent}` |
| DELETE | `/skills/catalog/:id` | Delete when there are no installation references; 200 `{success:true}` |

Installing to multiple sandboxes may be partially accepted: check `data.installs` and `data.errors`; HTTP 202 does not mean all installations are complete. Deleting a catalog entry does not implicitly uninstall it from each sandbox.

```bash
curl -X POST "$BASE/api/v1/skills/catalog" \
  -H "Authorization: Bearer $TOKEN" -F 'file=@skill.zip'
curl -X POST "$BASE/api/v1/skills/catalog/catalog-1/install" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"sandbox_config_ids":["cfg-1"]}'
```

For source syntax, anonymous download requirements, and ZIP restrictions, see [Skill Sources](../03-features/22-skills-sandbox.md#supported-sources).

## Skills Within a Sandbox

The following path prefix is `/sandbox-configs/:id/skills`; all operations, including reads, require Admin+.

| Method | Suffix | Behavior |
| --- | --- | --- |
| GET | empty | `{success,data:[SkillResponse]}` |
| POST | empty | Install via ZIP file or source JSON; 202 `{success,data:{skill_id}}` |
| GET | `/:skillId` | `{success,data:SkillResponse}` |
| POST | `/:skillId/reinstall` | Reinstall reusing the package; optional `{"instructions":"..."}` (≤10000 characters) as installation instructions; 202 `{success,data:{skill_id}}` |
| POST | `/:skillId/stop` | Stop installation; 200 returns skill status; can be called repeatedly if already failed; other non-installing states are rejected |
| GET | `/:skillId/guidance` | Instructions for the current installation run: `{success,data:{accepting,messages:[{id,content,status}]}}` |
| POST | `/:skillId/guidance` | Append instructions while installation is in progress `{expected_message_id,steer_id,content}`: `expected_message_id` takes the `install_message_id` from SkillResponse, `steer_id` is a client-generated UUID (retries will not append duplicates), `content` is 1–10000 characters; 202 `{success:true}`; returns 409 if installation has moved to another round |
| PATCH | `/:skillId` | `enabled?`, `envs?`; omitted means unchanged |
| DELETE | `/:skillId` | Uninstall from this sandbox, retaining the catalog package |
| GET | `/:skillId/files` | File list |
| GET | `/:skillId/files/content?path=SKILL.md` | Read a file at a relative path; traversal is rejected |
| GET | `/:skillId/install-events` | SSE installation progress |
| GET | `/:skillId/transcript` | SSE installation process events |

SkillResponse includes id/name/version/description/enabled/status/error/bundle_sha256/installed_snapshot_id/install_session_id/install_message_id/created_at/updated_at, as well as `envs:[{name,description,required,is_set}]`. During an upgrade or reinstall, or after a failure, `served:{version}` indicates the last ready version still being served by the sandbox; the skills available to agents are based on that version. File responses may be UTF-8 text, base64 for small images, or binary metadata; you cannot assume all files have text content.

PATCH envs only updates declared variables; undeclared names are ignored; an empty string clears the value while retaining the declaration.

```bash
curl -X PATCH "$BASE/api/v1/sandbox-configs/cfg-1/skills/skill-1" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"enabled":true,"envs":{"API_TOKEN":"<workspace-token>"}}'

curl -N "$BASE/api/v1/sandbox-configs/cfg-1/skills/skill-1/install-events" \
  -H "Authorization: Bearer $TOKEN"
```

The progress payload is `{percent,stage,log?,status?,done}`. Disconnecting does not cancel the installation; done may only mean progress can no longer be followed, and the final result is determined by the skill status. Without Redis, the stream prompt changes to polling status. A 204 from transcript means the installation has just started and event positioning is not yet ready; 404 may mean there is no log or the log has expired, and cannot be used to determine that installation failed.

`GET /skills?sandbox_config_id=cfg-1` returns skill metadata usable by agents `{success,data:[{name,description}],skills_available}`, which is different from the full installation records visible to administrators.

The assistant calls this API through `skill.list` and must pass the `sandbox_config_id` returned by `agent.get` (may be empty); only `name`, `description`, and `skills_available` are projected, and shared agent query parameters or skill management APIs are not exposed. An empty configuration ID returns an empty list with `skills_available:false`, and the user should be prompted to select a sandbox in the agent editor; when configuring skills, use the returned `name`, not the installation record ID.

When both `agent_id` and `agent_source_tenant_id` (the source workspace ID of the shared agent) are passed, list the skills that the shared agent can @-mention: the sandbox configuration is taken from the agent itself, ignoring `sandbox_config_id` in the query; when the agent's skill scope is `selected`, only the specified skills are returned; when disabled, an empty list is returned with `skills_available:false`. If the caller is not authorized to use the agent, 403 is returned.

## Session Interactive Terminal

The terminal in the chat sidebar connects to the remote sandbox for this session via WebSocket; only Cube/E2B are supported. Routes are registered by `internal/router/routes_chat.go` and are not yet in Swagger; for desktop APIs, see [Session API](02-api-chat.md#sandbox-desktop); for proxy requirements, see [Sandbox Deployment](../06-development/04-sandbox-deployment.md#interactive-terminal-and-graphical-desktop).

### POST /api/v1/sessions/:session_id/sandbox/terminal-ticket

Issues a handshake ticket valid for two minutes. Requires the session owner's logged-in Bearer access token; API Keys cannot call it.

```bash
curl -X POST "$BASE/api/v1/sessions/$SESSION_ID/sandbox/terminal-ticket" \
  -H "Authorization: Bearer $TOKEN"
```

Response: 200 `{"success":true,"data":{"ticket":"<ticket>","expires_in":120}}`.

### GET /api/v1/sessions/:id/sandbox/terminal

WebSocket handshake; does not go through the normal authentication middleware:

| Query Parameter | Required | Description |
| --- | --- | --- |
| `ticket` | Yes | Ticket obtained in the previous step, bound to the user, workspace, session, and the access token at issuance |
| `provision` | No | `1` means allow creating or waking a sandbox (may incur billing); when omitted, only connect to a running sandbox |
| `agent_id` / `agent_source_tenant_id` | No | Used together with `provision=1` to specify which agent's (including shared agents) sandbox configuration to create from |
| `cols` / `rows` | No | Initial terminal size |
| `pty_id` | No | Reattach to the Shell process returned by a previous `ready` frame |

After connection, binary frames carry terminal input/output in both directions; text frames are JSON control messages: the server sends `ready` (including `pty_id`, `backend`, `reattachable`), `exited` (including `exit_code`), and `error`; the client may send `{"type":"resize","cols":..,"rows":..}`. When `reattachable` is `false` (Docker), there is no `pty_id`, the Shell ends on disconnect, and the client should not automatically reconnect. Error codes include `SANDBOX_NOT_BOUND`, `SANDBOX_PAUSED`, `SANDBOX_STOPPED` (Docker backend that does not pause: container has stopped or has been reclaimed due to idleness), `TERMINAL_UNSUPPORTED`, `IDLE_DISCONNECTED`, `AUTH_REVOKED`, `INTERNAL`. Each session supports at most 5 terminals simultaneously; exceeding this returns 429.

## Lite Local Project Directory

Provided only by Lite desktop; other deployments return 404. It pops up a system directory picker on the computer running Lite, and the directory selected by the user is added to the approved list; then when creating a session, pass the returned `dir` as `project_dir` so that the session can use this directory in the [local sandbox](../03-features/22-skills-sandbox.md#lite-host). For the `project_dir` field, see [Session API](02-api-chat.md).

| Method | Path | Request / Response |
| --- | --- | --- |
| POST | `/system/host-project-dir` | No request body; 200 `{"code":0,"msg":"success","data":{"dir":"/Users/me/project"}}`; when selection is canceled, `dir` is empty. Accepts only logged-in JWT, Viewer+, API Keys are rejected |

## Personal Environment Variables

The caller identity is determined by the current authentication context and does not accept user_id. The personal API returns variable names, sources, and update times, but not plaintext.

| Method | Path | Request |
| --- | --- | --- |
| GET | `/me/env-vars` | Returns configuration variables and skill variables grouped by sandbox_config_id |
| PUT | `/me/env-vars/skill` | `{skill_id,name,value}`; the name must be declared by the skill |
| DELETE | `/me/env-vars/skill` | `{skill_id,name}` |
| PUT | `/me/env-vars/sandbox` | `{sandbox_config_id,name,value}` |
| DELETE | `/me/env-vars/sandbox` | `{sandbox_config_id,name}` |

Reads return `{success,data:[{sandbox_config_id,sandbox_config_name,description,vars,skills}]}`, and source indicates states such as user/workspace/unset. Set/delete success returns success; deleting an unset item returns 404. Personal sandbox variables reject the WEKNORA_ prefix and reserved names such as PATH; skill-declared variables may use the required WEKNORA_* credential names.

```bash
curl -X PUT "$BASE/api/v1/me/env-vars/skill" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"skill_id":"skill-1","name":"API_TOKEN","value":"<my-token>"}'

curl -X DELETE "$BASE/api/v1/me/env-vars/skill" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"skill_id":"skill-1","name":"API_TOKEN"}'
```

Override order is personal skill value > personal sandbox value > workspace skill value. Writing personal values does not change administrator configuration. Implementation references: `internal/router/routes_infra.go`, `routes_agent.go`, `routes_auth_tenant.go`, and `internal/handler/sandbox_config.go`, `sandbox_skill.go`, `skill_catalog.go`, `me_env_var.go`, `host_project_picker.go`.
