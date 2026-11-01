# P0-5: Go interfaces and named types via `go/types`

- 目的: verify, with real `go/types` API calls, the claims that the
  [go-interop-boundary](../decisions/2026-11-01-go-interop-boundary.md)
  decision depends on (TODO.md **P0-5**). No compiler work; a probing
  program only.
- 関連: [design.md](../design.md) §2.5 (Go package loading, identity),
  [spec.md](../spec.md) ch.8 (type mapping, `Go.conv`, interfaces, nil),
  P3-1/P3-2 in TODO.md (future `internal/goimport`).
- 実験コード: `experiments/p0/go-types/` (standalone module
  `github.com/podhmo/gom/experiments/p0/go-types`, go 1.27, stdlib only).
  `probe/` has helpers + one `*_test.go` per question; `cmd/probe`
  prints a readable dump.
- 環境: go1.27.1 linux/amd64, `importer.Default()` (gc importer reading
  export data). 2026-11-02.

## Loading approach

The production loader will be `go/packages` (design.md §2.5). For this
experiment `importer.Default()` suffices — it surfaces the same
`types.Type`/`types.Object` graph that `go/packages` produces in
`Package.Types`, so every claim here transfers. One caveat measured
below (Q3) is about *importer instances*, and it applies equally to
`packages.Load`: identity only holds inside one load session.

## Q1 — Interface types

**Shape.** `io.Reader`, `io.Writer`, `error`, `fmt.Stringer`,
`sort.Interface` are all `*types.Named` whose `Underlying()` is a
`*types.Interface`. `error` is not special: `types.Universe.Lookup(
"error")` is a `*types.TypeName` (IsAlias=false) over `*types.Named` →
interface with 1 method `Error() string`. `any` IS different: the
universe object is a `TypeName` with `IsAlias()=true` and `.Type()` is
a `*types.Alias` over the empty `*types.Interface`.

- `iface.NumMethods()`/`Method(i)` — flattened method list (includes
  embedded). `NumEmbeddeds()`/`EmbeddedType(i)` — embedded interfaces
  as their own `*types.Named` (e.g. `io.ReadWriter` embeds
  `io.Reader`, `io.Writer`; NumExplicitMethods=0, NumMethods=2).
- Methods are `*types.Func` with `*types.Signature` (Recv is nil for
  interface methods).

**Assignability** — the checker never re-implements method sets:
`types.AssignableTo(V, ifaceType)` performs the full check, including
the pointer-receiver distinction:

- `AssignableTo(*os.File, io.Reader)` = true
- `AssignableTo( os.File, io.Reader)` = false (`Read` is a pointer
  method — reached via the embedded `*file`)
- `AssignableTo(io.ReadWriter, io.Reader)` = true (interface subset)
- `AssignableTo(io.Reader, io.ReadWriter)` = false

`types.Implements(V, *Interface)` is the same check restricted to the
method set; `AssignableTo` is the right one for call sites because it
also covers nil and identical-type cases. `types.NewMethodSet(T)` and
`types.LookupFieldOrMethod(T, addressable, pkg, name)` resolve `x.M` —
the latter returns both fields and methods, with `index` being a
promotion path (`(*os.File).Read` comes back `index=[5]` because it is
promoted from the unexported embedded `*file`).

**Two gotchas found:**

1. Method sets include unexported methods: `os.File`'s *value* method
   set is exactly `{close}` — one unexported method, while `*os.File`
   has 44. Harmless for "Go value → Go interface" checks but it means
   interfaces mentioning unexported methods can only ever be satisfied
   by their own package (moot in v1 — Gom types cannot implement Go
   interfaces anyway).
