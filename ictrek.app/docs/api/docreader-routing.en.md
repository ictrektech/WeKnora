# Cross-node compute setup and API

Install the full HybRAG app on both VOS machines. Open the provider’s VOS HTTPS address, go to **System settings → Compute nodes**, enable sharing and generate a one-time access code. Paste it into the same settings page on the consumer and select **Add node**. Adding the first node in the UI enables ordered routing. Select **Save routing** after changing the strategy, order, or weights. Saved changes apply to new tasks. Leaving the page discards unsaved edits. The API contract below still applies. The provider obtains its local VOS gateway certificate fingerprint automatically. Both nodes retain the PGV dependency.

Use **Check** on paired nodes to inspect their status. The document parsing stage shows the selected node and strategy. Web imports use the local reader. Turning sharing off pauses new remote calls. Turning it back on restores existing authorizations. Consumers need new codes after all pairings are revoked.

## Document processing sharing

On the provider, select an available vision model and choose **Share document processing**. On the paired consumer, select **Check**, enable **Use document processing** for that node, then save routing. The chosen node parses eligible PDFs. When multimodal recognition is enabled for a document, the same node performs OCR on scanned pages and generates image descriptions, returning executor-measured OCR and caption durations. The consumer writes the results and the trace shows where each step ran and how long it took. **Stop sharing document processing** leaves existing authorizations and DocReader sharing in place.

Document processing applies to PDFs using the built-in parser and default image processing settings. Custom image processing settings and other file types follow the existing workflow. When document processing is enabled, eligible PDFs go to opted-in remote nodes or the local reader if it is explicitly in the routing group. The group can pick another candidate before sending the file. An interrupted run discards partial results. Reprocessing publishes replacement chunks after their index writes succeed. The prior chunks and indexes remain available if replacement fails.

## Text inference sharing (auto question generation and summary)

On the provider, open **System settings → Compute nodes**, select one or more local chat models under **Text inference** (reusing local V-Scheduler or a compatible model service), and choose **Share text inference**. Choosing **Stop sharing text inference** disables text inference while keeping existing pairings, DocReader, and document processing sharing intact. Existing pairings retain only their original authorizations until the provider explicitly enables text inference.

On the consumer, reuse the existing pairing without entering model URLs or credentials. Select **Check** on the paired node to discover its authorized text models. Then configure per-knowledge-base compute targets independently (existing model settings remain compatible):

- **Document summary** (Knowledge Base Settings → Model Settings → Summary compute node): leave empty to keep the knowledge base's local summary model; select a compute node model to offload document summary and summary regeneration while the primary node persists results and updates indexes.
- **Auto question generation** (Knowledge Base Settings → Advanced Settings → Auto-generate questions → Question generation compute node): leave empty to keep the existing local model; select a compute node model to offload question generation and regeneration while the primary node saves and indexes generated questions.

### Busy retry, explicit fallback, and deduplication

- When the provider's local V-Scheduler returns HTTP `429 WAIT_COMPUTE_RESOURCE` (propagated as gRPC `ResourceExhausted` with `WAIT_COMPUTE_RESOURCE` metadata), the consumer performs bounded exponential backoff retries against that target (default: 5 retries, 2 s initial backoff, 30 s max backoff; configurable via `HYBRAG_COMPUTE_BUSY_MAX_RETRIES`, `HYBRAG_COMPUTE_BUSY_BASE_BACKOFF_MS`, `HYBRAG_COMPUTE_BUSY_MAX_BACKOFF_MS`). On the provider side, per-model shared concurrency is bounded (`HYBRAG_COMPUTE_TEXT_CONCURRENCY`, default `1`) to prevent concurrent summary and question batches from colliding on a single GPU permit.
- **Strictly explicit fallback**: when no additional fallback compute target is configured and **Allow fallback to local model** (`fallback_to_local: false`, default) is unchecked, exhausted retries fail the task and record the failure reason in Trace without silently falling back to the local node. Switching occurs only across explicitly configured targets or when `fallback_to_local: true` is enabled.
- **Retry deduplication**: generated questions use deterministic IDs (`q-r<revision>-<index>`) and record the parse attempt plus an indexed flag on each chunk; when a batch task retries after a transient busy error, already-committed chunks are skipped so results are written only once. Summary retries update the existing summary chunk in place rather than appending duplicates.

## Management endpoints

The VOS API base is `https://<VOS_HOST>:<VOS_PORT>/app/com.ictrek.hybrag/api/v1`. Management endpoints below require a signed-in HybRAG system administrator and `Authorization: Bearer <HybRAG user token>`, except `GET /system/compute-text-models`, which any signed-in workspace member may call when editing knowledge base settings. Send write requests with `Content-Type: application/json`.

