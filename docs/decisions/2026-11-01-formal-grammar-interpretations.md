# Interpretations adopted in the formal grammar (spec 1.5 / 1.6 formalization)

- 状態: draft
- 日付: 2026-11-01

## Background

While writing `docs/grammar.ebnf` for task D-1, several rules that spec.md
states in prose could not be transcribed mechanically and had to be
formalized. Genuinely open questions were split into `docs/problems/`.
This file records the interpretations the grammar commits to. The status
stays draft until the actual parser (P1-3) validates them.

## Decisions (interpretations baked into the grammar)

### Newlines (spec 1.5) as a virtual-NL insertion rule

- The lexer keeps a delimiter stack. The **innermost open delimiter**
  decides: `(` `[` `${` make newlines whitespace; `{` makes them
  separator candidates. Spec only says "inside `( )` and `[ ]` newlines
  are whitespace"; the innermost-delimiter rule additionally covers
  `{ ... }` nested inside `()`/`[]` and `(`/`)` nested inside `{}`.
- NL is inserted iff `PREV ∈ can-end and NEXT ∉ line-start`, where
  can-end = tokens that may end an expression (identifiers, literals,
  `)` `]` `}` `_`) and line-start = binary operators, `else`, and `|`.
- `|` was added to line-start beyond the spec's informal list ("など").
  Every spec example writes variants as `type X =` NL `| A` NL `| B`, and
  the preceding line ends with `)` (a can-end token), so `|` at line start
  must continue. It also enables breaking an or-pattern across lines,
  `A x` NL `| B y -> e`.
- Tokens that may NOT start a continued line (as spec says): `->` `,` `.`
  `:` `~` `in` `with` `<-` `if`. So a `match` arm's `->` must sit on the
  same line as its pattern, and `{` must sit on the same line as the
  condition/head (like Go).

### Brace ambiguity (spec 1.6) as a lookahead rule

- After `{`: `}` means empty block (`Unit`); `ident =` means record
  literal; otherwise parse one expression — `with` afterwards makes it a
  record update, else it is the first block item. `{ a = 1 }` vs `{ a }`
  is decidable because `=` can never appear inside a block item (there is
  no assignment and `==` is a separate token).
- Spec 1.6 names only `if` and `match`; the same head-expr restriction
  was applied to the iterable of `for x in e`, since `for x in {a = 1}`
  would silently read `{a = 1}` as the loop body.
- In ordinary operand position `{` may start an atom (`f { x = 1 }` is
  `f` applied to a record literal), as spec 9.2's
  `Ok { c with source = path }` confirms.
- The `{` of `go {` `select {` `else` `test "x" {` is part of the
  construct, never an expression, so it is always a block/arm list.

### Other formalization choices

- Expression shape: control expressions (`if` `match` `for` `go` `select`
  and `fn` lambdas) may not appear in operand/argument position (the same
  discipline as Rust's expression-with-block). Exception: a bare lambda
  is allowed as a pipe right-hand side — spec's
  `Task.spawn <| fn () -> { ... }` and `|?> fn c -> ...`.
  Match/select arm bodies take a whole expression (`->` does not end one).
- Operator precedence (high to low): `.` (postfix) > juxtaposition >
  unary `-`/`try` > `*` `/` `%` > `+` `-` > comparison/equality
  (non-associative) > `&&` > `||` > `|>` `<|` `|?>` `>>` `<<`
  (all left-associative, weakest). This lines spec 1.4 up with
  decisions/fsharp-pipes ("pipe family is weakest, left-associative")
  mechanically; comparison/equality was made non-associative
  (`a == b == c` is a syntax error). Spec has no precedence table, so
  this is marked [PROVISIONAL].
- A `send` arm is `send ch v -> e` with both operands restricted to
  simple-expr (atoms + member access) to prevent the `send f x v`
  mis-parse (see problems/select-send-arm-operands).
- `let`/`for` patterns are grammar-wide (all patterns) with irrefutability
  deferred to [SEMANTIC]. `_` is an atom everywhere; its placeholder
  meaning in Go calls is [SEMANTIC].
- Attribute names/arguments are not constrained by the grammar; resolve
  validates them (non-`test` attributes are accepted then rejected).
- `member-name` accepts keywords (`Chan.send`, `x.M` need it); whether to
  exclude only the keywords that begin constructs is [PROVISIONAL] —
  currently every keyword is allowed.
- Two `:` readers coexist (labeled argument vs. ascription); the labeled
  argument wins greedily — `f ~x : T` is `f (~x: T)`.

## Consequences

- grammar.ebnf sections 4 (newline), 5 (precedence) and 8 (brace
  disambiguation) encode these interpretations verbatim.
- Parser (P1-3) can be written assuming the lexer holds a delimiter
  stack and performs the NL-insertion test at each newline.
- Validation (hand-traced spec examples) was done in this thread;
  testdata/spec coverage belongs to P1-3 and later.
