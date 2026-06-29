# 空机器 WeKnora 部署总指南

这是一份从空机器启动 WeKnora 的运维检查清单。中文说明在上方，英文原文在下方。更细的镜像构建、模型后端、Neo4j、上游同步等内容分别引用本目录中的专题文档。

`tc232` 这类名字只是操作员本机的 SSH config alias，不是服务 hostname。外部访问需要单独做公网端口映射、反向代理、VPN 或隧道。

## 目标服务

最小可用 WeKnora 部署包含：

```text
frontend   Web UI
app        WeKnora API 和 worker
docreader  文档解析服务
postgres   元数据；RETRIEVE_DRIVER=postgres 时也存向量数据
redis      stream/task 状态
```

基础 RAG 部署使用非 lite 栈，并保持：

```env
RETRIEVE_DRIVER=postgres
STORAGE_TYPE=local
STREAM_MANAGER_TYPE=redis
LOCAL_STORAGE_BASE_DIR=/data/files
```

可选组件按功能开启：

```text
neo4j      实体关系 GraphRAG
minio      用对象存储替代本地文件
qdrant     外部向量库
milvus     外部向量库
weaviate   外部向量库
searxng    自建网络搜索
langfuse   模型调用观测
```

## 机器准备

先确认 Docker 和 Compose：

```bash
docker version
docker compose version
```

如果本机要运行 GPU 模型后端，先验证 GPU runtime：

```bash
nvidia-smi
docker run --rm --gpus all nvidia/cuda:12.4.1-base-ubuntu22.04 nvidia-smi
```

Jetson/L4T 主机要使用对应平台的 NVIDIA container runtime 测试镜像。WeKnora 本身不依赖 GPU，但本地 vLLM/Ollama 模型后端要用 GPU 加速时，必须先解决 runtime。

创建部署目录和持久化目录：

```bash
mkdir -p /data/jhu/deploy/weknora
cd /data/jhu/deploy/weknora
mkdir -p data/files data/postgres data/redis config
```

`/data/jhu` 不是强制路径，可以换成本机实际数据盘。

## 选择 WeKnora 镜像

如果飞书发布表里已有镜像，优先使用已有镜像：

