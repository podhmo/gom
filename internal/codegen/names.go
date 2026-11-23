package codegen

import (
	"strconv"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
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

// pkgReserved are Go names a package-level declaration may not keep:
// `init` is a special package-init slot in *every* package (a decl
// named init either fails to build or silently runs at import), and
// `main` in `package main` belongs to the entry point alone. A
// non-entry decl escapes with `_` (the spec section 5 keyword rule);
// the entry `fn main () -> Unit` keeps `main` — it emits
// `func main()` by contract. Binders are unaffected: a local `init`
// is legal Go.
var pkgReserved = map[string]bool{"init": true, "main": true}

// valueName renders a Gom value name as a Go identifier in the case
// it was written: `use_answer` → `useAnswer`, `area` → `area`. The
// rule is core.ValueName — shared with the .gomi conformance check.
func valueName(s string) string {
	return core.ValueName(s)
}

// exportName renders a Gom name capitalized — type names, variant
// names, record field and payload names: `radius` → `Radius`,
// `point_x` → `PointX`. The rule is core.ExportName — the contract
// the .gomi conformance check and codegen agree on for emitted
// package-scope names.
func exportName(s string) string {
	return core.ExportName(s)
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
// `rect area` → `TestRectArea`. The base-name rule is
// core.TestFuncBase — the contract sibling files agree on through
// UnitScope.Tests.
func testFuncName(name string) string {
	return core.TestFuncBase(name)
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

// emitKind classifies a package-level Go identifier codegen emits.
// emitInternal marks generated helpers whose names carry no
// user-visible contract — evicting them is silent (the
// go-abi-stability decision puts 内部名 outside the promise).
type emitKind int

const (
	emitFunc     emitKind = iota // fn decl
	emitVar                      // top-level let decl
	emitType                     // type decl — verbatim claim
	emitVariant                  // sum variant struct T_V — verbatim, unclaimed
	emitMarker                   // sealing method isT — listing only
	emitTest                     // generated test func
	emitInternal                 // gomEqN, gomShowN — generated helpers
)

// Diagnostic codes the emit ledger reports — declared in
// internal/diag/codes.go with their explanations.
const (
	codeEmitNameDup   diag.Code = "W0601" // verbatim double-claim — redeclared Go name
	codeEmitNameEvict diag.Code = "W0602" // suffix eviction — emitted name differs from predicted
)

// String names the kind for the EmitName listing.
func (k emitKind) String() string {
	switch k {
	case emitFunc:
		return "func"
	case emitVar:
		return "var"
	case emitType:
		return "type"
	case emitVariant:
		return "variant"
	case emitMarker:
		return "marker"
	case emitTest:
		return "test"
	case emitInternal:
		return "internal"
	}
	return "unknown"
}

// emitRecord logs one package-level emitted name and the Gom decl it
// answers for. base is the name the naming rule predicts before
// collision suffixing; it equals name for verbatim claimants (types
// and variant structs have no suffix escape). sibling marks records
// from other member files of the package unit — sibling↔sibling
// duplicate claims are the owning file's to report at its local
// span, so they stay silent here.
type emitRecord struct {
	kind    emitKind
	gomName string
	name    string
	base    string
	sp      diag.Span
	sibling bool
}

// recordEmit registers an emitted package-scope name on g's ledger
// and reports W0601 for the two collision shapes the ledger exists
// to catch:
//   - verbatim double-claims — two decls emitting the same name
//     (type/type, type/variant, variant/variant) emit broken Go, and
//   - suffix evictions — a fn/let/test pushed off the name its owner
//     predicts by an earlier claimant.
//
// Internal helpers record for the listing but never warn.
func (g *gen) recordEmit(rec emitRecord) {
	g.emitLog = append(g.emitLog, rec)
	if rec.name == "" {
		return
	}
	owner, dup := g.emitOwner[rec.name]
	if !dup {
		g.emitOwner[rec.name] = rec
	} else if g.reportEmit && warnsOnDup(rec.kind) &&
		!(rec.sibling && owner.sibling) {
		// The losing claimant may be a sibling type (Unit.Types
		// carries no span) — report at whichever decl is local.
		sp := rec.sp
		if !sp.Valid() {
			sp = owner.sp
		}
		g.emitDiags = append(g.emitDiags,
			diag.Warning(codeEmitNameDup,
				"generated Go name "+q(rec.name)+" is emitted by both "+
					q(owner.gomName)+" and "+q(rec.gomName)).
				At(sp).
				Set("emitted", rec.name).
				Set("gom_names", []string{owner.gomName, rec.gomName}))
	}
	if g.reportEmit && warnsOnEvict(rec.kind) && rec.name != rec.base {
		msg := "generated Go name " + q(rec.base) + " is taken — " +
			q(rec.gomName) + " emits " + q(rec.name)
		d := diag.Warning(codeEmitNameEvict, msg).At(rec.sp).
			Set("emitted", rec.name).
			Set("base", rec.base)
		if prior, ok := g.emitOwner[rec.base]; ok {
			d = d.Set("taken_by", prior.gomName)
		}
		g.emitDiags = append(g.emitDiags, d)
	}
}

// warnsOnDup reports whether a verbatim second claim of an emitted
// name makes the generated Go invalid (a redeclaration). Tests and
// markers never claim verbatim, and internal helpers tolerate it.
func warnsOnDup(k emitKind) bool {
	switch k {
	case emitType, emitVariant:
		return true
	}
	return false
}

// warnsOnEvict reports whether a decl that lost its predicted name
// to the suffix mechanism is worth surfacing: user-facing members
// (fn/let, test funcs) promise a name the user can spell; internal
// helpers are outside the promise.
func warnsOnEvict(k emitKind) bool {
	switch k {
	case emitFunc, emitVar, emitTest:
		return true
	}
	return false
}

// q quotes a name for a diagnostic message. Test labels are
// arbitrary string literals — an embedded control character must
// not leak into the message (diag messages are single-line).
func q(s string) string { return strconv.Quote(s) }

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
