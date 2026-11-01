# E0404 never fires for Gom calls: a missing label is a partial application

- 状態: 未決

## 状況

E0404 ("missing labeled argument", diag/codes.go) is specified against
spec section 2 with the example

```
transfer alice ~to: bob          // ~amount: is missing
```

In the P1 checker (`internal/check/apply.go`), a Gom call that leaves
labeled parameters unfilled is not an error at all: it is `CallPartial`
— partial application, which spec section 2 itself allows ("partial
application supplies some labels now and leaves the rest in the
resulting function type"). If the resulting function type then fails
to unify with the context, the diagnostic is E0401, not E0404.

E0404 therefore only ever fires for **Go calls**, where partial
application does not exist and every declared labeled parameter must
be supplied.

## なぜ問題か

- The code table's example suggests `transfer alice ~to: bob` is an
  E0404 site, but the implementation types it as a closure
  `~amount: Int -> Unit`. Two readings of the same sentence produce
  different diagnostics.
- A user who *meant* to saturate the call sees "type mismatch:
  expected Unit, got ~amount: Int -> Unit" (E0401) rather than the
  more direct "the callee requires `~amount:`" (E0404). The machinery
  to report E0404 exists but cannot distinguish "forgot a label" from
  "intended partial application" — the curried semantics makes them
  identical at the call site.
- If E0404 is meant to cover Gom calls too, the rule for when a
  partially applied Gom call is an error needs to be specified (for
  example: when the call result must be a non-function type and the
  remaining parameters are labeled — but that changes what the
  language means, since the function value may be genuinely wanted).

## 考えられる対応

- Accept the divergence: document that E0404 is Go-only and change the
  code-table example to a Go call. The Gom case stays a curried call —
  a language-semantics position, not just a diagnostic one.
- Emit a *note* on the E0401 that an unapplied labeled parameter
  exists when a partially applied call fails to unify with a
  non-function expected type — keeps the semantics, improves the
  message.
- Disallow unfilled labeled parameters when all positional parameters
  are filled and the context demands a non-function result — a small
  language change; would need a decision and spec wording.

## 関連

- internal/diag/codes.go E0401, E0404
- internal/check/apply.go `checkGomApply` (CallPartial) and
  `checkGoApply` (E0404 for unfilled labeled parameters)
- docs/spec.md section 2; decisions/2026-11-01-labeled-required-arguments.md
