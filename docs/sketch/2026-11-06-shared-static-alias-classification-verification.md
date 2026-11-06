# Verification: shared static-alias classification (eval init-order residual)

- 日付: 2026-11-06
- 対象: [decision: shared-static-alias-classification](../decisions/2026-11-06-shared-static-alias-classification.md)

## 何を検証したか

The residual recorded in
[2026-11-06-eval-init-ready-order](../decisions/2026-11-06-eval-init-ready-order.md)
§5 and the gaps ledger ("残差: ValueAlias FuncDecl に下る let
の初期化順ずれ"): lower rewrites some `let`s into ValueAlias
`FuncDecl`s that eval's frontier graph could not see — a
`check.LetDecl` is a var node to eval but a function node in core.

The fix lifts the classification (`bindElems`, transitive
`static()`, `initNeedsEval`, the arity/thunks index) from
`internal/lower/alias.go` into a new shared package
`internal/staticalias`, has lower consume it unchanged, and makes
eval's `buildInitGraph` + `NewFiles` model the emitted node shapes:

- an alias-bound symbol registers as a **function node** whose body
  is the peeled element the wrapper applies (its references
  propagate to referencers like a `FnDecl` body), and binds eagerly
  to a wrapper `VFunc` at `NewFiles` time — applying it evaluates
  the element, never the hidden temp;
- a VarPat **pure** alias (`let f = g`) emits **no var node** — the
  decl produces no frontier slot;
- an **effectful** alias initializer (`let f = { …; count }`) keeps
  its hidden-temp **var node at the declaration slot** while `f`
  routes to the function node;
- non-alias bound symbols (projections, plain vars) keep the shared
  variable node.

## 設計上の落とし穴: 分類はファイル単位

`static()`'s ref-chain only sees `let`s in the *scanned* set —
lower runs per file (`Classify(f.Decls, f.Decls+Siblings)`), so a
`let h = f` in `a.gom` where `f` is `b.gom`'s alias is a plain
`var h = f` in the emitted Go. A unit-wide indiscriminate scan in
eval would mark `h` an effectful alias (`refs[f]` visible) and
shift the temp's effects. Verified adversarially:

```gom
-- a.gom: let v = { ignore (fmt.Println ("init:v")); h () }
         let h = { ignore (fmt.Println ("init:h")); f }
-- b.gom: let f = g
         fn g () -> Int = 1
         let z = { ignore (fmt.Println ("init:z")); 0 }
```

unit-wide scan: eval `v,h,z`; per-file scan (lower's, now eval's):
`h,v,z` — matching compiled (`var h = {…; f}`, h is an ordinary
var). Pinned as `TestDiffInitReadyOrderAliasAcrossFiles`.

## 検証手順と結果

### Generated Go shape confirmed (`gom build` + `go run`)

Shape (a) `let f = { ignore (…); count }` emits, as modeled:

```go
var tmp257c1f7c3 func(struct{}) int64 = func() func(struct{}) int64 {
	tmp3 := any("init:f"); v1, err1 := fmt.Println(tmp3); _ = result.FromGo(int64(v1), err1)
	return count
}()
func f(tmp4 struct{}) int64 { return count(tmp4) }
```

`var a`'s initializer calls the `f` *func*; the block's effects run
at the temp's own slot. `go run` prints `init:w, init:a, init:f,
main`.

Shape (b) `let f = g` emits **only** `func f(tmp2 …) { return g(tmp2) }`
— no var at all. `go run` prints `init:w, init:z, main`.

Shape (c) `let (f, x) = (count, {…; 0})` emits `var tmp…` +
`func f` + `var x = tmp.V1` — mixed alias/projection from one
pattern. `go run` prints `init:tmp, init:w, init:z, main`.

### Unit pins (`internal/eval`, `TestInitReadyFrontier`)

- `pure alias on a function-path loop` — the former
  `known_residual:` pin flipped from `[z w]` to the compiled
  `[w z]` (f is a function node; the self-loop became a
  function-function edge). ✔
- `effectful alias initializer splits temp var and wrapper` — new
  pin: `let a = {…; f ()}` / `let f = {…; count}` /
  `fn count = w` / `let w = {…}` gives `[w a f]` — the compiled
  `w,a,f-tmp`. ✔

`go test ./internal/eval` — ok.

### End-to-end (`internal/build`, eval vs compiled stdout)

`TestDiffInitReadyOrderValueAlias` runs each program through
`gom build` + `go build`/`go run` and `eval.NewFiles`, comparing
stdout byte-for-byte *and* against the expected order:

| shape | program | before (eval / compiled) | after |
| --- | --- | --- | --- |
| (a) effectful alias init | `a→f()`, `f={…;count}` | `w,f,a` / `w,a,f-tmp` | `init:w, init:a, init:f, main` both |
| (b) pure-alias loop | `f=g`, `g={ignore f; count}` | `z,w` / `w,z` | `init:w, init:z, main` both |
| aggregate | `let (again,_)=(count,0)` recursion | leftover-forced `z` first | `init:x, init:z, main` both |
| mixed pattern | `let (f,x)=(count,{…;0})` | f treated as var dep | `init:tmp, init:w, init:z, main` both |
| cross-file boundary | `a.gom` `h={…;f}` → `b.gom` alias | (would diverge under unit scan) | `init:h, init:v, init:z, main` both |

`go test ./internal/build -run 'TestDiffInitReadyOrder'` — all ok.

### Regression

`go test ./...` — all packages ok (the pre-lift suite passed with
the `[z w]` pin; the only changed expectation is that pin).
`internal/lower` tests unchanged — same classification, no lowering
difference; `testdata/golden` untouched (no generated-code change).
`gofmt -l .` clean, `go vet ./...` clean,
`sh scripts/check-sumtype.sh` ok.

## 残っている論点

- eval's alias wrapper is a `fnBuiltin` `VFunc` whose body evaluates
  the element per call — semantically the emitted `func f(u) =
  elem(u)`, but identity/name metadata differs from a real
  `fnLambda` value (unobservable: functions have no `==`/`show`
  surface).
- The classification stays file-local per consumer. A future
  cross-file alias feature (lifting `let h = f` where `f` is a
  sibling alias to a wrapper too) changes `Classify`'s contract for
  both consumers — a single point to amend rather than two.
