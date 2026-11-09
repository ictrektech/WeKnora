# API Reference: Chunks and Tags

A chunk is the smallest unit of retrieval, and tags are used to classify documents. Both sets of endpoints belong to the knowledge base resource and share the same permission rules as [Knowledge Bases and Knowledge](./02-api-knowledge.md): reads require Viewer+ and read permission on the parent KB (API key `retrieve`), writes require "KB creator OR Admin+" and write permission (API key `ingest`), and both are subject to the API key's KB allowlist.

For general conventions (Base URL, authentication, error codes, pagination), see the [API Overview](./01-api-overview.md).

## Chunks (/api/v1/chunks)

Handler: `internal/handler/chunk.go`. Read: Viewer+ and parent KB read (API key `retrieve`/full); write: KB creator OR Admin+ and parent KB write (API key `ingest`/full).

### GET /api/v1/chunks/:knowledge_id

Purpose: list the chunks of a knowledge item.

| Query parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `page` | int | No | Defaults to 1 |
| `page_size` | int | No | Defaults to 10, maximum 100 |
| `chunk_type` | string | No | Repeatable; filters by chunk type |

Response: 200 `{"success":true,"data":[Chunk],"total","page","page_size"}`

```bash
curl "$BASE/api/v1/chunks/k-1?page=1" -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/chunks/by-id/:id

Purpose: get a single chunk by chunk ID (no knowledge_id required).

Response: 200 `{"success":true,"data":{Chunk}}`. `Chunk.source_locators` is the chunk's position in the original file; for the structure, see the `source_locators` description in the [API Overview](01-api-overview.md).

```bash
curl $BASE/api/v1/chunks/by-id/c-1 -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/chunks/:knowledge_id/:id

Purpose: edit a chunk's content or enabled/disabled state (since migration `000078`, this is a versioned optimistic update).

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `content` | string | No | New content; after trimming leading and trailing whitespace it must not be empty; maximum length 200000 bytes |
| `is_enabled` | bool | No | Enable/disable the chunk |
| `expected_revision` | int | No | The expected `content_revision`, used for optimistic concurrency control |

Constraints and side effects:

- `text`, `image_ocr`, and `image_caption` are editable; other types such as `image_vector` are still not directly editable;
- If `expected_revision` does not match the current `content_revision`, returns **409** (`Chunk was modified by another user; refresh and retry`);
- Introducing image URLs that are not present in the source content during editing is not allowed; deleting a Markdown image also disables the corresponding OCR/caption sub-chunks;
- After a successful edit, `content_revision` is incremented by 1, the old version is written to the `chunk_revisions` table, and `index_status` goes through `processing` → `ready` in sequence; if rebuilding the retrieval index fails, the row is still saved but `index_status = failed`, and submitting the same content again can trigger a retry;
- Editing a text sub-chunk writes back to the parent chunk's content according to the offset (the parent chunk's `source_content` remains immutable); editing an image OCR/description does not rewrite the source text, nor does it regenerate the original image vector;
- Image edits synchronously update the `image_info` copies of the same image within the same document in the same transaction; the gallery and retrieval use the corrected text. Images in other documents are edited independently;
- A change to content or enabled/disabled state enqueues a document summary refresh.

Response: 200 `{"success":true,"data":{Chunk},"summary_status":"pending","description":"..."}`

```bash
curl -X PUT $BASE/api/v1/chunks/k-1/c-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"content":"Corrected content","expected_revision":2}'
```

After opening image details in the gallery, users with edit permission can select "Edit" under the existing "Image Description" or "Image Text". Saving reuses the version check and index synchronization flow described above; if indexing fails, indexing can be retried, and on a version conflict the latest data must be re-read first. This entry point is used to correct existing OCR/descriptions and does not create image chunks that have not yet been generated.

![Editing an image description in the gallery (example image and local preview endpoint)](/screenshots/gallery-image-edit.png)

### GET /api/v1/chunks/:knowledge_id/:id/revisions

Purpose: list a chunk's historical versions (the `chunk_revisions` table, in descending revision order).

Response: 200 `{"success":true,"data":[{ChunkRevision}]}`, where each entry contains `revision`, `content`, `is_enabled`, `editor_id`, `edit_source`, `edited_at`.

