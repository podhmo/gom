package gomgen

// The typed-ish layer (Options.Typed). It replaces the mechanical
// walk for declaration-level and expression-level emission with a
// type-directed one: signatures are picked first, then bodies are
// emitted to aim at them; patterns bind names the arm bodies can
// actually use; applications prefer callees whose declared arity
// matches. "Typed-ish" on purpose — the goal is a high fraction of
// programs that typecheck and reach codegen, not a proof. Anything
// it gets wrong still lands inside the user-diagnostic contract.

import (
	"fmt"
	"strings"
)

// ------------------------------------------------------------------
// generator-side types
// ------------------------------------------------------------------

type gtyKind int

const (
	tInt gtyKind = iota
	tFloat
	tBool
	tString
	tChar
	tUnit
	tList
	tOption
	tResult
	tTuple
	tFn
	tADT    // user-declared variant type; name is the type name
	tRecord // user-declared record type; name is the type name
	tNamed  // opaque builtin-scope name (GoError)
)

type gty struct {
	kind gtyKind
	name string // tADT, tRecord, tNamed
	args []*gty // tList/tOption: [elem]; tResult: [ok, err];
	// tTuple: elems; tFn: params..., ret (last)
}

func (t *gty) eq(o *gty) bool {
	if t == nil || o == nil {
		return t == o
	}
	if t.kind != o.kind || t.name != o.name || len(t.args) != len(o.args) {
		return false
	}
	for i, a := range t.args {
		if !a.eq(o.args[i]) {
			return false
		}
	}
	return true
}

var (
	intT    = &gty{kind: tInt}
	floatT  = &gty{kind: tFloat}
	boolT   = &gty{kind: tBool}
	stringT = &gty{kind: tString}
	charT   = &gty{kind: tChar}
	unitT   = &gty{kind: tUnit}
)

func listT(e *gty) *gty   { return &gty{kind: tList, args: []*gty{e}} }
func optionT(e *gty) *gty { return &gty{kind: tOption, args: []*gty{e}} }
func resultT(ok, err *gty) *gty {
	return &gty{kind: tResult, args: []*gty{ok, err}}
}
func tupleT(elems ...*gty) *gty { return &gty{kind: tTuple, args: elems} }
func fnT(ret *gty, params ...*gty) *gty {
	return &gty{kind: tFn, args: append(append([]*gty{}, params...), ret)}
}

