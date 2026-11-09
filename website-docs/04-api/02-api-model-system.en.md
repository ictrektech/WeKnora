# API Reference: Models and Initialization

Manage models, test connections, initialize knowledge bases, and launch evaluation tasks. The WeKnoraCloud interface is used for related cloud service integration.

For system information and system management (`/system`, `/system/admin`) interfaces, see [System and Platform Management](./02-api-system.md).

## Models (/api/v1/models)

API key: `manage_models` or full-access.

### GET /api/v1/models/providers

Purpose: Provider catalog (the frontend uses this to dynamically render the provider dropdown, icons, extra fields, and model selection). Permission: Viewer+. Query parameter: `model_type` (optional: `chat/embedding/rerank/vllm/asr`, also accepts backend values such as `KnowledgeQA`; unknown values return 400). When specified, only providers that support that type and their models of that type are returned. Handler: `internal/handler/model_catalog.go`

Response: 200 `{"success":true,"data":[ModelProviderDTO]}`, each item:

| Field | Description |
| --- | --- |
| `value` / `label` / `labels` / `description` / `descriptions` / `website` | Provider id, brand name, localized names and descriptions |
| `icon` | `data:image/svg+xml;base64,...`, can be used directly in `<img src>` |
| `api` / `auth` / `requiresAuth` | Default protocol (`openai-completions`, etc.), authentication method, whether a key is required |
| `defaultUrls` / `modelTypes` | Default URLs by model type and supported types. `defaultUrls` is returned only to Admin+ (or full-access / `manage_tenant_settings` API key); empty for other callers |
| `extraFields` | Provider extra configuration field definitions (`key,label,labels,type,required,default,placeholder,options,model_types,secret`); values are stored in `parameters.extra_config` |
| `credentialLabels` | Names and hints of credential input fields under some model types (e.g., the API Key field for Volcengine and LKEAP rerank is actually Access Key ID / SecretId) |
| `models` | Built-in model catalog (`id,name,type,api,reasoning,input,context_window,max_output_tokens,dimension,thinking_levels,cost,source`); `source` is the vendor documentation link on which the model parameters are based |
| `thinking` | Provider-level thinking encoding summary (`format`, `levels`) |
| `order` | List sort value |

```bash
curl "$BASE/api/v1/models/providers?model_type=chat" -H "Authorization: Bearer $TOKEN"
```

### GET|POST /api/v1/models/catalog/resolve

Purpose: Resolve the effective connection configuration (protocol, thinking level, context) by provider, model name, `base_url`, and `extra_config`, for real-time display in the model editor. Permission: Viewer+.

Parameters (query parameters for GET, JSON request body for POST, same fields): `provider` (provider ID), `model`, `base_url`, `model_type` (default `chat`), `api`, `thinking_control`, `remote_model_name`, and the non-secret extra fields declared by that provider (e.g., Azure's `api_version`). The POST request body may also include a `spec` object (same as the model's `parameters.spec`) to preview a single-row catalog override. Secret-type fields are never accepted.

Response: 200 `{"success":true,"data":{provider,api,remote_model,cataloged,model,capabilities,base_url,url}}`, where `capabilities` is `{provider,api,cataloged,reasoning,thinking_levels,thinking_format,input,context_window,max_output_tokens,max_tokens_field}`. `base_url` and `url` (the actual request address, returned only for providers that compute the address themselves, such as Azure) are returned only to Admin+ (or full-access / `manage_tenant_settings` API key). Returns 400 when resolution fails. The same `capabilities` structure is also returned with `ModelResponse.capabilities` for remote chat/vision models.

