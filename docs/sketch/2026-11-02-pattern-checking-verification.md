# Verification report: internal/pattern (P1-6)

- 日付: 2026-11-02
- 対象: [decision: pattern-checking](../decisions/2026-11-02-pattern-checking.md)

The `internal/pattern` package and `internal/pattern/pattern_test.go`
are the prototype for the draft decision. The tests build typed AST
fragments directly — the end-to-end pipeline (P1-11) does not exist
yet, so `testdata/spec/E0301_nonexhaustive_match.case.json` stays
skipped and the same scenario is exercised on hand-built
`check.Match` values.

## What was verified

### Maranget usefulness on the typed AST

- Bool: `true`+`false` exhaustive; `true` alone misses `false`;
  `Some`+`None` and `Some` alone (`missing = [None]`); `Ok` alone
  (`missing = [Err _]`).
- The spec section 4 / testdata/spec scenario — `Num`, `Add (Num 0)`,
  `Add`, `Neg (Neg x)`, guarded `Neg x` — reports exactly
  `[Neg (Num _), Neg (Add _ _), Neg (Let _ _ _), Let _ _ _]`: the
  constructor declaration order, depth-first.
- Nesting (`Add (Num 0) r` covering only `Add (Num 0) _`), tuples,
  records (single constructor; omitted fields are wildcards), and
  instantiated generics (`Option (List Int)` sees the list through
  the `Some` payload: `missing = [Some ([_, .._])]`).
- Or-patterns: `Some _ | None` is exhaustive; alternatives expand per
  row; an alternative binding different variables is E0304
  (`Circle r | Rect w h`), the same bindings pass.
- As-patterns: `Some x as w` covers `Some _` exactly.
- Lists: `[] + [x, ..rest]` exhaustive; `[x]` alone misses `[]` and
  `[_, _, .._]`; `[x, y]` alone misses `[]`, `[_]`, `[_, _, _, .._]`.
- Literals: normalized keys (`0x1` ≡ `1` → E0302 on the second arm);
  Int arms can never complete — `missing = [_]`; literal + wildcard
  exhaustive.
- Guards: `Some x if g` does not count toward exhaustiveness
  (`missing = [Some _]` + the spec note), never makes a later arm
  redundant, and is itself exempt from E0302 after `_`.
- Redundancy: `_` then `Some x` → E0302; `Some _`, `None`, then
  `Some 0` → E0302 (arm-level: every alternative covered).
- Counterexample cap: a 6-variant enum with one covered arm reports
  exactly the first five in declaration order.
- Refutability: `let Some x = …` and `for [x] in …` and a literal
  parameter produce E0303 with `context` ∈ {let, for, parameter};
  variables, wildcards, tuples and `[..rest]` pass.
- Abstract (`.gomi`) types are opaque-but-inhabited: an empty match
  reports `[_]` — distinct from an empty sum type, which is
  vacuously covered.
- `CheckFile` walks fn bodies, blocks, let statements and parameters
  (one E0303 + one E0301 in the walker test).

### Termination

Two bugs showed up here on review. The first prototype recursed
forever on recursive types: `missing`/`useful` on `List` re-derived
the identical matrix state under each `Cons` specialization (a
wildcard tail expands to the same all-wildcard row shape). The
all-wildcard-row cut was then credited with termination — wrongly,
as review found: on a *multi-column* matrix with a wildcard first
column (`match (l, u) { (_, ()) -> … }` on `(List Int, Unit)`) the
row is not all-wild, and specializing every signature constructor
put the `List` column back via `Cons`, cycling the column types
2→3→2 until stack overflow.

The fix is the Σ₁ case split the paper prescribes: at a wildcard
first column, specialize per constructor only when Σ₁ (the
constructors heading column 0) is a complete signature; an
incomplete Σ₁ takes the default matrix `D(P)` directly — the
missing-ctor specializations are vacuous there. The decreasing
measure is the constructor-node count of matrix and pattern vector
(wildcards excluded), then the column count, lexicographic — the
same measure the wasabi315/coverage-checking Agda mechanization of
this algorithm uses. The all-wildcard-row cut stays as a shortcut
for the common covered case, not the guarantee.
`TestRecursiveColumnTerminates` reproduces the reviewer's
`(List Int, Unit)` repro plus `(List Int, Bool)` and
`(List Int, <record>)` under a per-case timeout, and asserts the
`(_, false)` / `([], …)` witnesses come out in declaration order;
`TestSpecExampleOrder`'s expected output is unchanged.

### Spec section 12 wire shape

`TestE0301WireShape` marshals the diagnostic for the spec fixture's
source through `diag.MarshalDiagnostic` and compares against the
expected JSON: `code`/`severity`/`message` fields, `scrutinee_type`,
the four `missing_patterns`, the guards note, and the placeholder fix
— single edit at offset 289 (line 16 col 3, the byte before `}`) with
the exact text
`"  Neg (Num _) -> todo ()\n    Neg (Add _ _) -> todo ()\n    …\n  "`.

## Known limitations

- Per-alternative or-pattern redundancy (Maranget 2007 §6) is not
  implemented; arm-level only.
- Or-pattern expansion is a cartesian product per position — fine
  here, a candidate for a cap if it ever shows up in profiles.
- `.gomi`-imported ADTs need `Env.AddType` calls at wiring time
  (P1-11); unregistered types degrade to the unknown-constructor
  path, which stays conservative.

## Verification commands

- `go test ./internal/pattern/ -count=1` — all pass;
  `go test ./internal/pattern/ -count=3 -race` — all pass.
- `gofmt -l .` — clean; `go vet ./...` — clean; `go test ./... -count=1`
  — all pass; `make check` — all pass.
