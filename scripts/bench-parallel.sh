#!/bin/sh
# bench-parallel.sh — wave22 lane1: measure a multi-dir
# `gom check`/`gom build` invocation (compileInputs' layered
# schedule) against two baselines —
#   serial:      one gom invocation per dir, sequentially
#   GOMAXPROCS:  intra-invocation parallelism off / 4 / default
# GOM_BUILD_NOCACHE keeps every rep a full compile.
#
# Fixture: one module with two shapes —
#   indep{N}/   N dirs with no Gom imports between them
#   chain{N}/   N dirs, each importing the previous one
# so intra-layer parallelism and cross-layer serialization are
# both exercised. Dep dirs get .gomi drafts via `gom info`
# first (importers read interfaces from disk; `gom info` does
# not recurse, so it runs per dir in dep order).
#
# Usage: bench-parallel.sh [GOM] [N] [REPS]
set -eu

GOM=${1:-gom}
N=${2:-8}
REPS=${3:-5}

# A relative path like `./gom` dies on the first `cd` into the
# fixture — make it absolute. A bare name stays as-is so PATH
# lookup keeps working inside the subshells.
case $GOM in
*/*) GOM=$(cd "$(dirname "$GOM")" && pwd)/$(basename "$GOM") ;;
esac
ROOT=$(mktemp -d)
trap 'rm -rf "$ROOT"' EXIT

mod="bench.local/m"
mkdir -p "$ROOT/m"
printf 'module %s\n\ngo 1.24\n' "$mod" >"$ROOT/m/go.mod"

gen_src() { # dir name
	printf 'package %s\n\nimport go "strings"\n\n' "$1"
	i=1
	while [ "$i" -le 30 ]; do
		printf 'fn f%s_%s (x: Int) -> Int = x + %s * %s\n' "$1" "$i" "$i" "$i"
		printf 'fn s%s_%s (s: String) -> Int = strings.Count s "x" + %s\n' "$1" "$i" "$i"
		i=$((i + 1))
	done
	printf 'fn acc_%s (xs: List Int) -> Int =\n' "$1"
	printf '  List.fold (fn a b -> a + b + %s) 0 xs\n\n' "$(echo "$1" | wc -c)"
}

# Independent dirs — no Gom imports.
for i in $(seq 1 "$N"); do
	d="indep$i"
	mkdir -p "$ROOT/m/$d"
	gen_src "$d" >"$ROOT/m/$d/$d.gom"
done

# Chain dirs — chain{i} imports chain{i-1}.
mkdir -p "$ROOT/m/chain1"
gen_src chain1 >"$ROOT/m/chain1/chain1.gom"
for i in $(seq 2 "$N"); do
	d="chain$i"
	p="chain$((i - 1))"
	mkdir -p "$ROOT/m/$d"
	{
		printf 'package %s\n\n' "$d"
		printf 'import "%s/%s"\n\n' "$mod" "$p"
		gen_src "$d" | tail -n +3
		printf 'fn use_%s (x: Int) -> Int = %s.f%s_8 x + 1\n' "$d" "$p" "$p"
	} >"$ROOT/m/$d/$d.gom"
done

# Dir list in creation/dep order — `ls` alphabetical would put
# chain10 before chain2 and break `gom info`'s dep sequencing.
CHAIN=$(seq -f 'chain%g' 1 "$N" | tr '\n' ' ')
INDEP=$(seq -f 'indep%g' 1 "$N" | tr '\n' ' ')
ALL="$CHAIN $INDEP"

# .gomi drafts per dir — compile requires the package's own
# interface file (E0601) and importers read these from disk, so
# `gom info` runs per dir in dep order.
for d in $ALL; do
	(cd "$ROOT/m" && "$GOM" info "$d" >/dev/null 2>&1) ||
		echo "note: gom info $d failed (first-run E0203 is expected for missing dep .gomi)" >&2
done

timed() { # env... -- runs the command timed, returns ms on stdout
	start=$(date +%s%N)
	# Check the status explicitly: inside $( ...) errexit is off
	# under bash, so a failed compile would still print a sample.
	if ! (cd "$ROOT/m" && "$@" >/dev/null 2>&1); then
		echo "bench: failed: $*" >&2
		exit 1
	fi
	end=$(date +%s%N)
	echo $(( (end - start) / 1000000 ))
}

run() { # label cmd env...
	label=$1; cmd=$2; shift 2
	total=0
	for _ in $(seq 1 "$REPS"); do
		total=$((total + $(timed "$@" "$cmd" $ALL)))
	done
	printf '%-24s %8d ms  (avg %d ms over %s runs)\n' \
		"$label" "$total" "$((total / REPS))" "$REPS"
}

run_serial() { # label cmd — one gom invocation per dir, sequential
	label=$1; cmd=$2
	total=0
	for _ in $(seq 1 "$REPS"); do
		start=$(date +%s%N)
		for d in $ALL; do
			(cd "$ROOT/m" && GOM_BUILD_NOCACHE=1 "$GOM" "$cmd" "$d" >/dev/null 2>&1)
		done
		end=$(date +%s%N)
		total=$((total + (end - start) / 1000000))
	done
	printf '%-24s %8d ms  (avg %d ms over %s runs)\n' \
		"$label" "$total" "$((total / REPS))" "$REPS"
}

echo "fixture: $N independent dirs + $N-dir dep chain under $ROOT/m"
# GOM_BUILD_NOCACHE keeps every rep a full compile — otherwise the
# incremental cache turns later `build` reps into no-ops.
for cmd in check build; do
	echo "-- gom $cmd"
	run_serial "serial (per-dir)" "$cmd"
	run "multi-dir GOMAXPROCS=1" "$cmd" env GOMAXPROCS=1 GOM_BUILD_NOCACHE=1 "$GOM"
	run "multi-dir GOMAXPROCS=4" "$cmd" env GOMAXPROCS=4 GOM_BUILD_NOCACHE=1 "$GOM"
	run "multi-dir GOMAXPROCS=default" "$cmd" env GOM_BUILD_NOCACHE=1 "$GOM"
done
