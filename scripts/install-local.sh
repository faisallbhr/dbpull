#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"
INSTALL_DIR="${HOME}/.local/bin"
TARGET="${INSTALL_DIR}/dbpull"

detect_os() {
	case "$(uname -s)" in
	Linux) printf 'linux\n' ;;
	Darwin) printf 'darwin\n' ;;
	*)
		echo "Unsupported OS: $(uname -s)" >&2
		exit 1
		;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
	x86_64|amd64) printf 'amd64\n' ;;
	arm64|aarch64) printf 'arm64\n' ;;
	*)
		echo "Unsupported architecture: $(uname -m)" >&2
		exit 1
		;;
	esac
}

OS_NAME="$(detect_os)"
ARCH_NAME="$(detect_arch)"
PATTERN="$DIST_DIR/dbpull-*-${OS_NAME}-${ARCH_NAME}"

shopt -s nullglob
matches=($PATTERN)
shopt -u nullglob

if [[ "${#matches[@]}" -eq 0 ]]; then
	echo "No matching local build found for ${OS_NAME}/${ARCH_NAME} in dist/" >&2
	echo "Run: make build-all" >&2
	exit 1
fi

mkdir -p "$INSTALL_DIR"
install -m 0755 "${matches[0]}" "$TARGET"

case ":${PATH}:" in
*":${INSTALL_DIR}:"*) ;;
*)
	echo "Warning: ${INSTALL_DIR} is not in PATH" >&2
	;;
esac

echo "Installed ${TARGET}"
