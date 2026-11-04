# `gom fix` で `.gomi` 適合検査を走らせる（見送り）

- 状態: 不採用（未決に戻した — 価値が具体化した時点で再検討）
- 日付: 2026-11-05

## Context

[gomi-conformance-in-gom-test](2026-11-04-gomi-conformance-in-gom-test.md) decided `gom test` runs the `.gomi` conformance pass (`checkInterfaces`) and explicitly deferred `gom fix`. This file evaluated the deferred part — and concluded it is not worth adopting yet.

## Evaluation

`gom fix` is a repair tool: its stream is `applied` + `summary` only, diagnostics are never shown, and `planFixes` applies only `safe` fixes. Running `checkInterfaces` there would add E0601/E0603/E0604 (and discovered-.gomi compile diagnostics) to the fix-candidate pool — but today **no interface diagnostic carries a `safe` fix** (the compiler's only safe fix is E0402's `ignore (...)`), so the change is observationally inert. Likely future fixes for mismatch are mostly `placeholder` (stub a missing member with `todo ()`) or `unsafe` (rewrite the declared contract), so the payoff may stay small even then. Costs are small but nonzero: a directory scan plus a compile of the discovered `.gomi` per run, and a footgun — wiring the pass to `rep` instead of the fix pool would make `gom fix`'s summary count errors and change its exit code.

Repair paths for reference: E0601 (missing `.gomi`) resolves via `gom info`'s draft; E0603 is resolved by `gom info` only in the "the implementation is right" direction — it regenerates the `.gomi` from the current impl, which can merely ratify drift when the contract was authoritative; E0604 needs a rename, `gom info` does not help.

## Decision

Do not run `checkInterfaces` in `gom fix` for now; the question returns to open-questions. Revisit when interface diagnostics gain `safe` fixes — that is the only channel through which the pass helps `gom fix`. If adopted later, the agreed shape stands: diagnostics feed `planFixes` only — not reported (the stream stays `applied` + `summary`), and a broken contract never gates application (a repair tool must run exactly when the contract is broken).