2. `any` has two spellings at the `go/types` level. In go1.27.1
   export data, an `any` parameter arrives AS `*types.Alias` over the
   empty interface — `json.Marshal`'s `v any` probes as raw node
   `*types.Alias` — while a handwritten `interface{}` parameter
   arrives as plain `*types.Interface`. Neither shape alone is
   sufficient to detect an "any position": an alias-only check misses
   `interface{}`, a raw-`*types.Interface`-only check misses the
   `any` alias. Detection MUST be structural — `Unalias`, then
   empty-interface (`NumMethods()==0 && NumEmbeddeds()==0`), which
   covers both spellings (probe.IsAny does exactly this).

**Gom representation.** Exactly the decision's model: a Go interface
type is a *nominal* Gom type (`io.Reader` is a name, not a structural
shape). `internal/types` needs a Go-nominal-type node keyed on the
`*types.Named` object; assignability questions are delegated to
`types.AssignableTo` against the Gom value's *Go* type. `error` maps
to the builtin `GoError` (spec 3.1) — it is just the universe
singleton, so `GoError` can be a fixed entry in the type table.

## Q2 — Named types and aliases

- `time.Duration`: `*types.TypeName` (IsAlias=false) → `*types.Named`,
  `Underlying()` = `*types.Basic` int64. `types.Identical(Duration,
  int64)` = **false** — nominal identity is real, confirming why
  design.md §2.5 insists on keeping `go/types` identity rather than
  strings or underlying types.
- `Named.NumMethods()`/`Method(i)` list declared methods *including
  unexported and pointer-receiver ones* (`time.Duration` reports 11,
  one of them unexported `format`). Method *sets* still differ:
  `NewMethodSet(Celsius)` = 1 vs `NewMethodSet(*Celsius)` = 2 for a
  type with `func (Celsius) String` + `func (*Celsius) Set`.
- `type D int64` → `*types.Named`; `type A = int64` → `TypeName` with
  `IsAlias()=true`, `.Type()` = `*types.Alias` (`Rhs()`, `Obj()`,
  `Origin()`, `TypeArgs()`). `types.Unalias` collapses the chain;
  aliases are transparent to `Identical` (`A` IS `int64`,
  `MyDuration` IS `time.Duration` — same `*types.Named` underneath).
- `types.ConvertibleTo` exists and answers `Go.conv` legality:
  `ConvertibleTo(Duration, int64)` = true. The Gom checker can
  implement `Go.conv x : T` validation as a single call.
- **Correction to the task premise:** `net/http` has no `type Method
  string` — `http.MethodGet` is a `*types.Const` of *untyped* string.
  So the untyped-constant rule (spec 8.1.5) covers it, not the named
  type rule. The named-string example used instead is
  `html/template.HTML` (Named over string). Other handy instances:
  `time.Month`, `fs.FileMode`, `net.IP` (named `[]byte`).
- **Spec gap found:** `*types.Struct` never appears in the spec 8.3
  table. `http.Server` is Named over struct with 31 fields (18 of
  them exported, e.g. `Addr`, `Handler`, `ReadTimeout`);
  `LookupFieldOrMethod` returns them as `*types.Var` (`IsField()`).
  Whether `srv.Addr` reads / `http.Server{Addr: ...}` literals are
  expressible in Gom is undecided — worth a decision (suggestion:
  read-only field access via the same `x.Name` syntax as methods,
  literals via `extern` only).

**Identity key.** `*types.TypeName` is unique per (pkg, name) in one
session; `Obj().Pkg().Path() + "." + Obj().Name()` is the stable
string form for caches and `.gomi`/diagnostic output.

## Q3 — Type identity across lookups

Within one loader, `io.Reader` obtained via `Scope().Lookup`, via
`io.Copy`'s second parameter, and via `io.ReadWriter`'s embedded types
is the **same `*types.Named` pointer** (`==` and `Identical` both
true). Aliases declared in checked source (`type R = io.Reader`)
unwrap to the same object.

Across two independent importers: `io.Reader` objects differ **and
`types.Identical` is false** — nominal identity compares the
`*types.TypeName` object pointers, not package paths + names.
Structural types (`[]byte`) remain `Identical` across importers.

Consequences for `internal/goimport` (P3-1):

