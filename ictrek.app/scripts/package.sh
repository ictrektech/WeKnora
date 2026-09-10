#!/usr/bin/env bash
set -euo pipefail

APP_NAME="hybrag"
APP_ID="com.ictrek.hybrag"
ROUTER_PAGE_ID="hybrag"
ROUTER_IFRAME_SRC="/app/com.ictrek.hybrag/?v=__APP_VERSION__"
ROUTER_HASH_PATH="#/app/com.ictrek.hybrag/hybrag"
FRONTEND_BASE_PATH="/app/com.ictrek.hybrag"
SPREADSHEET_TOKEN="${FEISHU_SPREADSHEET_TOKEN:-Htotsn3oahO1zxt73YMcaB1zn8e}"
FEISHU_CONFIG_FILE="${FEISHU_CONFIG_FILE:-${HOME}/.feishu.components.json}"
FEISHU_FALLBACK_CONFIG_FILE="${FEISHU_FALLBACK_CONFIG_FILE:-${HOME}/.feishu.json}"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_DIR="$(cd "${ROOT_DIR}/.." && pwd)"
SRC_DIR="${ROOT_DIR}/src"
DIST_DIR="${ROOT_DIR}/dist"
STAGE_DIR="${DIST_DIR}/staging"
PACKAGE_ROOT="${DIST_DIR}/package-root"
VERSION_FILE="${ROOT_DIR}/VERSION"
LOCK_DIR="${DIST_DIR}/.package.lock"

# HybRAG itself no longer starts Ollama. Model Hub owns the runtime profile
# details, so this app only needs one AMD64 and one ARM64 profile.
PROFILES=(
  "amd|AMD_with_cuda"
  "arm|ARM_with_cuda"
)
COMPONENTS=(
  "WEKNORA_APP|weknora|swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora"
  "WEKNORA_UI|weknora-ui|swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-ui"
  "WEKNORA_DOCREADER|weknora-docreader|swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-docreader"
  "WEKNORA_SANDBOX|weknora-sandbox|swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-sandbox"
)
BASE_IMAGES=(
  "REDIS_AMD_IMAGE=swr.cn-southwest-2.myhuaweicloud.com/ictrek/redis:7.0-alpine"
  "NEO4J_AMD_IMAGE=swr.cn-southwest-2.myhuaweicloud.com/ictrek/neo4j:2025.10.1"
  "REDIS_ARM_IMAGE=swr.cn-southwest-2.myhuaweicloud.com/ictrek-arm/redis:7.0-alpine"
  "NEO4J_ARM_IMAGE=swr.cn-southwest-2.myhuaweicloud.com/ictrek-arm/neo4j:2025.10.1"
)

IMAGE_SOURCE="${IMAGE_SOURCE:-pull}"
PLATFORM="${PLATFORM:-}"
ASSET_FILES=()

detect_platform() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) die "unsupported architecture: $(uname -m)" ;;
  esac
}

usage() {
  cat <<'EOF'
Usage:
  ./scripts/package.sh [--image-source local|pull] [--platform amd64|arm64]

Options:
  --image-source     pull: one tarball for all profiles, image names only
                     (default). local: embed docker-archive assets for one
                     platform; the VOS host docker-loads them at install.
  --platform         amd64 or arm64 for --image-source local (default:
                     auto-detect).
EOF
}

log() { echo "[INFO] $*"; }
err() { echo "[ERROR] $*" >&2; }
die() { err "$*"; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing command: $1"
}

select_python() {
  local candidate
  if [[ -n "${PYTHON_BIN:-}" ]]; then
    require_cmd "$PYTHON_BIN"
    "$PYTHON_BIN" - <<'PYCHECK' || die "PYTHON_BIN cannot import yaml: ${PYTHON_BIN}"
import yaml
PYCHECK
    return
  fi

  for candidate in python3 python; do
    if command -v "$candidate" >/dev/null 2>&1 && "$candidate" - <<'PYCHECK' >/dev/null 2>&1; then
import yaml
PYCHECK
      PYTHON_BIN="$candidate"
      log "Python runtime: ${PYTHON_BIN}"
      return
    fi
  done

  die "missing Python runtime with PyYAML; install PyYAML or set PYTHON_BIN to a Python that can import yaml"
}