| Method and relative path | Request | HTTP 200 |
| --- | --- | --- |
| GET `/system/admin/docreader-routing` | No body | `{"available":true,"config":{...}}`, or `{"available":false}` in deployments without routing. |
| PUT `/system/admin/docreader-routing` | Complete config, up to 32 KiB | Returns current `available` and `config` with the revision incremented. |
| POST `/system/admin/docreader-routing/probe` | Node `origin`, `token`, optional `fingerprint`, up to 4 KiB | Returns `component` (`docreader`), normalized `origin`, `version`, `engines`, optional `capabilities` (`document_processing`, `text_inference`), and `text_models` when text inference is shared. Discovered `text_models` are cached on the saved node without incrementing `revision`. Probe deadline is 4 seconds. |
| GET `/system/admin/docreader-sharing` | No body | `{"available":true,"sharing":{"enabled":false,"document_processing":false,"text_inference":false,"paired_nodes":0}}`. Includes `document_model_id` and `text_model_ids` when configured. Returns only `available:false` when unavailable. |
| PUT `/system/admin/docreader-sharing` | Up to 4 KiB. Send at least one change to `enabled`, `revoke:true`, `document_processing`, `document_model_id`, `text_inference`, or `text_model_ids` (string array). | Returns the current sharing state. Disabling sharing invalidates a pending code. Revocation clears credentials and the pending code. |
| POST `/system/admin/docreader-sharing/code` | Up to 4 KiB. Required `origin`, the current VOS HTTPS origin with its port. | Returns string `code` and RFC 3339 `expires_at`. A new code replaces the old one and is valid for 10 minutes and one redemption. Maximum 32 authorized nodes. |
| POST `/system/admin/docreader-routing/pair` | Up to 8 KiB. Required `code` (up to 4096 characters) and current `revision`. Optional boolean `enable_if_empty` defaults to `false`. | Returns `config` and `node`. Re-pairing the same origin replaces its credential and preserves its weight. New nodes start at weight 1. With `enable_if_empty=true`, adding the first node enables ordered routing. Other requests preserve the current routing strategy and enabled state. |
| GET `/system/compute-text-models` | No body (any signed-in workspace member) | Returns `{"available":true,"nodes":[{"origin":"https://...","models":[{"id":"...","name":"...","display_name":"..."}]}]}` listing paired nodes with discovered text models. Returns an empty `nodes` list when unavailable. |

The initial config has routing disabled, strategy `order`, an empty node list, and revision `0`.

| Config field | Definition |
| --- | --- |
| `revision` | Unsigned integer returned by the last read. Reload and merge edits on conflict. |
| `enabled` | Boolean. Enabling requires one or more nodes. Disabling preserves the list and uses the local reader. |
| `strategy` | `order` tries nodes in configured order. `weight` uses weighted round robin by number of documents. Equal weights alternate. |
| `nodes` | Ordered array of up to 16 nodes. Origins are unique. |

| Node field | Definition |
| --- | --- |
| `origin` | `local` for the local reader, or an HTTPS origin with optional port (default 443) and no path, credentials, query or fragment. Probes require a remote origin. |
| `weight` | Integer 1–100. New nodes default to 1 in the UI. Probes omit this field. |
| `document_processing` | Optional boolean, defaults to `false` and is omitted from responses when off. A remote node with `true` participates in eligible PDF document processing. The local reader participates when included in the routing group. |
| `text_models` | Read-only cached array of authorized text models discovered during the latest **Check** or pairing (`id`, `name`, `display_name`). Preserved across routing saves. |
| `token` | Remote pairing credential, 16–512 printable ASCII characters without spaces. An omitted or empty token retains the saved token for the same origin. Responses omit the token. Omit for the local reader. |
| `fingerprint` | Optional SHA-256 certificate fingerprint, 64 hex characters with optional colon separators. An empty value uses system certificate trust and hostname verification. Omit for the local reader. |

Each item in `engines` contains `Name` and `Description` (strings), `FileTypes` (array of file extensions), `Available` (boolean), and `UnavailableReason` (string, may be empty).

`document_processing` defaults to `false`; enabling it requires a valid vision-capable `document_model_id`. `text_inference` defaults to `false`; enabling it requires a non-empty `text_model_ids` list (up to 16 active `KnowledgeQA` chat models in the current workspace). Missing, invalid, or unavailable models return HTTP 400 and preserve the saved configuration. Paired consumers can discover and invoke a capability only while provider sharing and that capability are both enabled.

### Per-knowledge-base text inference route fields

Knowledge base endpoints (`POST /knowledge-bases`, `PUT /knowledge-bases/:id`, `PATCH /knowledge-bases/:id`, `GET /knowledge-bases/:id`) support two optional fields, both defaulting to `null` (preserving existing local model behavior):

- `summary_compute_config`: cross-node text inference route for document summary generation.
- `question_generation_config.compute_route`: cross-node text inference route for auto question generation.

Both use `ComputeTextRouteConfig`:

