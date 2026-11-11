# API Reference: Infrastructure and Data Sources

Register and manage vector stores, file storage, web search services, and data sources, with connection testing and sync operations.

Unified conventions: read Viewer+, write/connection test Admin+ (credentials probe external systems). API key capabilities: vector stores `manage_vector_stores`, storage backends `manage_storage_backends`, web search `manage_web_search`, data sources `manage_datasources` (all can be full-access).

## Vector Stores (/api/v1/vector-stores)

### GET /api/v1/vector-stores/types

Purpose: Available engine types and configuration schema. Permission: Viewer+.

Response: 200 `{"success":true,"data":[type definitions]}`

```bash
curl $BASE/api/v1/vector-stores/types -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/vector-stores/test

Purpose: Test connection with raw configuration (not persisted). Permission: Admin+.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `engine_type` | string | Yes (`binding:"required"`) | Engine type |
| `connection_config` | object | Yes (`binding:"required"`) | Connection configuration |

Response: 200 `{"success":true|false,"version":"...","error":"..."}`

```bash
curl -X POST $BASE/api/v1/vector-stores/test -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"engine_type":"qdrant","connection_config":{"addr":"qdrant:6334"}}'
```

### POST /api/v1/vector-stores

Purpose: Create a vector store configuration. Permission: Admin+. Fields: `name` (required), `engine_type` (required), `connection_config` (required), `index_config` (optional).

Response: 201 `{"success":true,"data":{VectorStoreResponse}}` (`id,tenant_id,name,engine_type,connection_config,index_config,...`)

```bash
curl -X POST $BASE/api/v1/vector-stores -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"qdrant-main","engine_type":"qdrant","connection_config":{"addr":"qdrant:6334"}}'
```

### GET /api/v1/vector-stores

Purpose: List vector stores (environment-variable-injected `__env_*` stores come first). Permission: Viewer+.

Response: 200 `{"success":true,"data":[VectorStoreResponse]}`

```bash
curl $BASE/api/v1/vector-stores -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/vector-stores/:id

Purpose: Vector store details (supports `__env_*` IDs). Permission: Viewer+.

Response: 200 `{"success":true,"data":{VectorStoreResponse}}`

```bash
curl $BASE/api/v1/vector-stores/vs-1 -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/vector-stores/:id

Purpose: Update (rename only; env stores cannot be modified). Permission: Admin+. Request body: `{"name":"..."}` (`binding:"required"`).

Response: 200 `{"success":true,"data":{VectorStoreResponse}}`

```bash
curl -X PUT $BASE/api/v1/vector-stores/vs-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"qdrant-prod"}'
```

### DELETE /api/v1/vector-stores/:id

Purpose: Delete (env stores cannot be deleted). Permission: Admin+.

Response: 200 `{"success":true}`

```bash
curl -X DELETE $BASE/api/v1/vector-stores/vs-1 -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/vector-stores/:id/test

Purpose: Test a saved/env vector store. Permission: Admin+.

Response: 200 `{"success":true|false,"version","error"}`

```bash
curl -X POST $BASE/api/v1/vector-stores/vs-1/test -H "Authorization: Bearer $TOKEN"
```

## Storage Backends (/api/v1/storage-backends)

Request body (Create/Update/TestRaw share `storageBackendRequest`):

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | Yes (`binding:"required"`) | Name |
| `provider` | string | Yes (`binding:"required"`) | Provider: `local`/`minio`/`cos`/`tos`/`s3`/`oss`/`ks3`/`obs`, restricted by `STORAGE_ALLOW_LIST` |
| `config` | object | No | Provider configuration, fields see [Storage Backends](../03-features/19-storage-backends.md#连接参数) (credentials masked in responses) |
| `status` | string | No | `active` (default)/`disabled` |

### GET /api/v1/storage-backends/types

Purpose: List of provider names allowed by `STORAGE_ALLOW_LIST` (returns all when unset). Permission: Viewer+. Response: 200 `{"success":true,"data":["local","minio",...]}`

```bash
curl $BASE/api/v1/storage-backends/types -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/storage-backends/test

Purpose: Raw configuration connection test. Permission: Admin+. Response: 200 `{"success":bool,"error"}`

```bash
curl -X POST $BASE/api/v1/storage-backends/test -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"t","provider":"minio","config":{"endpoint":"minio:9000"}}'
```

### POST /api/v1/storage-backends

Purpose: Create a storage backend. Permission: Admin+. Response: 201 `{"success":true,"data":{StorageBackend}}`

```bash
curl -X POST $BASE/api/v1/storage-backends -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"minio-main","provider":"minio","config":{"endpoint":"minio:9000"}}'
```

### GET /api/v1/storage-backends

Purpose: List (includes `default_storage_backend_id`). Permission: Viewer+. Response: 200 `{"success":true,"data":[...],"default_storage_backend_id":"..."}`

```bash
curl $BASE/api/v1/storage-backends -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/storage-backends/:id

