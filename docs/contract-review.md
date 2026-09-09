# 合同审查

本文面向接手、维护或重构合同审查功能的开发者，描述当前仓库中的工作台行为、接口契约、数据关系、失败降级和必须保持的不变量。代码、配置、测试和迁移是事实源；本文只在功能边界或实现变化时同步更新，不把目标方案写成当前行为。

## 快速视图

| 能力 | 状态 | 当前结论 |
| --- | --- | --- |
| 工作台上传、解析、异步审查和结果展示 | `已实现` | `/platform/contract-review`；单条记录一次上传一个 PDF 或 DOCX |
| 风险、问题、修改建议、事实和警告 | `已实现` | 问题来自分片模型审查；事实和结构警告由服务端从全文确定性提取 |
| 原文定位和 PDF/DOCX 高亮 | `部分落地` | 依赖解析器提供有效 `source_units_json`，渲染文本不一致时只做受限精确降级 |
| 规则集 | `部分落地` | 当前只有 `general-contract-review` `1.0`，没有自定义规则集接口 |
| 任务取消、旧任务隔离和卡住任务回收 | `已实现` | Redis/Asynq 有队列清理；Lite 内联执行器不能出队，但数据库状态保护仍生效 |
| 完整合同版本管理、版本对比、红线版编辑、结果导出 | `未实现` | 不能把审查记录当作合同业务版本或可编辑文档 |
| 每个部署的解析器定位质量 | `待验证` | 需要确认实际 DocReader 版本是否返回有效 `source_units_json`；缺失时审查仍可运行，但定位降级 |

AI 结果只是辅助分析，不替代律师意见、业务审批或正式法律审查。工作台与 [`skills/preloaded/contract-review/SKILL.md`](../skills/preloaded/contract-review/SKILL.md) 是两个入口：工作台执行异步、结构化、机器可校验的审查；对话式 Agent 才执行该 Skill、知识库工具和网页搜索。工作台不会直接执行 Agent 的完整报告提示词，因此不承诺每个问题都有知识库、网页或案例引用。

法律工作台开关、数据保留和永久删除的通用规则见 [`ictrek.app/docs/legal-workspace.md`](../ictrek.app/docs/legal-workspace.md)。合同审查只补充该文档未覆盖的路由和资源清理细节。

## 代码导航和主数据流

| 关注点 | 入口 | 修改时先确认 |
| --- | --- | --- |
| 路由、权限和 HTTP 映射 | [`internal/router/routes_contract_review.go`](../internal/router/routes_contract_review.go)、[`internal/handler/contract_review.go`](../internal/handler/contract_review.go) | API 前缀、Viewer/Owner、开关、错误码、SSE |
| 状态、上传和任务编排 | [`internal/application/service/contract_review.go`](../internal/application/service/contract_review.go) | 状态转换、运行 ID、配置快照、资源清理、超时 |
| 证据、事实和质量 | [`internal/application/service/contract_review_quality.go`](../internal/application/service/contract_review_quality.go) | 原文范围、source unit、校验、警告和降级 |
| 持久化和领域结构 | [`internal/application/repository/contract_review.go`](../internal/application/repository/contract_review.go)、[`internal/types/contract_review.go`](../internal/types/contract_review.go) | 租户/用户隔离、条件更新、事务和兼容字段 |
| 前端 API、状态和预览 | [`frontend/src/api/contract-review.ts`](../frontend/src/api/contract-review.ts)、[`frontend/src/stores/contractReview.ts`](../frontend/src/stores/contractReview.ts)、[`frontend/src/views/legal/contract-review/`](../frontend/src/views/legal/contract-review/) | SSE/轮询、集合加载状态、定位状态和高亮安全性 |
| 内置 Agent 配置 | [`config/builtin_agents.yaml`](../config/builtin_agents.yaml) | 只用于模型选择回退；不要把完整 Agent 提示词当作工作台协议 |

工作台的数据流是：

```text
创建记录 draft
  → 上传校验、保存源文件、绑定 source_file、清理旧结果
  → contract_review:document_process
  → DocumentReader 返回 MarkdownContent + metadata.source_units_json
  → 保存精确源文本、source_text_hash/source_revision 和 locator
  → ready
  → start/retry 创建新的 analysis_run_id 和 config_hash
  → contract_review:analyze
  → 自动分片 + 跨片段上下文 + KnowledgeQA 结构化 JSON
  → 校验证据并写入 clauses/issues
  → 对完整源文本提取 facts、冲突和结构 warnings
  → 用已验证数据生成 overview，服务端计算风险
  → completed(valid/degraded) 或 failed/取消为 cancelled
```

