#!/bin/sh
set -eu

REPO="faisallbhr/dbpull"
BASE_URL="https://github.com/${REPO}/releases"
INSTALL_DIR="${DBPULL_INSTALL_DIR:-$HOME/.local/bin}"
BINARY="dbpull"
MODIFY_PATH=1

err() {
	printf 'dbpull install: %s\n' "$*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 ||
		err "missing required command: $1"
}

usage() {
	cat <<-EOF
	Usage: install.sh [options]

	Options:
	  --no-modify-path   Don't add the install directory to PATH automatically
	  -h, --help         Show this help message

	Environment variables:
	  DBPULL_INSTALL_DIR   Install directory (default: \$HOME/.local/bin)
	  DBPULL_VERSION        Version to install (default: latest release)
	EOF
}

# --- parse args ---
for arg in "$@"; do
	case "$arg" in
		--no-modify-path)
			MODIFY_PATH=0
			;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			err "unknown argument: ${arg}"
			;;
	esac
done

fetch() {
	url="$1"
	output="$2"

	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$url" -o "$output"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$output" "$url"
	else
		err "missing required command: curl or wget"
	fi
}

detect_os() {
	case "$(uname -s)" in
		Linux) printf 'linux\n' ;;
		Darwin) printf 'darwin\n' ;;
		*) err "unsupported OS: $(uname -s)" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
		x86_64 | amd64) printf 'amd64\n' ;;
		arm64 | aarch64) printf 'arm64\n' ;;
		*) err "unsupported architecture: $(uname -m)" ;;
	esac
}

latest_version() {
	output="$1"

	fetch \
		"https://api.github.com/repos/${REPO}/releases/latest" \
		"$output"

	sed -n \
		's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' \
		"$output" |
		head -n 1
}

validate_version() {
	version="$1"

	case "$version" in
		v[0-9]*.[0-9]*.[0-9]*) ;;
		*) err "invalid release version: ${version}" ;;
	esac
}

verify_checksum() {
	checksums="$1"
	archive="$2"
	name="$3"

	expected="$(
		awk -v name="$name" '$2 == name { print $1; exit }' \
			"$checksums"
	)"

	[ -n "$expected" ] ||
		err "checksum not found for ${name}"

	if command -v sha256sum >/dev/null 2>&1; then
		actual="$(sha256sum "$archive" | awk '{print $1}')"
	elif command -v shasum >/dev/null 2>&1; then
		actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
	else
		err "missing required command: sha256sum or shasum"
	fi

	[ "$actual" = "$expected" ] ||
		err "checksum mismatch for ${name}"
}

add_to_path() {
	dir="$1"

	if [ "$dir" != "$HOME/.local/bin" ]; then
		printf \
			'dbpull install: warning: custom install directory %s was not added to PATH automatically\n' \
			"$dir" >&2
		printf \
			'Add it with: export PATH="%s:$PATH"\n' \
			"$dir" >&2
		return
	fi

	line='export PATH="$HOME/.local/bin:$PATH"'
	marker="# added by dbpull installer"

	case "${SHELL:-}" in
		*/zsh) rc="$HOME/.zshrc" ;;
		*/bash) rc="$HOME/.bashrc" ;;
		*) rc="$HOME/.profile" ;;
	esac

	[ -f "$rc" ] || : >"$rc"

	if ! grep -Fxq "$line" "$rc" 2>/dev/null; then
		{
			printf '\n%s\n' "$marker"
			printf '%s\n' "$line"
		} >>"$rc"

		printf \
			'dbpull install: added %s to PATH in %s\n' \
			"$dir" "$rc" >&2

		printf \
			'dbpull install: run "source %s" or open a new terminal\n' \
			"$rc" >&2
	fi
}

need uname
need tar
need awk
need sed
need head
need mktemp
need mkdir
need mv
need chmod
need grep

os="$(detect_os)"
arch="$(detect_arch)"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

version="${DBPULL_VERSION:-}"

if [ -z "$version" ]; then
	version="$(latest_version "$tmpdir/latest.json")"
fi

[ -n "$version" ] ||
	err "could not resolve latest release version"

validate_version "$version"

archive_name="dbpull-${version}-${os}-${arch}.tar.gz"
archive_path="$tmpdir/$archive_name"
checksums_path="$tmpdir/checksums.txt"

printf 'dbpull install: downloading %s\n' "$version"

fetch \
	"${BASE_URL}/download/${version}/${archive_name}" \
	"$archive_path"

fetch \
	"${BASE_URL}/download/${version}/checksums.txt" \
	"$checksums_path"

verify_checksum \
	"$checksums_path" \
	"$archive_path" \
	"$archive_name"

tar -xzf "$archive_path" -C "$tmpdir"

[ -f "$tmpdir/$BINARY" ] ||
	err "binary not found in ${archive_name}"

mkdir -p "$INSTALL_DIR"

temporary_target="$INSTALL_DIR/.dbpull-install-$$"

cp "$tmpdir/$BINARY" "$temporary_target"
chmod 0755 "$temporary_target"
mv -f "$temporary_target" "$INSTALL_DIR/$BINARY"

case ":$PATH:" in
	*":$INSTALL_DIR:"*) ;;
	*)
		if [ "$MODIFY_PATH" -eq 1 ]; then
			add_to_path "$INSTALL_DIR"
		else
			printf \
				'dbpull install: warning: %s is not in PATH\n' \
				"$INSTALL_DIR" >&2
			printf \
				'Add it with: export PATH="%s:$PATH"\n' \
				"$INSTALL_DIR" >&2
		fi
		;;
esac

printf 'dbpull %s installed to %s\n' \
	"$version" \
	"$INSTALL_DIR/$BINARY"

"$INSTALL_DIR/$BINARY" version