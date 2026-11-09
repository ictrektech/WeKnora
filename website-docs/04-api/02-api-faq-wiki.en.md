# API Reference: FAQ and Wiki

Manage FAQ entries and Wiki pages in the knowledge base, with support for import, retrieval, editing, and version restoration.

Both groups are KB content sub-resources: reads require Viewer+ and KB read (API key `retrieve`/full); writes require "KB creator OR Admin+" and KB write (API key `ingest`/full), and are subject to the KB allow-list. Cross-KB Wiki search `POST /wiki-search` has no path KB, requires Viewer+ and API key `retrieve`/full, and the handler validates the allow-list and shared-library Viewer permission for each target KB.

## FAQ (/api/v1/knowledge-bases/:id/faq)

### GET /api/v1/knowledge-bases/:id/faq/entries

Purpose: List FAQ entries.

| Query parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `page` / `page_size` | int | No | Pagination |
| `tag_id` | int | No | Legacy single-tag seq_id |
| `tag_ids` | string | No | Comma-separated tag UUIDs |
| `keyword` | string | No | Keyword |
| `search_field` | string | No | `standard_question`/`similar_questions`/`answers` (defaults to all fields) |
| `sort_order` | string | No | `asc` (defaults to descending by update time) |
| `is_enabled` | bool | No | Filter by enabled status: `true` for enabled only, `false` for disabled only, omit to return all; any other value returns 400 |

Response: 200 `{"success":true,"data":{paginated FAQEntry list}}`

```bash
curl "$BASE/api/v1/knowledge-bases/kb-1/faq/entries?page=1&is_enabled=false" -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledge-bases/:id/faq/entries/export

Purpose: Export FAQs. Query parameter: `format` (`csv` default / `json`).

Response: 200 file download (`text/csv` or `application/json`).

```bash
curl -OJ "$BASE/api/v1/knowledge-bases/kb-1/faq/entries/export?format=csv" -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledge-bases/:id/faq/entries/:entry_id

Purpose: FAQ entry details (`entry_id` is an integer seq_id).

Response: 200 `{"success":true,"data":{FAQEntry}}`