validate_yaml_file() {
  local file="$1"
  "${PYTHON_BIN}" - "$file" <<'PYYAML' \
    || die "invalid YAML: ${file}"
import sys
import yaml
with open(sys.argv[1], "r", encoding="utf-8") as f:
    yaml.safe_load(f)
PYYAML
}

validate_migration_versions() {
  local migrations_dir="${REPO_DIR}/migrations/versioned"
  [[ -d "$migrations_dir" ]] || return 0
  "${PYTHON_BIN}" - "$migrations_dir" <<'PYMIGRATIONS' \
    || die "migration version set is invalid"
import re
import sys
from collections import Counter, defaultdict
from pathlib import Path

migrations_dir = Path(sys.argv[1])
pattern = re.compile(r"^(\d+)_.+\.(up|down)\.sql$")
versions = defaultdict(Counter)
bad_names = []
for path in migrations_dir.glob("*.sql"):
    match = pattern.match(path.name)
    if not match:
        bad_names.append(path.name)
        continue
    version, direction = match.groups()
    versions[version][direction] += 1

errors = []
if bad_names:
    errors.append("unexpected migration names: " + ", ".join(sorted(bad_names)))
for version, directions in sorted(versions.items()):
    if directions["up"] != 1 or directions["down"] != 1:
        errors.append(f"{version}: expected one .up.sql and one .down.sql")
if errors:
    print("\n".join(errors), file=sys.stderr)
    raise SystemExit(1)
PYMIGRATIONS
}

validate_staged_files() {
  local file
  for file in manifest.yml configs.yml routers.yml docker-compose.yml; do
    validate_yaml_file "${STAGE_DIR}/${file}"
  done
  validate_icon_file "${STAGE_DIR}/icon.png"

  local expected_profiles=()
  local profile_spec profile sheet_title
  for profile_spec in "${PROFILES[@]}"; do
    IFS='|' read -r profile sheet_title <<< "$profile_spec"
    expected_profiles+=("$profile")
  done
  "${PYTHON_BIN}" - "${STAGE_DIR}/manifest.yml" "${STAGE_DIR}/docker-compose.yml" "${expected_profiles[@]}" <<'PYPROFILE' \
    || die "manifest/docker-compose profile contract validation failed"
import re
import sys
import yaml

manifest_path, compose_path, *expected = sys.argv[1:]
with open(manifest_path, "r", encoding="utf-8") as f:
    manifest = yaml.safe_load(f) or {}
profiles = manifest.get("profiles")
if not isinstance(profiles, list) or not profiles:
    raise SystemExit("manifest.yml must declare non-empty profiles for VOS install UI")

names = []
for profile in profiles:
    if not isinstance(profile, dict) or "name" not in profile:
        raise SystemExit("manifest profile missing name")
    name = str(profile["name"])
    if not re.fullmatch(r"[a-z0-9][a-z0-9-]*", name):
        raise SystemExit(f"invalid VOS profile name {name!r}; use lowercase letters, digits, and hyphens only")
    names.append(name)

if sorted(names) != sorted(expected):
    raise SystemExit(f"manifest profiles {names!r} do not match package profiles {expected!r}")

for profile in profiles:
    name = str(profile["name"])
    conflicts = profile.get("conflicts") or []
    if not isinstance(conflicts, list):
        raise SystemExit(f"manifest profile {name!r} conflicts must be a list")
    missing = [other for other in names if other != name and other not in conflicts]
    if missing:
        raise SystemExit(f"manifest profile {name!r} conflicts missing {missing!r}")

with open(compose_path, "r", encoding="utf-8") as f:
    compose_text = f.read()
compose = yaml.safe_load(compose_text) or {}
services = compose.get("services") or {}
compose_profiles = set()
for service in services.values():
    for profile in service.get("profiles") or []:
        compose_profiles.add(str(profile))
missing = [name for name in names if name not in compose_profiles]
if missing:
    raise SystemExit(f"docker-compose.yml has no service using profiles {missing!r}")

bad_names = {"AMD_with_cuda", "ARM_with_cuda", "thor_spark", "SOPHON_bm1688"}
if bad_names & set(names):
    raise SystemExit(f"manifest profiles must not use Feishu sheet names: {sorted(bad_names & set(names))!r}")
bad_compose = bad_names & compose_profiles
if bad_compose:
    raise SystemExit(f"docker-compose.yml profiles must not use Feishu sheet names: {sorted(bad_compose)!r}")
PYPROFILE

  "${PYTHON_BIN}" - "${STAGE_DIR}/configs.yml" <<'PYCONFIG' \
    || die "configs.yml type validation failed"
import sys
import yaml

allowed = {"string", "integer", "number", "boolean", "array", "path"}
with open(sys.argv[1], "r", encoding="utf-8") as f:
    data = yaml.safe_load(f) or {}
configs = data.get("configs") or []
if not isinstance(configs, list):
    raise SystemExit("configs.yml `configs` must be a list")
for index, item in enumerate(configs):
    if not isinstance(item, dict):
        raise SystemExit(f"configs[{index}] must be an object")
    name = item.get("name", f"#{index}")
    config_type = item.get("type")
    if config_type not in allowed:
        raise SystemExit(
            f"configs[{index}] {name!r} type {config_type!r} is unsupported; "
            f"allowed: {sorted(allowed)!r}"
        )
PYCONFIG

  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    local profile
    for profile in amd arm; do
      docker compose --env-file "${STAGE_DIR}/.env" -f "${STAGE_DIR}/docker-compose.yml" --profile "$profile" config >/dev/null \
        || die "docker compose config failed for profile ${profile}"
    done
  else
    log "Skip docker compose profile validation because docker compose is unavailable"
  fi
}

