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

Optional profiles such as `qdrant`, `milvus`, `weaviate`, `minio`, `searxng`, `neo4j`, and `langfuse` should only be started when that feature is intentionally enabled.

## Existing Model Backends

The deployment expects the model backends documented in this directory:

- vLLM OpenAI-compatible LLM backend: `remote-vllm-backend.md`
- Ollama OpenAI-compatible embedding backend: `model-hub-ollama-embedding.md`

The app points to Ollama through the Docker host gateway:

```bash
OLLAMA_BASE_URL=http://host.docker.internal:21434
```

The default ictrek model records are declared in `config/builtin_models.yaml`
and mounted by `docker-compose.override.yml`. On every `app` startup WeKnora
upserts these records into the `models` table as YAML-managed built-ins:

```text
ictrek-qwen35-9b-awq      KnowledgeQA  qwen3.5-9b-awq  http://host.docker.internal:18118/v1
ictrek-bge-m3-embedding   Embedding    bge-m3:latest   http://host.docker.internal:21535/v1
```

Both are marked `is_default: true` and are visible to all tenants.

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
- `models` contains the YAML-managed built-ins `ictrek-qwen35-9b-awq` and
  `ictrek-bge-m3-embedding`

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
public LLM port -> tc232:18118    # direct vLLM OpenAI-compatible access, optional
```

Keep these internal by default:

```text
5432   # postgres
6379   # redis
50051  # docreader gRPC
21434  # Ollama native API
21535  # Ollama OpenAI-compatible embedding gateway
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
