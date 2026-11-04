package core

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// TestFuncBase renders a test block's name as the base of the Go
// test function codegen emits: `rect area` → `TestRectArea`, an
// empty or punctuation-only name → `TestCase`. Non-identifier
// characters act as word boundaries.
//
// The rule lives in core — not codegen — because it is the contract
// two independently-lowered sibling files agree on: UnitScope.Tests
// records each sibling file's test bases so a `test "x"` in two
// files of one package can be told apart before the emitted
// `func TestX` collides in the shared Go package (docs/decisions/
// 2026-11-05-test-name-file-tag.md).
func TestFuncBase(name string) string {
	var b strings.Builder
	b.WriteString("Test")
	cap := true
	for _, r := range name {
		if r == '_' || unicode.IsSpace(r) || !isIdentRune(r) {
			cap = true
			continue
		}
		if cap {
			b.WriteRune(unicode.ToUpper(r))
			cap = false
		} else {
			b.WriteRune(r)
		}
	}
	if b.Len() == len("Test") {
		b.WriteString("Case")
	}
	return b.String()
}

// isIdentRune reports whether r can appear in a Go identifier.
func isIdentRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// ExportName renders a Gom name as the exported Go identifier the
// generated package spells: `foo` → `Foo`, `my_fn` → `MyFn`. It is
// the contract the declaring package's codegen and the .gomi
// conformance pass agree on: codegen claims package-scope names in
// this spelling (type names first, then values in canonical order),
// so a member's emitted base — and a collision between members
// claiming the same base — is computable from the interface surface
// without running codegen (docs/decisions/
// 2026-11-05-exported-value-vs-type-name.md).
func ExportName(s string) string {
	return capitalize(ValueName(s))
}

// ValueName renders a Gom value name as the Go identifier a
// non-exported declaration emits: `use_answer` → `useAnswer`,
// `area` → `area` — the case it was written in, underscores
// camelized. Like ExportName it is the codegen/iface contract for
// emitted package-scope bases (a private `_foo` claims `Foo` just
// like an exported `foo` does).
func ValueName(s string) string {
	parts := strings.Split(s, "_")
	if len(parts) == 1 {
		return s
	}
	var b strings.Builder
	b.WriteString(parts[0])
	for _, p := range parts[1:] {
		b.WriteString(capitalize(p))
	}
	return b.String()
}

// capitalize upper-cases the first letter of s.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}
