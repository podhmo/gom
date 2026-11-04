# Fuzz failures file `fuzz-finding` issues automatically

- 状態: 採用
- 日付: 2026-11-04

## Background

The bounded fuzz job (`make check-fuzz`, advisory in CI) is
designed so a failure means "a new bug class to file" — but the
filing step was manual, and the advisory is easy to skip when the
gate job is what people watch. Two real findings sat in run logs
before anyone looked: an E0901 in `|?>`-adjacent partial
application (#11, run 41797923399) and a `gom fmt` non-idempotent
emission (#12, run 41852501359). The strategy in
[2026-11-02-fuzz-strategy](2026-11-02-fuzz-strategy.md) already
declares findings should not be lost; this decision automates the
"file" half so the signal reaches a tracked place on its own.

## Decision

A failed `fuzz (advisory)` run files **one GitHub issue per new
failure signature**, labeled `fuzz-finding`. The failure step
parses the `make check-fuzz` output (`scripts/file-fuzz-issues.sh`)
into records — one per nested `--- FAIL: Fuzz<Name>` block — and
derives a signature from the first failure line:

- an `E####` diagnostic code plus the message head up to the next
  `:` (input-dependent identifiers such as `~amount` are cut, so
  mutation variants of the same bug do not mint new issues), or
- the message head up to the first `:` (capped at 60 chars) when
  no code is present.

The signature is substring-matched against the titles of open
`fuzz-finding` issues. A match adds a recurrence comment with the
run URL instead of a duplicate issue; no match creates an issue
whose body carries the failure block, the `%q`-quoted input, the
corpus path, a `go test` reproducer line, and a checklist pointing
at the existing filing flow (corpus file → `knownBugs` entry →
TODO.md item → registry cleanup on fix).

## Constraints and scope

- **main pushes only.** `pull_request` tokens are read-only for
  forks, and a single filing channel keeps dedup trivial. The
  step is `continue-on-error` itself — a filing failure must not
  turn the advisory red.
- **Permissions:** the fuzz job gets `issues: write` (contents
  stay read); the gate job is unchanged.
- **This automates filing, not fixing.** The knownBugs registry
  stays the suppression mechanism: an issue is filed once per
  signature, then the bug is registered in TODO.md + knownBugs
  like any manual finding. Nothing here auto-registers a finding.
- Label `fuzz-finding` is the dedup key; the script lists open
  issues under it rather than searching globally.