- 在目标平台 sheet 中找 `weknora`、`weknora-ui`、`weknora-docreader`；
- 第 2 行是仓库地址，日期行是 tag；
- 组合成 `<row-2-repository>:<date-row-tag>`；
- 按 [build-images.md](build-images.md#start-from-existing-images) 生成 `docker-compose.images.yml`。

发布镜像不包含部署专用模型行。模型后续在 Web UI 添加，或者由运维人员显式挂载 `config/builtin_models.yaml`。

如果目标平台没有可用镜像，先按 [build-images.md](build-images.md) 构建并推送。WeKnora app/frontend/docreader 镜像本身没有 CUDA 依赖，tag 不应带 CUDA 标记。

## 准备模型后端

至少需要：

```text
KnowledgeQA  聊天/问答模型
Embedding    向量模型
```

需要图片/文档图片描述时，还要：

```text
VLLM         视觉模型
```

Rerank 可选。只有真实 `/v1/rerank` 或等价 rerank endpoint 可用时才配置。

### 方案 A：Ollama 为主

一个 Ollama 服务同时提供聊天、图片理解、embedding。具体容器启动和常驻模型方式见 [model-hub-ollama-embedding.md](model-hub-ollama-embedding.md)。

WeKnora 启动前先准备模型：

```bash
ollama pull qwen3.5:2b
ollama pull bge-m3
```

WeKnora 模型行建议：

```text
KnowledgeQA  source=local  name=qwen3.5:2b
VLLM         source=local  name=qwen3.5:2b
Embedding    source=local  name=bge-m3:latest  dimension=1024
```

`source=local` 的 Ollama 行不要填 `base_url` 和 `api_key`，WeKnora 会统一使用 `.env` 里的 `OLLAMA_BASE_URL`。

原生 Ollama 不提供通用 rerank API。Ollama 主方案如果也要 rerank，需要另准备 rerank 模型或 sidecar，并通过 gateway 暴露 `/v1/rerank`；否则不要配置 rerank。

### 方案 B：vLLM 做聊天/VLM，Ollama 做 embedding

聊天和 VLM 走 OpenAI-compatible vLLM endpoint，embedding 走 Ollama。参考：

- [remote-vllm-backend.md](remote-vllm-backend.md)
- [model-hub-ollama-embedding.md](model-hub-ollama-embedding.md)

模型行形态：

```text
KnowledgeQA  source=remote  name=<vllm-served-model>  base_url=http://host.docker.internal:<vllm-port>/v1
VLLM         source=remote  name=<vllm-served-model>  base_url=http://host.docker.internal:<vllm-port>/v1
Embedding    source=local 或 remote，取决于使用 Ollama 原生 API 还是 OpenAI-compatible embedding gateway
```

如果 app 容器通过宿主机映射端口访问模型服务，`SSRF_WHITELIST_EXTRA` 中要保留 `host.docker.internal`。

## `.env` 要改什么

从模板开始：

```bash
cp .env.example .env
```

基础项：

```env
GIN_MODE=release
TZ=Asia/Shanghai
WEKNORA_LANGUAGE=zh-CN

DB_USER=postgres
DB_PASSWORD=<strong-password>
DB_NAME=WeKnora

REDIS_PASSWORD=<strong-password>
REDIS_DB=0
REDIS_PREFIX=stream:

RETRIEVE_DRIVER=postgres
STORAGE_TYPE=local
STREAM_MANAGER_TYPE=redis
LOCAL_STORAGE_BASE_DIR=/data/files

FRONTEND_PORT=80
APP_PORT=8080
DOCREADER_PORT=50051

JWT_SECRET=<strong-random-value>
TENANT_AES_KEY=<strong-random-value>
SYSTEM_AES_KEY=<32-byte-value>
```

Ollama 主方案：

```env
OLLAMA_BASE_URL=http://host.docker.internal:<ollama-host-port>
SSRF_WHITELIST_EXTRA=host.docker.internal,searxng,qdrant,milvus,weaviate,doris-fe
```

vLLM 通过宿主机映射端口访问时，同样保留 `host.docker.internal` 白名单，并在模型行中使用 `http://host.docker.internal:<vllm-port>/v1`。

Neo4j/GraphRAG：

```env
NEO4J_ENABLE=true
ENABLE_GRAPH_RAG=true
NEO4J_URI=bolt://neo4j:7687
NEO4J_USERNAME=neo4j
NEO4J_PASSWORD=<strong-password>
```

参考 [neo4j.env.example](neo4j.env.example)。

## 模型用 UI 还是 YAML

首次部署推荐：先启动 WeKnora，登录后在 Web UI 中添加模型行。这样不会把某台机器的模型 ID、端口、后端写死进镜像或 compose。

只有需要声明式下发模型时，才使用 `config/builtin_models.yaml`：

```bash
cp config/builtin_models.yaml.example config/builtin_models.yaml
```

然后在 `docker-compose.override.yml` 中显式挂载：

```yaml
services:
  app:
    volumes:
      - ./config/builtin_models.yaml:/app/config/builtin_models.yaml:ro
```

YAML 中常改字段：

```yaml
builtin_models:
  - id: <stable-id-used-by-kb-and-agents>
    type: KnowledgeQA | VLLM | Embedding | Rerank
    source: local | remote
    is_default: true
    name: <model-name-or-ollama-tag>
    parameters:
      base_url: <empty-for-local-ollama-or-remote-/v1-url>
      api_key: <empty-or-env-placeholder>
      provider: generic
      embedding_parameters:
        dimension: <embedding-dimension>
```

Ollama `source=local` 行保持 `base_url`、`api_key` 为空，在 `.env` 配 `OLLAMA_BASE_URL`。vLLM/OpenAI-compatible 行使用 `source=remote`，`base_url` 以 `/v1` 结尾。

生命周期规则：

```text
builtin_models.yaml 创建的行会标记为 managed_by='yaml'。
app 启动时，当前 YAML 中不存在的 YAML 托管行会被软删除。
Web UI/API/手工 SQL 创建的行应保持 managed_by=''，不会被 YAML 清理。
```

把 YAML 改成空 `builtin_models: []` 前，先确认知识库、智能体和 GraphRAG 没有引用即将被软删除的模型行。排查方式见 [remote-weknora-deployment.md](remote-weknora-deployment.md#model-row-troubleshooting)。

## docker compose 用哪个

生产或远程空机器上，优先使用发布镜像 override：

```bash
docker compose \
  -f docker-compose.yml \
  -f docker-compose.override.yml \
  -f docker-compose.images.yml \
  up -d postgres redis docreader app frontend
```

`docker-compose.override.yml` 负责本地持久化：

```text
./data/files     -> app:/data/files
./data/postgres  -> postgres:/var/lib/postgresql/data
./data/redis     -> redis:/data
```

没有 `docker-compose.images.yml` 时，Compose 会使用 `docker-compose.yml` 中的默认 image/build 配置。生产类环境应尽量使用已有镜像，避免启动时本机构建。

可选 profile 按需启动：

```bash
docker compose -f docker-compose.yml -f docker-compose.override.yml --profile neo4j up -d neo4j
docker compose -f docker-compose.yml -f docker-compose.override.yml --profile minio up -d minio
docker compose -f docker-compose.yml -f docker-compose.override.yml --profile qdrant up -d qdrant
```

如果使用 `docker-compose.images.yml`，可选 profile 命令也要带上它。

## 启动顺序

1. 启动模型后端并验证 API。
2. 按需启动 Neo4j、MinIO、外部向量库等可选服务。
3. 启动 WeKnora 依赖：

```bash
docker compose -f docker-compose.yml -f docker-compose.override.yml -f docker-compose.images.yml up -d postgres redis docreader
```

4. 启动 app 和 frontend：

```bash
docker compose -f docker-compose.yml -f docker-compose.override.yml -f docker-compose.images.yml up -d app frontend
```

5. 打开前端，注册或登录，添加模型行。

只建议对外暴露 frontend。app、docreader、数据库、Redis、Neo4j 默认应保持私有，除非明确为了调试而开放。

## 组件测试

容器和日志：

```bash
docker compose ps
docker compose logs --tail=200 app
docker compose logs --tail=100 docreader
```

App 健康：

```bash
curl -fsS http://127.0.0.1:<app-host-port>/health
```

Frontend：

```bash
curl -I http://127.0.0.1:<frontend-host-port>/
```

Postgres：

```bash
docker compose exec postgres pg_isready -U "$DB_USER"
```

Redis：

```bash
docker compose exec redis redis-cli -a "$REDIS_PASSWORD" ping
```

docreader：

```bash
docker compose exec docreader grpc_health_probe -addr=localhost:50051
```

Ollama：

```bash
curl -fsS http://127.0.0.1:<ollama-host-port>/api/tags
curl -fsS http://127.0.0.1:<ollama-host-port>/api/embed \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":["中文知识库检索测试"]}'
```

vLLM：

```bash
curl -fsS http://127.0.0.1:<vllm-host-port>/v1/models
curl -fsS http://127.0.0.1:<vllm-host-port>/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"<served-model>","messages":[{"role":"user","content":"用一句中文说明你是谁。"}],"max_tokens":128}'
```

Neo4j/APOC：

```bash
docker compose exec neo4j cypher-shell \
  -u "$NEO4J_USERNAME" -p "$NEO4J_PASSWORD" \
  'RETURN apoc.version();'
```

模型行：

```bash
docker compose exec postgres psql -U "$DB_USER" -d "$DB_NAME" -c "
select id,type,source,name,is_default,is_builtin,managed_by,deleted_at
from models
order by type,id;"
```

## 功能测试

在 Web UI 中完成：

1. 添加或选择默认 KnowledgeQA、Embedding，以及可选 VLLM 模型。
2. 创建知识库。
3. 上传小 TXT/PDF，确认解析成功。
4. 对上传内容提问，确认问答能命中文档。
5. 配置 VLM 时，上传或解析带图片的文档，确认图片描述不报错。
6. 开启 GraphRAG 时，用短文本做实体关系抽取，确认能返回节点和关系。

Wiki graph 和 Neo4j GraphRAG 不是一回事。Wiki graph 来自 WeKnora 内部 wiki 页面和链接；Neo4j 用于实体关系 GraphRAG。

## 出问题先看哪里

先看：

```bash
docker compose ps
docker compose logs --tail=300 app
docker compose logs --tail=200 docreader
docker compose logs --tail=200 postgres
docker compose logs --tail=200 redis
```

常见现象：

```text
前端能打开但 API 失败
  查 APP_HOST、APP_BACKEND_PORT、frontend 日志、app /health。

注册/登录正常但聊天失败
  查 KnowledgeQA 模型行、模型后端 /models 或 /api/tags、app 日志。

文档解析失败
  查 docreader health/logs、MAX_FILE_SIZE_MB、./data/files 挂载、app 日志。

embedding/indexing 失败
  查 Embedding 模型行、dimension、OLLAMA_BASE_URL 或 embedding base_url、embedding API。

baseURL SSRF check failed
  只把需要的模型 host 加入 SSRF_WHITELIST_EXTRA 或 SSRF_WHITELIST。
  容器访问宿主机映射模型后端时，保留 host.docker.internal。

GraphRAG 显示“实体关系提取失败”
  查 NEO4J_ENABLE、ENABLE_GRAPH_RAG、Neo4j/APOC、选中的聊天模型行。
  如果日志有 model not found，查 models 表的 managed_by 和 deleted_at。

YAML 模型行重启后消失
  该行是 managed_by='yaml'，但不在当前 builtin_models.yaml 中。
  把它放回 YAML，或重建/转换为 managed_by=''。
```

详细参考：

- [remote-weknora-deployment.md](remote-weknora-deployment.md)
- [build-images.md](build-images.md)
- [model-hub-ollama-embedding.md](model-hub-ollama-embedding.md)
- [remote-vllm-backend.md](remote-vllm-backend.md)
- [neo4j.env.example](neo4j.env.example)
- [../BUILTIN_MODELS.md](../BUILTIN_MODELS.md)

---

# Fresh Host WeKnora Deployment

This note is the operator checklist for bringing up WeKnora on a machine that
has no existing WeKnora runtime state. It links to the narrower ictrek notes for
image build, model backends, and feature-specific details.

`tc232` and similar names are SSH config aliases on an operator workstation.
They are not service hostnames. Any public access URL must be provided by a
separate port mapping, reverse proxy, VPN, or tunnel.

## Target Shape

The smallest useful deployment runs:

```text
frontend   Web UI
app        WeKnora API and workers
docreader  document parsing service
postgres   metadata, vector data when RETRIEVE_DRIVER=postgres
redis      stream/task state
```

Use the default non-lite stack. For baseline RAG, keep:

```env
RETRIEVE_DRIVER=postgres
STORAGE_TYPE=local
STREAM_MANAGER_TYPE=redis
LOCAL_STORAGE_BASE_DIR=/data/files
```

Add optional services only when the feature is used:

```text
neo4j      entity/relation GraphRAG
minio      object storage instead of local files
qdrant     external vector database
milvus     external vector database
weaviate   external vector database
searxng    self-hosted web search
langfuse   model-call observability
```

## Machine Prerequisites

Install and verify:

```bash
docker version
docker compose version
```

For GPU model backends, verify the platform-specific runtime before starting
vLLM or Ollama:

```bash
nvidia-smi
docker run --rm --gpus all nvidia/cuda:12.4.1-base-ubuntu22.04 nvidia-smi
```

On Jetson/L4T hosts, use the host's NVIDIA container runtime test image instead
of the CUDA x86 image. Fix GPU runtime first; WeKnora can run without GPU, but
local model backends cannot use acceleration without it.

Create a deployment directory and persistent state directories:

```bash
mkdir -p /data/jhu/deploy/weknora
cd /data/jhu/deploy/weknora
mkdir -p data/files data/postgres data/redis config
```

Use your own root path if `/data/jhu` is not appropriate.

## Pick WeKnora Images

If images already exist, prefer the released-image path:

- read the platform sheet in the Feishu release table;
- find the `weknora`, `weknora-ui`, and `weknora-docreader` columns;
- combine row 2 repository URI with the selected dated row tag;
- create `docker-compose.images.yml` as shown in
  [build-images.md](build-images.md#start-from-existing-images).

The released WeKnora images do not include deployment-specific model rows.
That is intentional. Add models later in the UI, or mount an operator-created
`config/builtin_models.yaml`.

If images do not exist for the platform, build and push them first with
[build-images.md](build-images.md). Do not include CUDA markers in WeKnora image
tags unless the WeKnora image itself starts depending on CUDA libraries.

## Prepare Model Backends

WeKnora needs at least:

```text
chat model       KnowledgeQA
embedding model  Embedding
```

For image/document description, also configure:

```text
vision model     VLLM
```

Rerank is optional. Only configure it when a real rerank endpoint exists.

### Option A: Ollama-First

Use this when one local Ollama service should provide chat, image understanding,
and embedding. Follow
[model-hub-ollama-embedding.md](model-hub-ollama-embedding.md) for the exact
container command and keep-alive behavior.

Prepare the Ollama service and models before starting WeKnora:

```bash
ollama pull qwen3.5:2b
ollama pull bge-m3
```

Expected WeKnora model rows:

```text
KnowledgeQA  source=local  name=qwen3.5:2b
VLLM         source=local  name=qwen3.5:2b
Embedding    source=local  name=bge-m3:latest  dimension=1024
```

For local Ollama rows, leave `base_url` and `api_key` empty. WeKnora uses
`OLLAMA_BASE_URL` for all local Ollama calls.

Plain Ollama does not provide a generic rerank API. To use rerank in an
Ollama-first deployment, prepare a rerank-capable model or sidecar plus a
gateway that exposes `/v1/rerank`; otherwise leave rerank unconfigured.

### Option B: vLLM For Chat/VLM, Ollama For Embedding

Use this when the chat/VLM model should run behind an OpenAI-compatible vLLM
endpoint and embeddings should run through Ollama. Follow:

- [remote-vllm-backend.md](remote-vllm-backend.md)
- [model-hub-ollama-embedding.md](model-hub-ollama-embedding.md)

Expected WeKnora model rows:

```text
KnowledgeQA  source=remote  name=<vllm-served-model>  base_url=http://host.docker.internal:<vllm-port>/v1
VLLM         source=remote  name=<vllm-served-model>  base_url=http://host.docker.internal:<vllm-port>/v1
Embedding    source=local or remote, depending on whether native Ollama or an OpenAI-compatible embedding gateway is used
```

Keep `host.docker.internal` in `SSRF_WHITELIST_EXTRA` when the app container
calls model services through host-mapped ports.

## Prepare `.env`

Start from `.env.example`:

```bash
cp .env.example .env
```

Set the baseline values:

```env
GIN_MODE=release
TZ=Asia/Shanghai
WEKNORA_LANGUAGE=zh-CN

DB_USER=postgres
DB_PASSWORD=<strong-password>
DB_NAME=WeKnora

REDIS_PASSWORD=<strong-password>
REDIS_DB=0
REDIS_PREFIX=stream:

RETRIEVE_DRIVER=postgres
STORAGE_TYPE=local
STREAM_MANAGER_TYPE=redis
LOCAL_STORAGE_BASE_DIR=/data/files

FRONTEND_PORT=80
APP_PORT=8080
DOCREADER_PORT=50051

JWT_SECRET=<strong-random-value>
TENANT_AES_KEY=<strong-random-value>
SYSTEM_AES_KEY=<32-byte-value>
```

For Ollama-first:

```env
OLLAMA_BASE_URL=http://host.docker.internal:<ollama-host-port>
SSRF_WHITELIST_EXTRA=host.docker.internal,searxng,qdrant,milvus,weaviate,doris-fe
```

For vLLM through a host-mapped port, keep the same `SSRF_WHITELIST_EXTRA` and
use `http://host.docker.internal:<vllm-port>/v1` in the model row.

For Neo4j/GraphRAG:

```env
NEO4J_ENABLE=true
ENABLE_GRAPH_RAG=true
NEO4J_URI=bolt://neo4j:7687
NEO4J_USERNAME=neo4j
NEO4J_PASSWORD=<strong-password>
```

See [neo4j.env.example](neo4j.env.example).

## Decide How To Add Models

Recommended for first deployments: start WeKnora, log in, and add model rows in
the Web UI. This avoids stale model IDs baked into images or compose files.

Use `config/builtin_models.yaml` only when the deployment needs declarative
model rows. In that case:

```bash
cp config/builtin_models.yaml.example config/builtin_models.yaml
```

Edit only the model entries needed by the deployment, then explicitly mount it
in `docker-compose.override.yml`:

```yaml
services:
  app:
    volumes:
      - ./config/builtin_models.yaml:/app/config/builtin_models.yaml:ro
```

For each YAML model entry, the fields that normally change per deployment are:

```yaml
builtin_models:
  - id: <stable-id-used-by-kb-and-agents>
    type: KnowledgeQA | VLLM | Embedding | Rerank
    source: local | remote
    is_default: true
    name: <model-name-or-ollama-tag>
    parameters:
      base_url: <empty-for-local-ollama-or-remote-/v1-url>
      api_key: <empty-or-env-placeholder>
      provider: generic
      embedding_parameters:
        dimension: <embedding-dimension>
```

For Ollama `source=local` rows, keep `base_url` and `api_key` empty and set
`OLLAMA_BASE_URL` in `.env`. For vLLM/OpenAI-compatible rows, use
`source=remote` and set `base_url` to the endpoint ending in `/v1`.

Important lifecycle rule:

```text
Rows created from builtin_models.yaml are tagged managed_by='yaml'.
On app startup, YAML-managed rows missing from the current YAML are soft-deleted.
Rows created in the UI/API/manual SQL should keep managed_by='' and are not touched.
```

Before switching to an empty `builtin_models: []`, make sure knowledge bases,
agents, and GraphRAG do not still reference model rows that are about to be
soft-deleted. See [remote-weknora-deployment.md](remote-weknora-deployment.md#model-row-troubleshooting).

## Compose Files

Use these files for a normal released-image deployment:

```bash
docker compose \
  -f docker-compose.yml \
  -f docker-compose.override.yml \
  -f docker-compose.images.yml \
  up -d postgres redis docreader app frontend
```

`docker-compose.override.yml` is important for local persistence:

```text
./data/files     -> app:/data/files
./data/postgres  -> postgres:/var/lib/postgresql/data
./data/redis     -> redis:/data
```

If no `docker-compose.images.yml` is used, Compose falls back to the image names
and build sections in `docker-compose.yml`. On a production-like empty host,
prefer the released-image override so startup does not build locally.

Start optional profiles only when needed:

```bash
docker compose -f docker-compose.yml -f docker-compose.override.yml --profile neo4j up -d neo4j
docker compose -f docker-compose.yml -f docker-compose.override.yml --profile minio up -d minio
docker compose -f docker-compose.yml -f docker-compose.override.yml --profile qdrant up -d qdrant
```

When using `docker-compose.images.yml`, include it in the optional-profile
commands too.

## Startup Order

1. Start model backends and verify their APIs.
2. Start optional storage/graph services such as Neo4j if needed.
3. Start WeKnora dependencies:

```bash
docker compose -f docker-compose.yml -f docker-compose.override.yml -f docker-compose.images.yml up -d postgres redis docreader
```

4. Start app and frontend:

```bash
docker compose -f docker-compose.yml -f docker-compose.override.yml -f docker-compose.images.yml up -d app frontend
```

5. Open the frontend host/port, register or log in, and add model rows if they
were not declared through YAML.

Only expose the frontend publicly. Keep app, docreader, database, Redis, and
Neo4j ports private unless an operator intentionally publishes them for
debugging.

## Component Checks

Check containers:

```bash
docker compose ps
docker compose logs --tail=200 app
docker compose logs --tail=100 docreader
```

Check app health:

```bash
curl -fsS http://127.0.0.1:<app-host-port>/health
```

Check frontend:

```bash
curl -I http://127.0.0.1:<frontend-host-port>/
```

Check Postgres:

```bash
docker compose exec postgres pg_isready -U "$DB_USER"
```

Check Redis:

```bash
docker compose exec redis redis-cli -a "$REDIS_PASSWORD" ping
```

Check docreader:

```bash
docker compose exec docreader grpc_health_probe -addr=localhost:50051
```

Check Ollama:

```bash
curl -fsS http://127.0.0.1:<ollama-host-port>/api/tags
curl -fsS http://127.0.0.1:<ollama-host-port>/api/embed \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":["中文知识库检索测试"]}'
```

Check vLLM:

```bash
curl -fsS http://127.0.0.1:<vllm-host-port>/v1/models
curl -fsS http://127.0.0.1:<vllm-host-port>/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"<served-model>","messages":[{"role":"user","content":"用一句中文说明你是谁。"}],"max_tokens":128}'
```

Check Neo4j and APOC:

```bash
docker compose exec neo4j cypher-shell \
  -u "$NEO4J_USERNAME" -p "$NEO4J_PASSWORD" \
  'RETURN apoc.version();'
```

Check model rows:

```bash
docker compose exec postgres psql -U "$DB_USER" -d "$DB_NAME" -c "
select id,type,source,name,is_default,is_builtin,managed_by,deleted_at
from models
order by type,id;"
```

## Functional Checks

Use the Web UI for the final checks:

1. Create or select default KnowledgeQA, Embedding, and optional VLLM model rows.
2. Create a knowledge base.
3. Upload a small TXT/PDF file and confirm parsing succeeds.
4. Ask a question that should hit the uploaded content.
5. If VLM is configured, upload or parse a document containing an image and
   confirm image description does not fail.
6. If GraphRAG is enabled, run entity/relation extraction on a short text and
   confirm nodes/relations are returned.

The Wiki graph page is separate from Neo4j GraphRAG. Wiki graph is generated
from wiki pages and links inside WeKnora; Neo4j is used for entity/relation
GraphRAG.

## Troubleshooting Map

Use these first:

```bash
docker compose ps
docker compose logs --tail=300 app
docker compose logs --tail=200 docreader
docker compose logs --tail=200 postgres
docker compose logs --tail=200 redis
```

Common symptoms:

```text
Frontend opens but API fails
  Check APP_HOST, APP_BACKEND_PORT, frontend logs, and app /health.

Register/login works but chat fails
  Check KnowledgeQA model row, model backend /models or /api/tags, and app logs.

Document parsing fails
  Check docreader health/logs, MAX_FILE_SIZE_MB, mounted ./data/files, and app logs.

Embedding/indexing fails
  Check Embedding row, dimension, OLLAMA_BASE_URL or embedding base_url, and embedding API.

baseURL SSRF check failed
  Add only the required model host to SSRF_WHITELIST_EXTRA or SSRF_WHITELIST.
  For host-mapped local backends, keep host.docker.internal whitelisted.

GraphRAG shows “实体关系提取失败”
  Check NEO4J_ENABLE, ENABLE_GRAPH_RAG, Neo4j/APOC, and the selected chat model row.
  If logs contain model not found, inspect managed_by and deleted_at in models.

YAML model rows disappear after restart
  The row was managed_by='yaml' and absent from the current builtin_models.yaml.
  Put it back in YAML, or recreate/convert it to managed_by=''.
```

Detailed references:

- [remote-weknora-deployment.md](remote-weknora-deployment.md)
- [build-images.md](build-images.md)
- [model-hub-ollama-embedding.md](model-hub-ollama-embedding.md)
- [remote-vllm-backend.md](remote-vllm-backend.md)
- [neo4j.env.example](neo4j.env.example)
- [../BUILTIN_MODELS.md](../BUILTIN_MODELS.md)
