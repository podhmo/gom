#!/bin/sh
# Runs staticcheck over every module in the repository: the root
# module plus the nested modules `./...` never reaches (experiments/,
# testdata/sumtype, tutorial/). Modules with no Go packages (the
# tutorial fixtures hold only .gom sources) warn and pass.
#
# The tool is invoked as `go run <pkg>@<version>` so it never enters
# go.mod. Both `make check-staticcheck` and the CI workflow call this
# script, so this comment block and the pinned version below are the
# single place to update. staticcheck releases track Go releases:
# bump TOOL when it starts complaining about a newer language version.
#
# Baseline: clean as of 2026-11-01 (v0.8.2, the 2026.2.2 release).
# A finding is a real gate failure — fix the code, or for a deliberate
# construct add `//lint:ignore <check> <reason>` at the site.

set -eu

GO=${GO:-go}
TOOL=honnef.co/go/tools/cmd/staticcheck@v0.8.2

for mod in $(find . -name go.mod | sort); do
	dir=$(dirname "$mod")
	if [ "$dir" = "." ]; then
		"$GO" run "$TOOL" ./...
	else
		echo "== $dir"
		"$GO" -C "$dir" run "$TOOL" ./...
	fi
done
