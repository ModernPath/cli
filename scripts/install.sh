#!/usr/bin/env bash
# ModernPath CLI installer for Linux and macOS (non-Homebrew)
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/ModernPath/cli/main/scripts/install.sh | bash
#   curl -fsSL ... | bash -s -- --version 0.1.2

set -euo pipefail

REPO="ModernPath/cli"
INSTALL_DIR="${MODERNPATH_INSTALL_DIR:-${HOME}/.local/bin}"
VERSION="${MODERNPATH_VERSION:-latest}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      VERSION="$2"
      shift 2
      ;;
    --install-dir)
      INSTALL_DIR="$2"
      shift 2
      ;;
    -h|--help)
      echo "Usage: install.sh [--version VERSION] [--install-dir DIR]"
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *)
    echo "Unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

if [[ "$VERSION" == "latest" ]]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -o '"tag_name": *"v[^"]*"' | head -1 | sed 's/.*"v\(.*\)"/\1/')"
fi

asset="modernpath-${os}-${arch}.tar.gz"
tag="v${VERSION}"
url="https://github.com/${REPO}/releases/download/${tag}/${asset}"
checksum_url="https://github.com/${REPO}/releases/download/${tag}/checksums.txt"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

echo "→ Installing modernpath ${VERSION} (${os}/${arch})"
echo "→ Downloading ${url}"
curl -fsSL "$url" -o "${tmpdir}/${asset}"

if curl -fsSL "$checksum_url" -o "${tmpdir}/checksums.txt" 2>/dev/null; then
  expected="$(grep "${asset}" "${tmpdir}/checksums.txt" | awk '{print $1}')"
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "${tmpdir}/${asset}" | awk '{print $1}')"
  else
    actual="$(shasum -a 256 "${tmpdir}/${asset}" | awk '{print $1}')"
  fi
  if [[ -n "$expected" && "$expected" != "$actual" ]]; then
    echo "✗ Checksum mismatch for ${asset}" >&2
    exit 1
  fi
  echo "✓ Checksum verified"
fi

tar -xzf "${tmpdir}/${asset}" -C "$tmpdir"
mkdir -p "$INSTALL_DIR"
install -m 755 "${tmpdir}/modernpath" "${INSTALL_DIR}/modernpath"

echo "✓ Installed to ${INSTALL_DIR}/modernpath"
if ! echo ":${PATH}:" | grep -q ":${INSTALL_DIR}:"; then
  echo "⚠ Add ${INSTALL_DIR} to your PATH, e.g.:"
  echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
fi
echo ""
echo "Next: modernpath --version && modernpath auth"
