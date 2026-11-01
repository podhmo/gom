#!/bin/sh
# run.sh — build and run every tutorial step, diffing each program's
# output against its expected.txt. Mirrors tutorial_test.go for people
# who want a plain shell driver.
#
# Environment:
#   GO   — the `go` command to use (default: `go` on PATH). Needs to
#          be >= 1.27 either directly or via GOTOOLCHAIN=auto; e.g.
#          `GO=~/go/bin/go1.27.1 tutorial/run.sh`.
#   GOM  — the `gom` binary to use (default: built into a temp dir
#          from ./cmd/gom with $GO).
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
GO=${GO:-go}
GOM=${GOM:-}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

if [ -z "$GOM" ]; then
  GOM="$tmp/gom"
  (cd "$ROOT" && "$GO" build -o "$GOM" ./cmd/gom)
fi

fail=0

for dir in "$ROOT"/tutorial/*/; do
  name=$(basename "$dir")

  # Run steps: main.gom + expected.txt
  if [ -f "$dir/main.gom" ]; then
    mod="$tmp/$name"
    mkdir -p "$mod"
    if ! "$GOM" build -o "$mod/" "$dir/main.gom" > "$mod.build.json" 2>&1; then
      echo "FAIL $name: gom build"
      cat "$mod.build.json"
      fail=1
      continue
    fi
    cat > "$mod/go.mod" <<EOF
module tutorialrun/$name

go 1.27

require github.com/podhmo/gom v0.0.0

replace github.com/podhmo/gom => $ROOT
EOF
    if ! got=$(cd "$mod" && GOFLAGS=-mod=mod "$GO" run . 2>&1); then
      echo "FAIL $name: go run"
      echo "$got"
      fail=1
      continue
    fi
    want=$(cat "$dir/expected.txt")
    if [ "$got" != "$want" ]; then
      echo "FAIL $name: output mismatch"
      echo "--- got ---"; printf '%s\n' "$got"
      echo "--- want ---"; printf '%s\n' "$want"
      fail=1
      continue
    fi
    echo "PASS $name"
    continue
  fi

  # Diagnostics steps: *.gom + expected-check.txt (a code like E0301
  # that `gom check` must report, exiting 1).
  for src in "$dir"*.gom; do
    [ -e "$src" ] || continue
    wantcode=$(cat "$dir/expected-check.txt")
    if "$GOM" check "$src" > "$tmp/$name.check.json" 2>&1; then
      echo "FAIL $name: gom check unexpectedly succeeded"
      fail=1
      continue
    fi
    if ! grep -q "\"code\":\"$wantcode\"" "$tmp/$name.check.json"; then
      echo "FAIL $name: expected diagnostic $wantcode"
      cat "$tmp/$name.check.json"
      fail=1
      continue
    fi
    echo "PASS $name (check: $wantcode)"
  done
done

exit $fail