上传解析失败、模型或结构化校验不可恢复失败都走 `failed`。运行中可转为 `cancelled`；取消或旧任务返回后，记录状态和运行条件阻止迟到 worker 再写结果。

### 核心概念之间的关系

| 概念 | 当前语义和约束 |
| --- | --- |
| 源文件/资源 | 文件服务保存原始 PDF/DOCX，合同记录保存 `resource_ref`（不出现在 API JSON）；资源目录以 `source_file` 关系绑定记录 |
| `ExtractedContent` | DocReader 返回的完整 Markdown 文本；服务保存原样，不能 `TrimSpace`，因为所有 hash 和偏移都基于该值；该字段不直接序列化到 API |
| `source_text_hash` | `sha256(ExtractedContent)` 的十六进制值；数据库还保留内部兼容别名 `source_hash`，新 API 使用 `source_text_hash` |
| `source_revision` | 解析文本的身份标识；优先采用解析器 metadata 中的值，否则为 `text-v2:<source_text_hash>`；它不是合同业务版本号 |
| source unit | 解析器为页、页图、正文块、表格行或表格单元等提供的源范围；`source_start`/`source_end` 是 `rune`、左闭右开范围 |
| locator | `version: 1` 的定位信封，保存版本、偏移单位、源 hash/revision 和有效 units；无有效 unit 时审查可继续，但会产生 `LOCATOR_UNSUPPORTED` |
| 分析片段/`clause` | 服务按源文本切出的分析窗口，不保证是正式法律条款；保存窗口范围和由范围派生的 `evidence_id` |
| `issue` | 模型或确定性结构规则产生的问题；保存风险、类别、发现类型、说明、原文、建议、证据引用及精确源范围 |
| `fact` | 服务从全文提取的可核验事实，当前放在 `overview.facts` JSON 中而不是独立表；事实状态类型预留了 `present`、`ambiguous`、`conflict`、`not_found`，当前确定性提取器实际写入 `present` |
| `warning` | 质量、事实冲突、结构或模型降级信号；部分结构 warning 还会物化为普通 issue，便于复用同一证据展示流程 |
| `overview` | 对已验证 issue/fact 的摘要；风险数量、总体风险、party 和建议由服务端组装，不能当作模型的独立事实源 |
| `quality_status` | 结果质量状态，与任务 `status` 独立；`completed` 不等于 `valid` |

## 解析、分片和模型审查

### 上传和解析

`POST /contract-reviews/:id/document` 只接受安全文件名下的 `.pdf` 或 `.docx`。服务端同时检查非空大小、`MAX_FILE_SIZE_MB`、PDF `%PDF-` 文件头或 DOCX `PK\x03\x04` 文件头，然后保存文件并绑定资源。重新上传只允许在 `draft`、`ready`、`failed`；它会清空旧 clauses/issues、locator、overview、源 hash 和质量状态，并创建新的 `analysis_run_id`。

容器默认通过 `DOCREADER_ADDR` 和 `DOCREADER_TRANSPORT` 构造 `DocumentReader`，传输方式缺省为 `grpc`；未配置地址时客户端处于 disconnected，解析任务会失败。gRPC 优先调用 `ReadStream`，连接到未实现该 RPC 的旧 DocReader 时回退到 unary `Read`；回退路径受 gRPC 消息大小限制，适合兼容旧部署，不改变合同审查的源文本契约。

`ProcessDocument` 只接受当前运行的 `uploading` 任务（或无文本的解析失败任务）。它保存解析器返回的完整 `MarkdownContent` 和 metadata，计算源 hash/revision，并验证 `source_units_json` 中的 ID、范围和可选文本；非法 unit 被丢弃，范围按源文本起止位置排序。解析器没有 units、units 全部非法或使用旧版本时，仍可进入 `ready`，但定位质量为降级并在最终结果中给出 warning。

