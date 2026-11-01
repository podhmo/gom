# Verification report: references cited in design.md (D-0)

- 目的: verify the citations written from memory in [design.md](../design.md)
  section 8 — the two Maranget papers (§4.2) and the status of go-sumtype
  (§2.4), which B-3 depends on.
- 関連: TODO D-0, TODO B-3
- 確認日: 2026-11-02. All URLs below fetched on this date.

## Maranget 2007 — verified, citation was correct

design.md §4.2: "usefulness アルゴリズム（"Warnings for pattern matching",
JFP 2007）".

- Luc Maranget. *Warnings for pattern matching*. Journal of Functional
  Programming, Volume 17, Issue 3, May 2007, pp. 387–421.
- DOI: <https://doi.org/10.1017/S0956796807006223>
- Publisher page:
  <https://www.cambridge.org/core/journals/journal-of-functional-programming/article/warnings-for-pattern-matching/3165B75113781E2431E3856972940347>
- Author's PDF (full text):
  <http://cambium.inria.fr/~maranget/papers/warn/warn.pdf>
- Content confirmed against the abstract: it is the usefulness-algorithm
  paper. It defines the two anomalies (non-exhaustive matches, useless
  clauses), gives the recursive `U(P, q)` usefulness test, describes
  strengthening non-exhaustiveness diagnostics with counter-example values
  (§5), useless-clause / or-pattern refinement (§6), and notes the algorithm
  is implemented in the OCaml compiler and also works for Haskell (lazy
  semantics). This matches everything §4.2 relies on.

## Maranget 2008 — verified, citation was correct

design.md §4.2: "Maranget の決定木コンパイル（"Compiling pattern matching to
good decision trees", 2008）".

