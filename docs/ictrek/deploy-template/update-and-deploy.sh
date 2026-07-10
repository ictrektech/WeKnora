#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_URL="${WEKNORA_DEPLOY_REPO:-git@github.com:ictrektech/WeKnora.git}"
REPO_REF="${WEKNORA_DEPLOY_REF:-main}"
PLATFORM="${PLATFORM:-}"
DRY_RUN=0

usage() {
  cat <<'EOF'
Usage: ./update-and-deploy.sh --platform amd|l4t|thor [deploy.sh args...]

Pulls the latest docs/ictrek deployment files, syncs deploy-template into this
directory while preserving local .env files, then runs the platform deploy
script. Intended for a future "update deployment" web button.

Environment:
  WEKNORA_DEPLOY_REPO  Git repo to pull from, default git@github.com:ictrektech/WeKnora.git
  WEKNORA_DEPLOY_REF   Git ref to pull, default main
  PLATFORM           Alternative to --platform
EOF
}

log() { echo "[INFO] $*"; }
die() { echo "[ERROR] $*" >&2; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing command: $1"
}

ghfast_url() {
  python3 - "$1" <<'PY'
import re, sys
url = sys.argv[1]
if url.startswith("https://github.com/"):
    print("https://ghfast.top/" + url)
elif url.startswith("git@github.com:"):
    print("https://ghfast.top/https://github.com/" + url.split(":", 1)[1])
else:
    print("")
PY
}

clone_repo() {
  local url="$1"
  local ref="$2"
  local dest="$3"
  local fast
  if git clone --quiet --filter=blob:none --sparse --depth 1 --branch "$ref" "$url" "$dest"; then
    return 0
  fi
  fast="$(ghfast_url "$url")"
  [[ -n "$fast" ]] || return 1
  log "normal GitHub clone failed; retrying via ghfast.top"
  git clone --quiet --filter=blob:none --sparse --depth 1 --branch "$ref" "$fast" "$dest"
}

deploy_args=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --platform)
      PLATFORM="$2"
      deploy_args+=("$1" "$2")
      shift 2
      ;;
    --dry-run)
      DRY_RUN=1
      deploy_args+=("$1")
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      deploy_args+=("$1")
      shift
      ;;
  esac
done

[[ -n "$PLATFORM" ]] || die "--platform amd|l4t|thor is required"
case "$PLATFORM" in
  amd|l4t|thor) ;;
  *) die "unsupported platform: $PLATFORM" ;;
esac

require_cmd git
require_cmd rsync

tmp="$(mktemp -d)"
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT

log "pulling docs/ictrek from ${REPO_URL}@${REPO_REF}"
clone_repo "$REPO_URL" "$REPO_REF" "$tmp/repo"
git -C "$tmp/repo" sparse-checkout set docs/ictrek

changes="$(rsync -az --delete --dry-run --itemize-changes \
  --exclude='.env' \
  --exclude='.env.*' \
  --exclude='data/' \
  --exclude='*.log' \
  --exclude='config/builtin_models*.yaml' \
  "$tmp/repo/docs/ictrek/deploy-template/" "$ROOT_DIR/" || true)"
if [[ -n "$changes" ]]; then
  export WEKNORA_DEPLOY_CONFIG_CHANGED=1
  log "deployment files changed"
  rsync -az --delete \
    --exclude='.env' \
    --exclude='.env.*' \
    --exclude='data/' \
    --exclude='*.log' \
    --exclude='config/builtin_models*.yaml' \
    "$tmp/repo/docs/ictrek/deploy-template/" "$ROOT_DIR/"
else
  export WEKNORA_DEPLOY_CONFIG_CHANGED=0
  log "deployment files unchanged"
fi

chmod +x "$ROOT_DIR"/*.sh

if [[ "$DRY_RUN" == "1" ]]; then
  log "dry-run: config_changed=${WEKNORA_DEPLOY_CONFIG_CHANGED}; deploy args: ${deploy_args[*]}"
  exit 0
fi

exec "$ROOT_DIR/deploy.sh" "${deploy_args[@]}"
