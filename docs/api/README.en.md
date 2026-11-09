# HybRAG API Documentation

## Table of Contents

- [Overview](#overview)
- [Most Authoritative Reference: Swagger UI](#most-authoritative-reference-swagger-ui)
- [Basic Information](#basic-information)
- [Authentication Mechanism](#authentication-mechanism)
- [Calling the RAG API in Non-VOS Scenarios](#calling-the-rag-api-in-non-vos-scenarios)
- [VOS External Calling HybRAG API Integration Guide](./vos-external-api.md)
- [VOS External API Integration Checklist](./vos-external-api-validation.md)
- [VOS External API Integration Checklist (Traditional Chinese)](./vos-external-api-validation.zh-TW.md)
- [Error Handling](#error-handling)
- [File and Image References (`resource://` and Direct Links)](#file-and-image-referencesresource-and-direct-links)
- [API Overview](#api-overview)

## Overview

HybRAG provides a series of RESTful APIs for creating and managing knowledge bases, retrieving knowledge, and performing knowledge-based question answering. This document describes in detail how to use these APIs.

## Most Authoritative Reference: Swagger UI

HybRAG also provides OpenAPI-based Swagger documentation. **After starting the service, visit `http://localhost:8080/swagger/index.html`** to see the complete parameters and request/response schemas for all endpoints, and to try them out directly in the browser—it updates automatically with the code and is the most accurate interface reference.

The markdown documents in this directory provide more readable examples and scenario descriptions, and are maintained in sync with swagger; when the two differ, swagger takes precedence.

> Swagger UI is only mounted in non-release mode (`GIN_MODE != release`); it is disabled by default in production deployments.

## Basic Information

- **Base URL**: `/api/v1`
- **Response Format**: JSON
- **Authentication Method**: `X-API-Key` or `Authorization: Bearer <token>`

When the VOS app is installed, the HybRAG backend listens on `8080` inside the container and provides two access methods:

- VOS application path: `https://<VOS address>/app/com.ictrek.hybrag/api/v1`
- Host direct-connect port: `http://<host IP>:29081/api/v1`

The VOS package also maps the frontend to `http://<host IP>:29080` by default. If `HYBRAG_API_PORT` or `HYBRAG_FRONTEND_PORT` was modified during installation, the above ports are subject to the installation configuration. Older standalone compose or debug deployments may use other ports, such as `19081` in historical templates.

## Authentication Mechanism

Business API requests need to carry a valid credential in the HTTP request header.

For server-side integration, it is recommended to prioritize using the ordinary user's own API Key:

```
X-API-Key: your_api_key
```

Logged-in HybRAG users, as well as calls after OIDC/VOS automatic login, can also use a Bearer Token. The VOS/OAuth access token
will be validated by the HybRAG backend against the VOS userinfo endpoint; after successful validation, it is mapped to the corresponding VOS user's HybRAG user and permission checks are performed:

```
Authorization: Bearer <access_token>
```

To facilitate issue tracking and debugging, it is recommended to add `X-Request-ID` to the HTTP request header of each request:

```
X-Request-ID: unique_request_id
```

### Obtaining an API Key

Create an API Key in "Settings → API Integration".

- An ordinary user creates a **personal API Key**, which is bound only to the current user, can select capabilities such as `retrieve`, `chat`, `ingest`, and must be restricted to the knowledge base scope that the user can access; even if the user is the Owner of their own personal space, this does not grant workspace-level API Key permissions.
- Platform-level administrators (`CanAccessAllTenants=true`) can create workspace-level API Keys, and can open space management capabilities such as `manage_kbs`, `manage_models` as needed, or create full-access Keys.

Please keep your API Key safe and avoid leaking it. The plaintext Key is only returned at creation time; if you need to change it, delete the old Key and create a new one.

## Calling the RAG API in Non-VOS Scenarios

When HybRAG is opened as a VOS App, the frontend attempts to automatically log in using the current VOS user. External services, scripts, or browser extensions that do not run inside the VOS iframe can call the HybRAG REST API through the VOS application path or the host direct-connect port. Two authentication paths are currently supported:

1. **Personal API Key**: An ordinary user creates an API Key for themselves in HybRAG "API Integration", and the external application sends `X-API-Key`. This is the simplest and most long-term stable server-side integration method; the Key only inherits that user's own knowledge base access scope, and does not require an admin account to issue it in advance.
2. **VOS/OAuth Bearer token**: The external application first obtains the ordinary user's VOS access token through the VOS/OAuth login flow, then sends `Authorization: Bearer <VOS token>`. HybRAG will call VOS `/v1000/oauth2/userinfo` for validation; on failure, it falls back to the legacy VOS `/v1000/user/check`. After successful validation, it executes according to the HybRAG user permissions mapped from that VOS user.

Therefore, external applications accessing the user's own knowledge base **do not require an admin account**. When using a personal API Key, it is created by the ordinary user themselves; when using a VOS/OAuth Bearer token, it inherits the HybRAG permissions mapped from that VOS user.

### RAG Query / Chat API

The typical flow is to first create a session, then initiate a streaming RAG Q&A:

```bash
BASE=https://example.com/api/v1
API_KEY=sk_xxx

SESSION_ID=$(curl -s -X POST "$BASE/sessions" \
  -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{}' | jq -r '.data.id')

curl -N -X POST "$BASE/knowledge-chat/$SESSION_ID" \
  -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"query":"What are the key points in this knowledge base?","knowledge_base_ids":["<knowledge_base_id>"],"channel":"api"}'
```

If the caller already has a VOS/OAuth access token, they can also directly replace `X-API-Key` in the above example with:

```bash
VOS_TOKEN=eyJ...

curl -N -X POST "$BASE/knowledge-chat/$SESSION_ID" \
  -H "Authorization: Bearer $VOS_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"query":"What are the key points in this knowledge base?","knowledge_base_ids":["<knowledge_base_id>"],"channel":"api"}'
```

- `POST /knowledge-chat/:session_id` is knowledge base RAG Q&A, returning an SSE stream.
- `POST /agent-chat/:session_id` is agent Q&A, supporting Agent capabilities such as tools, MCP, and web search.
- `POST /knowledge-search` is sessionless knowledge retrieval, returning one-time JSON without calling the LLM to generate an answer.

### Specifying Knowledge Base / Knowledge Base ID

- RAG Chat uses the request body field `knowledge_base_ids` to specify one or more knowledge base IDs, for example `["kb-1"]`.
- Sessionless retrieval `POST /knowledge-search` also supports `knowledge_base_ids`; `knowledge_base_id` is retained only as a legacy single-base field.
- If a knowledge base scope was set when the API Key was created, the knowledge base IDs in the request must be within that scope.

### Specifying Documents / Knowledge ID

- `knowledge_id` in the HybRAG API corresponds to a single document, file, URL, or manually entered knowledge entry in business semantics; `document_id` in the business system can be passed as `knowledge_id`.
- After creating or uploading a document, `data.id` in the response is the `knowledge_id` of that document; the `id`, `file_name`, and `title` of each document record can also be read through the knowledge base document list endpoint.
- `POST /knowledge-search`, `POST /knowledge-chat/:session_id`, and `POST /agent-chat/:session_id` all support the request body field `knowledge_ids`, used to restrict retrieval to one or several documents.
- The field name is `knowledge_ids`, not `document_id` or `document_ids`; it can be passed together with `knowledge_base_ids` to further restrict to specified documents under a specified knowledge base.

```bash
curl -N -X POST "$BASE/knowledge-chat/$SESSION_ID" \
  -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "Answer only based on these two documents",
    "knowledge_base_ids": ["<knowledge_base_id>"],
    "knowledge_ids": ["<knowledge_id_1>", "<knowledge_id_2>"],
    "channel": "api"
  }'
```

### Conversation / Session ID

- `session_id` comes from `data.id` in the `POST /sessions` response.
- When the same end user continues a multi-turn conversation, reuse the same `session_id`; a new topic can create a new session.
- When using a personal API Key or VOS/OAuth Bearer token, the calling subject is already a specific user, so there is no need to configure `X-External-User-ID`.
- When integrating with a workspace-level API Key and needing to distinguish end users, configure the user identity mode in "API Integration": trusted servers can use `X-External-User-ID`, and user-facing applications are recommended to use `X-External-User-Token` signed tokens.

### Authentication / Service Account

- External services do not need to log in with an admin account every time to obtain a Token.
- Ordinary users can create their own personal API Keys, and third-party services can call with `X-API-Key` over the long term; the permission boundary is determined by the Key's capabilities and knowledge base scope.
- When strict inheritance of the current VOS user's permissions is required, the external application can use the ordinary user's VOS/OAuth access token as `Authorization: Bearer <token>`; after validation, HybRAG executes according to the mapped user's permissions.
- API Keys are not placed in the Bearer header; API Keys use `X-API-Key`, and VOS/OAuth or HybRAG user tokens use `Authorization: Bearer`.

## Error Handling

All APIs use standard HTTP status codes to indicate request status and return a unified error response format:

```json
{
  "success": false,
  "error": {
    "code": "error code",
    "message": "error message",
    "details": "error details"
  }
}
```

## File and Image References (`resource://` and Direct Links)

Images, charts, and attachments in responses are returned by default as internal references `resource://<handle>`, for example
`![diagram](resource://xifDo7NTSL300Lp1goVutw)` in a Q&A answer. Such references cannot be loaded directly by the browser; the client needs to
call the authenticated `GET /files?file_path=<reference>` proxy to fetch the byte stream.

If you are integrating HybRAG into your own App, you can have the server directly return **loadable http(s) direct links**, saving this
extra request:

| Method | Usage | Scope |
|------|------|----------|
| Single request | Add `?resource_urls=public` to the URL | Only that request |
| Entire deployment | Environment variable `RESOURCE_URL_MODE=public` | All requests that do not explicitly pass the parameter |

`resource_urls` takes the value `handle` (default, keep internal references) or `public` (return direct links); passing any other value returns
`400`. The single-request parameter takes precedence over the environment variable, so after setting the deployment default to `public`, you can still use
`?resource_urls=handle` to fall back individually.

Endpoints that support this parameter:

- `POST /api/v1/knowledge-chat/{session_id}` (SSE)
- `POST /api/v1/agent-chat/{session_id}` (SSE)
- `GET /api/v1/sessions/continue-stream/{session_id}` (SSE)
- `GET /api/v1/messages/{session_id}/load`
- `POST /api/v1/knowledge-search`
- `POST /api/v1/knowledge-bases/{id}/hybrid-search` (GET compatible)

The rewriting covers the answer body, retrieval results `content` / `image_info`, `knowledge_references`, Agent execution steps and tool results, as well as images
on messages. References that are truncated across two chunks in streaming answers are buffered first and then rewritten, so the client always receives complete links.

### Notes

- **External link capability is required.** Direct links are presigned by the storage backend, or provided by `APP_EXTERNAL_URL` + `/r/<token>`. When neither is
  available (for example, local storage without `APP_EXTERNAL_URL` set), the reference **remains as `resource://`**, and the client
  can still fall back to the `/files` proxy. See the `APP_EXTERNAL_URL` description in `.env.example` for details.
- **Direct links are time-limited and anonymously readable** (grants issued by HybRAG last 2 hours, MinIO presigned links last 24 hours). Anyone who obtains the link
  can read the file before it expires, so do not write it to logs or forward it to parties who should not see the file.
- **The embedded (embed) channel does not support this parameter.** Its visitors are anonymous, and endpoints under `/api/v1/embed/...` force the use of
  `handle` (even if `?resource_urls=public` is passed or the deployment default is `public`); images still go through the channel-dimension authenticated proxy.
- **API Keys restricted to a knowledge base cannot use `public`**, and return `403`. Such Keys are also denied access to the `/files`
  proxy; if they could obtain anonymous direct links, that would bypass the same restriction. Use `handle` instead to call normally.
- **Direct links for the same file are reused within the validity period**: repeated requests do not re-issue credentials, nor do they return a different URL each time, so client
  and CDN caches can hit. Links become invalid immediately after the credential is revoked or expires.

## API Overview

The HybRAG API is divided into the following categories by function:

| Category | Description | Documentation Link |
|------|------|----------|
| Authentication Management | User registration, login, token management; OIDC flow | [Authentication and User API](../../website-docs/04-api/02-api-auth.md) · [Authentication and Authorization](../../website-docs/03-features/01-tenant-auth.md) |
| Space Management | Create and manage space accounts | [Space and Member API](../../website-docs/04-api/02-api-tenant.md) |
| Knowledge Base Management | Create, query, and manage knowledge bases | [Knowledge Base and Knowledge API](../../website-docs/04-api/02-api-knowledge.md) |
| Knowledge Management | Upload, retrieve, and manage knowledge content | [Knowledge Base and Knowledge API](../../website-docs/04-api/02-api-knowledge.md) |
| Model Management | Configure and manage various AI models | [Model and Initialization API](../../website-docs/04-api/02-api-model-system.md) |
| Chunk Management | Manage chunked content of knowledge | [Chunk and Tag API](../../website-docs/04-api/02-api-chunks.md) |
| Tag Management | Manage tag classification of knowledge bases | [Chunk and Tag API](../../website-docs/04-api/02-api-chunks.md) |
| FAQ Management | Manage FAQ Q&A pairs | [FAQ and Wiki API](../../website-docs/04-api/02-api-faq-wiki.md) |
| Agent Management | Create and manage custom agents | [Agent, MCP and Skills API](../../website-docs/04-api/02-api-agent-mcp.md) |
| Contract Review | Upload contracts and asynchronously obtain risks, evidence, and revision suggestions | [contract-review.md](../contract-review.md) |
| Session Management | Create and manage conversation sessions | [Session, Message and Chat API](../../website-docs/04-api/02-api-chat.md) |
| Knowledge Search | Search content in knowledge bases | [Session, Message and Chat API](../../website-docs/04-api/02-api-chat.md) |
| Chat Functionality | Q&A based on knowledge bases and Agents | [Session, Message and Chat API](../../website-docs/04-api/02-api-chat.md) |
| Message Management | Retrieve and manage conversation messages | [Session, Message and Chat API](../../website-docs/04-api/02-api-chat.md) |
| Evaluation Functionality | Evaluate model performance | [Model and Initialization API](../../website-docs/04-api/02-api-model-system.md) |
| Initialization Management | Knowledge base model configuration and Ollama management | [Model and Initialization API](../../website-docs/04-api/02-api-model-system.md) |
| System Management | System information, parsing engine, storage engine | [System and Platform API](../../website-docs/04-api/02-api-system.md) |
| MCP Service | MCP tool service management | [Agent, MCP and Skills API](../../website-docs/04-api/02-api-agent-mcp.md) |
| Organization Management | Organizations, members, knowledge base/agent sharing | [Organization and Sharing API](../../website-docs/04-api/02-api-org.md) |
| Skills | Pre-installed and installed agent skills, skill environment variables | [Sandbox, Skills and Personal Variables API](../../website-docs/04-api/02-api-sandbox-skills.md) |
| Long-term Memory | Current user's cross-session memory (settings, entries, topics, document affinity, export, organization) | [Long-term Memory API](../../website-docs/04-api/02-api-memory.md) |
| Web Search | Web search service providers | [Infrastructure and Data Source API](../../website-docs/04-api/02-api-infra.md) |
| Vector Storage | Vector database connection management | [Infrastructure and Data Source API](../../website-docs/04-api/02-api-infra.md) |
| Storage Backend | Object/file storage instance (multi-instance) management | [Infrastructure and Data Source API](../../website-docs/04-api/02-api-infra.md) |
| IM Channels | Integration with IM platforms such as WeCom / Feishu / Slack, including channel CRUD and callbacks | [IM, Embed and File API](../../website-docs/04-api/02-api-channels.md) · [IM Integration](../../website-docs/03-features/12-im-integration.md) |
| Data Source Import | Integration and synchronization of external data sources such as Feishu / WeCom / Notion / Confluence | [Infrastructure and Data Source API](../../website-docs/04-api/02-api-infra.md) · [Data Source Synchronization](../../website-docs/03-features/10-datasource.md) |