validate_icon_file() {
  local file="$1"
  "${PYTHON_BIN}" - "$file" <<'PYICON' \
    || die "icon.png validation failed"
import struct
import sys
from pathlib import Path

path = Path(sys.argv[1])
data = path.read_bytes()
if not data.startswith(b"\x89PNG\r\n\x1a\n"):
    raise SystemExit("icon.png must be a PNG file")
if len(data) < 33 or data[12:16] != b"IHDR":
    raise SystemExit("icon.png is missing PNG IHDR")
width, height, bit_depth, color_type = struct.unpack(">IIBB", data[16:26])
if (width, height) != (256, 256):
    raise SystemExit(f"icon.png must be 256x256, got {width}x{height}")
if color_type not in (4, 6):
    raise SystemExit("icon.png must include an alpha channel for transparent corners")
if bit_depth != 8:
    raise SystemExit(f"icon.png bit depth must be 8, got {bit_depth}")
PYICON
}

acquire_lock() {
  while ! mkdir "$LOCK_DIR" 2>/dev/null; do
    sleep 1
  done
  trap 'rm -rf "$LOCK_DIR"' EXIT
}

read_version() {
  [[ -f "$VERSION_FILE" ]] || echo "0.0.0" > "$VERSION_FILE"
  tr -d '[:space:]' < "$VERSION_FILE"
}

read_feishu_field() {
  local config_file="$1"
  local field="$2"
  "${PYTHON_BIN}" - "$config_file" "$field" <<'PYJSON'
import json
import sys
path, field = sys.argv[1], sys.argv[2]
with open(path, "r", encoding="utf-8") as f:
    data = json.load(f)
val = data.get(field, "")
print(val if isinstance(val, str) else str(val))
PYJSON
}

feishu_api_json() {
  local method="$1"
  local url="$2"
  local token="$3"
  curl --fail -sS -X "$method" "$url" -H "Authorization: Bearer ${token}"
}

