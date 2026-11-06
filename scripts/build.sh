#!/bin/sh
# Build both binaries into bin/. Needs only the Go toolchain; go.mod has no
# dependencies, so no network access is used.
set -eu
cd "$(dirname "$0")/.."

command -v go >/dev/null 2>&1 || {
	echo "build.sh: Go toolchain not found on PATH; install Go and re-run" >&2
	exit 1
}

mkdir -p bin
go build -o bin/ninelives ./src/cmd/ninelives
go build -o bin/torture ./torture

test -x bin/ninelives || { echo "build.sh: bin/ninelives was not built" >&2; exit 1; }
test -x bin/torture   || { echo "build.sh: bin/torture was not built"   >&2; exit 1; }
echo "built bin/ninelives and bin/torture"