```json
{
  "targets": [
    {
      "node_origin": "https://192.168.1.35:1180",
      "model_id": "<provider_model_id>",
      "model_name": "Qwen3.5-9B"
    }
  ],
  "fallback_to_local": false
}
```

| Field | Type | Description |
| --- | --- | --- |
| `targets` | Array (up to 8 items) | Ordered inference targets. The first entry is the primary target; subsequent entries are explicit fallback nodes. `node_origin` is a paired HTTPS origin or `local`; `model_id` must match an authorized discovered model on that node (already-saved targets remain valid when a node is temporarily unreachable). |
| `fallback_to_local` | Boolean, default `false` | Whether to fall back to the knowledge base's local model after all `targets` fail. When `false`, no local fallback occurs and the failure reason is surfaced. |

Selecting an unauthorized or undiscovered remote model returns HTTP 400.

### Errors and Trace telemetry

Errors carry a JSON `message`. HTTP 401 indicates a missing or invalid session, 403 insufficient privileges, 400 invalid input, 404 routing unavailable, 409 stale revision, 500 a save failure, and 502 a remote identity, authorization or connectivity failure. Repeat a probe after correcting the problem. Use the new revision returned by a successful save for the next update.

Processing traces (`GET /knowledge/:id/spans`) record execution node and telemetry fields across `docreader`, `multimodal`, `multimodal.image[i].ocr`, `multimodal.image[i].caption`, `postprocess.summary`, `postprocess.question`, and `postprocess.question.batch[i]`:

| Trace output field | Stages | Description |
| --- | --- | --- |
| `compute_node` | Parsing, OCR, caption, summary, question generation | Executing node (`local` or remote VOS HTTPS origin). |
| `executor_duration_ms` | Cross-node OCR and image captioning | Actual executor-measured inference duration in milliseconds (also recorded as the span duration). |
| `remote_duration_ms` | Cross-node summary and question generation | Executor-measured text inference duration in milliseconds. |
| `busy_retries` | Summary, question generation | Number of backoff retries triggered by `WAIT_COMPUTE_RESOURCE`. |
| `failure_reason` | Failed summary or question generation | Structured reason such as `WAIT_COMPUTE_RESOURCE`, `NODE_UNAVAILABLE`, `UNAUTHORIZED`, `MODEL_NOT_FOUND`, `TIMEOUT`, or `INFERENCE_FAILED`. |
| `fallback_from` | When explicit fallback occurred | Attempt records for earlier failed targets (`node`, `model_id`, `reason`, `busy_retries`, `error`). |

The pairing response uses the probe schema for `node`. Code creation returns 409 when sharing is off or the credential limit is reached. An invalid or expired remote code returns 502. Generate a new code to retry. Sharing and pairing responses are served with no-store.

## Node compute gateway gRPC endpoints

Paired nodes invoke `docreader.DocumentCompute` through the VOS HTTPS gateway (`/docreader.DocumentCompute/`) with the pairing credential as a Bearer token. [Message schema](../../../docreader/proto/document_compute.proto)

### `/docreader.DocumentCompute/Process` (Document processing)

Accepts a PDF upload with engine empty or `builtin`, subject to the DocReader file size limit. Boolean `recognize_images` defaults to off; turning it on runs OCR on scanned pages and generates descriptions for returned images. The optional description language is limited to 64 bytes and defaults to the provider's language. The provider manages the model and its credentials.

The response stream starts with document text, source positions, and parser metadata. Each subsequent frame carries image bytes, OCR text, image description, execution flags, and executor-measured `ocr_duration_ms` and `description_duration_ms`. Parser-reported document errors appear in the first frame. An invalid credential returns `Unauthenticated`; disabled sharing or capability returns `PermissionDenied`; unsupported input returns `InvalidArgument`; oversized file returns `ResourceExhausted`; missing metadata or image data returns `DataLoss`; unavailable model or recognition failure returns `Unavailable`.

### `/docreader.DocumentCompute/ListTextModels` (Discover text models)

Returns the provider's currently authorized text models (`models[]` with `id`, `name`, `display_name`) during node **Check**. Returns `PermissionDenied` when sharing or text inference is disabled.

### `/docreader.DocumentCompute/GenerateText` (Run text inference)

Accepts `model_id`, `messages[]` (`role`, `content`), optional `temperature`, `max_tokens`, `disable_thinking`, and `purpose` (`document_summary`, `question_generation`). The provider verifies `model_id` is authorized, invokes its local V-Scheduler or compatible model service, and returns `content`, `finish_reason`, token usage counts, `duration_ms`, and `model_name`.

When local V-Scheduler returns HTTP `429 WAIT_COMPUTE_RESOURCE`, the gateway returns gRPC `ResourceExhausted` with trailing metadata `x-hybrag-error-code: WAIT_COMPUTE_RESOURCE` (and optional `x-hybrag-retry-after-ms`) so the consumer can perform bounded backoff retries.