Purpose: Details (credentials masked). Permission: Viewer+. Response: 200 `{"success":true,"data":{StorageBackend}}`

```bash
curl $BASE/api/v1/storage-backends/sb-1 -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/storage-backends/:id

Purpose: Update. Permission: Admin+. Response: 200 `{"success":true,"data":{StorageBackend}}`

```bash
curl -X PUT $BASE/api/v1/storage-backends/sb-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"minio-prod","provider":"minio"}'
```

### DELETE /api/v1/storage-backends/:id

Purpose: Delete. Permission: Admin+. Response: 200 `{"success":true}`

```bash
curl -X DELETE $BASE/api/v1/storage-backends/sb-1 -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/storage-backends/:id/test

Purpose: Test a saved backend. Permission: Admin+. Response: 200 `{"success":bool,"error"}`

```bash
curl -X POST $BASE/api/v1/storage-backends/sb-1/test -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/storage-backends/:id/default

Purpose: Set as default backend. Permission: Admin+. Response: 200 `{"success":true}`

```bash
curl -X PUT $BASE/api/v1/storage-backends/sb-1/default -H "Authorization: Bearer $TOKEN"
```

## Web Search (/api/v1/web-search and /api/v1/web-search-providers)

Currently 16 search providers are registered, including Metaso, Exa, Bocha, Brave, Serply, DeepSeek, SearchApi. See [Web Search](../03-features/11-web-search.md) for each provider's api_key and extra_config parameters.

### GET /api/v1/web-search/providers

Purpose: Built-in search provider catalog (read-only). Permission: Viewer+, JWT only (no API key policy declared). Handler: `internal/handler/web_search.go`

Response: 200 `{"success":true,"data":[...]}`

```bash
curl $BASE/api/v1/web-search/providers -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/web-search-providers/types

Purpose: Provider types and parameter schema. Permission: Viewer+. Handler: `internal/handler/web_search_provider.go`

Response: 200 `{"success":true,"data":[...]}`

```bash
curl $BASE/api/v1/web-search-providers/types -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/web-search-providers/test

Purpose: Raw credential test (not persisted). Permission: Admin+. Request body: `provider` (`binding:"required"`), `parameters` (optional).

Response: 200 `{"success":bool,"error"}`

```bash
curl -X POST $BASE/api/v1/web-search-providers/test -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"provider":"tavily","parameters":{"api_key":"tvly-..."}}'
```

### POST /api/v1/web-search-providers

Purpose: Create a provider configuration. Permission: Admin+.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | Yes (`binding:"required"`) | Name |
| `provider` | string | Yes (`binding:"required"`) | Type (bing/tavily/google…) |
| `description` | string | No | Description |
| `parameters` | object | No | Parameters (api_key recommended to go through the credentials sub-resource) |
| `is_default` | bool | No | Default provider |

Response: 201 `{"success":true,"data":{WebSearchProviderResponse}}`

```bash
curl -X POST $BASE/api/v1/web-search-providers -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"tavily-main","provider":"tavily"}'
```

### GET /api/v1/web-search-providers

Purpose: List providers. Permission: Viewer+. Response: 200 `{"success":true,"data":[...]}`

```bash
curl $BASE/api/v1/web-search-providers -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/web-search-providers/:id

Purpose: Details. Permission: Viewer+. Response: 200 `{"success":true,"data":{...}}`

```bash
curl $BASE/api/v1/web-search-providers/wsp-1 -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/web-search-providers/:id

Purpose: Update (empty fields retain original values; APIKey retained). Permission: Admin+. Request body: `name/description/parameters/is_default` (all optional).

Response: 200 `{"success":true,"data":{...}}`

```bash
curl -X PUT $BASE/api/v1/web-search-providers/wsp-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"is_default":true}'
```

### DELETE /api/v1/web-search-providers/:id

Purpose: Delete. Permission: Admin+. Response: 200 `{"success":true}`

```bash
curl -X DELETE $BASE/api/v1/web-search-providers/wsp-1 -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/web-search-providers/:id/credentials

Purpose: Set API key (`{"api_key":"..."}`, returns status when omitted). Permission: Admin+. Handler: `internal/handler/web_search_provider_credentials.go`

