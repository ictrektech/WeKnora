# HybRAG

HybRAG 是企业知识库、RAG 问答、Wiki 知识图谱和智能体平台。本 VOS 包使用 pull 模式安装 HybRAG 运行组件，默认复用 Model Hub 已预热并常驻的 QA、图片理解和 embedding 后端。为避免额外构建和飞书列迁移，镜像仓库名仍沿用已发布的 `weknora*` 镜像；VOS 应用名、app id、路由和容器服务名使用 HybRAG。

## 组件

- HybRAG Web 前端
- HybRAG App API
- DocReader 文档解析服务
- Agent Skills sandbox 镜像
- Redis
- Neo4j 知识图谱数据库
- 外部 PGV/Postgres 依赖

## Profile

安装时只需要选择一个 profile：`amd` 或 `arm`。HybRAG 自身不再启动 Ollama，Model Hub 负责 GPU/无 GPU、L4T、Thor 等运行时差异，因此 HybRAG 包只保留 AMD64 和 ARM64 两种应用 profile。

## 安装配置

安装 UI 会暴露模型、资源和文件映射配置。默认 HybRAG 运行数据写入 `/data/vos_workspace/hybrag` 下的 `files`、`docreader`、`redis` 子目录；模型下载、预热、常驻和 Ollama 资源参数由依赖的 Model Hub 应用管理。

Postgres 通过 PGV 提供，默认连接 `shared-pgv:5432`，用户/密码/数据库为 `weknora` / `weknora` / `WeKnora`。这些字段也会在安装 UI 中暴露；如果 PGV 安装时改过用户名、密码或数据库名，需要在 HybRAG 安装表单里同步修改。

实体关系知识图谱默认开启。VOS 包会随 HybRAG 启动独立 `hybrag-neo4j` 服务，App 内部默认使用 `bolt://hybrag-neo4j:7687` 连接，默认用户名/密码为 `neo4j` / `hybrag-neo4j`。Neo4j 数据默认写入 `/data/vos_workspace/hybrag/neo4j`，宿主机调试端口默认避开官方端口，使用 `27474`(Browser) 和 `27687`(Bolt)。如果要接入外部 Neo4j，可在安装 UI 中关闭内置服务使用方式对应的默认值，并同步修改 `NEO4J_URI`、`NEO4J_USERNAME`、`NEO4J_PASSWORD`。

## VOS 免登录

当前包提供一个临时 VOS iframe 免登录适配层，不要求修改 VOS。前端会优先读取未来可能注入的 `window.__VOS_APP_CONTEXT__`，然后兼容读取 VOS 当前同源会话里的 access token；后端再调用 `HYBRAG_VOS_USERINFO_URL` 指向的 `/v1000/user/check` 校验 token。

校验成功后，HybRAG 会按 VOS 用户名自动创建或登录 `username@local` 账户，并创建对应个人空间。`admin` 用户映射为 `admin@local`，会自动提升为 HybRAG 系统管理员并拥有跨空间管理权限。

这个方案只是过渡层。后续 VOS 支持标准 OIDC 或直接向 iframe 注入用户信息时，可以关闭 `HYBRAG_VOS_SSO_ENABLED`，或只替换前端取 token / 后端验身份的适配，不需要重做 HybRAG 本地用户和空间的创建逻辑。

## 其他 VOS App 接入 HybRAG 用户身份

其他 VOS app 如果要以当前 VOS 用户身份访问 HybRAG，不要共享或硬编码 HybRAG API Key。推荐做法是调用 HybRAG 的 VOS token exchange：

```http
POST /api/v1/auth/vos-token-exchange
Authorization: Bearer <VOS access token>
```

也可以用 JSON body：

```json
{
  "access_token": "<VOS access token>"
}
```

推荐流程：

1. 用户在 VOS 中打开其他 app。
2. 该 app 从 VOS 当前会话或 VOS 官方 SDK 取得当前用户的 VOS access token。
3. 该 app 后端把这个 token 转发给 HybRAG `/api/v1/auth/vos-token-exchange`。
4. HybRAG 调 VOS `/v1000/user/check` 校验 token。
5. HybRAG 按 VOS 用户名映射到 `${username}@local`，首次访问时自动创建用户和个人空间。
6. 该 app 后续调用 HybRAG API 时使用 exchange 返回的 HybRAG `token`。

账户统一规则：

| VOS 用户 | HybRAG 账户 | 默认空间 |
| --- | --- | --- |
| `admin` | `admin@local` | `admin's Workspace` |
| `alice` | `alice@local` | `alice's Workspace` |

当前 VOS 如果还没有稳定 OIDC 或正式 iframe 注入，前端临时适配可按 HybRAG 的顺序取 token：先读 `window.__VOS_APP_CONTEXT__.accessToken`，再读 `window.__VOS_APP_CONTEXT__.token`，再读 `window.__VOS_ACCESS_TOKEN__`，最后兼容同源 `localStorage` 中以 `-core-access` 结尾的 VOS store。这个 localStorage/secure-ls 方案只是过渡方案；未来 VOS 提供标准 OIDC 或 iframe 用户注入后，其他 app 应切到 VOS 官方方式。

`X-External-User-ID` 不是免 key 登录机制。它只适用于已经带 HybRAG API Key 的可信服务端调用，用来隔离终端用户会话；不负责校验 VOS 用户身份。

## 模型

本包不在 VOS 包里放额外 `config/` 目录。App 容器启动脚本会在运行时生成 `builtin_models.yaml` 并自动创建三条 YAML 托管模型行。界面中可通过 display name 区分两个 Model Hub Ollama 后端：

- `Model Hub Ollama QA (model-hub-ollama-qa)`：KnowledgeQA，端点 `http://model-hub-ollama-qa:11535/v1`
- `Model Hub Ollama VLM (model-hub-ollama-qa)`：VLLM，端点 `http://model-hub-ollama-qa:11535/v1`
- `Model Hub Ollama Embedding (model-hub-ollama-embedding)`：Embedding，端点 `http://model-hub-ollama-embedding:11535/v1`

模型行必须使用 `11535/v1` Gateway 地址，不能改成 Ollama 原生 `11434`。只有经过 Gateway 的请求才会被 Model Hub 统计槽位、运行阶段和 token/s。

默认模型名固定为 `qwen3.5:2b` 和 `bge-m3`，不再作为 HybRAG 安装参数暴露。模型下载、预热、上下文和并发由 Model Hub 配置；运行后也可以在 HybRAG UI 中添加或修改其他模型。

Ollama Qwen3.5 关闭思考使用 `thinking_control=think`，请求会发送顶层 `think:false`。vLLM / generic Qwen3.5 关闭思考使用 `thinking_control=chat_template_kwargs`，请求会发送 `chat_template_kwargs.enable_thinking=false`。

启动时，HybRAG 会等待 Model Hub 两个 gateway 的 `/v1/models` 可访问后再触发失败文档补交；服务本身不再负责拉取或常驻模型。默认按 QA 总槽位 `8`、聊天预留 `2`、后台共享 `6` 调度；embedding 后台使用 `2`。

详细预热、常驻和排错说明见仓库文档 `ictrek.app/docs/vos-ollama-prewarm.md`。
