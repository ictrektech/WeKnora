# 合同审查

本文首先面向刚接手或正在维护合同审查功能的开发者，帮助其确认当前行为、代码入口、不可破坏的约束和验证方式。Coding agent 可将本文作为功能级开发约束。

代码、配置、测试和迁移是事实源；本文在接口、状态、数据结构、权限边界或验证方式变化时同步更新。文中的状态描述当前仓库，不代表目标环境已经发布或验证。

## 快速视图

| 项目 | 当前结论 |
| --- | --- |
| 入口 | `/platform/contract-review`；API 前缀为 `/api/v1` |
| 已支持 | 单个 PDF/DOCX 上传、异步审查、风险与建议、事实和警告、原文定位、归档和批量操作 |
| 未支持 | 自定义 playbook、红线版直接编辑、版本对比、结果导出 |
| 结果边界 | AI 辅助分析，不替代律师意见、业务审批或正式法律审查 |
| 接手入口 | 先看下方代码导航和关键不变量，再按修改类型运行定向测试 |

## 当前状态与范围

| 能力 | 状态 | 说明 |
| --- | --- | --- |
| 合同记录、上传、解析和原文预览 | `已实现` | 每条记录一次上传一个 PDF 或 DOCX |
| 异步审查、风险、修改建议和结构化事实 | `已实现` | 使用专用结构化审查流程 |
| 质量状态、警告和原文定位 | `已实现` | 定位可信度需结合 `quality_status` 判断 |
| 指定知识问答模型 | `已实现` | 模型不可用时审查失败，不静默切换 |
| 取消审查与卡住任务回收 | `已实现` | 用户可取消运行中的任务；超时、进程丢失或队列记录消失时由后台巡检收敛为 `failed` |

工作台与 `skills/preloaded/contract-review/` 是两个入口：前者执行异步结构化审查；后者是对话式 Agent 的方法和参考清单。工作台不会直接执行 Agent 的完整报告提示词、工具调用或联网搜索，因此不承诺每个问题都带知识库、网页或案例引用。

工作台当前只有 `general-contract-review` `1.0` 规则集。文件大小受 `MAX_FILE_SIZE_MB` 限制；用户需要当前工作空间 Viewer 权限，且法律工作台处于启用状态。开关、数据保留和永久删除边界见 [法律工作台](../ictrek.app/docs/legal-workspace.md)。

## 主流程与代码导航

`创建记录 → 上传并解析 → ready → 启动异步任务 → 分片审查 → 全文事实与质量校验 → completed / failed / cancelled`

运行中的记录也可以进入 `cancelled`。取消先持久化记录状态，再尽力删除待执行、重试和活动队列任务；旧 worker 即使晚返回，也不能再写入已取消运行的结果。`cancelled` 与 `failed` 都允许重试。

| 关注点 | 主要入口 | 维护时重点 |
| --- | --- | --- |
| 路由、权限和 HTTP 映射 | `internal/router/routes_contract_review.go`、`internal/handler/contract_review.go` | Viewer/Owner 权限、错误码、SSE 和访问开关 |
| 上传、状态流转和任务编排 | `internal/application/service/contract_review.go` | 状态转换、模型选择、任务过期保护和资源清理 |
| 事实、证据和质量校验 | `internal/application/service/contract_review_quality.go` | 原文精确匹配、定位、警告和降级行为 |
| 持久化和领域结构 | `internal/application/repository/contract_review.go`、`internal/types/contract_review.go` | 租户/用户隔离、兼容字段和事务边界 |
| 前端调用和结果展示 | `frontend/src/api/contract-review.ts`、`frontend/src/views/legal/contract-review/` | 状态刷新、SSE、原文定位和失败提示 |
| Agent 审查方法 | `skills/preloaded/contract-review/SKILL.md` | 只影响对话式 Agent，不等同于工作台提示词 |

## 关键概念与不变量

