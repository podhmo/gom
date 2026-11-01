# Untyped Go constants take the default type, not the expected type

- 状態: 未解決
- 日付: 2026-11-02

## Situation

Spec 8.1.5 promises that Go's untyped constants (`math.Pi` and
friends) take the *expected* Gom type — the same rule Go applies
where an untyped constant adapts to its context
(`var f float64 = math.Pi`).

The goimport var/const support landed with a simpler interpretation:
an untyped constant is mapped to its *default* type
(untyped int → `Int`, untyped float → `Float`, untyped string →
`String`, untyped bool → `Bool`, untyped rune → `Char`) at the
member's signature — `SignatureOf` returns a single fixed type for
the member. That makes `math.Pi` behave exactly like a literal
`3.14159...` in *unconstrained* contexts, and the value compiles end
to end (`TestE2EGoPkgVars`), but it does not adapt where a narrower
or different type is expected:

```gom
let f: Float = strconv.IntSize   // E0401: expected Float, got Int
```

Gom's own numeric literals *do* adapt (`let f: Float = 2`
compiles), so the const-vs-literal difference is user-visible.

## Why it matters

- The spec text names this rule explicitly; today it holds only in
  the vacuous sense that the default type is "the" expected type
  when nothing constrains the expression.
- `math.MaxInt` and `os.O_RDONLY` are a different matter — Go
  itself treats them as `int` at use sites, so mapping them to
  `Int` with an `int64(...)` conversion at the boundary is the
  honest behavior and not part of this gap. Note that go/types
  reports `math.MaxInt` as an *untyped* int constant (`MaxInt =
  1<<63 - 1` is declared without a type), not a typed `int` one —
  but its default type is still `int`, which is what a
  `x := math.MaxInt` binding infers in generated Go. That is why
  the loader maps untyped int constants to `Int` in the Gom view
  and `Go.Int` in the raw view, and why every reference emits
  `int64(...)`. The same rule exposes an edge the other direction:
  `math.MaxUint` (`1<<64 - 1`, also untyped int) has no int64
  representation at all, so it stays Hidden (E0501 at use sites)
  rather than claim `Int` and emit a conversion that cannot
  compile.

## Plausible directions

- Treat untyped-const members like literals at the checker level:
  carry a marker ("flexible numeric") on the member's type so
  unification can fold it into the expected type, mirroring how
  int literals bind `?n` metas today.
- Or keep the default-type mapping and narrow the spec wording —
  a spec change, so per project rules it is *not* to be done
  silently; the discrepancy is recorded here instead.
