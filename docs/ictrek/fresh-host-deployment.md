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
