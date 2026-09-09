# VOS HybRAG Changelog

记录 VOS HybRAG 应用的可验证变化。

## [Unreleased]

### 重点变化

- **法律工作台与合同审查** — 新增工作区级法律工作台，入口为 `/platform/contract-review`；支持 PDF/DOCX 上传、异步解析、结构化风险审查、结果摘要、原文预览和证据定位。结果应结合 `quality_status` 与 `warnings` 判读，不替代律师意见或正式法律审查。[法律工作台说明](docs/legal-workspace.md) · [合同审查说明](../docs/contract-review.md)

### 新增

- **合同审查工作流与 API** — 提供 `general-contract-review` `1.0` 规则集、合同记录生命周期、批量归档/恢复/删除、SSE 状态事件，以及 `/api/v1/contract-reviews`、`/api/v1/contract-reviews/:id/document`、`/start`、`/retry`、`/cancel` 等接口。

- **审查配置与模型选择** — 在审查详情中配置审查视角、规则集和 `model_id`；`model_id` 为空时保留自动选模路径，并在模型异常时提供配置或重试入口。

- **证据、事实与质量信号** — 为审查记录增加 `source_revision`、`source_text_hash`、locator、evidence refs、全文事实、结构警告和 `quality_status`，把模型问题与服务端确定性校验结果分开呈现。

### 优化

- **PDF/DOCX 原文定位** — DocReader 为 PDF 和 DOCX 保留稳定 source units（源文档单元）；前端按源文本版本、证据范围和渲染结果执行精确定位，并对未找到、版本不匹配、重复候选或不支持的证据显示明确状态，不猜测高亮。

- **模型结果校验与降级** — 严格校验结构化 JSON、引用片段和证据范围；单条可隔离的证据错误不会丢弃同批有效问题，超出服务限制的问题批次保留优先项并标记降级，全文事实和结构警告由服务端生成。

- **异步任务隔离与恢复** — 通过 `analysis_run_id` 和 `config_hash` 隔离上传、解析、审查和重试运行；旧 worker 不能回写新运行，运行中的审查可取消，终态记录可重试，并增加队列清理和卡住任务回收路径。

### 修复

- **日语合同审查文案** — 补齐 `frontend/src/i18n/locales/ja-JP.ts` 中的合同审查界面文案，覆盖配置、状态、风险、证据和错误提示。

### 打包与部署

- **VOS Compose 配置** — `ictrek.app/src/docker-compose.yml` 支持以 `VOS_REDIS_PASSWORD` 优先设置应用和 Redis 密码，并向 AMD/ARM app profile 传递 `ICTREK_LEGAL_WORKSPACE_DEFAULT_ENABLED`；该变量默认关闭新建或未配置工作区，已保存的工作区设置优先。

- **迁移版本兼容** — 合同审查和法律工作台的 PostgreSQL 迁移固定在 `000101`–`000104`、`000106`–`000107`，SQLite 迁移固定在 `000016`–`000019`，避开已发布迁移的版本冲突，并保留缺失字段的幂等修复路径。

### 模型与运行

- **本地 QA vLLM 生命周期** — `ictrek.app/docs/local-dev/ictrek-dev.sh` 增加 `start-vllm`、`stop-vllm` 和 `restart-vllm`；脚本会检查现有容器的镜像、网络、GPU、共享内存、模型挂载和启动参数，仅复用参数一致的容器，参数变化时要求重建。该变化适用于本地开发验证，不改变 VOS 发布 compose 的模型服务边界。

### 安全与权限

- **工作区访问与数据删除边界** — 法律工作台开关按工作区生效：关闭后隐藏入口并阻止合同审查接口访问，但不删除数据；`Admin+` 可修改配置，只有 `Owner` 可调用 `/api/v1/legal-workspace-data` 永久删除合同审查记录、结果和源文件绑定，共享资源仍被其他对象引用时保留。

### 文档与验证

- **功能与运维文档** — 新增并刷新 `docs/contract-review.md`、`ictrek.app/docs/legal-workspace.md` 和 `ictrek.app/docs/local-dev/README.md`，记录工作流、状态、权限、数据删除、迁移和本地模型验证边界。

- **自动化验证覆盖** — 增加或更新合同审查 service/repository/handler/router、法律工作区、SQLite/PostgreSQL migration、DocReader source units、前端状态和 `documentLinking` 测试；代码同时覆盖队列取消、旧运行隔离、证据歧义和降级路径。

### 已知限制

- **精确定位依赖 DocReader 元数据** — 部署使用的 DocReader 必须返回有效 `source_units_json`；缺失或不一致时审查仍可运行，但 PDF/DOCX 精确高亮会降级为不可定位或受限匹配，实际 VOS 版本仍需在目标环境验证。

- **当前不是完整合同管理系统** — 目前只有 `general-contract-review` `1.0` 规则集；合同版本管理、版本对比、红线编辑、结果导出和自定义规则集接口尚未实现，工作台不会直接执行对话式 Agent 的完整知识库/网页引用流程。
