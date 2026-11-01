# Record type declaration, literal resolution, and field punning are unspecified

- 状態: 未解決
- 日付: 2026-11-01

## Situation

While writing `docs/grammar.ebnf` (task D-1) I could not find any spec
text that defines how a record type is declared or how a record literal
gets its (nominal) type.

What spec.md does define:

- record literal `{ name = "a", age = 3 }` and update `{ p with age = 4 }` (§1.2)
- record patterns `{ name, age = 0 }` with field punning and `..` (§4.1)
- "レコードは単一コンストラクタとして扱う" — a record is treated as a
  single-constructor type (§4.1)
- types are nominal (§3), so a record value must belong to some named type

What is missing:

- a `type` declaration form for records, e.g. `type Point = { x: Float, y: Float }`
  is never shown (all `type` examples use `|` variants)
- which type a bare literal `{ name = "a" }` belongs to: is the type
  inferred from expected type / field-set lookup? What if two record
  types share field names?
- whether `{ name }` (single field, no `=`) is allowed as a punned
  record literal — patterns allow punning but expression-level punning
  is never shown. In the grammar I read `{ name }` as a block
- whether `{}` can be an empty record (the grammar reads `{}` as an
  empty block, i.e. `Unit`)
- whether a record type can appear inline (`fn f (p: { x: Int }) -> ...`)

## Why it is a problem

The grammar must pick a syntax; if it guesses wrong the parser (P1-3)
and `.gomi` format will need rework. Also, literal resolution affects
name resolution (field names as labels).

## Related

- spec.md §1.2, §3, §3.2, §4.1 — `docs/grammar.ebnf` marks
  `record-type` and the `{ name }` reading [PROVISIONAL record-type-syntax]

## Possible responses

- Declare records with `type P = { name: T, ... }` and resolve a literal
  `{ name = v, ... }` by its expected type, erroring on ambiguity
- Alternatively: a record IS a single-variant ADT with named fields
  (`type P = | P (name: T)`) and `{ name = v }` is sugar requiring the
  expected type to determine the constructor
- Decide on expression-level punning `{ name }` and empty records `{}`
  explicitly — both currently read as blocks
