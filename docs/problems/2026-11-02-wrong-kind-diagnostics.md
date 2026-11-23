# Wrong-kind name references share E0201 with truly undefined names

- 状態: 解消（2026-11-23 wave36 棚卸し — 「leave it」選択を採択済みとして扱う: E0201 のコード表エントリが wrong-kind 用途を明示的に記述し（"The same code covers wrong-kind uses that have no legitimate reading"）、`data` の `namespace`/`resolved_kind` で形状を区別する設計が成立。LSP 実運用下で分かち書きの需要は確認されていない）
- 日付: 2026-11-02

## Situation

The resolver reports several distinct failure shapes under one code:

- undefined name / type / typevar / constructor / member — genuine
  unbound names
- "`Some` is not a constructor" — the name resolved, but to a
  non-constructor (a function in pattern-head position)
- "`x` is not a package" — the qualifier of `q.sel` resolved, but to
  neither module nor type
- "`Circle` has no field or label `zz`" — the constructor resolved,
  but has no such payload slot

All are E0201 because the code table only defines "undefined name",
"duplicate definition", and "cannot resolve import" for resolution.
The `data` block distinguishes them (`namespace`, `resolved_kind`,
`in`), so consumers are not blocked — but the messages a user sees
("undefined name" vs "wrong kind of thing") deserve to differ, and
the code cannot say so.

## Options

- leave it: the structured `namespace`/`resolved_kind` data carries
  the distinction and the one-line message already varies
- register a wrong-kind code (the E02xx block has room) and split
  "resolved to the wrong kind of symbol" out of E0201

Worth deciding when the diagnostic output formats are exercised
against real files — if E0201 ends up covering a confusing variety
of situations in practice, split then.