```bash
curl $BASE/api/v1/knowledge-bases/kb-1/faq/entries/12 -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/knowledge-bases/:id/faq/entries

Purpose: Batch upsert / import (asynchronous task). Handler method `UpsertEntries`.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `entries` | []FAQEntryPayload | Yes (`binding:"required"`) | Batch entries |
| `mode` | string | Yes (`binding:"oneof=append replace"`) | Append or replace |
| `knowledge_id` | string | No | FAQ knowledge entity ID |
| `task_id` | string | No | Custom task ID; only letters, digits, `_`, `-` allowed, max 128 characters, otherwise returns 400 |
| `dry_run` | bool | No | Validate only, do not persist |

Response: 200 `{"success":true,"data":{"task_id"}}`

```bash
curl -X POST $BASE/api/v1/knowledge-bases/kb-1/faq/entries -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"mode":"append","entries":[{"standard_question":"How do I get a refund?","answers":["Contact customer service"]}]}'
```

### POST /api/v1/knowledge-bases/:id/faq/entry

Purpose: Create a single FAQ. Request body (`types.FAQEntryPayload`):

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `standard_question` | string | Yes (`binding:"required"`) | Standard question |
| `similar_questions` | []string | No | Similar questions |
| `negative_questions` | []string | No | Negative sample questions |
| `answers` | []string | No | Answer list |
| `answer_strategy` | string | No | `all` / `random` |
| `tag_id` | int64 | No | Tag seq_id |
| `tag_name` | string | No | Tag name |
| `is_enabled` / `is_recommended` | *bool | No | Enabled/recommended |

Response: 200 `{"success":true,"data":{FAQEntry}}`

```bash
curl -X POST $BASE/api/v1/knowledge-bases/kb-1/faq/entry -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"standard_question":"How do I get a refund?","answers":["Refundable within 7 days"]}'
```

### PUT /api/v1/knowledge-bases/:id/faq/entries/:entry_id

Purpose: Update a single FAQ (request body same as create).

Response: 200 `{"success":true,"data":{FAQEntry}}`

```bash
curl -X PUT $BASE/api/v1/knowledge-bases/kb-1/faq/entries/12 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"standard_question":"How do I get a refund?","answers":["Refundable within 30 days"]}'
```

### POST /api/v1/knowledge-bases/:id/faq/entries/:entry_id/similar-questions

Purpose: Append similar questions. Request body: `{"similar_questions":["..."]}` (`binding:"required,min=1"`).

Response: 200 `{"success":true,"data":{FAQEntry}}`

```bash
curl -X POST $BASE/api/v1/knowledge-bases/kb-1/faq/entries/12/similar-questions \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"similar_questions":["How do I process a refund"]}'
```

### PUT /api/v1/knowledge-bases/:id/faq/entries/fields

Purpose: Batch update entry fields (`is_enabled`/`is_recommended`/`tag_id`).

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `by_id` | map[int64]object | No | Update by entry seq_id |
| `by_tag` | map[int64]object | No | Batch update by tag |
| `exclude_ids` | []int64 | No | Entries to exclude when using `by_tag` |

Response: 200 `{"success":true}`

```bash
curl -X PUT $BASE/api/v1/knowledge-bases/kb-1/faq/entries/fields -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"by_id":{"12":{"is_enabled":false}}}'
```

### PUT /api/v1/knowledge-bases/:id/faq/entries/tags

Purpose: Batch change entry tags. Request body: `{"updates":{"<entry_id>":<tag_id|null>}}` (`binding:"required,min=1"`; null removes the tag).

Response: 200 `{"success":true}`

```bash
curl -X PUT $BASE/api/v1/knowledge-bases/kb-1/faq/entries/tags -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"updates":{"12":3}}'
```

### DELETE /api/v1/knowledge-bases/:id/faq/entries

Purpose: Batch delete entries. Request body: `{"ids":[int64]}` (`binding:"required,min=1"`).

Response: 200 `{"success":true}`

```bash
curl -X DELETE $BASE/api/v1/knowledge-bases/kb-1/faq/entries -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"ids":[12,13]}'
```

### POST /api/v1/knowledge-bases/:id/faq/search

Purpose: FAQ retrieval (read-only semantics; scoped keys with `retrieve` can also call it).

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `query_text` | string | Yes (`binding:"required"`) | Query |
| `vector_threshold` | float64 | No | Vector threshold, default 0.7 |
| `match_count` | int | No | Default 10, max 50 |
| `first_priority_tag_ids` / `second_priority_tag_ids` | []int64 | No | Tag priority filtering |
| `only_recommended` | bool | No | Recommended entries only |

Response: 200 `{"success":true,"data":[FAQEntry (including match_type/score)]}`

```bash
curl -X POST $BASE/api/v1/knowledge-bases/kb-1/faq/search -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' -d '{"query_text":"refund"}'
```

### PUT /api/v1/knowledge-bases/:id/faq/import/last-result/display

Purpose: Set the display status of the most recent import result panel. Request body: `{"display_status":"open|close"}` (`binding:"required,oneof=open close"`).

Response: 200 `{"success":true}`

```bash
curl -X PUT $BASE/api/v1/knowledge-bases/kb-1/faq/import/last-result/display \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"display_status":"close"}'
```

### GET /api/v1/faq/import/progress/:task_id

Purpose: Query FAQ import/dry-run progress (tasks are isolated by space). Permission: Viewer+; API key `retrieve`/`ingest`/full.

Response: 200 `{"success":true,"data":{status,progress,failed_entries,...}}`

```bash
curl $BASE/api/v1/faq/import/progress/task-1 -H "X-API-Key: $API_KEY"
```

## Wiki (/api/v1/knowledgebase/:kb_id/wiki)

Note that this group's prefix is `/knowledgebase/:kb_id/wiki` (singular, no hyphen). Handler: `internal/handler/wiki_page.go`. Responses in this group are mostly **raw objects** (without the `success` wrapper).

### GET /api/v1/knowledgebase/:kb_id/wiki/pages

Purpose: List Wiki pages.

| Query parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `page_type` | string | No | Comma-separated types |
| `status` | string | No | Page status |
| `query` | string | No | Full-text search |
| `category_path` | string | No | `/`-separated path filter |
| `folder_id` | string | No | Exact folder filter (empty string = root) |
| `category_depth` | int | No | Folder depth |
| `page` / `page_size` | int | No | Pagination (default 1/20) |
| `sort_by` / `sort_order` | string | No | Sort field: `title`, `created_at`, `updated_at`, `page_type`, `wiki_path`, `sort_order`, `depth`; other values fall back to `updated_at`; default `updated_at` desc |

Response: 200 `WikiPageListResponse`

```bash
curl "$BASE/api/v1/knowledgebase/kb-1/wiki/pages?page=1" -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/knowledgebase/:kb_id/wiki/pages

