# WeKnora ictrek 本地快速调试

**已实现：本地源码开发可通过 V-Scheduler（统一模型网关与调度服务）替代旧 Model Hub（本地模型管理服务）代理。** 已安装 V-Scheduler 时，执行 `setup --profile scheduler` 并完全重启后端；QA（问答）、VLM（图片理解）、Embedding（文本转向量）和 ReRank（检索结果重排）统一访问宿主机的 `22888/v1`。

这套方式面向源码调试：Go 后端和 Vue/Vite 前端在宿主机运行，Docker 启动 PostgreSQL、Redis、DocReader、Neo4j 等基础设施。开发数据库和数据目录独立于 VOS（应用部署平台）安装的 HybRAG；模型服务由已安装的 V-Scheduler 提供，本脚本不启动或升级它。

## 当前状态

| 能力 | 状态 | 说明 |
| --- | --- | --- |
| Go 后端源码调试 | 已实现 | 支持 `go run`；安装 Air 后自动热重载。 |
| 前端源码调试 | 已实现 | Vite 热更新，默认代理到 `127.0.0.1:8080`。 |
| 基础设施 | 已实现 | 使用本目录的隔离 Compose 和 `/data/hybrag-dev-data`。 |
| PostgreSQL 镜像对齐 | 已实现（配置） | 默认使用 VOS PGV（提供 PostgreSQL 与检索扩展的应用）`0.0.49` 的 PostgreSQL 17 镜像；旧 PG16 数据须迁移或保留并改用新目录，见[操作说明](#postgresql-镜像替换)。 |
| V-Scheduler 模型接入 | 已实现 | 四类模型各保留一项，统一走宿主机网关；迁移旧模型 ID（数据库中的模型标识）引用后去重，见[接入说明](#v-scheduler)。 |
| 旧 Model Hub 代理 | 已实现（兼容） | 仅显式选择 `jetson-model-hub` 时使用，见[代理说明](model-hub-ollama-proxy.md)。 |
| 模型初始化 | 已实现（配置） | `auto` 优先探测 V-Scheduler；不可达时按架构选择旧配置。已有 `.env` 保留保存的 profile（开发配置组合），切换须显式指定。 |
| VOS iframe 免登录 | 未启用 | 本地源码模式使用普通注册/登录，`HYBRAG_VOS_SSO_ENABLED=false`。 |
| VOS 发布部署 | 不在本流程 | 发布和安装仍以 `ictrek.app/README.md`、`src/` 和 `package.sh` 为准。 |

## 快速启动

在仓库根目录执行：

```bash
# 已安装 V-Scheduler：切换已有开发环境
./ictrek.app/docs/local-dev/ictrek-dev.sh setup --profile scheduler
./ictrek.app/docs/local-dev/ictrek-dev.sh start
```

`setup` 更新已有 `.env`，保留数据库密码和应用密钥。`scheduler` 不创建旧 Model Hub 代理，不依赖旧 `31535`、`31536`、`31537` 端口。

首次新环境也可执行不带 `--profile` 的 `setup` 自动选择。保留 tc232 的 vLLM（兼容 OpenAI 接口的模型运行时）环境时，显式执行 `setup --profile tc232`；旧 Model Hub 环境按[代理说明](model-hub-ollama-proxy.md)准备镜像，再执行 `setup --profile jetson-model-hub`。

如果当前目录已经是 `ictrek.app`，上面的脚本路径应改为 `./docs/local-dev/ictrek-dev.sh`。

首次执行 `setup` 且根目录没有 `.env` 时，会为 `DB_PASSWORD`、`REDIS_PASSWORD`、`NEO4J_PASSWORD`、`JWT_SECRET`、`TENANT_AES_KEY` 和 `SYSTEM_AES_KEY` 生成随机值并写入 `.env`。已有值不会覆盖；已有 `.env` 缺少服务密码时会补写固定的本地开发回退值。

然后分别打开两个终端：

```bash
# 终端 2：Go 后端
./ictrek.app/docs/local-dev/ictrek-dev.sh app

# 终端 3：前端；也可以继续使用仓库已有的 make dev-frontend
make dev-frontend
```

首次启动 `docreader` 可能需要构建镜像；需要明确触发构建时使用：

```bash
./ictrek.app/docs/local-dev/ictrek-dev.sh start --build
```

首次注册用户使用 `admin@weknora.local` 时，启动配置会在下一次后端重启后尝试把该用户提升为系统管理员。这个变量只在数据库还没有系统管理员时生效，不会覆盖已有管理员。

## 模型后端

本地调试支持以下 profile：

| Profile | 模型配置 | 适用场景 |
| --- | --- | --- |
| `scheduler` | `builtin_models.scheduler.yaml` | 使用已安装 V-Scheduler 的统一网关；四类模型均访问 `http://127.0.0.1:22888/v1`。 |
| `tc232` | `builtin_models.tc232.yaml` | 原有 x86_64/tc232 vLLM 环境，参数和模型地址保持原样。 |
| `jetson-model-hub` | `builtin_models.model-hub.yaml` | 通过独立代理复用 VOS Model Hub；QA/VLM 使用 `31535`，Embedding 使用 `31536`，ReRank 使用 `31537`。 |
| `ollama` | `builtin_models.ollama.yaml` | 非 Jetson 主机上的宿主机 Ollama。 |

首次没有已保存 profile 时使用 `auto`：先以两秒超时检查 `ICTREK_DEV_SCHEDULER_URL` 的 `/healthz`，可达则选择 `scheduler`。否则按 `uname -m` 和 Jetson 的 `/etc/nv_tegra_release` 或设备树标识选择旧配置：Jetson 要求 Model Hub 的 QA/Embedding 容器已运行，其他 ARM64（64 位 ARM 架构）主机选择 `ollama`，`x86_64` 选择 `tc232`。服务健康不代表模型已经安装或推理获准。`setup` 保留已保存的 profile，重新探测使用 `setup --profile auto`。

### V-Scheduler

**已实现：**宿主机后端直接访问统一网关的 `/v1/chat/completions`、`/v1/embeddings` 和 `/v1/rerank`，由 V-Scheduler 选择模型后端并检查可用资源。模型列表接口也使用配置的宿主机地址，避免宿主机无法解析 Docker 内部的 `v-scheduler` 名称。原生 Ollama（本地模型运行时）端口和各实例的 `11535` 推理端口不能替代统一调度入口。

问答和图片理解使用 `thinking_control=reasoning_effort`，将关闭思考转换为 `reasoning_effort=none`；Ollama 的兼容接口通过该字段控制思考，见 [Ollama 接口文档](https://docs.ollama.com/api/openai-compatibility)。沿用 vLLM 的 `chat_template_kwargs.enable_thinking=false` 时，本机试请求仍只生成思考内容。向量和重排请求超时显式设为 180 秒，容纳网关准入及冷加载（模型首次载入内存）；调用方更早的取消仍生效，资源拒绝不会因延长超时而绕过。

默认模型如下，均须已在 V-Scheduler 的 `/v1/models` 中发布：

| 用途 | 统一网关模型名 | 覆盖变量 |
| --- | --- | --- |
| 问答、图片理解 | `v-scheduler/ollama-qa/qwen3.5:2b` | `ICTREK_DEV_SCHEDULER_QA_MODEL` |
| 向量 | `v-scheduler/ollama-embedding/bge-m3:latest` | `ICTREK_DEV_SCHEDULER_EMBEDDING_MODEL` |
| 重排 | `v-scheduler/ollama-rerank/qllama/bge-reranker-v2-m3:q8_0` | `ICTREK_DEV_SCHEDULER_RERANK_MODEL` |

覆盖变量写入 `.env`，值使用模型目录中的完整名称。更换地址时执行：

```bash
ICTREK_DEV_SCHEDULER_URL=http://127.0.0.1:22888 \
  ./ictrek.app/docs/local-dev/ictrek-dev.sh setup --profile scheduler
./ictrek.app/docs/local-dev/ictrek-dev.sh check
```

`ICTREK_DEV_SCHEDULER_URL` 可带 `/v1`，脚本会统一处理。远程网关须使用宿主机可达地址，并在 `SSRF_WHITELIST`（允许模型访问的私有主机清单）中加入实际主机。网关要求认证时，将受限推理密钥写入被 Git 忽略的 `.env` 的 `ICTREK_DEV_SCHEDULER_API_KEY`，再执行 `setup --profile scheduler`。该显式密钥在后端用于目录查询和配置网关上的推理；从页面新增的模型可以不填密钥，已有模型自身的显式密钥优先。只有 `ICTREK_DEV_PROFILE=scheduler` 才启用宿主机地址覆盖和本地密钥路径；本地请求不读取生产绑定文件，生产绑定的私有凭据仍限固定 Docker 网关，详见[网关身份边界](../model-backend-migration.md#网关与模型接入)。本地源码实例不会自动接收 VOS 已安装应用的身份绑定。

模型去重：开发配置只保留 QA、图片理解、向量、重排各一项；QA 与图片理解使用同一个 Qwen 模型，但在 WeKnora 中承担不同用途。原先临时保留的 7 个 `compat`（旧模型 ID 的兼容项）已移除。新增模型时，在模型来源的 V-Scheduler 下拉框选择已注册模型；前端使用后端返回的 `base_url` 保存和回显，旧响应未提供该字段时继续使用 `http://v-scheduler:8080/v1`。

已有开发数据库须先备份，再将会话、智能体、知识库等配置中的旧兼容 ID 改为 YAML 中对应类型的主 ID，确认无剩余引用后重启后端。移除的 YAML 行会被软删除（保留记录但不再显示），脚本不会自动迁移引用。本机已在事务内核对新旧模型的名称、类型、来源及参数相同，再迁移 1 条会话设置、1 条消息模型标识和 1 个知识库引用，移除 7 个兼容项；未改消息正文或已有向量。

默认 QA 统一使用 `qwen3.5:2b`，原 tc232 的 `qwen3.5-9b-awq` 引用也会改用该模型；需要其他模型时设置完整 QA 覆盖值。

YAML（模型配置文件）只更新 `managed_by=yaml` 的行，用户手工添加或编辑的模型继续保留，须另行在页面修改旧地址。向量维度保持 1024，但旧 vLLM 向量和当前 Ollama 向量的一致性不能仅凭维度确认；旧 vLLM 知识库须验证检索质量，必要时重新索引。不会自动删除文件或重建向量。

失败边界：认证失败返回 `401`，模型未就绪或资源不足可能返回 `503` / `429`，不会改走旧代理或原生 Ollama。`check` 验证网关健康和模型目录，不执行模型下载或真实推理；失败须先在 V-Scheduler 中检查模型、资源和授权。

验证标准：配置和密钥保留、旧模型引用保留、每类仅一个默认模型、宿主机模型目录查询、真实聊天、1024 维向量及重排均应通过；仅目录可达不能判定推理成功。

本机验证（2026-10-22，源码后端 `8081`、Vite `5173`、统一网关 `22888`）：

| 检查 | 结果与限制 |
| --- | --- |
| 自动选择、显式切换、配置及模型兼容 | 已验证：`test-scheduler.sh`、`TestLocalDevSchedulerHasOneModelPerRole` 通过；本机原凭证、端口及数据路径不变，引用迁移后每类仅一项，用户云模型保留；现有知识库缺失模型引用数为 0。 |
| 添加模型界面 | 已验证：`scheduler-models.spec.ts` 模拟宿主机地址、VOS 正式地址、旧响应缺省地址三种目录响应，选择、刷新、保存及重新编辑回显均通过；保存数据不含网关密钥。真实 API（应用接口）另通过目录查询、新增无密钥模型、约 18.4 秒返回正文“好”、删除临时模型的验证。 |
| 宿主机目录及凭据边界 | 已验证：`TestSchedulerCatalog*`、`internal/schedulergateway`、`internal/models/runtime` 测试通过；本地密钥仅用于配置网关的推理路由，生产绑定允许的地址保持不变。真实后端和 Vite 代理可发现 16 个模型。 |
| 聊天 | 已验证：通过 WeKnora 模型调试接口关闭思考，`qwen3.5:2b` 在约 11.7 秒返回正文“好”；只统计单次试请求，不代表吞吐基准。 |
| 重排 | 已验证：通过相同调试接口处理 2 条文档，约 77.5 秒返回，相关文档排首位。旧 60 秒上限下曾超时；延长为 180 秒后通过。 |
| 向量 | 已验证：重排结束后重试，通过 WeKnora 调试接口约 33.9 秒返回 1024 维向量。此前曾返回 `429 WAIT_RESOURCE`（调度器判定资源不足）和 `WAIT_STOP_CONFIRMATION`（前次推理停止仍在核验）；保留这些资源保护行为。 |
| VOS 路径回归 | 已验证：`ictrek.app/scripts/test_model_backend.py` 的 11 项测试及 `internal/schedulergateway` 全包测试通过，覆盖安装配置、自动发现、绑定、轮换和推理凭据限制。安装 Compose、启动脚本和绑定允许的固定地址未修改；本地 `.env` 不进入应用镜像，打包 `.env` 由脚本在暂存目录单独生成。尚未重新打包安装验证。 |
| 前端生产构建 | 已验证：`npm run build` 通过，仍有脚本打包和资源体积提示；不代表已构建或安装完整 VOS 包。 |
| 完整流程 | 待验证：图片理解、文件上传入库、完整检索问答及旧 vLLM 向量一致性。 |

### 旧 vLLM 与 Ollama 配置

`tc232` profile 把 QA/VLM 指向 `http://127.0.0.1:38118/v1`，把 bge-m3 embedding 指向 `http://127.0.0.1:32223/v1`，把 `bge-reranker-v2-m3` ReRank 指向 `http://127.0.0.1:32224`。这些地址都必须由 vLLM 或其他 OpenAI-compatible 服务提供；本目录的基础设施 Compose 不会自动启动模型服务。

如果 tc232 上已有 QA 模型目录，可以启动可选的 QA vLLM：

```bash
ICTREK_DEV_VLLM_MODEL_DIR=/path/to/Qwen-model \
  ./ictrek.app/docs/local-dev/ictrek-dev.sh start-vllm
```

脚本默认管理容器 `qwen35-9b-awq-vllm`，默认模型目录是 `/data/models/QuantTrio--Qwen3.5-9B-AWQ`，也支持直接指向 Hugging Face 的 `snapshots/<revision>/` 父目录。`stop-vllm` 只停止容器并保留容器配置，`start-vllm` 只复用启动参数完全一致的容器；如果参数不一致，命令会失败并提示使用 `restart-vllm`：

```bash
./ictrek.app/docs/local-dev/ictrek-dev.sh stop-vllm
./ictrek.app/docs/local-dev/ictrek-dev.sh start-vllm
./ictrek.app/docs/local-dev/ictrek-dev.sh restart-vllm
```

如果当前目录已经是 `ictrek.app`，上面三个命令分别使用 `./docs/local-dev/ictrek-dev.sh`。

`restart-vllm` 会优雅停止并删除 `qwen35-9b-awq-vllm`，再按当前配置重新执行 `docker run`。模型目录以只读方式挂载，不会删除模型文件。

显式传入的 `ICTREK_DEV_*` 环境变量优先于 `.env`，例如临时切换到已存在的模型目录：

```bash
ICTREK_DEV_VLLM_MODEL_DIR=/path/to/Qwen-model \
  ./docs/local-dev/ictrek-dev.sh restart-vllm
```

### ReRank vLLM

`bge-reranker-v2-m3` 使用 Hugging Face/Transformers 格式模型目录（包含 `model.safetensors`），通过 vLLM 的 pooling 模式提供 ReRank API，不通过 Ollama。默认使用独立端口 `32224`，与 QA `38118` 和 embedding `32223` 分开。

执行 `setup` 后，使用下面的命令启动或重建 ReRank 服务：

```bash
./ictrek.app/docs/local-dev/ictrek-dev.sh setup
./ictrek.app/docs/local-dev/ictrek-dev.sh start-rerank

# 模型目录或启动参数变化后重建
ICTREK_DEV_RERANK_VLLM_MODEL_DIR=/path/to/bge-reranker-v2-m3 \
  ./ictrek.app/docs/local-dev/ictrek-dev.sh restart-rerank
```

脚本默认管理容器 `bge-reranker-v2-m3-vllm`，只读挂载 `/data/models/bge-reranker-v2-m3`，使用 `vllm/vllm-openai:v0.18.1-cu130` 和 `--runner pooling`。`start-rerank` 会拒绝使用 QA 或 embedding 已配置的端口；如需改端口，同时设置 `ICTREK_DEV_RERANK_VLLM_PORT` 和 `ICTREK_DEV_RERANK_VLLM_BASE_URL`，并保持它与 `38118`、`32223` 不同。

验证服务：

```bash
curl -fsS http://127.0.0.1:32224/health
curl -sS http://127.0.0.1:32224/rerank \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-reranker-v2-m3","query":"什么是人工智能？","documents":["人工智能是研究智能机器的技术。","今天是晴天。"]}'
```

YAML 中 ReRank 的 `Base URL` 要填 `http://127.0.0.1:32224`，不要加 `/v1`；WeKnora 会在该地址后请求 `/rerank`。本地后端运行在宿主机，当前默认 `SSRF_WHITELIST` 已包含 `127.0.0.1`。YAML 模型在后端启动时加载；如果后端已经运行，启动 ReRank 服务后还需重启一次 `$DEV app`，新的默认模型才会出现在模型管理页面。

如果在非 Jetson 主机使用宿主机 Ollama，不需要启动 vLLM：

```bash
./ictrek.app/docs/local-dev/ictrek-dev.sh setup \
  --profile ollama
ollama pull qwen3.5:2b
ollama pull bge-m3
```

如果仍使用旧 VOS Model Hub，通过独立代理复用它：

```bash
./ictrek.app/docs/local-dev/ictrek-dev.sh setup \
  --profile jetson-model-hub
./ictrek.app/docs/local-dev/ictrek-dev.sh start-model-hub-proxy
```

该 profile 的 QA/Embedding 要求 `model-hub-ollama-qa(-l4t)` 和 `model-hub-ollama-embedding(-l4t)` 在 `vos_default` 网络运行；ReRank 还需 Model Hub `>= 0.0.92` 的 `model-hub-ollama-rerank` 服务和已下载的 `qllama/bge-reranker-v2-m3:q8_0` 模型。代理将 `127.0.0.1:31535` 转发到 QA `11535`，将 `127.0.0.1:31536` 转发到 Embedding `11535`；本地 QA/VLM 通过 `OLLAMA_BASE_URL=http://127.0.0.1:31535`，Embedding 通过 OpenAI-compatible 的 `http://127.0.0.1:31536/v1`。QA/Embedding 不要直连原生 `11434` 或容器 bridge IP，否则会绕过 Gateway 指标。ReRank 通过 `127.0.0.1:31537/v1` 转发到与 VOS 包相同的 `model-hub-ollama-rerank:11434/v1`；其指标能力待验证。VOS 重启后只要上游服务仍使用同名网络 alias，代理不需要改地址。

`tc232` 配置也包含该 Model Hub ReRank 模型，作为非默认可选项；可手动运行 `start-model-hub-proxy`。旧代理只有两个端口，需要按 [代理更新步骤](model-hub-ollama-proxy.md#创建代理容器) 重建。模型配置从 YAML（模型配置文件）同步到开发数据库，并非自动列出 Model Hub 中的所有模型；重启 `$DEV app`/Air 后再刷新页面，已有智能体需手动选择并保存新模型。

在非 Jetson 主机上使用宿主机 Ollama 时，将 profile 改为 `ollama`。不建议把 `legacy/deploy-template` 下的 Orin 容器 overlay 或分离 gateway YAML 直接用于本地源码调试；它们属于 VOS 部署流程。

也可以只在 Web UI 中添加模型。YAML 声明的行由 `managed_by=yaml` 管理，切换 YAML 时可能软删除旧的 YAML 托管行；因此不要把这套配置指向 VOS 生产数据或其他需要保留的部署数据库。

检查模型服务：

```bash
./ictrek.app/docs/local-dev/ictrek-dev.sh check
curl -fsS http://127.0.0.1:38118/v1/models
curl -fsS http://127.0.0.1:32223/v1/models
curl -fsS http://127.0.0.1:11434/api/tags  # 仅 ollama profile
```

Model Hub profile 应检查代理端口：

```bash
curl -fsS http://127.0.0.1:31535/api/tags
curl -fsS http://127.0.0.1:31535/v1/models
curl -fsS http://127.0.0.1:31536/v1/models
curl -fsS http://127.0.0.1:31537/v1/models
```

`check` 只检查当前 profile 选择的模型后端。Ollama profile 不会因为未启动 vLLM 或可选 ReRank 而产生无关告警。

## 地址与数据

| 服务 | 地址 | 数据 |
| --- | --- | --- |
| 前端 Vite | `http://localhost:5173` | — |
| Go API | `http://localhost:8080` | — |
| V-Scheduler 统一网关 | `http://127.0.0.1:22888/v1` | 已安装服务；本地脚本不管理其数据 |
| PostgreSQL | `127.0.0.1:15432` | `/data/hybrag-dev-data/postgres` |
| Redis | `127.0.0.1:6380` | `/data/hybrag-dev-data/redis` |
| DocReader | `127.0.0.1:15051` | `/data/hybrag-dev-data/docreader` |
| Neo4j Bolt | `bolt://127.0.0.1:27687` | `/data/hybrag-dev-data/neo4j` |
| Neo4j Browser | `http://127.0.0.1:27474` | — |
| Model Hub QA/VLM proxy | `http://127.0.0.1:31535` | 独立代理容器；上游 `model-hub-ollama-qa:11535` |
| Model Hub Embedding proxy | `http://127.0.0.1:31536/v1` | 独立代理容器；上游 `model-hub-ollama-embedding:11535` |
| Model Hub ReRank proxy | `http://127.0.0.1:31537/v1` | 独立代理容器；上游 `model-hub-ollama-rerank:11434` |
| QA vLLM | `http://127.0.0.1:38118/v1` | 外部模型目录 |
| bge-m3 vLLM | `http://127.0.0.1:32223/v1` | 外部模型目录 |
| ReRank vLLM | `http://127.0.0.1:32224` | `/data/models/bge-reranker-v2-m3` |

`stop` 只移除开发容器和网络，不删除 `/data/hybrag-dev-data`。如需清空本地数据库，请先确认目标路径，再单独备份或删除该目录。

### PostgreSQL 镜像替换

**已实现（配置）：**开发库默认使用 `swr.cn-southwest-2.myhuaweicloud.com/ictrek/pgv:amd_17_20261015`，脚本在 ARM64（64 位 ARM 架构）主机选择 `arm_17_20261015`。两个镜像标签已核对 VOS PGV `0.0.49` 正式发布包 `pgv_0.0.49_pull.tar` 内的 `.env`；启动时继续使用 `shared_preload_libraries=pg_search,pg_cron,vchord.so,vectors.so`（预加载的扩展库），以减少本地与生产的数据库运行差异。`ICTREK_DEV_PGV_IMAGE` 可覆盖为同架构完整 PGV 镜像；不要用仅含 ParadeDB 的基础镜像替换后继续预加载 PGV 专用扩展。标签固定，不会自动跟随 VOS 更新；若 `.env`、`.env.local` 或当前终端显式设置了旧镜像，须删除覆盖或改为上述对应架构标签。

开发库继续使用独立端口、凭证和数据目录。本地 Compose 只对齐数据库镜像，不包含 VOS 安装包的自动迁移与应用数据库初始化服务。**旧 PostgreSQL 16 数据不能直接由 PostgreSQL 17 读取**，更新配置不会自动迁移或清空数据；直接启动会报 `database files are incompatible with server` 并反复重启，延长就绪等待时间无效。保留数据时，先用兼容的 PG16 镜像恢复旧库运行并做逻辑备份（导出数据库对象和数据），再按 [PostgreSQL 主版本升级说明](https://www.postgresql.org/docs/17/upgrading.html) 迁移到独立新目录并验证；本脚本不执行主版本迁移。

若只需新建开发数据，先停止 Go 后端，再在仓库根目录执行以下命令。原目录保留；新目录用于本套开发基础设施的全部数据，包括 Redis 和 Neo4j，而不只是 PostgreSQL。

```bash
DEV=./ictrek.app/docs/local-dev/ictrek-dev.sh
$DEV stop
export ICTREK_DEV_DATA_DIR=/data/hybrag-dev-data-pg17
$DEV start
$DEV app
```

后端在新库初始化应用表。后续命令保持同一 `ICTREK_DEV_DATA_DIR`，或将新路径写入 `.env`；若 `.env.local` 也设置了该值，须同步修改。数据库健康检查等待 TCP（网络连接）服务就绪，避免在初始化临时服务器运行时提前启动后端；等待失败时 `start` 返回失败，不再报告基础设施启动成功。失败时查看日志，核对镜像架构、扩展预加载和数据目录主版本，不要删库重试。

直接运行 Compose 默认使用 AMD 镜像；ARM 主机须设置完整的 `ICTREK_DEV_PGV_IMAGE`。已有 PostgreSQL 17 库只升级扩展版本时，遵循 [ParadeDB 存量库升级](../../../website-docs/01-getting-started/06-paradedb-upgrade.md)，将数据库命令替换为本目录的 Compose 文件、项目名和实际环境文件；迁移 `000137` 已执行时仍需管理员完成扩展升级。

**已验证（`20261015` 标签）：**AMD/ARM 标签均已在镜像仓库确认存在；脚本选型、显式覆盖、Compose 默认值、预加载、健康检查配置及 PostgreSQL 就绪失败的返回行为通过本目录 `test-pgv.sh`。隔离空库使用 Compose 配置中的 AMD 镜像与启动参数启动成功，实际运行 PostgreSQL `17.11`；TCP 就绪、`pg_search`、`pg_cron`、`vchord`、`vector`、`vectors` 扩展创建及向量距离查询通过，临时容器与数据卷已清理。

**历史验证（`amd_17_20261014`，不代表新镜像回归结果）：**本地 Compose 的 AMD PG17 服务在独立项目与数据卷启动成功，实际运行 PostgreSQL `17.11`、`pg_search 0.25.11`、`vector 0.8.4`；Go 检索集成测试和中文关键词索引重建后查询通过。同一镜像的另一隔离库中，全部 141 个迁移 SQL 文件通过。

**已验证（本机旧开发库，2026-10-15）：**先停止开发库，保留原始 PG16 目录和带 SHA-256（文件内容校验值）的离线备份，在副本上复用 PGV 的导出、恢复逻辑迁移到 PG17。初始化仅检查 WeKnora 并维护关键词索引，保留原有角色权限；未执行 VOS 的其他应用建库、授权流程。3 个数据库（`WeKnora`、`paradedb`、`postgres`）的归属、95 张 `public` 表共 19,052 行数据的行数与逐行内容校验值全部一致后，才切换开发库目录。原始 PG16 目录、离线备份和逻辑导出均保留。

切换后 `start` 成功，PostgreSQL 为 `healthy`，`schema_migrations` 为 `version=141 / dirty=false`；TCP 认证、管理员权限、公共表索引有效性、关键词查询和向量距离查询通过。主版本迁移仍须手工执行，本脚本不自动迁移其他 PG16 开发库。PG17 接受新写入后，回退旧目录前须另行保留并处理这些新数据。

**待验证：**新镜像上的空库全量应用迁移、本地源码环境的 ARM 运行和完整上传、聊天流程。VOS ARM 环境的分步升级结果另见 [ARM VOS 顺序升级验证](../../README.md#arm-vos-顺序升级验证)。

## 常用命令

```bash
DEV=./ictrek.app/docs/local-dev/ictrek-dev.sh

$DEV setup                         # 创建/更新 .env
$DEV start                         # 启动 postgres/redis/docreader/neo4j
$DEV start --no-neo4j             # 不启动 Neo4j；同时需关闭 NEO4J_ENABLE
$DEV status                        # 查看容器状态
$DEV logs docreader                # 查看 DocReader 日志
$DEV stop                          # 停止开发基础设施
$DEV restart                       # 重启开发基础设施
$DEV start-model-hub-proxy         # 创建/复用 VOS Model Hub 独立代理
$DEV stop-model-hub-proxy          # 停止 VOS Model Hub 独立代理
$DEV restart-model-hub-proxy       # 重启 VOS Model Hub 独立代理
$DEV stop-vllm                     # 停止 QA vLLM，保留容器
$DEV start-vllm                    # 启动或复用参数一致的 QA vLLM
$DEV restart-vllm                  # 按当前参数重建 QA vLLM
$DEV start-rerank                  # 启动或复用 bge-reranker-v2-m3
$DEV stop-rerank                   # 停止 ReRank vLLM，保留容器
$DEV restart-rerank                # 按当前参数重建 ReRank vLLM
$DEV check                         # 检查配置、端口和模型 endpoint
```

后端调试可以安装 Air：

```bash
go install github.com/air-verse/air@latest
```

仓库根目录已有 `.air.toml`。如果 Air 不在 `PATH`，脚本会回退到普通 `go run`。

## 关键配置

`setup` 会更新根目录被 Git 忽略的 `.env`。常用覆盖项如下：

| 配置 | 默认值 | 用途 |
| --- | --- | --- |
| `ICTREK_DEV_PROFILE` | `auto` | `auto`、`scheduler`、`tc232`、`jetson-model-hub` 或 `ollama`；`setup` 会写入实际解析后的 profile。 |
| `ICTREK_DEV_SCHEDULER_URL` | `http://127.0.0.1:22888` | 宿主机可达的统一网关；支持传入 `/v1`。 |
| `ICTREK_DEV_SCHEDULER_API_KEY` | 空 | 显式开发推理密钥；按网关实际认证要求配置。 |
| `ICTREK_DEV_SCHEDULER_BASE_URL` | 根据网关地址生成 | 模型行使用的 `/v1` 地址，由脚本维护。 |
| `ICTREK_DEV_HOST_ARCH` | `uname -m` | 测试或特殊环境下覆盖架构检测；一般不要设置。 |
| `ICTREK_DEV_HOST_PLATFORM` | `auto` | 设为 `jetson` 或 `generic` 可覆盖 Jetson 环境识别；一般不要设置。 |
| `ICTREK_DEV_MODEL_BACKEND` | 按 profile | `scheduler`、`vllm`、`ollama` 或 `model-hub`，由脚本维护。 |
| `ICTREK_DEV_MODEL_HUB_NETWORK` | `vos_default` | Model Hub 上游和独立代理所在的 Docker 网络。 |
| `ICTREK_DEV_MODEL_HUB_PROXY_NAME` | `weknora-ictrek-local-dev-model-hub-gateway` | 独立代理容器名；同名容器配置不一致时脚本不会自动删除。 |
| `ICTREK_DEV_MODEL_HUB_PROXY_IMAGE` | `weknora-ictrek-model-hub-proxy:dev` | 代理镜像；源码和 Dockerfile 位于 `local-dev/model-hub-proxy/`。 |
| `ICTREK_DEV_MODEL_HUB_QA_HOST_PORT` | `31535` | 宿主机 QA/VLM Ollama 代理端口，映射到代理 `11535`。 |
| `ICTREK_DEV_MODEL_HUB_EMBEDDING_HOST_PORT` | `31536` | 宿主机 Embedding 代理端口，映射到代理 `11536`。 |
| `ICTREK_DEV_MODEL_HUB_RERANK_HOST_PORT` | `31537` | 宿主机 ReRank 代理端口，映射到代理 `11537`。 |
| `ICTREK_DEV_MODEL_HUB_QA_UPSTREAM` | `model-hub-ollama-qa:11535` | QA Gateway 上游；必须使用 VOS 网络 alias。 |
| `ICTREK_DEV_MODEL_HUB_EMBEDDING_UPSTREAM` | `model-hub-ollama-embedding:11535` | Embedding Gateway 上游；必须使用 VOS 网络 alias。 |
| `ICTREK_DEV_MODEL_HUB_RERANK_UPSTREAM` | `model-hub-ollama-rerank:11434` | ictrek Ollama 原生 ReRank 上游；与 VOS 包一致。 |
| `ICTREK_DEV_MODEL_HUB_QA_BASE_URL` | `http://127.0.0.1:31535/v1` | 由 profile 根据 QA host port 写入；不要改为 `11434` 或 bridge IP。 |
| `ICTREK_DEV_MODEL_HUB_EMBEDDING_BASE_URL` | `http://127.0.0.1:31536/v1` | 由 profile 根据 Embedding host port 写入；模型行使用 remote/OpenAI-compatible。 |
| `ICTREK_DEV_MODEL_HUB_RERANK_BASE_URL` | `http://127.0.0.1:31537/v1` | ReRank 模型行地址；远程开发可通过 SSH 隧道或显式覆盖连接。修改代理端口时须同步此值。 |
| `ICTREK_DEV_MODEL_HUB_QA_MODEL_NAME` | `qwen3.5:2b` | Model Hub QA/VLM 模型名。 |
| `ICTREK_DEV_MODEL_HUB_EMBEDDING_MODEL_NAME` | `bge-m3:latest` | Model Hub Embedding 模型名。 |
| `ICTREK_DEV_MODEL_HUB_RERANK_MODEL_NAME` | `qllama/bge-reranker-v2-m3:q8_0` | Model Hub ReRank 模型名。 |
| `ICTREK_DEV_DATA_DIR` | `/data/hybrag-dev-data` 或项目目录回退 | 基础设施持久化根目录；`/data` 不存在或不可写时自动使用 `.local-dev-data`，也可手工改成其他路径。 |
| `ICTREK_DEV_DB_PORT` | `15432` | 宿主机 PostgreSQL 端口。 |
| `ICTREK_DEV_PGV_IMAGE` | 按主机架构选择 `pgv:amd_17_20261015` 或 `pgv:arm_17_20261015` | 与 VOS PGV `0.0.49` 发布包一致，完整镜像地址使用 `swr.cn-southwest-2.myhuaweicloud.com/ictrek/pgv`；覆盖镜像须匹配主机架构、预加载扩展和数据目录主版本。 |
| `ICTREK_DEV_REDIS_PORT` | `6380` | 宿主机 Redis 端口。 |
| `ICTREK_DEV_DOCREADER_PORT` | `15051` | 宿主机 DocReader 端口。 |
| `ICTREK_DEV_NEO4J_BOLT_PORT` | `27687` | 宿主机 Neo4j Bolt 端口。 |
| `ICTREK_DEV_VLLM_BASE_URL` | tc232 profile 为 `http://127.0.0.1:38118/v1` | QA/VLM OpenAI-compatible endpoint；Ollama profile 会清空此类 vLLM 配置。 |
| `ICTREK_DEV_VLLM_CONTAINER` | `qwen35-9b-awq-vllm` | QA vLLM 容器名。 |
| `ICTREK_DEV_VLLM_MODEL_DIR` | `/data/models/QuantTrio--Qwen3.5-9B-AWQ` | 宿主机模型目录。 |
| `ICTREK_DEV_VLLM_NETWORK` | `lexai` | Docker 网络；启动前必须已存在。 |
| `ICTREK_DEV_VLLM_MAX_MODEL_LEN` | `65536` | vLLM `--max-model-len`。 |
| `ICTREK_DEV_VLLM_MAX_NUM_SEQS` | `20` | vLLM `--max-num-seqs`。 |
| `ICTREK_DEV_VLLM_MAX_NUM_BATCHED_TOKENS` | `4096` | vLLM `--max-num-batched-tokens`。 |
| `ICTREK_DEV_VLLM_GPU_MEMORY_UTILIZATION` | `0.3` | vLLM `--gpu-memory-utilization`。 |
| `ICTREK_DEV_VLLM_SHM_SIZE` | `8g` | Docker `--shm-size`。 |
| `ICTREK_DEV_VLLM_SECURITY_OPT` | `label=disable` | Docker `--security-opt`。 |
| `ICTREK_DEV_BGE_VLLM_BASE_URL` | tc232 profile 为 `http://127.0.0.1:32223/v1` | embedding endpoint；Ollama profile 使用 `OLLAMA_BASE_URL`。 |
| `ICTREK_DEV_RERANK_VLLM_PORT` | `32224` | ReRank vLLM 宿主机端口；必须不同于 QA `38118` 和 embedding `32223`。 |
| `ICTREK_DEV_RERANK_VLLM_BASE_URL` | `http://127.0.0.1:32224` | ReRank base URL；WeKnora 会追加 `/rerank`，不要追加 `/v1`。 |
| `ICTREK_DEV_RERANK_VLLM_CONTAINER` | `bge-reranker-v2-m3-vllm` | ReRank vLLM 容器名。 |
| `ICTREK_DEV_RERANK_VLLM_MODEL_DIR` | `/data/models/bge-reranker-v2-m3` | 宿主机 ReRank 模型目录。 |
| `ICTREK_DEV_RERANK_VLLM_MODEL_NAME` | `bge-reranker-v2-m3` | vLLM 对外暴露的模型名。 |
| `ICTREK_DEV_RERANK_VLLM_MAX_MODEL_LEN` | `8192` | vLLM `--max-model-len`。 |
| `ICTREK_DEV_RERANK_VLLM_MAX_NUM_SEQS` | `16` | vLLM `--max-num-seqs`。 |
| `ICTREK_DEV_RERANK_VLLM_MAX_NUM_BATCHED_TOKENS` | `8192` | vLLM `--max-num-batched-tokens`；不能小于 `max_model_len`。 |
| `ICTREK_DEV_RERANK_VLLM_GPU_MEMORY_UTILIZATION` | `0.1` | vLLM `--gpu-memory-utilization`。 |
| `ICTREK_DEV_OLLAMA_BASE_URL` | Ollama profile 为 `http://127.0.0.1:11434`；Model Hub profile 为 `http://127.0.0.1:31535` | Ollama 兼容/管理接口；Model Hub profile 必须走独立代理。tc232 可显式设为 `http://127.0.0.1:31535`，供非默认 Ollama fallback 模型复用已运行的代理。 |
| `ICTREK_DEV_CHAT_MODEL_CONTEXT_TOKENS` | Ollama `24000`，Model Hub `24000`，tc232 `65536` | profile 默认的应用侧上下文预算。 |
| `ICTREK_DEV_MAIN_QA_MODEL_CONCURRENCY` | Ollama `4`，Model Hub `3`，tc232 `20` | profile 默认的主 QA 并发。 |
| `ICTREK_DEV_MODEL_MAX_CONCURRENCY` | Ollama `2`，Model Hub `1`，tc232 `6` | 后台模型总并发上限。 |
| `ICTREK_DEV_BATCH_EMBED_SIZE` | Ollama `4`，Model Hub `4`，tc232 `4` | embedding 单批大小。 |
| `ICTREK_DEV_CONCURRENCY_POOL_SIZE` | Ollama `2`，Model Hub `1`，tc232 `4` | 应用侧 embedding 并发池。 |
| `BUILTIN_MODELS_CONFIG` | 按 profile | 声明式内置模型配置；由 `setup` 自动写入。 |

如果端口被占用，可以在 `.env` 中修改对应的 `ICTREK_DEV_*_PORT`，然后重新执行 `setup` 或直接运行 `start`。后端会读取脚本计算出的同一组端口；模型端口则只需要修改 URL。

## 与 VOS 部署的边界

本地源码调试不使用 `ictrek.app/src/docker-compose.yml`，也不使用 `VOS_APP_STORAGE_PATH`。VOS app 的 Model Hub、PGV、Traefik、iframe 路由和 OIDC Fastpath 只在 VOS 安装环境验证；本地调试使用普通登录和本地端口。

如果要验证 VOS 安装包，请回到 `ictrek.app/README.md` 的打包/安装流程。不要在 VOS app 的真实部署目录执行本地 `stop`，也不要把本地开发数据目录复制到 VOS 应用存储目录。

## 排错

- 切换后仍访问旧模型端口：完全停止并重启 `$DEV app`/Air。只刷新浏览器或触发 Air 编译不会更新父进程已加载的 `.env`。用户编辑过的模型需在页面更新。
- V-Scheduler 目录可达但推理失败：确认完整模型名已发布，并查看网关的认证、资源准入和加载错误。

- 后端连接不上数据库：先执行 `$DEV status`，确认 PostgreSQL 为 `healthy`，再检查 `.env` 中 `DB_PASSWORD` 是否在数据库首次初始化后被改过。
- Jetson 仍显示 tc232：重新执行 `$DEV setup --profile jetson-model-hub`，然后完全重启当前 `$DEV app`/Air 进程；只刷新浏览器不会重新加载 `.env`。
- Jetson Model Hub profile 找不到模型：先确认代理镜像已导入、`docker network inspect vos_default` 正常、`docker ps --filter name=model-hub-ollama` 能看到 QA 和 Embedding 容器，再执行 `$DEV start-model-hub-proxy`；不要把容器内的 `model-hub-ollama-qa` 主机名直接填给宿主机运行的 Go 进程。
- 文档上传失败：查看 `$DEV logs docreader`；源码后端必须使用 `DOCREADER_ADDR=127.0.0.1:15051` 和 `DOCREADER_TRANSPORT=grpc`。
- 默认模型不可用：检查对应 `/v1/models`，并确认 YAML 中的模型名与服务返回的 `id` 一致。
- vLLM ReRank 不可用：先执行 `$DEV start-rerank`，检查 `http://127.0.0.1:32224/health` 和 `/rerank`；如果改过端口，保持 YAML 的 `Base URL` 与 `ICTREK_DEV_RERANK_VLLM_PORT` 一致。
- Model Hub ReRank 名称显示 `${...}`：更新后完全重启后端再刷新页面，默认值及修正边界见 [代理说明](model-hub-ollama-proxy.md#配置本地-weknora)。
- Model Hub ReRank 不可用：按 [代理说明](model-hub-ollama-proxy.md#端到端验证) 测试 `31537/v1/rerank`；仅出现模型行不代表服务在线，本地模式不会自动下载模型。
- 模型名或 Base URL 显示为 `${ICTREK_DEV_RERANK_VLLM_*}`：说明后端启动时没有加载 `.env`；仅刷新页面无效，停止当前 `make dev-app`/`air` 后重新执行 `$DEV setup` 和 `$DEV app`。
- SSRF 校验拒绝本地模型：保留 `SSRF_WHITELIST=localhost,127.0.0.1,::1`；Model Hub 代理使用 loopback，不需要把容器 bridge IP 加入白名单。
- 管理员入口仍不可用：先用 `admin@weknora.local` 注册并完全登录一次，再重启 `$DEV app`；启动日志中应出现 bootstrap 提权结果。
