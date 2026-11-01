package codegen

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// This file holds the Gom-name → Go-name conventions. Visibility is
// not recorded on resolve.Symbol (it comes from .gomi membership,
// which is a P4-1 input), so every top-level name is emitted with the
// case it was written in: Gom type names are already capitalized
// (Shape), value names stay lower camel (area, mkAdd). The export
// step — capitalizing the .gomi export set — is a follow-up
// (docs/decisions/2026-11-02-generated-go-conventions.md).

// goKeywords is the set of Go keywords a bare identifier may not use.
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true,
	"continue": true, "default": true, "defer": true, "else": true,
	"fallthrough": true, "for": true, "func": true, "go": true,
	"goto": true, "if": true, "import": true, "interface": true,
	"map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true,
	"var": true,
}

// genReserved are bare identifiers generated code may emit at any
// point — every Go predeclared identifier (a shadowing `error` or
// `len` binder would silently change what `T(x)`/`len(xs)` resolves
// to) plus `t`, the *testing.T parameter of a generated test
// function. A user binder colliding with one is mangled instead.
var genReserved = map[string]bool{
	// predeclared types and values
	"any": true, "bool": true, "byte": true, "comparable": true,
	"complex64": true, "complex128": true, "error": true,
	"float32": true, "float64": true, "int": true, "int8": true,
	"int16": true, "int32": true, "int64": true, "rune": true,
	"string": true, "uint": true, "uint8": true, "uint16": true,
	"uint32": true, "uint64": true, "uintptr": true,
	"true": true, "false": true, "iota": true, "nil": true,
	// predeclared builtins
	"append": true, "cap": true, "clear": true, "close": true,
	"complex": true, "copy": true, "delete": true, "imag": true,
	"len": true, "make": true, "max": true, "min": true,
	"new": true, "panic": true, "print": true, "println": true,
	"real": true, "recover": true,
	// generated bindings
	"t": true,
}

// valueName renders a Gom value name as a Go identifier in the case
// it was written: `use_answer` → `useAnswer`, `area` → `area`.
func valueName(s string) string {
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

// exportName renders a Gom name capitalized — type names, variant
// names, record field and payload names: `radius` → `Radius`,
// `point_x` → `PointX`.
func exportName(s string) string {
	return capitalize(valueName(s))
}

// capitalize upper-cases the first letter of s.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}

// typeParamName renders a Gom type variable as a Go type parameter:
// `a` → `A`, `e` → `E`, unnamed → T<id>.
func typeParamName(name string, id int) string {
	if name == "" {
		return "T" + itoa(id)
	}
	return exportName(name)
}

// testFuncName renders a test block's name as a Go test function:
// `rect area` → `TestRectArea`. Non-identifier characters act as word
// boundaries.
func testFuncName(name string) string {
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

func isIdentRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// mangleKeyword appends `_` when the name collides with a Go keyword
// or a name generated code relies on (spec section 5: keyword
// collisions take a `_` suffix).
func (g *gen) safeIdent(n string) string {
	if goKeywords[n] || genReserved[n] {
		return n + "_"
	}
	return n
}

// fresh returns a binder-safe temporary name — base, then base1,
// base2, ... until it does not collide with any name in the file.
func (g *gen) fresh(base string) string {
	for i := 0; ; i++ {
		cand := base
		if i > 0 {
			cand = base + itoa(i)
		}
		if !g.used[cand] && !goKeywords[cand] {
			g.used[cand] = true
			return cand
		}
	}
}

// itoa renders a small non-negative int without importing strconv.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [8]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
