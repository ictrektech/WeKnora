# 合同审查

合同审查工作台支持上传单个 PDF 或 DOCX，解析合同文本后按分析片段查看风险、原文证据和修改建议，并通过 SSE 接收异步审查进度。

本文面向功能使用者、前端/后端集成者和部署维护者，说明当前工作台的概念、状态、接口和结果边界。对话式 Agent 的合同审查方法见 [`skills/preloaded/contract-review/`](../skills/preloaded/contract-review/SKILL.md)，它与工作台的异步审查流程不是同一个入口。

## 快速结论

- 工作台可以发现合同文本中的风险、缺失、矛盾、歧义和外部引用，并给出原文证据与修改建议。
- “分析片段”是服务按文本切分出的审查窗口，不一定等同于合同中的正式条款号。
- 结果是否适合定位到文档，应该结合证据状态和 `quality_status` 判断；`completed` 不代表所有结果都具备可靠的可视化定位。
- 结果属于 AI 辅助分析，不替代律师意见、正式法律审查或业务审批。

## 当前状态

| 能力 | 状态 |
| --- | --- |
| 合同审查记录、归档、批量操作 | 已实现 |
| PDF/DOCX 上传与原文预览 | 已实现 |
| 文档解析、分析片段切分、异步审查 | 已实现 |
| 风险等级、原文引用、修改建议 | 已实现 |
| 为单条审查指定知识问答模型 | 已实现 |
| 结构化事实提取、质量状态和警告 | 已实现 |
| 原文证据定位和多候选位置选择 | 已实现 |

## 入口、范围与边界

入口为 `/platform/contract-review`。该路由复用 `/platform/creatChat` 的平台壳层（菜单、设置、命令面板和主题），只替换中间内容区域；平台默认首页和现有知识库路由保持不变。

当前工作台的范围如下：

- 一次上传一个 PDF 或 DOCX 文件，文件大小受 `MAX_FILE_SIZE_MB` 限制。
- 当前内置规则集只有 `general-contract-review`，版本为 `1.0`，覆盖商业、法律、运营和文本起草风险。
- 当前不提供自定义 playbook、红线版合同直接编辑、旧新版本对比或审查结果导出。
- 工作台使用专用的异步结构化审查流程。它不会直接执行内置 Agent 的完整报告提示词、工具调用或联网搜索流程，因此不应承诺每条问题都带有知识库、网页或法律案例引用。
- `skills/preloaded/contract-review/` 是对话式 Agent 的审查方法和参考清单；工作台专用的结构化审查提示词位于 `internal/application/service/contract_review.go`。

## 使用前提与模型选择

1. WeKnora 已配置至少一个可用且已启用的知识问答聊天模型（`KnowledgeQA`）。
2. 用户需要具备当前工作空间的 Viewer 权限。
3. 合同文件必须是 PDF 或 DOCX，且文件不能为空、格式签名有效。
4. 审查视角默认为 `neutral`。创建或重新审查前可以选择代表方。

内置 Agent ID 为 `builtin-contract-review`。模型选择规则如下：

1. 如果审查记录指定了 `model_id`，使用该模型；模型必须仍然可用且类型为 `KnowledgeQA`。
2. 未指定 `model_id` 时，先尝试使用内置 Agent 的模型配置。
3. 内置 Agent 没有模型配置时，选择当前空间已启用的默认知识问答模型，或第一个可用模型。
4. 已配置但不可用的模型会导致审查失败，不会静默替换成另一个模型。

代表方取值：

| 值 | 含义 |
| --- | --- |
| `customer` | 以客户/甲方利益为审查视角 |
| `vendor` | 以供应商/乙方利益为审查视角 |
| `neutral` | 不偏向任一方 |

代表方是审查视角，不是系统从合同中自动识别出的实际合同主体。

## 核心概念

