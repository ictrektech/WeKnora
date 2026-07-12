#!/usr/bin/env bash
set -euo pipefail

SCRIPT_PATH="${BASH_SOURCE[0]:-$0}"
ROOT_DIR="$(cd "$(dirname "$SCRIPT_PATH")" && pwd)"
ENV_FILE="${ENV_FILE:-${ROOT_DIR}/.env}"
COMPOSE_FILE="${COMPOSE_FILE:-${ROOT_DIR}/docker-compose.yml}"
REPARSE_STATUSES="${REPARSE_STATUSES:-failed,pending,processing}"
REPARSE_BATCH_SIZE="${REPARSE_BATCH_SIZE:-200}"
REPARSE_WAIT_SECONDS="${REPARSE_WAIT_SECONDS:-120}"
REPARSE_READY_WAIT_SECONDS="${REPARSE_READY_WAIT_SECONDS:-300}"

log() { echo "[INFO] $*"; }
warn() { echo "[WARN] $*" >&2; }
die() { echo "[ERROR] $*" >&2; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing command: $1"
}

env_value() {
  local key="$1"
  local default="${2:-}"
  python3 - "$ENV_FILE" "$key" "$default" <<'PY'
from pathlib import Path
import sys

path, key, default = Path(sys.argv[1]), sys.argv[2], sys.argv[3]
value = default
if path.exists():
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        k, v = line.split("=", 1)
        if k.strip() == key:
            value = v.strip().strip('"').strip("'")
print(value)
PY
}

json_get_token() {
  python3 - <<'PY'
import json, sys
try:
    data = json.load(sys.stdin)
except Exception:
    raise SystemExit(1)
token = data.get("token") or data.get("data", {}).get("token") or ""
if not token:
    raise SystemExit(1)
print(token)
PY
}

json_payload() {
  local kb_id="$1"
  local ids_file="$2"
  python3 - "$kb_id" "$ids_file" <<'PY'
import json, sys
kb_id, ids_file = sys.argv[1], sys.argv[2]
with open(ids_file, "r", encoding="utf-8") as f:
    ids = [line.strip() for line in f if line.strip()]
print(json.dumps({"kb_id": kb_id, "ids": ids}, ensure_ascii=False))
PY
}

sql_list() {
  local postgres_cid="$1"
  local db_user="$2"
  local db_name="$3"
  local sql="$4"
  docker exec "$postgres_cid" psql -U "$db_user" -d "$db_name" -At -F $'\t' -c "$sql"
}

wait_for_token() {
  local api_url="$1"
  local token="${REPARSE_BEARER_TOKEN:-}"
  local deadline=$((SECONDS + REPARSE_WAIT_SECONDS))
  local body

  if [[ -n "$token" ]]; then
    echo "$token"
    return 0
  fi

  while (( SECONDS < deadline )); do
    body="$(curl -sS -X POST "${api_url}/api/v1/auth/auto-setup" \
      -H "Content-Type: application/json" \
      -d "{}" 2>/dev/null || true)"
    if token="$(printf '%s' "$body" | json_get_token 2>/dev/null)"; then
      echo "$token"
      return 0
    fi
    sleep 3
  done

  return 1
}

ready_http_code() {
  local url="$1"
  local code parsed host port path

  code="$(curl -sS -m 5 -o /dev/null -w "%{http_code}" "$url" 2>/dev/null || true)"
  if [[ "$code" =~ ^[0-9]+$ ]] && (( code >= 200 && code < 300 )); then
    echo "$code"
    return 0
  fi

  parsed="$(python3 - "$url" <<'PY' 2>/dev/null || true
from urllib.parse import urlparse
import sys

u = urlparse(sys.argv[1])
if u.scheme not in ("http", "https") or not u.hostname or not u.port:
    raise SystemExit(1)
print(u.hostname)
print(u.port)
print(u.path or "/")
PY
)"
  host="$(printf '%s\n' "$parsed" | sed -n '1p')"
  port="$(printf '%s\n' "$parsed" | sed -n '2p')"
  path="$(printf '%s\n' "$parsed" | sed -n '3p')"
  [[ -n "${host:-}" && -n "${port:-}" ]] || { echo "$code"; return 0; }

  if docker ps --format '{{.Names}}' | grep -Fxq "$host"; then
    docker exec "$host" python3 -c 'from http.client import HTTPConnection; import sys; port=int(sys.argv[1]); path=sys.argv[2]
try:
    conn=HTTPConnection("127.0.0.1", port, timeout=5); conn.request("GET", path); print(conn.getresponse().status)
except Exception:
    print("000")' "$port" "$path" 2>/dev/null || echo "$code"
    return 0
  fi
  echo "$code"
}

