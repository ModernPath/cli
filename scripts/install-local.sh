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
# LOCAL-ONLY MODE: a throwaway checkout must not overwrite the modernpath
# binaries other workspaces resolve by bare name — their hooks would silently
# start running this experiment. With --local-only the build lands in one
# workspace-private path and PATH is left untouched; call that path explicitly.
if [[ "${1:-}" == "--local-only" || -n "${MODERNPATH_LOCAL_ONLY:-}" ]]; then
  LOCAL_ONLY=1
  LOCAL_DEST="$(cd ../../.. && pwd)/.modernpath/bin/modernpath"
  mkdir -p "$(dirname "$LOCAL_DEST")"
  DESTS=("$LOCAL_DEST")
else
  LOCAL_ONLY=0
  DESTS=(/usr/local/bin/modernpath /opt/homebrew/bin/modernpath "$HOME/.local/bin/modernpath")
  while IFS= read -r found; do
    [[ -n "$found" ]] && DESTS+=("$found")
  done < <(type -a -p modernpath 2>/dev/null || true)
fi

# Plain string set: macOS ships bash 3.2, which has no associative arrays.
SEEN=""
SKIPPED=""
INSTALLED=""
for dest in "${DESTS[@]}"; do
  case "$SEEN" in *"|$dest|"*) continue;; esac
  SEEN="$SEEN|$dest|"
  dir="$(dirname "$dest")"
  if [[ -d "$dir" ]]; then
    if [[ -w "$dir" ]]; then
      echo "Installing to ${dest}"
      install -m 755 modernpath "$dest"
      INSTALLED="${INSTALLED}${dest}
"
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

# Read the effect back rather than printing instructions to read it back.
# An installed copy can be the right SIZE and the wrong BYTES: on RUN:2026-08-14
# a corrupt ~/.local/bin/modernpath hung on every subcommand — `--version`
# included — with no output and no error, and because it shadows homebrew, every
# install/check/sync silently did nothing for hours. `ls -l` could not see it;
# `cmp` and an actual run could. Telling the operator to verify is not verifying.
echo
FAILED=""
while IFS= read -r dest; do
  [[ -n "$dest" ]] || continue
  if ! cmp -s modernpath "$dest"; then
    echo "  BROKEN ${dest} — installed bytes differ from the build"
    FAILED="${FAILED}${dest} "
    continue
  fi
  # A bounded run, so a hanging binary fails the installer instead of waiting
  # to fail every command afterwards. `timeout` is coreutils and may be absent.
  if command -v timeout >/dev/null 2>&1; then
    got="$(timeout 20 "$dest" --version 2>&1)" || got=""
  else
    got="$("$dest" --version 2>&1)" || got=""
  fi
  if [[ "$got" != *"${COMMIT}"* ]]; then
    echo "  BROKEN ${dest} — did not report this build (got: ${got:-<no output>})"
    FAILED="${FAILED}${dest} "
  else
    echo "  ok ${dest}"
  fi
done <<< "$INSTALLED"

if [[ -n "$FAILED" ]]; then
  echo
  echo "ERROR: an installed copy does not run this build. PATH order decides which"
  echo "one the hooks execute, so a broken copy makes every command fail silently."
  echo "Reinstall it, or remove it: rm $FAILED"
  exit 1
fi

# The kit is COMPILED IN, so an asset edited after the last build is invisible to
# every workspace this binary then installs into — and `modernpath install`
# reports success while writing the previous build's bytes. That bit on
# RUN:2026-08-14: a skill was edited, copied to the kit source, and propagated to
# a sibling workspace from a binary built before the copy. `install --check`
# compares what is installed against what this CLI embeds, so running it here
# closes the loop the moment the loop exists.
ROOT="$(cd ../../.. 2>/dev/null && pwd || true)"
if [[ -n "$ROOT" && -d "$ROOT/.claude" ]]; then
  echo
  if [[ "$LOCAL_ONLY" == "1" ]]; then VERIFY_BIN="$LOCAL_DEST"; else VERIFY_BIN="$HOME/.local/bin/modernpath"; fi
  if (cd "$ROOT" && "$VERIFY_BIN" install --check >/dev/null 2>&1); then
    echo "  ok  installed process matches this build"
  else
    echo "  DRIFT: $ROOT/.claude differs from the assets this build embeds."
    echo "         Run 'modernpath install' there, or re-copy the edited file into"
    echo "         internal/kit/assets/ and rebuild — the kit is compiled in."
  fi
fi

echo
echo "Done. ${VERSION}"
echo "  modernpath hooks doctor   # names every copy on PATH and which one wins"
