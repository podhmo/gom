# tree-sitter grammar for Gom (tree-sitter-gom/)

- 状態: 採用（実装・検証した — editor 向けの決定で spec.md / design.md の更新対象はなし）
- 日付: 2026-11-06
- 検証: [tree-sitter-gom verification](../sketch/2026-11-06-tree-sitter-gom-verification.md)

## Background

`editor/emacs/gom-mode.el` was written regexp-first because no
tree-sitter grammar existed (sketch
[2026-11-05-emacs-gom-mode](../sketch/2026-11-05-emacs-gom-mode.md)).
The blocker is now removed: `tree-sitter-gom/` contains a real
grammar for the language, and the mode uses it when installed.

Two questions needed deciding: where newline separators come from,
and how closely the editor grammar must mirror the normative
`docs/grammar.ebnf`.

## Decisions

- **The grammar lives in `tree-sitter-gom/` at the repository root.**
  `grammar.js` is the source; `src/parser.c` is generated with
  `tree-sitter generate` and committed so that
  `treesit-install-language-grammar` (and any other consumer) can
  build it without the CLI. The normative `docs/grammar.ebnf`
  remains the reference; deviations are listed at the top of
  `grammar.js`.

- **The virtual NL token is produced by a stateless external
  scanner (`src/scanner.c`).** grammar.ebnf §3's NL-2 rule — insert
  NL iff PREV is in `can-end` and NEXT is not in `line-start` —
  splits cleanly in two:
  - *PREV*: the grammar only requests `_nl` inside `sep` positions
    (between block items, match/select arms, extern items, top-level
    declarations), which is exactly where the previous token can end
    an item. Validity of the external token therefore encodes the
    PREV half.
  - *NEXT*: after consuming the newline the scanner skips trivia
    (whitespace, more newlines, `//` and `/* */` comments — NL-3)
    and suppresses NL when the next character starts a `line-start`
    token (`|`, `>`, `&`, `+`, `*`, `%`, `<` except `<-`, `-` except
    `->`, `=` except `==`, `!` except `!=`, `/`, or the word `else`).
  - *NL-1*: inside `(`, `[`, `${` the grammar never requests `_nl`,
    so newlines there are plain whitespace without the scanner
    keeping a delimiter stack. A `{` block inside `(` requests `_nl`
    again for its items, matching "a `{` opened inside `(` still
    sees significant newlines".
  - The token covers just the `\n`; everything after `mark_end` is
    speculative lookahead, so comments keep their own nodes and
    blank lines collapse into one separator.

- **The grammar is an *editor* grammar: deliberately lenient where
  the normative grammar rejects.** The compiler stays the semantic
  authority; the tree-sitter grammar's job is a stable tree for
  highlighting, imenu and indentation, including while the user is
  mid-edit:
  - `.gom` and `.gomi` share one `source_file` start rule; body-less
    `.gomi` forms are ordinary top-level items that the compiler
    rejects per-file.
  - A newline between a construct's head and its `{` (`if c\n{ }`,
    `match s\n{...}`, `p\n-> e`) parses, where NL-2 would make the
    real parser report an error.
  - `let pat [: T] = e` keeps the type annotation the real parser
    accepts (docs/problems/2026-11-02-let-type-annotation.md).
  - A `..expr` element is accepted in any list position
    (parse_expr.go's parseList does the same; mid-list spread is the
    E0505 *semantic* error, not a parse error).
  - `{` forms other than block items — record literals/updates,
    record types, brace patterns — carry no `_sep` between elements,
    so `{ a = 1\n }` without a trailing comma parses in the editor
    while the real lexer inserts NL there and reports an error.
  - `string_content` matches `[^"\\$]+`, which spans a raw newline;
    the real lexer reports E0102 for an unterminated string at the
    line break, the editor keeps highlighting to the closing quote.

- **`gom-mode` keeps the regexp implementation as a fallback.**
  `gom-use-tree-sitter` (default t) plus `treesit-ready-p` decide
  which engine runs; the grammar stays an optional upgrade. The same
  ERT font-lock annotations and indentation expectations run under
  both engines (`make test` = `test-regex` + `test-treesit`).

- **The `internal/gomgen` EBNF interpreter is unaffected.** The
  grammar-based generator decision
  (2026-11-02-grammar-based-generator.md) chose the normative EBNF
  over tree-sitter for *generation*; this grammar serves *editors*
  and is allowed to drift from the EBNF within the documented
  leniency above.