wait_for_ready_urls() {
  local urls="$1"
  local deadline url code
  urls="${urls//;/,}"
  [[ -n "${urls//,/}" ]] || return 0

  deadline=$((SECONDS + REPARSE_READY_WAIT_SECONDS))
  while (( SECONDS < deadline )); do
    local all_ready=1
    IFS=',' read -ra parts <<< "$urls"
    for url in "${parts[@]}"; do
      url="$(echo "$url" | xargs)"
      [[ -n "$url" ]] || continue
      code="$(ready_http_code "$url")"
      if ! [[ "$code" =~ ^[0-9]+$ ]] || (( code < 200 || code >= 300 )); then
        all_ready=0
        break
      fi
    done
    if [[ "$all_ready" == "1" ]]; then
      log "reparse dependencies are ready"
      return 0
    fi
    sleep 5
  done

  warn "reparse dependencies not ready within ${REPARSE_READY_WAIT_SECONDS}s; skip incomplete knowledge reparse"
  return 1
}

main() {
  require_cmd docker
  require_cmd curl
  require_cmd python3

  [[ -f "$ENV_FILE" ]] || die "env file not found: $ENV_FILE"
  [[ -f "$COMPOSE_FILE" ]] || die "compose file not found: $COMPOSE_FILE"

  local db_user db_name app_port api_url ready_urls postgres_cid token status_sql kb_ids tmp ids_file total count payload code
  db_user="$(env_value DB_USER postgres)"
  db_name="$(env_value DB_NAME WeKnora)"
  app_port="$(env_value APP_PORT 30081)"
  api_url="${WEKNORA_API_URL:-http://127.0.0.1:${app_port}}"
  ready_urls="${REPARSE_WAIT_URLS:-$(env_value WEKNORA_REPARSE_WAIT_URLS "")}"

  wait_for_ready_urls "$ready_urls" || return 0

  postgres_cid="$(docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" ps -q postgres)"
  [[ -n "$postgres_cid" ]] || die "postgres service is not running"

  token="$(wait_for_token "$api_url")" || {
    warn "cannot get auth token from ${api_url}; skip incomplete knowledge reparse"
    return 0
  }

  status_sql="$(
    python3 - "$REPARSE_STATUSES" <<'PY'
import sys
statuses = [s.strip() for s in sys.argv[1].split(",") if s.strip()]
if not statuses:
    raise SystemExit("empty REPARSE_STATUSES")
print(",".join("'" + s.replace("'", "''") + "'" for s in statuses))
PY
  )"

  kb_ids="$(sql_list "$postgres_cid" "$db_user" "$db_name" \
    "select knowledge_base_id from knowledges where deleted_at is null and parse_status in (${status_sql}) and (parse_status <> 'finalizing' or processed_at is null) group by knowledge_base_id order by knowledge_base_id")"

  if [[ -z "$kb_ids" ]]; then
    log "no incomplete knowledge to reparse"
    return 0
  fi

  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT

  while IFS= read -r kb_id; do
    [[ -n "$kb_id" ]] || continue
    ids_file="${tmp}/${kb_id}.ids"
    sql_list "$postgres_cid" "$db_user" "$db_name" \
      "select id from knowledges where deleted_at is null and knowledge_base_id='${kb_id//\'/\'\'}' and parse_status in (${status_sql}) and (parse_status <> 'finalizing' or processed_at is null) order by updated_at nulls first, created_at" \
      > "$ids_file"
    total="$(wc -l < "$ids_file" | tr -d ' ')"
    [[ "$total" != "0" ]] || continue
    log "reparse incomplete knowledge: kb=${kb_id} count=${total}"

    split -l "$REPARSE_BATCH_SIZE" "$ids_file" "${tmp}/${kb_id}-part-"
    for part in "${tmp}/${kb_id}"-part-*; do
      [[ -s "$part" ]] || continue
      count="$(wc -l < "$part" | tr -d ' ')"
      payload="$(json_payload "$kb_id" "$part")"
      code="$(curl -sS -o "${tmp}/response.json" -w "%{http_code}" \
        -X POST "${api_url}/api/v1/knowledge/batch-reparse" \
        -H "Authorization: Bearer ${token}" \
        -H "Content-Type: application/json" \
        -d "$payload")"
      if [[ "$code" != "200" ]]; then
        warn "batch reparse failed: kb=${kb_id} count=${count} http=${code} body=$(cat "${tmp}/response.json")"
        continue
      fi
      log "batch reparse submitted: kb=${kb_id} count=${count}"
    done
  done <<< "$kb_ids"
}

main "$@"
