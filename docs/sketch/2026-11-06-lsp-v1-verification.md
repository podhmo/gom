# Verification: LSP v1 foundation (hover + pushed diagnostics)

- Date: 2026-11-06
- Decision: [lsp-v1-scope](../decisions/2026-11-06-lsp-v1-scope.md)

## What the slice claims

`gom lsp` serves a stdio JSON-RPC loop: `initialize`, `didOpen`,
`didChange` (full sync), `didClose`, `hover`, `shutdown`, `exit`,
with server-pushed `textDocument/publishDiagnostics`. Positions are
UTF-16 on the wire, byte offsets inside. Hover renders typed-AST
nodes; diagnostics are `diag.Diagnostic` values rendered at the
protocol boundary.

## Scripted round-trip (go test ./internal/lsp)

`TestServeSessionRoundTrip` drives a full session over in-process
pipes:

```
initialize → didOpen(bad src) → hover(add) → didChange(good src) → shutdown → exit
```

- didOpen of `let z = add 1 "oops"` pushes one `E0401` diagnostic at
  `{line:4, character:14..20}` (UTF-16).
- hover on `add` returns `add : Int -> Int -> Int` (fenced `gom`
  block when markdown is negotiated).
- didChange to clean code pushes `diagnostics: []` — stale
  diagnostics are cleared.

`TestServeUTF16Columns` pins the position encoding: the source

```gom
let s = "😀" + 1
```

pushes a diagnostic whose end character counts 😀 as **two** UTF-16
code units (the binary-expression span is characters 8→16, not
8→15). `internal/diag`'s
`lineindex_test.go` pins `OffsetUTF16` both ways — including the
surrogate-pair interior case, which snaps back to the rune start.

`TestServeHoverVariants` hovers 11 node kinds in one fixture:
fn/type/constructor heads, let binder, record-literal field, record
field declaration, variable use, match pattern binder, constructor
pattern, member selector, and a bare literal's type.

`TestServeBeforeInitialize` (`-32002`), `TestServeUnknownAndMalformed`
(unknown method → `-32601`, malformed JSON → the loop survives),
`TestServeHoverPlaintext` (negotiation below), and the framing tests
in `jsonrpc_test.go` cover the protocol edges.

## Real client: Emacs 30.2 + Eglot (batch)

Setup: `/tmp/lsptest/err.gom` inside a git-rooted dir, `gom-mode` +
`eglot-server-programs` entry `("/tmp/gom" "lsp")`, connected via
`eglot--connect` in `--batch -Q`.

Observed in the Eglot events log:

```
--> initialize      (eglot advertises contentFormat:["plaintext"],
                      positionEncodings:["utf-32","utf-8","utf-16"])
--> textDocument/didOpen
<-- textDocument/publishDiagnostics
    {"uri":"file:///tmp/lsptest/err.gom","diagnostics":[{"range":
     {"start":{"line":4,"character":14},"end":{"line":4,"character":20}},
     "severity":1,"code":"E0401","source":"gom",
     "message":"type mismatch: expected Int, got String"}]}
```

- `eglot-server-capable :hoverProvider` → `t`; negotiated
  `positionEncoding` is `utf-16` (the server pins it; eglot accepts
  the pick).
- A real `textDocument/hover` on `add` returned
  `{"contents":{"kind":"plaintext","value":"add : Int -> Int -> Int"},
   "range":{"start":{"line":2,"character":3},"end":{"line":2,"character":6}}}`
  — the markup negotiation works: eglot offered only `plaintext` and
  got bare text, not a fenced markdown block.
- The pushed diagnostic was converted into a flymake object:
  `eglot--diagnostics` held
  `flymake--diag … 70 76 eglot-error gom [E0401]: type mismatch: expected Int, got String`
  (buffer positions 70–76 = the `"oops"` literal — the UTF-16→point
  conversion round-trips through eglot correctly).

One artifact of the harness, not the server: `flymake-diagnostics`
returned nil in batch mode because flymake's backend reporting is
timer-deferred (`flymake-start` scheduling doesn't fire under
`sleep-for` alone). The LSP→eglot handoff itself is proven by
`eglot--diagnostics`; interactive use would show the squiggle.

## Verified vs deferred

| claim | status |
| --- | --- |
| JSON-RPC framing + header parsing | tested (`jsonrpc_test.go`) |
| initialize gating, shutdown/exit codes | tested |
| didOpen/didChange/didClose + publishDiagnostics | tested + eglot |
| UTF-16 both directions, surrogate pairs | tested + eglot ranges |
| hover over 11 typed-node kinds | tested |
| markdown/plaintext contentFormat negotiation | tested + eglot |
| `gom lsp` CLI wiring (compileOptions, exit codes) | compiled; eglot ran the real binary |
| multi-file package units | deferred (decision records E0201 limitation) |
| go-tool diagnostics / SourceMap inverse | deferred — v1 needs no generated-line mapping |
| incremental sync, cancellation, other requests | deferred |
