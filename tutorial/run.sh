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
    # A step may also carry expected-test.txt: run `gom test` on the
    # step and diff the JSONL report, normalizing `elapsed`.
    if [ -f "$dir/expected-test.txt" ]; then
      # gom test writes the generated main.go/main_test.go in place —
      # overwriting even a handwritten one. Snapshot whatever was
      # there and restore it after the run, so the tree (and `go test
      # ./...` package discovery) looks exactly as before.
      for f in main.go main_test.go; do
        if [ -f "$dir$f" ]; then cp "$dir$f" "$tmp/$name.keep.$f"; else rm -f "$tmp/$name.keep.$f"; fi
      done
      if ! (cd "$ROOT" && "$GOM" test "tutorial/$name") > "$tmp/$name.test.raw" 2>&1; then
        for f in main.go main_test.go; do
          if [ -f "$tmp/$name.keep.$f" ]; then cp "$tmp/$name.keep.$f" "$dir$f"; else rm -f "$dir$f"; fi
        done
        echo "FAIL $name: gom test"
        cat "$tmp/$name.test.raw"
        fail=1
        continue
      fi
      for f in main.go main_test.go; do
        if [ -f "$tmp/$name.keep.$f" ]; then cp "$tmp/$name.keep.$f" "$dir$f"; else rm -f "$dir$f"; fi
      done
      tgot=$(sed -E 's/"elapsed":[0-9][0-9.]*/"elapsed":0/g' "$tmp/$name.test.raw")
      twant=$(cat "$dir/expected-test.txt")
      if [ "$tgot" != "$twant" ]; then
        echo "FAIL $name: gom test output mismatch"
        echo "--- got ---"; printf '%s\n' "$tgot"
        echo "--- want ---"; printf '%s\n' "$twant"
        fail=1
        continue
      fi
      echo "PASS $name (test)"
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