get_feishu_token() {
  local app_id="$1"
  local app_secret="$2"
  local resp
  resp="$(
    curl --fail -sS -X POST "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal" \
      -H "Content-Type: application/json" \
      -d "{\"app_id\":\"${app_id}\",\"app_secret\":\"${app_secret}\"}"
  )"
  "${PYTHON_BIN}" - "$resp" <<'PYJSON'
import json
import sys
data = json.loads(sys.argv[1])
if data.get("code") != 0:
    raise SystemExit(f"get_feishu_token failed: {data}")
print(data["tenant_access_token"])
PYJSON
}

get_sheet_id_by_title() {
  local token="$1"
  local target_title="$2"
  local resp
  resp="$(feishu_api_json "GET" \
    "https://open.feishu.cn/open-apis/sheets/v3/spreadsheets/${SPREADSHEET_TOKEN}/sheets/query" \
    "$token")"
  "${PYTHON_BIN}" - "$target_title" "$resp" <<'PYJSON'
import json
import sys
target, resp = sys.argv[1], sys.argv[2]
data = json.loads(resp)
if data.get("code") != 0:
    raise SystemExit(f"query sheets failed: {data}")
for sheet in data.get("data", {}).get("sheets", []):
    if sheet.get("title") == target:
        print(sheet["sheet_id"])
        raise SystemExit(0)
raise SystemExit(f"sheet title not found: {target}")
PYJSON
}

get_range_values() {
  local token="$1"
  local range="$2"
  feishu_api_json "GET" \
    "https://open.feishu.cn/open-apis/sheets/v2/spreadsheets/${SPREADSHEET_TOKEN}/values/${range}" \
    "$token"
}

find_component_column_letter() {
  local token="$1"
  local sheet_id="$2"
  local component="$3"
  local resp
  resp="$(get_range_values "$token" "${sheet_id}!A1:ZZ1")"
  "${PYTHON_BIN}" - "$component" "$resp" <<'PYJSON'
import json
import sys
target, resp = sys.argv[1], sys.argv[2]
data = json.loads(resp)
if data.get("code") != 0:
    raise SystemExit(f"read header failed: {data}")
values = data.get("data", {}).get("valueRange", {}).get("values", [])
row = values[0] if values else []

def text(value):
    if value is None:
        return ""
    if isinstance(value, str):
        return value.strip()
    if isinstance(value, dict):
        return str(value.get("text") or value.get("link") or "").strip()
    if isinstance(value, list):
        return "".join(text(v) for v in value).strip()
    return str(value).strip()

def col(num):
    out = ""
    while num > 0:
        num, rem = divmod(num - 1, 26)
        out = chr(ord("A") + rem) + out
    return out

for index, value in enumerate(row, start=1):
    if text(value) == target:
        print(col(index))
        raise SystemExit(0)
raise SystemExit(f"component column not found in row1: {target}")
PYJSON
}

find_latest_tag() {
  local token="$1"
  local sheet_id="$2"
  local column="$3"
  local resp
  resp="$(get_range_values "$token" "${sheet_id}!${column}4:${column}2000")"
  "${PYTHON_BIN}" - "$resp" <<'PYJSON'
import json
import sys
data = json.loads(sys.argv[1])
if data.get("code") != 0:
    raise SystemExit(f"read version column failed: {data}")
values = data.get("data", {}).get("valueRange", {}).get("values", [])
for row in values:
    if not row:
        continue
    value = row[0]
    if value is None:
        continue
    text = str(value).strip()
    if text:
        print(text)
        raise SystemExit(0)
raise SystemExit("latest version not found")
PYJSON
}

latest_image() {
  local token="$1"
  local sheet_id="$2"
  local component="$3"
  local repository="$4"
  local column tag
  column="$(find_component_column_letter "$token" "$sheet_id" "$component")" || return 1
  tag="$(find_latest_tag "$token" "$sheet_id" "$column")" || return 1
  [[ -n "$tag" ]] || return 1
  echo "${repository}:${tag}"
}

