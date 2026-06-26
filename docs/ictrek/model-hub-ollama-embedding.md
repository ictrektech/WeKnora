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

## All-Ollama Model Configuration

WeKnora can use one Ollama-backed service for chat, image understanding, and
embedding when the Ollama container also exposes the OpenAI-compatible gateway.
Keep the two endpoint types separate:

```text
Ollama native API, used by OLLAMA_BASE_URL and the Ollama status page:
http://host.docker.internal:21434

OpenAI-compatible gateway, used by built-in model records:
http://host.docker.internal:11535/v1
```

For a Docker deployment, set the app environment to the native endpoint so the
Ollama status page can detect the service:

```yaml
services:
  app:
    environment:
      OLLAMA_BASE_URL: http://host.docker.internal:21434
```

Then declare the model records through `config/builtin_models.yaml` and mount
that file into the app container. Example:

```yaml
builtin_models:
  - id: ictrek-qwen35-2b
    type: KnowledgeQA
    source: remote
    is_default: true
    name: qwen3.5:2b
    display_name: Qwen3.5 2B Ollama
    parameters:
      base_url: http://host.docker.internal:11535/v1
      api_key: EMPTY
      provider: generic
      supports_vision: true

  - id: ictrek-qwen35-2b-vlm
    type: VLLM
    source: remote
    is_default: true
    name: qwen3.5:2b
    display_name: Qwen3.5 2B Ollama Vision
    parameters:
      base_url: http://host.docker.internal:11535/v1
      api_key: EMPTY
      provider: generic
      supports_vision: true

  - id: ictrek-bge-m3-embedding
    type: Embedding
    source: remote
    is_default: true
    name: bge-m3:latest
    display_name: BGE-M3 Ollama Embedding
    parameters:
      base_url: http://host.docker.internal:11535/v1
      api_key: EMPTY
      provider: generic
      embedding_parameters:
        dimension: 1024
        truncate_prompt_tokens: 8192
        supports_dimension_override: false
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

Only after that endpoint exists should a WeKnora `Rerank` built-in be declared:

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
curl -fsS http://127.0.0.1:11535/v1/models
curl -fsS http://127.0.0.1:11535/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":"中文知识库检索测试"}'
docker compose logs app | grep -i builtin-models
```

## Runtime Notes

- `model-hub:amd_20260625` was found and can be pulled, but the persistent runtime does not need a separate model-hub container for WeKnora embedding.
- `ollama/ollama:latest` could not be pulled on the prepared remote host because Docker Hub timed out, so `ollama_server:amd_0.30.6` was used as the Ollama engine.
- The downloaded `bge-m3:latest` files are under `/data/jhu/models/ollama`; current disk usage after pull is about `1.1G`.
- `bge-m3:latest` reports `embedding_length=1024` and `capabilities=["embedding"]` from Ollama.
- `OLLAMA_KEEP_ALIVE=-1` was verified on the prepared host: after a warmup embedding request, `GET /api/ps` reported `bge-m3:latest` resident in VRAM.
