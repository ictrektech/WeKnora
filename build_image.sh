#!/usr/bin/env bash
set -euo pipefail

# Build and push the ictrek WeKnora service images.
#
# The Feishu release table uses one service column per image. The same release
# tag is written to each service that is built:
#   swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora:<tag>
#   swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-ui:<tag>
#   swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-docreader:<tag>
#   swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-sandbox:<tag>

REGISTRY_PREFIX="swr.cn-southwest-2.myhuaweicloud.com/ictrek"
FEISHU_CONFIG_FILE="${FEISHU_CONFIG_FILE:-${HOME}/.feishu.json}"
FEISHU_SPREADSHEET_TOKEN="Htotsn3oahO1zxt73YMcaB1zn8e"
TARGET="${WEKNORA_BUILD_TARGET:-}"
TARGET_SHEET_SPEC="${FEISHU_SHEET_TITLE:-}"
PROFILE_TAG=""
TARGET_SHEET_TITLES=()

APP_IMAGE="${REGISTRY_PREFIX}/weknora"
UI_IMAGE="${REGISTRY_PREFIX}/weknora-ui"
DOCREADER_IMAGE="${REGISTRY_PREFIX}/weknora-docreader"
SANDBOX_IMAGE="${REGISTRY_PREFIX}/weknora-sandbox"

BUILD_APP=1
BUILD_FRONTEND=1
BUILD_DOCREADER=1
BUILD_SANDBOX=1
PUSH_IMAGES=1
UPDATE_FEISHU=1
DRY_RUN=0
SKIP_BUILD=0
BUILD_ENGINE="${WEKNORA_BUILD_ENGINE:-auto}"

log() {
  echo "[INFO] $*"
}

err() {
  echo "[ERROR] $*" >&2
}

usage() {
  cat <<'EOF'
Usage: ./build_image.sh [options]

Builds the WeKnora service images and records each service tag in Feishu.

Options:
  --app-only             Build only swr.../weknora
  --frontend-only        Build only swr.../weknora-ui
  --docreader-only       Build only swr.../weknora-docreader
  --sandbox-only         Build only swr.../weknora-sandbox
  --no-push              Build locally without docker push
  --no-feishu            Do not update Feishu after push
  --feishu-only          Do not build or push; only write selected service tags to Feishu
  --dry-run              Print the plan without building or writing Feishu
  --target TARGET        Build target tag prefix: amd or arm (default: detect current machine)
  --sheet SHEET          Override Feishu sheet title list; comma-separated values are accepted
  --tag TAG              Override the generated tag
  -h, --help             Show this help

Environment:
  FEISHU_CONFIG_FILE     Defaults to ~/.feishu.json on the build host
  WEKNORA_BUILD_TARGET   Optional default for --target
  FEISHU_SHEET_TITLE     Optional default for --sheet, comma-separated values accepted
  APK_MIRROR_ARG         Optional Debian mirror for app image
  APT_MIRROR             Optional Debian mirror for docreader image
  NPM_REGISTRY           Optional npm registry for frontend image
  GOPROXY_ARG            Optional Go proxy for app image
  GOPRIVATE_ARG          Optional Go private module pattern for app image
  GOSUMDB_ARG            Optional Go checksum DB setting, default off in Dockerfile
  DOCKER_CLI_VERSION     Optional Docker CLI version bundled into the app image
  WEKNORA_BUILD_ENGINE   auto (default), buildx, or docker. auto prefers buildx
                         and falls back to docker build.
EOF
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    err "missing command: $1"
    exit 1
  }
}

configure_build_engine() {
  case "$BUILD_ENGINE" in
    auto)
      if docker buildx version >/dev/null 2>&1; then
        BUILD_ENGINE="buildx"
      else
        BUILD_ENGINE="docker"
      fi
      ;;
    buildx)
      docker buildx version >/dev/null 2>&1 || {
        err "WEKNORA_BUILD_ENGINE=buildx but docker buildx is unavailable"
        exit 1
      }
      ;;
    docker)
      ;;
    *)
      err "Unsupported WEKNORA_BUILD_ENGINE=${BUILD_ENGINE}; expected auto, buildx, or docker"
      exit 1
      ;;
  esac
}

