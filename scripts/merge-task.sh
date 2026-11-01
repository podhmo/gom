#!/usr/bin/env bash
# merge-task.sh — land a finished task branch on main and clean up.
#
# Usage (from anywhere in the repo):
#   scripts/merge-task.sh <task-id> [-m "<commit subject>"] [--dry-run]
#
# What it does:
#   1. Locates the main worktree (the checkout holding branch `main`)
#      and the task worktree (the one holding task/<task-id>) via
#      `git worktree list`.
#   2. Refuses to run if either side has uncommitted changes.
#   3. Rebases task/<task-id> onto main inside its worktree.
#      Known generated-file conflict: docs/decisions/README.md is
#      resolved by taking main's version and REGENERATING afterwards;
#      it is the only auto-resolved conflict. Anything else aborts the
#      rebase and exits with instructions.
#   4. Regenerates docs/decisions/README.md and commits the regen if
#      the worktree came out dirty.
#   5. Runs the gates in the worktree: `gofmt -l .` must be empty,
#      `go vet ./...`, `go test ./... -count=1`. Aborts on failure.
#   6. Back on main: `git merge --squash task/<id>` + `git commit`
#      (subject from -m, default "merge task/<id>", with the Devin
#      trailers), then `git worktree remove` + `git branch -D`.
#
# --dry-run stops after step 5: main is never touched and the worktree
# is kept. Use it to verify a task is mergeable before landing it.
#
# Toolchain: GO and GOFMT select the binaries (default `go` / `gofmt`
# on PATH). The repo needs Go >= 1.27, so e.g.
#   GO=~/go/bin/go1.27.1 GOFMT=~/sdk/go1.27.1/bin/gofmt scripts/merge-task.sh <id>
#
# Deliberately NOT automated: TODO.md checkboxes. After the merge,
# mark the task's `[ ]` items `[x]` by hand if they are done — the
# script prints a reminder at the end.

set -euo pipefail

GO=${GO:-go}
GOFMT=${GOFMT:-gofmt}
GENERATED_README=docs/decisions/README.md

die() { echo "merge-task: error: $*" >&2; exit 1; }

# --- args -----------------------------------------------------------------

id=""
subject=""
dry_run=0
while [ $# -gt 0 ]; do
	case "$1" in
		-m | --message)
			[ $# -ge 2 ] || die "-m needs an argument"
			subject=$2
			shift 2
			;;
		-n | --dry-run)
			dry_run=1
			shift
			;;
		-h | --help)
			sed -n '2,/^set -euo/p' "$0" | sed 's/^# \{0,1\}//; $d'
			exit 0
			;;
		-*)
			die "unknown flag: $1"
			;;
		*)
			[ -z "$id" ] || die "unexpected extra argument: $1"
			id=$1
			shift
			;;
	esac
done
[ -n "$id" ] || die "usage: $0 <task-id> [-m <subject>] [--dry-run]"
[ -n "$subject" ] || subject="merge task/$id"

# --- locate the two worktrees ----------------------------------------------

main_wt=""
task_wt=""
wt_path=""
while IFS= read -r line; do
	case "$line" in
		"worktree "*) wt_path=${line#worktree } ;;
		"branch refs/heads/main") main_wt=$wt_path ;;
		"branch refs/heads/task/$id") task_wt=$wt_path ;;
	esac
done < <(git worktree list --porcelain)

[ -n "$main_wt" ] || die "no worktree has branch main checked out"
[ -n "$task_wt" ] || die "no worktree holds task/$id (create one with scripts/new-task.sh $id)"

# --- safety: both sides clean ----------------------------------------------

if [ -n "$(git -C "$task_wt" status --porcelain)" ]; then
	die "uncommitted changes in task worktree $task_wt — commit or stash first:
$(git -C "$task_wt" status --short)"
fi
if [ -n "$(git -C "$main_wt" status --porcelain)" ]; then
	die "uncommitted changes in main checkout $main_wt — commit or stash first:
$(git -C "$main_wt" status --short)"
fi

# --- step 3: rebase task/<id> onto main in its worktree ---------------------

