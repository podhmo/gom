#!/bin/sh
# Runs go-check-sumtype over every module in the repository, including
# the deliberate-failure canary (TODO B-3; docs/design.md section 2.4;
# docs/sketch/2026-11-02-sumtype-check-selection.md).
#
# The tool is invoked as `go run <pkg>@<version>` so it never enters
# go.mod. Both `make check-sumtype` and the CI workflow call this
# script, so this comment block and the pinned version below are the
# single place to update.

set -eu

GO=${GO:-go}
TOOL=github.com/alecthomas/go-check-sumtype/cmd/go-check-sumtype@v0.5.0

# With the default (-default-signifies-exhaustive=true) a `default`
# clause that does not panic would silence the check. Passing false
# keeps every switch verified; the conventional
# `default: panic("unreachable")` keeps the check active either way.
FLAGS="-default-signifies-exhaustive=false"

# Root module.
"$GO" run "$TOOL" $FLAGS ./...

# Nested modules under experiments/ — the same set check-experiments
# covers. The sealed interfaces annotated with //sumtype:decl live
# there until the internal AST lands (TODO P1-2).
if [ -d experiments ]; then
	for mod in $(find experiments -name go.mod | sort); do
		dir=$(dirname "$mod")
		echo "== $dir"
		"$GO" -C "$dir" run "$TOOL" $FLAGS ./...
	done
fi

# Canary: testdata/sumtype is a nested module with two fixtures.
# ./exhaustive must stay clean (positive control) and ./nonexhaustive
# MUST fail, proving that a missing variant really breaks the check.
canary=testdata/sumtype
"$GO" -C "$canary" run "$TOOL" $FLAGS ./exhaustive
if out=$("$GO" -C "$canary" run "$TOOL" $FLAGS ./nonexhaustive 2>&1); then
	echo "check-sumtype canary: $canary/nonexhaustive unexpectedly passed" >&2
	exit 1
fi
echo "$out"
if ! echo "$out" | grep -q "missing cases for Shape_Rect"; then
	echo "check-sumtype canary: failure did not report missing Shape_Rect" >&2
	exit 1
fi
echo "check-sumtype canary: missing-variant detection confirmed"
