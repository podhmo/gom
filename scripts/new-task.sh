#!/usr/bin/env bash
# new-task.sh — create a task worktree and branch.
#
# Usage (from anywhere in the repo):
#   scripts/new-task.sh <task-id>
#
# Creates <main-checkout>.worktrees/<task-id> on a new branch
# task/<task-id> forked from main — e.g. a repo checked out at
# ~/ghq/github.com/podhmo/gom produces
# ~/ghq/github.com/podhmo/gom.worktrees/<task-id>. The main checkout
# is located via `git worktree list`, so the script works no matter
# which worktree it is invoked from. Prints the worktree path as the
# last line of stdout.
#
# Pair with scripts/merge-task.sh to land the task back on main.

set -euo pipefail

die() { echo "new-task: error: $*" >&2; exit 1; }

[ $# -eq 1 ] || die "usage: $0 <task-id>"
id=$1
case "$id" in
	*/* | *..* | *" "*) die "task id must be a single path component: $id" ;;
esac

# The worktree holding branch `main` is the main checkout.
main_wt=""
wt_path=""
while IFS= read -r line; do
	case "$line" in
		"worktree "*) wt_path=${line#worktree } ;;
		"branch refs/heads/main") main_wt=$wt_path ;;
	esac
done < <(git worktree list --porcelain)
[ -n "$main_wt" ] || die "no worktree has branch main checked out"

if git -C "$main_wt" show-ref --verify --quiet "refs/heads/task/$id"; then
	die "branch task/$id already exists"
fi

wt="$(dirname "$main_wt")/$(basename "$main_wt").worktrees/$id"
[ ! -e "$wt" ] || die "path already exists: $wt"

mkdir -p "$(dirname "$wt")"
git -C "$main_wt" worktree add "$wt" -b "task/$id" main

echo "worktree ready:"
echo "$wt"
