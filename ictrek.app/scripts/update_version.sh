#!/usr/bin/env bash
set -euo pipefail

APP_LABEL="hybrag"
TAG_PREFIX="vos-hybrag-v"
APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION_FILE="${APP_DIR}/VERSION"
CHANGELOG_FILE="${APP_DIR}/CHANGELOG.md"
REPO_ROOT="$(git -C "$APP_DIR" rev-parse --show-toplevel)"
CHANGELOG_PATH="${CHANGELOG_FILE#"$REPO_ROOT"/}"

usage() {
  cat <<'EOF'
Usage:
  ./scripts/update_version.sh [patch|minor|major] [--with-changelog]

Updates ictrek.app/VERSION, commits it, creates a VOS CI trigger tag, and
pushes the branch and tag. GitHub Actions publishes the pull-mode tar on a
standard SemVer release tag.
Commit application code changes before running this script.

Options:
  --with-changelog  include the prepared ictrek.app/CHANGELOG.md in the
                     release commit; no other pending changes are allowed.
EOF
}

bump_version_from() {
  local current="$1" part="$2" major minor patch
  [[ "$current" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
    echo "invalid VERSION: $current" >&2
    exit 1
  }
  IFS=. read -r major minor patch <<< "$current"
  case "$part" in
    patch) patch=$((patch + 1)) ;;
    minor) minor=$((minor + 1)); patch=0 ;;
    major) major=$((major + 1)); minor=0; patch=0 ;;
    *) usage >&2; exit 1 ;;
  esac
  printf '%s.%s.%s\n' "$major" "$minor" "$patch"
}

remote_tag_exists() {
  local tag="$1"
  git ls-remote --exit-code --tags origin "refs/tags/${tag}" >/dev/null 2>&1
}

part=""
include_changelog=false
while (($#)); do
  case "$1" in
    patch|minor|major)
      [[ -z "$part" ]] || { usage >&2; exit 1; }
      part="$1"
      ;;
    --with-changelog)
      [[ "$include_changelog" == false ]] || { usage >&2; exit 1; }
      include_changelog=true
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 1
      ;;
  esac
  shift
done
part="${part:-patch}"

cd "$REPO_ROOT"
if [[ "$include_changelog" == true ]]; then
  [[ -f "$CHANGELOG_FILE" ]] || {
    echo "--with-changelog requires ${CHANGELOG_PATH}; prepare it with cl first" >&2
    exit 1
  }

  changed_files="$({
    git diff --name-only
    git diff --cached --name-only
    git ls-files --others --exclude-standard
  } | sort -u)"
  [[ -n "$changed_files" ]] || {
    echo "--with-changelog requires a modified ${CHANGELOG_PATH}" >&2
    exit 1
  }
  while IFS= read -r path; do
    [[ -z "$path" ]] && continue
    [[ "$path" == "$CHANGELOG_PATH" ]] || {
      echo "worktree has changes outside ${CHANGELOG_PATH}: ${path}" >&2
      exit 1
    }
  done <<< "$changed_files"
else
  if ! git diff --quiet || ! git diff --cached --quiet || [[ -n "$(git ls-files --others --exclude-standard)" ]]; then
    echo "worktree is not clean; commit code changes before releasing" >&2
    exit 1
  fi
fi

version="$(bump_version_from "$(tr -d '[:space:]' < "$VERSION_FILE")" "$part")"
tag="${TAG_PREFIX}${version}"
public_tag="v${version}"

if [[ "$include_changelog" == true ]] && ! grep -Eq "^## \[${version//./\.}\] - [0-9]{4}-[0-9]{2}-[0-9]{2}$" "$CHANGELOG_FILE"; then
  echo "${CHANGELOG_PATH} must contain ## [${version}] - YYYY-MM-DD" >&2
  exit 1
fi

if remote_tag_exists "$tag"; then
  echo "VOS trigger tag already exists on origin: ${tag}" >&2
  exit 1
fi

if remote_tag_exists "$public_tag"; then
  echo "public release tag already exists on origin: ${public_tag}" >&2
  exit 1
fi

printf '%s\n' "$version" > "$VERSION_FILE"
if [[ "$include_changelog" == true ]]; then
  git add -- "$VERSION_FILE" "$CHANGELOG_FILE"
else
  git add -- "$VERSION_FILE"
fi
git diff --cached --check
git commit -m "chore: release VOS ${APP_LABEL} ${version}"
git tag -f "$tag"
branch="$(git branch --show-current)"
git push origin "$branch"
git push origin "$tag"

echo "Pushed ${tag}. GitHub Actions will build the pull tar and create release ${public_tag}."
