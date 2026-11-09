# API Reference: Organizations and Sharing

Manage organization members and the sharing relationships of knowledge bases and agents. Organizations use workspaces as their unit of membership.

An organization uses "spaces (tenants)" as its unit of membership. The API key policy for organization group routes is `manage_spaces` or full-access; the two read-only "shared knowledge base list" endpoints additionally allow `retrieve`, so that read-only keys can discover shared knowledge bases they already have permission to retrieve; KB/Agent share management is available only to full-access keys.

## Organization Management (/api/v1/organizations)

### POST /api/v1/organizations

Purpose: Create an organization. Permission: Admin+.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | Yes | Organization name |
| `description` | string | No | Description |
| `avatar` | string | No | Avatar URL |
| `searchable` | bool | No | Whether it can be discovered via search |
| `require_approval` | bool | No | Whether joining requires approval |
| `member_limit` | int | No | Maximum number of member spaces |
| `invite_code_validity_days` | int | No | Invite code validity period (days) |

Response: 201 `{"success":true,"data":{OrganizationResponse}}`

```bash
curl -X POST $BASE/api/v1/organizations -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"R&D Organization"}'
```

### GET /api/v1/organizations

Purpose: List the organizations I belong to. Permission: Viewer+.

Response: 200 `{"success":true,"data":{"organizations":[...],"total":N,"resource_counts":{"knowledge_bases":{"by_organization":{}},"agents":{"by_organization":{}}}}}`

```bash
curl $BASE/api/v1/organizations -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/organizations/preview/:code

Purpose: Preview an organization by invite code (without joining). Permission: Viewer+. Path parameter: `code` invite code.

Response: 200 `{"success":true,"data":{id,name,description,avatar,member_count,share_count,agent_share_count,is_already_member,require_approval,created_at}}`

```bash
curl $BASE/api/v1/organizations/preview/ABC123 -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/organizations/join

Purpose: Join an organization with an invite code. Permission: Admin+. Request body: `{"invite_code":"..."}` (required).

Response: 200 `{"success":true,"data":{OrganizationResponse}}`

```bash
curl -X POST $BASE/api/v1/organizations/join -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"invite_code":"ABC123"}'
```

### POST /api/v1/organizations/join-request

Purpose: Submit a join request (for organizations that require approval). Permission: Admin+.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `invite_code` | string | Yes | Invite code |
| `message` | string | No | Request message |
| `role` | string | No | Desired role |

Response: 200 `{"success":true,"data":{JoinRequest}}`

```bash
curl -X POST $BASE/api/v1/organizations/join-request -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"invite_code":"ABC123","message":"Requesting to join"}'
```

### GET /api/v1/organizations/search

Purpose: Search discoverable (searchable) organizations. Permission: Viewer+.

| Query Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `q` | string | No | Keyword |
| `limit` | int | No | Default 20, maximum 100 |

Response: 200 `{"success":true,"data":[SearchableOrganization],"total":N}`

```bash
curl "$BASE/api/v1/organizations/search?q=R&D" -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/organizations/join-by-id

Purpose: Join a discoverable organization by organization ID (no invite code required). Permission: Admin+.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `organization_id` | string | Yes | Target organization ID |
| `message` | string | No | Message |
| `role` | string | No | Desired role |

Response: 200 `{"success":true,"data":{OrganizationResponse}}`

```bash
curl -X POST $BASE/api/v1/organizations/join-by-id -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"organization_id":"org-1"}'
```

### GET /api/v1/organizations/:id

Purpose: Organization details. Permission: Viewer+.

Response: 200 `{"success":true,"data":{OrganizationResponse}}`

```bash
curl $BASE/api/v1/organizations/org-1 -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/organizations/:id

