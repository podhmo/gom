#!/bin/sh
# file-fuzz-issues.sh — parse `make check-fuzz` output and file one
# GitHub issue per NEW failure signature under the fuzz-finding
# label; a signature already filed gets a recurrence comment with
# the run URL instead of a duplicate issue (see the fuzz job in
# .github/workflows/ci.yml).
#
# Usage: sh scripts/file-fuzz-issues.sh <check-fuzz.log>
#
# Env:
#   GH_TOKEN    token for `gh` (CI passes secrets.GITHUB_TOKEN)
#   RUN_URL     actions run URL recorded in the issue
#   LABEL       issue label to file under (default: fuzz-finding)
#   DRY_RUN=1   print what would be filed; no gh calls
#
# A fuzz failure block looks like:
#
#   --- FAIL: FuzzFormat (12.52s)
#       --- FAIL: FuzzFormat (0.00s)
#           fuzz_test.go:170: Source is not idempotent
#               <details...>
#               input: "package a\nfn a()->A={0(().A)}"
#       Failing input written to testdata/fuzz/FuzzFormat/<id>
#       To re-run:
#           go test -run=FuzzFormat/<id>
#
# The nested (indented) --- FAIL line opens a record; the
# `go test -run=` line closes it. The signature normalizes the
# first failure line: an E#### diagnostic code plus the message
# head up to the next ':', else the head of the message — so
# input-dependent identifiers (~amount etc.) do not mint new
# issues for the same bug.

set -eu

LOG=${1:?usage: file-fuzz-issues.sh <check-fuzz.log>}
LABEL=${LABEL:-fuzz-finding}
RUN_URL=${RUN_URL:-unknown run}
DRY_RUN=${DRY_RUN:-0}

OUT=$(mktemp -d)
trap 'rm -rf "$OUT"' EXIT

awk -v outdir="$OUT" '
  /^    --- FAIL: Fuzz/ {
    match($0, /Fuzz[A-Za-z0-9_]*/)
    n++
    file = outdir "/" n ".rec"
    print "TARGET " substr($0, RSTART, RLENGTH) > file
    open = 1
    next
  }
  open {
    print $0 >> file
    if ($0 ~ /go test -run=/) open = 0
    next
  }
  /^FAIL/ || /^ok  / { open = 0 }
' "$LOG"

n=$(find "$OUT" -name '*.rec' | wc -l)
if [ "$n" -eq 0 ]; then
	echo "no fuzz failures found in $LOG"
	exit 0
fi

existing=""
if [ "$DRY_RUN" != 1 ]; then
	existing=$(gh issue list --label "$LABEL" --state open --limit 200 \
		--json number,title --jq '.[] | "\(.number)\t\(.title)"')
fi

for f in "$OUT"/*.rec; do
	target=$(sed -n 's/^TARGET //p' "$f" | head -1)
	msg=$(sed -n 's/^ *[^ ]*_test\.go:[0-9][0-9]*: //p' "$f" | head -1)
	[ -n "$msg" ] || msg="(no failure line captured)"
	input=$(sed -n 's/^ *input: //p' "$f" | head -1)
	[ -n "$input" ] || input="(none — hang or crash before reporting)"
	corpus=$(sed -n 's/^ *Failing input written to //p' "$f" | head -1)
	detail=$(awk '/_test\.go:[0-9]+: /,/input: /' "$f")
	[ -n "$detail" ] || detail=$(sed '1d' "$f")

	code=$(printf '%s' "$msg" | grep -o 'E[0-9]\{4\}' | head -1 || true)
	if [ -n "$code" ]; then
		head=$(printf '%s' "$msg" | sed "s/.*$code: \([^:]*\).*/\1/")
		title="[fuzz] $target $code: $head"
		sig="$target $code: $head"
	else
		# Normalize the variable tails fuzzutil puts on hangs and
		# crashes ("did not return within 8s for 512-byte input",
		# "crashed in <phase> phase for N-byte input") so the same
		# class maps to one issue regardless of input size.
		head=$(printf '%s' "$msg" | sed \
			-e 's/did not return within .*/did not return/' \
			-e 's/ phase for [0-9][0-9]*-byte input.*/ phase/' \
			| cut -d: -f1 | cut -c1-60)
		title="[fuzz] $target: $head"
		sig="$target: $head"
	fi

	# The corpus path the fuzzer prints is package-relative; the
	# owning package comes from the `go test ./<pkg> ... -fuzz='^T$'`
	# line that launched the target.
	pkg=$(grep -- "-fuzz='\^$target\$'" "$LOG" | grep -o 'go test \./[^ ]*' \
		| sed 's|go test ./||' | head -1)
	[ -n "$pkg" ] || pkg="?"
	rerunid=$(sed -n 's/^ *go test -run=\(\S*\).*/\1/p' "$f" | head -1)
	if [ -n "$rerunid" ]; then
		rerun="go test ./$pkg -run=$rerunid"
	else
		rerun="(fuzzer left no reproducer — see the run log)"
	fi
	if [ -n "$corpus" ]; then
		fullcorpus="$pkg/$corpus"
	else
		fullcorpus="(none)"
	fi

	# The knownBugs registry is a per-package map named after the
	# fuzz target family, not the package: FuzzParse→knownParseBugs,
	# FuzzCompile→knownCompileBugs, FuzzFormat*→knownFormatBugs.
	case $pkg in
	internal/build) reg=knownCompileBugs ;;
	internal/format) reg=knownFormatBugs ;;
	internal/syntax) reg=knownParseBugs ;;
	*) reg='known*Bugs' ;;
	esac

	num=""
	if [ -n "$existing" ]; then
		num=$(printf '%s\n' "$existing" | grep -F -- "$sig" | cut -f1 | head -1)
	fi

	if [ -n "$num" ]; then
		echo "== signature already filed as issue #$num: $title"
		body="再発を検出しました: $RUN_URL"
		if [ "$DRY_RUN" = 1 ]; then
			echo "[dry-run] would comment on #$num"
		else
			printf '%s' "$body" | gh issue comment "$num" --body-file -
		fi
		continue
	fi

	body="CI の fuzz advisory run が新しい失敗を検出しました（自動起票）。

- run: $RUN_URL
- job: \`fuzz (advisory)\` → \`make check-fuzz\`
- target: \`$target\` ($pkg)
- corpus: \`$fullcorpus\`

## 失敗出力

\`\`\`
$detail
\`\`\`

## input (Go %q)

\`\`\`
$input
\`\`\`

## 再現

\`\`\`
$rerun
\`\`\`

## 記録チェックリスト

- [ ] corpus ファイルをコミット
- [ ] \`$reg\` レジストリにエントリ追加（Match + TODO リンク）
- [ ] TODO.md に修正タスクを追加
- [ ] 上記3件が済んだらこの issue を close（以降の正は knownBugs/TODO.md）
- [ ] 修正後に knownBugs エントリを除去し、corpus を regression seed として残す"

	echo "== new signature: $title"
	if [ "$DRY_RUN" = 1 ]; then
		echo "[dry-run] would create issue:"
		printf '%s\n' "$body"
	else
		printf '%s' "$body" | gh issue create --title "$title" --label "$LABEL" --body-file -
	fi
done
