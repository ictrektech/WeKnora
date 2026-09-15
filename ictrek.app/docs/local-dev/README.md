# WeKnora ictrek 本地快速调试

这套方式用于在源码目录直接调试 WeKnora 的 Go 后端和 Vue/Vite 前端。后端、前端在宿主机运行，Docker 只启动 PostgreSQL、Redis、DocReader、Neo4j 等基础设施；它不参与 VOS app 打包，也不复用 VOS app 的 compose 和数据目录。

## 当前状态

| 能力 | 状态 | 说明 |
| --- | --- | --- |
| Go 后端源码调试 | 已实现 | 支持 `go run`；安装 Air 后自动热重载。 |
| 前端源码调试 | 已实现 | Vite 热更新，默认代理到 `127.0.0.1:8080`。 |
| 基础设施 | 已实现 | 使用本目录的隔离 Compose 和 `/data/hybrag-dev-data`。 |
| 模型初始化 | 部分落地 | 默认提供 tc232 vLLM YAML；QA 和 ReRank 可由脚本启动，bge-m3 embedding 仍由外部服务提供；也提供宿主机 Ollama YAML。 |
| VOS iframe 免登录 | 未启用 | 本地源码模式使用普通注册/登录，`HYBRAG_VOS_SSO_ENABLED=false`。 |
| VOS 发布部署 | 不在本流程 | 发布和安装仍以 `ictrek.app/README.md`、`src/` 和 `package.sh` 为准。 |

## 快速启动

在仓库根目录执行：

```bash
./ictrek.app/docs/local-dev/ictrek-dev.sh setup
./ictrek.app/docs/local-dev/ictrek-dev.sh start
```

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

默认配置文件是 `ictrek.app/docs/local-dev/config/builtin_models.tc232.yaml`。它把 QA/VLM 指向 `http://127.0.0.1:38118/v1`，把 bge-m3 embedding 指向 `http://127.0.0.1:32223/v1`，把 `bge-reranker-v2-m3` ReRank 指向 `http://127.0.0.1:32224`。这些地址都必须由 vLLM 或其他 OpenAI-compatible 服务提供；本目录的基础设施 Compose 不会自动启动 bge-m3 或模型服务。

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

如果使用宿主机 Ollama，不需要启动 vLLM，切换到 Ollama 配置：

```bash
./ictrek.app/docs/local-dev/ictrek-dev.sh setup \
  --model-config ictrek.app/docs/local-dev/config/builtin_models.ollama.yaml
ollama pull qwen3.5:2b
ollama pull bge-m3
```

也可以只在 Web UI 中添加模型。YAML 声明的行由 `managed_by=yaml` 管理，切换 YAML 时可能软删除旧的 YAML 托管行；因此不要把这套配置指向 VOS 生产数据或其他需要保留的部署数据库。

检查模型服务：

```bash
./ictrek.app/docs/local-dev/ictrek-dev.sh check
curl -fsS http://127.0.0.1:38118/v1/models
curl -fsS http://127.0.0.1:32223/v1/models
curl -fsS http://127.0.0.1:11434/api/tags
```

只使用 vLLM 时，Ollama 检查失败是正常的；只使用 Ollama 时，vLLM 检查失败是正常的；尚未启动可选 ReRank 服务时，ReRank 检查失败也是正常的。

## 地址与数据

| 服务 | 地址 | 数据 |
| --- | --- | --- |
| 前端 Vite | `http://localhost:5173` | — |
| Go API | `http://localhost:8080` | — |
| PostgreSQL | `127.0.0.1:15432` | `/data/hybrag-dev-data/postgres` |
| Redis | `127.0.0.1:6380` | `/data/hybrag-dev-data/redis` |
| DocReader | `127.0.0.1:15051` | `/data/hybrag-dev-data/docreader` |
| Neo4j Bolt | `bolt://127.0.0.1:27687` | `/data/hybrag-dev-data/neo4j` |
| Neo4j Browser | `http://127.0.0.1:27474` | — |
| QA vLLM | `http://127.0.0.1:38118/v1` | 外部模型目录 |
| bge-m3 vLLM | `http://127.0.0.1:32223/v1` | 外部模型目录 |
| ReRank vLLM | `http://127.0.0.1:32224` | `/data/models/bge-reranker-v2-m3` |