in_rebase() {
	local wt=$1 kind dir
	for kind in rebase-merge rebase-apply; do
		dir=$(git -C "$wt" rev-parse --git-path "$kind" 2>/dev/null || true)
		[ -n "$dir" ] && [ -d "$dir" ] && return 0
	done
	return 1
}

echo "== rebase task/$id onto main (in $task_wt)"
if ! git -C "$task_wt" rebase main; then
	while in_rebase "$task_wt"; do
		conflicts=$(git -C "$task_wt" diff --name-only --diff-filter=U | sort)
		if [ "$conflicts" = "$GENERATED_README" ]; then
			echo "-- taking main's $GENERATED_README (regenerated below)"
			git -C "$task_wt" checkout main -- "$GENERATED_README"
			GIT_EDITOR=true git -C "$task_wt" rebase --continue || true
		else
			git -C "$task_wt" rebase --abort || true
			if [ -n "$conflicts" ]; then
				die "rebase hit conflicts this script does not auto-resolve (only $GENERATED_README is handled):
$conflicts

Resolve by hand:
  cd $task_wt && git rebase main
  # fix conflicts, git add, git rebase --continue
then rerun this script."
			else
				die "rebase paused with no conflicts (likely an empty commit).

Finish by hand:
  cd $task_wt && git rebase --skip   # or --continue
then rerun this script."
			fi
		fi
	done
fi

# Hard guarantee the rebase actually landed: main must be an ancestor
# of the task branch now, no matter how the loop above exited.
if ! git -C "$task_wt" merge-base --is-ancestor main HEAD; then
	die "rebase did not leave task/$id on top of main — finish it by hand in $task_wt and rerun"
fi

# --- step 4: regenerate the decisions README, commit if dirty ---------------

# The README is generated from docs/decisions/*.md by this test; a
# rebase can leave it stale even without a textual conflict, so the
# regen runs unconditionally — a no-op when already in sync.
echo "== regenerate $GENERATED_README"
( cd "$task_wt" && "$GO" test ./internal/tool/decisionsdoc -run TestDecisionsReadmeInSync -update )
if [ -n "$(git -C "$task_wt" status --porcelain)" ]; then
	echo "-- committing regenerated files"
	git -C "$task_wt" add -A
	git -C "$task_wt" commit -m "chore: regenerate $GENERATED_README" --quiet
fi

# --- step 5: gates in the worktree ------------------------------------------

echo "== gofmt -l . (must be empty)"
unformatted=$(cd "$task_wt" && "$GOFMT" -l .) || die "gofmt failed to run in $task_wt"
[ -z "$unformatted" ] || die "files need gofmt in $task_wt:
$unformatted"

echo "== go vet ./..."
( cd "$task_wt" && "$GO" vet ./... ) || die "go vet failed in $task_wt"

echo "== go test ./... -count=1"
( cd "$task_wt" && "$GO" test ./... -count=1 ) || die "go test failed in $task_wt"

# --- step 6: squash-merge on main, then clean up ----------------------------

if [ "$dry_run" -eq 1 ]; then
	echo "== dry run: gates passed; would now run on $main_wt:"
	echo "   git merge --squash task/$id"
	echo "   git commit  (subject: $subject)"
	echo "   git worktree remove $task_wt && git branch -D task/$id"
	exit 0
fi

echo "== git merge --squash task/$id (on $main_wt)"
if ! git -C "$main_wt" merge --squash "task/$id"; then
	git -C "$main_wt" reset --hard HEAD
	die "merge --squash conflicted (unexpected right after a clean rebase) — main was reset to HEAD, nothing committed"
fi
git -C "$main_wt" add -A
if git -C "$main_wt" diff --cached --quiet; then
	die "task/$id adds no changes vs main — nothing to commit (worktree kept: $task_wt)"
fi
git -C "$main_wt" commit -F - <<EOF
$subject

Generated with [Devin](https://devin.ai)
Co-Authored-By: Devin <158243242+devin-ai-integration[bot]@users.noreply.github.com>
EOF

echo "== remove worktree and branch"
git -C "$main_wt" worktree remove "$task_wt"
git -C "$main_wt" branch -D "task/$id"

cat <<EOF

Done: task/$id squash-merged into main, worktree removed.

Reminder: TODO.md was not touched — mark the task's checkboxes [x]
by hand if they are done.
EOF
