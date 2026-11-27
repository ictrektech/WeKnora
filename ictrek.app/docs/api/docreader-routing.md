# 跨节点算力节点配置与 API

两台 VOS 都完整安装 HybRAG。提供方管理员通过本机 VOS HTTPS 地址打开「系统设置 → 算力节点」，开启共享并生成一次性接入码。使用方管理员在同页粘贴接入码并点击「添加节点」。添加首个节点后，界面自动启用顺序路由。后续调整策略、顺序或权重时点击「保存路由」。更改在保存后对新任务生效，离开页面会丢弃未保存的修改。API 契约沿用下表。生成接入码时自动读取本机 VOS 网关证书指纹。两个节点均沿用 PGV 依赖。

已配对的路由可通过「检查」查询状态。文档进度的「文档解析」阶段显示实际节点和策略，网页导入走本机。关闭提供方共享暂停新的远端调用，再开启后恢复已有授权。撤销全部授权后，使用方需要重新配对。

## 文档处理共享

提供方在同页选一个可用的视觉模型，并点「开放文档处理」。已配对的使用方点击「检查」，勾选该节点的「使用文档处理」并保存路由。符合条件的 PDF 由选中的节点完成解析。文档启用了多模态识别时，该节点还会对扫描页做 OCR、生成图片描述，并回传执行端实测的 OCR 与图片描述耗时。使用方负责入库，处理进度的 Trace 可查看每步的执行节点与耗时。「停止共享文档处理」关闭该能力并保留原配对和 DocReader 共享。

文档处理适用于使用内置引擎的 PDF 和默认的图像处理配置。自定义图像处理及其他文件格式沿用原流程。启用文档处理后，此类 PDF 只会分配给已启用该能力的远端和明确加入路由的本机。如果节点在提交前不可用，按组内策略尝试其他候选。处理中失败时，不采用不完整的结果。重新解析会在新分块和索引都写好后切换，失败时保留原有分块和索引。

## 文本推理共享（自动问题生成与文档摘要）

提供方管理员在「系统设置 → 算力节点」的「文本推理」区域勾选一个或多个本机可用的对话模型（复用当地 V-Scheduler 或兼容模型服务），点击「开放文本推理」即可向已配对节点暴露这些模型。点击「停止共享文本推理」关闭文本推理能力，同时保留原配对、DocReader 和文档处理共享。已有配对在提供方主动开放前默认只保留原有授权。

使用方沿用现有配对，无需填写模型地址和凭据，在「算力节点」点击「检查」即可发现该节点已授权的文本模型列表。随后在具体知识库的设置页中，可分别为以下两项独立选择算力节点模型（逐库启用，兼容已有模型设置）：

- **文档摘要**（知识库设置 → 模型配置 → 摘要算力节点）：留空沿用知识库原有本机摘要模型；选择算力节点模型后，该知识库的文档摘要与重新生成摘要由所选节点执行，结果回传主节点保存和更新索引。
- **自动问题生成**（知识库设置 → 高级设置 → 自动生成问题 → 问题生成算力节点）：留空沿用原有问题生成模型；选择算力节点模型后，该知识库的自动问题生成与重新生成问题由所选节点执行，结果回传主节点保存和入库。

### 繁忙重试、兜底切换与去重