Purpose: Update an organization (the service layer verifies that the caller's space is an organization admin, not limited to the owner). Permission: Admin+. Request body fields are the same as for creation (all optional).

Response: 200 `{"success":true,"data":{OrganizationResponse}}`

```bash
curl -X PUT $BASE/api/v1/organizations/org-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"description":"Updated description"}'
```

### DELETE /api/v1/organizations/:id

Purpose: Delete an organization. Permission: Admin+ (the service layer requires the organization owner).

Response: 200 `{"success":true,"message":"Organization deleted successfully"}`

```bash
curl -X DELETE $BASE/api/v1/organizations/org-1 -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/organizations/:id/leave

Purpose: The current space leaves the organization. Permission: Admin+. No request body. Knowledge bases and Agents shared by the current space into this organization will also be revoked.

Response: 200 `{"success":true,"message":"Left organization successfully"}`

```bash
curl -X POST $BASE/api/v1/organizations/org-1/leave -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/organizations/:id/request-upgrade

Purpose: Request an upgrade of the current space's role within the organization. Permission: Admin+.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `requested_role` | string | Yes | Desired organization role (`viewer/editor/admin`) |
| `message` | string | No | Message |

Response: 200 `{"success":true,"data":{JoinRequest}}`

```bash
curl -X POST $BASE/api/v1/organizations/org-1/request-upgrade -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"requested_role":"editor"}'
```

### POST /api/v1/organizations/:id/invite-code

Purpose: Generate an organization invite code. Permission: Admin+ (the service layer requires organization admin). No request body.

Response: 200 `{"success":true,"data":{"invite_code":"..."}}`

```bash
curl -X POST $BASE/api/v1/organizations/org-1/invite-code -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/organizations/:id/search-tenants

Purpose: Resolve invitable spaces by space ID. Permission: Admin+, and the caller's space must be an organization admin. Since v0.8.2, only complete space IDs are accepted; cross-space search by space name is no longer supported, and the `limit` parameter has been removed.

| Query Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `q` | string | Yes | Complete space ID |

Response: 200 `{"success":true,"data":[{"tenant_id","tenant_name"}]}`. If `q` is not a valid ID, the space does not exist, or it is already an organization member, an empty array is returned; otherwise the single candidate is returned.

```bash
curl "$BASE/api/v1/organizations/org-1/search-tenants?q=10002" -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/organizations/:id/search-users

Purpose: Deprecated alias, behaves the same as `search-tenants`. Permission: Admin+. Parameters are the same as above.

```bash
curl "$BASE/api/v1/organizations/org-1/search-users?q=10002" -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/organizations/:id/invite

Purpose: Directly invite a space to join the organization. Permission: Admin+.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `tenant_id` | uint64 | Choose one | Target space ID (recommended) |
| `user_id` | string | Choose one | Compatibility path: user ID (resolved to its space) |
| `representative_user_id` | string | No | Ignored, retained only for compatibility: directly added spaces do not have a representative user attached, to avoid the inviter specifying which user's information from the other party's space appears in the member list |
| `role` | string | Yes | Role within the organization |

Response: 200 `{"success":true,"message":"Member added successfully"}`

```bash
curl -X POST $BASE/api/v1/organizations/org-1/invite -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"tenant_id":2,"role":"viewer"}'
```

### GET /api/v1/organizations/:id/members

Purpose: List organization members (spaces). Permission: Viewer+. `email` is returned only for the row of the caller's own space; for other spaces, only the username and avatar are returned.

Response: 200 `{"success":true,"data":{"members":[{id,user_id,representative_user_id,role,tenant_id,tenant_name,username,email,avatar,joined_at}],"total":N}}`

```bash
curl $BASE/api/v1/organizations/org-1/members -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/organizations/:id/members/:tenant_id

Purpose: Change a member space's organization role. Permission: Admin+. Path parameter `tenant_id` is the member space ID. Request body: `{"role":"editor"}` (required, `viewer/editor/admin`).

Response: 200 `{"success":true,"message":"Member role updated successfully"}`

```bash
curl -X PUT $BASE/api/v1/organizations/org-1/members/2 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"role":"editor"}'
```

### DELETE /api/v1/organizations/:id/members/:tenant_id

Purpose: Remove a member space (including self-removal). Permission: Admin+. Knowledge bases and Agents shared by the removed space into this organization will also be revoked.

