# HybRAG

HybRAG 是企业知识库、RAG 问答、Wiki 知识图谱和智能体平台。本 VOS 包使用 pull 模式安装 HybRAG 运行组件，并按所选 profile 启动对应 `ollama_server` 作为聊天、图片理解和 embedding 后端。为避免额外构建和飞书列迁移，镜像仓库名仍沿用已发布的 `weknora*` 镜像；VOS 应用名、app id、路由和容器服务名使用 HybRAG。

## 组件

- HybRAG Web 前端
- HybRAG App API
- DocReader 文档解析服务
- Agent Skills sandbox 镜像
- Ollama QA/VLM 容器
- Ollama embedding 容器
- Redis
- 外部 PGV/Postgres 依赖

## Profile

安装时选择一个 profile：`AMD_with_cuda`、`ARM_with_cuda`、`l4t` 或 `thor_spark`。

AMD 和 ARM 通用 profile 分别从 `AMD_with_cuda`、`ARM_with_cuda` 飞书表读取 `weknora*` 与 `ollama_server` 镜像版本；`l4t`、`thor_spark` 使用各自表格。本应用只发布 4 个 profile。

## 安装配置

安装 UI 会暴露模型、资源和文件映射配置。默认 HybRAG 运行数据写入 `/data/vos_workspace/hybrag` 下的 `files`、`docreader`、`redis` 子目录；Ollama 模型默认复用 Model Hub 共享目录 `/data/vos_workspace/model_hub/ollama`，除非安装时手动调整 `MODEL_HUB_SHARED_MODELS_PATH`。

Postgres 通过 PGV 提供，默认连接 `shared-pgv:5432`，用户/密码/数据库为 `weknora` / `weknora` / `WeKnora`。这些字段也会在安装 UI 中暴露；如果 PGV 安装时改过用户名、密码或数据库名，需要在 HybRAG 安装表单里同步修改。

## VOS 免登录

当前包提供一个临时 VOS iframe 免登录适配层，不要求修改 VOS。前端会优先读取未来可能注入的 `window.__VOS_APP_CONTEXT__`，然后兼容读取 VOS 当前同源会话里的 access token；后端再调用 `HYBRAG_VOS_USERINFO_URL` 指向的 `/v1000/user/check` 校验 token。

校验成功后，HybRAG 会按 VOS 用户名自动创建或登录 `username@local` 账户，并创建对应个人空间。`admin` 用户映射为 `admin@local`，会自动提升为 HybRAG 系统管理员并拥有跨空间管理权限。

这个方案只是过渡层。后续 VOS 支持标准 OIDC 或直接向 iframe 注入用户信息时，可以关闭 `HYBRAG_VOS_SSO_ENABLED`，或只替换前端取 token / 后端验身份的适配，不需要重做 HybRAG 本地用户和空间的创建逻辑。

## 模型

本包不在镜像中写死默认模型。安装后需要通过 HybRAG UI 添加模型，或后续挂载模型配置文件。默认网络端点为：

- QA/VLM: `http://hybrag-ollama-qa:11535/v1`
- Embedding: `http://hybrag-ollama-embedding:11535/v1`

如由 Model Hub 预先管理模型，请确保对应模型已经存在于 Ollama 数据目录中。

QA/VLM Ollama 默认模型名为 `qwen3.5:2b`。普通 profile 默认 QA 总槽位 `8`、聊天预留 `2`、后台共享 `6`；embedding Ollama 总槽位 `4`、文档 embedding 使用 `2`。`thor_spark` 使用更高默认值：QA 总槽位 `20`、聊天预留 `6`、后台共享 `14`、embedding 总槽位 `16`、文档 embedding 使用 `8`。