`stop` 只移除开发容器和网络，不删除 `/data/hybrag-dev-data`。如需清空本地数据库，请先确认目标路径，再单独备份或删除该目录。

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
| `ICTREK_DEV_DATA_DIR` | `/data/hybrag-dev-data` | 基础设施持久化根目录；可在 `.env` 中改为其他绝对路径或项目相对路径。 |
| `ICTREK_DEV_DB_PORT` | `15432` | 宿主机 PostgreSQL 端口。 |
| `ICTREK_DEV_REDIS_PORT` | `6380` | 宿主机 Redis 端口。 |
| `ICTREK_DEV_DOCREADER_PORT` | `15051` | 宿主机 DocReader 端口。 |
| `ICTREK_DEV_NEO4J_BOLT_PORT` | `27687` | 宿主机 Neo4j Bolt 端口。 |
| `ICTREK_DEV_VLLM_BASE_URL` | `http://127.0.0.1:38118/v1` | QA/VLM OpenAI-compatible endpoint。 |
| `ICTREK_DEV_VLLM_CONTAINER` | `qwen35-9b-awq-vllm` | QA vLLM 容器名。 |
| `ICTREK_DEV_VLLM_MODEL_DIR` | `/data/models/QuantTrio--Qwen3.5-9B-AWQ` | 宿主机模型目录。 |
| `ICTREK_DEV_VLLM_NETWORK` | `lexai` | Docker 网络；启动前必须已存在。 |
| `ICTREK_DEV_VLLM_MAX_MODEL_LEN` | `65536` | vLLM `--max-model-len`。 |
| `ICTREK_DEV_VLLM_MAX_NUM_SEQS` | `20` | vLLM `--max-num-seqs`。 |
| `ICTREK_DEV_VLLM_MAX_NUM_BATCHED_TOKENS` | `4096` | vLLM `--max-num-batched-tokens`。 |
| `ICTREK_DEV_VLLM_GPU_MEMORY_UTILIZATION` | `0.3` | vLLM `--gpu-memory-utilization`。 |
| `ICTREK_DEV_VLLM_SHM_SIZE` | `8g` | Docker `--shm-size`。 |
| `ICTREK_DEV_VLLM_SECURITY_OPT` | `label=disable` | Docker `--security-opt`。 |
| `ICTREK_DEV_BGE_VLLM_BASE_URL` | `http://127.0.0.1:32223/v1` | embedding endpoint。 |
| `ICTREK_DEV_RERANK_VLLM_PORT` | `32224` | ReRank vLLM 宿主机端口；必须不同于 QA `38118` 和 embedding `32223`。 |
| `ICTREK_DEV_RERANK_VLLM_BASE_URL` | `http://127.0.0.1:32224` | ReRank base URL；WeKnora 会追加 `/rerank`，不要追加 `/v1`。 |
| `ICTREK_DEV_RERANK_VLLM_CONTAINER` | `bge-reranker-v2-m3-vllm` | ReRank vLLM 容器名。 |
| `ICTREK_DEV_RERANK_VLLM_MODEL_DIR` | `/data/models/bge-reranker-v2-m3` | 宿主机 ReRank 模型目录。 |
| `ICTREK_DEV_RERANK_VLLM_MODEL_NAME` | `bge-reranker-v2-m3` | vLLM 对外暴露的模型名。 |
| `ICTREK_DEV_RERANK_VLLM_MAX_MODEL_LEN` | `8192` | vLLM `--max-model-len`。 |
| `ICTREK_DEV_RERANK_VLLM_MAX_NUM_SEQS` | `16` | vLLM `--max-num-seqs`。 |
| `ICTREK_DEV_RERANK_VLLM_MAX_NUM_BATCHED_TOKENS` | `8192` | vLLM `--max-num-batched-tokens`；不能小于 `max_model_len`。 |
| `ICTREK_DEV_RERANK_VLLM_GPU_MEMORY_UTILIZATION` | `0.1` | vLLM `--gpu-memory-utilization`。 |
| `ICTREK_DEV_OLLAMA_BASE_URL` | `http://127.0.0.1:11434` | Ollama 原生 API。 |
| `BUILTIN_MODELS_CONFIG` | tc232 YAML | 声明式内置模型配置。 |

如果端口被占用，可以在 `.env` 中修改对应的 `ICTREK_DEV_*_PORT`，然后重新执行 `setup` 或直接运行 `start`。后端会读取脚本计算出的同一组端口；模型端口则只需要修改 URL。

## 与 VOS 部署的边界

本地源码调试不使用 `ictrek.app/src/docker-compose.yml`，也不使用 `VOS_APP_STORAGE_PATH`。VOS app 的 Model Hub、PGV、Traefik、iframe 路由和 OIDC Fastpath 只在 VOS 安装环境验证；本地调试使用普通登录和本地端口。

如果要验证 VOS 安装包，请回到 `ictrek.app/README.md` 的打包/安装流程。不要在 VOS app 的真实部署目录执行本地 `stop`，也不要把本地开发数据目录复制到 VOS 应用存储目录。

## 排错

- 后端连接不上数据库：先执行 `$DEV status`，确认 PostgreSQL 为 `healthy`，再检查 `.env` 中 `DB_PASSWORD` 是否在数据库首次初始化后被改过。
- 文档上传失败：查看 `$DEV logs docreader`；源码后端必须使用 `DOCREADER_ADDR=127.0.0.1:15051` 和 `DOCREADER_TRANSPORT=grpc`。
- 默认模型不可用：检查对应 `/v1/models`，并确认 YAML 中的模型名与服务返回的 `id` 一致。
- ReRank 不可用：先执行 `$DEV start-rerank`，检查 `http://127.0.0.1:32224/health` 和 `/rerank`；如果改过端口，保持 YAML 的 `Base URL` 与 `ICTREK_DEV_RERANK_VLLM_PORT` 一致。
- 模型名或 Base URL 显示为 `${ICTREK_DEV_RERANK_VLLM_*}`：说明后端启动时没有加载 `.env`；仅刷新页面无效，停止当前 `make dev-app`/`air` 后重新执行 `$DEV setup` 和 `$DEV app`。
- SSRF 校验拒绝本地模型：保留 `SSRF_WHITELIST=localhost,127.0.0.1,::1`；不要为了绕过校验关闭生产环境 SSRF 防护。
- 管理员入口仍不可用：先用 `admin@weknora.local` 注册并完全登录一次，再重启 `$DEV app`；启动日志中应出现 bootstrap 提权结果。