Purpose: Create a page. Request body (`types.WikiPage`): `slug`, `title`, `content`, `folder_id`, `page_type`, etc. (all optional; slug is auto-generated if omitted).

Response: 201 `WikiPage`

```bash
curl -X POST $BASE/api/v1/knowledgebase/kb-1/wiki/pages -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"title":"Architecture Overview","content":"# Overview"}'
```

### PUT /api/v1/knowledgebase/:kb_id/wiki/move-page

Purpose: Move a page to a folder. Request body: `{"slug":"<page slug>","folder_id":"<folder ID|empty=root>"}` (slug required).

Response: 200 `WikiPage`

```bash
curl -X PUT $BASE/api/v1/knowledgebase/kb-1/wiki/move-page -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"slug":"overview","folder_id":"f-1"}'
```

### GET /api/v1/knowledgebase/:kb_id/wiki/pages/*slug

Purpose: Get a page (`*slug` is a wildcard path).

Response: 200 `WikiPageDetail`. Retains all fields of `WikiPage` and adds `in_link_titles`, which maps page slugs present in `in_links` to display titles, e.g. `{"concept/example":"Example Title"}`. Returns an empty object when there are no resolvable backlinks; nonexistent targets do not appear in the mapping. Returns 500 if the title lookup fails, to avoid returning an incomplete success response.

```bash
curl $BASE/api/v1/knowledgebase/kb-1/wiki/pages/overview -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/knowledgebase/:kb_id/wiki/pages/*slug

Purpose: Update a page (request body same as create). The old version is first snapshotted in full into `wiki_page_revisions`, `version` is incremented, and `last_edit_source` is recorded as `user` (`agent` when written by an Agent tool).

Response: 200 `WikiPage`

```bash
curl -X PUT $BASE/api/v1/knowledgebase/kb-1/wiki/pages/overview -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"content":"# Updated Overview"}'
```

### GET /api/v1/knowledgebase/:kb_id/wiki/revisions/*slug

Purpose: Page version history (migration `000075`). Permission: Viewer+ + KBAccessRead.

| Query parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `version` | int | No | When provided, returns the **full text of that version** (for diff); invalid or < 1 returns 400, not found returns 404 (versions written before the upgrade or snapshots cleaned up by the retention policy have no full text; this is normal) |
| `limit` | int | No | Default 50, max 200; only effective in list mode |
| `offset` | int | No | Pagination offset |

When `version` is omitted, returns the history list (version numbers in descending order, **without body text**) plus the page's current version number; each entry includes `edit_source` (`pipeline` / `agent` / `user` / `revert`), `editor_id`, `edited_at`.

History retention has two tiers of limits: the soft limit of 50 versions only prunes snapshots from `pipeline` and empty sources, while the hard limit of 200 versions applies to all sources, so manual edits are not flushed out by the pipeline.

```bash
# History list
curl $BASE/api/v1/knowledgebase/kb-1/wiki/revisions/entity/acme-corp -H "Authorization: Bearer $TOKEN"
# Get full text of version 3
curl "$BASE/api/v1/knowledgebase/kb-1/wiki/revisions/entity/acme-corp?version=3" -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/knowledgebase/:kb_id/wiki/revert

