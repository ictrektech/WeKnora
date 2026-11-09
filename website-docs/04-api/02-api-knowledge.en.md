# API Reference: Knowledge Bases and Knowledge

Create knowledge bases, import and manage documents, and query processing progress, copy or move content.

Permission quick reference: read routes are Viewer+ and require read permission on the KB (owned/organization-shared/shared Agent visible); write routes are "KB creator OR Admin+" and require write permission. API key: reads require `retrieve`, content writes require `ingest`, KB lifecycle requires `manage_kbs` (all can be overridden by full-access), and are subject to the KB allowlist.

Chunking, tags, and chunk preview endpoints (`/chunks`, `/knowledge-bases/:id/tags`, `/chunker/preview`) are in [Chunking and Tags](./02-api-chunks.md).


## Image Processing Pipeline

`GET /api/v1/image-pipelines` returns the available pipelines, parameter declarations, and validation rules; `POST /api/v1/image-pipelines/validate` validates a configuration. Validation is performed again when saving a knowledge base, so you cannot rely on the frontend passing alone. An explicit `image_pipeline` takes precedence over the legacy `image_attrs_enabled`.

The knowledge base PATCH can use `image_caption_enabled` / `image_ocr_enabled` to adjust manual actions; these two fields require `image_pipeline=default`. The `process_config` for document upload and reparse can also override the pipeline and its parameters; omitted items use the knowledge base configuration.

## Knowledge Bases (/api/v1/knowledge-bases)

### POST /api/v1/knowledge-bases

Purpose: create a knowledge base. Permission: Contributor+; API key `manage_kbs`/full. Handler: `internal/handler/knowledgebase.go`

Request body (`types.KnowledgeBase`):

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | Yes | Name |
| `description` | string | No | Description |
| `type` | string | No | `document` (default)/`faq`/`wiki` |
| `embedding_model_id` | string | No | Embedding model ID |
| `chunking_config` | object | No | Chunking configuration (chunk_size/overlap/separators/strategy…) |
| `image_processing_config` | object | No | Image processing configuration: `model_id` / `image_pipeline` (`default`, `smartocr`) / `image_pipeline_params` / `image_attrs_enabled` (compatibility field) / `image_actions` (`{ ocr: { on: [...], on_unobserved: bool } }`) / `image_vector_enabled` (image vector indexing and recall, default false, see [Knowledge Base Features](../03-features/02-knowledge-base.md)) |
| `vlm_config` | object | No | Image understanding configuration: `enabled` (default false) and `model_id` (ID of an already-created multimodal model) must both be set to enable; optional `description_language`, `custom_instructions` (≤4000 characters) |
| `storage_provider_config` | object | No | Storage configuration |
| `vector_store_id` | string | No | Vector store binding (invalid returns code 2200/2201) |
| `faq_config` / `wiki_config` / `extract_config` / `indexing_strategy` | object | No | Type-related configuration |
| `summary_model_id` | string | No | Summary model, also the default model for auto-tagging and AI description |
| `auto_tag_config` / `profile_config` | object | No | Auto-tagging, AI knowledge base description (document type only, see below) |

Response: 201 `{"success":true,"data":{KnowledgeBase}}`

Switches not specified in `indexing_strategy` keep their defaults: vector and keyword enabled, Wiki and graph disabled. Only an explicit `false` turns them off.

```bash
curl -X POST $BASE/api/v1/knowledge-bases -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"Product Docs","type":"document"}'
```

To generate OCR text or image descriptions for images or scanned documents, set `vlm_config` at the top level of the create request. `image_processing_config` is used for image processing strategy; **setting only `image_processing_config.model_id` does not enable image understanding**. For example (replace the model IDs with the corresponding models already configured in the current space):

```bash
curl -X POST "$BASE/api/v1/knowledge-bases" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{
    "name": "Scanned Documents",
    "type": "document",
    "embedding_model_id": "embedding-model-id",
    "vlm_config": {
      "enabled": true,
      "model_id": "vlm-model-id"
    }
  }'
```

An existing knowledge base can add `vlm_config` via the `PATCH /api/v1/knowledge-bases/:id` below. Configuration updates do not automatically reparse old documents; for already-imported images or scanned documents, call `POST /api/v1/knowledge/:id/reparse` again.

### POST /api/v1/knowledge-bases/:id/graph-search {#graph-search}

A standalone read-only JSON graph query. Performs a **case-sensitive substring match** on entity names against `query`, returning matching entities, their one-hop neighbors, directed relationships, and source chunks; it also returns matching entities with no relationships. There is no need to reparse Agent conversation output into structured data.

Permission: Viewer+ and read permission on the KB (owned, organization-shared, or shared Agent). API key requires `retrieve`/full, and is subject to the KB allowlist. Shared Agents may include `agent_id` / `agent_source_tenant_id`; authorization rules are the same as for KB details.

This endpoint does not call Chat, Embedding, or Rerank models, does not modify data, and does not change the default behavior of `knowledge-search` / `hybrid-search`. Natural language entity extraction, cross-KB search, and multi-hop traversal are out of scope for this endpoint.

| JSON field | Default | Limit / meaning |
| --- | --- | --- |
| `query` | Required | 1–512 characters after trimming leading/trailing whitespace; not Cypher |
| `max_nodes` | 50 | 100, including matching entities and neighbors |
| `max_relations` | 100 | 200 |
| `max_source_chunks` | 20 | 50, sources deduplicated by ID |

A limit field of `0` uses the default value; negative numbers or values exceeding the limit return 400. The request body is at most 8 KiB, accepts only a single JSON object, and unknown fields are rejected.