| 概念 | 说明 |
| --- | --- |
| `playbook` | 审查规则集。目前只有 `general-contract-review`，不支持在工作台中自定义规则集。 |
| 分析片段 | 文档解析后由服务按文本切分出的审查窗口。当前使用自动切分策略，单个窗口约 2800 个 Unicode 字符并保留重叠上下文；标题可能是自动生成的“Analysis segment N”。它不一定等同于正式合同条款。 |
| 问题（issue） | 针对某个分析片段提出的风险或文本问题，包含风险等级、类别、发现类型、解释、原文引用和修改建议。 |
| `category` | 问题主题，例如 `payment`、`acceptance`、`liability`、`dispute_resolution`、`data_security`。 |
| `finding_type` | 问题性质，例如 `missing`（缺失）、`contradiction`（矛盾）、`ambiguity`（歧义）、`placeholder`（占位符）、`external_reference`（外部引用）和 `inconsistency`（不一致）。 |
| 事实（fact） | 从全文提取的可核验信息，例如主体、日期、期限、金额、付款、验收和争议信息。事实是结果数据，不等同于风险问题。 |
| 警告（warning） | 对占位符、空白必填项、未勾选争议选项、外部引用、事实冲突、日期顺序冲突等问题的质量或结构检查结果。警告可能使整体质量降级，但不一定单独形成风险问题。 |
| 证据（evidence） | 问题所依据的合同原文片段。`original_quote` 必须能够在当前源文本中精确找到。 |
| `source_revision` | 当前解析文本的身份标识，例如 `text-v2:<sha256>`；它不是合同业务上的版本号，用于判断结果与当前文档是否匹配。 |
| locator | 将源文本范围、证据单元和可选页码映射到原始 PDF/DOCX 预览的位置数据。文本偏移量使用 `rune` 单位。 |
| `quality_status` | 结果质量状态：`pending`、`valid`、`degraded`、`invalid`、`stale`、`legacy`。它与任务状态 `completed` 独立。 |

### 风险等级

- `high`：可能造成重大法律、财务、效力或履约风险。
- `medium`：具有实际影响，但通常可以通过谈判或补充措辞修复。
- `low`：轻微起草或整理问题，通常不阻碍签署。

总体风险由服务端根据已验证的问题聚合：存在任一高风险时为高风险，否则存在任一中风险时为中风险，否则为低风险。建议主要来源于已验证问题和确定性结构检查，不是独立于证据之外生成的结论。详细方法见 [`risk-levels.md`](../skills/preloaded/contract-review/references/risk-levels.md)。

## 生命周期与操作条件

典型状态流转为：

`draft → uploading → ready → analyzing → reviewing_clauses → completed / failed`

| 操作 | 允许的状态或行为 |
| --- | --- |
| 上传合同 | `draft`、`ready`、`failed`；上传会重新解析并清理旧的条款、问题和定位数据。 |
| 开始审查 | 仅 `ready`；接口返回 `202`，任务异步执行。 |
| 重试/重新审查 | `completed` 或 `failed`；会生成新的审查运行并替换旧结果。 |
| 修改审查配置 | `draft`、`ready`、`completed`；已完成记录修改配置后变为 `stale`，需要重新审查。 |
| 归档 | 运行中的记录不能归档；归档不等于删除。 |
| 删除 | 清理条款、问题和文件资源；当前没有用户级恢复已删除记录的接口。批量 `restore` 仅恢复已归档记录。 |

上传阶段先解析文件，成功后进入 `ready`；审查阶段先分析各分析片段，再提取全文事实并生成概览。任何不可恢复的模型、解析或校验错误都会进入 `failed`，并在 `error_message` 中保留错误信息。

SSE 只在审查记录快照发生变化时发送 `snapshot` 事件，并每 15 秒发送 `heartbeat`。客户端断线后应重新连接，并以记录查询接口返回的数据作为最终状态来源。

## 结果如何判读

