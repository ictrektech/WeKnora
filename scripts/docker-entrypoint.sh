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
# for the HybRAG Ollama defaults or provides a custom YAML payload.
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
    OLLAMA_QA_MODEL="${OLLAMA_QA_MODEL:-qwen3.5:2b}"
    OLLAMA_EMBEDDING_MODEL="${OLLAMA_EMBEDDING_MODEL:-bge-m3}"
    cat > "$RUNTIME_BUILTIN_MODELS_FILE" <<EOF
builtin_models:
  - id: hybrag-ollama-qwen35-2b-qa
    type: KnowledgeQA
    source: remote
    is_default: true
    name: ${OLLAMA_QA_MODEL}
    display_name: HybRAG Ollama QA (hybrag-ollama-qa)
    parameters:
      base_url: http://hybrag-ollama-qa:11535/v1
      api_key: EMPTY
      provider: generic
      supports_vision: true
      extra_config:
        thinking_control: think

  - id: hybrag-ollama-qwen35-2b-vlm
    type: VLLM
    source: remote
    is_default: true
    name: ${OLLAMA_QA_MODEL}
    display_name: HybRAG Ollama VLM (hybrag-ollama-qa)
    parameters:
      base_url: http://hybrag-ollama-qa:11535/v1
      api_key: EMPTY
      provider: generic
      supports_vision: true
      extra_config:
        thinking_control: think

  - id: hybrag-ollama-bge-m3-embedding
    type: Embedding
    source: remote
    is_default: true
    name: ${OLLAMA_EMBEDDING_MODEL}
    display_name: HybRAG Ollama Embedding (hybrag-ollama-embedding)
    parameters:
      base_url: http://hybrag-ollama-embedding:11535/v1
      api_key: EMPTY
      provider: generic
      embedding_parameters:
        dimension: 1024
        truncate_prompt_tokens: 8192
        supports_dimension_override: false
EOF
    export BUILTIN_MODELS_CONFIG="$RUNTIME_BUILTIN_MODELS_FILE"
fi

if [ -f "$RUNTIME_BUILTIN_MODELS_FILE" ]; then
    chown -R appuser:appuser "$RUNTIME_CONFIG_DIR" 2>/dev/null || true
fi

# ─── Drop privileges and exec the main process ───
if [ "${WEKNORA_RUN_AS_ROOT:-}" = "true" ]; then
    exec "$@"
fi

exec gosu appuser "$@"
