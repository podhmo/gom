# Over-application may fill labeled parameters positionally

- 状態: 未決

## 状況

`checkGomApply` collects positional arguments that outlive the
callee's declared parameters as `extras` and hands them to
`overApply` (internal/check/apply.go), which unifies them with the
returned function's parameters **in order, labels ignored**:

```go
if i < len(rparams) {
    c.unify(rparams[i].Type, extras[i].Type(), extras[i].Span())
}
```

So `fn mk () -> (~x: Int -> Int)` called as `mk () 5` accepts the `5`
for `~x` — while spec section 2 rule 3 says a positional argument may
never fill a labeled parameter, and the main argument loop enforces
exactly that (positional args skip labeled slots; a Go call reports
E0404/E0406-style errors).

## なぜ問題か

**追記 (P2-3, 2026-11-05)**: the mechanism described above no longer
reproduces. `checkGomApply` now hands `overApply` the *final*
non-function return type (`flattenFunc` flattens the whole arrow
chain, so `ret` is never a `Func`), which makes `rparams` always
empty — the `i < len(rparams)` branch above is dead code. Every
positional extra therefore takes the meta-extension path and fails
against a saturated result: `mk () 5` is E0401 ("type mismatch:
expected Int, got ?3 -> ?5"). The *outcome* now matches spec 2 rule 3
(a positional argument never fills a labeled parameter — pinned by
testdata/spec/E0401_positional_for_labeled.gom), but the diagnostic
is an indirect mismatch, and the dead `rparams` branch still
documents the old "labels ignored" semantics. The open questions are
now: is E0401's wording acceptable for this error, and should the
unreachable branch be removed?

- The same source shape is legal or illegal depending on which side
  of the declared arity the argument lands — inconsistent.
- If it is an error, which code? "positional argument cannot fill a
  labeled parameter" is closest to E0404's domain (Go calls only
  today) or a plain E0401 mismatch.
- If it is legal partial application, the labeled parameter should
  arguably stay in the result type instead of being consumed, which
  changes the result the call reports.

## 考えられる対応

- Decide the spec rule for positional extras against labeled
  parameters of a *returned* function (error vs. left-over partial),
  then align `overApply` with the main loop's labeled-slot skipping.

## 関連

- docs/decisions/2026-11-01-labeled-required-arguments.md
- spec.md section 2 — labeled arguments