解析器的 source unit 由 [`docreader/source_units.py`](../docreader/source_units.py) 组装。不同解析器返回的 `kind` 不同，不能用 unit ID 推断法律条款号；表格父行和单元格可以重叠，父行适合作为页/块级回退，单元格适合作为更细粒度范围。

### 分析窗口和上下文

`buildReviewClauses` 使用 chunker 的自动策略，窗口目标为约 `2800` 个 Unicode 字符，重叠 `120` 个字符；窗口 excerpt 最多 `360` 个 Unicode 字符。短文（最多 `12000` 个 Unicode 字符）把当前窗口外的全文作为上下文；长文只发送文档大纲和按中文三元组/ASCII 词段计算的相关 section，相关上下文最多 `3600` 个字符，单段 passage 最多 `900` 个字符，大纲最多 `1400` 个字符。

每个窗口的 primary evidence unit 是完整窗口；supporting unit 只能来自同一合同的相关上下文。模型提示中的 `evidence_id` 由源 hash 和窗口范围派生，例如 `evidence_<sha256>`；它与解析器的 `unit_id` 是两个命名空间。提示里的窗口标题（如 `Analysis segment 1`）只是分析标签，不是正式条款编号。

模型选择顺序如下：

1. 记录指定的可用、当前用户/工作空间可见的 `KnowledgeQA` 模型。
2. 内置 Agent `builtin-contract-review` 的 `ModelID`。
3. 工作空间默认的 active `KnowledgeQA` 模型，找不到时取第一个 active `KnowledgeQA` 模型。

明确指定但已删除、停用、类型不符或无法实例化的模型不会静默换成其他模型；worker 进入 `failed`。没有任何可用模型也会失败。`represented_party` 只有 `customer`、`vendor`、`neutral`，表示审查视角，不表示服务已经识别出合同主体。

工作台使用 [`internal/application/service/contract_review.go`](../internal/application/service/contract_review.go) 中的小型结构化提示词，不直接拼接 Agent 的完整报告提示词。窗口模型必须返回单个 JSON 对象，只允许 `issues`（`facts` 可省略但不能返回非空事实）；每个 issue 必须含 `category`、`finding_type`、`risk_level`、`title`、`explanation`、`original_quote`、`suggestion`、`evidence_refs`。当前 canonical category 包括 `scope`、`parties`、`payment`、`term`、`acceptance`、`liability`、`dispute_resolution`、`guarantee`、`confidentiality`、`intellectual_property`、`data_security`、`compliance`、`other`；finding type 包括 `missing`、`contradiction`、`ambiguity`、`placeholder`、`external_reference`、`inconsistency`。服务接受代码中声明的别名后再归一化存储。

校验边界如下：

- 单窗口最多保留 `5` 个 issue；超出的尾部按模型返回顺序丢弃并记录 `MODEL_ISSUE_LIMIT_EXCEEDED`，不是把整批判为失败。
- `title` 最多 `80` 个 Unicode 字符；`explanation`/`suggestion` 最多 `540`；`original_quote` 最多 `360`；`evidence_refs` 必须有 `1..8` 个不重复 ID。
- JSON 只接受对象、严格字段和无尾随 JSON；Markdown code fence、解释文字和未知字段会校验失败。
- `original_quote` 必须是一个引用 evidence unit 中的连续原文。服务只忽略布局空白差异，仍要求标点、大小写和词语一致；省略号、模糊改写或拼接无关片段都不能成为证据。
- 每个窗口正常调用失败或输出截断后最多再尝试一次；恢复调用的 completion budget 至少为 `8192`。两次之后，只有可隔离的 quote/evidence 错误才会丢弃该 issue、保留其他已验证 issue，并记录 `MODEL_ISSUE_EVIDENCE_UNLOCATED`；风险、schema 等非证据错误仍使整个任务失败。
- 同一窗口内相同类别/发现类型/源范围的重复 issue 被忽略并记录 `MODEL_ISSUE_DUPLICATE`；跨窗口也会按已接受证据去重。模型返回非空 `facts` 是违反协议的 fatal 错误，因为事实归服务端所有。

### 全文事实、警告和概览

所有窗口完成后，服务重新读取完整 `ExtractedContent`，不依赖某个窗口是否覆盖了某行。当前提取器是固定的、基于行和正则/关键词的有限实现，最多 `64` 条事实；支持当事方、日期、期限、金额、付款、保证金、验收、争议解决、占位符和外部引用等类型。日期会产生规范化值（如 `2026-01-02`），事实带有原文 quote、`source_start/end` 和由范围派生的 evidence ID。它不是可配置的完整合同事实模型，未识别到事实不能证明合同中不存在该事实。