resolve_image_versions() {
  local env_file="$1"
  : > "$env_file"
  local base_image
  for base_image in "${BASE_IMAGES[@]}"; do
    log "base image -> ${base_image}"
    printf '%s\n' "$base_image" >> "$env_file"
  done
  for profile_spec in "${PROFILES[@]}"; do
    IFS='|' read -r profile sheet_title <<< "$profile_spec"
    profile_key="$(env_key "$profile")"
    sheet_id="$(get_sheet_id_by_title "$FEISHU_TOKEN" "$sheet_title")"
    for component_spec in "${COMPONENTS[@]}"; do
      IFS='|' read -r env_prefix component repository <<< "$component_spec"
      image="$(latest_image "$FEISHU_TOKEN" "$sheet_id" "$component" "$repository")" \
        || die "failed to resolve image for profile=${profile} component=${component} sheet=${sheet_title}"
      log "$profile ($sheet_title) $component -> $image"
      printf '%s_%s_IMAGE=%s\n' "$env_prefix" "$profile_key" "$image" >> "$env_file"
    done
  done
}

load_feishu_auth() {
  local config_file tried=""
  for config_file in "$FEISHU_CONFIG_FILE" "$FEISHU_FALLBACK_CONFIG_FILE"; do
    [[ -n "$config_file" && "$tried" != *"|$config_file|"* ]] || continue
    tried="${tried}|${config_file}|"
    [[ -r "$config_file" ]] || { log "Skip unreadable Feishu config: ${config_file}"; continue; }
    log "Read component versions with Feishu config: ${config_file}"
    if FEISHU_APP_ID="$(read_feishu_field "$config_file" "feishu_app_id")" \
      && FEISHU_APP_SECRET="$(read_feishu_field "$config_file" "feishu_app_secret")" \
      && [[ -n "$FEISHU_APP_ID" && -n "$FEISHU_APP_SECRET" ]] \
      && FEISHU_TOKEN="$(get_feishu_token "$FEISHU_APP_ID" "$FEISHU_APP_SECRET")"; then
      return 0
    fi
    log "Cannot read Feishu auth from ${config_file}; trying fallback"
  done
  die "failed to read Feishu credentials"
}

render_text_file() {
  local src="$1"
  local dst="$2"
  "${PYTHON_BIN}" - "$src" "$dst" "$APP_VERSION" <<'PYRENDER'
import sys
from pathlib import Path
src, dst = Path(sys.argv[1]), Path(sys.argv[2])
text = src.read_text(encoding="utf-8").replace("__APP_VERSION__", sys.argv[3])
dst.write_text(text, encoding="utf-8")
PYRENDER
}

render_compose_file() {
  local src="$1"
  local dst="$2"
  local env_file="$3"
  "${PYTHON_BIN}" - "$src" "$dst" "$APP_VERSION" "$env_file" <<'PYRENDER'
import re
import sys
from pathlib import Path

src, dst = Path(sys.argv[1]), Path(sys.argv[2])
version, env_path = sys.argv[3], Path(sys.argv[4])
env = {}
for line in env_path.read_text(encoding="utf-8").splitlines():
    if not line or line.startswith("#") or "=" not in line:
        continue
    key, value = line.split("=", 1)
    env[key] = value

text = src.read_text(encoding="utf-8").replace("__APP_VERSION__", version)

def replace_image_var(match):
    key = match.group(1)
    if key.endswith("_IMAGE") and key in env:
        return env[key]
    return match.group(0)

text = re.sub(r"\$\{([A-Z0-9_]+)(?::-[^}]*)?\}", replace_image_var, text)
dst.write_text(text, encoding="utf-8")
PYRENDER
}

