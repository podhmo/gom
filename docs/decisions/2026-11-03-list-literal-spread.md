# Spread element in list literals `[x, ..xs]`

- 状態: 採用
- 日付: 2026-11-03

## Context

The specification documents `[x, ..rest]` in a list *literal* — the
same `..rest` tail a list *pattern* accepts, splicing a whole list
after the leading elements. The parser's list-literal node
nonetheless rejected `..` with
`E0101 "spread \"..\" is not allowed in a list literal"`, so spec's
surface could not be written even though every phase downstream —
typed AST (`syntax.Spread`), the checker, lowering, code generation
(`append(List[A]{x}, rest...)`) and the evaluator — already
implemented it.

(訂正: an earlier version of this note claimed docs/grammar.ebnf
had documented the literal tail "since the grammar landed". That was
wrong — only `list-pat` carried `..`; `list-expr` omitted it and its
comment still said spread was pattern/call-argument only. The
production and comment were corrected in the same change that
adopted this decision.)

Writing the standard library in Gom (P4-8) made the mismatch
blocking: cons is the canonical list constructor and
`List.cons x xs` wants the natural body `[x, ..xs]`.

## Decision

The parser accepts `..` inside list literals exactly as it does in
list patterns: a `..` element parses the following expression and
produces `syntax.Spread`, and it must be the last element. The
checker/lowering/codegen needed no change — they handled the node
already.

Semantics are the slice contract's: `[x, ..xs]` is a *fresh* list —
`append` copies into new backing storage, so the spliced list is
never mutated — and `[..xs]` alone is an immutable copy. This
matches spec section 6's wording and the `List.cons`/`List.append`
implementations in `stdlib/list`.

`[x, ..xs]` is cons-shaped, not arbitrary interleave: `..` remains
tail-only (patterns and variadic arguments follow the same rule).

## Consequences

- `stdlib/list`'s `cons`, `append`, `rev_append` bodies read as
  ordinary list code instead of `list.List`-representation puns.
- `testdata/spec/ok_list_spread_lit.gom` pins the rule; the golden
  suite pins `append(list.List[int64]{x}, xs...)`.
- Verified in
  [sketch: stdlib built by the compiler](../sketch/2026-11-03-stdlib-gom-built.md).
