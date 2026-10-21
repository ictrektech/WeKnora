#!/bin/bash
set -e

# ─── Fix ownership of bind-mounted directories ───
# When users bind-mount host directories (e.g. ./data/files),
# the mount inherits the host UID/GID which may differ from the
# container's appuser. This entrypoint runs as root, fixes ownership,
# then drops privileges to appuser via gosu — the same pattern used
# by official postgres/redis images.

# Directories that may be bind-mounted and need appuser access
MOUNT_DIRS=(
    /data/files
)

mkdir -p /data
chown appuser:appuser /data 2>/dev/null || true

for dir in "${MOUNT_DIRS[@]}"; do
    if [ -d "$dir" ]; then
        chown -R appuser:appuser "$dir" 2>/dev/null || true
    fi
done

# ─── Merge built-in skills into preloaded ───
# Built-in skills are backed up at /app/skills/_builtin during image build.
# After a bind-mount replaces /app/skills/preloaded, copy back any
# missing built-in skills (without overwriting user-provided ones).
BUILTIN_DIR="/app/skills/_builtin"
PRELOADED_DIR="/app/skills/preloaded"

if [ -d "$BUILTIN_DIR" ]; then
    mkdir -p "$PRELOADED_DIR"
    for skill_dir in "$BUILTIN_DIR"/*/; do
        [ -d "$skill_dir" ] || continue
        skill_name="$(basename "$skill_dir")"
        if [ ! -d "$PRELOADED_DIR/$skill_name" ]; then
            cp -r "$skill_dir" "$PRELOADED_DIR/$skill_name"
        fi
    done
    chown -R appuser:appuser "$PRELOADED_DIR"
fi

# ─── Optional runtime built-in model config ───
# VOS app packages cannot ship arbitrary top-level directories in app.tar.gz.
# Generate this file at container startup when the deployment explicitly asks
# for the HybRAG Model Hub defaults or provides a custom YAML payload.
RUNTIME_CONFIG_DIR="${WEKNORA_RUNTIME_CONFIG_DIR:-/tmp/weknora-config}"
RUNTIME_BUILTIN_MODELS_FILE="$RUNTIME_CONFIG_DIR/builtin_models.yaml"

if [ -n "${HYBRAG_BUILTIN_MODELS_YAML:-}" ]; then
    mkdir -p "$RUNTIME_CONFIG_DIR"
    python3 - "$RUNTIME_BUILTIN_MODELS_FILE" <<'PY'
import os
import sys

output = sys.argv[1]
payload = os.environ.get("HYBRAG_BUILTIN_MODELS_YAML", "")
with open(output, "w", encoding="utf-8") as f:
    f.write(os.path.expandvars(payload))
    if not payload.endswith("\n"):
        f.write("\n")
PY
    export BUILTIN_MODELS_CONFIG="$RUNTIME_BUILTIN_MODELS_FILE"
elif [ "${HYBRAG_DEFAULT_BUILTIN_MODELS:-false}" = "true" ]; then
    mkdir -p "$RUNTIME_CONFIG_DIR"
    cat > "$RUNTIME_BUILTIN_MODELS_FILE" <<EOF
builtin_models:
  - id: hybrag-ollama-qwen35-2b-qa
    type: KnowledgeQA
    source: remote
    is_default: true
    name: qwen3.5:2b
    display_name: Model Hub Ollama QA (model-hub-ollama-qa)
    parameters:
      base_url: http://model-hub-ollama-qa:11434/v1
      api_key: EMPTY
      provider: generic
      supports_vision: true
      extra_config:
        thinking_control: reasoning_effort

  - id: hybrag-ollama-qwen35-2b-vlm
    type: VLLM
    source: remote
    is_default: true
    name: qwen3.5:2b
    display_name: Model Hub Ollama VLM (model-hub-ollama-qa)
    parameters:
      base_url: http://model-hub-ollama-qa:11434/v1
      api_key: EMPTY
      provider: generic
      supports_vision: true
      extra_config:
        thinking_control: reasoning_effort

  - id: hybrag-ollama-bge-m3-embedding
    type: Embedding
    source: remote
    is_default: true
    name: bge-m3
    display_name: Model Hub Ollama Embedding (model-hub-ollama-embedding)
    parameters:
      base_url: http://model-hub-ollama-embedding:11434/v1
      api_key: EMPTY
      provider: generic
      embedding_parameters:
        dimension: 1024
        truncate_prompt_tokens: 8192
        supports_dimension_override: false

  - id: hybrag-ollama-bge-reranker-v2-m3-rerank
    type: Rerank
    source: remote
    is_default: true
    name: qllama/bge-reranker-v2-m3:q8_0
    display_name: Model Hub Ollama ReRank (model-hub-ollama-rerank)
    parameters:
      base_url: http://model-hub-ollama-rerank:11434/v1
      api_key: EMPTY
      provider: generic
      extra_config:
        score_scale: probability
EOF
    python3 - "$RUNTIME_BUILTIN_MODELS_FILE" <<'PYBACKEND'
import json
import os
import pathlib
import platform
import socket
import sys
import urllib.request

backend = os.environ.get("HYBRAG_MODEL_BACKEND", "model-hub")
if backend not in {"auto", "scheduler", "model-hub"}:
    raise SystemExit("HYBRAG_MODEL_BACKEND must be auto, scheduler or model-hub")
if backend in {"auto", "scheduler", "model-hub"}:
    path = pathlib.Path(sys.argv[1])
    text = path.read_text()
    base = os.environ.get("HYBRAG_V_ROUTER_URL", "http://v-scheduler:8080").rstrip("/")
    key = os.environ.get("HYBRAG_V_ROUTER_API_KEY", "")
    architecture = "arm" if platform.machine().lower() in {"aarch64", "arm64"} else "amd"
    legacy_default = "http://v-router-backend-" + architecture + ":8080"
    if backend == "model-hub" and os.environ.get("HYBRAG_V_ROUTER_URL") not in {None, "", "http://v-scheduler:8080", "http://v-scheduler-gateway:8080"}:
        legacy_default = os.environ["HYBRAG_V_ROUTER_URL"]
    legacy_base = (os.environ.get("HYBRAG_LEGACY_V_ROUTER_URL") or legacy_default).rstrip("/")
    legacy_key = os.environ.get("HYBRAG_LEGACY_V_ROUTER_API_KEY") or (key if backend == "model-hub" else "")
    selected_models = {role: os.environ.get("HYBRAG_" + role.upper() + "_MODEL", native) for role, native in (("qa", "qwen3.5:2b"), ("embedding", "bge-m3"), ("rerank", "qllama/bge-reranker-v2-m3:q8_0"))}
    selected_models = {role: model if ":" in model.rsplit("/", 1)[-1] else model + ":latest" for role, model in selected_models.items()}
    published = set()
    if backend in {"auto", "model-hub"}:
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, *args, **kwargs):
                return None
        headers = {"Authorization": "Bearer " + legacy_key} if legacy_key else {}
        try:
            request = urllib.request.Request(legacy_base + "/v1/models", headers=headers)
            with urllib.request.build_opener(NoRedirect).open(request, timeout=10) as response:
                body = response.read(4194305)
            if len(body) > 4194304:
                raise ValueError()
            published = {row["id"] for row in json.loads(body)["data"]}
            if backend == "model-hub" and not {"model-hub/" + model for model in selected_models.values()} <= published:
                raise ValueError()
        except Exception:
            if backend == "model-hub":
                raise SystemExit("Legacy Model Hub mode requires an authenticated V-Router publishing all selected model-hub/<native> models; direct Ollama fallback is disabled") from None
    scheduler_available = backend == "scheduler"
    if backend == "auto":
        try:
            socket.getaddrinfo("v-scheduler", 8080)
            scheduler_available = True
        except socket.gaierror:
            pass
    entries = []
    for target in (["scheduler"] if scheduler_available else []) + (["model-hub"] if backend == "model-hub" or published else []):
        for entry in text.split("  - id: ")[1:]:
            role = "embedding" if "type: Embedding\n" in entry else "rerank" if "type: Rerank\n" in entry else "qa"
            selected = selected_models[role]
            qualified = "v-scheduler/ollama-" + role + "/" + selected if target == "scheduler" else "model-hub/" + selected
            if target == "model-hub" and qualified not in published:
                continue
            native = {"qa": "qwen3.5:2b", "embedding": "bge-m3", "rerank": "qllama/bge-reranker-v2-m3:q8_0"}[role]
            entry = entry.replace("name: " + native + "\n", "name: " + json.dumps(qualified) + "\n")
            entry = entry.replace("http://model-hub-ollama-" + role + ":11434/v1", (base if target == "scheduler" else legacy_base) + "/v1")
            entry = entry.replace("api_key: EMPTY", "api_key: " + json.dumps(key if target == "scheduler" else legacy_key))
            entry = entry.replace("Model Hub Ollama", "V-Scheduler Ollama" if target == "scheduler" else "Model Hub via V-Router")
            entry = entry.replace(" (model-hub-ollama-" + role + ")", "")
            if backend == "auto":
                entry = entry.replace("is_default: true", "is_default: false")
                if target == "model-hub" and scheduler_available:
                    entry = entry.replace("hybrag-ollama-", "hybrag-modelhub-", 1)
            entries.append("  - id: " + entry.rstrip() + "\n\n")
    path.write_text("builtin_models:\n" + "".join(entries) if entries else "builtin_models: []\n")
    path.chmod(0o600)
PYBACKEND
    if [ "${HYBRAG_MODEL_BACKEND:-model-hub}" = "scheduler" ] || [ "${HYBRAG_MODEL_BACKEND:-model-hub}" = "model-hub" ] || [ "${HYBRAG_MODEL_BACKEND:-model-hub}" = "auto" ]; then
        export OLLAMA_BASE_URL="${HYBRAG_V_ROUTER_URL:-http://v-scheduler:8080}"
        if [ "${HYBRAG_MODEL_BACKEND:-model-hub}" = "model-hub" ]; then
            legacy_arch=amd
            case "$(uname -m)" in aarch64|arm64) legacy_arch=arm ;; esac
            legacy_url="http://v-router-backend-${legacy_arch}:8080"
            if [ -n "${HYBRAG_V_ROUTER_URL:-}" ] && [ "$HYBRAG_V_ROUTER_URL" != "http://v-scheduler:8080" ] && [ "$HYBRAG_V_ROUTER_URL" != "http://v-scheduler-gateway:8080" ]; then
                legacy_url="$HYBRAG_V_ROUTER_URL"
            fi
            export OLLAMA_BASE_URL="${HYBRAG_LEGACY_V_ROUTER_URL:-$legacy_url}"
        fi
        export WEKNORA_REPARSE_WAIT_URLS="${OLLAMA_BASE_URL}/healthz"
        if [ "${HYBRAG_MODEL_BACKEND:-model-hub}" = "auto" ]; then
            export WEKNORA_REPARSE_WAIT_URLS=""
        fi
        export SSRF_WHITELIST_EXTRA="${SSRF_WHITELIST_EXTRA:-},v-scheduler,v-router,v-router-backend,v-scheduler-model-hub,v-scheduler-gateway,v-router-backend-amd,v-router-backend-arm"
    fi
    export BUILTIN_MODELS_CONFIG="$RUNTIME_BUILTIN_MODELS_FILE"
fi

if [ -f "$RUNTIME_BUILTIN_MODELS_FILE" ]; then
    chown -R appuser:appuser "$RUNTIME_CONFIG_DIR" 2>/dev/null || true
fi

# ─── Docker socket access for the sandbox backend ───
# The Engine API socket is typically root:docker 0660. This process then
# drops to appuser via gosu, which calls initgroups and therefore drops
# compose group_add. Match the socket's GID in /etc/group before gosu.
# Never chmod the host socket: that would weaken daemon access on the host.
grant_docker_sock_to_appuser() {
    local sock="$1"
    local gid grp
    [ -S "$sock" ] || return 0
    if gosu appuser sh -c "test -r \"$sock\" && test -w \"$sock\"" 2>/dev/null; then
        return 0
    fi
    gid="$(stat -c '%g' "$sock" 2>/dev/null || true)"
    if [ -z "$gid" ]; then
        echo "weknora: cannot stat $sock; Docker sandbox may be unable to reach the daemon" >&2
        return 0
    fi
    if [ "$gid" = "0" ]; then
        echo "weknora: $sock is not writable by appuser and owned by GID 0; Docker sandbox needs a group-writable socket with a non-root GID" >&2
        return 0
    fi
    if ! getent group "$gid" >/dev/null 2>&1; then
        if ! groupadd -g "$gid" dockersock >/dev/null 2>&1; then
            echo "weknora: failed to create group for $sock GID $gid; Docker sandbox may be unable to reach the daemon" >&2
            return 0
        fi
    fi
    grp="$(getent group "$gid" | cut -d: -f1)"
    if [ -z "$grp" ]; then
        echo "weknora: no group name for GID $gid on $sock" >&2
        return 0
    fi
    if ! usermod -aG "$grp" appuser >/dev/null 2>&1; then
        echo "weknora: failed to add appuser to $grp for $sock; Docker sandbox may be unable to reach the daemon" >&2
        return 0
    fi
}

grant_docker_sock_to_appuser /var/run/docker.sock
case "${DOCKER_HOST:-}" in
    unix://*)
        grant_docker_sock_to_appuser "${DOCKER_HOST#unix://}"
        ;;
esac

# ─── Drop privileges and exec the main process ───
if [ "${WEKNORA_RUN_AS_ROOT:-}" = "true" ]; then
    exec "$@"
fi

exec gosu appuser "$@"
