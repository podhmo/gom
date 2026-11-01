# Verification report: sealed-interface exhaustiveness checker (B-3)

- 目的: pick and wire the tool that keeps internal-AST type switches
  exhaustive (TODO B-3; design.md section 2.4), prove it detects a
  missing variant, and record behaviors that differ from
  [reference-verification](2026-11-02-reference-verification.md).
- 確認日: 2026-11-02. Toolchain: go1.27.1.

## Tool chosen: `github.com/alecthomas/go-check-sumtype` v0.5.0

Builds and runs under go1.27.1 (`go run` fetched
`golang.org/x/tools v0.49.0`). It is a `go/analysis` singlechecker
binary accepting package patterns and the usual analyzer flags
(`-json`, `-fix`, `-V`, `-test`, ...). It can also run as
`go vet -vettool` or via golangci-lint `gochecksumtype`; we use the
standalone command.

Invocation, kept in one place in `scripts/check-sumtype.sh` so the
Makefile target and the CI step stay identical:

```sh
go run github.com/alecthomas/go-check-sumtype/cmd/go-check-sumtype@v0.5.0 \
    -default-signifies-exhaustive=false ./...
```

- `go run <pkg>@<version>` was chosen over a `tool` directive or a
  vendored binary so go.mod gains zero dependencies — the repo's
  minimal-deps preference. The version is pinned (`v0.5.0`); bump the
  variable at the top of the script to update.
- `-default-signifies-exhaustive=false`: verified that a `default`
  clause whose body returns a value (non-panicking) silently disables
  the check under the default flag value. With `false` it does not.
  The conventional `default: panic("unreachable")` keeps the check
  active either way, so this flag is free strictness.

## Corrections / refinements to the earlier sketch

- **`_test.go` files ARE checked by the fork.** Reference-verification
  recorded that upstream `BurntSushi/go-sumtype` skips `_test.go`
  (commit fcb4a62). At v0.5.0 the alecthomas fork analyzes them by
  default (`-test` flag defaults to true): a probe switch in
  `nonexhaustive/probe_test.go` was reported. Good for us — test-file
  switches are covered too.
- **Generics gap confirmed by experiment, and it is vacuous.** The
  sketch inferred "generic named types are excluded from the variant
  set" (commit 0697847). Verified: deleting the `Option_None[A]` case
  from `GetOrElse` in `experiments/p0/adt-repr/sealed` produces NO
  diagnostic — for a generic sealed interface the variant set is
  effectively empty and any switch "passes". Non-generic interfaces in
  the same package are still checked correctly (removing
  `Shape_Empty` from `Area` was reported). `//sumtype:decl` is kept on
  `Option[A]`/`List[A]` as documentation, with a comment noting the
  check is vacuous there; if a generic sealed interface ever needs
  real coverage, the ~150-line custom analyzer mentioned in
  reference-verification is the escape hatch.
- The `//sumtype:decl` annotation takes no type-name argument and sits
  on the GenDecl comment — for `type Shape interface{ isShape() }` it
  is simply the line above the decl.

## What got annotated

No sealed interfaces exist yet in `internal/` on this branch — the
P1-2a ASTs (`Expr`, `Pat`, `Decl`, `TypeExpr`, ...) live on
`task/p1-2a`, unmerged. When it lands, every one of its sealed
interfaces needs `//sumtype:decl` (suggested TODO entry in the B-3
report).

To keep the check meaningful today, the annotation was added to the
real sealed interfaces in the experiments modules, which
`check-sumtype` scans via the same `find experiments -name go.mod`
loop as `check-experiments`:

- `experiments/p0/adt-repr/sealed/shape.go`: `Shape` (non-generic —
  checked for real)
- `experiments/p0/adt-repr/sealed/expr.go`: `Expr` (non-generic —
  checked for real)
- `experiments/p0/adt-repr/sealed/generic.go`: `Option[A]`, `List[A]`
  (generic — annotation documents the sum type; check is vacuous)