- 当提供方当地 V-Scheduler 返回 `429 WAIT_COMPUTE_RESOURCE`（经由算力网关映射为 gRPC `ResourceExhausted` 与 `WAIT_COMPUTE_RESOURCE` 响应头）时，主节点对该目标执行限次指数退避重试（默认最多重试 5 次，初始退避 2 秒，上限 30 秒，可通过 `HYBRAG_COMPUTE_BUSY_MAX_RETRIES`、`HYBRAG_COMPUTE_BUSY_BASE_BACKOFF_MS`、`HYBRAG_COMPUTE_BUSY_MAX_BACKOFF_MS` 调整）。提供方侧默认按模型限制跨节点并发（`HYBRAG_COMPUTE_TEXT_CONCURRENCY`，默认 `1`），降低同节点摘要与批量问题并发争抢。
- **严格显式兜底**：未配置备用算力节点且未勾选「允许回退到本机模型」（`fallback_to_local: false`，默认关闭）时，主节点在重试耗尽后直接报错并在 Trace 记录失败原因，不会静默切回本机。仅当知识库显式配置了多个备选节点或勾选 `fallback_to_local: true` 时，才按配置顺序切换。
- **重试去重**：问题生成按确定性 ID（`q-r<revision>-<index>`）写入并记录当前解析轮次和已入库标记，批次中途因节点繁忙触发任务重试时，已成功入库的分块直接跳过不重复写入；摘要生成重试时原地更新已有摘要分块，不产生重复摘要分块。

## 管理接口

VOS API 基址为 `https://<VOS_HOST>:<VOS_PORT>/app/com.ictrek.hybrag/api/v1`。下表管理接口要求已登录的 HybRAG 系统管理员和 `Authorization: Bearer <HybRAG 用户令牌>`，仅 `/system/compute-text-models` 允许普通空间成员读取以配置知识库。写请求使用 `Content-Type: application/json`。

| 方法与相对路径 | 请求 | HTTP 200 |
| --- | --- | --- |
| GET `/system/admin/docreader-routing` | 无正文 | `{"available":true,"config":{...}}`。部署不支持时为 `{"available":false}`。 |
| PUT `/system/admin/docreader-routing` | 完整路由配置，最多 32 KiB | 返回当前 `available` 和 `config`，修订加一。 |
| POST `/system/admin/docreader-routing/probe` | 节点的 `origin`、`token`、可选 `fingerprint`，最多 4 KiB | `component` 为 `docreader`，`origin` 为归一化地址，`version` 为服务版本字符串，`engines` 为解析引擎数组。可选 `capabilities` 字符串数组可包含 `document_processing`、`text_inference`。当提供方开放文本推理时返回 `text_models` 数组，并缓存到已保存节点（不递增 `revision`）。探测最多等待 4 秒。 |
| GET `/system/admin/docreader-sharing` | 无正文 | `{"available":true,"sharing":{"enabled":false,"document_processing":false,"text_inference":false,"paired_nodes":0}}`。已配置时包含 `document_model_id` 与 `text_model_ids`。不可用时仅返回 `available:false`。 |
| PUT `/system/admin/docreader-sharing` | 最多 4 KiB。可组合提交 `enabled`、`revoke:true`、`document_processing`、`document_model_id`、`text_inference`、`text_model_ids`（字符串数组）。至少指定一项变更。 | 返回当前共享状态。关闭共享时作废待兑换码；撤销清除现有授权和待兑换码。 |
| POST `/system/admin/docreader-sharing/code` | 最多 4 KiB。必填 `origin`（当前 VOS HTTPS 基址，含实际端口）。 | 返回字符串 `code` 和 RFC 3339 `expires_at`。新码替换旧码，有效 10 分钟且仅能兑换一次。最多保存 32 个授权。 |
| POST `/system/admin/docreader-routing/pair` | 最多 8 KiB。必填 `code`（最多 4096 字符）、当前 `revision`。可选布尔 `enable_if_empty`，默认 `false`。 | 返回 `config` 和 `node`。同地址更新凭据、保留权重。新节点权重为 1；`enable_if_empty=true` 且原列表为空时启用顺序路由，其他情况保留原有策略和启用状态。 |
| GET `/system/compute-text-models` | 无正文（任意已登录空间成员可调用） | 返回 `{"available":true,"nodes":[{"origin":"https://...","models":[{"id":"...","name":"...","display_name":"..."}]}]}`，列出已配对且已通过「检查」发现文本模型的远端节点。未配置或不可用时 `nodes` 为空数组。 |

路由配置如下。初始值为未启用、按顺序、空节点列表和修订 `0`。