| 概念 | 维护约束 |
| --- | --- |
| 分析片段 | 服务按文本自动切出的审查窗口，约 2800 个 Unicode 字符并保留重叠上下文；不保证等同正式条款号 |
| 问题（issue） | 必须包含风险、解释、原文引用和修改建议；可表示缺失、矛盾、歧义、占位符或外部引用等发现 |
| 事实与警告 | 事实是全文可核验信息；警告表示结构、冲突或质量问题，不一定单独形成风险问题 |
| `quality_status` | `pending`、`valid`、`degraded`、`invalid`、`stale`、`legacy`；与任务是否 `completed` 相互独立 |
| `source_revision` | 当前解析文本的身份标识，不是合同业务版本号；结果和定位必须与其一致 |
| locator | 将文本范围、证据单元和可选页码映射到预览位置；状态包括 `located`、`multiple_matches`、`not_found`、`version_mismatch`、`unsupported` 等，偏移量使用 `rune` 单位 |

以下行为是跨后端、前端和数据层的不变量：

- 所有记录按当前工作空间和用户隔离；租户级永久删除必须由 Owner 显式触发，关闭工作台不能隐式删除数据。
- `analysis_run_id` 和 `config_hash` 用于阻止旧任务覆盖新运行或新配置的结果。
- `original_quote` 必须能在当前源文本中精确找到；服务不能用模糊匹配或省略号匹配伪造证据。
- `original_quote` 的精确范围以问题的 `source_start`/`source_end` 为准；`evidence_refs` 对应的单元范围可能是整段或整页，不能直接当作引用范围。
- 原文定位先校验来源版本和 source unit，再用证据在 unit 内的相对偏移映射到预览文本；只有唯一且文本完全一致时才标记为 `located`。
- 同一原文仍有多个候选时返回 `multiple_matches`，不自动取第一个、不创建可靠高亮；若 locator 无法对齐，则仅在目标页或全文存在唯一精确匹配时降级定位。
- `completed` 只表示任务完成；结果能否可靠使用还要检查 `quality_status`、`warnings` 和证据状态。

原文定位以正确性优先于覆盖率。PDF.js 和 `docx-preview` 的渲染文本可能与服务端解析文本存在顺序或格式差异；对齐失败时必须安全降级，不能用首个匹配位置冒充证据位置。
### 风险等级

风险等级为 `high`、`medium`、`low`。总体风险按已验证问题聚合：存在高风险即为高风险，否则依次检查中、低风险。详细判定参考 [`risk-levels.md`](../skills/preloaded/contract-review/references/risk-levels.md)。

## 状态、操作与失败行为

状态流转为：

`draft → uploading → ready → analyzing → reviewing_clauses → completed / failed / cancelled`

| 操作 | 允许条件与结果 |
| --- | --- |
| 上传合同 | `draft`、`ready`、`failed`；重新解析并清理旧条款、问题和定位数据 |
| 开始审查 | 仅 `ready`；返回 `202` 后异步执行 |
| 取消审查 | `uploading`、`analyzing` 或 `reviewing_clauses`；先将记录置为 `cancelled`，再尽力停止队列任务；已生成的中间结果保留 |
| 重试/重新审查 | `completed`、`failed` 或 `cancelled`；创建新运行并替换旧结果 |
| 修改审查配置 | `draft`、`ready`、`completed`；已完成记录变为 `stale` |
| 归档 | 运行中不能归档；归档不等于删除 |
| 删除 | 清理记录、子项和文件资源；没有用户级恢复接口 |

模型、解析或结构化校验发生不可恢复错误时进入 `failed`，原因写入 `error_message`。

分析任务的单次 worker 超时为 `30m`，最多自动重试一次。超时 handler 会使用独立的短事务上下文写入 `failed`，避免 Asynq 的取消上下文阻止失败状态落库。若 worker 在写入失败状态前进程消失，后台巡检每 5 分钟检查一次；超过两次任务时限加 `10m` 缓冲（当前为 `70m`）且没有对应的活动队列任务时，记录会被标记为 `failed`，用户可以重试。队列探测失败时会延后回收，避免 Redis 短暂故障误杀正常任务。

### 卡住任务处理（已实现）

调整背景：任务可能在模型调用超时、worker 进程退出或队列记录被归档后仍保留 `analyzing`/`reviewing_clauses`，导致前端持续显示“审查中”。目标是让用户能主动停止任务，并让异常任务最终进入可解释、可重试的终态。