- One compilation must share ONE package-loading session; a per-call
  `importer.Default()` or repeated `packages.Load` silently breaks
  `Identical` and every assignability check involving named types.
- `error` is a universe singleton: the `error` result of any signature
  `==` `types.Universe.Lookup("error").Type()` regardless of package.
- `types.Type` is a usable Go map key (all node impls are pointers);
  the "Go nominal type → Gom type" table can be a plain
  `map[types.Type]Type` inside the session.

## Q4 — Signature probing

`*types.Signature` decomposes cleanly:

| func | params | results | variadic | shape |
| --- | --- | --- | --- | --- |
| `strconv.Atoi` | `string` | `int, error` | no | `(T, error)` |
| `os.ReadFile` | `string` | `[]byte, error` | no | `(T, error)` |
| `encoding/json.Marshal` | `any` | `[]byte, error` | no | `(T, error)` |
| `fmt.Sprintf` | `string, []any` | `string` | **yes** | `T` |
| `fmt.Println` | `[]any` | `int, error` | **yes** | `(T, error)` |
| `os.LookupEnv` | `string` | `string, bool` | no | `(T, bool)` |
| `io.Copy` | `io.Writer, io.Reader` | `int64, error` | no | `(T, error)` |

- Params/results: `*types.Tuple`, `At(i) *types.Var` — `Name()` is
  preserved from export data (`s`, `name`, `written`, `err`), useful
  for docs and `_` placeholder diagnostics.
- `(T, error)` detection: `Results().Len()==2 &&
  types.Identical(results.At(1).Type(), universeError)`. `error` alone
  (`Results().Len()==1` + error) → `Result Unit GoError`; `(T, bool)`
  stays a tuple (comma-ok only via `extern`, as decided).
- Variadic: `sig.Variadic()`; the last param type is `*types.Slice`
  whose `Elem()` is the per-argument type (`...any` → slice of empty
  interface). `fmt.Println(..xs)` style spread maps onto this slice.
- Methods: `sig.Recv()` carries the receiver (`Buffer.WriteString`'s
  recv is `*bytes.Buffer`); result classification is identical.
- `any` params: see Q1 gotcha 2 — detect structurally.
- Function types (`*types.Signature`) are themselves nilable (Q6).

## Q5 — Generics in Go 1.27

`sig.TypeParams() *TypeParamList`; each `*types.TypeParam` has
`Index()`, `Obj()`, `Constraint()`. The constraint classifies cleanly
by unwrapping to `*types.Interface` and asking two questions:

- `iface.IsMethodSet()` — fully described by methods (covers `any`
  *and* `interface{ String() string }`-style constraints)
- `iface.IsComparable()` — the `comparable` bit
- otherwise it is a **type-set** constraint: `*types.Union` embedded
  types, `~T` terms (`Term.Tilde()`), `cmp.Ordered`-style named
  constraint interfaces (the `cmp.Ordered` object itself is a
  `*types.Named` over an interface with a union embedded).

Measured against real stdlib generics:

| func | params | strict v1 (`any`/`comparable` only) |
| --- | --- | --- |
| `unique.Make` | `[T comparable]` | callable |
| `weak.Make` | `[T any]` | callable |
| `cmp.Or` | `[T comparable]` | **callable** (it is `comparable`, not `Ordered`) |
| `slices.Contains` | `[S ~[]E, E comparable]` | NOT callable — `~[]E` is a type set |
| `slices.Sort` | `[S ~[]E, E cmp.Ordered]` | NOT callable |
| `maps.All` | `[Map ~map[K]V, K comparable, V any]` | NOT callable |

**This is the largest finding of the experiment:** the v1 boundary
"only `any`/`comparable` constraints" excludes effectively *all* of
`slices`/`maps`/`iter`/`cmp`'s useful surface, because their element
parameters always carry a `~[]E`/`~map[K]V` core-type constraint — and
`~` is needed for `E` to be inferred from `S`. Two candidate
amendments (for the decision record):

