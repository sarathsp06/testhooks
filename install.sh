#!/bin/sh
# Install the testhooks binary from GitHub releases.
#
#   curl -fsSL https://raw.githubusercontent.com/sarathsp06/testhooks/main/install.sh | sh
#
# Environment:
#   TESTHOOKS_VERSION      release tag to install (default: latest)
#   TESTHOOKS_INSTALL_DIR  target directory (default: /usr/local/bin if writable, else ~/.local/bin)
#   TESTHOOKS_DOWNLOAD_URL base URL to download assets from (mirrors/testing)
set -eu

REPO="sarathsp06/testhooks"
BIN="testhooks"
VERSION="${TESTHOOKS_VERSION:-latest}"

say() { printf '%s\n' "$*" >&2; }
fail() { say "error: $*"; exit 1; }

fetch() { # fetch URL DEST
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1" -o "$2"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$2" "$1"
	else
		fail "curl or wget is required"
	fi
}

case "$(uname -s)" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	MINGW* | MSYS* | CYGWIN*) os=windows ;;
	*) fail "unsupported OS: $(uname -s)" ;;
esac

case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) fail "unsupported architecture: $(uname -m)" ;;
esac

ext=""
[ "$os" = windows ] && ext=".exe"
asset="${BIN}-${os}-${arch}${ext}"

if [ -n "${TESTHOOKS_DOWNLOAD_URL:-}" ]; then
	base="$TESTHOOKS_DOWNLOAD_URL"
elif [ "$VERSION" = latest ]; then
	base="https://github.com/${REPO}/releases/latest/download"
else
	base="https://github.com/${REPO}/releases/download/${VERSION}"
fi

if [ -n "${TESTHOOKS_INSTALL_DIR:-}" ]; then
	dir="$TESTHOOKS_INSTALL_DIR"
elif [ -w /usr/local/bin ]; then
	dir=/usr/local/bin
else
	dir="$HOME/.local/bin"
fi
mkdir -p "$dir"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

say "Downloading ${asset} (${VERSION})..."
fetch "${base}/${asset}" "${tmp}/${asset}" || fail "download failed: ${base}/${asset}"

# Verify the checksum when the release publishes one and a tool is available.
if fetch "${base}/checksums.txt" "${tmp}/checksums.txt" 2>/dev/null; then
	expected="$(grep " ${asset}\$" "${tmp}/checksums.txt" | cut -d' ' -f1)"
	if command -v sha256sum >/dev/null 2>&1; then
		actual="$(sha256sum "${tmp}/${asset}" | cut -d' ' -f1)"
	elif command -v shasum >/dev/null 2>&1; then
		actual="$(shasum -a 256 "${tmp}/${asset}" | cut -d' ' -f1)"
	else
		actual=""
	fi
	if [ -n "$expected" ] && [ -n "$actual" ]; then
		[ "$expected" = "$actual" ] || fail "checksum mismatch for ${asset}"
		say "Checksum verified."
	fi
fi

chmod +x "${tmp}/${asset}"
mv "${tmp}/${asset}" "${dir}/${BIN}${ext}"
say "Installed ${dir}/${BIN}${ext}"

case ":$PATH:" in
	*":$dir:"*) ;;
	*) say "Note: ${dir} is not on your PATH. Add it, e.g.: export PATH=\"${dir}:\$PATH\"" ;;
esac

say ""
say "Try it now with no database (data kept in memory):"
say "  DATABASE_URL=memory ${BIN}"
say "Then open http://localhost:8080"
say ""
say "For persistent storage, point DATABASE_URL at PostgreSQL. Docs: https://sarathsp06.github.io/testhooks/"