采用“记录状态优先、队列操作尽力而为”的方案。取消接口先将当前 `analysis_run_id` 置为 `cancelled`，再按记录和运行 ID 清理队列；repository 的运行条件会拒绝旧 worker 的迟到写入。后台巡检只回收超过任务时限且没有对应活动任务的记录；队列探测失败时延后处理。这样兼容 Redis/Asynq 和 Lite 模式，也避免把短暂的队列故障误判为审查失败。

SSE 仅在记录快照变化时发送 `snapshot`，并每 15 秒发送 `heartbeat`。断线后应重新连接；记录查询接口是最终状态来源。

模型选择顺序为：记录指定的可用 `KnowledgeQA` 模型、内置 Agent `builtin-contract-review` 的模型配置、工作空间默认或首个可用模型。已明确指定但不可用的模型会使审查失败。`represented_party` 取值为 `customer`、`vendor`、`neutral`，表示审查视角而非自动识别出的合同主体。

## API

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| `GET` | `/contract-review-playbooks` | 列出审查规则集 |
| `GET` / `POST` | `/contract-reviews` | 列出或创建记录；`archived=true` 查询归档记录 |
| `GET` / `PATCH` / `DELETE` | `/contract-reviews/:id` | 查看、更新或删除记录 |
| `POST` | `/contract-reviews/:id/document` | 通过 multipart 字段 `file` 上传合同 |
| `GET` | `/contract-reviews/:id/document/preview` | 预览原始文件 |
| `GET` | `/contract-reviews/:id/document/locator` | 获取源文本、证据单元和定位数据 |
| `POST` | `/contract-reviews/:id/start` | 开始审查，返回 `202` |
| `POST` | `/contract-reviews/:id/retry` | 重试或重新审查，返回 `202` |
| `POST` | `/contract-reviews/:id/cancel` | 取消运行中的审查，返回 `202` |
| `GET` | `/contract-reviews/:id/events` | 订阅 SSE `snapshot` 和 `heartbeat` |
| `POST` | `/contract-reviews/bulk/archive` | 批量归档 |
| `POST` | `/contract-reviews/bulk/restore` | 批量恢复归档 |
| `POST` | `/contract-reviews/bulk/delete` | 批量删除 |

`PATCH` 支持 `title`、`playbook_id`、`represented_party`、`model_id` 和 `archived`；空 `model_id` 恢复自动选择。批量请求为 `{ "ids": ["..."] }`，最多 500 个 ID，并返回逐项成功或失败结果。法律工作台关闭后，上述路由返回 `403`；Owner 使用的 `/legal-workspace-data` 删除接口不受开关限制。

PostgreSQL 迁移为 `000101_contract_reviews`、`000102_contract_review_model`、`000103_contract_review_quality`、`000104_contract_review_quality_source_fields`；SQLite 迁移为 `000013_contract_reviews`、`000014_contract_review_model`、`000015_contract_review_quality`。法律工作台开关的迁移由关联文档单独维护。

## 修改影响与验证

| 修改类型 | 至少检查 |
| --- | --- |
| 状态、重试或异步任务 | service、types、前端状态处理；验证状态转换、旧任务隔离、SSE 与查询一致性 |
| 证据、事实或原文定位 | quality service、handler、预览组件；验证重复原文、版本不匹配和定位失败 |
| API 字段或数据结构 | types、handler、repository、迁移、前端 API；验证兼容性和序列化 |
| 权限、开关或数据删除 | router、handler、service、资源绑定；验证权限、租户隔离、共享文件和重复删除 |

修改前先阅读根目录 `AGENTS.md`、`AGENTS.override.md`，并检查 `git status` 和相关 diff。只修改任务直接涉及的文件；不要在本页复制 Agent 方法或 VOS 部署说明。

按改动范围执行最小验证：

```bash
go test ./internal/application/service ./internal/handler ./internal/router
go test ./internal/database -run TestSQLiteMigrationsCreateVersionedSchema
cd frontend
npm test -- src/views/legal/contract-review/documentLinking.test.ts
npm run type-check
npm run check-i18n
```

无需机械执行全部命令：后端、迁移、前端和国际化检查只在对应范围受影响时运行。完成时执行 `git diff --check`，并说明改了什么、验证了什么、哪些内容仍待验证。
