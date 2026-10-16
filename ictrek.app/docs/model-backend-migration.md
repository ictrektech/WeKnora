# Model Hub 与 V-Scheduler

`HYBRAG_MODEL_BACKEND=auto` 按 VOS 网络内是否存在 `v-scheduler-model-hub` 选择统一后端；没有时保留旧 Model Hub。可显式设置 `scheduler` 或 `model-hub`。已选择 V-Scheduler 后，鉴权、容量或推理失败不会自动切回旧后端。

统一后端使用 `HYBRAG_V_ROUTER_URL`（默认 `http://v-router:8080`）的 `/v1` API。QA、图片理解、Embedding、Rerank 分别使用 `v-scheduler/ollama-qa/<HYBRAG_QA_MODEL>`、相同 QA 模型、`v-scheduler/ollama-embedding/<HYBRAG_EMBEDDING_MODEL>`、`v-scheduler/ollama-rerank/<HYBRAG_RERANK_MODEL>`。原生模型必须已安装并出现在统一模型目录。Embedding 默认保留 1024 维 bge-m3；已有知识库变更模型需要重新索引。

网关开启鉴权时，在服务端安装参数中设置 `HYBRAG_V_ROUTER_API_KEY`。后台索引没有可长期复用的浏览器身份；该凭据是网关 API Key，不是 Scheduler submitter token。网关为实际本地推理申请调度许可，HybRAG 不重复预约。三个 Ollama 的原生端口及 11535 端口不能替代统一网关的调度入口。

旧后端保留原来的模型地址、名称和配置。自定义 `HYBRAG_BUILTIN_MODELS_YAML` 仍由管理员管理，不会被新模式重写。应用包的两个模型后端依赖均为可选，PGV 为必需；运行时至少安装并配置所选模型后端。打包阶段实时核验发布记录和可下载包，不使用手写旧版本。
