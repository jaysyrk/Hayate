#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

PROJECT_NAME="hayate"
CMD_PATH="./cmd/hayate"
VERSION="$(go run ./cmd/hayate version | awk '{print $2}' | sed 's/^v//')"
VERSION="${VERSION:-dev}"
DIST_DIR="${ROOT_DIR}/dist"
RELEASE_DIR="${DIST_DIR}/${PROJECT_NAME}-v${VERSION}"
ARCHIVE_BASE="${DIST_DIR}/${PROJECT_NAME}-v${VERSION}"
LDFLAGS="-s -w"

require_cmd() {
	if ! command -v "$1" >/dev/null 2>&1; then
		printf 'error: required command not found: %s\n' "$1" >&2
		exit 1
	fi
}

sha256_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1"
	else
		shasum -a 256 "$1"
	fi
}

build_one() {
	local goos="$1"
	local goarch="$2"
	local suffix="$3"
	local out="${RELEASE_DIR}/${PROJECT_NAME}-${suffix}"

	if [[ "$goos" == "windows" ]]; then
		out="${out}.exe"
	fi

	printf 'building %s/%s -> %s\n' "$goos" "$goarch" "$(basename "$out")"
	CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
		-trimpath \
		-ldflags="$LDFLAGS" \
		-o "$out" \
		"$CMD_PATH"
}

copy_docs() {
	cp README.md LICENSE "$RELEASE_DIR/"
}

write_checksums() {
	(
		cd "$RELEASE_DIR"
		: > SHA256SUMS
		for file in "${PROJECT_NAME}"-* README.md LICENSE; do
			if [[ -f "$file" ]]; then
				sha256_file "$file" >> SHA256SUMS
			fi
		done
	)
}

create_archives() {
	(
		cd "$DIST_DIR"
		rm -f "${PROJECT_NAME}-v${VERSION}.tar.gz" "${PROJECT_NAME}-v${VERSION}.zip"
		tar -czf "${PROJECT_NAME}-v${VERSION}.tar.gz" "${PROJECT_NAME}-v${VERSION}"
		if command -v zip >/dev/null 2>&1; then
			zip -qr "${PROJECT_NAME}-v${VERSION}.zip" "${PROJECT_NAME}-v${VERSION}"
		fi
		sha256_file "${PROJECT_NAME}-v${VERSION}.tar.gz" > "${PROJECT_NAME}-v${VERSION}.tar.gz.sha256"
		if [[ -f "${PROJECT_NAME}-v${VERSION}.zip" ]]; then
			sha256_file "${PROJECT_NAME}-v${VERSION}.zip" > "${PROJECT_NAME}-v${VERSION}.zip.sha256"
		fi
	)
}

upload_over_ssh() {
	if [[ -z "${HAYATE_RELEASE_SSH_TARGET:-}" ]]; then
		return
	fi

	require_cmd scp
	printf 'uploading release artifacts to %s\n' "$HAYATE_RELEASE_SSH_TARGET"
	scp "${ARCHIVE_BASE}.tar.gz" "${ARCHIVE_BASE}.tar.gz.sha256" "$HAYATE_RELEASE_SSH_TARGET"
	if [[ -f "${ARCHIVE_BASE}.zip" ]]; then
		scp "${ARCHIVE_BASE}.zip" "${ARCHIVE_BASE}.zip.sha256" "$HAYATE_RELEASE_SSH_TARGET"
	fi
}

require_cmd go
require_cmd tar

printf 'running tests\n'
go test ./...

if [[ "${HAYATE_RELEASE_RACE:-0}" == "1" ]]; then
	printf 'running race tests\n'
	go test -race ./...
fi

rm -rf "$RELEASE_DIR"
mkdir -p "$RELEASE_DIR"

build_one darwin amd64 darwin-amd64
build_one darwin arm64 darwin-arm64
build_one linux amd64 linux-amd64
build_one linux arm64 linux-arm64
build_one linux arm64 termux-arm64
build_one windows amd64 windows-amd64
build_one windows arm64 windows-arm64

copy_docs
write_checksums
create_archives
upload_over_ssh

printf '\nrelease complete: %s\n' "$RELEASE_DIR"
printf 'archive: %s.tar.gz\n' "$ARCHIVE_BASE"
if [[ -f "${ARCHIVE_BASE}.zip" ]]; then
	printf 'archive: %s.zip\n' "$ARCHIVE_BASE"
fi
