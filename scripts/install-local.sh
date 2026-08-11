#!/bin/bash
# Build modernpath from source and install to common PATH locations.
# Use this after pulling CLI changes — Homebrew tap builds lag behind main.
set -euo pipefail

cd "$(dirname "$0")/.."

BASE="${MODERNPATH_VERSION:-0.5.0}"
# Stamp the commit and date, not just a hand-bumped number: a version string that
# never changes cannot tell a fresh build from one three weeks old, which is how
# a stale binary ran the hooks unnoticed (RUN:2026-08-11).
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILT="$(date -u +%Y-%m-%dT%H:%MZ)"
VERSION="${BASE}+${COMMIT} (${BUILT})"
LDFLAGS="-X 'github.com/modernpath/cli/cmd.Version=${VERSION}'"

echo "Building modernpath ${VERSION}..."
go build -ldflags "${LDFLAGS}" -o modernpath .

# Every copy already on PATH, plus the usual homes. Writing only the usual homes
# leaves an older copy earlier in PATH to shadow the new one — the hooks call the
# CLI by bare name, so the shadow is what actually runs.
# `./modernpath` is the build output itself; installing it over itself aborts
# the script under `set -e`.
DESTS=(/usr/local/bin/modernpath /opt/homebrew/bin/modernpath "$HOME/.local/bin/modernpath")
while IFS= read -r found; do
  [[ -n "$found" ]] && DESTS+=("$found")
done < <(type -a -p modernpath 2>/dev/null || true)

# Plain string set: macOS ships bash 3.2, which has no associative arrays.
SEEN=""
SKIPPED=""
for dest in "${DESTS[@]}"; do
  case "$SEEN" in *"|$dest|"*) continue;; esac
  SEEN="$SEEN|$dest|"
  dir="$(dirname "$dest")"
  if [[ -d "$dir" ]]; then
    if [[ -w "$dir" ]]; then
      echo "Installing to ${dest}"
      install -m 755 modernpath "$dest"
    else
      # Not writable is not fatal — but a copy left behind here is a copy that
      # can shadow, so say so rather than skipping quietly.
      echo "SKIPPED (needs sudo): ${dest}"
      SKIPPED="${SKIPPED}${dest} "
    fi
  fi
done

if [[ -n "$SKIPPED" ]]; then
  echo
  echo "WARNING: not writable, so these are now STALE and may shadow the new build:"
  for s in $SKIPPED; do echo "  $s"; done
  echo "Re-run with sudo, or remove them: sudo rm $SKIPPED"
fi

echo "Done. Verify:"
echo "  modernpath --version      # expect: ${VERSION}"
echo "  modernpath hooks doctor   # names every copy on PATH and which one wins"