```bash
curl "$BASE/api/v1/models/catalog/resolve?provider=deepseek&model=deepseek-v4-pro" -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/models

Purpose: Create a model. Permission: Admin+.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | Yes (`binding:"required"`) | Model name |
| `display_name` | string | No | Display name |
| `type` | string | Yes (`binding:"required"`) | Model type: `KnowledgeQA` / `Embedding` / `Rerank` / `VLLM` / `ASR` (stored as-is; frontend forms such as `chat` are not accepted) |
| `source` | string | Yes (`binding:"required"`) | Source (`local` / `remote`) |
| `description` | string | No | Description |
| `parameters` | object | Yes (`binding:"required"`) | Connection parameters (`base_url`, `provider`, `extra_config`, `spec`, `context_window`, etc.; see [Model Management](../03-features/06-models.md#模型配置字段) for fields). `api_key` / `app_secret` may be included directly at creation time and modified later via the credentials sub-resource |

`parameters` is validated against the model catalog (unknown protocols, incorrect compat keys, and invalid thinking levels return 400), and `base_url` undergoes SSRF validation.

Self-deployed `generic` / `Qwen3-VL-Embedding` submit independent inputs in batches via two-dimensional `messages` by default (verified with vLLM 0.29.0); single inputs retain the one-dimensional conversation format. Text continues to be batched using `BATCH_EMBED_SIZE`; the default upper limit for batched image calls is 5 images, a conservative client-side batching setting that can be adjusted via `parameters.spec.compat.max_image_batch_size`. Upload parsing still schedules tasks per single image and does not merge images across tasks. For older services that do not support two-dimensional `messages`, set `parameters.spec.compat.batch_messages=false` to restore per-item requests; the WeMM-Embedding preset also enables conversation batching by default; other models without this preset must enable it explicitly.

Batching preserves the conversation content and vector index mapping of each input, but does not guarantee that the server computes bit-identical results across batch sizes. When strict reproducibility is required, verify the numerical behavior of the deployment; vLLM's [Batch Invariance](https://docs.vllm.ai/en/v0.29.0/features/batch_invariance/) mode must be enabled on the server and verified against the actual model. You can set `WEKNORA_EMBEDDING_TEST_URL` and `WEKNORA_EMBEDDING_TEST_FIXTURE` and then run `go test ./internal/models/embedding -run '^TestLiveQwenMessagesBatchParity$' -v -count=1`: the fixture is `{"texts":[...],"image_paths":[...]}`, with at least two texts and two images; private/IP endpoints require `SSRF_WHITELIST` to be configured, and authentication can use `WEKNORA_EMBEDDING_TEST_API_KEY`. This test compares per-item, batched, reversed-order, and repeated per-item requests, records the maximum absolute error and minimum cosine similarity, and fails if strict thresholds are exceeded; it does not write to the knowledge base.

Response: 201 `{"success":true,"data":{ModelResponse}}` (`id,name,type,source,parameters,is_default,is_builtin,status,credentials,capabilities,...`; the response does not contain secrets)

```bash
curl -X POST $BASE/api/v1/models -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"gpt-5.5","type":"KnowledgeQA","source":"remote","parameters":{"provider":"openai","base_url":"https://api.openai.com/v1","api_key":"sk-..."}}'
```

### GET /api/v1/models

Purpose: List models. Permission: Viewer+.

Response: 200 `{"success":true,"data":[ModelResponse]}`

```bash
curl $BASE/api/v1/models -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/models/:id

Purpose: Model details. Permission: Viewer+.

Response: 200 `{"success":true,"data":{ModelResponse}}`

```bash
curl $BASE/api/v1/models/m-1 -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/models/:id/copy

Purpose: Copy a non-built-in model configuration in the current space. Permission: Admin+.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `display_name` | string | Yes (`binding:"required"`) | Display name of the copy; must not be empty after trimming leading and trailing whitespace, maximum 255 characters |

The server reads the complete configuration using the source model ID in the path: the copy's `name` remains the same as the source model, and `type`, `source`, `description`, `parameters`, and stored credentials are all copied; client re-submission is not accepted. The copy does not inherit built-in or default status; an already-activated local model is not downloaded again, while other non-activated local models re-enter the download process.

Response: 201 `{"success":true,"data":{ModelResponse}}` (the response does not contain secrets)

Returns 404 if the source model does not exist; returns 400 if the source model is a built-in model, or if the ID or `display_name` is invalid.

```bash
curl -X POST $BASE/api/v1/models/m-1/copy -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"display_name":"GPT-4o copy"}'
```

### POST /api/v1/models/:id/debug

Purpose: Debug a saved model (makes real upstream calls, incurring charges). Permission: Admin+. form-data fields: `input` (≤64KB), `options` (JSON-encoded debug options: `system_prompt`, `temperature` (0~2), `top_p`, `max_tokens` (1~8192), `thinking`, `reasoning_effort` (`off/auto/minimal/low/medium/high/xhigh/max`; when set, overrides `thinking`)), `documents` (JSON array, ≤100 items), `file` (optional).

Response: 200 `{"success":true,"data":{"ok",elapsed_ms,request,raw_response,observations,error}}`

```bash
curl -X POST $BASE/api/v1/models/m-1/debug -H "Authorization: Bearer $TOKEN" -F 'input=Hello'
```

### PUT /api/v1/models/:id

Purpose: Update a model (built-in models are restricted to SystemAdmin by the service layer). Permission: Admin+ or SystemAdmin (`AdminOrSystemAdmin`). Request body: `name`, `display_name` (pointer), `description`, `parameters` (stored secrets are preserved), `source`, `type` (all optional).

Response: 200 `{"success":true,"data":{ModelResponse}}`

```bash
curl -X PUT $BASE/api/v1/models/m-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"display_name":"GPT-4o mini"}'
```

### DELETE /api/v1/models/:id

Purpose: Delete a model. Permission: Admin+.

Response: 200 `{"success":true,"message":"Model deleted"}`

If the model is still referenced by a knowledge base, agent, or long-term memory in the current space, the response is HTTP 400; the compatibility message is retained, while `error.code=2300` and `error.details` give the specific objects and reference locations:

```json
{
  "success": false,
  "error": {
    "code": 2300,
    "message": "model is used by 2 knowledge base(s); reconfigure or remove those references before deleting",
    "details": {
      "knowledge_bases": [
        {"id": "kb-1", "name": "Product docs", "bindings": ["vlm_model"]},
        {"id": "kb-2", "name": "Engineering", "bindings": ["vlm_model"]}
      ],
      "agents": [],
      "long_term_memory": {"bindings": []},
      "knowledge_base_total": 2,
      "agent_total": 0
    }
  }
}
```

Knowledge base binding values: `embedding_model`, `summary_model`, `image_processing_model`, `vlm_model`, `asr_model`, `wiki_synthesis_model`, `auto_tag_model`; agent binding values: `chat_model`, `rerank_model`, `vlm_model`, `asr_model`, `query_understand_model`, `follow_up_model`; long-term memory binding values: `embedding_model`, `extract_model`. Details include the object `id`, `name`, merged `bindings`, and `knowledge_base_total` / `agent_total`. Each list contains at most 50 items; the deletion guard is based on the totals.

```bash
curl -X DELETE $BASE/api/v1/models/m-1 -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/models/:id/credentials

