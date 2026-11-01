# A multi-result Go function aliased through `let` loses its result shape

- 状態: 解消（誤報 — the adapter is attached at the reference site; see below）

## Retraction

The reported mechanism does not exist. `let atoi = strconv.Atoi` does
not bind the raw Go callee: a `GoFuncRef` in value position is
lowered by `goFuncRefOrAdapter` (`internal/lower/expr.go`) into an
adapter `Closure` whenever the real Go signature differs from the
Gom view — which is exactly the `(T, error) -> Result` case. The
bound value is already an ordinary `String -> Result Int GoError`
closure, so a later `atoi "7"` is a plain `CallFunc` on a
single-result callee and the generated Go compiles and runs.

Verified end to end: `let atoi = strconv.Atoi` + `match atoi "7" ...`
emits a `func(string) result.Result[int64, error]` adapter wrapping
`rt.ResultFromGo(strconv.Atoi(...))` and the program prints `7`.
Locked by `TestE2EAliasResultAdapter` in
`internal/goimport/pipeline_test.go`; the single-result sibling is
`TestE2EFuncValueAdapter`.

Only when `goBareRefOK` sees one parameter, one non-error result,
and no boundary conversion does a value-position reference stay a
bare `GoFuncRef` — the case where the two views coincide anyway.

## Original report (retained for the record)

`import go "strconv"; let atoi = strconv.Atoi` binds the *Gom view*
`String -> Result Int GoError` to `atoi`, but the underlying Go
callee still returns `(int, error)`. A later application `atoi "7"`
type-checks correctly, yet lowers to `core.CallFunc` — the generic
unknown-callee path — which emits `atoi("7")` and binds both Go
results where the type says a single `Result` value lives. The
generated Go does not compile.

Direct application (`strconv.Atoi "7"`) is unaffected: the callee is
a `GoFuncRef` and lower wraps the results through `ResultFromGo`.

Why it is not fixable in lower alone: `CallFunc` is the calling
convention for *any* function value, and a `String -> Result Int
GoError` Gom closure is representation-identical to the Go function
value — the boundary adapter cannot be picked from the type alone.
Options when it matters:

- keep the Go result shape on the *value* (a distinct type for "Go
  function value"), so `let`-binding a Go callee preserves what
  `GoFuncRef` knows and the alias lowers to an adapter closure;
- or have the checker mark such apply nodes `CallGo`-shaped when the
  callee value is known to originate in `import go` (flow-sensitive,
  fragile across bindings);
- or reject multi-result Go functions as first-class values until a
  real design exists (smallest honest contract).

Single-result Go functions (`let itoa = strconv.Itoa`) already work
— both views agree on one return value, so no adapter is needed.