docker_build_image() {
  if [[ "$BUILD_ENGINE" == "buildx" ]]; then
    docker buildx build --load --provenance=false --sbom=false "$@"
  else
    docker build "$@"
  fi
}

normalize_official_image_path() {
  local image="$1"
  local image_without_tag="${image%%:*}"

  if [[ "$image_without_tag" != */* ]]; then
    printf 'library/%s\n' "$image"
  else
    printf '%s\n' "$image"
  fi
}

pull_base_image() {
  local image="$1"
  local normalized_image mirror mirrored_image

  if docker pull "$image"; then
    return 0
  fi

  normalized_image="$(normalize_official_image_path "$image")"
  for mirror in docker.m.daocloud.io docker.1ms.run dockerproxy.com; do
    mirrored_image="${mirror}/${normalized_image}"
    log "Direct pull failed for ${image}; trying Docker registry mirror: ${mirrored_image}"
    if docker pull "$mirrored_image"; then
      docker tag "$mirrored_image" "$image"
      return 0
    fi
  done

  return 1
}

ensure_dockerfile_base_images() {
  local dockerfile="$1"
  local image missing=0

  while IFS= read -r image; do
    [[ -n "$image" ]] || continue
    if docker image inspect "$image" >/dev/null 2>&1; then
      log "Base image present locally: ${image}"
      continue
    fi

    log "Base image missing locally, pulling: ${image}"
    if ! pull_base_image "$image"; then
      err "Base image is not available locally and pull failed: ${image}"
      missing=1
    fi
  done < <(awk 'toupper($1) == "FROM" { print $2 }' "$dockerfile" | sort -u)

  [[ "$missing" == "0" ]]
}

docker_build_with_local_base_fallback() {
  local dockerfile="$1"
  shift

  ensure_dockerfile_base_images "$dockerfile"

  if docker_build_image "$@"; then
    return 0
  fi

  if [[ "$BUILD_ENGINE" != "buildx" ]]; then
    return 1
  fi

  log "Buildx build failed for ${dockerfile}; retrying with docker build --pull=false to use local base images"
  DOCKER_BUILDKIT=1 docker build --pull=false "$@"
}

column_letter() {
  python3 - "$1" <<'PY'
import sys
n = int(sys.argv[1])
s = ""
while n > 0:
    n, r = divmod(n - 1, 26)
    s = chr(ord("A") + r) + s
print(s)
PY
}

read_feishu_field() {
  local field="$1"
  python3 - "$FEISHU_CONFIG_FILE" "$field" <<'PY'
import json, sys
path, field = sys.argv[1], sys.argv[2]
with open(path, 'r', encoding='utf-8') as f:
    data = json.load(f)
val = data.get(field, "")
if not isinstance(val, str):
    val = str(val)
print(val)
PY
}

get_feishu_token() {
  local app_id="$1"
  local app_secret="$2"
  local resp

  resp=$(
    curl --fail -sS -X POST 'https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal' \
      -H 'Content-Type: application/json' \
      -d "{\"app_id\":\"${app_id}\",\"app_secret\":\"${app_secret}\"}"
  ) || {
    err "get_feishu_token: curl failed"
    return 1
  }

  python3 - "$resp" <<'PY'
import json, sys
resp = sys.argv[1]
data = json.loads(resp)
if data.get("code") != 0:
    raise SystemExit(f"get_feishu_token failed: {data}")
print(data["tenant_access_token"])
PY
}

feishu_api_json() {
  local method="$1"
  local url="$2"
  local token="$3"
  local body="${4:-}"

  if [[ -n "$body" ]]; then
    curl --fail -sS -X "$method" "$url" \
      -H "Authorization: Bearer ${token}" \
      -H "Content-Type: application/json" \
      --data "$body"
  else
    curl --fail -sS -X "$method" "$url" \
      -H "Authorization: Bearer ${token}"
  fi
}

get_sheet_id_by_title() {
  local token="$1"
  local target_title="$2"
  local resp

  resp=$(
    feishu_api_json "GET" \
      "https://open.feishu.cn/open-apis/sheets/v3/spreadsheets/${FEISHU_SPREADSHEET_TOKEN}/sheets/query" \
      "$token"
  )

  python3 - "$target_title" "$resp" <<'PY'
import json, sys
target = sys.argv[1]
data = json.loads(sys.argv[2])
if data.get("code") != 0:
    raise SystemExit(f"query sheets failed: {data}")
for sheet in data.get("data", {}).get("sheets", []):
    if sheet.get("title") == target:
        print(sheet["sheet_id"])
        raise SystemExit(0)
raise SystemExit(f"sheet title not found: {target}")
PY
}

get_range_values() {
  local token="$1"
  local range="$2"

  feishu_api_json "GET" \
    "https://open.feishu.cn/open-apis/sheets/v2/spreadsheets/${FEISHU_SPREADSHEET_TOKEN}/values/${range}" \
    "$token"
}

write_cell() {
  local token="$1"
  local sheet_id="$2"
  local cell="$3"
  local value="$4"
  local resp

  resp=$(
    feishu_api_json "PUT" \
      "https://open.feishu.cn/open-apis/sheets/v2/spreadsheets/${FEISHU_SPREADSHEET_TOKEN}/values" \
      "$token" \
      "{\"valueRange\":{\"range\":\"${sheet_id}!${cell}:${cell}\",\"values\":[[\"${value}\"]]}}"
  )

  python3 - "$resp" <<'PY'
import json, sys
data = json.loads(sys.argv[1])
if data.get("code") != 0:
    raise SystemExit(f"write_cell failed: {data}")
PY
}

find_or_create_component_column() {
  local token="$1"
  local sheet_id="$2"
  local component_name="$3"
  local repo_uri="$4"
  local resp_file

  resp_file="$(mktemp)"
  get_range_values "$token" "${sheet_id}!A1:ZZ2" > "$resp_file"

  python3 - "$component_name" "$resp_file" <<'PY'
import json, sys
target = sys.argv[1]
with open(sys.argv[2], "r", encoding="utf-8") as f:
    data = json.load(f)
if data.get("code") != 0:
    raise SystemExit(f"read header failed: {data}")
values = data.get("data", {}).get("valueRange", {}).get("values", [])
row = values[0] if values else []
repo_row = values[1] if len(values) > 1 else []

def cell_text(v):
    if v is None:
        return ""
    if isinstance(v, str):
        return v.strip()
    if isinstance(v, dict):
        return str(v.get("text") or v.get("link") or "").strip()
    if isinstance(v, list):
        return "".join(cell_text(x) for x in v).strip()
    return str(v).strip()

last_component_col = 0
max_len = max(len(row), len(repo_row))
for i in range(2, max_len + 1):
    header = cell_text(row[i - 1]) if i <= len(row) else ""
    repo = cell_text(repo_row[i - 1]) if i <= len(repo_row) else ""
    if header == target:
        print(i)
        raise SystemExit(0)
    if header or "swr.cn-southwest-2.myhuaweicloud.com/" in repo:
        last_component_col = i
# Do not bind new services to fixed column letters. Keep the sheet compact by
# appending after the current component block, and leave column deletion to
# manual maintenance outside this build script.
for i in range(last_component_col + 1, min(last_component_col + 33, 703)):
    header = cell_text(row[i - 1]) if i <= len(row) else ""
    repo = cell_text(repo_row[i - 1]) if i <= len(repo_row) else ""
    if not header and not repo:
        print(i)
        raise SystemExit(0)
print(last_component_col + 1)
PY
  rm -f "$resp_file"
}

find_date_row() {
  local token="$1"
  local sheet_id="$2"
  local target_date="$3"
  local resp

  resp=$(get_range_values "$token" "${sheet_id}!A4:A2000")

  python3 - "$target_date" "$resp" <<'PY'
import json, sys
target = sys.argv[1]
data = json.loads(sys.argv[2])
if data.get("code") != 0:
    raise SystemExit(f"read date column failed: {data}")
values = data.get("data", {}).get("valueRange", {}).get("values", [])
for idx, row in enumerate(values, start=4):
    if row and str(row[0]).strip() == target:
        print(idx)
        raise SystemExit(0)
print("")
PY
}

prepend_date_row() {
  local token="$1"
  local sheet_id="$2"
  local today="$3"
  local resp

  resp=$(
    feishu_api_json "POST" \
      "https://open.feishu.cn/open-apis/sheets/v2/spreadsheets/${FEISHU_SPREADSHEET_TOKEN}/values_prepend" \
      "$token" \
      "{\"valueRange\":{\"range\":\"${sheet_id}!A4:A4\",\"values\":[[\"${today}\"]]}}"
  )

  python3 - "$resp" <<'PY'
import json, sys
data = json.loads(sys.argv[1])
if data.get("code") != 0:
    raise SystemExit(f"prepend_date_row failed: {data}")
PY
}

update_feishu_cell() {
  local token="$1"
  local sheet_id="$2"
  local sheet_title="$3"
  local component_name="$4"
  local repo_uri="$5"
  local row="$6"
  local tag="$7"
  local component_col_idx component_col

  component_col_idx="$(find_or_create_component_column "$token" "$sheet_id" "$component_name" "$repo_uri")"
  component_col="$(column_letter "$component_col_idx")"

  write_cell "$token" "$sheet_id" "${component_col}1" "$component_name"
  write_cell "$token" "$sheet_id" "${component_col}2" "$repo_uri"
  write_cell "$token" "$sheet_id" "${component_col}${row}" "$tag"

  log "Feishu updated: ${sheet_title}!${component_col}${row} = ${tag} (${component_name})"
}

update_feishu() {
  local tag="$1"
  local app_id app_secret token sheet_id date_row sheet_title

  if [[ ! -f "$FEISHU_CONFIG_FILE" ]]; then
    err "Feishu config not found: $FEISHU_CONFIG_FILE"
    exit 1
  fi

  app_id="$(read_feishu_field "feishu_app_id")"
  app_secret="$(read_feishu_field "feishu_app_secret")"
  if [[ -z "$app_id" || -z "$app_secret" ]]; then
    err "feishu_app_id or feishu_app_secret missing in $FEISHU_CONFIG_FILE"
    exit 1
  fi

  for sheet_title in "${TARGET_SHEET_TITLES[@]}"; do
    token="$(get_feishu_token "$app_id" "$app_secret")"
    sheet_id="$(get_sheet_id_by_title "$token" "$sheet_title")"
    log "Resolved sheet: ${sheet_title} -> ${sheet_id}"

    token="$(get_feishu_token "$app_id" "$app_secret")"
    date_row="$(find_date_row "$token" "$sheet_id" "$DATE")"
    if [[ -z "$date_row" ]]; then
      log "Date ${DATE} not found in ${sheet_title}, creating a new row at top of data area"
      token="$(get_feishu_token "$app_id" "$app_secret")"
      prepend_date_row "$token" "$sheet_id" "$DATE"
      date_row=4
    else
      log "Date ${DATE} already exists in ${sheet_title} at row ${date_row}"
    fi

    token="$(get_feishu_token "$app_id" "$app_secret")"
    [[ "$BUILD_APP" == "1" ]] && update_feishu_cell "$token" "$sheet_id" "$sheet_title" "weknora" "$APP_IMAGE" "$date_row" "$tag"
    [[ "$BUILD_FRONTEND" == "1" ]] && update_feishu_cell "$token" "$sheet_id" "$sheet_title" "weknora-ui" "$UI_IMAGE" "$date_row" "$tag"
    [[ "$BUILD_DOCREADER" == "1" ]] && update_feishu_cell "$token" "$sheet_id" "$sheet_title" "weknora-docreader" "$DOCREADER_IMAGE" "$date_row" "$tag"
    [[ "$BUILD_SANDBOX" == "1" ]] && update_feishu_cell "$token" "$sheet_id" "$sheet_title" "weknora-sandbox" "$SANDBOX_IMAGE" "$date_row" "$tag"
  done
}

TAG_OVERRIDE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --app-only)
      BUILD_APP=1
      BUILD_FRONTEND=0
      BUILD_DOCREADER=0
      BUILD_SANDBOX=0
      shift
      ;;
    --frontend-only)
      BUILD_APP=0
      BUILD_FRONTEND=1
      BUILD_DOCREADER=0
      BUILD_SANDBOX=0
      shift
      ;;
    --docreader-only)
      BUILD_APP=0
      BUILD_FRONTEND=0
      BUILD_DOCREADER=1
      BUILD_SANDBOX=0
      shift
      ;;
    --sandbox-only)
      BUILD_APP=0
      BUILD_FRONTEND=0
      BUILD_DOCREADER=0
      BUILD_SANDBOX=1
      shift
      ;;
    --no-push)
      PUSH_IMAGES=0
      shift
      ;;
    --no-feishu)
      UPDATE_FEISHU=0
      shift
      ;;
    --feishu-only)
      SKIP_BUILD=1
      PUSH_IMAGES=0
      UPDATE_FEISHU=1
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      PUSH_IMAGES=0
      UPDATE_FEISHU=0
      shift
      ;;
    --tag)
      TAG_OVERRIDE="$2"
      shift 2
      ;;
    --target)
      TARGET="$2"
      shift 2
      ;;
    --sheet)
      TARGET_SHEET_SPEC="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      err "Unknown option: $1"
      usage
      exit 1
      ;;
  esac
done

require_cmd python3

ARCH="$(uname -m)"
if [[ -z "$TARGET" ]]; then
  case "$ARCH" in
    x86_64)
      TARGET="amd"
      ;;
    aarch64|arm64)
      TARGET="arm"
      ;;
    *)
      err "Unable to infer build target from architecture: ${ARCH}; pass --target amd or --target arm"
      exit 1
      ;;
  esac
fi

case "$TARGET" in
  amd)
    PROFILE_TAG="amd"
    TARGET_SHEET_SPEC="${TARGET_SHEET_SPEC:-AMD_with_cuda,AMD_with_mxn100}"
    ;;
  arm)
    PROFILE_TAG="arm"
    TARGET_SHEET_SPEC="${TARGET_SHEET_SPEC:-ARM_without_cuda,l4t,ARM_with_cuda,thor_spark,SOPHON_bm1688}"
    ;;
  *)
    err "Unsupported target: ${TARGET}; expected amd or arm"
    exit 1
    ;;
esac

IFS=',' read -r -a TARGET_SHEET_TITLES <<< "$TARGET_SHEET_SPEC"
if [[ "${#TARGET_SHEET_TITLES[@]}" -eq 0 ]]; then
  err "No Feishu sheet titles configured"
  exit 1
fi

if [[ "$DRY_RUN" != "1" ]]; then
  case "${TARGET}:${ARCH}" in
    amd:x86_64|arm:aarch64|arm:arm64)
      ;;
    *)
      err "Target ${TARGET} does not match native architecture ${ARCH}. Use a matching build host or extend this script with buildx."
      exit 1
      ;;
  esac
fi

DATE="$(date +%Y%m%d)"
TAG="${TAG_OVERRIDE:-${PROFILE_TAG}_${DATE}}"

log "TARGET=${TARGET}"
log "PROFILE_TAG=${PROFILE_TAG}"
log "TARGET_SHEETS=${TARGET_SHEET_TITLES[*]}"
log "TAG=${TAG}"
log "APP_IMAGE=${APP_IMAGE}:${TAG}"
log "UI_IMAGE=${UI_IMAGE}:${TAG}"
log "DOCREADER_IMAGE=${DOCREADER_IMAGE}:${TAG}"
log "SANDBOX_IMAGE=${SANDBOX_IMAGE}:${TAG}"

if [[ "$DRY_RUN" == "1" ]]; then
  exit 0
fi

if [[ "$SKIP_BUILD" != "1" ]]; then
  require_cmd docker
  configure_build_engine
  log "BUILD_ENGINE=${BUILD_ENGINE}"
fi
if [[ "$UPDATE_FEISHU" == "1" ]]; then
  require_cmd curl
fi

APP_BUILD_ARGS=(
  --build-arg "APK_MIRROR_ARG=${APK_MIRROR_ARG:-mirrors.tuna.tsinghua.edu.cn}"
  --build-arg "GOPROXY_ARG=${GOPROXY_ARG:-https://goproxy.cn,direct}"
  --build-arg "GOPRIVATE_ARG=${GOPRIVATE_ARG:-}"
  --build-arg "GOSUMDB_ARG=${GOSUMDB_ARG:-off}"
  --build-arg "VERSION_ARG=${TAG}"
  --build-arg "COMMIT_ID_ARG=${COMMIT_ID_ARG:-$(cat .git-commit 2>/dev/null || echo unknown)}"
  --build-arg "BUILD_TIME_ARG=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  --build-arg "GO_VERSION_ARG=$(go version 2>/dev/null | awk '{print $3}' || echo unknown)"
)

FRONTEND_BUILD_ARGS=(
  --build-arg "NPM_REGISTRY=${NPM_REGISTRY:-https://registry.npmmirror.com}"
)

DOCREADER_BUILD_ARGS=(
  --build-arg "APT_MIRROR=${APT_MIRROR:-http://mirrors.tuna.tsinghua.edu.cn}"
)

if [[ "$SKIP_BUILD" != "1" && "$BUILD_APP" == "1" ]]; then
  docker_build_with_local_base_fallback docker/Dockerfile.app \
    "${APP_BUILD_ARGS[@]}" \
    -f docker/Dockerfile.app \
    -t "${APP_IMAGE}:${TAG}" \
    .
fi

if [[ "$SKIP_BUILD" != "1" && "$BUILD_FRONTEND" == "1" ]]; then
  docker_build_with_local_base_fallback docker/Dockerfile.frontend \
    "${FRONTEND_BUILD_ARGS[@]}" \
    -f docker/Dockerfile.frontend \
    -t "${UI_IMAGE}:${TAG}" \
    .
fi

if [[ "$SKIP_BUILD" != "1" && "$BUILD_DOCREADER" == "1" ]]; then
  docker_build_with_local_base_fallback docker/Dockerfile.docreader \
    "${DOCREADER_BUILD_ARGS[@]}" \
    -f docker/Dockerfile.docreader \
    -t "${DOCREADER_IMAGE}:${TAG}" \
    .
fi

if [[ "$SKIP_BUILD" != "1" && "$BUILD_SANDBOX" == "1" ]]; then
  docker_build_with_local_base_fallback docker/Dockerfile.sandbox \
    -f docker/Dockerfile.sandbox \
    -t "${SANDBOX_IMAGE}:${TAG}" \
    .
fi

if [[ "$PUSH_IMAGES" == "1" ]]; then
  [[ "$BUILD_APP" == "1" ]] && docker push "${APP_IMAGE}:${TAG}"
  [[ "$BUILD_FRONTEND" == "1" ]] && docker push "${UI_IMAGE}:${TAG}"
  [[ "$BUILD_DOCREADER" == "1" ]] && docker push "${DOCREADER_IMAGE}:${TAG}"
  [[ "$BUILD_SANDBOX" == "1" ]] && docker push "${SANDBOX_IMAGE}:${TAG}"
fi

if [[ "$UPDATE_FEISHU" == "1" ]]; then
  update_feishu "$TAG"
fi

log "Done."
