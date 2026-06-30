# 远程 WeKnora 部署

本文件记录 ictrek 的远程 WeKnora 部署方式。中文说明在上方，英文原文在下方。

`tc232` 是操作员本机 SSH config alias，不是公网服务 hostname。本文端口都是远程宿主机本地绑定；如果要从外部访问，需要另外配置公网映射、反向代理、VPN 或 tunnel。

## 部署范围

默认使用非 lite WeKnora 栈：

```text
frontend
app
docreader
postgres  paradedb/paradedb:v0.22.2-pg17
redis     redis:7.0-alpine
```

基础 RAG 不需要额外 vector database 或对象存储 sidecar：

```env
RETRIEVE_DRIVER=postgres
STORAGE_TYPE=local
STREAM_MANAGER_TYPE=redis
```

ictrek 的 compose override 会把运行时状态映射到部署目录下：

```text
data/files     -> app:/data/files
data/postgres  -> postgres:/var/lib/postgresql/data
data/redis     -> redis:/data
```

这样上传原文、本地知识库文件、数据库状态和 Redis AOF 都不会落在匿名 Docker volume 中。

同一套部署必须固定使用同一组 compose 文件。使用发布镜像时建议写成：

```bash
COMPOSE_FILES="-f docker-compose.yml -f docker-compose.override.yml -f docker-compose.images.yml"
docker compose $COMPOSE_FILES ps
docker compose $COMPOSE_FILES up -d postgres redis docreader app frontend
```

不要混用不同文件集合，例如一次使用 `docker compose up -d`，另一次使用 `docker compose -f docker-compose.yml -f docker-compose.images.yml up -d`。只要显式传入 `-f`，Docker Compose 就不会自动加载 `docker-compose.override.yml`。如果持久化挂载写在 override 中，漏掉它会让 Postgres、Redis 或文件存储切到 `docker-compose.yml` 里的 named volume；如果现有数据本来就在 named volume 中，误带 override 又会切到本地空目录。

已有部署先以当前真实挂载为准，不要为了“统一文档”直接切换。先查：

```bash
docker inspect WeKnora-postgres --format '{{json .Mounts}}'
docker inspect WeKnora-app --format '{{json .Mounts}}'
docker inspect WeKnora-redis --format '{{json .Mounts}}'
```

如果某台机器已经稳定使用 named volume，可以在该机器 `.env` 中固定：

```env
COMPOSE_FILE=docker-compose.yml:docker-compose.images.yml
```

如果某台机器稳定使用本地目录挂载，则固定：

```env
COMPOSE_FILE=docker-compose.yml:docker-compose.override.yml:docker-compose.images.yml
```

无论选哪一种，升级、重启、查日志都必须使用同一组文件。

`qdrant`、`milvus`、`weaviate`、`minio`、`searxng`、`neo4j`、`langfuse` 等 profile 只在明确启用对应功能时启动。

## 模型配置

WeKnora app 镜像和 ictrek compose 默认不携带部署专用模型记录。新部署启动后没有内置 LLM、VLM、embedding、rerank 行。模型可在 Web UI 添加，或显式挂载由环境变量生成的 `config/builtin_models.yaml`。

`config/builtin_models.yaml` 创建的模型行属于 YAML 托管。app 启动时，如果数据库里某行仍是 `managed_by='yaml'`，但当前 YAML 中已经没有它，该行会被软删除。Web UI、API、手工 SQL 创建的模型行应保持 `managed_by=''`，不会被 YAML loader 清理。

不要在已有部署上直接切到空 `builtin_models: []`，除非确认知识库、智能体和 GraphRAG 不再引用那些 YAML 托管模型行。否则会出现 `model not found`，前端可能显示“实体关系提取失败”。

模型后端参考：

- vLLM OpenAI-compatible LLM/VLM 后端：[remote-vllm-backend.md](remote-vllm-backend.md)
- Ollama 聊天、图片理解、embedding、可选 rerank：[model-hub-ollama-embedding.md](model-hub-ollama-embedding.md)

Ollama 部署中，app 环境变量指向容器可访问的 Ollama 原生 API：

```bash
OLLAMA_BASE_URL=http://host.docker.internal:21434
```

如果 Ollama 映射端口不同，只改端口：

```bash
OLLAMA_BASE_URL=http://host.docker.internal:<ollama-host-port>
```

只要模型 base URL 使用 Docker host gateway，就要在 `SSRF_WHITELIST_EXTRA` 中保留 `host.docker.internal`。

