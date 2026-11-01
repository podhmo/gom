# `type X = T` has no alias production — AliasBody is unreachable

- 状態: 未解決
- 日付: 2026-11-02

## Situation

grammar.ebnf defines `type-body = variant-list | record-type`.
There is no `type X = SomeType` alias production, so under the
current grammar:

- `type X = Int` parses as a sum with a single nullary variant
  `Int` (variants are uppercase), which is silently the wrong
  meaning for an alias.
- `type X = List Int` parses as variant `List` with payload `Int`.
- `type X = a` (lowercase target) is a parse error — variants
  start with UIdent.

The AST defines `AliasBody{Type}`, but no grammar production can
produce it — the parser never builds one.

## Why it is a problem

- Type synonyms are a common expectation, and `type X = Y`
  *looks* like it should be one — instead it defines a variant
  named `Y`. That is a silent semantic trap, not an error.
- `AliasBody` in the AST suggests an alias was once planned; the
  grammar never grew the production.

## Related

- grammar.ebnf `type-decl`, `type-body`
- internal/syntax/decl.go `AliasBody` — parser.go `parseTypeBody`
- spec.md §3 (type declarations)

## Possible responses

- Add an alias production, e.g.
  `type-body = variant-list | record-type | "alias" type-expr` or
  a dedicated `type X = type-expr` reading disambiguated on the
  first token (lowercase or non-variant head ⇒ alias)
- Or remove `AliasBody` and document that `type X = Y` names a
  variant, keeping aliases to `.gomi` extern interfaces only