事实和结构检查产生的 warning 包括：

- `FACT_CONFLICT`、`DATE_ORDER_CONFLICT`、`PAYMENT_PERCENTAGE_OVER_100`；
- `PLACEHOLDER_OR_BLANK_FIELD`、`EMPTY_REQUIRED_FIELD`、`UNSELECTED_DISPUTE_OPTION`、`EXTERNAL_REFERENCE`；
- 没有有效定位单元时的 `LOCATOR_UNSUPPORTED`，没有识别到有证据当事方时的 `PARTIES_NOT_IDENTIFIED`；
- 模型输出超限、重复或隔离失败的模型 warning。

结构 warning 会被转换成普通 issue，并使用确定性的类别、finding type、风险和建议；因此 issue 数量不只是模型返回数量。概览模型只接收已验证的 issue/fact，并要求 `executive_summary`、`contract_type`、`parties`、`key_recommendations` 四个字段。服务端随后：

- 忽略模型给出的总体风险，按持久化 issue 的 `high`/`medium`/`low` 计数和聚合；有 high 为 high，否则有 medium 为 medium，否则有 low 为 low，否则为 low；
- 用全文 facts 生成 `parties`，不用模型自行识别的 party；
- 用已验证 issue 的 `suggestion` 和确定性 warning 建议生成 `key_recommendations`，避免概览再次产生一套独立建议；
- 将 `risk_counts`、`facts`、`quality_status`、`warnings` 作为兼容 overview JSON 的增量字段。

概览模型调用两次仍无法得到合法 JSON 时，服务会记录 `OVERVIEW_VALIDATION_FAILED`，把已写入的 issue/fact 放进降级 overview，将记录置为 `failed`、进度 `100`、质量 `invalid`，并保留错误信息。此时问题明细可能仍可读，但不能把该记录当作成功审查；应修复模型/协议或执行重试。

## 数据结构和 API

下面路径均省略公共前缀 `/api/v1`。JSON 接口通常返回 `{ "success": true, "data": ... }`；`Create` 返回 `201`，上传、开始、重试和取消返回 `202`，删除返回 `204`，原文预览返回原始二进制和 `Content-Type`。

### 路由

| 方法 | 路径 | 行为和边界 |
| --- | --- | --- |
| `GET` | `/contract-review-playbooks` | 返回当前规则集；目前只有 `general-contract-review` `1.0` |
| `GET` | `/contract-reviews?archived=true` | 只在查询值严格为 `true` 时列出归档记录；默认列出未归档记录 |
| `POST` | `/contract-reviews` | 创建 `draft` 记录 |
| `GET` / `PATCH` / `DELETE` | `/contract-reviews/:id` | 获取、更新或删除当前用户可见记录 |
| `POST` | `/contract-reviews/:id/document` | multipart 字段必须为 `file`；成功后异步解析 |
| `GET` | `/contract-reviews/:id/document/preview` | 返回原始 PDF/DOCX，不返回解析 Markdown |
| `GET` | `/contract-reviews/:id/document/locator` | 返回 locator 信封和 source units，不承诺返回 canonical source text |
| `POST` | `/contract-reviews/:id/start` | 仅 `ready`；排队后返回 `202` |
| `POST` | `/contract-reviews/:id/retry` | 仅 `completed`、`failed`、`cancelled`；清理旧结果后创建新运行 |
| `POST` | `/contract-reviews/:id/cancel` | 仅运行中状态；记录先转终态，再尽力清理队列 |
| `GET` | `/contract-reviews/:id/events` | SSE：初始/变化时 `snapshot`，每 15 秒 `heartbeat` |
| `POST` | `/contract-reviews/bulk/archive` | body `{ "ids": ["..."] }`，逐项结果 |
| `POST` | `/contract-reviews/bulk/restore` | 恢复归档记录，逐项结果 |
| `POST` | `/contract-reviews/bulk/delete` | 逐项删除；输入最多 `500` 个 ID |
| `DELETE` | `/legal-workspace-data` | Owner 专用的租户级永久删除；不受法律工作台开关阻断 |

