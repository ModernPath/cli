#!/usr/bin/env bash
# Refresh the CLI's embedded process snapshot from a clean req-driven-dev checkout.
set -euo pipefail

mode=sync
case "$#:${1:-}" in
  1:*) source_argument=$1 ;;
  2:--check) mode=check; source_argument=$2 ;;
  *)
    echo "usage: $0 [--check] /path/to/req-driven-dev" >&2
    exit 2
    ;;
esac

source_root=$(cd "$source_argument" && pwd)
cli_root=$(cd "$(dirname "$0")/.." && pwd)
destination_root="$cli_root/internal/kit/assets/rdd"
source_record="$cli_root/internal/kit/assets/rdd-source.txt"

# The consolidated package replaced process/{V-model-loop,state-tracking}.md with
# a single PROCESS.md; require what the current layout actually contains.
for required in AGENTS.md PROCESS.md; do
  if [ ! -f "$source_root/$required" ]; then
    echo "error: $source_root is missing $required" >&2
    exit 1
  fi
done

if [ -n "$(git -C "$source_root" status --porcelain --untracked-files=all)" ]; then
  echo "error: req-driven-dev checkout must be clean before it is packaged" >&2
  exit 1
fi

revision=$(git -C "$source_root" rev-parse HEAD)
temporary_root=$(mktemp -d "${TMPDIR:-/tmp}/modernpath-rdd.XXXXXX")
trap 'rm -rf "$temporary_root"' EXIT
snapshot_root="$temporary_root/rdd"
expected_record="$temporary_root/rdd-source.txt"
mkdir -p "$snapshot_root"

while IFS= read -r relative_path; do
  mkdir -p "$snapshot_root/$(dirname "$relative_path")"
  cp "$source_root/$relative_path" "$snapshot_root/$relative_path"
done < <(git -C "$source_root" ls-files '*.md' '*.yaml' '*.yml' '*.mjs')

{
  printf 'repository=https://github.com/ModernPath/req-driven-dev\n'
  printf 'revision=%s\n' "$revision"
} > "$expected_record"

if [ "$mode" = check ]; then
  diff -qr "$snapshot_root" "$destination_root"
  cmp "$expected_record" "$source_record"
  echo "Embedded req-driven-dev snapshot matches $revision"
  exit 0
fi

mkdir -p "$destination_root"
find "$destination_root" -mindepth 1 -delete
cp -R "$snapshot_root"/. "$destination_root"/
cp "$expected_record" "$source_record"

echo "Embedded req-driven-dev snapshot $revision"