- **liberal A**: allow `IsMethodSet() || IsComparable()` — admits
  `interface{ String() string }` constraints too, still excludes type
  sets. Cheap to implement, does not fix `slices`.
- **liberal B**: also admit type sets whose terms' underlying shapes
  are Gom-representable (`~[]E`, `~map[K]V`) and instantiate with the
  *core type* mapping (`~[]E` → `List`/`Go.Slice` boundary copy). This
  unlocks `slices.Contains`/`slices.Sort` but means Gom must handle
  `E`'s constraint (`comparable`, `cmp.Ordered`) — Ordered needs a
  "Gom type is Ordered" judgement which v1 cannot express for user
  types; restricting `E` to `comparable`/`any` + `S` to `~[]E`/`~map`
  is a middle path.

`types.Instantiate(ctxt-or-nil, orig, targs, validate=true)` performs
substitution AND constraint checking (`First[[]byte,int]` → "[]uint8
does not satisfy comparable") — the checker can reuse it verbatim for
validating inferred type arguments instead of re-implementing
constraint satisfaction.

Generic named types: `Named.TypeParams()` before instantiation,
`TypeArgs()` + `Origin()` after.

## Q6 — Nil-able kinds

Authoritative one-liner: `types.AssignableTo(types.Typ[types.UntypedNil], T)`.
It agrees with manual enumeration over `Unalias(t).Underlying()`:
nilable iff `*types.Pointer`, `*types.Slice`, `*types.Map`,
`*types.Chan`, `*types.Signature` (func types), `*types.Interface`, or
`*types.Basic` kind `UnsafePointer`. Named types inherit the
underlying's nilability (`type F func()` is nilable; `type S struct{}`
is not) — so `Ptr T`/`Go.Slice`/interface values arriving from Go can
be `nil`, matching spec 8.6's "Go values can be nil" clause.

Two traps found:

- `unsafe.Pointer` is a `*types.Basic`, not `*types.Pointer` — the
  enumeration must check `Kind()` (it is nilable *and* unrepresentable
  per spec 8.5: an "unrepresentable symbol" candidate).
- `*types.TypeParam.Underlying()` returns its *constraint interface* —
  a naive underlying-walk reports every `T any` as nilable. Check for
  `*types.TypeParam` before unwrapping; `AssignableTo(nil, T)`
  correctly returns false (`var x T = nil` is illegal in Go even when
  T could be instantiated with a pointer type).

## Implications for `internal/types` / `internal/goimport` (P3-1)

The Go-facing half of the type table needs:

1. A **Go-nominal-type node**: wraps `*types.Named`, keyed by the
   object (map by `types.Type` directly, or `pkg.Path()+"."+name` for
   serialization). Covers named scalars (`Duration`), named
   interfaces (`io.Reader`), named structs (`time.Time`), named
   containers (`net.IP`). `GoError` is the pre-wired entry for the
   universe `error`.
2. **Structural mirrors** for representable shapes: `*types.Basic` →
   builtin scalars (exactness table: `int`/`int64`→`Int` …),
   `*types.Slice`→`List` boundary copy / `Go.Slice`, `*types.Map`→
   `Go.Map`, `*types.Chan`→`Chan`/`RecvChan`/`SendChan`,
   `*types.Pointer`→`Ptr` (with the nil caveat), `*types.Signature`→
   tuple-taking function.
3. A **unrepresentable marker** with a reason, not an error at import:
   `unsafe.Pointer`, `*types.Array`? (arrays are representable in
   principle — `Array`? spec is silent), structs-until-decided,
   generics whose constraints fall outside the boundary, type params
   in signatures when the boundary check fails. Symbols referencing
   them become "referenced → diagnostic" per the decision.
4. Shared helpers (all demonstrated): `IsError` (Identical vs universe
   error), `IsAny` (empty interface post-Unalias), `ClassifyResults`
   (spec 8.3 table), `ClassifyConstraint` (Q5), `Nilable` (Q6),
   `ConvertibleTo` for `Go.conv`, `AssignableTo` for interface params,
   `LookupFieldOrMethod` for `x.M`, `Instantiate(…, true)` for generic
   calls.