```bash
curl -X POST "$BASE/api/v1/knowledge-bases/$KB_ID/graph-search" \
  -H "X-API-Key: $API_KEY" -H 'Content-Type: application/json' \
  -d '{"query":"Acme","max_nodes":50,"max_relations":100,"max_source_chunks":20}'
```

Response: 200 `{"success":true,"data":{...}}`. `data` fields:

- `knowledge_base_id`, `query`: the scope and normalized query for this request.
- `entities`: `id`, `name`, `knowledge_id`, `attributes`, `source_chunk_ids`. The ID is an opaque document entity identity that does not expose the Neo4j element ID; entities with the same name but in different documents are not merged. As long as the name/owning document is unchanged, rebuilding the graph store does not change the public ID.
- `relationships`: `id`, `source_id`, `target_id`, `type`, `source_chunk_ids`. The direction is the storage direction and is not reversed based on which end the query matched; both ends must actually appear in `entities`. The source list is the union of the source chunks of both endpoint entities and does not mean that every chunk directly states the relationship.
- `source_chunks`: `id`, `knowledge_id`, `title`, `content`, `chunk_index`, `start_at`, `end_at`, `content_truncated`, `title_truncated`. Content is at most 2000 characters and title at most 512 characters; when truncated, the original position fields are preserved and it is not disguised as complete content. When there are no matches, all three lists are `[]`.
- `meta`: `truncated`, `truncation_reasons`, and the three limits in effect for this request. It is not a total count of the whole graph and does not provide a pagination cursor; to get more complete results, narrow the entity name scope.

To limit the cost of fuzzy queries, internally at most 100 name-matching seeds, 512 one-hop relationships, and 512 source chunks are examined; query execution takes at most 10 seconds. Candidate seeds are ordered by exact name match first, then name length, name, and document identity. Attributes are at most 20 items, each at most 1000 characters.

Values of `truncation_reasons` include `candidate_seeds`, `candidate_relations`, `candidate_source_chunks`, `max_nodes`, `max_relations`, `max_source_chunks`, `entity_fields`, `relation_fields`, `source_fields`. Only limits actually reached are marked; the exact number of omitted results is not guessed.

Source validation uses conservative rules: **all** chunks referenced by an entity must belong to that document in the currently authorized KB, and the document/chunk must not be deleted or disabled, the document must not be in the process of being deleted, and the chunk's current index must be ready; if any source is invalid, the entity and its related relationships are omitted. Both ends of a relationship must also belong to the same document. Graph facts that cannot be validated or were not checked due to budget are not returned as verified results. This may reduce recall for some older graphs; the query does not automatically rebuild the graph, nor can it guarantee that the facts extracted by the model are themselves correct.

Status: 400 parameter error; 401 unauthenticated; 403 no read permission/outside key scope; 404 KB does not exist; 409 KB graph not enabled; 500 source database validation failed; 503 graph not configured, connection failed, or query timed out. Errors do not include partial graph results. For deployment configuration, see [Knowledge Graph](../03-features/09-knowledge-graph.md).

### Auto-Tagging and AI Description Configuration

When creating a knowledge base, `auto_tag_config` and `profile_config` are at the top level; when updating, they go in `config.auto_tag_config` and `config.profile_config`. Both are supported only for document knowledge bases, and default to enabled=false.

`auto_tag_config` (auto-tagging):

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `enabled` | bool | false | Asynchronously select from existing tags after parsing |
| `model_id` | string | empty | When empty, uses the knowledge base `summary_model_id` |
| `max_tags` | int | 3 | Maximum number associated per document, limit 10 |
| `skip_if_tagged` | bool | true | Skip if already tagged; false allows adding tags |

When enabled, it applies to newly parsed/reparsed documents and does not automatically scan all old documents. When there are no candidate tags or no available model, ingestion is not blocked.

`profile_config` (AI knowledge base description):

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `enabled` | bool | false | When enabled, `generated_profile` is automatically refreshed when documents are added, deleted, moved, or summaries are updated |
| `model_id` | string | empty | When empty, uses the knowledge base `summary_model_id` |
| `custom_instructions` | string | empty | Additional requirements appended to the generation prompt |

`generated_profile` is a read-only field written by the system; it does not overwrite the hand-written `description`; it can also be generated immediately via `profile/generate` below. Update example:

```bash
curl -X PUT "$BASE/api/v1/knowledge-bases/kb-1" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Product Docs","config":{"auto_tag_config":{"enabled":true,"max_tags":3,"skip_if_tagged":true}}}'
```

### GET /api/v1/knowledge-bases

Purpose: list knowledge bases. Permission: Viewer+; API key `retrieve`/full.

| Query parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `agent_id` | string | No | Filter KBs visible to a shared Agent |
| `agent_source_tenant_id` | uint64 | No | When an Agent with the same name is shared by multiple spaces, specify the source space; the value is validated against the sharing relationship, and an invalid value returns 400 directly |
| `creator` | string | No | `mine` / `others` |

Response: 200 `{"success":true,"data":[KnowledgeBase],"total","page","page_size"}`

```bash
curl $BASE/api/v1/knowledge-bases -H "X-API-Key: $API_KEY"
```

### GET /api/v1/knowledge-bases/:id

Purpose: knowledge base details (shared KBs carry `my_permission`). Permission: Viewer+, KB read. Query parameter: `agent_id` (optional).

Response: 200 `{"success":true,"data":{KnowledgeBase}}`

