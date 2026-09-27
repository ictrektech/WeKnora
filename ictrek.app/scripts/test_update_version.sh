#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_DIR="$(mktemp -d)"
trap 'rm -rf "$TEST_DIR"' EXIT

# Use only disposable repositories and local remotes; never publish a release.
run_case() (
  local scenario="$1" expected="$2" mode="--with-changelog"
  local repo="$TEST_DIR/$scenario" remote="$TEST_DIR/$scenario.git" log="$TEST_DIR/$scenario.log"
  git init --quiet --bare "$remote"
  git init --quiet "$repo"
  cd "$repo"
  git config user.name 'Release Test'
  git config user.email 'release-test@example.invalid'
  git config commit.gpgsign false
  git config tag.gpgsign false
  git config core.hooksPath /dev/null
  git remote add origin "$remote"
  mkdir -p ictrek.app/scripts
  cp "$SCRIPT_DIR/update_version.sh" ictrek.app/scripts/
  printf '0.1.63\n' > ictrek.app/VERSION
  printf '## [0.1.64] - 2026-09-27\n' > ictrek.app/CHANGELOG.md
  if [[ "$scenario" == missing-version ]]; then
    printf '## [Unreleased]\n' > ictrek.app/CHANGELOG.md
  fi
  git add .
  git commit --quiet -m initial
  local initial_head
  initial_head="$(git rev-parse HEAD)"
  case "$scenario" in
    modified|staged|default-dirty)
      printf '\n- Prepared release note\n' >> ictrek.app/CHANGELOG.md
      [[ "$scenario" != staged ]] || git add ictrek.app/CHANGELOG.md
      ;;
    unrelated) printf 'unrelated\n' > other.txt ;;
  esac
  [[ "$scenario" != default-* ]] || mode=""
  local result=0
  bash ictrek.app/scripts/update_version.sh patch ${mode:+"$mode"} > "$log" 2>&1 || result=$?
  if [[ "$expected" == success ]]; then
    [[ "$result" == 0 ]] || { cat "$log"; exit 1; }
    [[ "$(cat ictrek.app/VERSION)" == 0.1.64 ]]
    [[ "$(git show HEAD:ictrek.app/CHANGELOG.md)" == "$(cat ictrek.app/CHANGELOG.md)" ]]
    [[ "$(git --git-dir="$remote" rev-parse refs/tags/vos-hybrag-v0.1.64)" == "$(git rev-parse HEAD)" ]]
    [[ "$(git --git-dir="$remote" rev-parse "refs/heads/$(git branch --show-current)")" == "$(git rev-parse HEAD)" ]]
  else
    [[ "$result" != 0 ]]
    [[ "$(cat ictrek.app/VERSION)" == 0.1.63 ]]
    [[ "$(git rev-parse HEAD)" == "$initial_head" ]]
    [[ -z "$(git --git-dir="$remote" for-each-ref --format='%(refname)')" ]]
    case "$scenario" in
      missing-version) grep -Fq 'must contain ## [0.1.64]' "$log" ;;
      unrelated) grep -Fq 'worktree has changes outside ictrek.app/CHANGELOG.md: other.txt' "$log" ;;
      default-dirty) grep -Fq 'worktree is not clean' "$log" ;;
    esac
  fi
  printf 'PASS %s\n' "$scenario"
)

for scenario in committed modified staged default-clean; do
  run_case "$scenario" success
done
for scenario in missing-version unrelated default-dirty; do
  run_case "$scenario" failure
done
