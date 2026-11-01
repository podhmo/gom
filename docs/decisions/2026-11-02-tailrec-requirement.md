# `@tailrec`: a requirement attribute for tail-call optimization

- 状態: draft
- 日付: 2026-11-02

## Background

Users want to *require* constant-stack compilation: mark a function so
that compilation fails when the compiler cannot turn its recursion
into iteration. This is a design study — the feature is not
implemented.

### What exists today

Spec §1 item 7 already *guarantees* that self-tail recursion compiles
to a Go `for` loop, and the machinery exists unconditionally:

- `lower.markTailCalls` (`internal/lower/expr.go:1936`) runs after a
  `fn` body is lowered and rewrites every `CallDirect` whose `Callee`
  is the function itself and that sits in a `Body.Tail` slot — at any
  depth through `If`, `Match` (decision-tree leaves), and `SelectExpr`
  arm bodies — into `core.TailCall`, then sets `Func.TailLoop`.
- `core.TailCall` (`internal/core/call.go:142`) is the self-tail call;
  `core.Func.TailLoop` (`internal/core/node.go:96`) tells codegen to
  wrap the body in `for { ... }`.
- `codegen.tailCall` (`internal/codegen/emit.go:580`) emits
  `p0, p1 = a0, a1; continue` — a multi-assignment, so arguments are
  evaluated before rebinding.
- `gom dump -phase=core` shows `:tailloop` / `tailcall` nodes.

So today the optimization is **best-effort and silent**: calls that
convert become loops; calls that don't stay ordinary Go calls with no
diagnostic. The modifier turns the existing guarantee into a *proof
obligation* — it does not request a new optimization.

### What does and does not convert (measured, 2026-11-02)

Probe files were compiled with `gom build` and the generated Go
inspected:

| Source shape | Loops? |
| --- | --- |
| `if ... { f x }`, `match` arm bodies, `select` arm bodies | yes |
| `x |> f` at tail (desugared before the check) | yes |
| leading `let`s before a tail `if` (e.g. a `try` escape early in the body) | yes — the escape check runs inside the loop each iteration |
| arity-split fn `fn cur (n) -> Int -> Int` recursing at declared arity `cur (n-1)` | yes |
| `f x + y`, `Ok (f x)`, `Go.conv (f x) : T`, `let r = f x`, `ignore (f x)` — operand / `let` positions | no (real call) |
| `x |?> f` at tail | **no** — the result is consumed by the escape: on `Err` it is `%w`-wrapped and on `Ok` re-wrapped, so the call is genuinely not in tail position (a `Let`-bound `CallDirect` feeding an `EscapeResult`) |
| `try (f x)` at tail | unreachable as a tail call — `try` unwraps to `a`, which cannot be the `Result a e` tail value; as an operand it is a `Let`-bound call |
| self-call inside a `for` body or any `let` | no |
| self-call inside a nested lambda (`let g = fn k -> f k`) | no — different function boundary |
| self-call via alias `let g = f; g x` at tail | no — `CallFunc` on a `Var`, not `CallDirect` |
| self-call inside `go { f x }` | no — correctly not a `TailCall` (the `continue` target is a different emitted function), and harmless for stack (fresh goroutine) |
| mutual recursion `even → odd → even` in tail position | no — both sides emit real calls; stack grows linearly |
| `let f = fn n -> ... f ...` (let-bound lambda) | no — the call goes through the package `var` |
| passing `f` as a value (`apply2 f x`, `Task.spawn f`) | n/a — `FuncRef`, not a call |

No spec/implementation divergence was found: the conversion spec §1
item 7 promises is what `markTailCalls` implements. One *unrelated*
miscompile was found while probing — a `TailLoop` parameter captured
by a closure sees the rebound value — filed separately as
[problems/2026-11-02-tailloop-param-capture](../problems/2026-11-02-tailloop-param-capture.md)
(design.md §4.5's "captured variables are all immutable" assumption is
what the rebind breaks).

### Attribute surface

