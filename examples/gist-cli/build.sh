#!/bin/sh
# Compile the Gom package (every .gom in this directory) and then Go into
# the requested binary, without generating source files in this directory.
# Building never sends an HTTP request.
set -eu
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
GO=${GO:-go}
OUT=${1:-"$ROOT/gist-cli"}
case "$OUT" in /*) ;; *) OUT="$PWD/$OUT" ;; esac
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT HUP INT TERM
(cd "$ROOT" && "$GO" build -o "$TMP/gom" ./cmd/gom)
mkdir "$TMP/gen"
"$TMP/gom" build -o "$TMP/gen/" "$ROOT/examples/gist-cli" >/dev/null
(cd "$ROOT" && "$GO" build -o "$OUT" "$TMP"/gen/*.go)
printf 'Built %s\n' "$OUT"