| 字段 | 定义 |
| --- | --- |
| `revision` | 无符号整数。保存时提交读取到的修订。冲突时重新读取并合并。 |
| `enabled` | 布尔值。启用后需配置至少一个节点，关闭时保留列表并使用本机。 |
| `strategy` | `order` 按节点顺序，`weight` 按文件数加权轮询。权重相同时轮流分配。 |
| `nodes` | 有序数组，最多 16 个成员，地址不可重复。每项含下表字段。 |

| 节点字段 | 定义 |
| --- | --- |
| `origin` | `local` 代表本机，或填写 HTTPS 基址（可含端口，缺省 443，不含路径、用户信息、查询及片段）。探测只接受远端地址。 |
| `weight` | 整数 1–100，设置页新节点默认 1。探测无需此字段。 |
| `document_processing` | 可选布尔值，默认 `false`。远端为 `true` 时参与符合条件的 PDF 文档处理。读取时省略 `false`。本机由是否加入路由组决定是否参与。 |
| `text_models` | 只读缓存数组，记录最近一次「检查」或配对时从该节点发现的已授权文本模型（每项含 `id`、`name`、`display_name`）。保存路由时自动保留。 |
| `token` | 远端节点配对凭据，16–512 个不含空格的可打印 ASCII 字符。省略或留空时，同地址的已保存节点继续使用现有凭据。响应省略凭据。本机省略此字段。 |
| `fingerprint` | 可选的证书 SHA-256 指纹，64 个十六进制字符，可带冒号。留空时使用系统证书链及主机名校验。本机省略此字段。 |

`engines` 的每项含 `Name`、`Description`（字符串），`FileTypes`（文件扩展名数组），`Available`（布尔值）和 `UnavailableReason`（字符串，可为空）。

`document_processing` 默认为 `false`。提交 `true` 时同时提供 `document_model_id`（当前工作区可用的视觉模型或支持视觉输入的问答模型）。`text_inference` 默认为 `false`。提交 `true` 时同时提供非空的 `text_model_ids`（每个模型必须是当前工作区可用的 `KnowledgeQA` 类型对话模型，最多 16 个）。缺少、无效或不可用的模型返回 HTTP 400，并保留原设置。只有本机共享与对应能力均启用时，已配对远端才能探测和调用该能力。

### 知识库跨节点文本推理配置字段

知识库创建与更新接口（`POST /knowledge-bases`、`PUT /knowledge-bases/:id`、`PATCH /knowledge-bases/:id`、`GET /knowledge-bases/:id`）新增以下可选字段，均默认为 `null`（沿用原有本机模型设置）：

- `summary_compute_config`：文档摘要的跨节点文本推理路由配置。
- `question_generation_config.compute_route`：自动问题生成的跨节点文本推理路由配置。

两者共用 `ComputeTextRouteConfig` 结构：

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

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `targets` | 数组（最多 8 项） | 按优先级排序的推理目标列表。首项为主执行目标，其余为显式备选节点。`node_origin` 为已配对节点的归一化 HTTPS 地址或 `local`；`model_id` 必须属于该节点已发现的授权模型（已保存的原目标在节点临时离线时允许保留）。 |
| `fallback_to_local` | 布尔值，默认 `false` | 全部 `targets` 失败后是否允许回退到知识库原有本机模型。为 `false` 时不回退并直接上报失败原因。 |

配置未授权或尚未「检查」发现的远端模型时，接口返回 HTTP 400。

### 错误与 Trace 观测字段

错误返回 JSON `message`。无有效会话时返回 401，权限不足时返回 403。400 表示请求格式或字段无效，404 表示部署未启用管理接口，409 表示修订过期，500 表示保存失败，502 表示远端身份校验、授权或服务连接失败。管理员可用同一探测请求检查修复结果。保存成功后应使用返回的新修订发起下一次修改。

文档处理 Trace（`GET /knowledge/:id/spans`）在 `docreader`、`multimodal`、`multimodal.image[i].ocr`、`multimodal.image[i].caption`、`postprocess.summary`、`postprocess.question` 和 `postprocess.question.batch[i]` 等节点均记录执行节点与关键观测字段：

