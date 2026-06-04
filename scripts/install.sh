#!/bin/sh
set -e

REPO="inferbolthq/inferbolt"
BINARY="inferbolt"
INSTALL_DIR="/usr/local/bin"

detect_os() {
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  case "$OS" in
    linux*)  echo "linux" ;;
    darwin*) echo "darwin" ;;
    *)       echo "Unsupported OS: $OS" >&2; exit 1 ;;
  esac
}

detect_arch() {
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64)  echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *)       echo "Unsupported arch: $ARCH" >&2; exit 1 ;;
  esac
}

latest_version() {
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | sed -E 's/.*"([^"]+)".*/\1/'
}

main() {
  OS=$(detect_os)
  ARCH=$(detect_arch)
  VERSION=$(latest_version)

  FILENAME="inferbolt_${OS}_${ARCH}.tar.gz"
  URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"

  echo "Installing InferBolt ${VERSION} for ${OS}/${ARCH}..."

  TMP=$(mktemp -d)
  curl -fsSL "$URL" | tar -xz -C "$TMP"
  install -m 755 "$TMP/$BINARY" "$INSTALL_DIR/$BINARY"
  rm -rf "$TMP"

  echo "InferBolt installed to $INSTALL_DIR/$BINARY"
  echo "Run 'inferbolt version' to verify"
  echo "Run 'inferbolt configure' to set up your server URL and API key"
}

main
