# `let x : T = v` is not in the grammar

- 状態: 未解決
- 日付: 2026-11-02

## Situation

grammar.ebnf declares `let-decl = "let" , pattern , "=" , expr` —
no type annotation slot. But:

- the AST already has `LetDecl.Type` / `Let.Type`
- the `.gomi` production *requires* the colon: `let-sig = "let" ,
  ident , ":" , type-expr`
- the E0503 diagnostic design assumes annotations exist on lets

The P1-3 parser therefore accepts `let pat : T = e` in `.gom`
files too (one shared head parser), which is ungrammatical under
the written grammar but almost certainly intended — the `.gomi`
form cannot express a constant signature without it.

## Why it is a problem

- A user reading grammar.ebnf cannot tell `let x : Int = 1` is
  accepted, or whether it means anything beyond inference
  (e.g. is `let (a, b) : T` legal? the parser accepts it in .gom,
  rejects it in .gomi where let-sig binds a plain name).
- The checker (P1-5) will need a decision on whether the
  annotation ascribes the whole pattern or each binder.

## Related

- grammar.ebnf `let-decl`, `let-sig` — spec.md let declarations
- internal/syntax/parser.go `parseLetHead`
- docs/decisions/2026-11-02-p1-3-parser-interpretations.md

## Possible responses

- Add the optional annotation to the grammar:
  `let-decl = "let" , pattern , [ ":" , type-expr ] , "=" , expr`
- Or restrict `: T` to `.gomi` let-sig and simple names in `.gom`