Purpose: Roll a page back to a historical version. Permission: KB owner or Admin+ + KBAccessWrite.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `slug` | string | Yes | Target page |
| `version` | int | Yes | Target version number (≥ 1) |

Rollback **does not roll the version number back**: the content of the target version is written as a new version, and `last_edit_source` is recorded as `revert`, so a rollback can itself be rolled back. Rolling back to the current version returns 400 (usually because the frontend's history list is stale).

Response: 200 `WikiPage`

```bash
curl -X POST $BASE/api/v1/knowledgebase/kb-1/wiki/revert -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"slug":"entity/acme-corp","version":3}'
```

### DELETE /api/v1/knowledgebase/:kb_id/wiki/pages/*slug

Purpose: Delete a page.

Response: 204 No Content

```bash
curl -X DELETE $BASE/api/v1/knowledgebase/kb-1/wiki/pages/overview -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledgebase/:kb_id/wiki/folders

Purpose: List folders. Query parameters: `parent_id` (empty = root), `page_types` (comma-separated).

Response: 200 `WikiFolderListResponse`

```bash
curl $BASE/api/v1/knowledgebase/kb-1/wiki/folders -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/knowledgebase/:kb_id/wiki/folders

Purpose: Create a folder.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | Yes | Folder name |
| `parent_id` | string | No | Parent folder |

Response: 201 `WikiFolder`

```bash
curl -X POST $BASE/api/v1/knowledgebase/kb-1/wiki/folders -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"Design Docs"}'
```

### PUT /api/v1/knowledgebase/:kb_id/wiki/folders/:folder_id

Purpose: Rename/move a folder. Request body: `name`, `parent_id`, `move_parent` (bool), all optional.

Response: 200 `WikiFolder`

```bash
curl -X PUT $BASE/api/v1/knowledgebase/kb-1/wiki/folders/f-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"Architecture Design"}'
```

### DELETE /api/v1/knowledgebase/:kb_id/wiki/folders/:folder_id

Purpose: Delete a folder.

Response: 204 No Content

```bash
curl -X DELETE $BASE/api/v1/knowledgebase/kb-1/wiki/folders/f-1 -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledgebase/:kb_id/wiki/index

Purpose: Wiki index page (grouped windows by type). Query parameters: `types` (comma-separated), `limit` (1-200, default 50), `cursor` (cursor).

Response: 200 `WikiIndexResponse`

```bash
curl $BASE/api/v1/knowledgebase/kb-1/wiki/index -H "Authorization: Bearer $TOKEN"
```

::: warning Removed
`GET /api/v1/knowledgebase/:kb_id/wiki/log` (Wiki change log) has been taken offline along with migration `000077_remove_wiki_log`, and the `wiki_log_entries` table has been dropped. Wiki changes are now projected uniformly into the knowledge base activity stream; use `GET /api/v1/knowledge-bases/:id/activity` instead.
:::

### GET /api/v1/knowledgebase/:kb_id/wiki/graph

Purpose: Page relationship graph.

| Query parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `mode` | string | No | `overview` (default) / `ego` |
| `center` | string | No | Center slug for ego mode (required in ego mode) |
| `depth` | int | No | 1-3, default 1 |
| `types` | string | No | page_type filter |
| `limit` | int | No | Default 500, max 2000 |

Response: 200 `WikiGraphData`

```bash
curl "$BASE/api/v1/knowledgebase/kb-1/wiki/graph?mode=overview" -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledgebase/:kb_id/wiki/stats

Purpose: Wiki statistics.

Response: 200 `WikiStats`

```bash
curl $BASE/api/v1/knowledgebase/kb-1/wiki/stats -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledgebase/:kb_id/wiki/search

Purpose: Page search. Query parameters: `q` (required), `limit` (default 10).

Matching semantics vary by storage dialect:

- Postgres: `q` is matched as a POSIX regular expression (`~*`, case-insensitive).
- SQLite / Lite: matched as a literal `LIKE` substring; `%` / `_` are escaped and not treated as wildcards.

Response: 200 `{"pages":[WikiPage]}`

```bash
curl "$BASE/api/v1/knowledgebase/kb-1/wiki/search?q=deployment" -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/wiki-search

Purpose: Cross-knowledge-base Wiki page search (stateless, non-hybrid retrieval). Handler: `SearchPagesAcross` in `internal/handler/wiki_page.go`. The algorithm is the same as single-KB `GET .../wiki/search` (title > slug > summary > body), performing one query across multiple KBs and then globally truncating by `match_rank`. API key: `retrieve`/full.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `query` | string | Yes (`binding:"required"`) | Same as single-KB `q`: Postgres uses POSIX regex (case-insensitive); SQLite / Lite uses literal `LIKE` substring |
| `knowledge_base_ids` | []string | Conditionally required | Multiple KBs; at least one of this and `knowledge_base_id` must be provided, max 32 |
| `knowledge_base_id` | string | No | Single-KB compatibility field, merged into ids |
| `limit` | int | No | Global top-N, default 10, max 50 |

If any target KB has wiki disabled → 400. KB does not exist or no read permission → 404. API Key allow-list violation → 403. Invalid regex (Postgres) → 400.

Response: 200 `{"success":true,"data":[WikiSearchHit]}`. Hits contain only navigation fields + `match_snippet`, not the body; use `GET /knowledgebase/:kb_id/wiki/pages/{slug}` for the full text.

| Response field | Type | Description |
| --- | --- | --- |
| `id` | string | Page ID |
| `knowledge_base_id` | string | Source KB, used to construct the page URL in cross-KB scenarios |
| `slug` | string | Page slug |
| `title` | string | Title |
| `page_type` | string | Page type |
| `aliases` | []string | Aliases |
| `summary` | string | Summary |
| `match_snippet` | string | Approximately 60 characters before + match + 60 characters after the first hit in the body; omitted when there is no body hit. The snippet still compiles `query` with Go regex; under SQLite, when `query` contains regex metacharacters, the in-DB match may succeed while the snippet may be empty |

`content`, tree/links/metadata, and timestamps are not included in search results. Single-KB `GET .../wiki/search` still returns the full `WikiPage`.

```bash
curl -X POST $BASE/api/v1/wiki-search -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"query":"deploy|release","knowledge_base_ids":["kb-1","kb-2"],"limit":10}'
```

### POST /api/v1/knowledgebase/:kb_id/wiki/rebuild-links

Purpose: Rebuild page interlinks. Write permission. No request body.

Response: 200 `{"message":"Links rebuilt successfully"}`

```bash
curl -X POST $BASE/api/v1/knowledgebase/kb-1/wiki/rebuild-links -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledgebase/:kb_id/wiki/lint

Purpose: Wiki consistency check report.

Response: 200 `WikiLintReport`

```bash
curl $BASE/api/v1/knowledgebase/kb-1/wiki/lint -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/knowledgebase/:kb_id/wiki/auto-fix

Purpose: Automatically fix lint issues. Write permission. No request body.

Response: 200 `{"fixed":N,"message":"Auto-fixed N issues"}`

```bash
curl -X POST $BASE/api/v1/knowledgebase/kb-1/wiki/auto-fix -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledgebase/:kb_id/wiki/issues

Purpose: List issues. Query parameters: `slug` (filter by page), `status` (`pending/ignored/resolved`).

Response: 200 `[WikiPageIssue]`

```bash
curl $BASE/api/v1/knowledgebase/kb-1/wiki/issues -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/knowledgebase/:kb_id/wiki/issues/:issue_id/status

Purpose: Update issue status. Write permission. Request body: `{"status":"pending|ignored|resolved"}` (`binding:"required"`). `issue_id` must belong to the knowledge base in the path, otherwise 404.

Response: 200 `{"message":"Issue status updated successfully"}`

```bash
curl -X PUT $BASE/api/v1/knowledgebase/kb-1/wiki/issues/i-1/status -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"status":"resolved"}'
```

## Implementation Reference

Route registration: `RegisterFAQRoutes` and `RegisterWikiPageRoutes` in `internal/router/routes_knowledge.go`. Handlers: `internal/handler/faq.go`, `internal/handler/wiki_page.go`.
