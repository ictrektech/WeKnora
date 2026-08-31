# WeKnora ictrek 本地快速调试

这套方式用于在源码目录直接调试 WeKnora 的 Go 后端和 Vue/Vite 前端。后端、前端在宿主机运行，Docker 只启动 PostgreSQL、Redis、DocReader、Neo4j 等基础设施；它不参与 VOS app 打包，也不复用 VOS app 的 compose 和数据目录。

## 当前状态

| 能力 | 状态 | 说明 |
| --- | --- | --- |
| Go 后端源码调试 | 已实现 | 支持 `go run`；安装 Air 后自动热重载。 |
| 前端源码调试 | 已实现 | Vite 热更新，默认代理到 `127.0.0.1:8080`。 |
| 基础设施 | 已实现 | 使用本目录的隔离 Compose 和 `.local-data/ictrek`。 |
| 模型初始化 | 部分落地 | 默认提供 tc232 vLLM YAML；也提供宿主机 Ollama YAML。 |
| VOS iframe 免登录 | 未启用 | 本地源码模式使用普通注册/登录，`HYBRAG_VOS_SSO_ENABLED=false`。 |
| VOS 发布部署 | 不在本流程 | 发布和安装仍以 `ictrek.app/README.md`、`src/` 和 `package.sh` 为准。 |

## 快速启动

在仓库根目录执行：

```bash
./ictrek.app/docs/local-dev/ictrek-dev.sh setup
./ictrek.app/docs/local-dev/ictrek-dev.sh start
```

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

默认配置文件是 `ictrek.app/docs/local-dev/config/builtin_models.tc232.yaml`。它把 QA/VLM 指向 `http://127.0.0.1:38118/v1`，把 bge-m3 embedding 指向 `http://127.0.0.1:32223/v1`。两个地址都必须由 vLLM 或其他 OpenAI-compatible 服务提供；本目录的基础设施 Compose 不会自动启动 bge-m3。

如果 tc232 上已有 QA 模型目录，可以启动可选的 QA vLLM：

```bash
ICTREK_DEV_VLLM_MODEL_DIR=/path/to/Qwen-model \
  ./ictrek.app/docs/local-dev/ictrek-dev.sh start-vllm
```

脚本默认模型目录是 `/data/jhu/models/hf/QuantTrio--Qwen3.5-9B-AWQ`，也支持直接指向 Hugging Face 的 `snapshots/<revision>/` 父目录。已有同名容器会复用创建时的参数；修改 vLLM 参数前先手工删除该开发容器：

```bash
docker rm -f weknora-ictrek-dev-vllm
```

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

只使用 vLLM 时，Ollama 检查失败是正常的；只使用 Ollama 时，vLLM 检查失败是正常的。

## 地址与数据

| 服务 | 地址 | 数据 |
| --- | --- | --- |
| 前端 Vite | `http://localhost:5173` | — |
| Go API | `http://localhost:8080` | — |
| PostgreSQL | `127.0.0.1:15432` | `.local-data/ictrek/postgres` |
| Redis | `127.0.0.1:6380` | `.local-data/ictrek/redis` |
| DocReader | `127.0.0.1:15051` | `.local-data/ictrek/docreader` |
| Neo4j Bolt | `bolt://127.0.0.1:27687` | `.local-data/ictrek/neo4j` |
| Neo4j Browser | `http://127.0.0.1:27474` | — |
| QA vLLM | `http://127.0.0.1:38118/v1` | 外部模型目录 |
| bge-m3 vLLM | `http://127.0.0.1:32223/v1` | 外部模型目录 |

`stop` 只移除开发容器和网络，不删除 `.local-data/ictrek`。如需清空本地数据库，请先确认目标路径，再单独备份或删除该目录。

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
| `ICTREK_DEV_DATA_DIR` | `.local-data/ictrek` | 基础设施持久化根目录。 |
| `ICTREK_DEV_DB_PORT` | `15432` | 宿主机 PostgreSQL 端口。 |
| `ICTREK_DEV_REDIS_PORT` | `6380` | 宿主机 Redis 端口。 |
| `ICTREK_DEV_DOCREADER_PORT` | `15051` | 宿主机 DocReader 端口。 |
| `ICTREK_DEV_NEO4J_BOLT_PORT` | `27687` | 宿主机 Neo4j Bolt 端口。 |
| `ICTREK_DEV_VLLM_BASE_URL` | `http://127.0.0.1:38118/v1` | QA/VLM OpenAI-compatible endpoint。 |
| `ICTREK_DEV_BGE_VLLM_BASE_URL` | `http://127.0.0.1:32223/v1` | embedding endpoint。 |
| `ICTREK_DEV_OLLAMA_BASE_URL` | `http://127.0.0.1:11434` | Ollama 原生 API。 |
| `BUILTIN_MODELS_CONFIG` | tc232 YAML | 声明式内置模型配置。 |

如果端口被占用，可以在 `.env` 中修改对应的 `ICTREK_DEV_*_PORT`，然后重新执行 `setup` 或直接运行 `start`。后端会读取脚本计算出的同一组端口；模型端口则只需要修改 URL。

## 与 VOS 部署的边界

本地源码调试不使用 `ictrek.app/src/docker-compose.yml`，也不使用 `VOS_APP_STORAGE_PATH`。VOS app 的 Model Hub、PGV、Traefik、iframe 路由和 OIDC Fastpath 只在 VOS 安装环境验证；本地调试使用普通登录和本地端口。

如果要验证 VOS 安装包，请回到 `ictrek.app/README.md` 的打包/安装流程。不要在 VOS app 的真实部署目录执行本地 `stop`，也不要把 `.local-data/ictrek` 复制到 VOS 应用存储目录。

## 排错

- 后端连接不上数据库：先执行 `$DEV status`，确认 PostgreSQL 为 `healthy`，再检查 `.env` 中 `DB_PASSWORD` 是否在数据库首次初始化后被改过。
- 文档上传失败：查看 `$DEV logs docreader`；源码后端必须使用 `DOCREADER_ADDR=127.0.0.1:15051` 和 `DOCREADER_TRANSPORT=grpc`。
- 默认模型不可用：检查对应 `/v1/models`，并确认 YAML 中的模型名与服务返回的 `id` 一致。
- SSRF 校验拒绝本地模型：保留 `SSRF_WHITELIST=localhost,127.0.0.1,::1`；不要为了绕过校验关闭生产环境 SSRF 防护。
- 管理员入口仍不可用：先用 `admin@weknora.local` 注册并完全登录一次，再重启 `$DEV app`；启动日志中应出现 bootstrap 提权结果。
