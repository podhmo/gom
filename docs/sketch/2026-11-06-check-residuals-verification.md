# Wave-11 check residuals — verification report

- 日付: 2026-11-06
- 関連: [decision: nullary fn value application](../decisions/2026-11-06-go-nullary-fn-value-application.md), [decision: any-hole deferred pin](../decisions/2026-11-06-any-hole-deferred-pin.md), [prompts](2026-11-06-wave11-prompts.md) lane 1

Verification for the five wave-11 lane-1 items in
`internal/check`. Each item cites the reproducer, the change, and
where the behaviour is pinned.

## (a) `-> T` Go function values apply as `f ()`

Reproduced the problem's example: `let f = e.Error; f ()` reported
`E0401: expected String, got Unit -> ?1` while `e.Error ()`
worked — both for a bound method value and a package-level
`rand.Int`.

- `checkGomApply` now saturates a `-> T` callee applied to a
  literal `()` into a zero-argument `CallUnknown`; `replayGomApply`
  mirrors it for members settling to `-> T` after the provisional
  pass.
- `let m = x.Error` inside a lambda — where `.Error` is still a
  pendingSel — joins the member's replay via `pendingApply` with a
  new `alwaysGom` flag: the apply's callee is a Var, so no CallGo
  can name the receiver. `replayApply` picks `replayGoApply` only
  when the callee *is* the member expression.
- `unknownCall` emits `core.CallFunc{Args: nil}` — a real `f()` —
  for a `-> T` callee; `core.Validate` exempts exactly this shape
  from the one-argument-per-arrow invariant. Without it the
  zero-argument Apply lowered to the bare reference `_ = f` and
  silently dropped the call.

Verification: `gom check` clean and the emitted `f()`/`g()`/`m()`
compile under `go build`. `Unit -> T` calls still emit
`f(struct{}{})` (unitfn.gom), and a `-> T` callee over-applied
(`f 5`) keeps the ordinary residual reading. Spec fixture
`ok_go_nullary_fn_value.gom` covers the bound method value, the
package function, and both deferred forms; `TestCallNullaryGoValue`
asserts `CallUnknown` applied-0.

## (b) `Go.conv` Gom-decl oracle — qualified and nested decls reject

Reproduced the unsoundness: `type Int = | Boxed` beside
`x : fs.FileMode` admitted `Go.conv x : Int` — the oracle judged
the Con by name (`Int` the builtin), came back "unproven", and the
`*types.Go` fallback accepted; the emitted `Int(x)` fails `go
build` (`fs.FileMode does not implement Int`).

`convOK`'s shadowed-builtin gate became a declaration oracle
`containsGomDecl`: `tyCons` (populated by file declarations *and*
`registerForeignTypes`, the `check.TypeDeclSource` mechanism the
task pointed at) answers "is this Con a declared Gom type" at any
nesting depth — inside `List`, `Go.Slice`, `Go.Map`, `Func`, and
`Tuple` arguments. A qualified `Con{Qual:"geom"}` the table does
not know still rejects: `Qual` is only ever assigned to a package
member or the `Go.` view. A hit rejects the pair outright rather
than merely skipping the oracle consult — the old `*types.Go`
fallback was the bypass.

Verification: `fs.FileMode → Int` (shadowed), `fs.FileMode →
List Foo`, `fs.FileMode → Go.Slice Foo`, and `List Int →
List Foo` all report E0507; `fs.FileMode → os.File` and
`Int64 → Int` still accept. `TestConvOKQualifiedGomDecl` pins the
qualified-Con rejection and the fallback's survival without a
ConvertSource. Spec fixture `E0507_go_conv_shadowed_handle.gom`.

Note: builtin `Option`/`Result` Cons are seeded in `tyCons`, so a
pair naming them now rejects early — correct: they are nominal Gom
ADTs and never Go-convertible (identity still passes through
`types.Equal`).

## (c) Deferred `any`-hole — the pin binds monomorphically

Reproduced: `x.Print(_)` inside a lambda, then `f "a"`, reported
`expected String, got Go.Any` (E0401) plus E0407s while the eager
`l.Print(_)`/`f "a"` accepted.

Two fixes in `replayGoApply`: the hole's meta binds to the
parameter in the binding direction (`unify(hole, pt)` — the
want-side `any` acceptance never bound the *got* meta, leaving the
placeholder unsolved), and the standing-result unify is pin-aware
via `unifyDeferredResult` — a premature `?res := String -> ?r`
bind conforms to `any` on the want side and unifies the tails.
Emitted `f := func(tmp1 string) { x.Print(any(tmp1)) }` — the pin
narrows the generated closure. Decision recorded in
[2026-11-06-any-hole-deferred-pin](../decisions/2026-11-06-any-hole-deferred-pin.md);
spec fixture `ok_go_hole_any_deferred.gom` covers both paths.

## (d) `...any` block want — accidental asymmetry, aligned

`checkExpr` stamped raw `want` on `*syntax.Block` while
if/match/select/recordLit use `bw` (`Go.Any` → nil): the block
node carried `Go.Any` while its last statement stayed `Float`.
Nothing in the code marks the block as special — the `bw` comment
itself lists the nodes it covers and the block's own doc says the
block's type is its last statement's, so the raw `want` read as
accidental. `checkBlock(e, want)` → `checkBlock(e, bw)`; the
E0901 worry in the `bw` comment is unaffected because a nil want
infers the honest type and boxing happens at the argument edge,
the same shape the branching nodes already produce.

Verification: `fmt.Println ({563.33})` still checks and builds;
spec fixture `ok_go_block_arg.gom`.

## (e) Raw `.String()` audit — two reachable sites fixed, E0502 latent, E0417 confirmed

- **E0401 ascription `data["type"]` (expr.go ~728) — reachable.**
  `5 : {x: Int}` renders the anon-record target; two ascriptions
  in one file leaked the mint ID (`?1` then `?4`). Now
  `types.Display(target, metaNamer())` — both report `?1`. Spec
  fixture `E0401_ascribe_meta_id.gom`.
- **E0507 `data["from"]`/`data["to"]` (checkConv) — reachable.**
  `Go.conv [todo ()] : Go.Slice Int` nests an unsolved meta:
  after an earlier `todo ()` the raw ID exceeds `?1`. One fresh
  `metaNamer()` now names both sides (the pair shares the
  diagnostic). Spec fixture `E0507_go_conv_meta_render.gom` pins
  `from: "List ?1"`.
- **E0502 `data["type"]` (expr.go ~363) — latent, fixed anyway.**
  The `sel.Kind == KindMethod` branch cannot fire through surface
  syntax today: the resolver leaves `.M` on a value receiver
  unbound (the deferred pendingSel path), so `x.M` on a Gom value
  reports E0401 "has no field or method" instead — reproduced on
  `s.Area()` and `x.WriteString` over `Int`. The site only
  observes hand-bound method members (e.g. `TestMethodOnGomValue`
  builds one); it was switched to `types.Display` for
  consistency, but no leak repro exists.
- **E0417 `judgeT` — confirmed concrete, untouched.** `judgeT`
  comes from `types.Chase(typ)` or the `Go.conv` target and is
  only used when `litRange` accepts it; every produced type is
  concrete at report time.

## Residual notes

- `x.Print(_, ..["b"])` with a *spread* beside the hole reports
  E0505 (`..e` must supply the variadic tail alone) — unrelated
  existing rule; the fixture uses `x.Print(_)` alone.
- `m ()` on a meta that is *not* a pendingSel's standing meta
  keeps the over-apply reading (`Unit -> ?r`) — the `()` marker is
  only ambiguous when a `-> T` can still arrive.