verify_package() {
  local package_path="$1"
  local app_tarball="$2"
  local package_text
  local required_file
  local manifest_text
  local app_file_list
  local package_file_list
  local routers_text
  local compose_text
  local router_iframe_src
  local sidebar_route
  log "Verify app.tar.gz contents"
  app_file_list="$(mktemp)"
  package_file_list="$(mktemp)"
  tar tzf "$app_tarball" > "$app_file_list"
  tar tf "$package_path" > "$package_file_list"
  for required_file in .env manifest.yml docker-compose.yml configs.yml routers.yml icon.png README.zh-CN.md README.en.md; do
    grep -qx "$required_file" "$app_file_list" || die "app.tar.gz missing required file: ${required_file}"
  done
  if [[ -f "${STAGE_DIR}/traefik.yml" ]]; then
    grep -qx "traefik.yml" "$app_file_list" || die "app.tar.gz missing staged traefik.yml"
  fi
  ! grep -q "^config/" "$app_file_list"
  grep -qx "app.tar.gz" "$package_file_list"
  if [[ "$IMAGE_SOURCE" == "local" ]]; then
    local asset
    for asset in "${ASSET_FILES[@]}"; do
      grep -qx "$asset" "$package_file_list" || die "package missing ${asset}"
    done
    tar xOf "$app_tarball" manifest.yml | grep -q 'kind: docker-archive' \
      || die "manifest.yml missing docker-archive assets"
  else
    ! grep -q "^assets/" "$package_file_list"
  fi
  rm -f "$app_file_list" "$package_file_list"
  package_text="$(tar tzf "$app_tarball" | while IFS= read -r file; do [[ "$file" == */ ]] && continue; tar xOf "$app_tarball" "$file"; printf '\n'; done)"
  if printf '%s' "$package_text" | grep -q '__APP_VERSION__'; then
    die "unrendered placeholder remains"
  fi
  manifest_text="$(tar xOf "$app_tarball" manifest.yml)"
  if ! printf '%s\n' "$manifest_text" | grep -Fq "icon: icon.png"; then
    die "manifest.yml must declare icon: icon.png"
  fi
  if ! printf '%s\n' "$manifest_text" | grep -q '^[[:space:]]*frontend:[[:space:]]*$'; then
    die "manifest.yml must declare frontend for VOS open button compatibility"
  fi
  if ! printf '%s\n' "$manifest_text" | grep -q '^[[:space:]]*enabled:[[:space:]]*true[[:space:]]*$'; then
    die "manifest.yml frontend.enabled must be true"
  fi
  if ! printf '%s\n' "$manifest_text" | grep -Fq "  basePath: ${FRONTEND_BASE_PATH}"; then
    die "manifest.yml frontend.basePath must be ${FRONTEND_BASE_PATH}"
  fi
  compose_text="$(tar xOf "$app_tarball" docker-compose.yml)"
  if printf '%s\n' "$compose_text" | grep -q '\${[^}]*_IMAGE[^}]*}'; then
    die "unrendered image variable remains in docker-compose.yml"
  fi
  if printf '%s\n' "$compose_text" | awk '/^[[:space:]]*image:/ {print $2}' | grep -v '^[^/[:space:]]\+\.[^/[:space:]]\+/' | grep -q .; then
    die "docker-compose.yml contains short image reference"
  fi
  if ! printf '%s\n' "$compose_text" | grep -Fq 'HeadersRegexp(`Sec-Fetch-Dest`, `document`)'; then
    die "docker-compose.yml must redirect top-level document opens to VOS hash route"
  fi
  if ! printf '%s\n' "$compose_text" | grep -Fq "${ROUTER_HASH_PATH}"; then
    die "docker-compose.yml redirect must target ${ROUTER_HASH_PATH}"
  fi
  routers_text="$(tar xOf "$app_tarball" routers.yml)"
  router_iframe_src="${ROUTER_IFRAME_SRC/__APP_VERSION__/${APP_VERSION}}"
  if ! printf '%s\n' "$routers_text" | grep -Fq "  - id: ${ROUTER_PAGE_ID}"; then
    die "routers.yml must declare top-level page id ${ROUTER_PAGE_ID}"
  fi
  if ! printf '%s\n' "$routers_text" | grep -q 'kind:[[:space:]]*page'; then
    die "routers.yml top-level router must be a page"
  fi
  if printf '%s\n' "$routers_text" | grep -q 'children:[[:space:]]*$'; then
    die "routers.yml must not use child pages for the HybRAG entry"
  fi
  if ! printf '%s\n' "$routers_text" | grep -Fq "    iframe-src: ${router_iframe_src}"; then
    die "routers.yml page iframe-src must be ${router_iframe_src}"
  fi
  if printf '%s\n' "$routers_text" | grep -Eq 'iframe-src:[[:space:]]*https?://'; then
    die "routers.yml iframe-src must use a VOS same-origin /app/<app-id>/ path"
  fi
  if ! printf '%s\n' "$routers_text" | grep -q 'entry-point:[[:space:]]*true'; then
    die "routers.yml page must declare entry-point: true"
  fi
  if ! printf '%s\n' "$routers_text" | grep -q 'embed:[[:space:]]*true'; then
    die "routers.yml must declare embed: true for sidebar iframe"
  fi
  sidebar_route="#/app/${APP_ID}/${ROUTER_PAGE_ID}"
  log "VOS sidebar route: ${sidebar_route}"
}

