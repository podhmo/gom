# Gom diagnostics

Condensed from `docs/spec.md` chapter 12. The authoritative catalog
is `docs/error-codes.md` (generated from `internal/diag/codes.go`).

## Stream shape

Every `gom` command writes **JSON Lines** to stdout by default — one
JSON object per line:

```json
{"kind":"diagnostic","code":"E0301","severity":"error","message":"non-exhaustive match on `Shape`","span":{"file":"badmatch.gom","start":{"line":9,"col":3,"offset":145},"end":{"line":12,"col":4,"offset":199}},"data":{"scrutinee_type":"Shape","missing_patterns":["Empty"]},"notes":[],"fixes":[{"title":"add the missing arms","applicability":"placeholder","edits":[{"file":"badmatch.gom","start":{"line":12,"col":3,"offset":198},"end":{"line":12,"col":3,"offset":198},"text":"Empty -> todo ()\n  "}]}]}
{"kind":"summary","ok":false,"errors":1,"warnings":0,"files":["badmatch.gom"],"outputs":[]}
```

- The last line is always `kind: "summary"`. Exit codes: `0` clean,
  `1` error diagnostics, `2` usage error / internal failure /
  unreadable input.
- `-format text` renders the same diagnostics for humans
  (`error[E0301]: ...` with source snippets and notes).
- Positions: byte `offset` is canonical (UTF-8); `line`/`col` are
  1-based, col in code points.
- `data` carries the structured payload per code — `expected`/`got`
  types for E0401, `missing_patterns` for E0301, etc.
- `fixes`: `applicability` is `safe` | `placeholder` | `unsafe`.
  `placeholder` inserts `todo ()` — compiles, panics at run time.
- `kind` values per command: `diagnostic` + `summary` everywhere;
  `build` adds `outputs` in summary; `fmt --check` adds
  `unformatted`; `test` adds `test`; `fix` adds `applied`.
- Go toolchain failures on generated code come back mapped to `.gom`
  positions with `"source": "go"` (E0001) — almost always a compiler
  bug, not your bug.

## jq recipes

```sh
gom check f.gom | jq -r 'select(.kind=="diagnostic") | .code + " " + .message'
gom check f.gom | jq -r 'select(.code=="E0401") | .data'          # inspect payloads
gom check f.gom | jq -r '.fixes[]? | select(.applicability=="safe")'
gom check f.gom | jq -r 'select(.kind=="summary") | .ok'          # gate on ok
```

## Reading codes

`LCCNN`: `L` severity (`E`/`W`/`N`), `CC` category, `NN` serial.
Codes are stable: never re-meaned, never reused (retired codes stay
marked).

| Category | Area | Examples |
| --- | --- | --- |
| `00xx` | toolchain | E0001 go-toolchain error on generated code, E0002 invalid input path |
| `01xx` | syntax | E0101 generic parse error, E0102 unterminated string, E0103 unterminated block comment, E0104 bad escape, E0105 bad literal byte |
| `02xx` | resolve | E0201 undefined name, E0202 duplicate, E0203 bad import, E0204 unknown `@attr` |
| `03xx` | patterns | E0301 non-exhaustive, E0302 unreachable arm, E0303 refutable let, E0304 or-pattern binds, E0305 (retired) |
| `04xx` | typing | E0401 mismatch, E0402 non-Unit mid-block, E0403 missing else, E0404/5/6 label args, E0407 annotation needed, E0408 labeled call on unknown type, E0409 `==` on fn type, E0410/11 `try` boundary/type, E0412 unbound type var, E0413 record self-by-value, E0414 div by const 0, E0415 builtin shadowed, E0416 generic instantiation cycle |
| `05xx` | Go interop | E0501 unrepresentable symbol, E0502 method on Gom value, E0503 implicit numeric conv, E0504 misplaced `_`, E0505 misplaced `..`, E0506 generic constraint, E0507 bad `Go.conv`, E0508 unsatisfied Go constraint, E0509 extern signature mismatch |
| `06xx` | packages/.gomi | E0601 missing `.gomi`, E0602 disallowed decl in `.gomi`, E0603 impl↔iface mismatch, E0604 stray `.gomi`, E0605 package-name conflict, E0606 import cycle, E0607 emitted-name collision |
| `07xx` | concurrency | E0701 `go` non-Unit, E0702 `try` across `go`, E0703 double default, E0704 recv-arm pattern, E0705 empty select; W0701/2 mutable sharing |
| `08xx` | tests | E0801 test body type, E0802 `@test` leak, E0803 assert outside test |
| `09xx` | lowering | E0901 IR invariant (compiler bug), E0902–E0905 `@tailrec` violations, E0906 eq-comparator budget, E0907 initialization cycle |
| `90xx` | evaluator | E9001–E9006 interpreter limits/invariants (test harness) |

## Diagnostic work loop

1. `gom check f.gom` → read first error only (phases stop at the
   first error boundary — later messages are often downstream noise).
2. `docs/error-codes.md` for the code's full write-up + example.
3. If the error doesn't match what spec promises, check
   `docs/implementation-gaps.md` — it may be a known divergence.
4. `gom dump -phase=check f.gom` shows the typed AST the checker
   produced when you need the compiler's view.