// text renders the type as a type-expr — used in signatures and
// ascriptions.
func (t *gty) text() string {
	switch t.kind {
	case tInt:
		return "Int"
	case tFloat:
		return "Float"
	case tBool:
		return "Bool"
	case tString:
		return "String"
	case tChar:
		return "Char"
	case tUnit:
		return "()"
	case tNamed:
		return t.name
	case tList:
		return "List " + t.args[0].atomText()
	case tOption:
		return "Option " + t.args[0].atomText()
	case tResult:
		return "Result " + t.args[0].atomText() + " " + t.args[1].atomText()
	case tTuple:
		parts := make([]string, len(t.args))
		for i, e := range t.args {
			parts[i] = e.text()
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case tFn:
		var b strings.Builder
		for _, p := range t.args[:len(t.args)-1] {
			b.WriteString(p.atomText())
			b.WriteString(" -> ")
		}
		b.WriteString(t.args[len(t.args)-1].text())
		return b.String()
	case tADT, tRecord:
		return t.name
	default:
		panic("unreachable")
	}
}

// atomText renders the type where a type-atom is required —
// composites get parentheses, everything else stands alone.
func (t *gty) atomText() string {
	switch t.kind {
	case tList, tOption, tResult, tFn:
		return "(" + t.text() + ")"
	default:
		return t.text()
	}
}

// ------------------------------------------------------------------
// scope and registries
// ------------------------------------------------------------------

type bindKind int

const (
	bVar    bindKind = iota // value binding (let, fn param, match arm)
	bFn                     // declared fn — full signature in fns
	bCtor                   // ADT constructor
	bModule                 // imported module name
)

type binding struct {
	name string
	t    *gty // bVar: the value's type; bFn: fnT sig; bCtor: the ADT it builds
	kind bindKind
}

// paramDef is one fn parameter group: "(x: T)", "(~x: T)" or "()".
type paramDef struct {
	label   string // "x" — doubles as the bound name; "~x"-ness is flagged by labeled
	ty      *gty
	labeled bool
	unit    bool // the "()" Unit parameter
}

type fnDef struct {
	name   string
	params []paramDef
	ret    *gty
}

type fieldDef struct {
	name string // "" for positional variant fields
	ty   *gty
}

type ctorDef struct {
	name   string
	fields []fieldDef
	ret    *gty // the ADT this ctor builds
}

func (g *Gen) initTyped() {
	g.scope = [][]binding{{}}
	g.ctors = map[string][]ctorDef{}
	g.records = map[string][]fieldDef{}
	g.imports = map[string]bool{}
	g.bindTop(binding{name: "not", t: fnT(boolT, boolT), kind: bFn})
	g.fns = append(g.fns, fnDef{name: "not",
		params: []paramDef{{label: "b", ty: boolT}}, ret: boolT})
}

func (g *Gen) pushScope() { g.scope = append(g.scope, nil) }

func (g *Gen) popScope() { g.scope = g.scope[:len(g.scope)-1] }

func (g *Gen) bind(b binding) {
	i := len(g.scope) - 1
	g.scope[i] = append(g.scope[i], b)
}

func (g *Gen) bindTop(b binding) {
	g.scope[0] = append(g.scope[0], b)
}

// varsOf collects in-scope bindings of the wanted kinds, innermost
// frames first — callers pick with rng, so order stays deterministic.
func (g *Gen) varsOf(want *gty, kinds ...bindKind) []binding {
	var out []binding
	for i := len(g.scope) - 1; i >= 0; i-- {
		for _, b := range g.scope[i] {
			for _, k := range kinds {
				if b.kind != k {
					continue
				}
				if want == nil || b.t.eq(want) {
					out = append(out, b)
				}
			}
		}
	}
	return out
}

// scopeIdent is genIdent's typed-mode helper: any bound name works as
// "an identifier" (a fn or ctor name is still an expr).
func (g *Gen) scopeIdent() string {
	vars := g.varsOf(nil, bVar, bFn, bCtor)
	if len(vars) == 0 {
		return ""
	}
	return vars[g.rng.IntN(len(vars))].name
}

// scopeCtor is genUIdent's typed-mode helper.
func (g *Gen) scopeCtor() string {
	vars := g.varsOf(nil, bCtor)
	if len(vars) == 0 {
		return ""
	}
	return vars[g.rng.IntN(len(vars))].name
}

// fnOf finds a declared fn returning want — for application sites.
func (g *Gen) fnOf(want *gty) []fnDef {
	var out []fnDef
	for _, f := range g.fns {
		if f.ret.eq(want) {
			out = append(out, f)
		}
	}
	return out
}

// ctorOf is the constructor table for a scrutinee/result type:
// user ADTs, plus the builtin Option and Result ctors.
func (g *Gen) ctorOf(t *gty) []ctorDef {
	switch t.kind {
	case tADT:
		return g.ctors[t.name]
	case tOption:
		e := t.args[0]
		return []ctorDef{
			{name: "Some", fields: []fieldDef{{ty: e}}, ret: t},
			{name: "None", ret: t},
		}
	case tResult:
		return []ctorDef{
			{name: "Ok", fields: []fieldDef{{ty: t.args[0]}}, ret: t},
			{name: "Err", fields: []fieldDef{{ty: t.args[1]}}, ret: t},
		}
	}
	return nil
}

// ------------------------------------------------------------------
// type picking
// ------------------------------------------------------------------

// pickType chooses a type for signatures and lets. Composite types
// shrink with depth so signatures stay readable; declared ADTs and
// records join the palette once they exist.
func (g *Gen) pickType(d int) *gty {
	if d <= 0 {
		switch g.rng.IntN(5) {
		case 0, 1:
			return intT
		case 2:
			return boolT
		case 3:
			return stringT
		case 4:
			return floatT
		}
		return intT
	}
	if len(g.types) > 0 && g.rng.Float64() < 0.25 {
		name := g.types[g.rng.IntN(len(g.types))]
		if _, ok := g.records[name]; ok {
			return &gty{kind: tRecord, name: name}
		}
		return &gty{kind: tADT, name: name}
	}
	switch g.rng.IntN(14) {
	case 0, 1, 2, 3:
		return intT
	case 4:
		return floatT
	case 5, 6:
		return boolT
	case 7:
		return stringT
	case 8:
		return charT
	case 9:
		return unitT
	case 10:
		return listT(g.pickType(0))
	case 11:
		return optionT(g.pickType(d - 1))
	case 12:
		return resultT(g.pickType(0), g.pickType(0))
	default:
		return tupleT(g.pickType(0), g.pickType(0))
	}
}

// pickComparable picks a type with cheap structural equality — the
// operand type for ==/!=. Function and record types are skipped on
// purpose (E0409); declared ADTs join the palette once they exist.
func (g *Gen) pickComparable() *gty {
	if len(g.types) > 0 && g.rng.Float64() < 0.3 {
		adts := g.adtNames()
		if len(adts) > 0 {
			return &gty{kind: tADT, name: adts[g.rng.IntN(len(adts))]}
		}
	}
	switch g.rng.IntN(6) {
	case 0, 1, 2:
		return intT
	case 3:
		return floatT
	case 4:
		return boolT
	case 5:
		if g.rng.IntN(2) == 0 {
			return stringT
		}
		return listT(g.pickType(0))
	}
	return intT
}

// pickMatchableType picks a type worth matching on — a declared ADT
// if any exist, else a builtin-sum or list shape.
func (g *Gen) pickMatchableType() *gty {
	if len(g.types) > 0 && g.rng.Float64() < 0.5 {
		adts := g.adtNames()
		if len(adts) > 0 {
			return &gty{kind: tADT, name: adts[g.rng.IntN(len(adts))]}
		}
	}
	switch g.rng.IntN(4) {
	case 0:
		return optionT(g.pickType(0))
	case 1:
		return resultT(g.pickType(0), g.pickType(0))
	case 2:
		return listT(g.pickType(0))
	default:
		return boolT
	}
}

// ------------------------------------------------------------------
// typed declaration emitters — the expandTypedRef dispatch
// ------------------------------------------------------------------

// expandTypedRef reports whether name has a typed emitter and, if so,
// emits it. Rules not listed here walk mechanically even in Typed
// mode (sep machinery, attributes, package-decl).
func (g *Gen) expandTypedRef(name string) bool {
	switch name {
	case "fn-decl":
		g.typedFnDecl()
	case "let-decl":
		g.typedLetDecl()
	case "type-decl":
		g.typedTypeDecl()
	case "test-decl":
		g.typedTestDecl()
	case "import-decl":
		g.typedImportDecl()
	case "extern-decl":
		g.typedExternDecl()
	case "expr":
		// Reached inside string interpolation and any mechanical
		// spot that still asks for an expression under Typed mode.
		g.exprOf(g.pickType(0), 2)
	case "head-expr":
		g.headOf(g.pickType(0), 2)
	default:
		return false
	}
	return true
}

// freshName coins a binding name that does not collide with anything
// visible — typed mode reuses names for readability, but fresh ones
// keep generated programs from accidentally meaning something else.
var freshNames = []string{
	"a", "b", "c", "d", "e", "k", "p", "q", "r", "s", "t", "u", "w",
	"x", "y", "z", "x0", "y0", "n0", "s0", "tmp0", "acc0", "res0",
}

func (g *Gen) freshVar() string {
	if g.rng.Float64() < 0.6 {
		return identPool[g.rng.IntN(len(identPool))]
	}
	return freshNames[g.rng.IntN(len(freshNames))] + fmt.Sprint(g.rng.IntN(10))
}

func (g *Gen) freshFn() string {
	base := fnPool[g.rng.IntN(len(fnPool))]
	return fmt.Sprintf("%s%d", base, g.rng.IntN(100))
}

func (g *Gen) freshTypeName() string {
	return uidentPool[g.rng.IntN(len(uidentPool))] + fmt.Sprint(g.rng.IntN(100))
}

// emitFrag writes a preformatted fragment like "(x: Int)" — the
// typed emitters build small pieces textually and emit them raw;
// tokens that need perturbation/spacing still go through emit.
func (g *Gen) emitFrag(s string) { g.emitRaw(s) }

// typedFnDecl emits:
//
//	fn name (p0: T0) (~p1: T1) -> R = <expr aiming at R>
//
// The fn is registered BEFORE the body is emitted, so recursive and
// mutually-recursive-looking calls are in scope (real mutual
// recursion needs forward decls — out of scope here).
func (g *Gen) typedFnDecl() {
	name := g.freshFn()
	var params []paramDef
	switch g.rng.IntN(6) {
	case 0:
		params = []paramDef{{unit: true}}
	default:
		n := 1 + g.rng.IntN(2)
		for i := 0; i < n; i++ {
			p := paramDef{label: g.freshVar(), ty: g.pickType(1)}
			if g.rng.Float64() < 0.15 {
				p.labeled = true
			}
			params = append(params, p)
		}
	}
	ret := g.pickType(1)
	def := fnDef{name: name, params: params, ret: ret}
	var pts []*gty
	for _, p := range params {
		if !p.unit {
			pts = append(pts, p.ty)
		}
	}
	g.fns = append(g.fns, def)
	g.bindTop(binding{name: name, t: fnT(ret, pts...), kind: bFn})

	g.emit("fn")
	g.emit(name)
	for _, p := range params {
		switch {
		case p.unit:
			g.emit("()")
		case p.labeled:
			g.emitFrag(fmt.Sprintf("(~%s: %s)", p.label, p.ty.text()))
		default:
			g.emitFrag(fmt.Sprintf("(%s: %s)", p.label, p.ty.text()))
		}
	}
	g.emit("->")
	g.emitFrag(ret.text())
	g.emit("=")

	g.pushScope()
	for _, p := range params {
		if !p.unit {
			g.bind(binding{name: p.label, t: p.ty, kind: bVar})
		}
	}
	saveRet := g.curRet
	g.curRet = ret
	g.exprOf(ret, 3)
	g.curRet = saveRet
	g.popScope()
}

// typedLetDecl emits a top-level or block-level let:
//
//	let x = <e>          let (a, b) = <(e, e)>     let _ = <e>
func (g *Gen) typedLetDecl() {
	g.emit("let")
	switch g.rng.IntN(10) {
	case 0, 1, 2, 3, 4, 5:
		name := g.freshVar()
		t := g.pickType(1)
		g.emit(name)
		g.emit("=")
		g.exprOf(t, 2)
		g.bind(binding{name: name, t: t, kind: bVar})
	case 6:
		a, b := g.freshVar(), g.freshVar()
		ta, tb := g.pickType(0), g.pickType(0)
		g.emitFrag(fmt.Sprintf("(%s, %s)", a, b))
		g.emit("=")
		g.exprOf(tupleT(ta, tb), 2)
		g.bind(binding{name: a, t: ta, kind: bVar})
		g.bind(binding{name: b, t: tb, kind: bVar})
	case 7:
		g.emit("_")
		g.emit("=")
		g.exprOf(g.pickType(1), 2)
	case 8:
		// A record-pattern let when a record type exists —
		// "let {x = a, y = b} = rec" binds a/b to the field types.
		recs := g.recordNames()
		if len(recs) > 0 {
			rname := recs[g.rng.IntN(len(recs))]
			fields := g.records[rname]
			var binds []binding
			var parts []string
			for _, f := range fields {
				if g.rng.Float64() < 0.6 {
					v := g.freshVar()
					parts = append(parts, f.name+" = "+v)
					binds = append(binds, binding{name: v, t: f.ty, kind: bVar})
				}
			}
			if len(parts) == 0 {
				v := g.freshVar()
				parts = append(parts, fields[0].name+" = "+v)
				binds = append(binds, binding{name: v, t: fields[0].ty, kind: bVar})
			}
			g.emitFrag("{ " + strings.Join(parts, ", ") + " }")
			g.emit("=")
			g.exprOf(&gty{kind: tRecord, name: rname}, 2)
			for _, b := range binds {
				g.bind(b)
			}
			break
		}
		fallthrough
	default:
		name := g.freshVar()
		t := g.pickType(0)
		g.emit(name)
		g.emit("=")
		g.exprOf(t, 2)
		g.bind(binding{name: name, t: t, kind: bVar})
	}
}

// recordNames lists declared record type names in declaration order.
func (g *Gen) recordNames() []string {
	var out []string
	for _, n := range g.types {
		if _, ok := g.records[n]; ok {
			out = append(out, n)
		}
	}
	return out
}

// adtNames lists declared variant type names in declaration order.
func (g *Gen) adtNames() []string {
	var out []string
	for _, n := range g.types {
		if _, ok := g.records[n]; !ok {
			out = append(out, n)
		}
	}
	return out
}

// typedTypeDecl emits:
//
//	type Shape = | Circle Int | Rect (w: Float) (h: Float)
//	type Point = { x: Float, y: Float }
//
// registering ctors/fields before any use site can be generated.
func (g *Gen) typedTypeDecl() {
	name := g.freshTypeName()
	g.emit("type")
	g.emit(name)
	g.emit("=")
	if g.rng.Float64() < 0.6 {
		// Variant type: leading "|" is optional but every spec
		// example writes it.
		t := &gty{kind: tADT, name: name}
		nCtors := 1 + g.rng.IntN(3)
		var ctors []ctorDef
		g.emit("|")
		for i := 0; i < nCtors; i++ {
			if i > 0 {
				g.emit("|")
			}
			c := ctorDef{name: uidentPool[g.rng.IntN(len(uidentPool))] + fmt.Sprint(i), ret: t}
			g.emit(c.name)
			nf := g.rng.IntN(3)
			for j := 0; j < nf; j++ {
				ft := g.pickType(0)
				if g.rng.Float64() < 0.3 {
					label := g.freshVar()
					g.emitFrag(fmt.Sprintf("(%s: %s)", label, ft.text()))
					c.fields = append(c.fields, fieldDef{name: label, ty: ft})
				} else {
					g.emitFrag(ft.atomText())
					c.fields = append(c.fields, fieldDef{ty: ft})
				}
			}
			ctors = append(ctors, c)
			// The ctor's binding type is its signature: a nullary
			// ctor is a value of the ADT, a ctor with fields is a
			// function into it — so varsOf(ADT) picks up nullary
			// ctors only and varsOf(Fn...) picks up the rest.
			ct := t
			if len(c.fields) > 0 {
				var fts []*gty
				for _, f := range c.fields {
					fts = append(fts, f.ty)
				}
				ct = fnT(t, fts...)
			}
			g.bindTop(binding{name: c.name, t: ct, kind: bCtor})
		}
		g.ctors[name] = ctors
	} else {
		// Record type.
		var fields []fieldDef
		g.emit("{")
		nf := 1 + g.rng.IntN(3)
		for i := 0; i < nf; i++ {
			if i > 0 {
				g.emit(",")
			}
			f := fieldDef{name: g.freshVar(), ty: g.pickType(0)}
			g.emitFrag(fmt.Sprintf("%s: %s", f.name, f.ty.text()))
			fields = append(fields, f)
		}
		g.emit("}")
		g.records[name] = fields
	}
	g.types = append(g.types, name)
}

// typedTestDecl emits `test "desc" { <assertions and lets> }` —
// the spec's inline tests (they exercise TestSource codegen).
func (g *Gen) typedTestDecl() {
	g.emit("test")
	g.emitRaw(fmt.Sprintf("%q", "gen "+fmt.Sprint(g.rng.IntN(1000))))
	g.emit("{")
	g.pushScope()
	g.inTest++
	defer func() { g.inTest-- }()
	items := 1 + g.rng.IntN(3)
	for i := 0; i < items; i++ {
		if i > 0 {
			g.newline()
		}
		switch g.rng.IntN(4) {
		case 0:
			g.typedLetDecl()
		case 1:
			g.emit("assert")
			g.emit("(")
			g.exprOf(boolT, 1)
			g.emit(")")
		default:
			g.emit("assert_eq")
			t := g.pickType(0)
			g.atomOf(t, 1)
			g.atomOf(t, 1)
		}
	}
	g.popScope()
	g.newline()
	g.emit("}")
}

// typedImportDecl emits an import. `import go "pkg"` picks from a
// stdlib-ish allowlist so the compile canary's loader can resolve it;
// a bare `import "x"` stays rare — it exercises the E0203 user-diagnostic
// path instead.
func (g *Gen) typedImportDecl() {
	g.emit("import")
	if g.rng.Float64() < 0.85 {
		g.emit("go")
		pkg := goPkgs[g.rng.IntN(len(goPkgs))]
		g.emitRaw(fmt.Sprintf("%q", pkg))
		g.imports[pkg] = true
		g.bindTop(binding{name: pkg, kind: bModule})
	} else {
		g.emitRaw(fmt.Sprintf("%q", "gen"+fmt.Sprint(g.rng.IntN(100))))
	}
}

var goPkgs = []string{"fmt", "strconv", "strings", "math", "errors"}

// typedExternDecl emits a plausible extern block. The signatures are
// invented — downstream phases may reject them, which is fine
// canary input (user diagnostics).
func (g *Gen) typedExternDecl() {
	g.emit("extern")
	g.emit("go")
	g.emitRaw(fmt.Sprintf("%q", goPkgs[g.rng.IntN(len(goPkgs))]))
	g.emit("{")
	n := 1 + g.rng.IntN(2)
	for i := 0; i < n; i++ {
		if i > 0 {
			g.newline()
		}
		g.emit("fn")
		g.emit(uidentPool[g.rng.IntN(len(uidentPool))])
		m := 1 + g.rng.IntN(2)
		for j := 0; j < m; j++ {
			g.emitFrag(fmt.Sprintf("(%s: %s)", g.freshVar(), g.pickType(0).text()))
		}
		g.emit("->")
		g.emitFrag(g.pickType(0).text())
	}
	g.newline()
	g.emit("}")
}
