# `@name` attributes have no AST representation, so E0204 is unreachable

- 状態: 未解決
- 日付: 2026-11-02

## Situation

`internal/diag` registers **E0204 "unknown attribute"** in the
resolve phase: "the grammar accepts attributes syntactically and
rejects unknown ones during name resolution (docs/grammar.ebnf
section 1)". But the surface AST has no attribute list — `FnDecl`
and `LetDecl` carry only a `TestOnly bool`. Whatever produces the
AST must already have decided `@test` vs anything-else, so by the
time the resolver sees a file the distinction is gone and E0204 can
never be emitted where the registry says it should.

It also means the resolver cannot know where an attribute *was*
written — a parser that silently drops `@memo` would lose the
diagnostic entirely.

## Options

- keep attributes as an `Attrs []Attr{Name *Ident, Sp}` list on
  declarations and let the resolver map `@test` → `TestOnly` and
  report E0204 for the rest (what the code table assumes)
- or declare `@test` the only legal attribute and let the
  lexer/parser reject others — then E0204 never fires and should be
  retired, but spec 11 would need updating since it frames
  attributes as a general mechanism

Either way the grammar/AST and the code table disagree today; the
parser task (P1-3) should pick a side before this is decided.