Response: 200 `{"success":true,"message":"Member removed successfully"}`

```bash
curl -X DELETE $BASE/api/v1/organizations/org-1/members/2 -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/organizations/:id/join-requests

Purpose: Join request queue. Permission: Admin+.

Response: 200 `{"success":true,"data":{"requests":[{id,user_id,username,email,message,request_type,prev_role,requested_role,status,created_at,reviewed_at}],"total":N}}`

```bash
curl $BASE/api/v1/organizations/org-1/join-requests -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/organizations/:id/join-requests/:request_id/review

Purpose: Approve or reject join/upgrade requests. Permission: Admin+.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `approved` | bool | Yes | Approve/reject |
| `message` | string | No | Review comment |
| `role` | string | No | Role granted upon approval |

Response: 200 `{"success":true,"message":"Review completed"}`

```bash
curl -X PUT $BASE/api/v1/organizations/org-1/join-requests/req-1/review \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"approved":true}'
```

### GET /api/v1/organizations/:id/shares

Purpose: View the list of KBs shared to this organization. Permission: Viewer+.

Response: 200 `{"success":true,"data":{"shares":[KnowledgeBaseShareResponse],"total":N}}`

```bash
curl $BASE/api/v1/organizations/org-1/shares -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/organizations/:id/agent-shares

Purpose: View the list of Agents shared to this organization. Permission: Viewer+.

Response: 200 `{"success":true,"data":{"shares":[AgentShareResponse],"total":N}}`

```bash
curl $BASE/api/v1/organizations/org-1/agent-shares -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/organizations/:id/shared-knowledge-bases

Purpose: Organization space view: all shared KBs within the organization (including my own). Permission: Viewer+; the API key requires `manage_spaces`, `retrieve`, or full-access, and only KBs within that key's whitelist are returned.

Response: 200 `{"success":true,"data":[...including is_mine and source_from_agent flags...],"total":N}`

```bash
curl $BASE/api/v1/organizations/org-1/shared-knowledge-bases -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/organizations/:id/shared-agents

Purpose: Organization space view: all shared Agents within the organization. Permission: Viewer+.

Response: 200 `{"success":true,"data":[SharedAgentInfo],"total":N}`

```bash
curl $BASE/api/v1/organizations/org-1/shared-agents -H "Authorization: Bearer $TOKEN"
```

## KB Sharing (/api/v1/knowledge-bases/:id/shares)

API key: full-access only. Handler: `internal/handler/organization.go`

### POST /api/v1/knowledge-bases/:id/shares

Purpose: Share a KB to an organization. Permission: KB creator OR Admin+.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `organization_id` | string | Yes | Target organization |
| `permission` | string | Yes | Share permission (organization role semantics, e.g. `viewer/editor`) |

Response: 201 `{"success":true,"data":{KBShare}}`

```bash
curl -X POST $BASE/api/v1/knowledge-bases/kb-1/shares -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"organization_id":"org-1","permission":"viewer"}'
```

### GET /api/v1/knowledge-bases/:id/shares

Purpose: View the share list of this KB. Permission: Viewer+.

Response: 200 `{"success":true,"data":{"shares":[KnowledgeBaseShareResponse],"total":N}}`

```bash
curl $BASE/api/v1/knowledge-bases/kb-1/shares -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/knowledge-bases/:id/shares/:share_id

Purpose: Modify share permission. Permission: KB creator OR Admin+. Request body: `{"permission":"editor"}` (required).

Service layer rules (shared with removing a share):

- The original sharer must operate within the space that owns the KB, and their space role must be Contributor+;
- Admin+ of the space that owns the KB can manage all shares of that space;
- For a space whose role in the target organization is admin, its Admin+ users can only **lower** the permission or remove the share; they cannot raise the permission above the current value.

`share_id` must belong to the KB in the path, otherwise 404.

Response: 200 `{"success":true,"message":"Share permission updated successfully"}`

```bash
curl -X PUT $BASE/api/v1/knowledge-bases/kb-1/shares/s-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"permission":"editor"}'
```

