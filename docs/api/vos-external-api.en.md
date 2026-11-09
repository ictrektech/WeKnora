# VOS External HybRAG API Integration Guide

[Back to Table of Contents](./README.md)

This document explains how external systems outside the VOS iframe can access the HybRAG API as a regular VOS user. The examples are based on the default ports of the VOS App:

- HybRAG frontend: `http://<VOS host IP>:29080`
- HybRAG API: `http://<VOS host IP>:29081/api/v1`
- VOS API: `http://<VOS host IP>:8105`

If `HYBRAG_FRONTEND_PORT` or `HYBRAG_API_PORT` was modified during HybRAG installation, use the actual installed ports.

For more integration check items, see: [VOS External API Integration Checklist](./vos-external-api-validation.md).

## Permission Model

External applications accessing the user's own knowledge base do not require an admin account. There are two approaches:

1. Regular user personal API Key: After a regular user logs in to HybRAG, they create their own API Key in "Settings -> API Integration". This Key is bound only to the current user and can only access the knowledge base scope selected at creation time.
2. VOS/OAuth Bearer Token: The external application first obtains a regular user's VOS access token through VOS login or OAuth flow, then calls HybRAG with `Authorization: Bearer <VOS_ACCESS_TOKEN>`. HybRAG validates the token, maps it to the corresponding local user, and executes according to that user's permissions.

Only platform-level administrators (`CanAccessAllTenants=true`) can create workspace-level API Keys. Regular users, even if they are the Owner of their own personal space, can only create personal API Keys and cannot create full-access or high-privilege Keys such as member, model, or data source Keys.

## Approach 1: Regular User Personal API Key

Suitable for long-term server-side integration. The user first creates an API Key in the HybRAG UI and binds the knowledge bases allowed for access.

Subsequent requests use:

```bash
BASE="http://<VOS host IP>:29081/api/v1"
API_KEY="sk_xxx"

curl -sS "$BASE/auth/me" \
  -H "X-API-Key: $API_KEY"
```

Create a session:

```bash
SESSION_ID=$(curl -sS -X POST "$BASE/sessions" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"title":"External System RAG Session"}' | jq -r '.data.id')
```

Initiate RAG Q&A with a specified knowledge base:

```bash
curl -N -X POST "$BASE/knowledge-chat/$SESSION_ID" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What are the key points in this knowledge base?",
    "knowledge_base_ids": ["<knowledge_base_id>"],
    "channel": "api"
  }'
```

Sessionless retrieval:

```bash
curl -sS -X POST "$BASE/knowledge-search" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Search keywords",
    "knowledge_base_ids": ["<knowledge_base_id>"]
  }'
```

## Approach 2: VOS User Token

Suitable for scenarios that need to strictly inherit the current VOS user identity and permissions.

### 1. Obtain VOS access token

The VOS OAuth token endpoint does not support password grant; in browsers or within the VOS App, authorization code / Fastpath is recommended. For command-line verification, you can call the VOS login endpoint to obtain a regular user's access token:

```bash
VOS_BASE="http://<VOS host IP>:8105"
VOS_USER="<regular username>"
VOS_PASS="<regular user password>"

VOS_TOKEN=$(curl -sS -X POST "$VOS_BASE/v1000/user/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"$VOS_USER\",\"password\":\"$VOS_PASS\"}" \
  | jq -r '.data.access_token')
```

If the production integration has already obtained an access token through the VOS OAuth flow, you can use that token directly without calling the username/password login again.

### 2. Call HybRAG directly

```bash
BASE="http://<VOS host IP>:29081/api/v1"

curl -sS "$BASE/auth/me" \
  -H "Authorization: Bearer $VOS_TOKEN" | jq
```

You can also first exchange it for a HybRAG local login state, then use the returned HybRAG token to call subsequent APIs:

```bash
HYBRAG_TOKEN=$(curl -sS -X POST "$BASE/auth/vos-oidc" \
  -H "Content-Type: application/json" \
  -d "{\"access_token\":\"$VOS_TOKEN\"}" \
  | jq -r '.token')
```

### 3. Create a personal API Key

A regular user's personal API Key must be bound to a knowledge base scope. First list the knowledge bases visible to the current user:

```bash
curl -sS "$BASE/knowledge-bases?creator=all" \
  -H "Authorization: Bearer $VOS_TOKEN" | jq
```

Create a personal API Key that only allows retrieval and chat:

```bash
KB_ID="<knowledge_base_id>"

API_KEY=$(curl -sS -X POST "$BASE/tenants/<tenant_id>/api-keys" \
  -H "Authorization: Bearer $VOS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"external-rag-readonly\",
    \"full_access\": false,
    \"capabilities\": [\"retrieve\", \"chat\"],
    \"knowledge_base_ids\": [\"$KB_ID\"]
  }" | jq -r '.data.api_key')
```

After that, external services can use the `X-API-Key` from Approach 1 to call the RAG API.

## Specifying Knowledge Bases and Sessions

- `knowledge_base_ids` is the recommended field and can accept one or more knowledge base IDs.
- `knowledge_base_id` is the legacy single-knowledge-base field, retained only for compatibility.
- `knowledge_ids` is used to specify a single or multiple documents. The `knowledge_id` in the HybRAG API corresponds to the document ID in business semantics; after uploading a file, importing a URL, or creating manual knowledge, the `data.id` in the response is the `knowledge_id` of that document.
- If you only want to retrieve within one or several documents, put these document IDs into `knowledge_ids`; the field name is not `document_id` or `document_ids`.
- `session_id` is returned by `POST /sessions`; reuse the same `session_id` for multi-turn conversations by the same user, and create a new one for a new topic.
- The `knowledge_base_ids` in a personal API Key request must be within the scope bound at Key creation time, otherwise `403` will be returned.

```bash
curl -N -X POST "$BASE/knowledge-chat/$SESSION_ID" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"Answer this question based only on the specified document\",
    \"knowledge_base_ids\": [\"$KB_ID\"],
    \"knowledge_ids\": [\"$KNOWLEDGE_ID\"],
    \"channel\": \"api\",
    \"disable_title\": true
  }"
```

## Integration Confirmation Items

- Regular users can obtain a VOS access token through VOS login or OAuth flow.
- HybRAG `/auth/vos-oidc` can validate this token and map it to a HybRAG local user.
- Regular users have `can_access_all_tenants=false` and cannot create workspace-level high-privilege API Keys.
- Regular users can only create personal API Keys bound to a knowledge base scope; workspace-level permissions such as `manage_members`, `manage_models`, `manage_datasources`, and `full_access` are only available to platform-level administrators.