Purpose: Set model secrets (secrets are not transmitted via the main PUT). Permission: Admin+ or SystemAdmin. Handler: `internal/handler/model_credentials.go`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `api_key` | *string | No | New API Key |
| `app_secret` | *string | No | New App Secret (if both are omitted, only the status is returned) |

Response: 200 `{"success":true,"data":{"fields":{"api_key":{"configured":bool},"app_secret":{"configured":bool}}}}`

```bash
curl -X PUT $BASE/api/v1/models/m-1/credentials -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"api_key":"sk-..."}'
```

### DELETE /api/v1/models/:id/credentials/:field

Purpose: Delete a secret field (`api_key` or `app_secret`). Permission: Admin+ or SystemAdmin.

Response: 204 No Content

```bash
curl -X DELETE $BASE/api/v1/models/m-1/credentials/api_key -H "Authorization: Bearer $TOKEN"
```

## WeKnoraCloud

Handler: `internal/handler/weknoracloud.go`. API key: `manage_models`/full.

### POST /api/v1/weknoracloud/credentials

Purpose: Save WeKnoraCloud SaaS credentials. Permission: Admin+. Request body: `{"app_id":"...","app_secret":"..."}` (both `binding:"required"`).

Response: 200 `{"success":true,"message":"Credentials saved successfully"}`

```bash
curl -X POST $BASE/api/v1/weknoracloud/credentials -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"app_id":"app","app_secret":"secret"}'
```

### GET /api/v1/models/weknoracloud/status

Purpose: WeKnoraCloud readiness status probe. Permission: Viewer+.

Response: 200 service status object.

```bash
curl $BASE/api/v1/models/weknoracloud/status -H "Authorization: Bearer $TOKEN"
```

## Initialization (/api/v1/initialization)

Handler: `internal/handler/initialization.go`. KB configuration category: API key `manage_kbs` (write) / `retrieve` (read); model detection category: `manage_models` (both accept full-access).

### GET /api/v1/initialization/config/:kbId

Purpose: Read the KB's current model/parsing configuration. Permission: Viewer+, KB read.

The model `baseUrl` is returned only to Admin+ of the space that owns the KB (or full-access / `manage_tenant_settings` API key). Spaces accessed via organization sharing can only see whether credentials are configured (`credentials.*`), not the model addresses and storage bucket information of the source space.

Response: 200 `{"success":true,"data":{"hasFiles",llm,embedding,rerank,multimodal,documentSplitting,nodeExtract,questionGeneration}}`

