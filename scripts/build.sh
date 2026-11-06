#!/bin/sh
# Build both binaries into bin/. Needs only the Go toolchain; no network.
set -eu
cd "$(dirname "$0")/.."
go build -o bin/ninelives ./src/cmd/ninelives
go build -o bin/torture ./torture
echo "built bin/ninelives and bin/torture"
