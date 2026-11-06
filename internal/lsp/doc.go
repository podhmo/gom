// Package lsp is the Language Server Protocol front end of the gom
// compiler (TODO P4-6, docs/decisions/2026-11-06-lsp-v1-scope.md).
// It serves a minimal v1 slice over stdio JSON-RPC: textDocument
// didOpen / didChange (full sync) / didClose, hover, and the
// server-pushed publishDiagnostics notification.
//
// The package is an output layer: compiler messages stay
// diag.Diagnostic internally and are converted to LSP shapes at the
// wire boundary (docs/design.md section 2.3 — the compiler never
// sees protocol types). Positions convert between the compiler's
// UTF-8 byte offsets and LSP's UTF-16 line/character pairs through
// diag.LineIndex (PosUTF16 for outbound spans, OffsetUTF16 for
// inbound positions).
//
// The compile unit is the package: opening or editing a .gom
// document recompiles its directory's .gom files as one
// build.CompileFiles unit — open documents' buffers shadow the disk
// files (the standard LSP overlay shape), unopened siblings read
// from disk — so a sibling-declared symbol resolves instead of
// reporting E0201. Diagnostics publish per member file: open
// documents under their own URI, unopened members under a
// synthesized file:// URI. A document that is not a .gom path (a
// .gomi interface, an untitled buffer) compiles as a singleton.
//
// Scope limits: CheckOnly — parse, resolve, check and pattern run
// (so hover has a typed AST and exhaustiveness diagnostics push
// immediately), while lowering, codegen and any `go` tool run stay
// out of the loop. There is no workspace-wide indexing, no
// incremental sync, no request cancellation, and no generated-code
// position mapping — the SourceMap never enters this path.
package lsp
