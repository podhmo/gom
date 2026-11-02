#!/bin/sh
# Compile Gom source and then Go into the requested binary, without generating
# source files in this directory. Building never sends an HTTP request.
set -eu
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
GO=${GO:-go}
OUT=${1:-"$ROOT/gist-cli"}
case "$OUT" in /*) ;; *) OUT="$PWD/$OUT" ;; esac
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT HUP INT TERM
(cd "$ROOT" && "$GO" build -o "$TMP/gom" ./cmd/gom)
"$TMP/gom" build -o "$TMP/main.go" "$ROOT/examples/gist-cli/main.gom"
(cd "$ROOT" && "$GO" build -o "$OUT" "$TMP/main.go")
printf 'Built %s\n' "$OUT"
