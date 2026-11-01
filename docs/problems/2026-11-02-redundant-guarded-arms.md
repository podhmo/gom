# Are unreachable *guarded* arms still errors?

docs/error-codes.md E0302 says "Guarded arms are exempt — a guard may
fail, so the arm can still be reached." That rationale only covers one
direction: the guarded arm stays reachable when the *earlier* arms are
guarded. It does not describe the other half — a guarded arm placed
*after* unguarded coverage:

```gom
match o {
  _ -> 0
  Some x if is_zero x -> x   // truly dead: `_` has no guard to fail
}
```

`internal/pattern` follows the blanket wording: any arm with a guard
is exempt from E0302, so the arm above is not reported even though it
can never run. OCaml flags the equivalent (`| Some x when g -> …`
after a catch-all) and rustc warns "unreachable pattern" there too —
the guard's own condition cannot rescue it.

Options:

- Keep the blanket exemption (current). Simple, matches the
  error-codes table literally, errs toward silence.
- Restrict the exemption: a guarded arm is unreachable when every
  *preceding* arm is unguarded and covers it. Matches the rationale's
  spirit and both other implementations, at the price of one more
  rule.

Related: spec section 4 only fixes the coverage side ("ガード付きの
腕は…その値を覆うとは見なさない"); the target side is inferred from
the error-codes table. Whichever way this resolves, the example in
error-codes.md should get a sentence for the trailing-guard case.