5. **Session discipline** (Q3): exactly one `packages.Load`/importer
   per compilation graph; the loader must live in `resolve`/`goimport`
   and be threaded, not re-created per symbol.

## Suggested append to the go-interop-boundary decision

Proposed wording for the orchestrator to append under a
`## 検証の追記 (2026-11-02)` heading (see sketch report
`2026-11-02-p0-5-go-types-mapping.md`):

> - Go の型は `*types.Named`（named scalar・named interface・named
>   struct を含む）を同一性のキーとして保持する。`types.Identical`
>   は named type の object 同一性を見るため、**1 回のコンパイルでは
>   1 つの読み込みセッション（`packages.Load` / importer）を共有する
>   必要がある**。セッションをまたぐと同名の型でも Identical になら
>   ないことが実験で確認された（docs/sketch/
>   2026-11-02-p0-5-go-types-mapping.md）
> - `(T, error)` の判定は `types.Identical(末尾結果, universe error)`
>   で実装できる。`any` は export data 上では `*types.Alias`、
>   `interface{}` と書かれた場合は `*types.Interface` と、表記によっ
>   てノードが変わるため、`Unalias` 後に empty interface かを見る
>   構造的な判定が必要（alias の有無では検出できない）
> - `Go.conv` の可否は `types.ConvertibleTo`、interface への代入可否
>   は `types.AssignableTo`、nil 代入可否は `AssignableTo(untyped nil,
>   T)`、メソッド・フィールド解決は `types.LookupFieldOrMethod` で
>   実装できることが確認された
> - `nil` になりうる型は underlying が pointer / slice / map / chan /
>   func / interface / `unsafe.Pointer` のもの（named type は基底型に
>   従う）。`*T`・interface・func を返す Go 関数の戻り値は nil を持ち
>   うる値として扱う
> - Go のジェネリック関数の制約判定は `IsMethodSet() ||
>   IsComparable()`（+ union/`~` の検出）で分類できる。ただし
>   `any`/`comparable` だけの厳格な境界では `slices`/`maps`/`cmp` の
>   主要な関数（`slices.Contains` `[S ~[]E, E comparable]` など）が
>   呼べないことが分かった。`~[]E`/`~map[K]V` 形状の制約まで広げるか
>   は P3-3 実装前に再検討する

Also noted for P0-6 / open-questions: Go struct field access and
composite literals (`srv.Addr`, `http.Server{Addr: ...}`) are not
covered by the spec 8.3 table — decide whether fields are readable in
v1.

## Concerns for P3 implementation

1. `http.MethodGet` 型の例は untyped const であり named type ではない
   — `import go` のシンボル分類は `*types.Const`（`Val()` +
   untyped `Basic`）も扱う必要がある。
2. unexported methods/fields appear in method sets and
   `LookupFieldOrMethod` results; diagnostics should either skip or
   specially report them (a Gom program can never *call*
   `os.File.close`, but it does count toward interface satisfaction
   inside package `os`).
3. Cross-session identity loss is silent — worth an early
   `internal/goimport` invariant test (load `io` twice, assert the
   objects are the same pointers).
4. Strict-boundary generics leave out `slices`/`maps`/`cmp` — the
   usable surface is currently `unique.Make`, `weak.Make`, `cmp.Or`,
   `iter.Pull` class. If that stays, P3-3's stdlib story needs a
   different "batteries" answer for list processing.

## Reproduce

```sh
cd experiments/p0/go-types
~/sdk/go1.27.1/bin/gofmt -l .     # empty
~/go/bin/go1.27.1 vet ./...
~/go/bin/go1.27.1 test -v ./probe # per-question assertions + logs
~/go/bin/go1.27.1 run ./cmd/probe # readable dump
```
