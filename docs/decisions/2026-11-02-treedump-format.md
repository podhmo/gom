# IR テキストダンプ形式 (internal/treedump)

- 状態: draft
- 日付: 2026-11-02
- 検証: [treedump verification](../sketch/2026-11-02-treedump.md)

## Background

B-6 adds a stable textual dump of the compiler's intermediate
representations so that phase-isolated tests can compare readable
strings instead of giant Go struct literals. Three uses drove the
format: table tests inside a phase package, per-phase `gomtest`
golden fixtures that make it obvious which phase broke, and a future
`gom dump -phase=X` for tutorials and debugging. The dump is internal
— it never appears in user-facing diagnostics — but it must be
*stable*: equal trees produce byte-equal text, and an unrelated edit
produces a minimal diff. Neither property holds if the format depends
on map order, pointer values, or struct field order the reader cannot
see.

The package is `internal/treedump` with one function per phase output:
`DumpSyntax(*syntax.File)`, `DumpResolved(*resolve.Resolved)`,
`DumpTyped(*check.File)`, and `DumpType(types.Type)`. A `DumpCore`
for `core.File` follows the same conventions once P1-2b lands.

## Decisions

- **S-expression shape.** One node is `(head fields... children...)`.
  The head carries the node kind, its span, and the node's own
  payload (name, operator, resolved symbol); children are nested
  forms. Example:

  ```text
  (file@0:80 main@8:12
    (fn@14:79 add%2@17:20
      (params (param@21:28 (varpat@22:23 x%3@22:23) (: Int%4@25:28)))
      (-> Int%4@42:45)
      (block@48:79 (binary@62:75 + x%5@62:63 x%5@66:67))))
  ```

- **Spans are `@<start>:<end>`** in UTF-8 byte offsets (the
  `diag.Span` currency), glued to the token they annotate:
  `fn@14:79`, `x%3@22:23`. The file name is omitted inside nodes —
  one dump covers one file; the enclosing `file`/`resolved` head
  gives the context. A zero span is omitted entirely, so synthesized
  or fixture nodes with no position print clean.

- **Symbols are `name%<n>`** where `n` is assigned in first-encounter
  order during the single traversal that builds the output — `%1`,
  `%2`, … per dump. Keeping the source name next to the index keeps
  dumps readable, while the index makes two same-named bindings
  (`x` the parameter vs `x` the shadowing let) visibly distinct:
  `x%3` vs `x%5`. A trailing `(syms (sym %1 fmt module :pkg "fmt"
  :go) ...)` section lists every interned symbol with its kind,
  definition span, package, and `:go`/`:test` flags, so renames and
  origins are reviewable. `?%` flags a nil symbol — malformed input
  is visible rather than silently dropped.

- **Resolution is `name -> symbol`.** Where a written name was
  resolved, the dump shows both: `radius -> radius%7 :i0`,
  `Circle@72:78 -> Circle%6`. Unresolved identifiers print bare
  (`x@62:63` in a pure syntax dump), resolved ones carry their index
  inline (`x%6@62:63`).

- **Types are `:T`** on every typed node, rendered through
  `types.Type.String()` — the same rendering diagnostics use, so a
  dump and an error message agree. Renderings containing a space are
  parenthesized: `:(Int -> Int)`, `:(List Int)`. `:?` means a nil
  type. `DumpType` alone returns the atom without the colon.

- **Layout is structural, not width-based.** A form renders on one
  line while its subtree stays "flat" — zero or one non-atom child,
  itself flat. Two block children force a break: every child goes on
  its own line, indented two spaces. Line length never enters the
  decision, so renames do not reflow unrelated lines and the output
  is diff-stable.

- **Deterministic ordering.** Defs/Uses side tables are emitted as
  `(defs (def@pos -> sym) ...)` / `(uses ...)` sorted by the key
  identifier's span, then name — never in map order. File trivia
  (comments, blank lines) is merged into the declaration stream by
  position. Nothing in the output depends on map iteration, pointer
  addresses, or goroutine scheduling.

- **Escaping is minimal and total.** Bare tokens stay raw
  (`x`, `add`, `日本語`); anything containing whitespace, parens,
  quotes or backslashes — and every always-string payload (comment
  text, import paths, test names, literal text of str parts) — is
  `strconv.Quote`d: `"fmt"`, `" header"`. One escaping rule, applied
  uniformly.

- **Exhaustiveness mirrors the AST.** Every sealed-interface switch
  in the package ends with `default: panic("unreachable")`, checked
  by `scripts/check-sumtype.sh` like the rest of the tree; a new AST
  node without a dump case fails CI.

## Consequences

- Phase tests (P1-4 resolve, P1-5 check, P1-6 pattern, P1-7 lowering)
  assert against dump strings — readable diffs instead of struct
  comparison.
- A later `gomtest` side (e.g. `testdata/phases/<name>.<phase>.dump`
  fixtures) and `gom dump -phase=X` reuse the same functions; the
  format above is the contract they freeze.
- `DumpCore`/`DumpCoreFile` for `core.File` is a parallel addition
  after P1-2b — same conventions, new walker; nothing here changes.
- The format is intentionally not a parseable serialization: `?%`,
  `:pun`, `~` markers and elided zero spans privilege review-readers
  over round-tripping.