所有合同审查路由要求当前用户至少 `Viewer`；租户级永久删除要求 `Owner`。记录查询、更新、删除和任务 payload 都同时带 `tenant_id`/`user_id` 作用域，不能只用记录 ID 授权。法律工作台关闭时，除 `/legal-workspace-data` 外的合同审查和 playbook 路由返回 `403`。前端路由守卫在旧后端缺少开关接口时会兼容放行 URL，但后端路由和 handler 仍是最终权限边界。

### 更新和批量契约

`PATCH` 的字段是 `title`、`playbook_id`、`represented_party`、`model_id` 和 `archived`。非空 `title` 会被 trim 并设置 `title_customized`；标题单独更新不要求记录处于可审查状态。`playbook_id`、`represented_party`、`model_id` 只有在 `draft`、`ready`、`completed` 可变更；空 `model_id` 表示恢复自动选模。

在 `completed` 记录上改变 playbook、视角或 model 会清空 `analysis_run_id`/`config_hash`，保留旧明细但标记 `quality_status=stale`，追加 `CONFIG_CHANGED`，并在 overview 同步质量字段；它不会自动重审，调用方必须随后 `retry`。运行中不能归档或恢复。批量服务先拒绝空输入和超过 `500` 个原始 ID，再 trim、去重并逐项执行；返回的 `requested` 是去重后的有效 ID 数，失败项不影响其他项。

### 公共数据结构

`ContractReview` 的关键公开字段是：

| 字段 | 语义 |
| --- | --- |
| `id`、`title`、`title_customized` | 记录身份和显示名称 |
| `status`、`progress`、`error_message` | 任务状态、用户可见进度和最后错误 |
| `playbook_id`、`playbook_version`、`represented_party`、`model_id` | 审查配置；`model_id` 为空表示自动选模 |
| `analysis_run_id` | 当前运行身份；`config_hash` 刻意不出现在 JSON |
| `file_name`、`file_type`、`mime_type`、`file_size`、`metadata` | 原文件和解析元数据；原始资源引用和解析全文不直接返回 |
| `source_revision`、`source_text_hash`、`quality_status`、`warnings` | 源文本身份和质量信号 |
| `locator`、`overview`、`clauses`、`issues` | 定位元数据、概览、分析窗口和问题明细 |
| `archived_at`、`started_at`、`completed_at`、`created_at`、`updated_at` | 生命周期时间 |

overview 保留旧消费者使用的字段，并追加质量字段，典型形状为：

```json
{
  "overall_risk": "high",
  "executive_summary": "……",
  "contract_type": "服务合同",
  "parties": ["甲公司", "乙公司"],
  "key_recommendations": ["……"],
  "risk_counts": {"high": 1, "medium": 2, "low": 3},
  "facts": [],
  "quality_status": "degraded",
  "warnings": []
}
```

`ContractReviewClause` 至少包含 `id`、`sequence`、`title`、`excerpt`、`source_start`、`source_end`、`evidence_id`、`review_status`、`issue_count`。`ContractReviewIssue` 至少包含 `id`、`clause_id`、`sequence`、`risk_level`、`category`、`finding_type`、`title`、`explanation`、`original_quote`、`suggestion`、`evidence_refs`、`source_start`、`source_end`。服务端读时根据 `evidence_refs` 合成只读的 `evidence` 和 `evidence_status`；它们不是独立持久化表。

Issue 的 `source_start/end` 是 `original_quote` 在当前 `ExtractedContent` 中的精确范围；`evidence_refs` 指向窗口模型使用的 evidence ID，通常第一个为 primary、其余为 supporting。解析器 unit 的范围可能覆盖整页、整段或整行，不能直接拿来代替 issue 的 quote 范围。`ContractReviewFact` 具有 `type`、`key`、`value`、`normalized_value`、`unit`、`currency`、`condition`、`status`、`evidence_quote`、`source_start/end`、`evidence_refs`。

### Locator 和前端高亮

当前 locator 信封的核心字段是：

```json
{
  "version": 1,
  "offset_unit": "rune",
  "source_revision": "text-v2:<sha256>",
  "source_text_hash": "<sha256>",
  "source_length": 1234,
  "units": [
    {
      "unit_id": "page-1",
      "kind": "page",
      "page": 1,
      "source_start": 0,
      "source_end": 120,
      "text": "……"
    }
  ]
}
```

