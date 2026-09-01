# 合同审查

当前已实现合同审查工作台：用户可以上传 PDF 或 DOCX，按条款查看风险、原文证据和修改建议，并通过 SSE 接收审查进度。

## 当前状态

| 能力 | 状态 |
| --- | --- |
| 合同审查记录、归档、批量操作 | 已实现 |
| PDF/DOCX 上传与原文预览 | 已实现 |
| 文档解析、条款切分、异步逐条审查 | 已实现 |
| 风险等级、原文引用、修改建议 | 已实现 |
| 合同起草、智能归档、法律导航 | 未实现 |

入口为 `/platform/contract-review`。该路由复用 `/platform/creatChat` 的平台壳层（菜单、设置、命令面板和主题），只替换中间内容区域；平台默认首页和现有知识库路由保持不变。

## 使用前提

1. WeKnora 已配置至少一个可用的知识问答聊天模型。
2. 用户需要具备当前工作空间的 Viewer 权限。
3. 合同文件必须是 PDF 或 DOCX，大小受 `MAX_FILE_SIZE_MB` 限制。
4. 审查结果是 AI 辅助分析，不替代律师意见或正式法律审查。

内置 Agent ID 为 `builtin-contract-review`，默认使用 General Contract Review playbook。若该 Agent 没有指定模型，服务会回退到已启用的默认知识问答模型。

## API

API 前缀为 `/api/v1`，所有记录按当前用户和工作空间隔离。

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| `GET` | `/contract-review-playbooks` | 列出审查规则 |
| `GET` / `POST` | `/contract-reviews` | 列出或创建审查记录 |
| `GET` / `PATCH` / `DELETE` | `/contract-reviews/:id` | 查看、更新或删除记录 |
| `POST` | `/contract-reviews/:id/document` | 上传合同 |
| `GET` | `/contract-reviews/:id/document/preview` | 预览原始文件 |
| `POST` | `/contract-reviews/:id/start` | 开始审查 |
| `POST` | `/contract-reviews/:id/retry` | 重试失败或重新审查 |
| `GET` | `/contract-reviews/:id/events` | 订阅 SSE 快照事件 |
| `POST` | `/contract-reviews/bulk/{archive,restore,delete}` | 批量处理记录 |

上传和审查均为异步任务。上传后先执行文档解析，状态进入 `ready`；点击开始后逐条分析，最终进入 `completed` 或 `failed`。删除记录会同时清理其条款、问题和文件资源。

## 实现约束

- 审查模型必须返回结构化 JSON；服务会限制每个条款最多五个问题，并对截断或无效 JSON 自动重试一次。
- 问题中的 `original_quote` 保留合同原文，用于在 PDF/DOCX 预览中定位；无法定位时会回退到条款起始位置。
- 当前内置规则集只有 `general-contract-review`，审查提示词和方法说明位于 `skills/preloaded/contract-review/`。
- 新增数据库迁移为 PostgreSQL `000097_contract_reviews` 和 SQLite `000013_contract_reviews`。
