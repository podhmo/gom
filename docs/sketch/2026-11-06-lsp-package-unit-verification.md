# LSP package-unit compile — verification report

Verifies [docs/decisions/2026-11-06-lsp-package-unit.md](../decisions/2026-11-06-lsp-package-unit.md):
the `gom lsp` compile unit becomes the package (dir's `*.gom`,
open-buffer overlay), diagnostics publish per member file, sibling
declarations resolve.

Method: (a) in-process JSON-RPC tests in `internal/lsp/server_test.go`,
(b) a real Emacs/Eglot session against the built `gom` binary,
(c) a wall-time measurement of the per-recompile cost.

## In-process tests (`go test ./internal/lsp/`)

| Test | Covers |
| --- | --- |
| `TestServePackageTwoFiles` | didOpen(a) with unopened b.gom on disk: first compile sees both members (a from buffer, b from disk), a gets only its own E0401 — no E0201 on `helper` — and b's E0401 publishes under the synthesized `file://` URI before the client ever opens it; didOpen(b) then routes the same set under the client URI |
| `TestServePackageChangeFlipsSibling` | didChange(b) recompiles the unit; a's published set flips (removing the sibling error removes a's E0201-class complaint) — a change in one file republishes diagnostics for the other |
| `TestServePackageCloseKeepsSets` | didClose(a) clears a's client URI, then the package republish refills b's set — the surviving member's diagnostics outlive the closed tab |
| `TestServePackageHoverCrossFile` | hover on a use whose declaration lives in the sibling returns `helper : Int -> Int` |
| `TestServePackageSiblingParseError` | a malformed sibling publishes its parse diagnostic under its own URI; the healthy member keeps its own parse diagnostics (later-phase sets are withheld by `CompileFiles`'s batch-abort semantics — unit-level behavior, not misattribution) |
| `TestServePackageSiblingRetracts` | an unopened sibling's set retracts (empty publish) when the disk file is *fixed* between compiles |
| `TestServePackageSiblingDeleteRetracts` | an unopened sibling *deleted* from disk leaves the unit — its last pushed set is ghost-retracted with an empty publish and the `published` record drops |
| `TestServePackageURIRespell` | a member whose pushed URI respells (synthesized `file:///` ↔ client `file://localhost`) gets the stale slot cleared before the new one fills; the registered URI updates |
| `TestServePackageSingletonUnits` | non-absolute/`.gom`-suffixed opaque names (`untitled:scratch.gom`) compile alone — the CWD-bait regression for the `filepath.IsAbs` guard — and a `.gomi` compiles alone inside a real package directory |
| `TestServePackageCompileCost` | wall-time log of per-recompile cost (below; skipped under `-short`) |

Plus the v1 invariants still hold (session round-trip, UTF-16
columns, hover variants, panic boundaries, after-shutdown rules).

## Real-client check — GNU Emacs 30.2 + Eglot

Batch Eglot session against the built binary, package
`/tmp/lsp-pkg2/`:

- `a.gom`: `package main / let w = helper 1 / let za = "a" + 1` — one
  own E0401, one use of a sibling declaration.
- `b.gom`: `package main / fn helper (x: Int) -> Int = x / let wb =
  "oops" + 1` — one own E0401; file never opened in the editor.

Session transcript summary (recorded plist):

```text
(:server . t)                          ; eglot--connect over stdio
(:a-codes "E0401")                     ; only a's own error — no E0201 on helper
(:b-list-only-count . 1)               ; unopened b.gom got a pushed set…
(:b-list-only-messages
  "gom [E0401]: type mismatch: expected a numeric type, got String")
(:hover . "helper : Int -> Int")       ; hover on `helper` in a.gom, declared in b.gom
(:a-codes-after-edit "E0201")          ; editing a.gom republished a flipped set
(:b-after-close . 1)                   ; after killing a's buffer, b's set persists
```

Eglot files diagnostics for documents it has no buffer for in
`flymake-list-only-diagnostics`, which is exactly where b's set
landed — the synthesized `file://` URI is handled by a real client
with no special casing. One harness note: batch Emacs does not run
the `post-command-hook` path eglot's change tracker schedules
didChange through, so the script called
`eglot--signal-textDocument/didChange` directly — an artifact of
batch mode, not of the server (the same sequence hand-fed to `gom
lsp` on stdin produces didOpen → {a:E0401, b:E0401} then didChange →
{a:E0201, b:[]}, matching the test expectations).

## Per-recompile cost

`TestServePackageCompileCost` drives the real cmd/gom-shaped compile
(`build.CompileFiles` + fresh `compileOptions` with loaders) on a
two-member package:

| Package | Wall time per recompile |
| --- | --- |
| 2 files, one `import go` | ~48–50 ms — the `go list -export` subprocess behind the fresh `goimport` session dominates |
| 2 files, no imports | ~0.1 ms |

Options rebuild per compile, so `.gomi` interfaces and Go deps are
never stale. ~50 ms per keystroke-triggered recompile is acceptable
for v1 (didChange is already behind the client's own debounce); a
loader-session cache would drop imports to the no-imports scale but
trades staleness — recorded as deferred work in the decision.

## Stretch: SourceMap-aware inverse (sketch only, not implemented)

For the deferred go-tool-diagnostics task (`go build`/`go vet` output
folded back to `.gom`): the generated file already carries `//line`
directives, so the Go toolchain itself resolves positions — but only
at directive granularity and only for the file/line the directive
claims. Two holes to close:

- **`//line` drift**: inside one anchored region the toolchain keeps
  incrementing the directive's line counter, so a diagnostic 3 lines
  into a construct maps to `<.gom line +3>` — a real but *wrong*
  `.gom` line when the construct's body spans fewer lines than the
  drift claims. `SourceMap.SpanAt(line)` already encodes the
  non-drifted answer: nearest anchor at-or-before the generated line,
  reporting the whole construct span. The inverse layer should prefer
  `SpanAt` over trusting the toolchain's translated line, and flag
  ambiguity (diagnostic lands inside a construct whose span covers
  many `.gom` lines — report the construct head, note imprecision).
- **SpanAt granularity**: it returns whole spans, not the exact
  `.gom` line. For editor squiggles whole-construct highlighting is
  usable but noisy; if finer precision is needed the anchor table
  would need per-statement anchors — a codegen emission change, i.e.
  a separate decision, not part of the LSP lane.

Files: `internal/diag/srcmap.go` (`Anchor`, `SourceMap.SpanAt`,
`LineAt`); the folding would live beside `memberDiagnostics` in
`internal/lsp` or a `golint`-style adapter — no change needed in the
LSP lane for now.

## Conclusion

All required behaviors verified: package-granularity compile with
open-buffer overlay, per-file diagnostic routing (open docs' own
URIs + synthesized URIs for unopened members), didChange
cross-invalidation, didClose recompilation, cross-file hover, and
correct parse-error attribution — in tests and in a real Emacs/Eglot
session. Decision status → 採用.
