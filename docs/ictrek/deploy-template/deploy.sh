#!/usr/bin/env bash
set -euo pipefail

SCRIPT_PATH="${BASH_SOURCE[0]:-$0}"
ROOT_DIR="$(cd "$(dirname "$SCRIPT_PATH")" && pwd)"
FEISHU_READ_CONFIG_FILE="${FEISHU_READ_CONFIG_FILE:-${HOME}/.feishu.components.json}"
FEISHU_CONFIG_FILE="${FEISHU_CONFIG_FILE:-${HOME}/.feishu.json}"
SPREADSHEET_TOKEN="${FEISHU_SPREADSHEET_TOKEN:-Htotsn3oahO1zxt73YMcaB1zn8e}"
REGISTRY="${REGISTRY:-swr.cn-southwest-2.myhuaweicloud.com/ictrek}"
ENV_FILE="${ENV_FILE:-${ROOT_DIR}/.env}"
COMPOSE_FILE="${COMPOSE_FILE:-${ROOT_DIR}/docker-compose.yml}"
PLATFORM=""
SHEET_TITLE=""
DRY_RUN=0

usage() {
  cat <<'EOF'
Usage: ./deploy.sh --platform amd|l4t|thor [--sheet SHEET] [--compose-file FILE] [--dry-run]

Looks up the latest WeKnora image tags in Feishu, writes them to .env, then runs docker compose up -d.
After compose is healthy, docreader and app are recreated and incomplete documents are reparse-submitted.
EOF
}

log() { echo "[INFO] $*"; }
die() { echo "[ERROR] $*" >&2; exit 1; }
require_cmd() { command -v "$1" >/dev/null 2>&1 || die "missing command: $1"; }

compose_has_service() {
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" config --services | grep -qx "$1"
}

wait_service_healthy() {
  local service="$1" timeout="${2:-180}" cid status deadline
  cid="$(docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" ps -q "$service")"
  [[ -n "$cid" ]] || die "service not running: $service"
  deadline=$((SECONDS + timeout))
  while (( SECONDS < deadline )); do
    status="$(docker inspect "$cid" --format '{{if .State.Health}}{{.State.Health.Status}}{{end}}' 2>/dev/null || true)"
    [[ -z "$status" || "$status" == "healthy" ]] && return 0
    sleep 3
  done
  die "service did not become healthy: $service"
}

read_feishu_field() {
  python3 - "$1" "$2" <<'PY'
import json, sys
with open(sys.argv[1], "r", encoding="utf-8") as f:
    data = json.load(f)
value = data.get(sys.argv[2], "")
print(value if isinstance(value, str) else str(value))
PY
}

feishu_json() {
  curl --fail -sS -X "$1" "$2" -H "Authorization: Bearer $3"
}

get_feishu_token() {
  local resp
  resp="$(curl --fail -sS -X POST "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal" \
    -H "Content-Type: application/json" \
    -d "{\"app_id\":\"$1\",\"app_secret\":\"$2\"}")"
  python3 - "$resp" <<'PY'
import json, sys
data = json.loads(sys.argv[1])
if data.get("code") != 0:
    raise SystemExit(f"get_feishu_token failed: {data}")
print(data["tenant_access_token"])
PY
}

get_sheet_id_by_title() {
  local resp
  resp="$(feishu_json GET "https://open.feishu.cn/open-apis/sheets/v3/spreadsheets/${SPREADSHEET_TOKEN}/sheets/query" "$1")"
  python3 - "$2" "$resp" <<'PY'
import json, sys
title, resp = sys.argv[1], sys.argv[2]
data = json.loads(resp)
if data.get("code") != 0:
    raise SystemExit(f"query sheets failed: {data}")
for sheet in data.get("data", {}).get("sheets", []):
    if sheet.get("title") == title:
        print(sheet["sheet_id"])
        raise SystemExit(0)
raise SystemExit(f"sheet title not found: {title}")
PY
}

resolve_feishu_reader() {
  local config app_id app_secret
  for config in "$FEISHU_READ_CONFIG_FILE" "$FEISHU_CONFIG_FILE"; do
    [[ -f "$config" ]] || continue
    app_id="$(read_feishu_field "$config" feishu_app_id)"
    app_secret="$(read_feishu_field "$config" feishu_app_secret)"
    [[ -n "$app_id" && -n "$app_secret" ]] || continue
    if TOKEN="$(get_feishu_token "$app_id" "$app_secret")" \
      && SHEET_ID="$(get_sheet_id_by_title "$TOKEN" "$SHEET_TITLE")"; then
      log "Feishu read config=${config}"
      return 0
    fi
  done
  die "No Feishu read config can read sheet ${SHEET_TITLE}"
}

column_letter() {
  python3 - "$1" <<'PY'
import sys
n = int(sys.argv[1])
out = ""
while n > 0:
    n, r = divmod(n - 1, 26)
    out = chr(ord("A") + r) + out
print(out)
PY
}

get_range_values() {
  feishu_json GET "https://open.feishu.cn/open-apis/sheets/v2/spreadsheets/${SPREADSHEET_TOKEN}/values/$2" "$1"
}

get_sheet_column_count() {
  local resp
  resp="$(feishu_json GET "https://open.feishu.cn/open-apis/sheets/v3/spreadsheets/${SPREADSHEET_TOKEN}/sheets/query" "$1")"
  python3 - "$2" "$resp" <<'PY'
import json, sys
sheet_id, resp = sys.argv[1], sys.argv[2]
data = json.loads(resp)
for sheet in data.get("data", {}).get("sheets", []):
    if sheet.get("sheet_id") == sheet_id:
        print(sheet.get("grid_properties", {}).get("column_count", 1))
        raise SystemExit(0)
raise SystemExit(f"sheet id not found: {sheet_id}")
PY
}

