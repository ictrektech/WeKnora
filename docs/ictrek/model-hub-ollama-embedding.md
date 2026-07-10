# Ollama Embedding 后端

本文件记录 WeKnora 使用 Ollama 提供 embedding，以及在 Ollama 为主方案中同时提供聊天、图片理解、embedding 的方式。中文说明在上方，英文原文在下方。

`tc232` 只是操作员本机 SSH config alias，不是网络 hostname。外部访问需要单独的公网映射、反向代理、VPN 或 SSH tunnel。

## 当前准备过的后端形态

- Ollama engine 镜像：`swr.cn-southwest-2.myhuaweicloud.com/ictrek/ollama_server:amd_0.30.6`
- Docker network：`weknora-model-net`
- 容器名：`weknora-model-hub-ollama`
- Ollama 原生 API 远程宿主机端口：`21434` -> 容器 `11434`
- OpenAI-compatible gateway 远程宿主机端口：`21535` -> 容器 `11535`
- 宿主机模型目录：`/data/jhu/models/ollama`
- 容器模型目录：`/root/.ollama`
- embedding 模型：`bge-m3:latest`
- embedding 维度：`1024`
- 常驻模型：`OLLAMA_KEEP_ALIVE=-1`

## 启动 Ollama 后端

在远程目标上执行：

```bash
ssh tc232 'bash -s' <<'EOF'
set -euo pipefail

OLLAMA_SERVER_IMAGE='swr.cn-southwest-2.myhuaweicloud.com/ictrek/ollama_server:amd_0.30.6'
NETWORK='weknora-model-net'
OLLAMA_ROOT='/data/jhu/models/ollama'

mkdir -p "$OLLAMA_ROOT"
docker network inspect "$NETWORK" >/dev/null 2>&1 || docker network create "$NETWORK" >/dev/null

docker rm -f weknora-model-hub-ollama >/dev/null 2>&1 || true
docker pull "$OLLAMA_SERVER_IMAGE"

docker run -d \
  --name weknora-model-hub-ollama \
  --restart unless-stopped \
  --network "$NETWORK" \
  --gpus all \
  --entrypoint /bin/sh \
  -p 21434:11434 \
  -p 21535:11535 \
  -v "$OLLAMA_ROOT:/root/.ollama" \
  "$OLLAMA_SERVER_IMAGE" \
  -lc 'export OLLAMA_HOST=0.0.0.0:11434 OLLAMA_KEEP_ALIVE=-1; ollama serve >/tmp/ollama.log 2>&1 & exec uvicorn ollama_gateway.gateway:app --host 0.0.0.0 --port 11535'
EOF
```

使用显式 `/bin/sh -lc ...` 是为了覆盖某些镜像默认 entrypoint 中只监听 `127.0.0.1` 的行为，让 Ollama 原生 API 能被宿主机端口和同网络容器访问。

## 拉取并常驻 embedding 模型

```bash
ssh tc232 'bash -s' <<'EOF'
set -euo pipefail

MODEL_NAME='bge-m3:latest'

for i in $(seq 1 60); do
  if curl -fsS http://127.0.0.1:21434/api/tags >/dev/null; then
    break
  fi
  [ "$i" = 60 ] && exit 1
  sleep 2
done

if ! curl -fsS http://127.0.0.1:21434/api/tags |
  python3 -c 'import json,sys; target=sys.argv[1]; d=json.load(sys.stdin); raise SystemExit(0 if any(m.get("name")==target or m.get("model")==target for m in d.get("models", [])) else 1)' "$MODEL_NAME"; then
  curl -fsS -X POST http://127.0.0.1:21434/api/pull \
    -H 'Content-Type: application/json' \
    -d "{\"model\":\"${MODEL_NAME}\",\"stream\":false}"
fi

curl -fsS http://127.0.0.1:21434/api/embed \
  -H 'Content-Type: application/json' \
  -d "{\"model\":\"${MODEL_NAME}\",\"input\":\"resident warmup\"}" >/dev/null

curl -fsS http://127.0.0.1:21434/api/ps
EOF
```