```bash
curl $BASE/api/v1/initialization/config/kb-1 -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/initialization/initialize/:kbId

Purpose: Initialize the KB's model and parsing configuration (first-time configuration wizard). Permission: KB creator OR Admin+, KB write.

Only the space that owns the KB can call this; spaces that obtained edit permission via organization sharing are rejected (403). If the KB already has bound models, this interface updates those models' configurations in place, which requires the same permission as `PUT /models/:id` (Admin+, or an API key with the `manage_models` capability); otherwise 403.

Main fields (`InitializationRequest`):

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `llm.source` / `llm.modelName` | string | Yes | LLM source and model name |
| `llm.baseUrl` / `llm.apiKey` | string | No | Connection parameters |
| `embedding.source` / `embedding.modelName` | string | Yes | Embedding model |
| `embedding.baseUrl` / `embedding.apiKey` / `embedding.dimension` | — | No | Connection and dimension |
| `rerank.enabled` + `rerank.modelName/baseUrl/apiKey` | — | No | Rerank configuration |
| `multimodal.enabled` + `multimodal.vlm.*` + `multimodal.storageType` + `multimodal.cos.*|minio.*` | — | No | Multimodal and image hosting |
| `documentSplitting.chunkSize` / `separators` | int / []string | Yes | Chunking configuration |
| `documentSplitting.chunkOverlap` | int | No | Overlap |
| `nodeExtract.*` | — | No | Graph extraction (enabled/text/tags/nodes/relations) |
| `questionGeneration.*` | — | No | Question generation (enabled/questionCount) |

Response: 200 `{"success":true,"message":"Knowledge base configuration updated successfully","data":{"models":[Model],"knowledge_base":{KnowledgeBase}}}`

```bash
curl -X POST $BASE/api/v1/initialization/initialize/kb-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"llm":{"source":"remote","modelName":"gpt-4o-mini"},"embedding":{"source":"remote","modelName":"text-embedding-3-small"},"documentSplitting":{"chunkSize":512,"separators":["\n\n"]}}'
```

### PUT /api/v1/initialization/config/:kbId

Purpose: Update the KB model/chunking configuration (`KBModelConfigRequest`: `llmModelId` required; `embeddingModelId`, `vlm_config`, `asr_config`, `documentSplitting.*`, `multimodal.enabled`, `storageProvider`, `storageBackendId`, `nodeExtract.*`, `questionGeneration.*` optional). Permission: KB creator OR Admin+, KB write.

When accessed via organization sharing, the effective share permission must be admin; editor can only edit content, not change settings (403). Storage bindings (`storageBackendId` / `storageProvider`) can only be modified by the space that owns the KB; other spaces submitting values different from the current ones return 403.

Response: 200 `{"success":true,"message":"Configuration updated successfully"}`

```bash
curl -X PUT $BASE/api/v1/initialization/config/kb-1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"llmModelId":"m-1","embeddingModelId":"m-2"}'
```

### GET /api/v1/initialization/ollama/status

Purpose: Ollama availability probe. Permission: Viewer+.

Response: 200 `{"success":true,"data":{"available","version","baseUrl","error"}}`

```bash
curl $BASE/api/v1/initialization/ollama/status -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/initialization/ollama/models

Purpose: List local Ollama models. Permission: Viewer+.

Response: 200 `{"success":true,"data":{"models":[...]}}`

```bash
curl $BASE/api/v1/initialization/ollama/models -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/initialization/ollama/models/check

Purpose: Batch-check whether models already exist. Permission: Admin+. Request body: `{"models":["llama3"]}` (`binding:"required"`).

Response: 200 `{"success":true,"data":{"models":{"llama3":true}}}`

```bash
curl -X POST $BASE/api/v1/initialization/ollama/models/check -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"models":["llama3"]}'
```

### POST /api/v1/initialization/ollama/models/download

Purpose: Pull an Ollama model (asynchronous task). Permission: Admin+. Request body: `{"modelName":"llama3"}` (`binding:"required"`).

Response: 200 `{"success":true,"data":{"taskId","modelName","status","progress"}}`

```bash
curl -X POST $BASE/api/v1/initialization/ollama/models/download -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"modelName":"llama3"}'
```

### GET /api/v1/initialization/ollama/download/progress/:taskId

Purpose: Download task progress. Permission: Viewer+.

Response: 200 `{"success":true,"data":{id,modelName,status,progress,message,startTime,endTime}}`

```bash
curl $BASE/api/v1/initialization/ollama/download/progress/task-1 -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/initialization/ollama/download/tasks

Purpose: List all download tasks. Permission: Viewer+.

Response: 200 `{"success":true,"data":[DownloadTask]}`

```bash
curl $BASE/api/v1/initialization/ollama/download/tasks -H "Authorization: Bearer $TOKEN"
```

### Model connectivity checks (all POST, permission Admin+)