env_key() {
  printf '%s' "$1" | tr '[:lower:]-' '[:upper:]_' | tr -c 'A-Z0-9_' '_'
}

embed_local_images() {
  local profile_suffix key image archive
  case "$PLATFORM" in
    amd64) profile_suffix="AMD" ;;
    arm64) profile_suffix="ARM" ;;
    *) die "local mode requires --platform amd64 or arm64" ;;
  esac
  local keys=(
    "WEKNORA_APP_${profile_suffix}_IMAGE"
    "WEKNORA_UI_${profile_suffix}_IMAGE"
    "WEKNORA_DOCREADER_${profile_suffix}_IMAGE"
    "WEKNORA_SANDBOX_${profile_suffix}_IMAGE"
    "REDIS_${profile_suffix}_IMAGE"
    "NEO4J_${profile_suffix}_IMAGE"
  )
  local images=()
  for key in "${keys[@]}"; do
    image="$(grep -E "^${key}=" "$ENV_FILE" | tail -1 | cut -d= -f2-)"
    [[ -n "$image" ]] || die "missing ${key} in ${ENV_FILE}"
    images+=("$image")
  done
  log "Embed ${PLATFORM} images:"
  printf '  %s\n' "${images[@]}"

  local asset_dir="${PACKAGE_ROOT}/assets/${PLATFORM}"
  mkdir -p "$asset_dir"
  for image in "${images[@]}"; do
    archive="$(printf '%s' "$image" | sed -E 's|.*/||; s|[:/]|-|g').tar.gz"
    log "pull ${image}"
    docker pull --platform "linux/${PLATFORM}" "$image"
    log "save ${image} -> assets/${PLATFORM}/${archive}"
    docker save "$image" | gzip > "${asset_dir}/${archive}"
    ASSET_FILES+=("assets/${PLATFORM}/${archive}")
  done

  "${PYTHON_BIN}" - "${STAGE_DIR}/manifest.yml" "$PLATFORM" "${ASSET_FILES[@]}" <<'PYASSETS'
import sys
from pathlib import Path

manifest = Path(sys.argv[1])
platform = sys.argv[2]
assets = sys.argv[3:]
text = manifest.read_text(encoding="utf-8")
old_arch = "arch:\n- amd64\n- arm64\n"
if old_arch not in text:
    raise SystemExit("expected arch block not found in manifest.yml")
text = text.replace(old_arch, f"arch: [{platform}]\n")
block = ["assets:"]
for asset in assets:
    name = asset.rsplit("/", 1)[-1]
    block.append(f"  - filename: {name}")
    block.append("    kind: docker-archive")
    block.append("    arch: host")
idx = text.find("storage:")
if idx < 0:
    raise SystemExit("storage: marker not found in manifest.yml")