### DELETE /api/v1/knowledge-bases/:id/shares/:share_id

Purpose: Remove a share. Permission: KB creator OR Admin+, service layer rules as above. `share_id` must belong to the KB in the path, otherwise 404.

Response: 200 `{"success":true,"message":"Share removed successfully"}`

```bash
curl -X DELETE $BASE/api/v1/knowledge-bases/kb-1/shares/s-1 -H "Authorization: Bearer $TOKEN"
```

## Agent Sharing (/api/v1/agents/:id/shares)

API key: full-access only. Handler: `internal/handler/organization.go`

### POST /api/v1/agents/:id/shares

Purpose: Share an Agent to an organization. Permission: Agent creator OR Admin+. Request body is the same as KB sharing (`organization_id` + `permission`, both required). Built-in agents cannot be shared (400): every space has a built-in agent with the same ID, and after sharing the recipient cannot distinguish them. An Agent's knowledge base scope is opened up to organization members, so the caller must have permission to directly share each of its knowledge bases (knowledge base creator or Admin+); `kb_selection_mode: all` can be shared only by Admin+, otherwise 403.

Response: 201 `{"success":true,"data":{AgentShare}}`

```bash
curl -X POST $BASE/api/v1/agents/agent-1/shares -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"organization_id":"org-1","permission":"viewer"}'
```

### GET /api/v1/agents/:id/shares

Purpose: View the share list of this Agent. Permission: Agent creator OR Admin+.

Response: 200 `{"success":true,"data":{"shares":[AgentShareResponse],"total":N}}`

```bash
curl $BASE/api/v1/agents/agent-1/shares -H "Authorization: Bearer $TOKEN"
```

### DELETE /api/v1/agents/:id/shares/:share_id

Purpose: Remove an Agent share. Permission: Agent creator OR Admin+, service layer rules same as removing a KB share. `share_id` must belong to the Agent in the path, otherwise 404.

Response: 200 `{"success":true,"message":"Share removed successfully"}`

```bash
curl -X DELETE $BASE/api/v1/agents/agent-1/shares/s-1 -H "Authorization: Bearer $TOKEN"
```

## Shared Resource Aggregated View

### GET /api/v1/shared-knowledge-bases

Purpose: List KBs shared with me through organizations (owner-side vector store metadata is removed). Permission: Viewer+; the API key requires `manage_spaces`, `retrieve`, or full-access, and only KBs within that key's whitelist are returned.

Response: 200 `{"success":true,"data":[...],"total":N}`

```bash
curl $BASE/api/v1/shared-knowledge-bases -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/shared-agents

Purpose: List Agents shared with me through organizations. Permission: Viewer+; API key as above.

Response: 200 `{"success":true,"data":[SharedAgentInfo],"total":N}`. `SharedAgentInfo` includes `source_tenant_id` (source space), `org_name`, `shared_by_username`, `permission`, and `web_search_ready`—only this single boolean indicating "whether web search is available in the source space" is returned; the source space's provider configuration is not sent (which would leak the configuration), nor is the recipient space's provider ID used for comparison (which would cause false negatives).

When using a shared Agent to call other endpoints, if an Agent with the same name is shared by multiple spaces, you can include `agent_source_tenant_id` to specify the source space; this value is validated against the sharing relationships one by one, and an error is returned directly if it is invalid or unauthorized, without silently falling back to another source.

```bash
curl $BASE/api/v1/shared-agents -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/shared-agents/disabled

Purpose: Set "disable a shared Agent for this space" (affects the session dropdown for the entire space). Permission: Admin+; API key as above.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent_id` | string | Yes (`binding:"required"`) | Shared Agent ID |
| `disabled` | bool | No | Whether to disable (default false) |

Response: 200 `{"success":true}`

```bash
curl -X POST $BASE/api/v1/shared-agents/disabled -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"agent_id":"agent-1","disabled":true}'
```

## Implementation Reference

Route registration: `RegisterOrganizationRoutes` in `internal/router/routes_agent.go`. Handler: `internal/handler/organization.go`.
