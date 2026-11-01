# The AST cannot represent attributes other than `@test`

- 状態: 未解決
- 日付: 2026-11-02

## Situation

`FnDecl`/`LetDecl` carry attributes as a single `TestOnly bool` —
there is no slot for an attribute name or arguments. The parser
therefore reports an unknown attribute (`@foo`) as E0101 at parse
time and drops it, since there is nowhere to put it.

This is fine while `test` is the only attribute. But attributes are
a plausible extension point (`@deprecated`, `@extern`, custom
derive markers), and parse-time rejection locks the design in
place: a later phase cannot even see that an attribute was
present.

## Why it is a problem

- If attributes grow, the AST needs `Attrs []Attr{Sp, Name, Args?}`
  on declarations, and unknown-attribute checking moves to
  resolve (the code comment reserves E0204 for that).
- Until then the surface grammar's `attribute = "@" member-name`
  promises more than the implementation can express.

## Related

- grammar.ebnf `attribute` — spec.md §10 `@test`
- internal/syntax/decl.go `FnDecl.TestOnly`, `LetDecl.TestOnly`
- internal/syntax/parser.go `parseAttrDecl`

## Possible responses

- Give declarations an attribute list now and let resolve
  diagnose unknown ones (E0204)
- Or narrow the grammar to `"@test"` literally and revisit when a
  second attribute is needed