- `experiments/p0/list-repr/consadt/list.go`: `List[A]` (generic —
  same caveat)

All type switches in those packages already pass, including defaults
written as `panic(rt.InvalidVariant(...))` and
`panic(fmt.Sprintf(...))` — any single-statement `panic(...)` body
keeps the check active, not only the literal `panic("unreachable")`.

## Canary design (proving a missing variant breaks the build)

A broken switch cannot be committed to real code, so the canary is a
dedicated nested module, `testdata/sumtype/` (`go.mod` inside, so the
root `./...` and `find experiments` never touch it; `go -C` still
works inside it):

- `exhaustive/` — sealed `Shape` + full switch + panicking default.
  Must stay clean; if it ever fails, the tool or the convention
  regressed.
- `nonexhaustive/` — same `Shape`, `Area` deliberately lacks
  `case Shape_Rect`. MUST keep failing. Do not fix it.

`scripts/check-sumtype.sh` asserts both halves: it runs the checker on
`./exhaustive` (expects exit 0), then on `./nonexhaustive` and fails
`make check` unless the run exits non-zero AND reports
`missing cases for Shape_Rect`. This is stronger than "runs the tool":
it fails loudly if a future tool release stops detecting the gap.

## Wiring

- `Makefile`: new `check-sumtype` target (`GO='$(GO)' sh
  scripts/check-sumtype.sh`), added to `check` between `test` and
  `check-experiments`.
- `.github/workflows/ci.yml`: one step `sh scripts/check-sumtype.sh`
  after the root `go test`, mirroring the Makefile.
- `testdata/README.md`: one line documenting `sumtype/`.

## Exhaustiveness gaps found in existing code

None on this branch — the only checked non-generic switches (`Shape`,
`Expr` in adt-repr) are complete.

To get ahead of the merge, P1-2a (`task/p1-2a` @ 3eb2036) was also
scanned: extracted to a scratch dir, `//sumtype:decl` injected on its
sealed interfaces (`types.Type`, `types.GoRef`, `syntax.Expr`, `Pat`,
`Decl`, `TypeExpr`, `StrPart`, `SelectArm`, `TypeBody`, `ExternEntry`,
`check.Expr`, `StrPart`, `SelectArm`, `Pat`, `Decl`, `TypeBody` — not
`syntax.Node`, which is a Span() superinterface, not a variant set),
then the tool was run over `./...`. Result:

- All NON-test switches are already exhaustive (`types` `atom`,
  `writeType`, `subst`, `equal`, and the `check`/`syntax` switches):
  zero diagnostics. The `default: panic("unreachable")` convention is
  followed everywhere.
- Two `_test.go` switches in `internal/syntax/expr_test.go` are
  flagged — `TestSelectArmsAndStrPartsAreSealed` uses a partial probe
  `switch arm.(type) { case *RecvArm: default: t.Fatalf }` on
  `SelectArm` and `StrPart`. They are intentional "does X implement
  the interface" assertions, not bugs — but because the fork checks
  `_test.go` files AND `-default-signifies-exhaustive=false` stops the
  `t.Fatalf` default from silencing, they WILL fail `make check` once
  P1-2a merges with annotations. There is no per-switch opt-out
  comment in v0.5.0. Fix when annotating: rewrite both probes as type
  assertions (`if _, ok := arm.(*RecvArm); !ok { ... }`) — assertions
  are not type switches, so they are outside the checker's scope.

## Costs / notes

- `go run <pkg>@<version>` resolves and builds the tool on first use
  (module cache), ~seconds in CI; `GOPROXY` network needed once per
  cache miss, same as any `go install`-style CI tool.
- The checker emits `exit status 3` through `go run`; the canary
  greps the diagnostic text, not the exit code, so the assertion is
  stable.
- gofmt still walks `testdata/sumtype` — keep the fixtures formatted.