| Trace 输出字段 | 适用阶段 | 说明 |
| --- | --- | --- |
| `compute_node` | 解析、OCR、图片描述、摘要、问题生成 | 实际执行节点（`local` 或远端 VOS HTTPS 基址）。 |
| `executor_duration_ms` | 跨节点 OCR 与图片描述 | 执行端实测的模型识别耗时（毫秒），同时作为对应 Span 的展示耗时。 |
| `remote_duration_ms` | 跨节点摘要、问题生成 | 执行端实测的文本推理耗时（毫秒）。 |
| `busy_retries` | 摘要、问题生成 | 因 `WAIT_COMPUTE_RESOURCE` 触发的退避重试次数。 |
| `failure_reason` | 失败的摘要、问题生成 | 结构化失败原因，如 `WAIT_COMPUTE_RESOURCE`、`NODE_UNAVAILABLE`、`UNAUTHORIZED`、`MODEL_NOT_FOUND`、`TIMEOUT`、`INFERENCE_FAILED`。 |
| `fallback_from` | 发生显式兜底切换时 | 切换前各失败节点的尝试记录（含 `node`、`model_id`、`reason`、`busy_retries`、`error`）。 |

配对返回的 `node` 采用探测响应结构。接入码签发在未开启共享或授权达上限时返回 409。错误或过期的远端接入码返回 502，需重新签发。状态与配对响应禁止缓存。

## 节点算力网关 gRPC 接口

已配对节点通过 VOS HTTPS 网关调用 `docreader.DocumentCompute` 服务（路径前缀 `/docreader.DocumentCompute/`），使用节点配对凭据作为 Bearer 令牌。[消息结构](../../../docreader/proto/document_compute.proto)

### `/docreader.DocumentCompute/Process`（文档处理）

仅接收有文件内容的 PDF，解析引擎字段接受空值和 `builtin`。文件大小沿用 DocReader 上限。布尔字段 `recognize_images` 默认关闭。开启后，对扫描页执行 OCR，并为返回的图片生成描述。描述语言可选，最多 64 字节，省略时使用提供方默认语言。模型和调用凭据由提供方管理。

服务端先发一个包含正文、来源定位和解析元数据的帧，再逐张发图片字节、OCR 文本、图片描述、是否执行识别的标记，以及执行端实测的 `ocr_duration_ms` 与 `description_duration_ms`。识别到无文字时 OCR 文本可为空。解析器返回文档错误时，错误随首帧返回。无效凭据返回 `Unauthenticated`，关闭共享或能力返回 `PermissionDenied`，不支持的文件或请求参数返回 `InvalidArgument`，文件超限返回 `ResourceExhausted`，解析流缺少元数据或图片返回 `DataLoss`，模型不可用或识别失败返回 `Unavailable`。

### `/docreader.DocumentCompute/ListTextModels`（发现文本模型）

用于配对节点在「检查」时获取提供方当前授权开放的文本模型清单，返回 `models[]`（含 `id`、`name`、`display_name`）。提供方未开启共享或未开放「文本推理」时返回 `PermissionDenied`。

### `/docreader.DocumentCompute/GenerateText`（执行文本推理）

请求包含 `model_id`、`messages[]`（`role`、`content`）、可选 `temperature`、`max_tokens`、`disable_thinking` 与 `purpose`（如 `document_summary`、`question_generation`）。提供方校验模型是否在已开放清单中后，通过当地 V-Scheduler 或兼容模型服务执行推理并返回 `content`、`finish_reason`、`prompt_tokens`、`completion_tokens`、`total_tokens`、`duration_ms` 和 `model_name`。

当当地 V-Scheduler 返回 HTTP `429 WAIT_COMPUTE_RESOURCE` 时，网关返回 gRPC `ResourceExhausted` 并在 trailing metadata 附加 `x-hybrag-error-code: WAIT_COMPUTE_RESOURCE`（及可选 `x-hybrag-retry-after-ms`），供主节点执行限次退避重试。
