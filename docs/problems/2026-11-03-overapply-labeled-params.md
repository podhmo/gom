# Over-application may fill labeled parameters positionally

- 状態: 一部解消（2026-11-05 — the positional-fills-labeled path is
  removed; the E0401 wording question stays open）

## 状況

`checkGomApply` collects positional arguments that outlive the
callee's declared parameters as `extras` and hands them to
`overApply` (internal/check/apply.go), which used to unify them with
the returned function's parameters **in order, labels ignored**:

```go
if i < len(rparams) {
    c.unify(rparams[i].Type, extras[i].Type(), extras[i].Span())
}
```

So `fn mk () -> (~x: Int -> Int)` called as `mk () 5` accepted the `5`
for `~x` — while spec section 2 rule 3 says a positional argument may
never fill a labeled parameter, and the main argument loop enforces
exactly that (positional args skip labeled slots; a Go call reports
E0404/E0406-style errors).

## なぜ問題か

**追記 (P2-3, 2026-11-05)**: the mechanism described above no longer
reproduces *for that source shape*. `checkGomApply` hands `overApply`
the *final* non-function return type (`flattenFunc` flattens the whole
arrow chain), so a concrete `ret` gives `rparams` empty and extras take
the meta-extension path: `mk () 5` is E0401 ("type mismatch:
expected Int, got ?3 -> ?5"). The *outcome* matches spec 2 rule 3
(pinned by testdata/spec/E0401_positional_for_labeled.gom).

**訂正 (2026-11-05, same day)**: the branch was NOT dead code.
`flattenFunc` only guarantees `ret` is not a `Func` *at that moment* —
it can be an unbound meta that later arguments bind to a function type.
`id mk 3` (`id : a -> a`) bound `?a := ~x: Int -> Int` during argument
checking, and `overApply` then reached the `rparams` branch and let `3`
fill `~x` positionally — the spec violation survived through metas
(verified with `gom check`: `id mk 3` compiled while `mk 3` was E0401).

**解消 (2026-11-05)**: `overApply` no longer peels parameters off the
returned type at all. Every extra extends the result with a fresh
curried step, unified against the return type as it stands — a labeled
parameter can therefore never face a positional argument
(`unifyFunc`'s labeled-run rule reports the mismatch). `id mk 3` is
E0401 now; legitimate over-application (`id id 3`, partial fills like
`id add3 1 2`) still types correctly. Fixture:
testdata/spec/E0401_positional_for_labeled_via_meta.gom.

Still open:

- Is E0401's wording acceptable for this error? "expected
  `~x: Int -> Int`, got `Int -> ?n`" is informative but indirect —
  a dedicated "positional argument cannot fill a labeled parameter"
  code (E0404's domain, currently Go-call only) may read better.

## 考えられる対応

- ~~Decide the spec rule for positional extras against labeled
  parameters of a *returned* function (error vs. left-over partial),
  then align `overApply` with the main loop's labeled-slot skipping.~~
  Done: the meta-extension path IS the labeled-slot skipping — unifying
  `extra -> next` against the return type goes through `unifyFunc`,
  which never pairs a labeled parameter with a positional one.

## 関連

- docs/decisions/2026-11-01-labeled-required-arguments.md
- spec.md section 2 — labeled arguments