1. 先查看任务状态，再查看 `quality_status` 和 `warnings`。
2. 每个问题应优先核对 `original_quote`、`evidence_refs` 和证据状态。
3. 证据状态为 `located` 且源版本一致、匹配唯一时，前端才会自动高亮原文。
4. 同一原文出现多次时，状态会提示多个候选位置，用户需要选择具体位置；`not_found`、`version_mismatch`、`unsupported` 或 `error` 时仍可阅读问题文本，但不能把它当作已完成的可视化定位。
5. 如果模型问题无法通过原文证据校验，服务可能舍弃该问题并增加警告；如果整体结构化输出或模型调用失败，本次审查会进入 `failed`。
6. 单个分析片段的模型输出最多包含五个问题。这是单次模型输出上限，不是最终记录的问题总数上限；确定性结构检查还可能追加问题。

事实和警告用于补充结果可信度。`degraded` 表示结果存在警告，`invalid` 表示本次结果不能作为可靠结果使用，`stale` 表示审查配置或来源已变化，`legacy` 表示历史结果缺少当前定位所需的信息。

## API

API 前缀为 `/api/v1`，所有记录按当前用户和工作空间隔离。JSON 接口通常使用 `{ "success": true, "data": ... }` 包装；错误响应包含 `error.code`、`error.message` 和可选的 `error.details`。

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| `GET` | `/contract-review-playbooks` | 列出审查规则集 |
| `GET` / `POST` | `/contract-reviews` | 列出或创建审查记录；列表可用 `archived=true` 查看归档记录 |
| `GET` / `PATCH` / `DELETE` | `/contract-reviews/:id` | 查看、更新或删除记录 |
| `POST` | `/contract-reviews/:id/document` | 以 multipart 字段 `file` 上传合同 |
| `GET` | `/contract-reviews/:id/document/preview` | 预览原始文件 |
| `GET` | `/contract-reviews/:id/document/locator` | 获取源文本、证据单元和文档定位数据 |
| `POST` | `/contract-reviews/:id/start` | 开始审查，返回 `202` |
| `POST` | `/contract-reviews/:id/retry` | 重试或重新审查，返回 `202` |
| `GET` | `/contract-reviews/:id/events` | 订阅 SSE `snapshot` 和 `heartbeat` 事件 |
| `POST` | `/contract-reviews/bulk/:action` | 批量 `archive`、`restore` 或 `delete` |

`PATCH /contract-reviews/:id` 支持 `title`、`playbook_id`、`represented_party`、`model_id` 和 `archived`。将 `model_id` 设置为空字符串表示恢复自动选择。批量接口请求体为 `{ "ids": ["..."] }`，最多接收 500 个 ID，并返回逐项成功或失败结果。

> 当前合同审查接口尚未收录到生成的 `docs/swagger.yaml` 和 `docs/swagger.json`；本页接口表是补充说明。若要作为稳定的外部集成接口使用，还需要同步补充 OpenAPI schema。

## 实现约束与数据迁移

- 审查模型必须返回结构化 JSON。对截断、无效 JSON 或证据校验失败的输出，服务最多自动重试一次。
- 每个问题的 `original_quote` 必须来自合同原文；服务不会使用模糊匹配或任意首个匹配位置替代精确证据。
- 条款问题由模型按分析片段生成，全文事实由服务单独提取；条款问题响应不直接承载事实列表。
- 当前内置规则集只有 `general-contract-review`，版本为 `1.0`。
- PostgreSQL 迁移为 `000101_contract_reviews`、`000102_contract_review_model`、`000103_contract_review_quality`、`000104_contract_review_quality_source_fields`；SQLite 迁移为 `000013_contract_reviews`、`000014_contract_review_model`、`000015_contract_review_quality`。

## 取消与卡住任务（已实现）

运行中的合同审查可通过 `POST /api/v1/contract-reviews/:id/cancel` 取消。接口先将当前运行置为 `cancelled`，再尽力停止对应的队列任务；`cancelled` 与 `failed` 都允许重试。

模型任务单次超时为 `30m`，最多自动重试一次。超时 handler 会使用独立的短事务上下文写入 `failed`；若 worker 在写入前退出，后台巡检每 5 分钟运行一次，在两次任务时限加 `10m` 缓冲（当前为 `70m`）后，只有在没有对应活动队列任务时才将记录收敛为 `failed`。队列探测失败会延后回收，避免 Redis 短暂故障误杀正常任务。
