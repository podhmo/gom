// Package goimport derives Gom types from Go packages loaded with
// go list -export, go/importer, and go/types (docs/design.md section 2.5).
//
// Types keep their go/types identity rather than a name string so that
// named types stay distinct. Symbols whose types cannot be represented in
// Gom are not exposed; referencing them produces a diagnostic with the
// reason. extern declarations override the derived types and conversion
// rules.
package goimport
