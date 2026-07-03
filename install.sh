#!/bin/sh
# Install the ainpt CLI from the latest GitHub release.
#   curl -fsSL https://raw.githubusercontent.com/ryan-alexander-zhang/ainpt/main/install.sh | sh
set -e

OWNER="ryan-alexander-zhang"
REPO="ainpt"
BIN="ainpt"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64 | amd64) ARCH=amd64 ;;
  arm64 | aarch64) ARCH=arm64 ;;
  *) echo "unsupported architecture: $ARCH" >&2; exit 1 ;;
esac
case "$OS" in
  linux | darwin) ;;
  *) echo "unsupported OS: $OS" >&2; exit 1 ;;
esac

ASSET="${BIN}_${OS}_${ARCH}.tar.gz"
BASE="https://github.com/${OWNER}/${REPO}/releases/latest/download"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "Downloading ${ASSET} ..."
curl -fsSL "${BASE}/${ASSET}" -o "${TMP}/${ASSET}"

# Best-effort checksum verification.
if curl -fsSL "${BASE}/checksums.txt" -o "${TMP}/checksums.txt" 2>/dev/null; then
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$TMP" && grep " ${ASSET}\$" checksums.txt | sha256sum -c - >/dev/null 2>&1) \
      && echo "checksum ok" || echo "warning: checksum not verified"
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$TMP" && grep " ${ASSET}\$" checksums.txt | shasum -a 256 -c - >/dev/null 2>&1) \
      && echo "checksum ok" || echo "warning: checksum not verified"
  fi
fi

tar -xzf "${TMP}/${ASSET}" -C "$TMP"

DEST="/usr/local/bin"
if [ -w "$DEST" ]; then
  install -m 0755 "${TMP}/${BIN}" "${DEST}/${BIN}"
elif command -v sudo >/dev/null 2>&1; then
  sudo install -m 0755 "${TMP}/${BIN}" "${DEST}/${BIN}"
else
  DEST="${HOME}/.local/bin"
  mkdir -p "$DEST"
  install -m 0755 "${TMP}/${BIN}" "${DEST}/${BIN}"
fi

echo "Installed ${BIN} to ${DEST}"
echo "Run: ${BIN} new my-project"