## 在 Web UI 中添加模型

全 Ollama 方案：

```text
KnowledgeQA  source=local  name=<ollama chat model tag>
VLLM         source=local  name=<ollama vision model tag>
Embedding    source=local  name=<ollama embedding model tag>  dimension=<embedding dimension>
```

`source=local` 时 `base_url` 和 `api_key` 留空；WeKnora 使用 `OLLAMA_BASE_URL`。每个模型类型选择一个默认行。

Ollama 主方案常用配置：

```text
KnowledgeQA  source=local  name=qwen3.5:2b
VLLM         source=local  name=qwen3.5:2b
Embedding    source=local  name=bge-m3:latest  dimension=1024
```

原生 Ollama 不提供 WeKnora 通用 rerank API。除非另有 rerank sidecar/gateway 暴露 `/v1/rerank`，否则不要配置默认 rerank。

添加 UI 行前先确认模型服务可用：

```bash
curl -fsS http://127.0.0.1:<ollama-host-port>/api/tags
curl -fsS http://127.0.0.1:<ollama-openai-port>/v1/models
curl -fsS http://127.0.0.1:<ollama-openai-port>/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":["中文 embedding 测试"]}'
```

OpenAI-compatible 远程 endpoint，例如 vLLM 或 gateway：

```text
source=remote
base_url=<endpoint ending in /v1>
api_key=<required only if backend checks it>
```

## 通过环境变量和 YAML 添加模型

只有需要声明式模型行时才挂载 `config/builtin_models.yaml`。示例 `.env`：

```bash
OLLAMA_BASE_URL=http://host.docker.internal:21434
WEKNORA_CHAT_MODEL_ID=local-ollama-chat
WEKNORA_CHAT_MODEL_NAME=qwen3.5:2b
WEKNORA_VLM_MODEL_ID=local-ollama-vlm
WEKNORA_VLM_MODEL_NAME=qwen3.5:2b
WEKNORA_EMBEDDING_MODEL_ID=local-ollama-embedding
WEKNORA_EMBEDDING_MODEL_NAME=bge-m3:latest
WEKNORA_EMBEDDING_DIMENSION=1024
```

示例 `config/builtin_models.yaml`：

```yaml
builtin_models:
  - id: ${WEKNORA_CHAT_MODEL_ID}
    type: KnowledgeQA
    source: local
    is_default: true
    name: ${WEKNORA_CHAT_MODEL_NAME}
    parameters:
      base_url: ""
      api_key: ""
      provider: generic
      supports_vision: true

  - id: ${WEKNORA_VLM_MODEL_ID}
    type: VLLM
    source: local
    is_default: true
    name: ${WEKNORA_VLM_MODEL_NAME}
    parameters:
      base_url: ""
      api_key: ""
      provider: generic
      supports_vision: true

  - id: ${WEKNORA_EMBEDDING_MODEL_ID}
    type: Embedding
    source: local
    is_default: true
    name: ${WEKNORA_EMBEDDING_MODEL_NAME}
    parameters:
      base_url: ""
      api_key: ""
      provider: generic
      embedding_parameters:
        dimension: ${WEKNORA_EMBEDDING_DIMENSION}
        truncate_prompt_tokens: 8192
        supports_dimension_override: false
```

挂载方式：

```yaml
services:
  app:
    volumes:
      - ./config/builtin_models.yaml:/app/config/builtin_models.yaml:ro
```

改完 YAML 后只需重启 app：

```bash
docker compose $COMPOSE_FILES restart app
```

## 模型行排障

GraphRAG 实体关系抽取显示“实体关系提取失败”，且 app 日志有 `model not found` 时，先查模型行是否还存在、是否被软删除：

```bash
docker compose exec postgres psql -U postgres -d WeKnora -c "
select id,type,source,name,is_default,is_builtin,managed_by,deleted_at
from models
where id in ('<chat-model-id>','<vlm-model-id>','<embedding-model-id>')
order by type,id;"
```

如果确认只是 YAML 生命周期变化导致软删除，可以把模型放回 YAML 后重启 app，或谨慎转为手工行：

```bash
docker compose exec postgres psql -U postgres -d WeKnora -c "
update models
set deleted_at = null,
    managed_by = '',
    updated_at = now()
where id in ('<chat-model-id>','<vlm-model-id>','<embedding-model-id>');"
```

只在 endpoint、模型名、provider、embedding dimension 都仍然正确时使用 SQL 恢复。否则应在 Web UI 或 YAML 中重新创建。

