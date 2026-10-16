# Model Hub 与 V-Scheduler

`HYBRAG_MODEL_BACKEND=auto` 独立发现统一 V-Scheduler 和旧 V-Router 发布的 Model Hub 模型，两者存在时同时加入可用列表，模型 ID 分开保存。自动添加不覆盖用户的云模型和默认模型选择。两套本地服务均未安装时生成空内置列表，应用仍可启动并使用用户添加的云模型。可显式设置 `scheduler` 或 `model-hub` 限定后端；推理失败不会跨后端回退。

仅存在旧 Model Hub 时沿用已有 `hybrag-ollama-*` 模型 ID，保留知识库引用；两套服务并存时，这些 ID 接入统一后端，旧服务以独立 `hybrag-modelhub-*` ID 补充显示。

统一后端使用 `HYBRAG_V_ROUTER_URL`（默认 `http://v-scheduler-gateway:8080`）的 `/v1` API，独立别名由 V-Scheduler 0.0.28 提供，避免旧 Router 同名 DNS 冲突。QA、图片理解、Embedding、Rerank 分别使用 `v-scheduler/ollama-qa/<HYBRAG_QA_MODEL>`、相同 QA 模型、`v-scheduler/ollama-embedding/<HYBRAG_EMBEDDING_MODEL>`、`v-scheduler/ollama-rerank/<HYBRAG_RERANK_MODEL>`。原生模型必须已安装并出现在统一模型目录。Embedding 默认保留 1024 维 bge-m3；已有知识库变更模型需要重新索引。

统一后端通过 `/api/v1/scheduler/gateway-binding` 声明能力并接收 V-Scheduler 自动下发的受限网关身份。
POST 仅接受 VOS userinfo 核验为启用的 `com.ictrek.v-scheduler` 应用，固定网关为
`http://v-scheduler-gateway:8080/v1`；修订必须递增，同修订同内容可重试。凭据原子保存在持久化
`/data/scheduler-gateway.local.json`（0600），不进入模型数据库、浏览器响应或日志。
四个内置统一模型每次实际请求都会重新读取私有凭据，已有后台索引客户端无需重启即可轮换；
请求仅可发到固定网关的模型列表、聊天、Embedding 和 Rerank 路径，禁止跟随重定向。
用户自建模型和旧 Model Hub 模式不改密钥。显式 `HYBRAG_V_ROUTER_API_KEY` 保留为未自动绑定时的兼容配置；
损坏或不安全的绑定文件不会退回旧密钥。该凭据是网关身份，不是 Scheduler submitter token，
不授予资源管理权限。网关为实际本地推理申请许可，HybRAG 不重复预约。
三个 Ollama 的原生端口及 11535 端口不能替代统一网关的调度入口。

旧后端同样经旧 V-Router 的 `/v1`，模型 ID 为 `model-hub/<原生名称>`，不是新模式的角色前缀。auto 默认按架构发现发布包已有的 `v-router-backend-amd` / `v-router-backend-arm` 独立服务名；也可通过 `HYBRAG_LEGACY_V_ROUTER_URL` 指定。鉴权使用独立 `HYBRAG_LEGACY_V_ROUTER_API_KEY`，不将 V-Scheduler 私钥发送给旧 Router。
无显式 tag 的 Ollama 名称归一为实际目录使用的 `:latest`（如 `bge-m3:latest`），避免把原生别名误当成已发布的统一模型 ID。
启动会用显式旧网关凭据检查 `/v1/models` 是否真实发布全部选定模型；缺路由、鉴权失败或缺模型时拒绝启动，
不回退到 Ollama 原生端口。旧链路必须安装并配置旧 Model Hub、V-Router 和其调度服务，只有旧管理 API 可达并不表示可调度。
自定义 `HYBRAG_BUILTIN_MODELS_YAML` 仍由管理员管理，不会被新模式重写。安装清单仅声明 PGV 依赖；V-Scheduler、旧 Model Hub / V-Router 都通过运行时发现，不声明安装版本约束，避免已安装的旧服务阻止纯云模式安装。统一本地网关需要 V-Scheduler 0.0.28 提供的独立别名；旧版本不存在该别名时不自动添加统一模型。打包阶段实时核验 PGV 发布记录和可下载包，不使用手写旧版本。
