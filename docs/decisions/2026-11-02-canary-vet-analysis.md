# Compile canary: stdlib-only vet-equivalent analysis on generated Go

- 状態: 採用
- 日付: 2026-11-02

## Background

The compile canary (`FuzzCompile` / `TestGeneratedCompileCanary`,
[fuzz-strategy](2026-11-02-fuzz-strategy.md)) judges generated Go by
parsing plus an in-process `go/types` check over the
`go list -export` importer. That catches malformed trees and type
errors but not the "compiles yet wrong" classes `go vet` exists for:
unreachable code after a fully-terminating branch, self-assignment,
a discarded must-use result, `string(int)` conversions, shadowed
declarations. The TODO asked for vet-equivalent coverage and named
three candidate mechanisms.

## Options

- **`golang.org/x/tools/go/analysis`** — the analyzers themselves,
  but it would be the module's first external dependency. Rejected:
  the stdlib-only convention stands, and a decision-sized dependency
  just for test oracles is not worth it.
- **Real `go vet` in a sandbox temp module** — vet needs a package
  on disk plus a buildable `go.mod` (the generated code imports
  `github.com/podhmo/gom/rt`), and a toolchain subprocess per
  input. The harness builds no temp module today (typecheck is
  in-process), and a subprocess per fuzz input is far too slow
  inside the rlimit'd helper child. Rejected.
- **Port the analyzers onto go/ast + go/types** — the pass logic is
  small (the vendored sources under `cmd/vendor` in GOROOT are
  self-contained), the typecheck pass already computes `types.Info`,
  and the whole thing runs in-process at ~µs per input. Chosen.

## Decision

`internal/fuzzutil.VetGenerated(fset, files, info)` runs five ported
checks over the generated files in `checkCompile`, after the
existing typecheck gate (or standalone, AST-only, when no toolchain
is present — `unreachable` and `shadow` need no types):

- **unreachable** — a direct port of cmd/vet's `deadState` walk
  (labels/gotos/breaks pre-pass, `panic` recognized via `types.Info`
  or `ast.Ident.Obj`). Highest-signal check for generated code:
  codegen emits `return`/`panic` in every branch, so any code after
  a terminating construct is an emission bug.
- **assign** (self-assignment) — `x = x` with vet's own side-effect
  and map-index guards (`NoEffects`/`CallsPureBuiltin` ported).
- **unusedresult** — only vet's whitelist of always-must-use calls
  (`fmt.Sprintf`-class, `errors.New`, `slices`/`maps`/`sort`
  helpers, `context.With*`) plus `Error`/`String` methods, because
  `ignore e` legitimately emits bare expression statements. A hit
  still marks a genuine wart: real `go vet` on the emitted module
  would flag it.
- **stringintconv** — `T(x)` where T's underlying is `string` and x
  is a non-byte/rune integer. `string(c)` for Char (→ `rune`) is
  legal; an `int64` landing there is the classic Go-targeting bug.
- **shadow** — stricter than vet's (non-default) shadow pass: the
  renamer uniquifies every generated name (`tmpN`, `vN`, `y1`), so
  ANY collision with an enclosing function-local declaration —
  parameters, `:=`, range vars, type-switch guards, function
  literals included — is a renamer defect. Same-scope
  redeclarations stay legal and unflagged.

Checks deliberately NOT ported: `printf` and `bools` would flag
faithful translations of user-level smells (a user's own format
string, `and x x`), not compiler defects; `structtag`, `tests`,
`asmdecl`, `cgocall` match constructs codegen never emits;
`copylock`, `shift` are already covered by go/types or by rt's
lock-free types.

Each finding becomes its own contract-violation string so a
knownBugs signature can pin one bug without masking a new finding
reported alongside it. Findings carry `//line`-resolved positions,
so they report the `.gom` line the emitted code claims to come
from.

## First results

The first sweep found a real defect: the TailLoop rebind emitted
`p = p` for self-calls passing parameters unchanged
(`for { cur, d1 = cur, d1; continue }`), which vet — and now the
canary — reports as self-assignment. `codegen.tailCall` now drops
self-pairs (all unchanged → bare `continue`; partial → only the
changing parameters). Golden: `testdata/golden/tailcall_unchanged_args`.

A second finding was pre-existing in the typecheck gate rather than
new: constant `10 / 0` emits an invalid Go constant division. It is
filed in TODO.md and registered in knownCompileBugs — whether check
rejects it or codegen forces a non-constant operand is a spec-level
call.