当前 Go handler 返回的是 locator 元数据和 units，不返回 `ExtractedContent`；该字段在 `ContractReview` 上标记为 `json:"-"`。前端类型额外接受 `text`/`source_text` 和 `source_units_json` 是为了兼容不同服务器版本，不代表当前 endpoint 一定提供 canonical source text。没有有效 units 时 endpoint 仍可返回 `200` 和空 `units`，前端把 `404`、`405`、`501` 或 `LOCATOR_UNSUPPORTED` 等视为 `unsupported`。

定位分两层：

1. 后端 `normalizeContractReviewForRead` 只根据 source revision、unit 和范围给 API evidence 一个粗粒度状态；后端的 `located` 不等于浏览器已经成功高亮。
2. 前端 [`documentLinking.ts`](../frontend/src/views/legal/contract-review/documentLinking.ts) 在 PDF.js 或 `docx-preview` 渲染文本上重新定位。它先检查 revision 和 source range，再用 unit 内相对偏移、可选 rendered offsets、页范围或全文精确匹配。只忽略布局空白，保留标点、大小写和词语；重复候选返回 `multiple_matches`，不取第一个。

前端 resolver 当前会产生 `pending`、`located`、`multiple_matches`、`not_found`、`version_mismatch`、`unsupported`、`error`；类型中的 `legacy_exact` 只保留作兼容值，当前 resolver 没有把纯文本 legacy 匹配提升为该状态。只有状态为 `located` 且恰好一个候选时，viewer 才创建高亮；无法映射时才接受目标页或全文的精确候选，重复候选仍不高亮。PDF 的渲染文本是 PDF.js text layer，DOCX 是 `docx-preview` DOM；服务器的 `rune` 源偏移和浏览器内部的 normalized text 偏移不是同一存储单位，重构时不能混用。

## 状态、任务和失败行为

### 状态和进度

合法状态值为：

`draft` → `uploading` → `ready` → `analyzing` → `reviewing_clauses` → `completed` / `failed` / `cancelled`

| 状态 | 进入条件和含义 |
| --- | --- |
| `draft` | 创建后、尚未成功上传文件 |
| `uploading` | 文件已保存，等待或正在解析；上传/解析进度初始化为 `5` |
| `ready` | 有非空解析文本和 locator 信封，可启动审查；进度 `15` |
| `analyzing` | 已创建审查运行并排队分析任务；进度 `20` |
| `reviewing_clauses` | clauses 已写入，逐窗口调用模型；初始化进度 `25` |
| `completed` | 所有窗口、全文校验和概览流程结束；进度 `100`，质量可能是 `valid` 或 `degraded` |
| `failed` | 解析、模型、队列、结构化校验或概览不可恢复失败；原因在 `error_message`，可重试 |
| `cancelled` | 用户取消了运行；保留已生成的中间结果，质量为 `invalid`，可重试 |

窗口阶段进度为 `25 + floor(已完成窗口数/总窗口数*60)`，所以最后一个窗口完成后通常是 `85`，概览成功后才到 `100`。失败通常保留当时进度；概览失败的显式 fallback 会写 `100`。前端把 `issues == undefined` 视为“尚未加载”，把 `issues == []` 视为“已加载且为空”，不能用空数组表示流式结果尚未到达。

### 操作、副作用和任务参数

| 操作 | 允许状态 | 关键副作用 |
| --- | --- | --- |
| 上传 | `draft`、`ready`、`failed` | 清理旧 clauses/issues 和源定位，写新资源，排队解析；排队失败转 `failed` |
| 开始 | `ready` | 创建新的 `analysis_run_id`、`config_hash`，转 `analyzing`，排队分析 |
| 重试 | `completed`、`failed`、`cancelled` | 事务清理旧结果并创建新运行；没有解析文本时先重新解析，否则直接分析 |
| 取消 | `uploading`、`analyzing`、`reviewing_clauses` | 先条件更新为 `cancelled`，设置 `completed_at` 和错误信息，再尽力清队列 |
| 归档/恢复 | 非运行中 | 只改 `archived_at`，不等于删除；运行中归档被拒绝 |
| 用户删除 | 任意可见记录 | 子表硬删除，主记录 GORM soft delete，源文件删除尽力而为；没有用户级恢复接口 |
| 租户永久删除 | Owner、`/legal-workspace-data` | 事务硬删除租户全部合同记录、子项、资源绑定；提交后清理物理源文件，共享资源仍保留 |

