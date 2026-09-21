# 智能档案

本文面向工作区管理员、使用者和部署维护者，说明法律工作台中的“智能档案”入口、异步导入、字段证据和提醒边界。工作区开关及数据删除原则见 [法律工作台](legal-workspace.md)。

## 快速结论

| 能力 | 当前状态 |
| --- | --- |
| 法律工作台入口 `/platform/smart-archive` | 已实现；与合同审查共用工作区开关和页面级子导航 |
| PDF、Word、Excel、JPG、PNG、WEBP 批量导入 | 已实现；档案立即可见，解析和镜像状态持久化 |
| 字段抽取、原文证据、客户归一化、文档关联 | 已实现；图片走 OCR，档案源文件是唯一业务事实源，解析/OCR 结果只生成一份 |
| 全文/结构化检索、回收站、批量操作、原文预览 | 已实现；模型和序列号筛选从标准化字段 JSON 检索 |
| 到期/归还/付款等提醒、待确认候选、站内通知 | 已实现；候选不会自动生成正式提醒 |
| 目标 VOS 实例上的迁移、模型和 OCR 实测 | 待验证；本地编译和聚焦测试已完成 |

## 使用方式

1. 在 `/platform/settings?section=general` 的“发布集成 → 法律工作台”中启用工作区。
2. 从侧栏进入“法律工作台 → 智能档案”。
3. 选择“导入”，上传受支持的文件。上传接口立即返回批次；页面通过 SSE 和轮询显示进度。
4. 在“文档”中查看抽取字段、置信度和证据定位；低置信度或图片 OCR 不可用时会进入“待复核/失败”状态。
5. 在“待确认事项”中确认日期和负责人，再创建提醒。创建后的提醒仍需手动启用。

## 数据流和一致性

导入请求先把源文件写入文件服务，再创建 `archive_import_batches`、`archive_import_items` 和 `archive_documents` 记录，最后投递 `smart_archive:document_process` 任务。Redis 模式使用 Asynq；Lite 模式使用当前 `SyncTaskExecutor`，两者都只在任务负载中传递租户、批次、文档和指纹，不传递 multipart 文件内容。

任务执行前会以租户和指纹声明领取租约；租约过期或进程重启时，启动恢复逻辑会重新投递未完成项目。项目指纹是 `tenant_id + file_hash + extraction_version`，重复导入不会重复建档或重复计数。解析/OCR 结果写入 `document_parse_artifacts`，写入成功后档案即标记为解析完成并正常显示。

托管知识库只是独立的镜像，使用 `mirror_status`（`not_started`、`pending`、`processing`、`submitted`、`failed`）表示最终一致状态。页面将解析和镜像合并成一个用户状态：解析未完成先显示解析阶段，解析完成后显示“等待同步/同步中/同步失败”，镜像已提交则统一显示“已完成”，避免同一文档同时出现两个成功状态。镜像任务只读取已经保存的源文件和解析产物：创建镜像或重新索引都以文档 ID 幂等，临时失败只重试镜像任务，耗尽后显示“同步失败”。读设置、读文档等接口不会扫描档案、回填产物或创建镜像；服务重启只恢复 `pending/processing` 镜像。删除中的文档会让并发镜像任务安全退出。

智能档案托管知识库由系统维护，描述带有 `[weknora-managed-smart-archive]` 标记。知识库详情页允许只读查看镜像文档，但不提供上传、编辑、重解析、移动或删除；这些操作必须通过智能档案页面完成。普通知识库接口拒绝直接编辑或删除该知识库；导入、恢复、重试和清理通过智能档案服务的内部写路径执行。源文件通过资源目录绑定，只有最后一个引用释放后才删除。

内部写路径会先校验当前租户、受管知识库和档案镜像元数据，再生成仅绑定到该受管知识库的任务级写授权。删除镜像时因此可以通过知识库服务的统一权限校验，同时不会获得其他知识库的写权限；API Key 的知识库范围限制仍然有效。

## 接口和权限

API 前缀为 `/api/v1/archive`。`Viewer+` 可读取文档、证据、客户、检索、提醒和站内通知；`Contributor+` 可导入、修正字段、重试解析、归档和维护提醒；`Admin+` 可恢复、移入回收站和永久清理文档。所有接口按当前租户隔离，并受法律工作台开关保护。

主要路由如下：

| 路径 | 作用 |
| --- | --- |
| `POST /import-batches` | 批量导入并返回持久化批次 |
| `GET /import-batches/:id` / `GET /import-batches/:id/events` | 读取批次快照或 SSE 进度 |
| `GET/PATCH /documents/:id` | 查看或修正档案字段 |
| `POST /documents/:id/retry-extraction` | 重新排队解析/OCR |
| `POST /documents/:id/retry-mirror` | 只重新排队知识库镜像，不重新解析 |
| `GET /documents/:id/evidence` / `GET /documents/:id/preview` | 查看证据或打开原文 |
| `POST /search` | 按自然语言和结构化过滤条件检索 |
| `/reminder-candidates`、`/reminders`、`/notifications` | 候选确认、提醒和站内通知 |

## 关闭工作区和提醒调度

关闭法律工作台只隐藏入口并使 `/api/v1/archive/**` 返回 `403`，不会删除档案、原文或托管知识库。到期扫描会跳过已关闭租户；重新开启后，调度器只处理仍然有效的到期事件，发生记录使用持久化指纹去重。删除数据仍应通过法律工作台规定的 Owner 清理流程完成。

## 迁移与验证

- PostgreSQL 使用 `migrations/versioned/000121_smart_archive_mirror_status.up.sql` 增加镜像状态字段和待处理索引。
- SQLite/Lite 使用 `migrations/sqlite/000029_smart_archive_mirror_status.up.sql` 增加镜像状态字段和待处理索引。
- 解析共享结果存储在 `document_parse_artifacts`；回滚迁移会按依赖顺序删除智能档案表。
- 已验证：`go test ./internal/types ./internal/application/repository -count=1`、智能档案受管镜像写授权聚焦测试、后端路由/服务/容器编译、SQLite migration 聚焦测试，以及前端 `npm run type-check`、`npm run check-i18n`、智能档案状态测试和生产构建。
- 待验证：目标部署的真实 OCR/模型调用、Redis 重启后的任务恢复、VOS 包升级后的迁移和资源共享删除行为。