find_component_column_letter() {
  local end_col resp
  end_col="$(column_letter "$(get_sheet_column_count "$1" "$2")")"
  resp="$(get_range_values "$1" "${2}!A1:${end_col}1")"
  python3 - "$3" "$resp" <<'PY'
import json, sys
target, resp = sys.argv[1], sys.argv[2]
data = json.loads(resp)
row = (data.get("data", {}).get("valueRange", {}).get("values", []) or [[]])[0]
def text(v):
    if v is None: return ""
    if isinstance(v, str): return v.strip()
    if isinstance(v, dict): return str(v.get("text") or v.get("link") or "").strip()
    if isinstance(v, list): return "".join(text(x) for x in v).strip()
    return str(v).strip()
def col(n):
    out = ""
    while n > 0:
        n, r = divmod(n - 1, 26)
        out = chr(ord("A") + r) + out
    return out
for i, value in enumerate(row, start=1):
    if text(value) == target:
        print(col(i))
        raise SystemExit(0)
raise SystemExit(f"component column not found in row1: {target}")
PY
}

latest_tag_for_component() {
  local column resp
  column="$(find_component_column_letter "$1" "$2" "$3")"
  resp="$(get_range_values "$1" "${2}!${column}4:${column}2000")"
  python3 - "$resp" <<'PY'
import json, sys
data = json.loads(sys.argv[1])
for row in data.get("data", {}).get("valueRange", {}).get("values", []):
    if row and row[0] is not None and str(row[0]).strip():
        print(str(row[0]).strip())
        raise SystemExit(0)
raise SystemExit("latest version not found")
PY
}

write_env_value() {
  python3 - "$1" "$2" "$3" <<'PY'
from pathlib import Path
import sys
key, value, path = sys.argv[1], sys.argv[2], Path(sys.argv[3])
lines = path.read_text(encoding="utf-8").splitlines() if path.exists() else []
prefix = key + "="
out, done = [], False
for line in lines:
    if line.startswith(prefix):
        out.append(f"{key}={value}")
        done = True
    else:
        out.append(line)
if not done:
    out.append(f"{key}={value}")
path.write_text("\n".join(out) + "\n", encoding="utf-8")
PY
}

platform_sheet() {
  case "$1" in
    amd) echo "AMD_with_cuda" ;;
    l4t|arm) echo "l4t" ;;
    thor) echo "thor_spark" ;;
    *) die "unsupported platform: $1" ;;
  esac
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --platform) PLATFORM="$2"; shift 2 ;;
    --sheet) SHEET_TITLE="$2"; shift 2 ;;
    --compose-file) COMPOSE_FILE="$2"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1" ;;
  esac
done

[[ -n "$PLATFORM" ]] || die "--platform amd|l4t|thor is required"
SHEET_TITLE="${SHEET_TITLE:-$(platform_sheet "$PLATFORM")}"
require_cmd curl
require_cmd python3
resolve_feishu_reader

WEKNORA_APP_TAG="$(latest_tag_for_component "$TOKEN" "$SHEET_ID" weknora)"
WEKNORA_UI_TAG="$(latest_tag_for_component "$TOKEN" "$SHEET_ID" weknora-ui)"
WEKNORA_DOCREADER_TAG="$(latest_tag_for_component "$TOKEN" "$SHEET_ID" weknora-docreader)"
WEKNORA_APP_IMAGE="${REGISTRY}/weknora:${WEKNORA_APP_TAG}"
WEKNORA_UI_IMAGE="${REGISTRY}/weknora-ui:${WEKNORA_UI_TAG}"
WEKNORA_DOCREADER_IMAGE="${REGISTRY}/weknora-docreader:${WEKNORA_DOCREADER_TAG}"

log "sheet=${SHEET_TITLE}"
log "WEKNORA_APP_IMAGE=${WEKNORA_APP_IMAGE}"
log "WEKNORA_UI_IMAGE=${WEKNORA_UI_IMAGE}"
log "WEKNORA_DOCREADER_IMAGE=${WEKNORA_DOCREADER_IMAGE}"
[[ "$DRY_RUN" == "1" ]] && exit 0

require_cmd docker
write_env_value WEKNORA_APP_IMAGE "$WEKNORA_APP_IMAGE" "$ENV_FILE"
write_env_value WEKNORA_UI_IMAGE "$WEKNORA_UI_IMAGE" "$ENV_FILE"
write_env_value WEKNORA_DOCREADER_IMAGE "$WEKNORA_DOCREADER_IMAGE" "$ENV_FILE"

cd "$ROOT_DIR"
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d

if [[ "${WEKNORA_RECREATE_DOCREADER_ON_DEPLOY:-true}" != "false" ]] && compose_has_service docreader; then
  log "recreating docreader to clear stale parser process state"
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d --no-deps --force-recreate docreader
  wait_service_healthy docreader 180
  if compose_has_service app; then
    log "recreating app after docreader is healthy"
    docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d --no-deps --force-recreate app
    wait_service_healthy app 180
  fi
fi

if [[ "${WEKNORA_TRIGGER_REPARSE_AFTER_DEPLOY:-true}" != "false" && -x "${ROOT_DIR}/trigger-reparse-incomplete.sh" ]]; then
  log "triggering full-document reparse for incomplete knowledge"
  ENV_FILE="$ENV_FILE" COMPOSE_FILE="$COMPOSE_FILE" "${ROOT_DIR}/trigger-reparse-incomplete.sh"
fi