队列配置是：

| 任务类型 | Asynq 队列 | `MaxRetry` | `Timeout` |
| --- | --- | ---: | ---: |
| `contract_review:document_process` | `chat_attachment` | `2` | `10m` |
| `contract_review:analyze` | `summary` | `1` | `30m` |

两类任务 payload 都包含 `tenant_id`、`user_id`、`review_id`、`analysis_run_id`，分析任务还必须带非空 `config_hash`。`ProcessReview` 要求 run ID 和 config hash 都与记录完全相等；`ProcessDocument` 至少要求当前 run ID。repository 的 `UpdateForRun`、`ReplaceClausesForRun`、`UpsertIssueForRun` 还要求记录处于运行中状态，因此取消、重试或删除赢得竞争后，旧 worker 的迟到写入会被拒绝。`fingerprint` 的唯一约束和 `ON CONFLICT DO NOTHING` 进一步保证重复投递不会重复插入同一 issue。

Redis/Asynq 模式中，取消只扫描合同审查的两种 live task（pending、scheduled、retry、active），删除待执行任务并向 active worker 发停止信号；这些操作是 best effort，归档任务不视为 live。Lite 模式的 `SyncTaskExecutor` 以内联 goroutine 执行，合同审查 inspector 是 no-op，无法出队或撤销已经启动的 goroutine；状态条件和旧运行隔离仍是正确性保障。

### 超时、巡检和质量状态

分析 worker 单次超时为 `30m`，最多自动重试一次。超时上下文可能已被 Asynq 取消，`fail` 使用独立的最多 `5s` 上下文持久化 `failed`；条件更新不会覆盖已经成功取消的记录。若进程在写失败状态前消失，housekeeping 默认启用，启动时立即执行并每 `5` 分钟执行一次。它把 `uploading`/`analyzing`/`reviewing_clauses` 中超过 `30m * (1 + 1) + 10m = 70m` 的记录作为候选；Redis 队列探测到对应 live task 时跳过，探测报错时延后，避免短暂 Redis 故障误杀正常任务。可通过 `WEKNORA_HOUSEKEEPING_ENABLED=false` 显式关闭该安全网。

质量状态的含义是：

| `quality_status` | 当前产生条件 |
| --- | --- |
| `pending` | 新记录、解析完成待审查或审查进行中 |
| `valid` | 任务 `completed` 且没有 warning |
| `degraded` | 任务 `completed` 但存在模型/结构/事实/定位 warning |
| `invalid` | `failed`、`cancelled` 或概览 fallback 失败 |
| `stale` | 已完成记录改变审查配置，旧结果等待重审 |
| `legacy` | 旧迁移创建的已结束记录没有新质量/来源字段；读时兼容归一化 |

SSE handler 每秒检查记录快照，只在 hash 变化时发送 `event: snapshot`，首次连接立即发送一次；每 `15s` 发送 `event: heartbeat`。前端 store 同时立即 GET 并每 `1.5s` 轮询，以覆盖代理缓冲、断线和 malformed snapshot。查询接口是最终状态来源，SSE 不是持久化日志。

## 边界和兼容性

- `MAX_FILE_SIZE_MB` 是部署级环境变量，默认 `500` MB；前端/nginx、App 和 DocReader 共享该上限，改动需要在启动时让这些层一致生效，不能当作单独的运行时 workspace setting。
- 当前工作台只支持 PDF/DOCX 单文件。没有红线编辑、版本对比、导出、自定义 playbook 或用户自定义事实 schema。扫描 PDF、表格和复杂 DOCX 的解析文本顺序以及 source units 由实际 parser engine 决定。
- 旧 DocReader 没有 `ReadStream` 时可用 unary 回退；旧 parser 没有 `source_units_json` 时可以审查，但会得到 `LOCATOR_UNSUPPORTED`，旧结果不能仅凭 quote 文本安全高亮。
- PostgreSQL 的 `000103_contract_review_quality` 增加运行、来源、locator、质量、warning 和证据字段；`000104_contract_review_quality_source_fields` 是对已发布迁移编号的幂等修复，不应通过其 down migration 删除属于 `000103` 的字段。SQLite 的 `000019_contract_review_quality` 对应质量字段，`000016_legal_workspace_config` 是开关迁移；合同审查的 SQLite 迁移从 `000017` 开始，以避开上游已发布的 `000013_mcp_tool_enabled`。现有旧记录的迁移默认 `quality_status=legacy`。
- 当前 [`docs/swagger.json`](swagger.json) 和 [`docs/swagger.yaml`](swagger.yaml) 没有合同审查 paths；本页的路由表以及 route/handler/types 代码才是当前接口事实源，不能以生成 Swagger 推断合同审查 API。
- 关闭法律工作台不会隐式删除数据；只有 Owner 明确调用 `/legal-workspace-data` 才会租户级永久删除。物理文件清理发生在数据库事务提交后，存储服务失败可能需要再次处理。

