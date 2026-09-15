#!/usr/bin/env bash
# ictrek local development helper.
#
# This bundle is intentionally separate from the VOS package compose and from
# the upstream generic dev compose. The app and frontend run from source on
# the host; Docker only provides local infrastructure and, optionally, vLLM.

set -euo pipefail

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
ENV_FILE="${ICTREK_DEV_ENV_FILE:-$PROJECT_ROOT/.env}"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"
PROJECT_NAME="${ICTREK_DEV_COMPOSE_PROJECT:-weknora-ictrek-local-dev}"
DEFAULT_MODEL_CONFIG="ictrek.app/docs/local-dev/config/builtin_models.tc232.yaml"
DEFAULT_DEV_DATA_DIR="/data/hybrag-dev-data"

declare -a EXTERNAL_ICTREK_DEV_ENV_NAMES=()
declare -A EXTERNAL_ICTREK_DEV_ENV_VALUES=()
EXTERNAL_ICTREK_DEV_ENV_CAPTURED=0

log_info() {
    printf "%b\n" "${BLUE}[INFO]${NC} $*"
}

log_success() {
    printf "%b\n" "${GREEN}[SUCCESS]${NC} $*"
}

log_warning() {
    printf "%b\n" "${YELLOW}[WARNING]${NC} $*"
}

log_error() {
    printf "%b\n" "${RED}[ERROR]${NC} $*" >&2
}

show_help() {
    cat <<EOF
WeKnora ictrek local development helper

Usage:
  $0 setup [--model-config PATH]  Prepare the root .env for local development
  $0 start [--no-neo4j] [--build] Start local infrastructure
  $0 stop                         Stop and remove local containers/network
  $0 restart                      Restart local infrastructure
  $0 logs [SERVICE]               Follow infrastructure logs
  $0 status                       Show infrastructure status
  $0 app                          Run the Go backend from source
  $0 frontend                     Run the Vite frontend from source
  $0 start-vllm                   Start or reuse the optional QA vLLM container
  $0 stop-vllm                    Stop the QA vLLM container and keep it
  $0 restart-vllm                 Recreate the QA vLLM container with current parameters
  $0 start-rerank                 Start or reuse the local ReRank vLLM container
  $0 stop-rerank                  Stop the local ReRank vLLM container and keep it
  $0 restart-rerank               Recreate the local ReRank vLLM container
  $0 check                        Check configuration, containers and endpoints
  $0 help                         Show this help

Default endpoints:
  frontend  http://localhost:5173
  backend   http://localhost:8080
  postgres  127.0.0.1:15432
  redis     127.0.0.1:6380
  docreader 127.0.0.1:15051
  neo4j     bolt://127.0.0.1:27687 (HTTP: 127.0.0.1:27474)
  vLLM      http://127.0.0.1:38118/v1
  bge-m3    http://127.0.0.1:32223/v1 (external or separately started)
  rerank    http://127.0.0.1:32224 (optional vLLM)

Typical flow:
  $0 setup
  $0 start
  $0 app                         # terminal 2
  make dev-frontend              # terminal 3, or: $0 frontend
EOF
}

_source_env_file() {
    local source_file="$1"
    local temporary_file

    [ -f "$source_file" ] || return 1
    temporary_file="$(mktemp)"
    sed -e 's/\r$//' "$source_file" > "$temporary_file"
    set -a
    # shellcheck disable=SC1090
    source "$temporary_file"
    set +a
    rm -f "$temporary_file"
}

capture_external_ictrek_dev_env() {
    local name

    while IFS= read -r name; do
        case "$name" in
            ICTREK_DEV_*)
                if [[ -v "$name" ]]; then
                    EXTERNAL_ICTREK_DEV_ENV_NAMES+=("$name")
                    EXTERNAL_ICTREK_DEV_ENV_VALUES["$name"]="${!name}"
                fi
                ;;
        esac
    done < <(compgen -A variable ICTREK_DEV_)
}

restore_external_ictrek_dev_env() {
    local name

    for name in "${EXTERNAL_ICTREK_DEV_ENV_NAMES[@]}"; do
        printf -v "$name" '%s' "${EXTERNAL_ICTREK_DEV_ENV_VALUES[$name]}"
        export "$name"
    done
}

load_env() {
    [ -f "$ENV_FILE" ] || return 1
    if [ "$EXTERNAL_ICTREK_DEV_ENV_CAPTURED" -eq 0 ]; then
        capture_external_ictrek_dev_env
        EXTERNAL_ICTREK_DEV_ENV_CAPTURED=1
    fi
    _source_env_file "$ENV_FILE"
    if [ -f "$PROJECT_ROOT/.env.local" ] && [ "$ENV_FILE" != "$PROJECT_ROOT/.env.local" ]; then
        _source_env_file "$PROJECT_ROOT/.env.local"
    fi
    restore_external_ictrek_dev_env
}

ensure_env_file() {
    if [ -f "$ENV_FILE" ]; then
        return 0
    fi
    if [ "$ENV_FILE" != "$PROJECT_ROOT/.env" ]; then
        log_error "Environment file does not exist: $ENV_FILE"
        return 1
    fi
    [ -f "$PROJECT_ROOT/.env.example" ] || {
        log_error "Missing .env.example"
        return 1
    }
    cp "$PROJECT_ROOT/.env.example" "$ENV_FILE"
    log_success "Created $ENV_FILE from .env.example"
}

get_env_value() {
    local key="$1"
    awk -F= -v key="$key" '$1 == key { sub(/^[^=]*=/, ""); print; found=1; exit } END { if (!found) exit 1 }' "$ENV_FILE" 2>/dev/null || true
}

set_env_value() {
    local key="$1"
    local value="$2"
    local temporary_file

    temporary_file="$(mktemp)"
    awk -v key="$key" -v value="$value" '
        BEGIN { done = 0; pattern = "^[[:space:]]*#?[[:space:]]*" key "=" }
        $0 ~ pattern {
            if (!done) {
                print key "=" value
                done = 1
            }
            next
        }
        { print }
        END {
            if (!done) print key "=" value
        }
    ' "$ENV_FILE" > "$temporary_file"
    mv "$temporary_file" "$ENV_FILE"
}

ensure_csv_value() {
    local key="$1"
    local item="$2"
    local current

    current="$(get_env_value "$key")"
    case ",$current," in
        *,"$item",*) return 0 ;;
    esac
    if [ -n "$current" ]; then
        current="$current,$item"
    else
        current="$item"
    fi
    set_env_value "$key" "$current"
}

random_secret() {
    od -An -N16 -tx1 /dev/urandom | tr -d ' \n'
}