## 远程源码同步

不要在远程部署机上跑 git。将本地工作树同步到远程部署目录：

```bash
rsync -az --delete \
  --exclude '.git' \
  --exclude 'frontend/node_modules' \
  --exclude 'frontend/dist' \
  --exclude 'data' \
  --exclude '.cache' \
  --exclude '.env' \
  apps/WeKnora/ tc232:/data/jhu/deploy/weknora/
```

## 镜像和启动

正式部署优先使用已构建镜像，按 [build-images.md](build-images.md#start-from-existing-images) 生成 `docker-compose.images.yml` 后启动：

```bash
COMPOSE_FILES="-f docker-compose.yml -f docker-compose.override.yml -f docker-compose.images.yml"
docker compose \
  -f docker-compose.yml \
  -f docker-compose.override.yml \
  -f docker-compose.images.yml \
  up -d postgres redis docreader app frontend
```

后续升级镜像必须使用同一组文件：

```bash
docker compose $COMPOSE_FILES pull docreader app frontend
docker compose $COMPOSE_FILES up -d docreader app frontend
```

构建和飞书更新使用 `build_image.sh`，流程见 [build-images.md](build-images.md)。

## 常用检查

```bash
docker compose ps
docker compose logs --tail=300 app
docker compose logs --tail=200 docreader
curl -fsS http://127.0.0.1:<app-port>/health
```

如果模型、session、知识库突然变少，先查是否切了挂载：

```bash
docker inspect WeKnora-postgres --format '{{json .Mounts}}'
docker compose $COMPOSE_FILES exec postgres psql -U "$DB_USER" -d "$DB_NAME" -c "
select 'models' as table_name, count(*) from models
union all select 'sessions', count(*) from sessions
union all select 'messages', count(*) from messages
union all select 'knowledge_bases', count(*) from knowledge_bases;"
```

## 升级后的强制冒烟检查

`/health` 只能说明 app 进程活着，不能说明模型、SSRF 白名单、prompt 和 RAG 链路可用。每次升级或重启后至少检查：

```bash
curl -fsS http://127.0.0.1:<app-port>/health
curl -fsS http://127.0.0.1:<vllm-port>/v1/models
curl -fsS http://127.0.0.1:<embedding-openai-port>/v1/models
curl -fsS http://127.0.0.1:<embedding-openai-port>/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":["中文 embedding 测试"]}'
```

再从前端或 API 问一次“你是谁”。如果回答出现以下任一情况，说明部署仍有问题，不能算完成：

```text
baseURL SSRF check failed
hostname host.docker.internal is restricted
You are WeKnora
developed by Tencent
CRITICAL: Language Rule
{{language}}
Sorry, I could not find content directly related...
```

对应排查：

```bash
docker compose exec app env | grep SSRF
docker compose logs --tail=300 app | grep -E 'SSRF|model not found|fallback|CRITICAL|host.docker.internal'
docker compose exec postgres psql -U "$DB_USER" -d "$DB_NAME" -c "
select id,type,source,name,parameters->>'base_url' as base_url,deleted_at
from models
order by type,id;
select id,position('Vivibit' in config->>'system_prompt') as vivibit_pos,
          position('WeKnora' in config->>'system_prompt') as weknora_pos
from custom_agents
where is_builtin = true;"
```

如果新问题被旧的“生成中”状态卡住，先查是否有未完成 assistant 消息：

```bash
docker compose exec postgres psql -U "$DB_USER" -d "$DB_NAME" -c "
select m.id,m.session_id,s.title,m.created_at,m.updated_at
from messages m
join sessions s on s.id=m.session_id
where m.role='assistant'
  and m.is_completed=false
  and m.deleted_at is null
  and s.deleted_at is null
order by m.created_at desc;"
```

这通常不是 vLLM 并发数问题，而是前端看到旧 assistant 未完成后进入 `continue-stream` 续接旧消息。正常代码会在 QA goroutine 退出时写 terminal complete event 并把 assistant 标完成；如果仍出现，继续查 app 日志和对应 Redis stream key。

## Mandatory Smoke Check After Upgrades

`/health` only proves the app process is alive. It does not prove that model
backends, SSRF whitelist, prompts, and the RAG path work. After every upgrade or
restart, check at least:

```bash
curl -fsS http://127.0.0.1:<app-port>/health
curl -fsS http://127.0.0.1:<vllm-port>/v1/models
curl -fsS http://127.0.0.1:<embedding-openai-port>/v1/models
curl -fsS http://127.0.0.1:<embedding-openai-port>/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":["中文 embedding 测试"]}'
```

Then ask “你是谁” from the frontend or API. The deployment is not complete if
the answer contains any of:

```text
baseURL SSRF check failed
hostname host.docker.internal is restricted
You are WeKnora
developed by Tencent
CRITICAL: Language Rule
{{language}}
Sorry, I could not find content directly related...
```

Use these checks to narrow it down:

```bash
docker compose exec app env | grep SSRF
docker compose logs --tail=300 app | grep -E 'SSRF|model not found|fallback|CRITICAL|host.docker.internal'
docker compose exec postgres psql -U "$DB_USER" -d "$DB_NAME" -c "
select id,type,source,name,parameters->>'base_url' as base_url,deleted_at
from models
order by type,id;
select id,position('Vivibit' in config->>'system_prompt') as vivibit_pos,
          position('WeKnora' in config->>'system_prompt') as weknora_pos
from custom_agents
where is_builtin = true;"
```

If a new question is blocked by an old "generating" state, first check for
incomplete assistant messages:

```bash
docker compose exec postgres psql -U "$DB_USER" -d "$DB_NAME" -c "
select m.id,m.session_id,s.title,m.created_at,m.updated_at
from messages m
join sessions s on s.id=m.session_id
where m.role='assistant'
  and m.is_completed=false
  and m.deleted_at is null
  and s.deleted_at is null
order by m.created_at desc;"
```

This is usually not a vLLM concurrency issue. It means the frontend saw an old
incomplete assistant message and entered `continue-stream` for that message.
Normal code should now write a terminal complete event and mark the assistant
message complete when the QA goroutine exits. If it still happens, inspect the
app logs and the matching Redis stream key.

需要从空机器完整部署时，优先看 [fresh-host-deployment.md](fresh-host-deployment.md)。

---

# Remote WeKnora Deployment

This note records the ictrek deployment path used on `ssh tc232`.

`tc232` is an SSH config alias, not a public service hostname. The ports below are remote host bindings only. Add an external port mapping or reverse proxy separately when the service needs to be reached from outside that host.

## Scope

Use the default, non-lite WeKnora stack:

- `frontend`
- `app`
- `docreader`
- `postgres` (`paradedb/paradedb:v0.22.2-pg17`)
- `redis` (`redis:7.0-alpine`)

For the baseline RAG deployment, no extra vector database or object storage sidecar is required:

- `RETRIEVE_DRIVER=postgres`
- `STORAGE_TYPE=local`
- `STREAM_MANAGER_TYPE=redis`

The ictrek compose override maps runtime state to host directories under the
remote deployment tree:

```text
/data/jhu/deploy/weknora/data/files     -> app:/data/files
/data/jhu/deploy/weknora/data/postgres  -> postgres:/var/lib/postgresql/data
/data/jhu/deploy/weknora/data/redis     -> redis:/data
```

This keeps uploaded source documents, local knowledge-base files, database
state, and Redis append-only data outside anonymous Docker volumes.

Keep one stable compose file set for each deployment. For released images, a
directory-mount deployment should use:

```bash
COMPOSE_FILES="-f docker-compose.yml -f docker-compose.override.yml -f docker-compose.images.yml"
docker compose $COMPOSE_FILES ps
docker compose $COMPOSE_FILES up -d postgres redis docreader app frontend
```

Do not mix file sets, such as plain `docker compose up -d` in one operation and
`docker compose -f docker-compose.yml -f docker-compose.images.yml up -d` in
the next. Once any `-f` flag is provided, Docker Compose does not auto-load
`docker-compose.override.yml`. Omitting the override can switch Postgres,
Redis, or file storage to the named volumes from `docker-compose.yml`; adding
the override to a deployment whose data already lives in named volumes can
switch it to empty local directories.

For an existing deployment, trust the current real mounts first:

```bash
docker inspect WeKnora-postgres --format '{{json .Mounts}}'
docker inspect WeKnora-app --format '{{json .Mounts}}'
docker inspect WeKnora-redis --format '{{json .Mounts}}'
```

If a machine intentionally uses named volumes, pin this in that machine's
`.env`:

```env
COMPOSE_FILE=docker-compose.yml:docker-compose.images.yml
```

If a machine intentionally uses local directory mounts, pin:

```env
COMPOSE_FILE=docker-compose.yml:docker-compose.override.yml:docker-compose.images.yml
```

Whichever option is selected, use it consistently for upgrades, restarts, and
logs.

Optional profiles such as `qdrant`, `milvus`, `weaviate`, `minio`, `searxng`, `neo4j`, and `langfuse` should only be started when that feature is intentionally enabled.

## Model Configuration

The WeKnora app image and ictrek compose files do not ship deployment-specific
model records by default. A new deployment starts without built-in LLM, VLM,
embedding, or rerank rows. Add models after startup in the Web UI, or mount an
operator-created `config/builtin_models.yaml` generated from environment
variables.

Model rows created from `config/builtin_models.yaml` are YAML-managed. On app
startup, rows still marked `managed_by='yaml'` but missing from the current YAML
are soft-deleted. Rows created through the Web UI, API, or deliberate SQL
maintenance should keep `managed_by=''` and are not touched by the YAML loader.
Do not switch an existing deployment to an empty `builtin_models: []` file
until the model rows referenced by knowledge bases, agents, and GraphRAG have
been recreated in the Web UI/API or converted to `managed_by=''`.

Optional model backend notes are documented in this directory:

- vLLM OpenAI-compatible LLM backend: `remote-vllm-backend.md`
- Ollama backend for chat, image understanding, embedding, and optional rerank:
  `model-hub-ollama-embedding.md`

For Ollama-based deployments, set the app environment to the native Ollama API
endpoint so the initialization page and local model clients can detect it:

```bash
OLLAMA_BASE_URL=http://host.docker.internal:21434
```

If the Ollama service runs on a different mapped port, change only the port:

```bash
OLLAMA_BASE_URL=http://host.docker.internal:<ollama-host-port>
```

The base `docker-compose.yml` keeps `host.docker.internal` in
`SSRF_WHITELIST_EXTRA` by default. Keep that entry when any model base URL uses
the Docker host gateway.

### Add Models In The Web UI

For an all-Ollama deployment, create these model rows in the system model page:

```text
KnowledgeQA  source=local  name=<ollama chat model tag>
VLLM         source=local  name=<ollama vision model tag>
Embedding    source=local  name=<ollama embedding model tag>  dimension=<embedding dimension>
```

Leave `base_url` and `api_key` empty for `source=local`; WeKnora uses
`OLLAMA_BASE_URL` for local Ollama calls. Mark the intended row in each model
type as the default.

Common Ollama-first rows:

```text
KnowledgeQA  source=local  name=qwen3.5:2b
VLLM         source=local  name=qwen3.5:2b
Embedding    source=local  name=bge-m3:latest  dimension=1024
```

Plain Ollama does not expose WeKnora's generic rerank API. Do not configure a
default rerank row unless a separate rerank sidecar/gateway exposes
`/v1/rerank`.

Verify the model service before adding UI rows:

```bash
curl -fsS http://127.0.0.1:<ollama-host-port>/api/tags
curl -fsS http://127.0.0.1:<ollama-openai-port>/v1/models
curl -fsS http://127.0.0.1:<ollama-openai-port>/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":["中文 embedding 测试"]}'
```

For an OpenAI-compatible remote endpoint such as vLLM or a gateway, use
`source=remote`, set `base_url` to the endpoint ending in `/v1`, and provide the
API key if required.

### Add Models Through Environment Variables

To declare model rows at deployment time, create a local
`config/builtin_models.yaml` from environment variables and mount it explicitly.
This file is not mounted by default because shipping concrete model IDs and
host ports in the image caused deployments to call stale backends.

Example `.env` for local Ollama:

```bash
OLLAMA_BASE_URL=http://host.docker.internal:21434
WEKNORA_CHAT_MODEL_ID=local-ollama-chat
WEKNORA_CHAT_MODEL_NAME=qwen3.5:2b
WEKNORA_VLM_MODEL_ID=local-ollama-vlm
WEKNORA_VLM_MODEL_NAME=qwen3.5:2b
WEKNORA_EMBEDDING_MODEL_ID=local-ollama-embedding
WEKNORA_EMBEDDING_MODEL_NAME=bge-m3:latest
WEKNORA_EMBEDDING_DIMENSION=1024
```

Example `config/builtin_models.yaml`:

```yaml
builtin_models:
  - id: ${WEKNORA_CHAT_MODEL_ID}
    type: KnowledgeQA
    source: local
    is_default: true
    name: ${WEKNORA_CHAT_MODEL_NAME}
    display_name: Local Ollama Chat
    parameters:
      base_url: ""
      api_key: ""
      provider: generic
      supports_vision: true

  - id: ${WEKNORA_VLM_MODEL_ID}
    type: VLLM
    source: local
    is_default: true
    name: ${WEKNORA_VLM_MODEL_NAME}
    display_name: Local Ollama Vision
    parameters:
      base_url: ""
      api_key: ""
      provider: generic
      supports_vision: true

  - id: ${WEKNORA_EMBEDDING_MODEL_ID}
    type: Embedding
    source: local
    is_default: true
    name: ${WEKNORA_EMBEDDING_MODEL_NAME}
    display_name: Local Ollama Embedding
    parameters:
      base_url: ""
      api_key: ""
      provider: generic
      embedding_parameters:
        dimension: ${WEKNORA_EMBEDDING_DIMENSION}
        truncate_prompt_tokens: 8192
        supports_dimension_override: false
```

Then enable the mount in the deployment compose override:

```yaml
services:
  app:
    volumes:
      - ./config/builtin_models.yaml:/app/config/builtin_models.yaml:ro
```

Restart only the app after changing this file:

```bash
docker compose $COMPOSE_FILES restart app
```

Rows declared in `builtin_models.yaml` are upserted on every app startup and
tagged `managed_by='yaml'`. Rows removed from the YAML are soft-deleted on the
next app startup if they are still YAML-managed. Manual rows with
`managed_by=''` are left alone.

The built-in quick-answer and smart-reasoning agents do not hard-code a VLM
model id. Select the image model in the agent settings after creating the VLM
model row.

The default assistant identity is defined in `config/prompt_templates/*.yaml`.
For the ictrek deployment, the relevant system prompt templates identify the
assistant as `Vivibit AI小助手` instead of the upstream WeKnora/Tencent persona.

### Model Row Troubleshooting

If GraphRAG entity/relation extraction shows “实体关系提取失败” and app logs
contain `model not found`, first check that the selected chat model row still
exists and is not soft-deleted:

```bash
docker compose exec postgres psql -U postgres -d WeKnora -c "
select id,type,source,name,is_default,is_builtin,managed_by,deleted_at
from models
where id in ('<chat-model-id>','<vlm-model-id>','<embedding-model-id>')
order by type,id;"
```

If a required row was only soft-deleted during a YAML lifecycle change, either
add it back to the mounted YAML and restart `app`, or convert it to a manual row
with a deliberate maintenance update:

```bash
docker compose exec postgres psql -U postgres -d WeKnora -c "
update models
set deleted_at = null,
    managed_by = '',
    updated_at = now()
where id in ('<chat-model-id>','<vlm-model-id>','<embedding-model-id>');"
```

Only use the SQL recovery for rows whose endpoint, model name, provider, and
embedding dimension are still correct. Otherwise recreate them in the Web UI or
through the mounted YAML.

## Remote Source Copy

Do not run `git` commands on the remote deployment host. Sync the local working tree to the remote deploy directory:

```bash
rsync -az --delete \
  --exclude '.git' \
  --exclude 'frontend/node_modules' \
  --exclude 'frontend/dist' \
  --exclude 'data' \
  --exclude '.cache' \
  --exclude '.env' \
  apps/WeKnora/ tc232:/data/jhu/deploy/weknora/
```

## Base Images

If Docker Hub access is slow or unavailable, pull missing base images through a reachable mirror and retag them to the names expected by the Dockerfiles and compose file:

```bash
ssh tc232 'bash -s' <<'EOF'
set -euo pipefail

pull_retag() {
  src="$1"
  dst="$2"
  docker image inspect "$dst" >/dev/null 2>&1 || {
    docker pull "$src"
    docker tag "$src" "$dst"
  }
}

pull_retag docker.m.daocloud.io/library/golang:1.26-bookworm golang:1.26-bookworm
pull_retag docker.m.daocloud.io/library/debian:12.12-slim debian:12.12-slim
pull_retag docker.m.daocloud.io/library/python:3.10.18-bookworm python:3.10.18-bookworm
pull_retag docker.m.daocloud.io/library/nginx:stable-alpine nginx:stable-alpine
pull_retag docker.m.daocloud.io/library/node:22-alpine node:22-alpine
pull_retag docker.m.daocloud.io/paradedb/paradedb:v0.22.2-pg17 paradedb/paradedb:v0.22.2-pg17
pull_retag docker.m.daocloud.io/library/redis:7.0-alpine redis:7.0-alpine
EOF
```

## Build Images

For release image builds and Feishu release-table updates, use
`build_image.sh`. The current image-build flow is documented in
`docs/ictrek/build-images.md`.

The manual commands below are kept as deployment/debugging reference only.

Build the frontend assets first, then build the three runtime images. The docreader image is intentionally large in the non-lite deployment because it includes LibreOffice, Java, Playwright WebKit, fonts, and document parsing libraries.

```bash
ssh tc232 'bash -s' <<'EOF'
set -euo pipefail
cd /data/jhu/deploy/weknora

docker run --rm \
  -v "$PWD/frontend:/app" \
  -w /app \
  -e VITE_IS_DOCKER=true \
  node:22-alpine \
  sh -lc 'npm config set registry https://registry.npmmirror.com && npm ci && npm run build'

export DOCKER_BUILDKIT=1
BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
COMMIT_ID=$(cat .git-commit 2>/dev/null || echo local-sync)
VERSION=ictrek-tc232

docker build -f frontend/Dockerfile \
  -t wechatopenai/weknora-ui:${VERSION} \
  frontend

docker build \
  --build-arg GOPROXY_ARG=https://goproxy.cn,direct \
  --build-arg GOSUMDB_ARG=sum.golang.google.cn \
  --build-arg APK_MIRROR_ARG=mirrors.tuna.tsinghua.edu.cn \
  --build-arg VERSION_ARG="$VERSION" \
  --build-arg COMMIT_ID_ARG="$COMMIT_ID" \
  --build-arg BUILD_TIME_ARG="$BUILD_DATE" \
  --build-arg GO_VERSION_ARG=1.26 \
  -f docker/Dockerfile.app \
  -t wechatopenai/weknora-app:${VERSION} \
  .

docker build \
  --build-arg APT_MIRROR=http://mirrors.tuna.tsinghua.edu.cn \
  -f docker/Dockerfile.docreader \
  -t wechatopenai/weknora-docreader:${VERSION} \
  .
EOF
```

The tested image set on `tc232` was:

```text
wechatopenai/weknora-ui:ictrek-tc232
wechatopenai/weknora-app:ictrek-tc232
wechatopenai/weknora-docreader:ictrek-tc232
paradedb/paradedb:v0.22.2-pg17
redis:7.0-alpine
```

## Runtime Environment

Create `/data/jhu/deploy/weknora/.env` on `tc232`. Keep this file out of git.

```bash
WEKNORA_VERSION=ictrek-tc232
GIN_MODE=release
LOG_LEVEL=info
TZ=Asia/Shanghai
WEKNORA_LANGUAGE=zh-CN
DISABLE_REGISTRATION=false

FRONTEND_PORT=18080
APP_PORT=18081
APP_BACKEND_PORT=8080
APP_HOST=app
APP_SCHEME=http

DB_DRIVER=postgres
RETRIEVE_DRIVER=postgres
STORAGE_TYPE=local
STREAM_MANAGER_TYPE=redis
OLLAMA_BASE_URL=http://host.docker.internal:21434

DB_HOST=postgres
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=postgres123!@#
DB_NAME=WeKnora
REDIS_PASSWORD=redis123!@#
REDIS_DB=0
REDIS_PREFIX=stream:
LOCAL_STORAGE_BASE_DIR=/data/files
AUTO_RECOVER_DIRTY=true

TENANT_AES_KEY=weknorarag-api-key-secret-secret
SYSTEM_AES_KEY=weknora-system-aes-key-32bytes!!
JWT_SECRET=weknora-jwt-secret

LANGFUSE_ENABLED=false
LANGFUSE_PUBLIC_KEY=
LANGFUSE_SECRET_KEY=
LANGFUSE_HOST=

CONCURRENCY_POOL_SIZE=5
WEKNORA_ASYNQ_CONCURRENCY=4
MAX_FILE_SIZE_MB=50
```

## Start

```bash
ssh tc232 'bash -s' <<'EOF'
set -euo pipefail
cd /data/jhu/deploy/weknora
mkdir -p skills/preloaded data/files data/postgres data/redis
docker compose up -d postgres redis docreader app frontend
EOF
```

For an existing deployment that was already using Docker named volumes, migrate
the named-volume data to the host bind paths before applying the override:

```bash
ssh tc232 'bash -s' <<'EOF'
set -euo pipefail
cd /data/jhu/deploy/weknora

docker compose stop frontend app postgres redis
mkdir -p data/files data/postgres data/redis

docker run --rm \
  -v weknora_data-files:/from:ro \
  -v "$PWD/data/files:/to" \
  redis:7.0-alpine sh -lc 'rm -rf /to/* /to/.[!.]* /to/..?* 2>/dev/null || true; cp -a /from/. /to/'

docker run --rm \
  -v weknora_postgres-data:/from:ro \
  -v "$PWD/data/postgres:/to" \
  redis:7.0-alpine sh -lc 'rm -rf /to/* /to/.[!.]* /to/..?* 2>/dev/null || true; cp -a /from/. /to/'

docker compose up -d postgres redis docreader app frontend
EOF
```

On a fresh ParadeDB volume, `postgres` may briefly become healthy and then restart after `/docker-entrypoint-initdb.d/10_bootstrap_paradedb.sh` finishes. If `app` starts during that window and becomes unhealthy with `connection refused`, start `app` and `frontend` again after Postgres settles:

```bash
ssh tc232 'cd /data/jhu/deploy/weknora && docker compose up -d app frontend'
```

If `config/builtin_models.yaml` is changed after the stack is already running,
restart `app` so the startup loader applies the model records:

```bash
ssh tc232 'cd /data/jhu/deploy/weknora && docker compose restart app'
```

When enabling `docker-compose.override.yml` for the first time on an already
running stack, force-recreate `app` once so Compose applies the new bind mount:

```bash
ssh tc232 'cd /data/jhu/deploy/weknora && docker compose up -d --force-recreate app'
```

## Verify

```bash
ssh tc232 'bash -s' <<'EOF'
set -euo pipefail
cd /data/jhu/deploy/weknora
docker compose ps
curl -i --max-time 10 http://127.0.0.1:18081/health
curl -I --max-time 10 http://127.0.0.1:18080/
EOF
```

Expected baseline:

- `WeKnora-app` is `healthy` and bound to `0.0.0.0:18081->8080/tcp`
- `WeKnora-docreader` is `healthy`
- `WeKnora-postgres` is `healthy`
- `WeKnora-frontend` is bound to `0.0.0.0:18080->80/tcp`
- `GET http://127.0.0.1:18081/health` returns `{"status":"ok"}`
- `HEAD http://127.0.0.1:18080/` returns `HTTP/1.1 200 OK`
- `models` may be empty on a fresh deployment until the operator adds model
  rows through the Web UI or a mounted `config/builtin_models.yaml`

## External Access

`tc232` is only the local SSH config alias. The deployment is verified on the
remote host, but outside access still needs a public port mapping or reverse
proxy in front of that host.

The minimum external mapping is:

```text
public HTTPS/HTTP port -> tc232:18080
```

The frontend nginx container serves the UI and proxies application API traffic
to the `app` service inside the Docker network, so normal browser usage only
needs `18080` exposed externally.

Expose these only when there is a separate operational need:

```text
public API port -> tc232:18081    # direct app API access, optional
public model port -> <model-host>:<model-port>  # direct model backend access, optional
```

Keep these internal by default:

```text
5432   # postgres
6379   # redis
50051  # docreader gRPC
21434  # Ollama native API, only when an Ollama backend is running
```

If the external proxy terminates TLS, forward plain HTTP to `tc232:18080`.

For an operator-only check without public exposure, use an SSH tunnel:

```bash
ssh -L 18080:127.0.0.1:18080 -L 18081:127.0.0.1:18081 tc232
```

Then open `http://127.0.0.1:18080/` locally.

## Login and Registration

This deployment uses the standard WeKnora stack, not Lite. There is no built-in
default username or password.

The current `.env` keeps public registration enabled:

```bash
DISABLE_REGISTRATION=false
```

With the default `self_serve` registration mode, a newly registered user gets a
new tenant and an Owner membership for that tenant. The cloud image notes also
warn that the first registration should be done immediately because the first
registered account becomes the initial administrator for the deployment.

After creating the first account, close public registration unless self-service
signup is intentional:

```bash
ssh tc232 'bash -s' <<'EOF'
set -euo pipefail
cd /data/jhu/deploy/weknora
perl -0pi -e 's/^DISABLE_REGISTRATION=false$/DISABLE_REGISTRATION=true/m' .env
docker compose up -d app frontend
EOF
```

For platform-level SystemAdmin bootstrap, set
`WEKNORA_BOOTSTRAP_SYSTEM_ADMIN_EMAIL` to an email that has already registered,
then restart the app. The setting only promotes; it does not demote existing
SystemAdmin users.
