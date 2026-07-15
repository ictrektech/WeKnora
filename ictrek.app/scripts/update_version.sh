#!/usr/bin/env bash
set -euo pipefail

APP_LABEL="weknora"
TAG_PREFIX="vos-weknora-v"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
VERSION_FILE="${APP_DIR}/VERSION"
MANIFEST_FILE="${APP_DIR}/src/manifest.yml"
REPO_ROOT="$(git -C "$APP_DIR" rev-parse --show-toplevel)"

usage() {
  cat <<'EOF'
Usage:
  ./scripts/update_version.sh [patch|minor|major]

Increments ictrek.app/VERSION, refreshes dependency versions from the local
monorepo when available, packages the pull-mode VOS tarball by reading Feishu
component image versions, commits the changes, pushes the branch, and publishes
the tarball with GitHub CLI.

Required tools: git, python3, tar, curl, gh.
EOF
}

log() { echo "[INFO] $*"; }
die() { echo "[ERROR] $*" >&2; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing command: $1"
}

bump_version() {
  local part="$1" current major minor patch
  current="$(tr -d '[:space:]' < "$VERSION_FILE")"
  [[ "$current" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "invalid VERSION: $current"
  IFS=. read -r major minor patch <<< "$current"
  case "$part" in
    patch) patch=$((patch + 1)) ;;
    minor) minor=$((minor + 1)); patch=0 ;;
    major) major=$((major + 1)); minor=0; patch=0 ;;
    *) usage >&2; exit 1 ;;
  esac
  printf '%s.%s.%s\n' "$major" "$minor" "$patch"
}

github_repo() {
  local url repo=""
  if [[ -n "${WEKNORA_RELEASE_REPO:-}" ]]; then
    printf '%s\n' "$WEKNORA_RELEASE_REPO"
    return 0
  fi
  url="$(git remote get-url origin 2>/dev/null || true)"
  case "$url" in
    git@github.com:*)
      repo="${url#git@github.com:}"
      repo="${repo%.git}"
      ;;
    https://github.com/*)
      repo="${url#https://github.com/}"
      repo="${repo%.git}"
      ;;
  esac
  if [[ "$repo" =~ ^[^/]+/[^/]+$ ]]; then
    printf '%s\n' "$repo"
    return 0
  fi
  gh repo view --json nameWithOwner -q .nameWithOwner
}

github_org() {
  local repo="$1"
  printf '%s\n' "${repo%%/*}"
}

latest_release_asset_version() {
  local repo="$1"
  local asset_prefix="$2"
  local resp
  resp="$(gh api "repos/${repo}/releases?per_page=100" 2>&1)" || die "cannot query GitHub releases for ${repo}: ${resp}"
  python3 - "$asset_prefix" "$resp" <<'PY'
import json
import re
import sys

prefix, resp = sys.argv[1], sys.argv[2]
data = json.loads(resp)
pattern = re.compile(rf"^{re.escape(prefix)}_([0-9]+\.[0-9]+\.[0-9]+)_pull\.tar$")
versions = []
for release in data:
    for asset in release.get("assets", []):
        name = asset.get("name") or ""
        match = pattern.match(name)
        if match:
            versions.append(tuple(int(part) for part in match.group(1).split(".")))
if not versions:
    raise SystemExit(f"no release asset found for {prefix}_*_pull.tar")
latest = max(versions)
print(".".join(str(part) for part in latest))
PY
}

refresh_dependency_versions() {
  local repo org model_hub_repo pgv_repo model_hub_version pgv_version
  repo="$(github_repo)"
  [[ -n "$repo" ]] || die "cannot detect GitHub repo with gh"
  org="$(github_org "$repo")"
  model_hub_repo="${WEKNORA_MODEL_HUB_RELEASE_REPO:-${org}/model_hub}"
  pgv_repo="${WEKNORA_PGV_RELEASE_REPO:-${org}/pgv}"
  model_hub_version="$(latest_release_asset_version "$model_hub_repo" "model_hub")"
  pgv_version="$(latest_release_asset_version "$pgv_repo" "pgv")"
  [[ "$model_hub_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "invalid model_hub release version: $model_hub_version"
  [[ "$pgv_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "invalid pgv release version: $pgv_version"

  python3 - "$MANIFEST_FILE" "$model_hub_version" "$pgv_version" <<'PY'
import sys
from pathlib import Path

path = Path(sys.argv[1])
versions = {
    "com.ictrek.model-hub": f">={sys.argv[2]}",
    "com.ictrek.pgv": f">={sys.argv[3]}",
}
lines = path.read_text(encoding="utf-8").splitlines()
current_id = None
out = []
for line in lines:
    stripped = line.strip()
    if stripped.startswith("- id: "):
        current_id = stripped.split(":", 1)[1].strip().strip('"').strip("'")
    elif stripped.startswith("version: ") and current_id in versions:
        indent = line[: len(line) - len(line.lstrip())]
        line = f'{indent}version: "{versions[current_id]}"'
        current_id = None
    out.append(line)
path.write_text("\n".join(out) + "\n", encoding="utf-8")
PY
  log "Dependency versions refreshed from GitHub releases: ${model_hub_repo} model_hub>=${model_hub_version}, ${pgv_repo} pgv>=${pgv_version}"
}

part="${1:-patch}"
[[ "${1:-}" != "-h" && "${1:-}" != "--help" ]] || { usage; exit 0; }

require_cmd git
require_cmd python3
require_cmd tar
require_cmd curl
require_cmd gh

cd "$REPO_ROOT"
git diff --quiet && git diff --cached --quiet || {
  die "worktree is not clean; commit code changes before releasing"
}

version="$(bump_version "$part")"
trigger_tag="${TAG_PREFIX}${version}"
public_tag="v${version}"
git rev-parse -q --verify "refs/tags/${trigger_tag}" >/dev/null && die "tag already exists: ${trigger_tag}"
git rev-parse -q --verify "refs/tags/${public_tag}" >/dev/null && die "public release tag already exists: ${public_tag}"

printf '%s\n' "$version" > "$VERSION_FILE"
refresh_dependency_versions

PACKAGE_VERSION="$version" "${APP_DIR}/scripts/package.sh"
package_path="${APP_DIR}/dist/${APP_LABEL}_${version}_pull.tar"
[[ -f "$package_path" ]] || die "package not found: $package_path"

git add "$VERSION_FILE" "$MANIFEST_FILE"
git commit -m "chore: release VOS ${APP_LABEL} ${version}"
git tag "$trigger_tag"
branch="$(git branch --show-current)"
git push origin "$branch"
git push origin "$trigger_tag"

if gh release view "$public_tag" >/dev/null 2>&1; then
  log "GitHub release ${public_tag} exists; uploading tarball"
  gh release upload "$public_tag" "$package_path" --clobber
else
  gh release create "$public_tag" "$package_path" \
    --title "v${version}" \
    --notes "WeKnora VOS pull package ${version}."
fi

log "Published ${package_path} to GitHub release ${public_tag}"
