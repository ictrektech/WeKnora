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

## Runtime Notes

- `model-hub:amd_20260625` was found and can be pulled, but the persistent runtime does not need a separate model-hub container for WeKnora embedding.
- `ollama/ollama:latest` could not be pulled on the prepared remote host because Docker Hub timed out, so `ollama_server:amd_0.30.6` was used as the Ollama engine.
- The downloaded `bge-m3:latest` files are under `/data/jhu/models/ollama`; current disk usage after pull is about `1.1G`.
- `bge-m3:latest` reports `embedding_length=1024` and `capabilities=["embedding"]` from Ollama.
- `OLLAMA_KEEP_ALIVE=-1` was verified on the prepared host: after a warmup embedding request, `GET /api/ps` reported `bge-m3:latest` resident in VRAM.
