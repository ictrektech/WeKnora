#!/bin/bash
set -e

# ─── Fix ownership of bind-mounted directories ───
# When users bind-mount host directories (e.g. ./skills/preloaded),
# the mount inherits the host UID/GID which may differ from the
# container's appuser. This entrypoint runs as root, fixes ownership,
# then drops privileges to appuser via gosu — the same pattern used
# by official postgres/redis images.

# Directories that may be bind-mounted and need appuser access
MOUNT_DIRS=(
    /app/skills/preloaded
    /data/files
)

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
      base_url: http://model-hub-ollama-qa:11535/v1
      api_key: EMPTY
      provider: generic
      supports_vision: true
      extra_config:
        thinking_control: think

  - id: hybrag-ollama-qwen35-2b-vlm
    type: VLLM
    source: remote
    is_default: true
    name: qwen3.5:2b
    display_name: Model Hub Ollama VLM (model-hub-ollama-qa)
    parameters:
      base_url: http://model-hub-ollama-qa:11535/v1
      api_key: EMPTY
      provider: generic
      supports_vision: true
      extra_config:
        thinking_control: think

  - id: hybrag-ollama-bge-m3-embedding
    type: Embedding
    source: remote
    is_default: true
    name: bge-m3
    display_name: Model Hub Ollama Embedding (model-hub-ollama-embedding)
    parameters:
      base_url: http://model-hub-ollama-embedding:11535/v1
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
      base_url: http://model-hub-ollama-rerank:11535
      api_key: EMPTY
      provider: ollama
      extra_config:
        ollama_rerank_template: "Query: {query}\nDocument: {document}"
EOF
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
