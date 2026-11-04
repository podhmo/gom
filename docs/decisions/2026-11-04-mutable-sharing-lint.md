# Mutable-sharing lint: what W0701/W0702 check and where they run

- 状態: 採用（P5-2 で実装・検証した — [検証](../sketch/2026-11-04-p5-2-mutable-lint-verification.md)）
- 日付: 2026-11-04

## Background

Spec section 10 ("送ってよい値と捕捉してよい値") reserves two
*warnings* for the linter — the mechanism is already decided (warning,
not error; [concurrency-model](2026-11-01-concurrency-model.md) says
"v1 ではリンターが警告する" and leaves a `Send`-style type restriction
to future work). What the spec leaves open is the lint surface: which
types count as "containing a mutable value", which expressions count
as "making a channel" and as "capturing a variable", and where in the
pipeline the warnings are produced. `internal/diag` already carries
the machinery — `diag.Warning`, `SeverityWarning`, the seeded codes
`W0701`/`W0702` under `PhaseLint` — so the only work is the analysis
and the decisions below.

## Decision

### Where the lint runs

A post-inference sweep inside `check` (`(c *checker)
lintMutableSharing`, invoked at the end of `Check` after
`validateGoGenerics`), walking the typed declarations with
`WalkDecl`/`WalkExpr`. Reasons:

- The checks need *solved* types. `Chan.make 0`'s element is a fresh
  meta at the call site; only after `finalize` has run is `Chan (Ref
  Int)` visible. A pass inside check sees the same chased types the
  errors were judged on.
- Containment must expand declared type bodies (records, variant
  payloads, aliases) — that data lives in the checker's `tyCons`
  tables and is not exported.
- The shadowed-builtin rule must answer "is `Ref` here the builtin or
  the file's own `type Ref`?" — `c.shadowedBuiltin` already answers
  exactly that.
- `Phase: lint` in the code table names the emitting *component*, not
  the package: the sweep is conceptually the linter even though it
  shares the checker's tables.

The warnings are emitted unconditionally — the spec fixes "リンターは
警告する" and defines no opt-out, so every `Compile` caller (check,
build, test, fix, dump) sees them. They are warnings, so they never
gate a phase (`diag.List.HasError` is untouched).

### The mutable set

Exactly the spec's closed list: `Ref a`, `Array a`, `Ptr T`,
`Go.Slice a`, `Go.Map k v`. Builtin recognition is by *declaration*:
under a file-local `type Ref` an unqualified `Ref` is the local ADT,
not the builtin — it is expanded like any declared type instead of
auto-flagging. `Chan`/`RecvChan`/`SendChan`/`Task` are not in the
set: channels and task handles exist to be shared safely.

### "Contains a mutable value" is transitive

A type contains mutable state when a mutable constructor is reachable
through:

- type arguments — `List (Ref Int)`, `Option (Array Int)`;
- tuple elements — `(Ref Int, Bool)`;
- declared type bodies, instantiated: a sum's variant payloads, a
  record's fields, an alias's target — `type Cell a = | Cell a` at
  `Cell (Ref Int)` counts, and so does a monomorphic `type Counter =
  { n: Ref Int }`. Expansion substitutes the constructor's arguments
  for the declaration's parameters and is guarded by a `seen` set on
  the constructor identity so recursive declarations terminate;
- the type arguments of a Go named type — `pkg.Box[Ref Int]` mentions
  `Ref Int`, so a value of the box can reach one.

Not reachable through:

- function types — a `Ref Int -> Int` value is itself immutable;
  whether a closure shares mutable cells is not visible in its type;
- opaque Go named types — `sync.Mutex`, `bytes.Buffer` cannot be
  inspected and are not in the spec's list;
- type variables and unsolved metas — a `Chan a` inside a generic
  function warns only when `a` is pinned to a mutable type at the
  instantiated use (the concrete `Chan.make` sites see the bound
  type; genuinely unresolved element metas already report E0407).

### "Making a channel" is `Chan.make` only

`Chan.make n` is the only channel constructor in the language. A
`Chan`/`RecvChan`/`SendChan` value arriving from a Go call was made
by Go, not by Gom, so it does not warn. An aliased callee (`let mk =
Chan.make; mk 0`) is not recognized — the check identifies the
call by the resolved `rt.MakeChan` symbol on `Apply.Call.Callee`
(`CallGo`), and re-exports through local lets lose it. That escape is
accepted as a v1 limitation.

The warning lands at the `Chan.make` application span with the
element type in `data.type`; one warning per call site.

### "Capturing" is a free-variable test on the typed AST

`go { body }` captures a variable when a `Var`/`MemberExpr` inside
`body` references a `KindVar` symbol (let bindings, parameters,
pattern variables — locals and package-level/imported `let`s alike)
whose defining syntax node lies *outside* the body's span. Symbol
Defs are source spans, so the test needs no scope reconstruction:
anything bound inside the body (a `let`, a lambda parameter, a match
binder) has its Def inside and is not a capture.

- Each distinct captured variable warns once per `go` expression, at
  its first use span inside the body, with `name` and `type` in
  `data`. A function symbol (`KindFunc`) is not a variable — but a
  `let`-bound value of function type is, and never warns because
  function types contain no mutable constructors.
- Nested `go` blocks warn independently — each goroutine boundary is
  a distinct sharing event.
- Captured *imported* or *sibling-file* variables count: a
  package-level `let r = Ref.make 0` or `os.Stdin` referenced inside
  `go {}` is shared mutable state exactly like a local one.

### Deliberately out of scope

- `Task.spawn`'s thunk — spec names `go { ... }` only. The thunk's
  own captures are invisible at the spawn site unless it is a lambda
  literal; flagged here so a later wave can extend the same free-var
  test to `Task.spawn` lambda arguments if the spec grows the rule.
- `select` — spec does not list it; send arms already require the
  channel to exist, so nothing new is shared by the arm itself.
- Suppression — no comment or flag silences a warning; spec defines
  none.

## Verification plan

Prototype inside check, drive the spec-fixture corpus plus targeted
programs through `gom check`, and record results in
docs/sketch/2026-11-04-p5-2-mutable-lint-verification.md: ADT/record/
tuple/alias containment, Go.Slice channel element, shadowed `type
Ref`, nested `go`, imported `let`, and the `Chan.make` alias escape.
