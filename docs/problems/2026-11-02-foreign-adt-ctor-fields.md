# Foreign-package ADT construction guesses `V%d` field names

- 状態: 未解決
- 発見: 2026-11-02, P1-8 review follow-up

## Situation

`ctorLit`/`ctorGoType` (`internal/codegen/expr.go`) render a variant
struct literal as `Type_Ctor{F0: a, ...}` — with field names taken
from `g.ctors`, the index of *locally declared* variants. For a
constructor of an ADT declared in another Gom package
(`typ.(*types.Con).Qual == "pkg"`), codegen emits `pkg.Type_Ctor{V0:
x, ...}` positionally-named fields — a guess that only works when
the foreign variant's fields happen to be named `V0`, `V1`, ...

## Why it is a problem

- A foreign variant with named payload (`| Circle (radius: Float)`
  exports `Radius`, not `V0`) emits a struct literal naming a
  nonexistent field — broken Go.
- The information needed (the variant's declared field names) lives
  in the imported package's `.gomi` interface, which is a later
  phase's input (P4-1+); until then codegen has no way to look it
  up.
- Same-root variants are fine (`g.ctors` covers them), as are
  builtin `Option`/`Result` constructors (special-cased to
  `option.Option_Some[A]{}` / `result.Ok[T,E](a)`).

## 関連

- `internal/codegen/expr.go` (`ctorLit`, `ctorGoType`, `tfieldName`)
- `.gomi` interface files — decisions/gomi-interface-files, TODO
  P4-1

## 考えられる対応

- When `.gomi` loading lands, index foreign variant field names
  alongside their symbols (like `g.ctors` but for imports).
- Interim: nothing to do — foreign ADTs cannot be constructed from
  Gom source yet anyway (no cross-package compilation exists).