```bash
curl $BASE/api/v1/knowledge-bases/kb-1 -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/knowledge-bases/:id

Purpose: update a knowledge base. Permission: creator OR Admin+, KB write; API key `manage_kbs`/full.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | Yes (`binding:"required"`) | Name |
| `description` | string | No | Description |
| `config` | object | No | Partial configuration update: `chunking_config`, `image_processing_config`, `faq_config`, `wiki_config`, `auto_tag_config`, `profile_config`, `indexing_strategy`. If `image_processing_config` is not passed, it stays unchanged; if passed, it is replaced entirely (fields not included, such as `image_vector_enabled`, revert to false) |
| `vlm_config` | object | No | Multimodal configuration, replaced entirely: only `enabled`, `model_id` (must be a VLLM model), `description_language`, `custom_instructions` (≤4000 characters) are taken; fields not passed are treated as empty values; do not pass it if you do not want to change it. Shared KBs require owner space or shared admin permission (editor returns 403). Legacy inline fields `model_name`/`base_url`/`api_key`/`interface_type` are not written: enabling a managed `model_id` or disabling clears the stored legacy configuration, but a pure legacy KB (no `model_id` stored, only `model_name`/`base_url`) whose GET result is returned as-is (`enabled=false` and `model_name`/`base_url` matching storage) is treated as unchanged; other KBs with `enabled=true` but missing `model_id` return 400 |

Response: 200 `{"success":true,"data":{KnowledgeBase}}`

```bash
curl -X PUT $BASE/api/v1/knowledge-bases/kb-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"Product Docs v2"}'
```

### PATCH /api/v1/knowledge-bases/:id

Purpose: modify only the settings present in the request, leaving the rest unchanged. This includes fields in name, description, models, storage, chunking, index switches, FAQ, Wiki, auto-tagging, AI description, image, speech, and graph extraction: fields not written in nested objects also keep their original values. Permission is the same as PUT; when modifying the embedding model, summary model, multimodal, or speech, shared knowledge bases also require admin sharing permission. The storage instance can only be modified by the space that owns the knowledge base, and if the knowledge base already has files, storage must be migrated first; a shared admin can return the current storage ID as-is but cannot change it. `embedding_model_id` cannot be changed when the knowledge base already has documents. `vector_store_id` can only be specified at creation. If `name` is passed, it cannot be empty; passing an empty string for `description` clears the description.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | No | New name |
| `description` | string | No | New description; empty string clears it |
| `embedding_model_id` / `summary_model_id` / `storage_backend_id` | string | No | Model or storage instance |
| `chunking_config` | object | No | Only overrides the chunking fields written |
| `indexing_strategy` | object | No | Only overrides the switches written; at least one index must remain |
| `vlm_config` / `asr_config` / `image_processing_config` | object | No | Multimodal, speech, image |
| `faq_config` / `wiki_config` / `auto_tag_config` / `profile_config` | object | No | Type-related settings |
| `extract_config` / `question_generation_config` | object | No | Graph extraction, chunk question generation |

Response: 200 `{"success":true,"data":{KnowledgeBase}}`

```bash
curl -X PATCH $BASE/api/v1/knowledge-bases/kb-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"Product Docs v2"}'
```

### DELETE /api/v1/knowledge-bases/:id

Purpose: delete a knowledge base (locked to the owning space + Admin; shared editors cannot delete). Permission: creator OR Admin+, KB write; API key `manage_kbs`/full.

Response: 200 `{"success":true,"message":"Knowledge base deleted successfully"}`

```bash
curl -X DELETE $BASE/api/v1/knowledge-bases/kb-1 -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/knowledge-bases/:id/pin

Purpose: pin/unpin (stored per user). Permission: Viewer+, KB read. No request body.

Response: 200 `{"success":true,"data":{KnowledgeBase(is_pinned toggled)}}`

```bash
curl -X PUT $BASE/api/v1/knowledge-bases/kb-1/pin -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/knowledge-bases/:id/hybrid-search (GET compatible)

Purpose: low-level recall within a KB (vector + keyword), no rerank by default, returns recall scores; rerank can optionally be enabled. Suitable for scenarios that need control over raw recall, such as evaluating recall or passing precomputed vectors; for general retrieval use [`knowledge-search`](./02-api-chat.md), see [How to Choose a Retrieval Endpoint](./01-api-overview.md#retrieval-api). Permission: Viewer+, KB read; API key `retrieve`/full. GET carrying a JSON body is only for backward compatibility (#1727); POST is recommended.

Query parameter: `resource_urls=handle|public` (`public` replaces `resource://` in the result `content` / `image_info` with loadable direct links, see [API Overview](./01-api-overview.md)).

