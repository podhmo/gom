# `.f` on a sum value holding a different variant — eval vs compiled

- 状態: 解消（[decision](../decisions/2026-11-05-variant-field-assert-owning-ctor.md) — 名前付きペイロード読み取りは所有者バリアントをアサートすると決定。評価器が ctor 一致を検査して E9003 で panic するようになり生成コードの型アサーションと揃った。検証は [sketch](../sketch/2026-11-05-variant-field-assert-owning-ctor.md)、spec 3.2 / 8.6 節に反映）
- 日付: 2026-11-02

Spec section 3.2 lets a uniquely named payload slot be selected
through the sum type: `s.r` on `s : Shape` typechecks when exactly
one variant declares `r`, regardless of which variant the value
actually holds at run time. What the read means on a *wrong*-variant
value is not defined anywhere.

`fn getr (s: Shape) -> Float = s.r` applied to a non-`Circle` value
shows the two implementations answering differently:

- The evaluator does a dynamic lookup over the runtime variant's
  *own* field list — payload index first, then field symbol, then
  field name (`internal/eval/eval.go` `evalMember`, `VVariant`).
  `getr (Rect 2.0 3.0)` therefore returns `2.0`: the index-0 slot is
  `w`, a different field than the one named. `getr Empty` fails
  `E9004 "variant has no such field"`.
- The compiled side asserts the receiver to the owning variant's
  concrete struct — `s.(Shape_Circle).R` — and panics on any other
  variant (`core.FieldGet.Ctor`, since the letConv fix; before that
  the latent bug emitted `s.R`, which does not compile).

So `Rect 2.0 3.0` gives `2.0` under eval and a panic when compiled —
a clean-execution divergence, not just a different failure shape.
The eval index-first answer is almost certainly an accident of the
lookup order (it returns a field the program never named), but the
spec does not say the read asserts the owning variant either.

Options:

- **Assert semantics (compiled as-is)**: a named-payload read is
  legal only for the variant owning the name; a wrong-variant value
  is a contract violation and panics, matching the match DPanic
  family (spec 8.6). Then `evalMember` should check ctor identity
  before the index/sym/name fallbacks — small fix, turns the eval
  `Rect .r -> 2.0` answer into the same failure `Empty .r` produces.
- **Positional semantics (eval as-is)**: `.r` means "slot i of
  whatever variant this is". Compiled code would need a type switch
  emitting the i-th field of every variant — poor output, and the
  type of slot i differs per variant, so it cannot even produce the
  statically known field type. Not viable.
- **Static restriction**: only allow `.f` where the variant is
  provably known (let-bound ctor applications, case binds). The
  common `fn f (s: Shape) -> Float = s.r` case stops typechecking —
  the spec's wording ("名前を使える" — the name is usable in field
  references) does not support the restriction.

Assert semantics is what the generated code now does; the open
question is whether spec.md should say so explicitly and eval
should follow.
