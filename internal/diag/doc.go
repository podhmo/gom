// Package diag is a shared building block that defines the internal
// representation of compiler diagnostics: position spans anchored to
// UTF-8 byte offsets, severity, stable error codes, one-line English
// messages, structured data, notes, and suggested fixes.
//
// Output formats (JSON Lines, text, LSP) are renderings of this internal
// form and are invisible to the compiler proper; line and column are
// computed only at output time (docs/design.md section 2.3). The
// emitters live here too: Report.WriteJSONLines and Report.WriteText
// render a run's diagnostics plus the trailing "summary" object in the
// two wire formats of docs/spec.md section 12, resolving byte offsets
// to line and column through a Resolver (NewMapResolver for in-memory
// sources, NewDirResolver for lazy file reads). The package also holds
// the error-code table and the JSON Schema emitted by gom schema.
// Error codes are never reused.
package diag
