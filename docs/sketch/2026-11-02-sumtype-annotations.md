# Verification report: //sumtype:decl on the internal sealed interfaces (B-5)

- 目的: annotate every sealed interface that exists in `internal/`
  today so `scripts/check-sumtype.sh` (B-3) actually verifies the
  internal-AST type switches, and prove detection on real code.
- 確認日: 2026-11-02. Toolchain: go1.27.1.
- Follow-up of
  [sumtype-check-selection](2026-11-02-sumtype-check-selection.md),
  which predicted this work before P1-2a merged.

## Interfaces annotated

The annotation is a `//sumtype:decl` line at the end of the doc
comment directly above the type declaration — same form as the
experiments modules.

| File | Interface |
| --- | --- |
| `internal/types/type.go` | `Type` |
| `internal/syntax/expr.go` | `Expr`, `StrPart`, `SelectArm` |
| `internal/syntax/pat.go` | `Pat` |
| `internal/syntax/decl.go` | `Decl`, `TypeBody`, `ExternEntry` |
| `internal/syntax/typeexpr.go` | `TypeExpr` |
| `internal/check/typed.go` | `Expr`, `StrPart`, `SelectArm` |
| `internal/check/tpat.go` | `Pat` |
| `internal/check/tdecl.go` | `Decl`, `TypeBody` |

15 interfaces total. `internal/syntax/ast.go`'s `Node` is NOT
annotated: it is a `Span()` superinterface, not a variant set (no
marker method), exactly as the B-3 report noted. `internal/resolve`,
`internal/diag`, `internal/core` and the other stub packages declare
no interfaces yet. When P1-2b or later phases add sealed interfaces,
each needs the same line — this list is the diff to mirror.

## Test-probe rewrite

`internal/syntax/expr_test.go`'s `TestSelectArmsAndStrPartsAreSealed`
used two partial type switches (`case *RecvArm: default: t.Fatalf`)
as "does X implement the interface" probes. The fork checks
`_test.go` files and `-default-signifies-exhaustive=false` keeps a
`t.Fatalf` default from silencing, so both were rewritten to type
assertions (`if _, ok := arm.(*RecvArm); !ok`), which are outside the
checker's scope. Test intent is unchanged — the compile-time
interface assignment was already the real check.

## Results

- `GO=go1.27.1 sh scripts/check-sumtype.sh`: clean over `./...`, all
  experiments modules, and the canary still detects `Shape_Rect`.
  Zero diagnostics on the annotated internal packages — every
  existing switch (non-test and the remaining full-coverage test
  switches) is already exhaustive with `panic("unreachable")`.
- Detection spot-check on real code: temporarily deleting `case *Go:`
  from `types.Subst.Apply` produced
  `subst.go:14:2: exhaustiveness check failed for sum type "Type" ...
  missing cases for Go`. Restored afterwards; nothing committed.
- Exhaustiveness gaps found: none — no genuine bugs surfaced.