text = text[:idx] + "\n".join(block) + "\n" + text[idx:]
manifest.write_text(text, encoding="utf-8")
PYASSETS
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --image-source)
      case "${2:-}" in
        local|pull) IMAGE_SOURCE="${2}"; shift 2 ;;
        *) die "unsupported image source: ${2:-} (use local or pull)" ;;
      esac
      ;;
    --platform)
      case "${2:-}" in
        amd64|arm64) PLATFORM="${2}"; shift 2 ;;
        *) die "unsupported platform: ${2:-} (use amd64 or arm64)" ;;
      esac
      ;;
    --profile)
      die "profile is selected during VOS install; package.sh creates one tarball per run"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

require_cmd curl
select_python
require_cmd tar
if [[ "$IMAGE_SOURCE" == "local" ]]; then
  require_cmd docker
  require_cmd gzip
  PLATFORM="${PLATFORM:-$(detect_platform)}"
fi
validate_migration_versions
mkdir -p "$DIST_DIR"
acquire_lock

if [[ -n "${PACKAGE_VERSION:-}" ]]; then
  [[ "$PACKAGE_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "invalid PACKAGE_VERSION: $PACKAGE_VERSION"
  APP_VERSION="$PACKAGE_VERSION"
else
  APP_VERSION="$(read_version)"
fi
[[ "$APP_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "invalid VERSION: $APP_VERSION"
log "Package version: ${APP_VERSION}"
log "Image source: ${IMAGE_SOURCE}"
[[ "$IMAGE_SOURCE" == "local" ]] && log "Embedded platform: ${PLATFORM}"
load_feishu_auth

rm -rf "$STAGE_DIR"
mkdir -p "$STAGE_DIR"

ENV_FILE="${STAGE_DIR}/.env"
resolve_image_versions "$ENV_FILE"

for file in manifest.yml configs.yml routers.yml traefik.yml README.zh-CN.md README.en.md; do
  if [[ -f "${SRC_DIR}/${file}" ]]; then
    render_text_file "${SRC_DIR}/${file}" "${STAGE_DIR}/${file}"
  fi
done
if [[ -f "${SRC_DIR}/icon.png" ]]; then
  cp "${SRC_DIR}/icon.png" "${STAGE_DIR}/icon.png"
fi
render_compose_file "${SRC_DIR}/docker-compose.yml" "${STAGE_DIR}/docker-compose.yml" "$ENV_FILE"
if grep -q '\${[A-Z0-9_]*_IMAGE}' "${STAGE_DIR}/docker-compose.yml"; then
  grep '\${[A-Z0-9_]*_IMAGE}' "${STAGE_DIR}/docker-compose.yml" >&2
  die "unresolved image placeholder remains in rendered docker-compose.yml"
fi
validate_staged_files

if [[ "$IMAGE_SOURCE" == "local" ]]; then
  PACKAGE_NAME="${APP_NAME}_${APP_VERSION}_${PLATFORM}.tar"
else
  PACKAGE_NAME="${APP_NAME}_${APP_VERSION}_pull.tar"
fi
APP_TARBALL="${DIST_DIR}/app.tar.gz"
PACKAGE_PATH="${DIST_DIR}/${PACKAGE_NAME}"

rm -rf "$PACKAGE_ROOT"
mkdir -p "$PACKAGE_ROOT"
if [[ "$IMAGE_SOURCE" == "local" ]]; then
  embed_local_images
fi
TAR_FILES=(.env manifest.yml docker-compose.yml configs.yml routers.yml icon.png README.zh-CN.md README.en.md)
[[ -f "${STAGE_DIR}/traefik.yml" ]] && TAR_FILES+=(traefik.yml)
tar czf "$APP_TARBALL" -C "$STAGE_DIR" "${TAR_FILES[@]}"
cp "$APP_TARBALL" "${PACKAGE_ROOT}/app.tar.gz"
if [[ "$IMAGE_SOURCE" == "local" ]]; then
  tar cf "$PACKAGE_PATH" -C "$PACKAGE_ROOT" app.tar.gz assets
else
  tar cf "$PACKAGE_PATH" -C "$PACKAGE_ROOT" app.tar.gz
fi
verify_package "$PACKAGE_PATH" "$APP_TARBALL"

log "Done: ${PACKAGE_PATH}"
