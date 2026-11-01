# `ignore e` is spec'd but unparseable — the only discard is `let _`

- 状態: 未解決
- 日付: 2026-11-04

## Situation

Spec 1.1 item 2 prescribes `ignore e` as the explicit discard for
non-Unit middle expressions, and the spec 8.1 `report` example uses
`ignore (buf.WriteString("!"))`. `internal/syntax` and
`internal/check` both define an `Ignore` node — but nothing
constructs one: the parser has no `ignore` keyword or production, so
`ignore (...)` reaches resolve as an ordinary name reference and
fails E0201 "undefined name `ignore`".

The only working discard is `let _ = e` (wildcard `let`), which the
`import go` fixtures use: `fmt.Println` really returns
`Result Int GoError`, so a bare `fmt.Println(...)` tail of
`-> Unit` is E0401, not a statement. Verified through `gom check`.

## Why it matters

- Spec examples that follow the documented idiom do not compile.
- Once `import go` is wired end to end, discarded `(T, error)`
  calls are the common shape — the missing form is on the
  hello-world path (`testdata/golden/hello.gom` uses `let _ =`
  instead).

## Plausible directions

- Implement the `ignore e` form (keyword or builtin-ish special form
  producing `check.Ignore`), or
- amend spec 1.1 to bless `let _ =` as the discard and drop
  `ignore`, or
- both: keep `let _ =` as the general wildcard, add `ignore` later.