`bge-m3` 是多语言文本 embedding 模型，适合 WeKnora 的 Ollama embedding 路径，不需要多模态 embedding。`OLLAMA_KEEP_ALIVE=-1` 会让 warmup 后的模型保持常驻。

## 验证

```bash
ssh tc232 'bash -s' <<'EOF'
set -euo pipefail

curl -fsS http://127.0.0.1:21434/api/tags
curl -fsS http://127.0.0.1:21535/v1/models

curl -fsS http://127.0.0.1:21434/api/embed \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":["hello world","中文知识库检索测试"]}' \
  > /tmp/ollama_embed.json

python3 -c 'import json; d=json.load(open("/tmp/ollama_embed.json")); e=d["embeddings"]; print(len(e), len(e[0]))'

curl -fsS http://127.0.0.1:21535/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":["hello world","中文知识库检索测试"]}' \
  > /tmp/openai_embed.json

python3 -c 'import json; d=json.load(open("/tmp/openai_embed.json")); e=d["data"]; print(len(e), len(e[0]["embedding"]))'
EOF
```

期望 shape：

```text
2 1024
2 1024
```

## WeKnora 配置

如果 WeKnora 使用本地 Ollama provider，`.env` 中设置：

```env
OLLAMA_BASE_URL=http://host.docker.internal:21434
```

然后在 Web UI 或挂载的 `config/builtin_models.yaml` 中创建模型行：

```text
KnowledgeQA  source=local  name=qwen3.5:2b
VLLM         source=local  name=qwen3.5:2b
Embedding    source=local  name=bge-m3:latest  dimension=1024
```

`source=local` 时不要填 `base_url` 和 `api_key`。WeKnora 会通过 `OLLAMA_BASE_URL` 调用 Ollama。

如果 `OLLAMA_BASE_URL` 使用 `http://host.docker.internal:<port>`，app 容器必须能解析 Docker host gateway，且 `SSRF_WHITELIST_EXTRA` 要保留 `host.docker.internal`。基础 `docker-compose.yml` 已默认包含它；自定义该变量时不要漏掉：

```env
SSRF_WHITELIST_EXTRA=host.docker.internal,searxng,qdrant,milvus,weaviate,doris-fe
```

端口要和正在运行的容器一致。比如 `weknora-model-hub-ollama` 同时暴露 `21434` 原生 Ollama API 和 `21535` OpenAI-compatible gateway，WeKnora 的 `source=local` Ollama 行走 `OLLAMA_BASE_URL=http://host.docker.internal:21434`；`source=remote` embedding gateway 行才使用 `http://host.docker.internal:21535/v1`。

## Orin NX 分离 Ollama 方案

Orin NX / L4T 机器上，不建议让一个 Ollama 实例同时承担聊天、图片理解和 embedding 的高并发。`OLLAMA_NUM_PARALLEL` 是单个 Ollama 实例的全局调度并发，它不能区分“聊天保留槽位”和“文档 embedding 槽位”。

推荐使用 [deploy-template/docker-compose.orin-ollama.yml](deploy-template/docker-compose.orin-ollama.yml)：

```text
ollama-qa
  qwen3.5:2b
  用于 KnowledgeQA 和 VLLM
  OpenAI-compatible endpoint: http://ollama-qa:11535/v1

ollama-embedding
  bge-m3:latest
  用于 Embedding
  OpenAI-compatible endpoint: http://ollama-embedding:11535/v1
```

模型行使用 `source=remote`：

```text
KnowledgeQA  source=remote  name=qwen3.5:2b    base_url=http://ollama-qa:11535/v1
VLLM         source=remote  name=qwen3.5:2b    base_url=http://ollama-qa:11535/v1
Embedding    source=remote  name=bge-m3:latest base_url=http://ollama-embedding:11535/v1 dimension=1024
```

起步并发：