Response: 200 `{"success":true,"data":{"fields":{"api_key":{"configured":bool}}}}`

```bash
curl -X PUT $BASE/api/v1/web-search-providers/wsp-1/credentials -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"api_key":"tvly-..."}'
```

### DELETE /api/v1/web-search-providers/:id/credentials/:field

Purpose: Delete a credential field (`field` is only `api_key`). Permission: Admin+. Response: 204.

```bash
curl -X DELETE $BASE/api/v1/web-search-providers/wsp-1/credentials/api_key -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/web-search-providers/:id/test

Purpose: Test a saved provider. Permission: Admin+. Response: 200 `{"success":bool,"error"}`

```bash
curl -X POST $BASE/api/v1/web-search-providers/wsp-1/test -H "Authorization: Bearer $TOKEN"
```

## Data Sources (/api/v1/datasource)

External content connectors (Feishu/Notion/Yuque, etc.); sync jobs write into the KB. Handler: `internal/handler/datasource.go`. Most responses in this group are raw objects/arrays (no `success` wrapper).

Currently registered types are feishu, lark, feishu_drive, lark_drive, notion, confluence, yuque, dingtalk, ima, rss, gitlab. See [Data Source Import](../03-features/10-datasource.md) for each connector's credentials, resource selection, and sync limitations. When sync_deletions is enabled, deleted knowledge belonging to that data source is actually deleted; source_created_at/source_updated_at are stored in the knowledge metadata.

### GET /api/v1/datasource/types

Purpose: Available connector catalog. Permission: Viewer+.

Response: 200 `[{type,name,description,icon,priority,auth_type,capabilities}]`

```bash
curl $BASE/api/v1/datasource/types -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/datasource/validate-credentials

Purpose: Validate raw credentials ("Test Connection" button, not persisted). Permission: Admin+.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `type` | string | Yes (`binding:"required"`) | Connector type |
| `credentials` | map | Yes (`binding:"required"`) | Credentials |

Response: 200 `{"status":"connected"}`; failure 400 `{"error":"..."}`

```bash
curl -X POST $BASE/api/v1/datasource/validate-credentials -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"type":"notion","credentials":{"api_key":"ntn_xxx"}}'
```

### POST /api/v1/datasource

Purpose: Create a data source. Permission: Admin+. Request body (`types.DataSource`):

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `knowledge_base_id` | string | Yes | Target KB (must belong to this space) |
| `name` | string | Yes | Name |
| `type` | string | Yes | Connector type |
| `config` | object | Yes | Credentials (stored encrypted) + resource selection + settings |
| `sync_schedule` | string | No | cron expression |
| `sync_mode` | string | No | `incremental` (default)/`full` |
| `conflict_strategy` | string | No | `overwrite` (default)/`skip` |
| `sync_deletions` | bool | No | Default true |
| `sync_log_retention_days` | int | No | Sync log retention days for this data source, default 30; non-positive values are treated as 30 days |

Response: 201 `DataSourceResponse` (credentials stripped, see `internal/handler/dto/datasource.go`).