model_config_file() {
    local configured="${1:-${ICTREK_DEV_MODEL_CONFIG:-${BUILTIN_MODELS_CONFIG:-$DEFAULT_MODEL_CONFIG}}}"
    if [[ "$configured" = /* ]]; then
        printf '%s\n' "$configured"
    else
        printf '%s\n' "$PROJECT_ROOT/$configured"
    fi
}

refresh_config() {
    DEV_DATA_DIR="${ICTREK_DEV_DATA_DIR:-$DEFAULT_DEV_DATA_DIR}"
    if [[ "$DEV_DATA_DIR" != /* ]]; then
        DEV_DATA_DIR="$PROJECT_ROOT/$DEV_DATA_DIR"
    fi
    DEV_DB_PORT="${ICTREK_DEV_DB_PORT:-15432}"
    DEV_REDIS_PORT="${ICTREK_DEV_REDIS_PORT:-6380}"
    DEV_DOCREADER_PORT="${ICTREK_DEV_DOCREADER_PORT:-15051}"
    DEV_APP_PORT="${ICTREK_DEV_APP_PORT:-8080}"
    DEV_NEO4J_HTTP_PORT="${ICTREK_DEV_NEO4J_HTTP_PORT:-27474}"
    DEV_NEO4J_BOLT_PORT="${ICTREK_DEV_NEO4J_BOLT_PORT:-27687}"
    DEV_NEO4J_URI="${ICTREK_DEV_NEO4J_URI:-bolt://127.0.0.1:${DEV_NEO4J_BOLT_PORT}}"
    DEV_VLLM_PORT="${ICTREK_DEV_VLLM_PORT:-38118}"
    DEV_VLLM_BASE_URL="${ICTREK_DEV_VLLM_BASE_URL:-http://127.0.0.1:${DEV_VLLM_PORT}/v1}"
    DEV_BGE_VLLM_PORT="${ICTREK_DEV_BGE_VLLM_PORT:-32223}"
    DEV_BGE_VLLM_BASE_URL="${ICTREK_DEV_BGE_VLLM_BASE_URL:-http://127.0.0.1:${DEV_BGE_VLLM_PORT}/v1}"
    DEV_OLLAMA_BASE_URL="${ICTREK_DEV_OLLAMA_BASE_URL:-http://127.0.0.1:11434}"
    DEV_VLLM_CONTAINER="${ICTREK_DEV_VLLM_CONTAINER:-qwen35-9b-awq-vllm}"
    DEV_VLLM_IMAGE="${ICTREK_DEV_VLLM_IMAGE:-vllm/vllm-openai:v0.18.1-cu130}"
    DEV_VLLM_MODEL_DIR="${ICTREK_DEV_VLLM_MODEL_DIR:-/data/models/QuantTrio--Qwen3.5-9B-AWQ}"
    DEV_VLLM_MODEL_NAME="${ICTREK_DEV_VLLM_MODEL_NAME:-qwen3.5-9b-awq}"
    DEV_VLLM_NETWORK="${ICTREK_DEV_VLLM_NETWORK:-lexai}"
    DEV_VLLM_HF_HOME="${ICTREK_DEV_VLLM_HF_HOME:-/tmp/hf-home}"
    DEV_VLLM_SHM_SIZE="${ICTREK_DEV_VLLM_SHM_SIZE:-8g}"
    DEV_VLLM_SECURITY_OPT="${ICTREK_DEV_VLLM_SECURITY_OPT:-label=disable}"
    DEV_VLLM_STOP_TIMEOUT="${ICTREK_DEV_VLLM_STOP_TIMEOUT:-30}"
    DEV_VLLM_MAX_MODEL_LEN="${ICTREK_DEV_VLLM_MAX_MODEL_LEN:-65536}"
    DEV_VLLM_MAX_NUM_SEQS="${ICTREK_DEV_VLLM_MAX_NUM_SEQS:-20}"
    DEV_VLLM_MAX_NUM_BATCHED_TOKENS="${ICTREK_DEV_VLLM_MAX_NUM_BATCHED_TOKENS:-4096}"
    DEV_VLLM_GPU_MEMORY_UTILIZATION="${ICTREK_DEV_VLLM_GPU_MEMORY_UTILIZATION:-0.3}"
    DEV_RERANK_VLLM_PORT="${ICTREK_DEV_RERANK_VLLM_PORT:-32224}"
    DEV_RERANK_VLLM_BASE_URL="${ICTREK_DEV_RERANK_VLLM_BASE_URL:-http://127.0.0.1:${DEV_RERANK_VLLM_PORT}}"
    DEV_RERANK_VLLM_CONTAINER="${ICTREK_DEV_RERANK_VLLM_CONTAINER:-bge-reranker-v2-m3-vllm}"
    DEV_RERANK_VLLM_MODEL_DIR="${ICTREK_DEV_RERANK_VLLM_MODEL_DIR:-/data/models/bge-reranker-v2-m3}"
    DEV_RERANK_VLLM_MODEL_NAME="${ICTREK_DEV_RERANK_VLLM_MODEL_NAME:-bge-reranker-v2-m3}"
    DEV_RERANK_VLLM_MAX_MODEL_LEN="${ICTREK_DEV_RERANK_VLLM_MAX_MODEL_LEN:-8192}"
    DEV_RERANK_VLLM_MAX_NUM_SEQS="${ICTREK_DEV_RERANK_VLLM_MAX_NUM_SEQS:-16}"
    DEV_RERANK_VLLM_MAX_NUM_BATCHED_TOKENS="${ICTREK_DEV_RERANK_VLLM_MAX_NUM_BATCHED_TOKENS:-8192}"
    DEV_RERANK_VLLM_GPU_MEMORY_UTILIZATION="${ICTREK_DEV_RERANK_VLLM_GPU_MEMORY_UTILIZATION:-0.1}"
}

export_compose_env() {
    export ICTREK_DEV_DATA_DIR="$DEV_DATA_DIR"
    export ICTREK_DEV_DB_PORT="$DEV_DB_PORT"
    export ICTREK_DEV_REDIS_PORT="$DEV_REDIS_PORT"
    export ICTREK_DEV_DOCREADER_PORT="$DEV_DOCREADER_PORT"
    export ICTREK_DEV_NEO4J_HTTP_PORT="$DEV_NEO4J_HTTP_PORT"
    export ICTREK_DEV_NEO4J_BOLT_PORT="$DEV_NEO4J_BOLT_PORT"
}

compose() {
    docker compose --project-name "$PROJECT_NAME" --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"
}

check_docker() {
    if ! command -v docker >/dev/null 2>&1; then
        log_error "Docker is not installed"
        return 1
    fi
    if ! docker info >/dev/null 2>&1; then
        log_error "Docker daemon is not running"
        return 1
    fi
    if ! docker compose version >/dev/null 2>&1; then
        log_error "Docker Compose v2 is not available"
        return 1
    fi
}

wait_for_service() {
    local service="$1"
    local timeout="${2:-180}"
    local container_id status state deadline

    container_id="$(compose ps -q "$service" | tail -n 1)"
    [ -n "$container_id" ] || {
        log_error "Service is not running: $service"
        return 1
    }
    deadline=$((SECONDS + timeout))
    while [ "$SECONDS" -lt "$deadline" ]; do
        status="$(docker inspect "$container_id" --format '{{if .State.Health}}{{.State.Health.Status}}{{end}}' 2>/dev/null || true)"
        state="$(docker inspect "$container_id" --format '{{.State.Status}}' 2>/dev/null || true)"
        if [ "$status" = "healthy" ] || { [ -z "$status" ] && [ "$state" = "running" ]; }; then
            log_success "$service is ready"
            return 0
        fi
        if [ "$state" = "exited" ] || [ "$state" = "dead" ]; then
            log_error "$service stopped before becoming ready"
            return 1
        fi
        sleep 3
    done
    log_warning "$service did not become ready in ${timeout}s; inspect with: $0 logs $service"
    return 1
}

setup_env() {
    local requested_model_config="${ICTREK_DEV_MODEL_CONFIG:-$DEFAULT_MODEL_CONFIG}"
    local env_created=0
    local config_path

    while [ "$#" -gt 0 ]; do
        case "$1" in
            --model-config)
                [ "$#" -ge 2 ] || { log_error "--model-config requires a path"; return 1; }
                requested_model_config="$2"
                shift 2
                ;;
            *)
                log_error "Unknown setup option: $1"
                return 1
                ;;
        esac
    done

    cd "$PROJECT_ROOT"
    if [ ! -f "$ENV_FILE" ]; then
        ensure_env_file
        env_created=1
    fi
    load_env
    refresh_config
    config_path="$(model_config_file "$requested_model_config")"
    [ -f "$config_path" ] || {
        log_error "Model config not found: $config_path"
        return 1
    }

    if [ "$env_created" -eq 1 ]; then
        set_env_value DB_PASSWORD "$(random_secret)"
        set_env_value REDIS_PASSWORD "$(random_secret)"
        set_env_value JWT_SECRET "$(random_secret)"
        set_env_value TENANT_AES_KEY "$(random_secret)"
        set_env_value SYSTEM_AES_KEY "$(random_secret)"
        set_env_value NEO4J_PASSWORD "$(random_secret)"
    fi
    if [ -z "$(get_env_value DB_PASSWORD)" ]; then
        set_env_value DB_PASSWORD local-dev-postgres
    fi
    if [ -z "$(get_env_value REDIS_PASSWORD)" ]; then
        set_env_value REDIS_PASSWORD local-dev-redis
    fi
    if [ -z "$(get_env_value JWT_SECRET)" ]; then
        set_env_value JWT_SECRET "$(random_secret)"
    fi
    if [ -z "$(get_env_value TENANT_AES_KEY)" ]; then
        set_env_value TENANT_AES_KEY "$(random_secret)"
    fi
    if [ -z "$(get_env_value SYSTEM_AES_KEY)" ]; then
        set_env_value SYSTEM_AES_KEY "$(random_secret)"
    fi
    if [ -z "$(get_env_value NEO4J_PASSWORD)" ]; then
        set_env_value NEO4J_PASSWORD local-dev-neo4j
    fi

    set_env_value GIN_MODE debug
    set_env_value LOG_LEVEL debug
    set_env_value DB_DRIVER postgres
    set_env_value DB_HOST 127.0.0.1
    set_env_value DB_PORT "$DEV_DB_PORT"
    set_env_value DB_USER "${DB_USER:-postgres}"
    set_env_value DB_NAME "${DB_NAME:-WeKnora}"
    set_env_value RETRIEVE_DRIVER postgres
    set_env_value STORAGE_TYPE local
    set_env_value STREAM_MANAGER_TYPE redis
    set_env_value REDIS_ADDR "127.0.0.1:$DEV_REDIS_PORT"
    set_env_value REDIS_PORT "$DEV_REDIS_PORT"
    set_env_value DOCREADER_ADDR "127.0.0.1:$DEV_DOCREADER_PORT"
    set_env_value DOCREADER_PORT "$DEV_DOCREADER_PORT"
    set_env_value DOCREADER_TRANSPORT grpc
    set_env_value ICTREK_DEV_DATA_DIR "$DEV_DATA_DIR"
    set_env_value ICTREK_DEV_DB_PORT "$DEV_DB_PORT"
    set_env_value ICTREK_DEV_REDIS_PORT "$DEV_REDIS_PORT"
    set_env_value ICTREK_DEV_DOCREADER_PORT "$DEV_DOCREADER_PORT"
    set_env_value ICTREK_DEV_APP_PORT "$DEV_APP_PORT"
    set_env_value ICTREK_DEV_NEO4J_HTTP_PORT "$DEV_NEO4J_HTTP_PORT"
    set_env_value ICTREK_DEV_NEO4J_BOLT_PORT "$DEV_NEO4J_BOLT_PORT"
    set_env_value ICTREK_DEV_VLLM_PORT "$DEV_VLLM_PORT"
    set_env_value ICTREK_DEV_BGE_VLLM_PORT "$DEV_BGE_VLLM_PORT"
    if [ -z "${LOCAL_STORAGE_BASE_DIR:-}" ] || [ "$LOCAL_STORAGE_BASE_DIR" = "/data/files" ]; then
        set_env_value LOCAL_STORAGE_BASE_DIR "$DEV_DATA_DIR/files"
    else
        set_env_value LOCAL_STORAGE_BASE_DIR "$LOCAL_STORAGE_BASE_DIR"
    fi
    set_env_value SERVER_PORT "$DEV_APP_PORT"
    set_env_value APP_PORT "$DEV_APP_PORT"
    set_env_value VITE_DEV_PROXY_TARGET "http://127.0.0.1:$DEV_APP_PORT"
    set_env_value DEFAULT_LOCALE zh-CN
    set_env_value WEKNORA_LANGUAGE zh-CN
    set_env_value VITE_VOS_SSO_ENABLED false
    set_env_value WEKNORA_BOOTSTRAP_SYSTEM_ADMIN_EMAIL "${WEKNORA_BOOTSTRAP_SYSTEM_ADMIN_EMAIL:-admin@weknora.local}"
    set_env_value WEKNORA_AUTH_DEFAULT_TENANT_MODE create_personal
    set_env_value WEKNORA_TENANT_SELF_SERVICE_CREATION_ENABLED true
    if [ -z "$(get_env_value ICTREK_LEGAL_WORKSPACE_DEFAULT_ENABLED)" ]; then
        set_env_value ICTREK_LEGAL_WORKSPACE_DEFAULT_ENABLED false
    fi
    set_env_value DISABLE_REGISTRATION false
    set_env_value HYBRAG_VOS_SSO_ENABLED false
    set_env_value NEO4J_ENABLE true
    set_env_value ENABLE_GRAPH_RAG true
    set_env_value NEO4J_URI "$DEV_NEO4J_URI"
    set_env_value NEO4J_USERNAME "${NEO4J_USERNAME:-neo4j}"
    set_env_value BUILTIN_MODELS_CONFIG "${requested_model_config}"
    set_env_value ICTREK_DEV_VLLM_BASE_URL "$DEV_VLLM_BASE_URL"
    set_env_value ICTREK_DEV_MODEL_CONFIG "$requested_model_config"
    set_env_value ICTREK_DEV_VLLM_CONTAINER "$DEV_VLLM_CONTAINER"
    set_env_value ICTREK_DEV_VLLM_IMAGE "$DEV_VLLM_IMAGE"
    set_env_value ICTREK_DEV_VLLM_MODEL_DIR "$DEV_VLLM_MODEL_DIR"
    set_env_value ICTREK_DEV_VLLM_MODEL_NAME "$DEV_VLLM_MODEL_NAME"
    set_env_value ICTREK_DEV_VLLM_NETWORK "$DEV_VLLM_NETWORK"
    set_env_value ICTREK_DEV_VLLM_HF_HOME "$DEV_VLLM_HF_HOME"
    set_env_value ICTREK_DEV_VLLM_SHM_SIZE "$DEV_VLLM_SHM_SIZE"
    set_env_value ICTREK_DEV_VLLM_SECURITY_OPT "$DEV_VLLM_SECURITY_OPT"
    set_env_value ICTREK_DEV_VLLM_MAX_MODEL_LEN "$DEV_VLLM_MAX_MODEL_LEN"
    set_env_value ICTREK_DEV_VLLM_MAX_NUM_SEQS "$DEV_VLLM_MAX_NUM_SEQS"
    set_env_value ICTREK_DEV_VLLM_MAX_NUM_BATCHED_TOKENS "$DEV_VLLM_MAX_NUM_BATCHED_TOKENS"
    set_env_value ICTREK_DEV_VLLM_GPU_MEMORY_UTILIZATION "$DEV_VLLM_GPU_MEMORY_UTILIZATION"
    set_env_value ICTREK_DEV_RERANK_VLLM_PORT "$DEV_RERANK_VLLM_PORT"
    set_env_value ICTREK_DEV_RERANK_VLLM_BASE_URL "$DEV_RERANK_VLLM_BASE_URL"
    set_env_value ICTREK_DEV_RERANK_VLLM_CONTAINER "$DEV_RERANK_VLLM_CONTAINER"
    set_env_value ICTREK_DEV_RERANK_VLLM_MODEL_DIR "$DEV_RERANK_VLLM_MODEL_DIR"
    set_env_value ICTREK_DEV_RERANK_VLLM_MODEL_NAME "$DEV_RERANK_VLLM_MODEL_NAME"
    set_env_value ICTREK_DEV_RERANK_VLLM_MAX_MODEL_LEN "$DEV_RERANK_VLLM_MAX_MODEL_LEN"
    set_env_value ICTREK_DEV_RERANK_VLLM_MAX_NUM_SEQS "$DEV_RERANK_VLLM_MAX_NUM_SEQS"
    set_env_value ICTREK_DEV_RERANK_VLLM_MAX_NUM_BATCHED_TOKENS "$DEV_RERANK_VLLM_MAX_NUM_BATCHED_TOKENS"
    set_env_value ICTREK_DEV_RERANK_VLLM_GPU_MEMORY_UTILIZATION "$DEV_RERANK_VLLM_GPU_MEMORY_UTILIZATION"
    set_env_value ICTREK_DEV_BGE_VLLM_BASE_URL "$DEV_BGE_VLLM_BASE_URL"
    set_env_value ICTREK_DEV_BGE_VLLM_MODEL_NAME "${ICTREK_DEV_BGE_VLLM_MODEL_NAME:-bge-m3}"
    set_env_value ICTREK_DEV_OLLAMA_BASE_URL "$DEV_OLLAMA_BASE_URL"
    set_env_value OLLAMA_BASE_URL "$DEV_OLLAMA_BASE_URL"
    set_env_value ICTREK_DEV_OLLAMA_CHAT_MODEL_NAME "${ICTREK_DEV_OLLAMA_CHAT_MODEL_NAME:-qwen3.5:2b}"
    set_env_value ICTREK_DEV_OLLAMA_VLM_MODEL_NAME "${ICTREK_DEV_OLLAMA_VLM_MODEL_NAME:-qwen3.5:2b}"
    set_env_value ICTREK_DEV_OLLAMA_EMBEDDING_MODEL_NAME "${ICTREK_DEV_OLLAMA_EMBEDDING_MODEL_NAME:-bge-m3}"
    set_env_value WEKNORA_CHAT_MODEL_CONTEXT_TOKENS "$DEV_VLLM_MAX_MODEL_LEN"
    set_env_value WEKNORA_MAIN_QA_MODEL_CONCURRENCY "$DEV_VLLM_MAX_NUM_SEQS"
    set_env_value WEKNORA_MODEL_MAX_CONCURRENCY "${WEKNORA_MODEL_MAX_CONCURRENCY:-6}"
    set_env_value WEKNORA_CHAT_RESERVED_CONCURRENCY "${WEKNORA_CHAT_RESERVED_CONCURRENCY:-2}"
    set_env_value WEKNORA_ASYNQ_CORE_CONCURRENCY "${WEKNORA_ASYNQ_CORE_CONCURRENCY:-1}"
    set_env_value WEKNORA_ASYNQ_POSTPROCESS_CONCURRENCY "${WEKNORA_ASYNQ_POSTPROCESS_CONCURRENCY:-1}"
    set_env_value WEKNORA_ASYNQ_ENRICHMENT_CONCURRENCY "${WEKNORA_ASYNQ_ENRICHMENT_CONCURRENCY:-1}"
    set_env_value WEKNORA_ASYNQ_MAINTENANCE_CONCURRENCY "${WEKNORA_ASYNQ_MAINTENANCE_CONCURRENCY:-1}"
    set_env_value WEKNORA_ASYNQ_SHARED_CONCURRENCY "${WEKNORA_ASYNQ_SHARED_CONCURRENCY:-0}"
    set_env_value WEKNORA_WIKI_ASYNQ_CONCURRENCY "${WEKNORA_WIKI_ASYNQ_CONCURRENCY:-1}"
    set_env_value BATCH_EMBED_SIZE "${BATCH_EMBED_SIZE:-4}"
    set_env_value CONCURRENCY_POOL_SIZE "${CONCURRENCY_POOL_SIZE:-4}"
    set_env_value WEKNORA_REPARSE_INCOMPLETE_ON_START false
    set_env_value WEKNORA_TRIGGER_REPARSE_AFTER_DEPLOY false
    ensure_csv_value SSRF_WHITELIST localhost
    ensure_csv_value SSRF_WHITELIST 127.0.0.1
    ensure_csv_value SSRF_WHITELIST ::1

    log_success "Prepared ictrek local-dev environment"
    log_info "Model config: $requested_model_config"
    log_info "Data directory: $DEV_DATA_DIR"
    log_info "Next: $0 start, then $0 app and make dev-frontend"
}

start_services() {
    local include_neo4j=1
    local build=0
    local services=(postgres redis docreader)

    while [ "$#" -gt 0 ]; do
        case "$1" in
            --no-neo4j) include_neo4j=0 ;;
            --build) build=1 ;;
            *) log_error "Unknown start option: $1"; return 1 ;;
        esac
        shift
    done

    cd "$PROJECT_ROOT"
    load_env || { log_error "Missing $ENV_FILE; run: $0 setup"; return 1; }
    refresh_config
    export_compose_env
    check_docker
    mkdir -p "$DEV_DATA_DIR/postgres" "$DEV_DATA_DIR/redis" "$DEV_DATA_DIR/docreader" "$DEV_DATA_DIR/neo4j"
    if [ "$include_neo4j" -eq 1 ]; then
        services+=(neo4j)
    fi

    log_info "Starting ictrek local infrastructure with project $PROJECT_NAME"
    if [ "$build" -eq 1 ]; then
        compose up -d --build "${services[@]}"
    else
        compose up -d "${services[@]}"
    fi
    wait_for_service postgres 180 || true
    wait_for_service docreader 240 || true
    if [ "$include_neo4j" -eq 1 ]; then
        wait_for_service neo4j 240 || true
    fi
    log_success "Infrastructure started"
    printf '  PostgreSQL: 127.0.0.1:%s\n' "$DEV_DB_PORT"
    printf '  Redis:      127.0.0.1:%s\n' "$DEV_REDIS_PORT"
    printf '  DocReader:  127.0.0.1:%s\n' "$DEV_DOCREADER_PORT"
    if [ "$include_neo4j" -eq 1 ]; then
        printf '  Neo4j:      %s\n' "$DEV_NEO4J_URI"
    fi
}

stop_services() {
    cd "$PROJECT_ROOT"
    load_env || { log_warning "Missing $ENV_FILE; nothing to stop"; return 0; }
    refresh_config
    export_compose_env
    check_docker
    compose down
    log_success "ictrek local infrastructure stopped"
}

restart_services() {
    stop_services
    start_services
}

show_logs() {
    cd "$PROJECT_ROOT"
    load_env || { log_error "Missing $ENV_FILE; run: $0 setup"; return 1; }
    refresh_config
    export_compose_env
    check_docker
    compose logs -f "$@"
}

show_status() {
    cd "$PROJECT_ROOT"
    load_env || { log_error "Missing $ENV_FILE; run: $0 setup"; return 1; }
    refresh_config
    export_compose_env
    check_docker
    compose ps
}

anydoc_archive() {
    case "$(uname -s)-$(uname -m)" in
        Darwin-arm64) printf '%s\n' "$PROJECT_ROOT/third_party/anydoc-go/lib/darwin_arm64/libanydoc_go.a" ;;
        Darwin-x86_64) printf '%s\n' "$PROJECT_ROOT/third_party/anydoc-go/lib/darwin_amd64/libanydoc_go.a" ;;
        Linux-x86_64)
            if [ -f "$PROJECT_ROOT/third_party/anydoc-go/lib/linux_amd64_gnu/libanydoc_go.a" ]; then
                printf '%s\n' "$PROJECT_ROOT/third_party/anydoc-go/lib/linux_amd64_gnu/libanydoc_go.a"
            else
                printf '%s\n' "$PROJECT_ROOT/third_party/anydoc-go/lib/linux_amd64_musl/libanydoc_go.a"
            fi
            ;;
        Linux-aarch64)
            if [ -f "$PROJECT_ROOT/third_party/anydoc-go/lib/linux_arm64_gnu/libanydoc_go.a" ]; then
                printf '%s\n' "$PROJECT_ROOT/third_party/anydoc-go/lib/linux_arm64_gnu/libanydoc_go.a"
            else
                printf '%s\n' "$PROJECT_ROOT/third_party/anydoc-go/lib/linux_arm64_musl/libanydoc_go.a"
            fi
            ;;
        *) printf '%s\n' "" ;;
    esac
}

start_app() {
    cd "$PROJECT_ROOT"
    load_env || { log_error "Missing $ENV_FILE; run: $0 setup"; return 1; }
    refresh_config
    command -v go >/dev/null 2>&1 || { log_error "Go is not installed"; return 1; }

    export SERVER_PORT="$DEV_APP_PORT"
    export DB_HOST=127.0.0.1
    export DB_PORT="$DEV_DB_PORT"
    export REDIS_ADDR="127.0.0.1:$DEV_REDIS_PORT"
    export DOCREADER_ADDR="127.0.0.1:$DEV_DOCREADER_PORT"
    export DOCREADER_TRANSPORT=grpc
    export NEO4J_URI="$DEV_NEO4J_URI"
    if [ -z "${LOCAL_STORAGE_BASE_DIR:-}" ] || [ "$LOCAL_STORAGE_BASE_DIR" = "/data/files" ]; then
        export LOCAL_STORAGE_BASE_DIR="$DEV_DATA_DIR/files"
    elif [[ "$LOCAL_STORAGE_BASE_DIR" != /* ]]; then
        export LOCAL_STORAGE_BASE_DIR="$PROJECT_ROOT/$LOCAL_STORAGE_BASE_DIR"
    fi
    mkdir -p "$LOCAL_STORAGE_BASE_DIR"

    if [ -z "${GO_BUILD_TAGS+x}" ]; then
        local archive
        archive="$(anydoc_archive)"
        if [ -f "$archive" ]; then
            export GO_BUILD_TAGS=anydoc
            log_info "Detected anydoc static library; enabling -tags anydoc"
        fi
    fi

    export CGO_CFLAGS="-Wno-deprecated-declarations -Wno-gnu-folding-constant"
    if [ "$(uname)" = "Darwin" ]; then
        export CGO_LDFLAGS="-Wl,-no_warn_duplicate_libraries"
    fi

    log_info "Starting Go backend"
    log_info "Database: $DB_HOST:$DB_PORT"
    log_info "Redis: $REDIS_ADDR"
    log_info "DocReader: $DOCREADER_ADDR"
    log_info "Storage: $LOCAL_STORAGE_BASE_DIR"

    if command -v air >/dev/null 2>&1; then
        log_info "Air detected; Go changes will rebuild automatically"
        exec air
    fi

    local ldflags
    ldflags="$(./scripts/get_version.sh ldflags) -X 'google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn'"
    if [ -n "${GO_BUILD_TAGS:-}" ]; then
        exec go run -tags "$GO_BUILD_TAGS" -ldflags="$ldflags" ./cmd/server
    fi
    exec go run -ldflags="$ldflags" ./cmd/server
}

start_frontend() {
    cd "$PROJECT_ROOT"
    load_env || { log_error "Missing $ENV_FILE; run: $0 setup"; return 1; }
    refresh_config
    command -v npm >/dev/null 2>&1 || { log_error "npm is not installed"; return 1; }
    cd "$PROJECT_ROOT/frontend"
    if [ ! -d node_modules ]; then
        log_info "Installing frontend dependencies"
        npm install
    fi
    export VITE_DEV_PROXY_TARGET="http://127.0.0.1:$DEV_APP_PORT"
    export VITE_VOS_SSO_ENABLED="${VITE_VOS_SSO_ENABLED:-false}"
    log_info "Starting Vite at http://localhost:5173"
    log_info "API proxy target: $VITE_DEV_PROXY_TARGET"
    exec npm run dev
}

resolve_vllm_model_dir() {
    local candidate="$1"
    local resolved

    if [ -f "$candidate/config.json" ]; then
        printf '%s\n' "$candidate"
        return 0
    fi
    resolved="$(find "$candidate/snapshots" -mindepth 1 -maxdepth 1 -type d -exec test -f '{}/config.json' ';' -print 2>/dev/null | sort | tail -n 1 || true)"
    [ -n "$resolved" ] || return 1
    printf '%s\n' "$resolved"
}

wait_for_url() {
    local name="$1"
    local url="$2"
    local timeout="${3:-300}"
    local container="${4:-${DEV_VLLM_CONTAINER:-vLLM container}}"
    local deadline=$((SECONDS + timeout))

    if ! command -v curl >/dev/null 2>&1; then
        log_warning "curl is not installed; skipping $name readiness check"
        return 0
    fi
    while [ "$SECONDS" -lt "$deadline" ]; do
        if curl -fsS --max-time 5 "$url" >/dev/null 2>&1; then
            log_success "$name is ready: $url"
            return 0
        fi
        sleep 5
    done
    log_warning "$name did not become ready in ${timeout}s; inspect: docker logs $container"
    return 1
}

quote_shell_arg() {
    printf "%q" "$1"
}

vllm_shm_size_bytes() {
    local size
    local number
    local multiplier

    size="$(printf "%s" "$1" | tr '[:lower:]' '[:upper:]')"
    if command -v numfmt >/dev/null 2>&1 && numfmt --from=iec "$size" 2>/dev/null; then
        return 0
    fi

    case "$size" in
        *K) number="${size%K}"; multiplier=1024 ;;
        *M) number="${size%M}"; multiplier=$((1024 * 1024)) ;;
        *G) number="${size%G}"; multiplier=$((1024 * 1024 * 1024)) ;;
        *T) number="${size%T}"; multiplier=$((1024 * 1024 * 1024 * 1024)) ;;
        ''|*[!0-9]) return 1 ;;
        *) number="$size"; multiplier=1 ;;
    esac
    [ -n "$number" ] || return 1
    [[ "$number" =~ ^[0-9]+$ ]] || return 1
    printf '%s\n' "$((10#$number * multiplier))"
}

vllm_container_matches() {
    local container="$1"
    local image="$2"
    local network="$3"
    local port="$4"
    local shm_size="$5"
    local security_opt="$6"
    local resolved_model_dir="$7"
    local hf_home="$8"
    shift 8
    local expected_args
    local actual_args
    local expected_shm_size
    local actual_shm_size
    local actual_mount
    local actual_port
    local actual_devices

    # The image entrypoint contributes the leading serve argument to .Args.
    expected_args="$(printf '%s\n' serve "$@")"
    actual_args="$(docker inspect --format '{{range .Args}}{{println .}}{{end}}' "$container")" || return 1
    [ "$actual_args" = "$expected_args" ] || return 1
    [ "$(docker inspect --format '{{json .Config.Entrypoint}}' "$container")" = '["vllm","serve"]' ] || return 1

    [ "$(docker inspect --format '{{.Config.Image}}' "$container")" = "$image" ] || return 1
    [ "$(docker inspect --format '{{.HostConfig.NetworkMode}}' "$container")" = "$network" ] || return 1
    [ "$(docker inspect --format '{{.HostConfig.IpcMode}}' "$container")" = "host" ] || return 1
    [ "$(docker inspect --format '{{.HostConfig.Runtime}}' "$container")" = "nvidia" ] || return 1
    [ "$(docker inspect --format '{{.HostConfig.RestartPolicy.Name}}' "$container")" = "no" ] || return 1
    [ "$(docker inspect --format '{{.HostConfig.AutoRemove}}' "$container")" = "false" ] || return 1

    actual_devices="$(docker inspect --format '{{json .HostConfig.DeviceRequests}}' "$container")" || return 1
    [ "$actual_devices" = '[{"Driver":"","Count":-1,"DeviceIDs":null,"Capabilities":[["gpu"]],"Options":{}}]' ] || return 1

    expected_shm_size="$(vllm_shm_size_bytes "$shm_size")" || return 1
    actual_shm_size="$(docker inspect --format '{{.HostConfig.ShmSize}}' "$container")" || return 1
    [ "$actual_shm_size" = "$expected_shm_size" ] || return 1

    actual_mount="$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/model"}}{{printf "%s|%s" .Source .Mode}}{{end}}{{end}}' "$container")" || return 1
    [ "$actual_mount" = "${resolved_model_dir}|ro" ] || return 1

    if ! docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$container" | grep -Fxq "HF_HOME=$hf_home"; then
        return 1
    fi

    actual_port="$(docker inspect --format '{{range $port, $bindings := .HostConfig.PortBindings}}{{if eq $port "8000/tcp"}}{{range $bindings}}{{printf "%s:%s" .HostIp .HostPort}}{{end}}{{end}}{{end}}' "$container")" || return 1
    [ "$actual_port" = ":$port" ] || return 1

    [ "$(docker inspect --format '{{json .HostConfig.SecurityOpt}}' "$container")" = "[\"$security_opt\"]" ] || return 1
}

print_vllm_docker_command() {
    local container="$1"
    local image="$2"
    local network="$3"
    local port="$4"
    local shm_size="$5"
    local security_opt="$6"
    local resolved_model_dir="$7"
    local hf_home="$8"
    shift 8
    local arg

    printf '  docker run -d \\\n'
    printf '    --name %s \\\n' "$(quote_shell_arg "$container")"
    printf '    --gpus all \\\n'
    printf '    --runtime nvidia \\\n'
    printf '    --ipc host \\\n'
    printf '    --shm-size %s \\\n' "$(quote_shell_arg "$shm_size")"
    printf '    --security-opt %s \\\n' "$(quote_shell_arg "$security_opt")"
    printf '    --network %s \\\n' "$(quote_shell_arg "$network")"
    printf '    -p %s \\\n' "$(quote_shell_arg "$port:8000")"
    printf '    -v %s \\\n' "$(quote_shell_arg "${resolved_model_dir}:/model:ro")"
    printf '    -e %s \\\n' "$(quote_shell_arg "HF_HOME=$hf_home")"
    printf '    %s' "$(quote_shell_arg "$image")"
    for arg in "$@"; do
        printf ' \\\n    %s' "$(quote_shell_arg "$arg")"
    done
    printf '\n'
}

start_vllm() {
    local force_recreate=0
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --recreate) force_recreate=1 ;;
            *) log_error "Unknown start-vllm option: $1"; return 1 ;;
        esac
        shift
    done

    cd "$PROJECT_ROOT"
    load_env || { log_error "Missing $ENV_FILE; run: $0 setup"; return 1; }
    refresh_config
    check_docker
    [ -d "$DEV_VLLM_MODEL_DIR" ] || {
        log_error "Model directory not found: $DEV_VLLM_MODEL_DIR"
        log_error "Override with ICTREK_DEV_VLLM_MODEL_DIR=/path/to/model"
        return 1
    }

    local resolved_model_dir
    resolved_model_dir="$(resolve_vllm_model_dir "$DEV_VLLM_MODEL_DIR")" || {
        log_error "No config.json found under $DEV_VLLM_MODEL_DIR"
        return 1
    }
    docker network inspect "$DEV_VLLM_NETWORK" >/dev/null 2>&1 || {
        log_error "Docker network not found: $DEV_VLLM_NETWORK"
        return 1
    }

    local vllm_args=(
        --host 0.0.0.0
        --port 8000
        --model /model
        --max-model-len "$DEV_VLLM_MAX_MODEL_LEN"
        --max-num-batched-tokens "$DEV_VLLM_MAX_NUM_BATCHED_TOKENS"
        --gpu-memory-utilization "$DEV_VLLM_GPU_MEMORY_UTILIZATION"
        --served-model-name "$DEV_VLLM_MODEL_NAME"
        --trust-remote-code
        --max-num-seqs "$DEV_VLLM_MAX_NUM_SEQS"
        --reasoning-parser qwen3
        --tool-call-parser qwen3_xml
        --enable-auto-tool-choice
        --enforce-eager
        --enable-prefix-caching
        --enable-chunked-prefill
    )
    local docker_run_args=(
        docker run -d
        --name "$DEV_VLLM_CONTAINER"
        --gpus all
        --runtime nvidia
        --ipc host
        --shm-size "$DEV_VLLM_SHM_SIZE"
        --security-opt "$DEV_VLLM_SECURITY_OPT"
        --network "$DEV_VLLM_NETWORK"
        -p "$DEV_VLLM_PORT:8000"
        -v "${resolved_model_dir}:/model:ro"
        -e "HF_HOME=$DEV_VLLM_HF_HOME"
        "$DEV_VLLM_IMAGE"
        "${vllm_args[@]}"
    )

    log_info "Equivalent docker deployment command:"
    print_vllm_docker_command \
        "$DEV_VLLM_CONTAINER" "$DEV_VLLM_IMAGE" "$DEV_VLLM_NETWORK" \
        "$DEV_VLLM_PORT" "$DEV_VLLM_SHM_SIZE" "$DEV_VLLM_SECURITY_OPT" \
        "$resolved_model_dir" "$DEV_VLLM_HF_HOME" "${vllm_args[@]}"

    if docker ps -a --format '{{.Names}}' | grep -Fxq "$DEV_VLLM_CONTAINER"; then
        if [ "$force_recreate" -eq 1 ]; then
            log_info "Recreating $DEV_VLLM_CONTAINER with the current vLLM parameters"
            if docker ps --format '{{.Names}}' | grep -Fxq "$DEV_VLLM_CONTAINER"; then
                docker stop --time "$DEV_VLLM_STOP_TIMEOUT" "$DEV_VLLM_CONTAINER" >/dev/null
            fi
            docker rm "$DEV_VLLM_CONTAINER" >/dev/null
        elif vllm_container_matches \
            "$DEV_VLLM_CONTAINER" "$DEV_VLLM_IMAGE" "$DEV_VLLM_NETWORK" \
            "$DEV_VLLM_PORT" "$DEV_VLLM_SHM_SIZE" "$DEV_VLLM_SECURITY_OPT" \
            "$resolved_model_dir" "$DEV_VLLM_HF_HOME" "${vllm_args[@]}"; then
            if docker ps --format '{{.Names}}' | grep -Fxq "$DEV_VLLM_CONTAINER"; then
                log_success "vLLM container is already running with consistent parameters: $DEV_VLLM_CONTAINER"
            else
                docker start "$DEV_VLLM_CONTAINER" >/dev/null
                log_success "Started existing vLLM container with consistent parameters: $DEV_VLLM_CONTAINER"
            fi
            wait_for_url "vLLM" "${DEV_VLLM_BASE_URL%/}/models" "${ICTREK_DEV_VLLM_WAIT_SEC:-900}" "$DEV_VLLM_CONTAINER" || true
            return 0
        else
            log_error "Existing container $DEV_VLLM_CONTAINER has different startup parameters"
            log_error "Run $0 restart-vllm to recreate it with the current configuration"
            return 1
        fi
    fi

    log_info "Starting vLLM container $DEV_VLLM_CONTAINER"
    log_info "Model: $resolved_model_dir"
    log_info "vLLM tuning: max_model_len=$DEV_VLLM_MAX_MODEL_LEN, max_num_seqs=$DEV_VLLM_MAX_NUM_SEQS, gpu_memory_utilization=$DEV_VLLM_GPU_MEMORY_UTILIZATION"
    "${docker_run_args[@]}" >/dev/null
    log_success "Started vLLM on $DEV_VLLM_BASE_URL"
    wait_for_url "vLLM" "${DEV_VLLM_BASE_URL%/}/models" "${ICTREK_DEV_VLLM_WAIT_SEC:-900}" "$DEV_VLLM_CONTAINER" || true
}

stop_vllm() {
    cd "$PROJECT_ROOT"
    if [ -f "$ENV_FILE" ]; then
        load_env
    fi
    refresh_config
    check_docker

    if ! docker ps -a --format '{{.Names}}' | grep -Fxq "$DEV_VLLM_CONTAINER"; then
        log_warning "vLLM container does not exist: $DEV_VLLM_CONTAINER"
        return 0
    fi
    if docker ps --format '{{.Names}}' | grep -Fxq "$DEV_VLLM_CONTAINER"; then
        docker stop --time "$DEV_VLLM_STOP_TIMEOUT" "$DEV_VLLM_CONTAINER" >/dev/null
        log_success "Stopped vLLM container: $DEV_VLLM_CONTAINER"
    else
        log_info "vLLM container is already stopped: $DEV_VLLM_CONTAINER"
    fi
}

restart_vllm() {
    start_vllm --recreate
}

validate_rerank_vllm_port() {
    if [ "$DEV_RERANK_VLLM_PORT" = "$DEV_VLLM_PORT" ] || [ "$DEV_RERANK_VLLM_PORT" = "$DEV_BGE_VLLM_PORT" ]; then
        log_error "ReRank port $DEV_RERANK_VLLM_PORT conflicts with an existing model service port"
        log_error "Use ICTREK_DEV_RERANK_VLLM_PORT to select a free port"
        return 1
    fi
}

start_rerank_vllm() {
    local force_recreate=0
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --recreate) force_recreate=1 ;;
            *) log_error "Unknown start-rerank option: $1"; return 1 ;;
        esac
        shift
    done

    cd "$PROJECT_ROOT"
    load_env || { log_error "Missing $ENV_FILE; run: $0 setup"; return 1; }
    refresh_config
    check_docker
    validate_rerank_vllm_port || return 1
    [ -d "$DEV_RERANK_VLLM_MODEL_DIR" ] || {
        log_error "Model directory not found: $DEV_RERANK_VLLM_MODEL_DIR"
        log_error "Override with ICTREK_DEV_RERANK_VLLM_MODEL_DIR=/path/to/model"
        return 1
    }

    local resolved_model_dir
    resolved_model_dir="$(resolve_vllm_model_dir "$DEV_RERANK_VLLM_MODEL_DIR")" || {
        log_error "No config.json found under $DEV_RERANK_VLLM_MODEL_DIR"
        return 1
    }
    docker network inspect "$DEV_VLLM_NETWORK" >/dev/null 2>&1 || {
        log_error "Docker network not found: $DEV_VLLM_NETWORK"
        return 1
    }

    local vllm_args=(
        --host 0.0.0.0
        --port 8000
        --model /model
        --served-model-name "$DEV_RERANK_VLLM_MODEL_NAME"
        --runner pooling
        --max-model-len "$DEV_RERANK_VLLM_MAX_MODEL_LEN"
        --max-num-seqs "$DEV_RERANK_VLLM_MAX_NUM_SEQS"
        --max-num-batched-tokens "$DEV_RERANK_VLLM_MAX_NUM_BATCHED_TOKENS"
        --gpu-memory-utilization "$DEV_RERANK_VLLM_GPU_MEMORY_UTILIZATION"
        --trust-remote-code
    )
    local docker_run_args=(
        docker run -d
        --name "$DEV_RERANK_VLLM_CONTAINER"
        --gpus all
        --runtime nvidia
        --ipc host
        --shm-size "$DEV_VLLM_SHM_SIZE"
        --security-opt "$DEV_VLLM_SECURITY_OPT"
        --network "$DEV_VLLM_NETWORK"
        -p "$DEV_RERANK_VLLM_PORT:8000"
        -v "${resolved_model_dir}:/model:ro"
        -e "HF_HOME=$DEV_VLLM_HF_HOME"
        "$DEV_VLLM_IMAGE"
        "${vllm_args[@]}"
    )

    log_info "Equivalent docker deployment command:"
    print_vllm_docker_command \
        "$DEV_RERANK_VLLM_CONTAINER" "$DEV_VLLM_IMAGE" "$DEV_VLLM_NETWORK" \
        "$DEV_RERANK_VLLM_PORT" "$DEV_VLLM_SHM_SIZE" "$DEV_VLLM_SECURITY_OPT" \
        "$resolved_model_dir" "$DEV_VLLM_HF_HOME" "${vllm_args[@]}"

    if docker ps -a --format '{{.Names}}' | grep -Fxq "$DEV_RERANK_VLLM_CONTAINER"; then
        if [ "$force_recreate" -eq 1 ]; then
            log_info "Recreating $DEV_RERANK_VLLM_CONTAINER with the current vLLM parameters"
            if docker ps --format '{{.Names}}' | grep -Fxq "$DEV_RERANK_VLLM_CONTAINER"; then
                docker stop --time "$DEV_VLLM_STOP_TIMEOUT" "$DEV_RERANK_VLLM_CONTAINER" >/dev/null
            fi
            docker rm "$DEV_RERANK_VLLM_CONTAINER" >/dev/null
        elif vllm_container_matches \
            "$DEV_RERANK_VLLM_CONTAINER" "$DEV_VLLM_IMAGE" "$DEV_VLLM_NETWORK" \
            "$DEV_RERANK_VLLM_PORT" "$DEV_VLLM_SHM_SIZE" "$DEV_VLLM_SECURITY_OPT" \
            "$resolved_model_dir" "$DEV_VLLM_HF_HOME" "${vllm_args[@]}"; then
            if docker ps --format '{{.Names}}' | grep -Fxq "$DEV_RERANK_VLLM_CONTAINER"; then
                log_success "ReRank vLLM container is already running with consistent parameters: $DEV_RERANK_VLLM_CONTAINER"
            else
                docker start "$DEV_RERANK_VLLM_CONTAINER" >/dev/null
                log_success "Started existing ReRank vLLM container with consistent parameters: $DEV_RERANK_VLLM_CONTAINER"
            fi
            wait_for_url "ReRank vLLM" "${DEV_RERANK_VLLM_BASE_URL%/}/health" "${ICTREK_DEV_RERANK_VLLM_WAIT_SEC:-900}" "$DEV_RERANK_VLLM_CONTAINER" || true
            return 0
        else
            log_error "Existing container $DEV_RERANK_VLLM_CONTAINER has different startup parameters"
            log_error "Run $0 restart-rerank to recreate it with the current configuration"
            return 1
        fi
    fi

    log_info "Starting ReRank vLLM container $DEV_RERANK_VLLM_CONTAINER"
    log_info "Model: $resolved_model_dir"
    log_info "ReRank vLLM tuning: max_model_len=$DEV_RERANK_VLLM_MAX_MODEL_LEN, max_num_seqs=$DEV_RERANK_VLLM_MAX_NUM_SEQS, gpu_memory_utilization=$DEV_RERANK_VLLM_GPU_MEMORY_UTILIZATION"
    "${docker_run_args[@]}" >/dev/null
    log_success "Started ReRank vLLM on $DEV_RERANK_VLLM_BASE_URL"
    wait_for_url "ReRank vLLM" "${DEV_RERANK_VLLM_BASE_URL%/}/health" "${ICTREK_DEV_RERANK_VLLM_WAIT_SEC:-900}" "$DEV_RERANK_VLLM_CONTAINER" || true
}

stop_rerank_vllm() {
    cd "$PROJECT_ROOT"
    if [ -f "$ENV_FILE" ]; then
        load_env
    fi
    refresh_config
    check_docker

    if ! docker ps -a --format '{{.Names}}' | grep -Fxq "$DEV_RERANK_VLLM_CONTAINER"; then
        log_warning "ReRank vLLM container does not exist: $DEV_RERANK_VLLM_CONTAINER"
        return 0
    fi
    if docker ps --format '{{.Names}}' | grep -Fxq "$DEV_RERANK_VLLM_CONTAINER"; then
        docker stop --time "$DEV_VLLM_STOP_TIMEOUT" "$DEV_RERANK_VLLM_CONTAINER" >/dev/null
        log_success "Stopped ReRank vLLM container: $DEV_RERANK_VLLM_CONTAINER"
    else
        log_info "ReRank vLLM container is already stopped: $DEV_RERANK_VLLM_CONTAINER"
    fi
}

restart_rerank_vllm() {
    start_rerank_vllm --recreate
}

check_url() {
    local name="$1"
    local url="$2"
    if ! command -v curl >/dev/null 2>&1; then
        log_warning "curl is not installed; skipping $name"
    elif curl -fsS --max-time 5 "$url" >/dev/null 2>&1; then
        log_success "$name: ok ($url)"
    else
        log_warning "$name: unavailable ($url)"
    fi
}

check_port() {
    local name="$1"
    local host="$2"
    local port="$3"
    if ! command -v nc >/dev/null 2>&1; then
        log_warning "nc is not installed; skipping $name port check"
    elif nc -z -w 3 "$host" "$port" >/dev/null 2>&1; then
        log_success "$name: reachable ($host:$port)"
    else
        log_warning "$name: unavailable ($host:$port)"
    fi
}

check_setup() {
    local failed=0
    local config_path

    cd "$PROJECT_ROOT"
    if [ ! -f "$ENV_FILE" ]; then
        log_error "Missing $ENV_FILE; run: $0 setup"
        return 1
    fi
    load_env
    refresh_config
    config_path="$(model_config_file)"
    [ -f "$config_path" ] || { log_error "Missing model config: $config_path"; failed=1; }
    [ -w "$PROJECT_ROOT" ] || { log_error "Project root is not writable"; failed=1; }

    printf '  Model config: %s\n' "${BUILTIN_MODELS_CONFIG:-<unset>}"
    printf '  Data dir:     %s\n' "$DEV_DATA_DIR"
    printf '  Backend:      http://127.0.0.1:%s\n' "$DEV_APP_PORT"
    printf '  vLLM:         %s\n' "$DEV_VLLM_BASE_URL"
    printf '  bge-m3:       %s\n' "$DEV_BGE_VLLM_BASE_URL"
    printf '  ReRank vLLM:  %s\n' "$DEV_RERANK_VLLM_BASE_URL"
    printf '  Ollama:       %s\n' "$DEV_OLLAMA_BASE_URL"

    if check_docker; then
        export_compose_env
        if ! compose config >/dev/null; then
            log_error "Local-dev Compose configuration is invalid"
            failed=1
        fi
        compose ps || true
    else
        failed=1
    fi

    check_port PostgreSQL 127.0.0.1 "$DEV_DB_PORT"
    check_port Redis 127.0.0.1 "$DEV_REDIS_PORT"
    check_port DocReader 127.0.0.1 "$DEV_DOCREADER_PORT"
    check_port Neo4j 127.0.0.1 "$DEV_NEO4J_BOLT_PORT"
    check_url "vLLM models" "${DEV_VLLM_BASE_URL%/}/models"
    check_url "bge-m3 vLLM models" "${DEV_BGE_VLLM_BASE_URL%/}/models"
    check_url "ReRank vLLM health" "${DEV_RERANK_VLLM_BASE_URL%/}/health"
    check_url "Ollama tags" "${DEV_OLLAMA_BASE_URL%/}/api/tags"
    return "$failed"
}

command_name="${1:-help}"
shift || true
case "$command_name" in
    setup) setup_env "$@" ;;
    start) start_services "$@" ;;
    stop) stop_services "$@" ;;
    restart) restart_services "$@" ;;
    logs) show_logs "$@" ;;
    status) show_status "$@" ;;
    app) start_app "$@" ;;
    frontend) start_frontend "$@" ;;
    start-vllm) start_vllm "$@" ;;
    stop-vllm) stop_vllm "$@" ;;
    restart-vllm) restart_vllm "$@" ;;
    start-rerank) start_rerank_vllm "$@" ;;
    stop-rerank) stop_rerank_vllm "$@" ;;
    restart-rerank) restart_rerank_vllm "$@" ;;
    check) check_setup "$@" ;;
    help|-h|--help) show_help ;;
    *) log_error "Unknown command: $command_name"; show_help; exit 1 ;;
esac