```env
OLLAMA_CONTEXT_LENGTH=18000
OLLAMA_QA_NUM_PARALLEL=3
OLLAMA_EMBEDDING_NUM_PARALLEL=4
WEKNORA_MAIN_QA_MODEL_CONCURRENCY=3
WEKNORA_CHAT_RESERVED_CONCURRENCY=2
WEKNORA_GRAPH_LLM_CONCURRENCY=1
WEKNORA_WIKI_INGEST_MAP_PARALLEL=1
WEKNORA_WIKI_INGEST_REDUCE_PARALLEL=1
WEKNORA_ASYNQ_CONCURRENCY=1
WEKNORA_WIKI_ASYNQ_CONCURRENCY=1
WEKNORA_MODEL_MAX_CONCURRENCY=1
CONCURRENCY_POOL_SIZE=1
BATCH_EMBED_SIZE=4
```

QA 上下文需要大于 16k 时不要设成正好 `16384`，Orin NX 16G 起步用 `18000`。如果只启动一个 Ollama 容器，可以用 `source=local` 和 `OLLAMA_BASE_URL`，但这只是简化方案。此时要把 `CONCURRENCY_POOL_SIZE` 降到 `1`，并接受文档 embedding 可能和聊天在 Ollama 内部排队。

Rerank 需要单独的 rerank endpoint。原生 Ollama 不提供 `/v1/rerank`；如果 gateway 只提供 `/v1/models`、`/v1/chat/completions`、`/v1/embeddings`，就不要配置 rerank，或改用外部 rerank provider。

---

# Ollama Embedding Backend

This note records the Ollama embedding backend prepared on the remote machine reached from this workstation with `ssh tc232`.

`tc232` is an SSH config alias on the operator workstation. It is not a network hostname for API clients. External access requires a separately managed public mapping, reverse proxy, VPN route, or SSH tunnel.

## Current Backend

- Remote SSH target: `ssh tc232`
- Ollama engine image: `swr.cn-southwest-2.myhuaweicloud.com/ictrek/ollama_server:amd_0.30.6`
- Docker network: `weknora-model-net`
- Ollama container: `weknora-model-hub-ollama`
- Ollama native remote host port: `21434` -> container `11434`
- Ollama OpenAI gateway remote host port: `21535` -> container `11535`
- Host Ollama directory: `/data/jhu/models/ollama`
- Container Ollama model directory: `/root/.ollama`
- Embedding model: `bge-m3:latest`
- Embedding dimensions: `1024`
- Ollama keep-alive: `OLLAMA_KEEP_ALIVE=-1`

The `model-hub:amd_20260625` image exists, but it is not required as a persistent runtime dependency for WeKnora embedding after the Ollama model has been downloaded. Keep only the Ollama container running unless the operator explicitly needs the model-hub management UI/API.

## Start Backend

Run on the remote target:

```bash
ssh tc232 'bash -s' <<'EOF'
set -euo pipefail

OLLAMA_SERVER_IMAGE='swr.cn-southwest-2.myhuaweicloud.com/ictrek/ollama_server:amd_0.30.6'
NETWORK='weknora-model-net'
OLLAMA_ROOT='/data/jhu/models/ollama'

mkdir -p "$OLLAMA_ROOT"
docker network inspect "$NETWORK" >/dev/null 2>&1 || docker network create "$NETWORK" >/dev/null

docker rm -f weknora-model-hub-ollama >/dev/null 2>&1 || true

docker pull "$OLLAMA_SERVER_IMAGE"

docker run -d \
  --name weknora-model-hub-ollama \
  --restart unless-stopped \
  --network "$NETWORK" \
  --gpus all \
  --entrypoint /bin/sh \
  -p 21434:11434 \
  -p 21535:11535 \
  -v "$OLLAMA_ROOT:/root/.ollama" \
  "$OLLAMA_SERVER_IMAGE" \
  -lc 'export OLLAMA_HOST=0.0.0.0:11434 OLLAMA_KEEP_ALIVE=-1; ollama serve >/tmp/ollama.log 2>&1 & exec uvicorn ollama_gateway.gateway:app --host 0.0.0.0 --port 11535'
EOF
```

The `ollama_server` image's default `/app/start.sh` hardcodes `OLLAMA_HOST=127.0.0.1:11434`. Use the explicit `/bin/sh -lc ...` command above so the Ollama native API is reachable on the mapped host port and from other containers on the Docker network.

`OLLAMA_KEEP_ALIVE=-1` keeps models resident after they are loaded. This matters for WeKnora because embedding calls are latency-sensitive and repeated document indexing should not reload the embedding model between batches.

