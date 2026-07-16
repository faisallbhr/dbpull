#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"
MODULE_PATH="$(cd "$ROOT_DIR" && go list -m)"
BUILDINFO_PKG="${MODULE_PATH}/internal/buildinfo"
README_FILE="$ROOT_DIR/README.md"
LICENSE_FILE="$ROOT_DIR/LICENSE"

version_from_git() {
	git -C "$ROOT_DIR" describe --tags --abbrev=0 2>/dev/null || true
}

commit_from_git() {
	git -C "$ROOT_DIR" rev-parse --short HEAD 2>/dev/null || true
}

resolve_version() {
	if [[ -n "${VERSION:-}" ]]; then
		printf '%s\n' "$VERSION"
		return
	fi

	local tag
	tag="$(version_from_git)"
	if [[ -n "$tag" ]]; then
		printf '%s\n' "$tag"
		return
	fi

	printf 'dev\n'
}

VERSION="$(resolve_version)"
COMMIT="${COMMIT:-$(commit_from_git)}"
COMMIT="${COMMIT:-unknown}"
BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
LDFLAGS="-s -w -X ${BUILDINFO_PKG}.Version=${VERSION} -X ${BUILDINFO_PKG}.Commit=${COMMIT} -X ${BUILDINFO_PKG}.BuildDate=${BUILD_DATE}"

build_binary() {
	local goos="$1"
	local goarch="$2"
	local ext="$3"
	local name="dbpull-${VERSION}-${goos}-${goarch}${ext}"

	echo "Building ${name}"
	CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
		go build -trimpath -buildvcs=false -ldflags="$LDFLAGS" -o "$DIST_DIR/$name" .
}

archive_tarball() {
	local binary_name="$1"
	local archive_name="$2"
	local temp_dir
	temp_dir="$(mktemp -d)"
	trap 'rm -rf "$temp_dir"' RETURN

	cp "$DIST_DIR/$binary_name" "$temp_dir/dbpull"
	cp "$README_FILE" "$temp_dir/README.md"
	if [[ -f "$LICENSE_FILE" ]]; then
		cp "$LICENSE_FILE" "$temp_dir/LICENSE"
	fi

	tar -C "$temp_dir" -czf "$DIST_DIR/$archive_name" .
	trap - RETURN
	rm -rf "$temp_dir"
}

archive_zip() {
	local binary_name="$1"
	local archive_name="$2"
	local temp_dir
	temp_dir="$(mktemp -d)"
	trap 'rm -rf "$temp_dir"' RETURN

	cp "$DIST_DIR/$binary_name" "$temp_dir/dbpull.exe"
	cp "$README_FILE" "$temp_dir/README.md"
	if [[ -f "$LICENSE_FILE" ]]; then
		cp "$LICENSE_FILE" "$temp_dir/LICENSE"
	fi

	if command -v zip >/dev/null 2>&1; then
		(
			cd "$temp_dir"
			zip -q "$DIST_DIR/$archive_name" ./*
		)
	else
		bsdtar -a -C "$temp_dir" -cf "$DIST_DIR/$archive_name" .
	fi

	trap - RETURN
	rm -rf "$temp_dir"
}

checksum_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$@"
		return
	fi

	shasum -a 256 "$@"
}

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

cd "$ROOT_DIR"

build_binary linux amd64 ""
build_binary linux arm64 ""
build_binary darwin amd64 ""
build_binary darwin arm64 ""
build_binary windows amd64 ".exe"

archive_tarball "dbpull-${VERSION}-linux-amd64" "dbpull-${VERSION}-linux-amd64.tar.gz"
archive_tarball "dbpull-${VERSION}-linux-arm64" "dbpull-${VERSION}-linux-arm64.tar.gz"
archive_tarball "dbpull-${VERSION}-darwin-amd64" "dbpull-${VERSION}-darwin-amd64.tar.gz"
archive_tarball "dbpull-${VERSION}-darwin-arm64" "dbpull-${VERSION}-darwin-arm64.tar.gz"
archive_zip "dbpull-${VERSION}-windows-amd64.exe" "dbpull-${VERSION}-windows-amd64.zip"

(
	cd "$DIST_DIR"
	checksum_file \
		"dbpull-${VERSION}-linux-amd64.tar.gz" \
		"dbpull-${VERSION}-linux-arm64.tar.gz" \
		"dbpull-${VERSION}-darwin-amd64.tar.gz" \
		"dbpull-${VERSION}-darwin-arm64.tar.gz" \
		"dbpull-${VERSION}-windows-amd64.zip"
) >"$DIST_DIR/checksums.txt"

echo
echo "Release build summary"
echo "Version : ${VERSION}"
echo "Commit  : ${COMMIT}"
echo "Built   : ${BUILD_DATE}"
echo "Dist    : ${DIST_DIR}"
