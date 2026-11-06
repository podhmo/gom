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
// Scope limits of v1: each open document compiles alone through
// build.Compile with CheckOnly — parse, resolve, check and pattern
// all run (so hover has a typed AST and exhaustiveness diagnostics
// push immediately), while lowering, codegen and any `go` tool run
// stay out of the loop. There is no workspace or multi-file package
// unit, no incremental sync, no request cancellation, and no
// generated-code position mapping — the SourceMap never enters this
// path.
package lsp