The request body is uniformly `ModelTestRequest`:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `source` | string | No | Defaults to `remote` |
| `modelName` | string | Yes | Model name |
| `baseUrl` / `apiKey` / `appSecret` | string | No | Connection parameters |
| `provider` / `interfaceType` | string | No | Provider/interface type |
| `dimension` / `supportsDimensionOverride` | int / bool | No | embedding dimension; whether to specify the dimension in the request |
| `customHeaders` / `extraConfig` | map | No | Extensions |
| `spec` | object | No | Single-row catalog override, same as the model's `parameters.spec` |
| `modelId` | string | No | Existing model ID: missing secrets, `extraConfig`, and `spec` in the request are filled in from that model |

| Endpoint | Purpose | Response data |
| --- | --- | --- |
| `POST /api/v1/initialization/remote/check` | LLM remote connectivity | `{available,message}` |
| `POST /api/v1/initialization/embedding/test` | Embedding test | `{available,message,dimension}` |
| `POST /api/v1/initialization/rerank/check` | Rerank test | `{available,message}` |
| `POST /api/v1/initialization/asr/check` | ASR test | `{available,message}` |

```bash
curl -X POST $BASE/api/v1/initialization/remote/check -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"modelName":"gpt-4o-mini","baseUrl":"https://api.openai.com/v1","apiKey":"sk-..."}'
```

### POST /api/v1/initialization/multimodal/test

Purpose: End-to-end multimodal (VLM + image hosting) test. Permission: Admin+. multipart fields: `image` (required), `vlm_model`, `vlm_base_url` (required), `vlm_api_key`, `vlm_interface_type`, `storage_type` (`cos|minio`, required) and the corresponding `cos_*`/`minio_*` fields, `chunk_size`, `chunk_overlap`, `separators`.

Response: 200 `{"success":true,"data":{"success","caption","ocr","processing_time"}}`

```bash
curl -X POST $BASE/api/v1/initialization/multimodal/test -H "Authorization: Bearer $TOKEN" \
  -F 'image=@demo.png' -F 'vlm_model=qwen-vl' -F 'vlm_base_url=http://x' -F 'storage_type=minio'
```

### POST /api/v1/initialization/extract/text-relation

Purpose: Text graph extraction test. Permission: Admin+. Request body: `text` (required, ≤5000 characters), `tags` (required, at least one), `model_id` (required).

Response: 200 `{"success":true,"data":{"nodes":[GraphNode],"relations":[GraphRelation]}}`

```bash
curl -X POST $BASE/api/v1/initialization/extract/text-relation -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"text":"Alice works at Tencent","tags":["Person","Company"],"model_id":"m-1"}'
```

### POST /api/v1/initialization/extract/fabri-tag

Purpose: Generate sample tags. Permission: Admin+. No request body.

Response: 200 `{"success":true,"data":{"tags":[...]}}`

```bash
curl -X POST $BASE/api/v1/initialization/extract/fabri-tag -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/initialization/extract/fabri-text

Purpose: Generate sample text by tags. Permission: Admin+. Request body: `{"tags":[...],"model_id":"m-1"}` (model_id required).

Response: 200 `{"success":true,"data":{"text":"..."}}`

```bash
curl -X POST $BASE/api/v1/initialization/extract/fabri-text -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"model_id":"m-1","tags":["Person"]}'
```

## Evaluation (/api/v1/evaluation)

Handler: `internal/handler/evaluation.go`. API key: `run_evaluations`/full.

### POST /api/v1/evaluation

Purpose: Launch an evaluation task (drives LLM calls, incurring charges). Permission: Admin+.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `dataset_id` | string | No | Dataset ID |
| `knowledge_base_id` | string | No | Target KB |
| `chat_id` | string | No | Chat model ID |
| `rerank_id` | string | No | Rerank model ID |

Response: 200 `{"success":true,"data":{evaluation task}}`

```bash
curl -X POST $BASE/api/v1/evaluation -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"knowledge_base_id":"kb-1","chat_id":"m-1"}'
```

### GET /api/v1/evaluation

Purpose: Query evaluation results. Permission: Viewer+. Query parameter: `task_id` (required).

Response: 200 `{"success":true,"data":{evaluation result}}`

```bash
curl "$BASE/api/v1/evaluation?task_id=task-1" -H "Authorization: Bearer $TOKEN"
```

## Implementation Reference

Route registration: `internal/router/router.go` calls `RegisterModelRoutes`, `RegisterInitializationRoutes`, `RegisterEvaluationRoutes`, `RegisterWeKnoraCloudRoutes` (defined in `internal/router/routes_infra.go`). Handlers: `internal/handler/model.go`, `internal/handler/model_catalog.go`, `internal/handler/model_credentials.go`, `internal/handler/initialization.go`, `internal/handler/evaluation.go`, `internal/handler/weknoracloud.go`.