Every day in the early morning (in the server's timezone), expired `success`, `partial`, and `canceled` sync logs are cleaned up, including paused data sources and those with manual-only sync. The retention period is calculated from the completion time; when old logs lack a completion time, the start time is used. Running or pending logs are not cleaned up; `failed` logs of existing data sources are also retained because that state may still be awaiting task retry. After a data source is soft-deleted, the original retention period continues to apply, and `failed` logs may be cleaned up; logs without a source are retained for the default 30 days.

```bash
curl -X POST $BASE/api/v1/datasource -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"knowledge_base_id":"kb-1","name":"notion sync","type":"notion","config":{}}'
```

### GET /api/v1/datasource

Purpose: List data sources. Permission: Viewer+. Query parameter: `kb_id` (required).

Response: 200 `[DataSourceResponse]`

```bash
curl "$BASE/api/v1/datasource?kb_id=kb-1" -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/datasource/:id

Purpose: Details. Permission: Viewer+. Response: 200 `DataSourceResponse`; 404 `{"error":"data source not found"}`

```bash
curl $BASE/api/v1/datasource/ds-1 -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/datasource/:id

Purpose: Update (`id/tenant_id/knowledge_base_id` are locked to their original values). Permission: Admin+. Request body same as create.

PUT replaces the entire data source configuration and does not support partial updates: first GET the details, modify, then send back the complete object (`name`, `type`, `config`, `sync_schedule`, `sync_mode`, `conflict_strategy`, `sync_deletions`, `sync_log_retention_days`, `status`, etc.). `sync_schedule` and `sync_deletions` are always written from the request body: omitting `sync_schedule` clears it to an empty string (manual sync only, and any existing scheduled task is removed accordingly), and omitting `sync_deletions` means false; whether other fields retain their original values when omitted is not guaranteed. Credentials are not modified through this endpoint; `config.credentials` in the request body is ignored and the stored credentials are retained.

Response: 200 `DataSourceResponse`

```bash
curl -X PUT $BASE/api/v1/datasource/ds-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"notion sync v2","type":"notion","knowledge_base_id":"kb-1",
       "config":{"type":"notion","resource_ids":["page-1"]},
       "sync_schedule":"0 0 */6 * * *","sync_mode":"incremental","conflict_strategy":"overwrite",
       "sync_deletions":true,"sync_log_retention_days":30,"status":"active"}'
```

### DELETE /api/v1/datasource/:id

Purpose: Delete. Permission: Admin+. Response: 204.

```bash
curl -X DELETE $BASE/api/v1/datasource/ds-1 -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/datasource/:id/credentials

Purpose: Replace credentials entirely (data source credentials are an atomic map of the "single logical field `credentials`"). Permission: Admin+. Request body: `{"credentials":{...}}` (non-empty map required). Handler: `internal/handler/datasource_credentials.go`

Response: 200 `{"success":true,"data":{"fields":{"credentials":{"configured":bool}}}}`

```bash
curl -X PUT $BASE/api/v1/datasource/ds-1/credentials -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"credentials":{"api_key":"ntn_xxx"}}'
```

### DELETE /api/v1/datasource/:id/credentials/:field

Purpose: Clear credentials (`field` must be `credentials`). Permission: Admin+. Response: 204.

```bash
curl -X DELETE $BASE/api/v1/datasource/ds-1/credentials/credentials -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/datasource/:id/validate

Purpose: Validate a saved data source connection. Permission: Admin+. Response: 200 `{"status":"connected"}`

```bash
curl -X POST $BASE/api/v1/datasource/ds-1/validate -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/datasource/:id/resources

Purpose: Browse the external resource tree (lazy loading). Permission: Admin+. Query parameter: `parent_id` (optional, empty = top level).

Response: 200 `[{external_id,name,type,description,url,modified_at,parent_id,has_children,metadata}]`

```bash
curl "$BASE/api/v1/datasource/ds-1/resources?parent_id=" -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/datasource/:id/resource-ancestors

Purpose: Resolve resource ancestor chains (selector expansion). Permission: Admin+. Request body: `{"resource_ids":["..."]}` (required).

Response: 200 `{"ancestors":[...]}`

```bash
curl -X POST $BASE/api/v1/datasource/ds-1/resource-ancestors -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"resource_ids":["page-1"]}'
```

### POST /api/v1/datasource/:id/sync

Purpose: Manually trigger sync. Permission: Admin+. Response: 200 `SyncLog` (`id,status,started_at,items_total,items_created,items_updated,items_deleted,items_failed,...`)

```bash
curl -X POST $BASE/api/v1/datasource/ds-1/sync -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/datasource/:id/pause and POST /api/v1/datasource/:id/resume

Purpose: Pause / resume scheduled sync. Permission: Admin+.

Response: 200 `{"status":"paused"}` / `{"status":"active"}`

```bash
curl -X POST $BASE/api/v1/datasource/ds-1/pause -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/datasource/:id/logs

Purpose: List sync logs. Permission: Viewer+. Query parameters: `limit` (default 10, max 100), `offset` (default 0).

Response: 200 `[SyncLog]`

```bash
curl "$BASE/api/v1/datasource/ds-1/logs?limit=10" -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/datasource/logs/:log_id

Purpose: Single sync log. Permission: Viewer+. Response: 200 `SyncLog`; 404 `{"error":"sync log not found"}`

```bash
curl $BASE/api/v1/datasource/logs/log-1 -H "Authorization: Bearer $TOKEN"
```

## Implementation Reference

Route registration: `RegisterVectorStoreRoutes`, `RegisterStorageBackendRoutes`, `RegisterWebSearchRoutes`, `RegisterWebSearchProviderRoutes`, `RegisterDataSourceRoutes` in `internal/router/routes_infra.go`. Handlers: `internal/handler/vectorstore.go`, `internal/handler/storagebackend.go`, `internal/handler/web_search.go`, `internal/handler/web_search_provider.go`, `internal/handler/web_search_provider_credentials.go`, `internal/handler/datasource.go`, `internal/handler/datasource_credentials.go`.
