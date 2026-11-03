// Package stdlib embeds the interface files of the Gom-written
// standard library so the compiler can resolve the builtin stdlib
// modules (List, Option, Result, ...) without reaching the
// filesystem — a gom binary compiled anywhere still carries them.
//
// The package holds no Go API of its own: each stdlib/<name>
// subdirectory is a Gom package (a .gom implementation, its .gomi
// interface, the generated .go) plus the hand-written representation
// file generated code expects (types.go).
package stdlib

import "embed"

// Gomis is the source of every standard-library interface file,
// keyed by its slash path ("list/list.gomi", ...).
//
//go:embed list/list.gomi option/option.gomi result/result.gomi
var Gomis embed.FS
