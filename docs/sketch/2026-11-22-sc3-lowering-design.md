# SC-3 native defer lowering — implementation design notes

Working notes for wave30 lane B (TODO SC-3). Target form:
experiments/defer/cgen/scenarios.go.

## Conditions for native lowering

`rt.ScopeRun(f)` / `result.ScopeRunResult(f)` (CallGo, f = Closure):
emit native Go defer iff EVERY `rt.ScopeDefer`/`ScopeDeferErr`/
`ScopeOnErr` call inside f's body sits in the SAME Go function —
i.e. none is nested inside an inner Closure boundary (a registration
inside a nested closure would run at that closure's call end, not
scope end). Otherwise keep SC-1's rt stack unchanged.

`s` (the Scope param) is never stored/returned (SC-2), so its only
uses are as arg0 of the scope ops — all rewritten away.

## Emission

- run body inline: the CallGo application is replaced by the body's
  Lets/Tail — i.e. as if `f()` were inlined (nested position wraps in
  IIFE so Go defers still run at scope end).
- `ScopeDefer(s, f)` → `defer f()`
- `ScopeDeferErr(s, f)` → needs result observation:
  `defer func() { if r := f(); isErr(r) && res.IsOk() { res = r } }()`
  Multiple defer_errs join via a shared `cerrs []error` var + a fold
  defer emitted at scope start (runs last):
  `defer func() { if len(cerrs)>0 && res.IsOk() { res = Err(errors.Join(cerrs...)) } }()`
- `ScopeOnErr(s, f)` → `defer func() { if <bodyFailed> { f() } }()`
  where bodyFailed = `!res.IsOk()` for run_result; for run it is
  panic-only — needs `recover()` re-panic pattern.
- Result-observing cleanups force a named result on the enclosing
  emitted Go func: `(res T)` — IIFE or FuncDecl.

## Open implementation questions

- Core IR node needed: a new `Defer` Stmt/Expr carrying a Closure,
  or keep `CallGo` + a marker — decide during impl; Body.Lets are
  ordered so a `Let{X: Defer}` can't model it — likely a new
  Body-position node or a Let whose X is a `Defer` marker emitting
  `defer f()` and binding nothing.
- Named-result threading: codegen Return-destination handling must
  write `res` when cleanups observe it — check how TailLoop's
  result buffer precedent works.
- `try` boundary: a `try` inside the run body escaping to Err must
  still close the scope — with native defer that's automatic (defer
  runs on any func exit path) — verify Try lowering doesn't bypass.
- Fallback granularity: whole run call stays rt-backed if any
  registration is nested inside an inner Closure.