`grammar.ebnf` already has `top-decl = [ attribute ] , ( fn-decl |
let-decl )` with `attribute = "@" member-name` — but the AST carries
only a `TestOnly bool`, so `parseAttrDecl`
(`internal/syntax/parser.go:514`) reports any other name as `E0101` at
parse time. Two open problems already record that this representation
cannot grow: `problems/2026-11-02-attribute-representation` (E0204
unreachable) and `problems/2026-11-02-unknown-attribute-representation`.
Attributes are `.gom`-only (`gomOnly`), so `.gomi` signatures are
unaffected.

## Proposal

### Syntax

`@tailrec` on a `fn` declaration:

```gom
@tailrec
fn loop (n: Int) (acc: Int) -> Int =
  if n <= 0 { acc } else { loop (n - 1) (acc + n) }
```

- The name follows Scala's `@tailrec` (same semantics: the annotation
  asserts the function is tail-recursive; error if not).
- `fn` only. `@tailrec let` is rejected — a `let`-bound lambda calls
  itself through the package variable (`CallFunc`), which the loop
  conversion cannot see.
- The grammar's `[ attribute ]` admits one attribute per decl, so
  `@test @tailrec fn` is not expressible today; see *Alternatives* for
  generalizing to an attribute list.

### Semantics

Define the contract on the **lowered Core IR**, per `FuncDecl` marked
`@tailrec` (symbol `f`):

- **R1 — recursion required.** `f` must contain at least one
  self-call (`TailCall` or `CallDirect` to `f`). Otherwise error:
  "`f` is marked @tailrec but has no recursive call". Catches
  misplaced or stale annotations.
- **R2 — every self-call is a tail call.** After `markTailCalls`,
  walk `f`'s `Func` (all `Let.X` at every depth, plus every nested
  `Closure` and `ForRange` body); a residual `CallDirect` to `f` is an
  error: "recursive call is not in tail position" at the call's span.
  Calls inside `Spawn` (`go { ... }`) bodies are **exempt** — a
  spawned call runs on a fresh stack and cannot grow `f`'s own
  activation (the same special status spec §10 already gives `go`,
  e.g. E0702).
- **R3 — `f` is not used as a value.** A `FuncRef{Sym: f}` inside
  `f`'s own `Func` is an error: "`f` escapes as a value; constant
  stack cannot be proven". Aliasing (`let g = f; g x`) and indirect
  application defeat `CallDirect`-based detection, so they are
  rejected rather than analyzed. Uses of `f` as a value *outside* `f`
  (e.g. `List.map f` in another function) cannot recurse on `f`'s
  stack and are allowed.
- **R4 — no mutual recursion.** Build the file's direct-call graph
  (edge `g → h` for each `CallDirect` to a file-local `FuncDecl`,
  counting calls inside nested `Closure`s but not `Spawn` bodies).
  If `f`'s strongly-connected component contains another function,
  error: "`f` is mutually recursive through `g`". Go has no TCO, so
  `f → g → f` grows the stack even when every call is "in tail
  position" — without R4 the attribute lies (the `even`/`odd` probe
  compiles today with real calls). The analysis is file-local, which
  is complete today: Go forbids import cycles, so recursion cycles
  cannot cross packages, and each `.gom` file is one compilation
  unit. If multi-file packages land (open-questions), the graph must
  widen to the package.

Formalized: **`f` runs in constant stack iff its call-graph SCC is
`{f}` and every `f → f` edge is a `TailCall`** — plus R1 (nonempty)
and R3 (no value escape), which are what make the SCC check complete.

`@tailrec` changes nothing else: the conversion itself stays
unconditional, and unmarked functions keep their current behavior.

### Where the check runs

In `internal/lower`, as a post-pass in `File()` after the decl loop
(before `core.Validate`), reading `markTailCalls`' output:

1. `decl()` records marked symbols (`check.FnDecl.TailRec`, threaded
   from `syntax.FnDecl`).
2. Per marked `f`: R1–R3 walk `f`'s `Func` (a ~50-line type-switch
   walker; `markTailCalls` itself and `codegen/walk.go` are the
   templates).