## Ensure Resident Embedding Model

Run on the remote target:

```bash
ssh tc232 'bash -s' <<'EOF'
set -euo pipefail

MODEL_ID='ollama://bge-m3:latest'
MODEL_NAME='bge-m3:latest'

for i in $(seq 1 60); do
  if curl -fsS http://127.0.0.1:21434/api/tags >/dev/null; then
    break
  fi
  [ "$i" = 60 ] && exit 1
  sleep 2
done

if ! curl -fsS http://127.0.0.1:21434/api/tags |
  python3 -c 'import json,sys; target=sys.argv[1]; d=json.load(sys.stdin); raise SystemExit(0 if any(m.get("name")==target or m.get("model")==target for m in d.get("models", [])) else 1)' "$MODEL_NAME"; then
  curl -fsS -X POST http://127.0.0.1:21434/api/pull \
    -H 'Content-Type: application/json' \
    -d "{\"model\":\"${MODEL_NAME}\",\"stream\":false}"
fi

curl -fsS http://127.0.0.1:21434/api/embed \
  -H 'Content-Type: application/json' \
  -d "{\"model\":\"${MODEL_NAME}\",\"input\":\"resident warmup\"}" >/dev/null

curl -fsS http://127.0.0.1:21434/api/ps
EOF
```

`bge-m3` is a multilingual text embedding model. It is suitable for WeKnora's Ollama embedding path and does not require multimodal embedding support.

With `OLLAMA_KEEP_ALIVE=-1`, the warmup embedding call above loads `bge-m3:latest` and keeps it resident. On the prepared host, `GET /api/ps` showed `bge-m3:latest` loaded with a far-future `expires_at`.

## Verify

Run on the remote target:

```bash
ssh tc232 'bash -s' <<'EOF'
set -euo pipefail

curl -fsS http://127.0.0.1:21434/api/tags
curl -fsS http://127.0.0.1:21535/v1/models

curl -fsS http://127.0.0.1:21434/api/embed \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":["hello world","中文知识库检索测试"]}' \
  > /tmp/ollama_embed.json

python3 -c 'import json; d=json.load(open("/tmp/ollama_embed.json")); e=d["embeddings"]; print(len(e), len(e[0]))'

curl -fsS http://127.0.0.1:21535/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":["hello world","中文知识库检索测试"]}' \
  > /tmp/openai_embed.json

python3 -c 'import json; d=json.load(open("/tmp/openai_embed.json")); e=d["data"]; print(len(e), len(e[0]["embedding"]))'
EOF
```

Expected embedding shape:

```text
2 1024
2 1024
```

The prepared remote host has verified both:

- Ollama native embedding API: `http://127.0.0.1:21434/api/embed`
- OpenAI-compatible embedding API: `http://127.0.0.1:21535/v1/embeddings`

## WeKnora Configuration

For WeKnora's local Ollama embedding provider, point `OLLAMA_BASE_URL` at the Ollama native API endpoint visible from the WeKnora container or host:

```env
OLLAMA_BASE_URL=http://<ollama-endpoint>:21434
```

Then create or select an embedding model record with:

```text
source: local
model: bge-m3:latest
dimensions: 1024
```

If WeKnora runs on the same remote host outside Docker, use `http://127.0.0.1:21434`. If it runs in Docker on the same Docker network, use `http://weknora-model-hub-ollama:11434`. For callers outside the remote machine, replace `127.0.0.1:21434` or `127.0.0.1:21535` with the external endpoint created by the operator.

If `OLLAMA_BASE_URL` uses `http://host.docker.internal:<port>`, the app
container must resolve the Docker host gateway and `SSRF_WHITELIST_EXTRA` must
keep `host.docker.internal`. The base `docker-compose.yml` includes it by
default; if you override the variable, include it explicitly:

```env
SSRF_WHITELIST_EXTRA=host.docker.internal,searxng,qdrant,milvus,weaviate,doris-fe
```