- Luc Maranget. *Compiling pattern matching to good decision trees*.
  Proceedings of the ACM SIGPLAN Workshop on ML (ML '08), Victoria, BC,
  Canada, 21 September 2008, pp. 35–46.
- DOI: <https://doi.org/10.1145/1411304.1411311>
- ACM page: <https://dl.acm.org/doi/10.1145/1411304.1411311>
- Pre-submission draft (title cased "Compiling Pattern Matching to good
  Decision Trees", marked "Submitted to ML'08"):
  <https://www.cs.tufts.edu/comp/150FP/archive/luc-maranget/jun08.pdf>
- Content confirmed: decision-tree compilation guided by heuristics derived
  from "necessity" (a lazy-pattern-matching concept), with experiments
  comparing against the optimizing backtracking-automata match compiler of
  Le Fessant & Maranget (2001).
- Disambiguation: do not confuse with Le Fessant & Maranget, *Optimizing
  pattern matching* (ICFP 2001), which is the backtracking-automata paper,
  nor with Maranget's earlier *Two techniques for compiling lazy pattern
  matching* (INRIA RR-2385, 1994). The ML '08 workshop paper is the one
  §4.2 means.

## go-sumtype — real but near-dormant; a maintained fork exists

**`github.com/BurntSushi/go-sumtype`** (<https://github.com/BurntSushi/go-sumtype>)

- Not archived, but minimally maintained: 15 commits total, last push
  2025-03-21 (`v0.0.0-20250321152530-ada76606277d`), which bumped
  `golang.org/x/tools` to v0.30.0 and the go directive to 1.22. 8 open
  issues; OpenSSF scorecard "Maintained: 0/10" (as of 2026-07).
- Should still build/run on Go 1.27 — it only uses stable
  `go/packages` + `go/types` APIs — but that is a build-time check for B-3,
  not something verified here.
- Generics: not supported. Open issue #22 (May 2024) shows a generic
  `MySumType[T]` switch being mishandled; the author notes the tool
  "was written well before Go generics existed".
- Checking semantics (read from `check.go` on master): a type switch on an
  annotated sealed interface is checked unless it has a `default` clause —
  with one special case: a `default` whose body is a single `panic(...)`
  call statement is still checked. That exactly matches our
  `default: panic("unreachable")` convention, provided the panic stays a
  one-statement expression.
- Annotation required: `//go-sumtype:decl MySumType`. Checks in `_test.go`
  files are skipped (commit fcb4a62), so the B-3 canary must live in
  non-test source.

**`github.com/alecthomas/go-check-sumtype`** (<https://github.com/alecthomas/go-check-sumtype>)
— maintained fork, same core algorithm:

- Active: latest commits 2026-08-28 — "update to 1.27, use analysis
  package's fact api" (#23) and a min-Go-version pin; `go.mod` says
  `go 1.26.0`, `golang.org/x/tools v0.49.0`. Explicitly kept current with
  Go 1.27. Renovate-driven dependency updates.
- Refactored into a `go/analysis` analyzer: usable as the standalone
  `cmd/go-check-sumtype` command, via `go vet -vettool`, or through
  golangci-lint's `gochecksumtype` linter (integrated by alecthomas
  himself, merged in golangci-lint v1.55.0, 2023-10).
- Annotation: `//sumtype:decl` on the type's GenDecl (no type-name
  argument). Same always-panics-`default` special case as upstream.
- Extra flags: `-default-signifies-exhaustive=false` forces checks even
  with a catch-all `default`; `-include-shared-interfaces` lets an
  interface-typed case cover multiple variants.
- Generics: partial. Commit 0697847 (2024-01-11) "don't include generic
  types in variants" means generic named types are excluded from the
  variant set — weaker checking for generic sum types, not full support.
  Our planned internal AST/IR interfaces are non-generic, so this is
  acceptable; re-verify if we ever seal a generic interface.
- Not upstreamed: no type-switch exhaustiveness analyzer exists in
  `golang.org/x/tools` or gopls as of this date.

## Other approaches found (bonus)

- `github.com/nishanths/exhaustive` — also checks *type switches* now
  (`-check=typeswitch`), via a "knowable type set" of implementers across
  the package + imports; `default` does not silence it. No annotation
  needed, but it computes implementers for every switched interface
  (opt-out rather than opt-in), so it is noisier than a `//sumtype:decl`
  marker. Also shipped in golangci-lint as `exhaustive`.
- `github.com/YuitoSato/gounion` — auto-detects sealed interfaces by their
  unexported marker method (no annotation); a `default` skips the check
  unless it ends in `panic()` or returns an error.
- `github.com/gomoni/sumlint` — `go vet` analyzer driven by a `Sum`-prefix
  naming convention for interfaces and seal methods; also has a `oneoflint`
  variant for protobuf oneofs.
- `github.com/effect-go/effect-go/analysis/exhaustive` — analyzer covering
  sealed interfaces and enum-constant switches.
- Writing our own is cheap: the whole check is ~150 lines of `go/ast` +
  `go/types` in both go-sumtype implementations, so it is a viable fallback
  if the external tools hit the generics edge cases.

## Recommendation for B-3

Use `go-check-sumtype` (the alecthomas fork) in CI:

1. Annotate each internal sealed AST/IR interface with `//sumtype:decl`.
2. Run it as a pinned tool (`go run
   github.com/alecthomas/go-check-sumtype/cmd/go-check-sumtype@<version>`
   or a Go 1.24 `tool` directive) inside `make check` / CI; golangci-lint's
   `gochecksumtype` linter is equivalent if golangci-lint is ever adopted.
3. Keep every exhaustive type switch's `default` as a single `panic(...)`
   statement (our existing convention) so the check stays active; consider
   `-default-signifies-exhaustive=false` for extra strictness.
4. The "adding a variant breaks CI" canary must be in a normal (non
   `_test.go`) file.
5. Original `BurntSushi/go-sumtype` remains a working fallback for
   non-generic code; if generic sealed interfaces appear later, re-verify —
   both tools have generics gaps and the custom ~150-line analyzer is the
   escape hatch.

## Sources checked (all fetched 2026-11-02)

- <https://doi.org/10.1017/S0956796807006223> and Cambridge Core article page
- <https://doi.org/10.1145/1411304.1411311> and ACM DL page, researchr entry,
  tufts mirror of the ML'08 submission draft
- <https://github.com/BurntSushi/go-sumtype> (README, `check.go`,
  `decl.go`, issue #22, commit history via pkg.go.dev versions)
- <https://github.com/alecthomas/go-check-sumtype> (README, `check.go`,
  `decl.go`, `go.mod`, commit history, issues)
- <https://golangci-lint.run/docs/linters/configuration/> (gochecksumtype
  settings) and golangci-lint PR #3671
- <https://github.com/nishanths/exhaustive>, <https://pkg.go.dev/github.com/YuitoSato/gounion>,
  <https://github.com/gomoni/sumlint>,
  <https://pkg.go.dev/github.com/effect-go/effect-go/analysis/exhaustive>
