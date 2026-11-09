# VOS External API Integration Checklist

[Back to Contents](./README.md)

This document provides configuration checks, call sequence, and permission boundary descriptions for external systems integrating with the HybRAG API. The content applies to scenarios where HybRAG is installed as a VOS App, and also to integration approaches that directly access the RAG API through host-mapped ports.

## Access Addresses

The HybRAG API uniformly uses the `/api/v1` prefix. The VOS App template provides two types of access by default:

- VOS application path: `/app/com.ictrek.hybrag/api/v1`
- Direct host connection: `http://<VOS host IP>:29081/api/v1`

The frontend page is mapped to `http://<VOS host IP>:29080` by default. If `HYBRAG_FRONTEND_PORT` or `HYBRAG_API_PORT` was adjusted during installation, use the actual ports.

## Authentication Paths

HybRAG supports two external authentication paths.

### Regular User Personal API Key

After a regular user logs in to HybRAG, they create a personal API Key in "Settings -> API Integration" and bind the knowledge base scope allowed for access. The external service stores this Key and uses `X-API-Key` to call the API.

A personal API Key only represents the user who created it:

- No admin issuance is required.
- It does not have space-level full access.
- Available capabilities are limited to personal-scope capabilities such as knowledge retrieval, knowledge Q&A, and document writing.
- Access scope is limited by the `knowledge_base_ids` selected at creation time.

### VOS/OAuth Bearer Token

External applications can also first obtain a regular user's access token through VOS login or the OAuth authorization flow, then call HybRAG with `Authorization: Bearer <VOS_ACCESS_TOKEN>`.

HybRAG validates the VOS token, maps it to the corresponding HybRAG user, and executes according to that user's permissions. This approach is suitable for integrations that need to strictly inherit the current VOS user identity and permissions.

## Call Sequence

### 1. Confirm the Calling Subject

```bash
BASE="http://<VOS host IP>:29081/api/v1"
API_KEY="<regular user personal API Key>"

curl -sS "$BASE/auth/me" \
  -H "X-API-Key: $API_KEY"
```

When using a VOS token:

```bash
VOS_TOKEN="<regular user VOS/OAuth access token>"

curl -sS "$BASE/auth/me" \
  -H "Authorization: Bearer $VOS_TOKEN"
```

### 2. List Accessible Knowledge Bases

```bash
curl -sS "$BASE/knowledge-bases?creator=all&page=1&page_size=20" \
  -H "X-API-Key: $API_KEY"
```

The returned knowledge base `id` is used as `knowledge_base_ids` in subsequent requests.

### 3. Create a Session

```bash
SESSION_ID="$(curl -sS -X POST "$BASE/sessions" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"title":"External system RAG session"}' \
  | jq -r '.data.id')"
```

`session_id` is used for multi-turn conversations. A new topic can create a new session; the same topic can reuse the same session.

### 4. Sessionless Retrieval

```bash
KB_ID="<knowledge_base_id>"

curl -sS -X POST "$BASE/knowledge-search" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"retrieval keywords\",
    \"knowledge_base_ids\": [\"$KB_ID\"]
  }"
```

### 5. RAG Q&A

```bash
curl -N -X POST "$BASE/knowledge-chat/$SESSION_ID" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"Please answer the question based on the knowledge base\",
    \"knowledge_base_ids\": [\"$KB_ID\"],
    \"channel\": \"api\",
    \"disable_title\": true
  }"
```

## Restricting to Specified Documents

The `document_id` in a business system corresponds to the `knowledge_id` in the HybRAG API.

You can obtain `knowledge_id` in the following ways:

- The `data.id` in the response after uploading a file, importing a URL, or creating manual knowledge.
- The `id` of each record returned by the knowledge base document list endpoint.

Use `knowledge_ids` in the request to restrict to one or several documents:

```bash
KNOWLEDGE_ID="<knowledge_id>"

curl -sS -X POST "$BASE/knowledge-search" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"Only retrieve content from this document\",
    \"knowledge_base_ids\": [\"$KB_ID\"],
    \"knowledge_ids\": [\"$KNOWLEDGE_ID\"]
  }"
```

For a personal API Key bound to a knowledge base scope, when `knowledge_ids` is included, it is recommended to also pass the corresponding `knowledge_base_ids` to explicitly verify that the document restriction occurs within the authorized knowledge base scope.

## Capability Boundaries

| Capability | HybRAG API Support | Description |
| --- | --- | --- |
| Regular users accessing their own RAG | Supported | Via personal API Key or VOS/OAuth token |
| Specifying Knowledge Base | Supported | Use `knowledge_base_ids` |
| Specifying single or multiple documents | Supported | Use `knowledge_ids` |
| Session reuse | Supported | `POST /sessions` returns `session_id` |
| RAG Chat | Supported | `POST /knowledge-chat/:session_id`, SSE output |
| Retrieval | Supported | `POST /knowledge-search` |
| Citation / Sources | Supported | Retrieval results and Q&A events include source information; specific content depends on whether documents are matched |
| API Key creation, listing, update, deletion | Supported | Regular users create personal Keys; administrators can create workspace-level Keys |
| API Key expiration time | Supported | `expires_at_unix` can be set at creation or update |
| API Key rotation | Achieved through recreation | Create a new Key, switch the caller, delete the old Key |
| NAS ACL, user groups, global audit | Platform capability | HybRAG inherits VOS identity but does not replace VOS/NAS platform APIs |

## Integration Recommendations

- For long-term server-side integration, prefer personal API Keys to facilitate binding capabilities and knowledge base scope.
- When strict inheritance of the current VOS user permissions is required, use VOS/OAuth tokens.
- Terminal devices should not store admin credentials, platform-level API Keys, or user passwords.
- NAS file permissions and RAG knowledge base permissions should be designed separately: file readability does not mean automatic entry into the knowledge base; there should be a clear process before publishing to RAG.
