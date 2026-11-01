# `@test` on its own line is not grammatical

- 状態: 未解決
- 日付: 2026-11-02

## Situation

spec §10's example writes the attribute on its own line:

    @test
    fn unit_square () -> Shape = Rect 1.0 1.0

but grammar.ebnf declares `top-decl = [ attribute ] , ( fn-decl |
let-decl )` — there is no `sep` between the attribute and the
declaration, so under a literal reading `@test NL fn` is a syntax
error (NL cannot appear inside the production).

The P1-3 parser accepts the separator: after the attribute name it
skips NL/`;` before requiring `fn` or `let`. This makes the spec
examples parse, but it is a divergence the grammar does not
sanction.

## Why it is a problem

- If the own-line style is intended, the grammar production must
  admit `[ sep ]` after the attribute — a spec/grammar edit.
- If it is not intended, spec §10's own example is wrong and the
  parser's leniency hides a style the spec does not bless.
- Either way a reader comparing grammar and implementation finds
  an undocumented difference.

## Related

- spec.md §10 — grammar.ebnf `top-decl`, `attribute`
- internal/syntax/parser.go `parseAttrDecl`
- docs/decisions/2026-11-02-p1-3-parser-interpretations.md

## Possible responses

- Amend the grammar: `top-decl = [ attribute , sep ] , ( fn-decl |
  let-decl )` (or bake the optional sep into `attribute`)
- Or tighten the parser to require same-line and fix spec §10