## 重构不变量和验证

重构必须保持以下不变量：

1. **运行隔离。** 新上传、开始、重试都生成新的 `analysis_run_id`；分析任务还绑定当前 `config_hash`。任何 worker 写主记录、clause 或 issue 前都必须校验租户、用户、记录、run ID 和运行中状态。
2. **来源一致。** `ExtractedContent`、`source_text_hash`、`source_revision`、locator units、clause 范围、issue quote/range 和 fact evidence 必须描述同一个精确解析文本。不得 trim 文本、把 rune 偏移当字节偏移，或用业务版本号替代 source revision。
3. **证据可核验。** 每个模型 issue/fact 的 quote 必须是引用 unit 中可在源文本验证的连续片段；重复候选必须显式返回 `multiple_matches`，不得取首个、补省略号或扩大高亮范围。没有可靠 locator 时应显示 unsupported/degraded，而不是伪造 located。
4. **服务端拥有聚合结果。** 风险聚合、facts、parties、recommendations 和结构 warnings 由服务端已验证数据生成；模型 overview 只能摘要这些输入，不能重新发明风险、事实或建议。
5. **状态和质量分离。** `completed` 只表示流程结束；消费者必须同时检查 `quality_status`、`warnings`、issue/fact 是否已加载和每个 evidence 状态。配置变化后的 completed 记录必须成为 `stale`，不能继续被当成当前配置结果。
6. **删除和权限边界。** 普通 API 始终按 tenant/user 隔离；用户删除不提供恢复接口；关闭 workspace 不删除数据；Owner purge 需先完成数据库级联清理，再按共享资源计数清理物理文件。
7. **兼容回退不改变正确性。** Asynq/Lite、ReadStream/unary、旧质量字段和无 locator 版本可以降级吞吐或定位覆盖率，但不能放宽 stale worker、证据精确性或权限校验。

按改动范围执行最小验证：

| 改动 | 重点测试 |
| --- | --- |
| 状态、任务、取消或重试 | `internal/application/service/contract_review_test.go`、`internal/application/service/contract_review_cancel_test.go`、`internal/router/task_inspector_contract_review_test.go`；检查状态转换、旧 run、超时持久化、巡检和队列匹配 |
| 证据、事实或质量 | `internal/application/service/contract_review_quality_test.go`、`frontend/src/views/legal/contract-review/documentLinking.test.ts`、`docreader/tests/test_source_units.py`；检查重复 quote、Unicode/rune 范围、source unit、模型降级和事实 evidence |
| API、权限或删除 | `internal/handler/contract_review_test.go`、`internal/router/routes_contract_review_test.go`、`internal/application/repository/contract_review_test.go`；检查 wrapper、403/404、tenant/user 隔离、子项清理和 purge |
| schema/迁移 | `internal/database/migration_sqlite_versioned_schema_test.go`；检查 SQLite 版本 `16`、合同表及质量/证据字段 |

推荐命令：

```bash
go test ./internal/application/service ./internal/handler ./internal/router
go test ./internal/database -run TestSQLiteMigrationsCreateVersionedSchema
cd frontend
npm test -- src/views/legal/contract-review/documentLinking.test.ts
npm run type-check
npm run check-i18n
```

不必机械执行全部命令：按后端、迁移、前端和国际化改动范围选择。完成任何文档或实现改动前都运行 `git diff --check`，并确认本文中的路径、状态值、JSON 字段、迁移编号和示例仍与事实源一致。