```bash
curl $BASE/api/v1/chunks/k-1/c-1/revisions -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/chunks/:knowledge_id/:id/revert

Purpose: revert to a historical version. The revert itself is also a new edit: `content_revision` continues to increment, and the current content is saved as a new historical version.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `revision` | int | Yes | Target historical revision number (non-negative) |
| `expected_revision` | int | No | Optimistic lock, semantics as above; a conflict returns 409 |

Response: 200 `{"success":true,"data":{Chunk},"summary_status":"...","description":"..."}`

```bash
curl -X POST $BASE/api/v1/chunks/k-1/c-1/revert -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"revision":1}'
```

### DELETE /api/v1/chunks/:knowledge_id/:id

Purpose: delete a single chunk.

Response: 200 `{"success":true,"message":"Chunk deleted"}`

```bash
curl -X DELETE $BASE/api/v1/chunks/k-1/c-1 -H "Authorization: Bearer $TOKEN"
```

### DELETE /api/v1/chunks/:knowledge_id

Purpose: delete all chunks under a knowledge item.

Response: 200 `{"success":true,"message":"All chunks under knowledge deleted"}`

```bash
curl -X DELETE $BASE/api/v1/chunks/k-1 -H "Authorization: Bearer $TOKEN"
```

### DELETE /api/v1/chunks/by-id/:id/questions

Purpose: delete a generated question under the chunk. Request body: `{"question_id":"..."}` (`binding:"required"`).

Response: 200 `{"success":true,"message":"Generated question deleted"}`

```bash
curl -X DELETE $BASE/api/v1/chunks/by-id/c-1/questions -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"question_id":"q-1"}'
```

### PUT /api/v1/chunks/by-id/:id/questions

Purpose: add or modify a generated question for the chunk. Request body: `{"question":"...","question_id":"..."}`, `question` is required; leaving `question_id` empty means adding a new one.

Response: 200 `{"success":true,"data":{GeneratedQuestion}}`

```bash
curl -X PUT $BASE/api/v1/chunks/by-id/c-1/questions -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"question_id":"q-1","question":"How do I configure a vector store in WeKnora?"}'
```

### POST /api/v1/chunks/by-id/:id/questions/regenerate

Purpose: regenerate retrieval questions based on the chunk's current content. After content is edited, existing questions are not deleted but marked as "stale" (the revision does not match the current body); this endpoint can be used to refresh them.

Response: 200 `{"success":true,"data":[{GeneratedQuestion}]}`

```bash
curl -X POST $BASE/api/v1/chunks/by-id/c-1/questions/regenerate -H "Authorization: Bearer $TOKEN"
```

## Tags (/api/v1/knowledge-bases/:id/tags)

Handler: `internal/handler/tag.go`. Read: Viewer+ + KB read (API key `retrieve`/full); write: KB creator OR Admin+ + KB write (API key `ingest`/full).

### GET /api/v1/knowledge-bases/:id/tags

Purpose: list tags. Query parameters: `page`, `page_size`, `keyword` (all optional).

Response: 200 `{"success":true,"data":[KnowledgeTag]}`

```bash
curl $BASE/api/v1/knowledge-bases/kb-1/tags -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/knowledge-bases/:id/tags

Purpose: create a tag.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | Yes (`binding:"required"`) | Tag name |
| `color` | string | No | Color |
| `sort_order` | int | No | Sort order |

Response: 200 `{"success":true,"data":{KnowledgeTag}}`

```bash
curl -X POST $BASE/api/v1/knowledge-bases/kb-1/tags -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"After-sales"}'
```

### PUT /api/v1/knowledge-bases/:id/tags/:tag_id

Purpose: update a tag (`tag_id` accepts a UUID or an integer seq_id). Request body: `name`/`color`/`sort_order` (pointer fields, all optional).

Response: 200 `{"success":true,"data":{KnowledgeTag}}`

```bash
curl -X PUT $BASE/api/v1/knowledge-bases/kb-1/tags/t-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"After-sales support"}'
```

### DELETE /api/v1/knowledge-bases/:id/tags/:tag_id

Purpose: delete a tag. Query parameters: `force` (bool; also delete documents or FAQ entries that still reference the tag), `content_only` (bool; delete only the content under the tag and keep the tag). Request body (optional): `{"exclude_ids":[int64]}`, listing the seq_ids of FAQ entries to keep during deletion. If `force` is not passed, and knowledge or FAQ entries still reference the tag, returns 400 and documents are not deleted.

An invalid request body format or an ID that is not a positive integer returns 400; a nonexistent ID returns 404; an FAQ entry ID that does not belong to the current knowledge base returns 403.

Response: 200 `{"success":true}`

```bash
curl -X DELETE $BASE/api/v1/knowledge-bases/kb-1/tags/t-1 -H "Authorization: Bearer $TOKEN"
```

## Chunk Debugging

### POST /api/v1/chunker/preview

Purpose: stateless chunking preview (KB editor debug panel). Permission: Viewer+; API key `retrieve`/`ingest`/full. Handler: `internal/handler/chunker_debug.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `text` | string | Yes (code validates non-empty, ≤64k characters) | Sample text |
| `chunking_config.chunk_size` | int | No | Chunk size in characters |
| `chunking_config.chunk_overlap` | int | No | Overlap |
| `chunking_config.separators` | []string | No | Separators |
| `chunking_config.strategy` | string | No | `auto/heading/heuristic/recursive/legacy` |
| `chunking_config.token_limit` | int | No | Token limit |
| `chunking_config.languages` | []string | No | Language hints |
| `chunking_config.enable_parent_child` | bool | No | Trial-chunk by parent-child, returning child chunks (consistent with retrieval granularity) |
| `chunking_config.parent_chunk_size` / `child_chunk_size` | int | No | Parent/child chunk sizes, defaulting to 4096 / 384 |

Inline HTML `<table>` in the sample text is first converted to a Markdown table, consistent with processing at ingestion time.

Response: 200 `{"success":true,"data":{"selected_tier","tier_chain","rejected","profile","chunks":[...],"stats":{count,avg_chars,min_chars,max_chars,stddev_chars,truncated_to}}}`; text too long returns 413; chunking timeout (5s) returns 504.

```bash
curl -X POST $BASE/api/v1/chunker/preview -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"text":"# Title\nBody...","chunking_config":{"chunk_size":512}}'
```

## Implementation Reference

Route registration: `RegisterChunkRoutes`, `RegisterKnowledgeTagRoutes`, `RegisterChunkerDebugRoutes` in `internal/router/routes_knowledge.go`.
