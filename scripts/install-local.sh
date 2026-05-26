#!/bin/bash
# Build modernpath from source and install to common PATH locations.
# Use this after pulling CLI changes — Homebrew tap builds lag behind main.
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${MODERNPATH_VERSION:-0.1.2}"
LDFLAGS="-X github.com/modernpath/cli/cmd.Version=${VERSION}"

echo "Building modernpath ${VERSION}..."
go build -ldflags "${LDFLAGS}" -o modernpath .

for dest in ./modernpath /usr/local/bin/modernpath /opt/homebrew/bin/modernpath; do
  if [[ -d "$(dirname "$dest")" ]]; then
    echo "Installing to ${dest}"
    install -m 755 modernpath "$dest"
  fi
done

echo "Done. Verify:"
echo "  $(command -v modernpath) --version"
echo "  # should print: modernpath version ${VERSION}"