Make sure the port matches the running container. For
`weknora-model-hub-ollama`, `21434` is the native Ollama API and `21535` is the
OpenAI-compatible gateway. WeKnora `source=local` Ollama rows use
`OLLAMA_BASE_URL=http://host.docker.internal:21434`; only `source=remote`
embedding-gateway rows should use `http://host.docker.internal:21535/v1`.

## All-Ollama Model Configuration

WeKnora can use one Ollama-backed service for chat, image understanding, and
embedding. The preferred deployment mode uses WeKnora's local Ollama model
source and the native Ollama API:

```text
Ollama native API, used by OLLAMA_BASE_URL and the Ollama status page:
http://host.docker.internal:21434
```

For a Docker deployment, set the app environment to the native endpoint:

```yaml
services:
  app:
    environment:
      OLLAMA_BASE_URL: http://host.docker.internal:21434
```

No concrete model rows are shipped in the WeKnora image or default compose
files. Add the model rows in the Web UI, or mount an operator-created
`config/builtin_models.yaml` that is driven by environment variables.

For local Ollama rows:

- `source` must be `local`;
- `name` is the Ollama tag, for example `qwen3.5:2b`;
- `parameters.base_url` and `parameters.api_key` stay empty;
- WeKnora uses `OLLAMA_BASE_URL` for chat, VLM, and embedding calls.

Example `.env`:

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

Mount this file explicitly only when you want YAML-managed model rows:

```yaml
services:
  app:
    volumes:
      - ./config/builtin_models.yaml:/app/config/builtin_models.yaml:ro
```

The Ollama service must have these models prepared before WeKnora can use this
configuration:

```bash
ollama pull qwen3.5:2b
ollama pull bge-m3
```

`qwen3.5:2b` is used for both chat and VLM/image understanding, so it must show
vision and completion capability in `GET /api/tags`. `bge-m3:latest` is a
multilingual embedding model and should report `embedding_length=1024`.

Rerank needs a separate rerank-capable endpoint. WeKnora's generic rerank client
calls:

```text
POST <base_url>/rerank
```

with a request body containing `model`, `query`, and `documents`. Plain Ollama
native APIs do not provide that endpoint. To make rerank also "Ollama-backed",
prepare both of the following:

- a rerank model in Ollama or a rerank sidecar that can score query/document
  pairs, such as a BGE reranker family model;
- an OpenAI-style gateway endpoint that exposes `/v1/rerank` and returns
  `results[].index` plus `results[].relevance_score`.

Only after that endpoint exists should a WeKnora `Rerank` model be added in the
UI or declared in a mounted `builtin_models.yaml`:

```yaml
  - id: ictrek-bge-reranker
    type: Rerank
    source: remote
    is_default: true
    name: bge-reranker-v2-m3
    display_name: BGE Reranker
    parameters:
      base_url: http://host.docker.internal:11535/v1
      api_key: EMPTY
      provider: generic
```

If the Ollama gateway only exposes `/v1/models`, `/v1/chat/completions`, and
`/v1/embeddings`, leave rerank unconfigured or use an external rerank provider.
Configuring a `Rerank` row without a working `/v1/rerank` endpoint will let the
service start, but rerank calls will fail during retrieval.

After changing `config/builtin_models.yaml`, restart only the app service:

```bash
docker compose restart app
```

Verify the effective state:

```bash
curl -fsS http://127.0.0.1:21434/api/tags
curl -fsS http://127.0.0.1:21434/api/embed \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":["中文知识库检索测试"]}'
docker compose logs app | grep -i builtin-models
```

## Runtime Notes

- `model-hub:amd_20260625` was found and can be pulled, but the persistent runtime does not need a separate model-hub container for WeKnora embedding.
- `ollama/ollama:latest` could not be pulled on the prepared remote host because Docker Hub timed out, so `ollama_server:amd_0.30.6` was used as the Ollama engine.
- The downloaded `bge-m3:latest` files are under `/data/jhu/models/ollama`; current disk usage after pull is about `1.1G`.
- `bge-m3:latest` reports `embedding_length=1024` and `capabilities=["embedding"]` from Ollama.
- `OLLAMA_KEEP_ALIVE=-1` was verified on the prepared host: after a warmup embedding request, `GET /api/ps` reported `bge-m3:latest` resident in VRAM.
