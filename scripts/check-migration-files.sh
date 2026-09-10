#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  printf 'Usage: %s MIGRATIONS_DIR\n' "$0" >&2
  exit 2
fi

migrations_dir="$1"
[[ -d "$migrations_dir" ]] || {
  printf 'Migration directory does not exist: %s\n' "$migrations_dir" >&2
  exit 1
}

declare -A versions=()
declare -A up_names=()
declare -A down_names=()
declare -a errors=()

while IFS= read -r -d '' file; do
  filename="${file##*/}"
  if [[ ! "$filename" =~ ^([0-9]+)_(.+)\.(up|down)\.sql$ ]]; then
    errors+=("malformed migration filename: ${filename}")
    continue
  fi

  version="${BASH_REMATCH[1]}"
  name="${BASH_REMATCH[2]}"
  direction="${BASH_REMATCH[3]}"
  versions["$version"]=1

  case "$direction" in
    up)
      if [[ -n "${up_names[$version]+x}" ]]; then
        errors+=("${version} has more than one .up.sql file: ${up_names[$version]}, ${filename}")
      else
        up_names["$version"]="$name"
      fi
      ;;
    down)
      if [[ -n "${down_names[$version]+x}" ]]; then
        errors+=("${version} has more than one .down.sql file: ${down_names[$version]}, ${filename}")
      else
        down_names["$version"]="$name"
      fi
      ;;
  esac
done < <(find "$migrations_dir" -maxdepth 1 -type f -name '*.sql' -print0 | sort -z)

while IFS= read -r version; do
  [[ -n "$version" ]] || continue

  if [[ -z "${up_names[$version]+x}" ]]; then
    errors+=("${version} is missing its .up.sql file")
  fi
  if [[ -z "${down_names[$version]+x}" ]]; then
    errors+=("${version} is missing its .down.sql file")
  fi
  if [[ -n "${up_names[$version]+x}" && -n "${down_names[$version]+x}" \
    && "${up_names[$version]}" != "${down_names[$version]}" ]]; then
    errors+=("${version} has mismatched migration names: ${up_names[$version]} vs ${down_names[$version]}")
  fi
done < <(printf '%s\n' "${!versions[@]}" | sort -n)

if ((${#errors[@]} > 0)); then
  printf 'Invalid migration files in %s:\n' "$migrations_dir" >&2
  printf ' - %s\n' "${errors[@]}" >&2
  exit 1
fi

printf 'Migration file pairs are valid: %s\n' "$migrations_dir"
