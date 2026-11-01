# Fuzz strategy: parser oracle, compile canary, and the knownBugs registry

- 状態: 採用
- 日付: 2026-11-02

## Background

Review sessions kept surfacing two bug classes that fuzzing catches
cheaply: (a) the parser panicking or producing malformed
diagnostics on adversarial input, and (b) `gom build` emitting Go
that does not compile (`:=` shadowing, undeclared type parameters,
`==` on slices). Go's native `testing.F` gives us deterministic
seed coverage plus bounded mutation runs without new tooling. This
decision fixes what the fuzz targets assert, how already-filed
bugs are suppressed without hiding new ones, and how the runs are
wired into `make` and CI.

## Targets and oracles

- **`FuzzParse`** (`internal/syntax`): for any byte string, `Parse`
  and `ParseInterface` must not panic, must return a non-nil
  `*File`, and every diagnostic must pass
  `diag.Diagnostic.Validate` with spans inside the input.
- **`FuzzCompile`** (`internal/build`): `build.Compile` must not
  panic, return an internal error, or hang; every diagnostic must
  be well-formed and none may carry E0901 (a "core invariant
  violation" reaching the user is always a compiler bug); and
  generated Go — `Source` plus `TestSource` when present — must
  parse AND typecheck. Rejected input is fine: a diagnostic is a
  correct answer for bad programs.
- **Typecheck via `go/types`, not `go build`.** A full build costs
  ~1 s+ per emitted package; `go/types` over the same
  `go list -export` mechanism `internal/goimport` uses runs in
  milliseconds and still catches the target classes (undefined
  names, invalid composite types, non-comparable operands).
- **`import go` is allowlisted** (a dozen std packages plus this
  module). Without it, every mutated path would spawn a
  `go list` subprocess; E0203 in resolve is the cheap correct
  answer for arbitrary paths. No `go` toolchain on PATH degrades
  the canary to nil oracles + parse-only output checking rather
  than skipping.

## The knownBugs registry (internal/fuzzutil)

Bugs fuzzing finds are NOT fixed in the same change; they are
filed in TODO.md and registered so the run stays meaningful:

- Registry maps SHA-256 of a repro input to `{Match, TODO, Desc,
  Skip}`. `Match` is a signature — substrings a failure text must
  all contain — so mutation variants of the same bug suppress
  themselves instead of rediscovering it forever.
- `TODO` must be a substring of a live TODO.md entry;
  `CheckRegistry` fails the suite on orphans, and a run whose
  input now passes reports the entry as *stale* — forcing the
  registry, corpus file and TODO item to be removed together once
  a fix lands.
- `Skip` pins exact repros whose sandbox turn always burns the
  timeout for no new information (the ctor-pattern `-` loop —
  verified hangs in both `match` arms and `let` patterns).
- Registries are capped (16/24 entries): a list that keeps
  growing means findings outpace fixes, which should fail loudly.

## Hanging inputs: the persistent sandbox

The dominant hang (ctor pattern + stray `-` where `->`/`=` was
expected — `match r {A -`, `let A -`) spins forever emitting live
diagnostics at GB/s. An in-process watchdog can report it but
cannot kill the leaked goroutine, which then OOM-kills the whole
fuzz worker seconds later (observed: ~14.7 GB RSS, workers dying
mid-minimize leaving non-reproducing corpus files). A heuristic
that picks which inputs to sandbox missed the `let`-pattern
variant — proving any un-sandboxed input is a potential worker
kill. The design that fell out:

- **Every input runs in a persistent disposable child**
  (`fuzzutil.Sandbox`): one long-lived helper subprocess per
  worker, fed length-prefixed inputs; ~0.5 ms of IPC per input,
  no heuristics, no leaks — a hang costs one kill+respawn, a
  fatal crash becomes a captured failure instead of a dead
  worker. The child's caches (goimport loader, typecheck
  importer) persist across inputs, so memoization is free.
- **Phase markers** give the hang a name: `parse` and `compile`
  markers produce "parse did not return…" vs "compile did not
  return…", so a post-parse hang cannot be suppressed under the
  parser-hang signature.
- The child gets `ulimit -v` (via `sh` when present): a spinning
  child dies on allocation failure in ~1-2 s instead of growing
  gigabytes into uninterruptible D state, where even SIGKILL
  could not reap it (observed: 6.7 GB child stalling every
  worker). The reap is bounded for the same reason.

## Bounded runs

`make check-fuzz` runs each target for 25 s (`FUZZTIME`,
`FUZZPARALLEL` overridable) with `GOMEMLIMIT=2GiB`. It is
deliberately NOT part of `make check` — a new unregistered bug
fails the run, and filing it is the intended response — and CI
carries it as a `continue-on-error` advisory job for the same
reason: signal without a flaky gate.