Request body (`types.SearchParams`):

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `query_text` | string | Conditionally required | Query text (unless `query_embedding` is provided; required when rerank is enabled) |
| `query_embedding` | []float32 | No | Precomputed vector |
| `vector_threshold` / `keyword_threshold` | float64 | No | Match thresholds |
| `match_count` | int | No | Maximum number of results (default 50) |
| `disable_keywords_match` / `disable_vector_match` | bool | No | Disable one recall path |
| `knowledge_base_ids` | []string | No | Retrieve across multiple knowledge bases at once; the `:id` in the path must be among them; these knowledge bases must have the same embedding model, otherwise 400 is returned |
| `knowledge_ids` | []string | No | Limit to knowledge entries |
| `tag_ids` | []string | No | Tag filter (OR) |
| `only_recommended` | bool | No | FAQ only recommended entries |
| `skip_context_enrichment` | bool | No | Skip parent chunk/context enrichment |
| `rerank` | object | No | Passing it enables rerank (`{}` uses the space-configured model), fields see [rerank object](./01-api-overview.md#retrieval-api) |

Response: 200 `{"success":true,"data":[SearchResult]}`; with `rerank`, an additional `meta.rerank` (see [meta.rerank diagnostics](./01-api-overview.md#retrieval-api)).

```bash
curl -X POST "$BASE/api/v1/knowledge-bases/kb-1/hybrid-search?resource_urls=public" -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' -d '{"query_text":"refund process","match_count":5}'

# Fix recall parameters, then rerank with the specified model
curl -X POST $BASE/api/v1/knowledge-bases/kb-1/hybrid-search -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"query_text":"refund process","vector_threshold":0.3,"match_count":5,"rerank":{"model_id":"rr-1","threshold":0.2}}'
```

### POST /api/v1/knowledge-bases/copy

Purpose: copy content across KBs (asynchronous task). Permission: Contributor+; API key `manage_kbs`/full (source/target KB allowlist validated in the handler).

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `source_id` | string | Yes (`binding:"required"`) | Source KB |
| `target_id` | string | No | Target KB (auto-created if empty) |
| `task_id` | string | No | Custom task ID |

Response: 200 `{"success":true,"data":{"task_id","source_id","target_id","message"}}`

```bash
curl -X POST $BASE/api/v1/knowledge-bases/copy -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"source_id":"kb-1"}'
```

### POST /api/v1/knowledge-bases/:id/duplicate

Purpose: create a KB copy (copies settings only, not content/index/sharing). Permission: Contributor+, source KB read; API key `manage_kbs`/full. No request body.

Response: 201 `{"success":true,"data":{"source_id","target_id","message","knowledge_base":{...}}}`

```bash
curl -X POST $BASE/api/v1/knowledge-bases/kb-1/duplicate -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/knowledge-bases/:id/profile/generate

Purpose: immediately regenerate the knowledge base's AI description (`generated_profile`), synchronously performing one document profile aggregation and one small model call, without modifying the hand-written `description`. Permission: same as updating a knowledge base (creator/Admin and KB write); API key `manage_kbs`/full. No request body. Document type only; returns 400 if no model is configured.

Response: 200 `{"success":true,"data":{"gist","topics":[...],"typical_questions":[...],"stats":{"document_count",...},"status":"ready","model_id","generated_at"}}`

```bash
curl -X POST $BASE/api/v1/knowledge-bases/kb-1/profile/generate -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledge-bases/copy/progress/:task_id

Purpose: query copy progress (tasks are isolated by space). Permission: Viewer+; API key `retrieve`/`manage_kbs`/full.

Response: 200 `{"success":true,"data":{status,progress,message,...}}`

```bash
curl $BASE/api/v1/knowledge-bases/copy/progress/task-1 -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledge-bases/:id/move-targets

Purpose: list KBs that can be move targets (same type/same embedding). Permission: Viewer+, KB read.

Response: 200 `{"success":true,"data":[KnowledgeBase]}`

```bash
curl $BASE/api/v1/knowledge-bases/kb-1/move-targets -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledge-bases/:id/files

Purpose: KB-scoped file proxy (renders images in shared KB content; the context tenant has been rewritten to the KB owner). Permission: Viewer+, KB read; KB-restricted keys are rejected, full-space `retrieve`/full keys are allowed. Registered in `serveKBScopedFiles` (`internal/router/files.go`).

| Query parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `file_path` | string | Yes | `provider://...` storage path (`..` forbidden) |

Response: 200 file stream (`Content-Type` inferred from extension; `Cache-Control: private`).

```bash
curl "$BASE/api/v1/knowledge-bases/kb-1/files?file_path=local://1/exports/chart.png" \
  -H "Authorization: Bearer $TOKEN" -o chart.png
```

## Knowledge (KB Content, /api/v1/knowledge-bases/:id/knowledge and /api/v1/knowledge)

### POST /api/v1/knowledge-bases/:id/knowledge/file

Purpose: upload a file to create knowledge. Permission: KB creator OR Admin+, KB write; API key `ingest`/full. Handler: `internal/handler/knowledge.go`

multipart/form-data fields:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `file` | file | Yes | Uploaded file |
| `fileName` | string | No | Custom file name or relative path including directories, e.g. `docs/spec/design.md`; the directory part is used for categorization, and the last segment is used as the file name |
| `metadata` | JSON string | No | Custom metadata |
| `enable_multimodel` | bool | No | Multimodal processing switch |
| `tag_ids` | string | No | Comma-separated tag IDs |
| `channel` | string | No | Ingestion channel |
| `process_config` | JSON string | No | Parse configuration overrides (KnowledgeProcessOverrides), see table below |

Common `process_config` fields (all optional; when omitted, the knowledge base configuration is used):

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `summary_enabled` | bool | true | Whether to generate a summary for the documents imported this time; when disabled, parsing, indexing, and other processing proceed as usual |
| `parser_engine_rules` | []object | KB configuration | Specify the parser engine by file type |
| `parser_engine_overrides` | map[string]string | empty | Engine parameters, e.g. `pdf_force_scanned` |
| `chunking_config` | object | KB configuration | Chunking parameters |
| `enable_multimodel` / `vlm_config` / `asr_config` | - | KB configuration | Multimodal and speech recognition |
| `question_generation_config` | object | KB configuration | Question generation |
| `graph_enabled` / `extract_config` | - | KB configuration | Graph extraction |

Response: 200 `{"success":true,"data":{Knowledge}}`; duplicate files return 409 with `data` being the existing Knowledge. Files with the same name that are being deleted or failed to parse are not counted as duplicates.

```bash
curl -X POST $BASE/api/v1/knowledge-bases/kb-1/knowledge/file \
  -H "X-API-Key: $API_KEY" -F 'file=@./manual.pdf' -F 'enable_multimodel=true'
```

**Uploading to a specified folder:** pass a path relative to the knowledge base root in `fileName`, using `/` to separate directories, and keep the file name and extension. The upload endpoint does not accept a separate `folder_path` field.

```bash
curl -X POST "$BASE/api/v1/knowledge-bases/kb-1/knowledge/file" \
  -H "X-API-Key: $API_KEY" \
  -F 'file=@./design.md' \
  -F 'fileName=docs/spec/design.md'
```

The above request categorizes the document under `docs/spec`, and the returned Knowledge has `folder_path` as `docs/spec` and `file_name` as `design.md`. `file=@./design.md` specifies the local file, and `fileName` specifies the categorization path in the knowledge base. If `fileName` is omitted or only a file name is passed, the document is located in the knowledge base root.

The directories of a document knowledge base are derived from the documents' `folder_path` and do not need to be created in advance: after uploading to a new path, the directory and its intermediate levels appear in the directory tree. Currently, independently creating empty folders that contain no documents or subdirectory documents is not supported. The Wiki create-directory endpoint manages Wiki directories and is not used for document knowledge bases.

After uploading, you can view the directory tree with `GET /api/v1/knowledge-bases/:id/knowledge/folders` on this page. Existing documents can be moved to a new path via `POST /api/v1/knowledge/folder` without re-uploading; that move endpoint is the one that uses the `folder_path` request field.

### POST /api/v1/knowledge-bases/:id/knowledge/url

Purpose: create knowledge by fetching from a URL. Permission/API key same as above.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `url` | string | Yes (`binding:"required"`) | Fetch address |
| `file_name` / `file_type` / `title` | string | No | Override information |
| `enable_multimodel` | *bool | No | Multimodal switch |
| `tag_ids` | []string | No | Tags |
| `channel` | string | No | Channel |
| `process_config` | object | No | Parse overrides |

Response: 201 `{"success":true,"data":{Knowledge}}`; duplicate URLs return 409.

```bash
curl -X POST $BASE/api/v1/knowledge-bases/kb-1/knowledge/url -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' -d '{"url":"https://example.com/doc"}'
```

### POST /api/v1/knowledge-bases/:id/knowledge/manual

Purpose: create manual (Markdown) knowledge. Permission/API key same as above.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `title` | string | No | Title |
| `content` | string | No | Markdown content |
| `status` | string | No | `draft` / `publish` |
| `tag_ids` | []string | No | Tags |
| `channel` | string | No | Channel |
| `process_config` | object | No | Parse overrides |

Response: 200 `{"success":true,"data":{Knowledge}}`

```bash
curl -X POST $BASE/api/v1/knowledge-bases/kb-1/knowledge/manual -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"title":"FAQ Summary","content":"# Content","status":"publish"}'
```

### GET /api/v1/knowledge-bases/:id/knowledge

Purpose: list knowledge under a KB. Permission: Viewer+, KB read; API key `retrieve`/full.

| Query parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `page` / `page_size` | int | No | Pagination (default 1/20) |
| `tag_ids` | string | No | Comma-separated tags (OR) |
| `keyword` | string | No | Keyword |
| `file_type` | string | No | File type filter |
| `parse_status` | string | No | `pending/processing/completed/failed` |
| `source` | string | No | Channel or `manual`/`url` |
| `start_time` / `end_time` | string | No | RFC3339, filtered by `updated_at` |
| `folder_path` | string | No | Filter by folder; empty string means the knowledge base root, and not passing it means no folder filter |
| `folder_recursive` | bool | No | Used with `folder_path`; when `true`, includes documents in subfolders |
| `sort_by` | string | No | Sort field: `updated_at`, `created_at`, or `file_name`; default `created_at` |
| `sort_order` | string | No | Sort direction: `asc` or `desc`; default `desc` |

When no sort parameters are passed, sorts by `created_at desc`; values outside the above range return 400. When using `updated_at`, reparsing, editing, or status changes affect the order; when using `file_name`, sorts by display file name case-insensitively, and empty file names fall back to title and then source in order. Equal sort values are ordered by knowledge ID to ensure stable pagination results.

Response: 200 `{"success":true,"data":[Knowledge],"total","page","page_size"}`

```bash
curl "$BASE/api/v1/knowledge-bases/kb-1/knowledge?page=1&parse_status=completed" -H "X-API-Key: $API_KEY"
```

### POST /api/v1/knowledge-bases/:id/knowledge/batch-download

Purpose: package the original files of multiple documents in the same knowledge base into a ZIP for download. Permission is the same as single-file download: Contributor+ and KB write (organization-shared Viewers cannot download); API key `retrieve`/full.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `ids` | []string | Yes | List of knowledge IDs, 1–200 |

Behavior:

- The total size of original files must not exceed 512 MiB; exceeding returns 400;
- Entries without original files (such as web imports) are skipped; if none of the selected entries have original files, returns 400;
- The ZIP preserves the knowledge base folder structure, and duplicate file names are automatically suffixed with a number;
- If any ID does not exist or does not belong to the knowledge base, returns 404; read failure returns 500, and no archive missing files is generated;
- The same instance processes at most 4 batch downloads simultaneously; exceeding returns 429.

Response: 200 `application/zip` file stream, with a file name like `knowledge-files-20260923-150405.zip`.

```bash
curl -X POST $BASE/api/v1/knowledge-bases/kb-1/knowledge/batch-download \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"ids":["k-1","k-2"]}' -o knowledge-files.zip
```

### GET /api/v1/knowledge-bases/:id/knowledge/folders

Purpose: get the folder directory tree of the knowledge base. The directory structure is preserved when uploading whole directories (since migration `000079` the `knowledges.folder_path` column exists, and paths in historical `file_name` have been backfilled into this field). Permission: Viewer+ + KBAccessRead.

The directory tree is generated by aggregating the `folder_path` of knowledge entries; intermediate directories that have no direct documents but contain subdirectory documents are also returned. After the last document under a directory and all subdirectory documents are moved away, the directory no longer appears in the tree.

Response: 200. Example (with only the above-uploaded `docs/spec/design.md`):

```json
{
  "success": true,
  "data": {
    "root_document_count": 0,
    "total_document_count": 1,
    "folders": [{
      "path": "docs",
      "name": "docs",
      "document_count": 0,
      "total_count": 1,
      "children": [{
        "path": "docs/spec",
        "name": "spec",
        "document_count": 1,
        "total_count": 1
      }]
    }]
  }
}
```

`root_document_count` is the number of documents in the knowledge base root, and `total_document_count` is the total number of documents in the knowledge base; a directory node's `document_count` counts only direct documents, while `total_count` includes all subdirectory documents.

```bash
curl $BASE/api/v1/knowledge-bases/kb-1/knowledge/folders -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/knowledge-bases/:id/knowledge/folders

Purpose: rename or move a folder, changing the path of all its subdirectories together. If the target path already exists, the two folders are merged; moving into its own subdirectory is not allowed. Permission: KB owner or Admin+ + KBAccessWrite.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `from` | string | Yes | Original path |
| `to` | string | Yes | New path |

Response: 200 `{"success":true}`

```bash
curl -X PUT $BASE/api/v1/knowledge-bases/kb-1/knowledge/folders -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"from":"Design Docs/Old Version","to":"Archive/Design Docs"}'
```

### DELETE /api/v1/knowledge-bases/:id/knowledge

Purpose: clear all content of the KB (destructive). Permission: Admin+, KB write; API key full-access only.

Response: 200 `{"success":true,"message":"Knowledge base contents clear task submitted","data":{"deleted_count":N}}`

```bash
curl -X DELETE $BASE/api/v1/knowledge-bases/kb-1/knowledge -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledge/batch

Purpose: batch get knowledge by ID (across KBs, the handler validates access itself). Permission: Viewer+; API key `retrieve`/full.

| Query parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `ids` | []string | Yes | Knowledge IDs (can be passed repeatedly or comma-separated) |
| `kb_id` | string | No | Limit to KB |
| `agent_id` | string | No | Shared Agent scope |
| `agent_source_tenant_id` | uint64 | No | Source space selector for shared Agents, validated against the sharing relationship |

Response: 200 `{"success":true,"data":[Knowledge]}`. Knowledge in `pending`/`processing`/`finalizing` additionally carries `last_activity_at` (RFC3339), which is the later of the row's `updated_at` and the most recent write of all spans of that knowledge. Knowledge with no progress for more than 20 minutes additionally carries `stall_state`: `queued` means there are still tasks waiting in the asynq queue or the Wiki persistent queue (backlog), `stalled` means there are no tasks that can advance it (suspected stuck). The determination is the same as the housekeeping backlog determination; the queue side is a full-queue scan, shared by all requests and cached for 60 seconds. If probing fails, `stall_state` is not returned, and the frontend displays it as normal parsing.

```bash
curl "$BASE/api/v1/knowledge/batch?ids=k-1&ids=k-2" -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledge/:id

Purpose: knowledge details. Permission: Viewer+, parent KB read.

Response: 200 `{"success":true,"data":{Knowledge}}`

```bash
curl $BASE/api/v1/knowledge/k-1 -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledge/:id/stages and GET /api/v1/knowledge/:id/spans

Purpose: parse stages/trace (both paths use the same handler `GetKnowledgeSpans`). Permission: Viewer+, parent KB read. Query parameter: `attempt` (int, 0=latest).

Response: 200 `{"success":true,"data":{"knowledge_id","attempt","latest_attempt","parse_status","current_stage","last_activity_at","stall_state","trace":{...},"last_error":{...}}}`

`last_activity_at` is returned only while parsing is in progress, and is the later of the row's `updated_at` and the most recent write of each span in this attempt; `stall_state` has the same meaning as above. `current_stage` is the stage still running; when there is no running stage (e.g. `finalizing`, where post-processing stages are closed but subtasks such as summarization are still running), it takes the stage to which the still-running sub-spans belong. For knowledge determined by housekeeping to be stuck, the span at the stuck position is marked as failed with `TASK_STALLED`, and `last_error` points to it first.

```bash
curl $BASE/api/v1/knowledge/k-1/spans -H "Authorization: Bearer $TOKEN"
```

### DELETE /api/v1/knowledge/:id

Purpose: delete knowledge (asynchronous). Permission: KB creator OR Admin+, KB write; API key `ingest`/full.

Response: 200 `{"success":true,"message":"Delete task submitted","data":{"task_id"}}`

```bash
curl -X DELETE $BASE/api/v1/knowledge/k-1 -H "X-API-Key: $API_KEY"
```

### PUT /api/v1/knowledge/:id

Purpose: update knowledge metadata. Permission same as above. Request body (`types.Knowledge` subset): `title`, `description`, `tags`, `custom_metadata` (all optional). If description is omitted, the original summary is kept; an explicit empty string clears the summary; a non-empty value saves a manual summary; the UI can edit it on the document content page.

`custom_metadata` is user-supplied descriptive metadata (stored separately from the system-internal `metadata`, migration `000078`); validation rules are in `internal/application/service/knowledge.go`:

| Constraint | Value |
| --- | --- |
| Number of fields | ≤ 20 |
| Key length | 1-64 characters, cannot be blank |
| Value type | string / number / boolean / null |
| Value length | ≤ 1000 characters |

Full overwrite update (the passed object replaces the original object). When metadata changes and the document already has a summary, a summary refresh is automatically enqueued (`summary_status` becomes `pending`). Metadata text participates in summary generation and document-level model context (`Knowledge.CustomMetadataText()`).

Response: 200 `{"success":true,"message":"Knowledge updated successfully","data":{Knowledge}}`

```bash
curl -X PUT $BASE/api/v1/knowledge/k-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"title":"New Title","custom_metadata":{"Department":"R&D Center","Classification":"Internal","Version":3}}'
```

### POST /api/v1/knowledge/:id/regenerate-summary

Purpose: regenerate the document's summary after chunk content or custom metadata has been edited. Permission: KB owner or Admin+, and write permission on the parent KB.

Behavior has two cases: when the document previously had no summary (`summary_status` empty or `none`), a generation is triggered synchronously; when a summary already exists, a refresh task is enqueued instead, `summary_status` becomes `pending`, and it is executed asynchronously by `knowledge_summary_refresh.go`.

Response: 200 `{"success":true,"data":{Knowledge}}`

```bash
curl -X POST $BASE/api/v1/knowledge/k-1/regenerate-summary -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/knowledge/manual/:id

Purpose: replace the entire manually entered Markdown. Only applies to manual knowledge; uploaded files cannot use this endpoint to change the body. `content` is the complete body, and empty content is rejected. `status` is `draft` (not searchable) or `publish` (indexed); if `status` is not passed, it is saved as a draft. Permission same as above.

Response: 200 `{"success":true,"data":{Knowledge}}`

```bash
curl -X PUT $BASE/api/v1/knowledge/manual/k-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"content":"# Updated Content","status":"publish"}'
```

### POST /api/v1/knowledge/:id/reparse

Purpose: reparse knowledge. Permission same as above. Request body (optional): `{"process_config":{...}}`.

Response: 200 `{"success":true,"message":"Reparse task submitted","data":{Knowledge}}`

```bash
curl -X POST $BASE/api/v1/knowledge/k-1/reparse -H "X-API-Key: $API_KEY"
```

### POST /api/v1/knowledge/:id/cancel-parse

Purpose: cancel parsing. Permission same as above. No request body.

Response: 200 `{"success":true,"message":"Knowledge parse cancelled","data":{Knowledge}}`

```bash
curl -X POST $BASE/api/v1/knowledge/k-1/cancel-parse -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledge/:id/download

Purpose: download the original source file (stricter than preview: Contributor+ and KB write; organization-shared Viewers cannot download source files). API key `retrieve`/full.

Response: 200 binary stream (`application/octet-stream`).

```bash
curl -OJ $BASE/api/v1/knowledge/k-1/download -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledge/:id/preview

Purpose: preview the parsed file content. Permission: Viewer+, KB read.

Response: 200 preview stream (text/HTML).

```bash
curl $BASE/api/v1/knowledge/k-1/preview -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/knowledge/image/:id/:chunk_id

Purpose: update the image information of a chunk (caption/OCR, etc.). Permission: KB creator OR Admin+, KB write. Path parameters: `id` knowledge ID, `chunk_id` chunk ID. Request body is image information JSON.

Response: 200 `{"success":true,...}`

```bash
curl -X PUT $BASE/api/v1/knowledge/image/k-1/c-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"caption":"Architecture diagram"}'
```

### GET /api/v1/knowledge/search

Purpose: cross-KB file search (session @file selector). Permission: Viewer+; API key `retrieve`/full.

| Query parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `keyword` / `query` | string | Conditionally required | Keyword (the two are equivalent); when empty, `recent=true` must be passed, otherwise 400 is returned |
| `file_types` | string | No | Comma-separated extension filter, e.g. `csv,xlsx` |
| `offset` / `limit` | int | No | Pagination; `limit` defaults to 20, range 1–100 |
| `recent` | bool | No | Return recent files when the keyword is empty |
| `agent_id` | string | No | Shared Agent scope |
| `agent_source_tenant_id` | uint64 | No | Source space selector for shared Agents, validated against the sharing relationship |

Response: 200 `{"success":true,"data":[Knowledge],"has_more":bool,"total":N}`

```bash
curl "$BASE/api/v1/knowledge/search?keyword=report&limit=20" -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledge/move/progress/:task_id

Purpose: query move task progress. Permission: Viewer+; API key `retrieve`/full.

Response: 200 `{"success":true,"data":{MoveProgress}}`

```bash
curl $BASE/api/v1/knowledge/move/progress/task-1 -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/knowledge/tags

Purpose: batch update knowledge tags. Permission: Contributor+; API key `ingest`/full (KB allowlist validated in the handler). The two forms are mutually exclusive: `updates`, or `knowledge_ids` plus `tag_ids`.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `updates` | map[string][]string | One of two with `knowledge_ids` | knowledge_id → tag_ids, replaces the document's existing tags |
| `kb_id` | string | No | Limit to knowledge base; when not passed, inferred from the first knowledge |
| `knowledge_ids` | []string | One of two with `updates` | At most 20 documents, same set of tags |
| `tag_ids` | []string | Required when using `knowledge_ids` | At most 50; an empty array clears tags |

Response: 200 `{"success":true}`

```bash
curl -X PUT $BASE/api/v1/knowledge/tags -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"updates":{"k-1":["t-1"]},"kb_id":"kb-1"}'
```

### POST /api/v1/knowledge/batch-reparse

Purpose: batch reparse. Permission: Contributor+; API key `ingest`/full.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `kb_id` | string | Yes (`binding:"required"`) | KB ID |
| `ids` | []string | Yes (`binding:"required"`) | List of knowledge IDs |
| `process_config` | object | No | Parse overrides |

Response: 200 `{"success":true,"message":"Batch reparse task submitted","data":{"task_id"}}`

```bash
curl -X POST $BASE/api/v1/knowledge/batch-reparse -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' -d '{"kb_id":"kb-1","ids":["k-1","k-2"]}'
```

### POST /api/v1/knowledge/batch-delete

Purpose: batch delete (≤200). Permission: Contributor+; API key `ingest`/full.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `kb_id` | string | Yes (`binding:"required"`) | KB ID |
| `ids` | []string | Yes (`binding:"required"`) | List of knowledge IDs (≤200) |

Response: 200 `{"success":true,"message":"Batch delete task submitted","data":{"task_id","deleted_count"}}`

```bash
curl -X POST $BASE/api/v1/knowledge/batch-delete -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"kb_id":"kb-1","ids":["k-1"]}'
```

### POST /api/v1/knowledge/folder

Purpose: categorize several documents into a specified folder (only changes categorization, does not change knowledge base ownership, and does not reparse). Permission: Contributor+ / API key `ingest`.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `kb_id` | string | Yes | Knowledge base ID |
| `knowledge_ids` | []string | Yes | Documents to move |
| `folder_path` | string | No | Target folder path relative to the knowledge base root, e.g. `docs/spec`; new paths do not need to be created in advance, and an empty string means move back to the knowledge base root |

Response: 200 `{"success":true}`

```bash
curl -X POST $BASE/api/v1/knowledge/folder -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"kb_id":"kb-1","knowledge_ids":["k-1","k-2"],"folder_path":"Design Docs"}'
```

### POST /api/v1/knowledge/move

Purpose: move knowledge across KBs (asynchronous). Permission: Contributor+; API key `ingest`/full (both source and target KBs must be in the allowlist).

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `knowledge_ids` | []string | Yes (`binding:"required,min=1"`) | Knowledge to move |
| `source_kb_id` | string | Yes (`binding:"required"`) | Source KB |
| `target_kb_id` | string | Yes (`binding:"required"`) | Target KB |
| `mode` | string | Yes (`binding:"required,oneof=reuse_vectors reparse"`) | Reuse vectors or reparse |

Response: 200 `{"success":true,"data":{"task_id","source_kb_id","target_kb_id","knowledge_count","message"}}`

```bash
curl -X POST $BASE/api/v1/knowledge/move -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"knowledge_ids":["k-1"],"source_kb_id":"kb-1","target_kb_id":"kb-2","mode":"reuse_vectors"}'
```

## Gallery and Image Attributes {#image-gallery-api}

Gallery reads require Viewer+ and the target knowledge base to be readable; API Key requires `retrieve` or full, and complies with the knowledge base scope. The gallery and the image vector switch are independent; the list returns the descriptions, OCR, and sources of extracted images.

### GET /api/v1/knowledge-bases/:id/gallery-config

Returns `{"success":true,"data":{"attribute_sources":[...],"attributes":[...],"mode":"...","status":{...}}}`. Clients construct search, filtering, and sorting based on the returned attribute definitions and usage policies; do not hardcode available attributes. Attribute IDs carry a source prefix, e.g. `builtin:caption`, `system:contain.text`.

### GET /api/v1/knowledge-bases/:id/images

| Query parameter | Description |
| --- | --- |
| `page` / `page_size` | Default 1 / 20, page_size max 1000 |
| `keyword` / `search_in` | Case-insensitive substring search; search_in is a comma-separated list of searchable attribute IDs, defaulting to description and OCR |
| `sort_by` / `sort_order` | Sortable attribute ID, default created_at / desc; compatible with legacy values created_at, updated_at, caption |
| `knowledge_ids` | Comma-separated source document IDs; GET takes only the first 50, use POST for more documents |
| `is_enabled` | true / false, filter by the enabled status of the owning chunk |
| `attr_filters` | JSON object, attribute ID → array of allowed values, OR within the same attribute, AND across different attributes |
| `attr_rules` | JSON object, attribute ID → value → on/off; on forces display and takes precedence over off, and images not matching any rule are kept |

Only search, filter, and sort fields declared applicable by gallery-config are accepted; inapplicable fields are removed. Response `{"success":true,"data":[ImageAsset],"total":N,"page":1,"page_size":20}`. Each item includes `id`, `chunk_id`, `knowledge_id`, `source_name`, `url`, `original_url`, `caption`, `ocr_text`, `attrs`, `is_enabled`, `status`, and timestamps.

### POST /api/v1/knowledge-bases/:id/images

The same read-only query as GET, with parameters moved to the JSON body; `knowledge_ids` and `search_in` use string arrays, and `attr_filters` / `attr_rules` are passed directly as objects. At most 1000 source documents; exceeding returns 400. Suitable for selecting all source filters, avoiding overly long URLs.

```bash
curl -X POST "$BASE/api/v1/knowledge-bases/kb-1/images" -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"page":1,"page_size":20,"knowledge_ids":["k-1","k-2"],"keyword":"architecture"}'
```

### GET /api/v1/image-attrs/schema

A global read-only registry of image observation attributes, requires Viewer+, API Key requires retrieve/full, no path knowledge base. The knowledge base editor uses it to configure image observation and OCR conditions; the actual search/filter usage of the gallery is still based on each knowledge base's gallery-config.

## Implementation Reference

Route registration: `RegisterKnowledgeBaseRoutes`, `RegisterKnowledgeRoutes` in `internal/router/routes_knowledge.go`. Handlers: `internal/handler/knowledgebase.go`, `internal/handler/knowledge.go`.