3. R4 builds the file's `CallDirect` adjacency once and checks
   reachability/SCC per marked `f` (files are small).

Diagnostics go through `l.diags`/`l.err` like lowering's other
reports. This checks the *actual* criterion — "did the self-call
survive as a `CallDirect`" — so it cannot drift from what codegen
does: any construct ANF moves to `Let.X` (an operand of `Go.conv`,
`try`, `|?>`, a tuple, a string interpolation, a `for` head, a
`select` channel expression, ...) is automatically non-tail.

Proposed codes — register the `09` "Lowering" category, which also
resolves the provisional `E0901` recorded in
`problems/2026-11-02-lower-input-contract-gaps`:

| code | message sketch | data |
| --- | --- | --- |
| E0902 | `@tailrec function f has a recursive call that is not in tail position` | `fn`, call span |
| E0903 | `@tailrec function f is used as a value` | `fn`, use span |
| E0904 | `@tailrec function f is mutually recursive through g` | `fn`, `through`, path |
| E0905 | `@tailrec function f has no recursive call` | `fn` |

### What the spec must define

`@tailrec` is user-visible, so spec.md needs a definition of *tail
position* (it currently has none — §1 item 7 says only "self tail
recursion"). In source terms, a call to `f` is in tail position when
it *is* the result expression of `f`, reached only through:

- a block's or `fn` body's last expression,
- `if`/`else` branch results,
- `match` arm results (arm guards are operands, not tails),
- `select` arm results,
- the outermost call of a `|>` / `<|` pipeline when `f` is the
  applied function — the pipe desugars to the call before the check,

and nowhere else. Everything that consumes the call's result — an
operand position (arguments, `Ok (...)`, arithmetic, `Go.conv ... :
T`, interpolations), a `let` binding, a `for` body, `try` / `|?>` (the
escape post-processes the `Result`), a nested `fn` lambda, or a
partial application — takes the call out of tail position. A call
inside `go { ... }` is a separate case: not a tail call, but harmless
because the goroutine owns a fresh stack (R2's exemption).

`markTailCalls` is the executable version of this definition; the
spec text should be written to match it, not the other way around.

## Alternatives considered

### Where to check

- **Typed AST in `check` (rejected, noted for the `gom check`
  trade-off).** The same rules can be written over `check.Expr` —
  `Apply{Fn: Var f}` vs `Var f`, `Block` tails, `If`/`MatchArm`/
  `SelectExpr` arm bodies, `Lambda`/`GoExpr`/`For` as boundaries, and
  the sugared `Binary` ops treated by their desugaring (`|>`, `<|`
  preserve the outer call; `|?>`, `>>`, `<<` do not). Advantage:
  `gom check` (CheckOnly) stops before lowering, so only a
  check-phase implementation reports violations without emitting
  code. Costs: it re-derives lowering's ANF/desugar semantics on a
  second tree — the same walk `markTailCalls` already is — and can
  silently disagree with what actually converts (e.g. a future
  desugaring that lifts a call out of the tail). If `gom check`
  coverage is required, this alternative is the way to get it;
  the residual-`CallDirect` rule it approximates stays the
  authoritative one.
- **Core validator (`internal/core`)**: wrong layer — the validator
  checks compiler invariants, not user-facing rules, and its output
  is compiler-bug evidence (E0901), not a spec diagnostic.
- **A separate analysis package**: justifiable if the walk grows
  (e.g. a future `core.Walk` helper or SCC util shared with other
  passes); `internal/lower` is fine for R1–R4 as specified.

### Rule strictness

- **Scala-style (R1+R2 only, no R3/R4):** cheaper — no call graph —
  but `@tailrec` on `even`/`odd` passes vacuously while the stack
  grows. R4 is what makes "requirement" honest; it is also cheap
  (file-local SCC). Recommended as proposed; dropping R4 is the
  documented fallback if it proves noisy.
- **Stricter R3:** flag `FuncRef` of a marked `f` anywhere in the
  file (including other decls). Rejects `Task.spawn f`-style uses
  that are actually safe. The within-`f` scope is the one that plugs
  the real hole (self-calls hidden behind aliases).
- **No R1:** a non-recursive marked function passes silently —
  matches Scala, but the annotation usually signals intent that no
  self-call satisfies, so erroring is friendlier.
- **Strict `go`:** no spawn exemption — every self-call must be a
  `TailCall`. Simpler to state but makes `go { f x }` (a legitimate
  fresh-stack pattern) unwritable under the attribute, and
  `Task.spawn`-lambda versions are still caught by R2 anyway — the
  strict rule buys nothing.
- **Alias chasing:** recognize `let g = f; g x` and convert the
  `CallFunc` to a `TailCall`. Requires extending `markTailCalls`
  beyond the requirement check (a real optimization improvement, not
  just a check) — future work, not v1.
- **`let`-bound recursion:** extending `TailCall` to closures whose
  self-reference flows through a package `var` is a bigger machinery
  change; `@tailrec let` is rejected until then.

### Surface syntax

- `@tailrec` (proposed) — Scala precedent, self-describing.
- `@tailcall` — reads as per-call-site (OCaml `[@tailcall]`); the
  Gom modifier is per-function, so `tailrec` is the better name.
- A keyword (`rec fn`, `fn ... -> T where tailrec`): heavier — new
  keyword, no precedent in the grammar; attributes already exist for
  exactly this job.
- A lint (`PhaseLint`, opt-in warning): different UX — a lint sweeps
  every function, not a marked contract. Complementary, not a
  substitute.
- **Attribute list generalization:** the open problem files already
  recommend `Attrs []Attr{Name, Sp}` on `FnDecl`/`LetDecl`, moving
  unknown-attribute diagnostics to resolve (E0204 as the code table
  intends) and permitting `@test @tailrec fn`. The MVP is a
  `TailRec bool` next to `TestOnly` — consistent with today's shape
  but keeps the representation debt. Adopting `Attrs` now resolves
  two recorded problems at once; recommended if the implementation
  budget allows.

## Impact

User-visible surface changes ⇒ spec.md. Internal plumbing ⇒ design.md.

- `docs/spec.md` — §1 item 7 gains the `@tailrec` rule and the
  tail-position definition above; §11 or a new attributes subsection
  registers the name.
- `docs/grammar.ebnf` — `attribute` comment lists `tailrec` (and the
  production changes if `Attrs` is adopted).
- `docs/design.md` — §4.5 gains the requirement pass (and the
  "captured variables are all immutable" line needs the param-capture
  caveat regardless).
- `docs/error-codes.md` — register `09` Lowering; E0902–E0905 (and
  give E0901 its real entry).
- `internal/syntax` — `FnDecl.TailRec` (or `Attrs`); whitelist
  `tailrec` in `parseAttrDecl`; reject `@tailrec let`.
- `internal/check` — `FnDecl.TailRec` copied from the surface decl.
- `internal/lower` — marked-symbol set + post-pass diagnostics.
- `testdata/spec/` — one `.gom` + `.expected.json` per rule
  (AGENTS: spec rules get per-rule cases).
- `docs/decisions/README.md` — regenerated by
  `go generate ./internal/tool/decisionsdoc`.

No `testdata/golden/` changes are expected: codegen is untouched.

## Open questions

- Is a build-only diagnostic acceptable (`gom check` stays clean),
  or should the rule live on the typed AST for `gom check` coverage?
  (See *Alternatives — where to check*.)
- R4 in v1, or document "mutual recursion is not covered" as
  design.md §4.5 already does for the plain conversion?
- R1 (non-recursive marked fn errors) — error or silent accept?
- `Attrs` generalization now (resolves two open problems, enables
  `@test @tailrec`) or the `TailRec bool` MVP?
- Should a `@tailrec` fn whose parameter is captured by a closure
  get an explicit error until
  [problems/tailloop-param-capture](../problems/2026-11-02-tailloop-param-capture.md)
  is fixed, or is that bug fixed independently?
