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
	tPoly   // a hole in a binding PATTERN — see ph; never rendered
)

// ph is the wildcard marker inside a binding's type PATTERN: it
// stands for a position the checker generalized, so a use site may
// instantiate it to anything. Patterns live only in binding.t —
// they are matched by matchT and never reach text() or a Witness.
var ph = &gty{kind: tPoly}

// matchT reports whether a use at want unifies with pat — structural
// equality, except a ph position (a variable a top-level let
// generalized) matches any type.
func matchT(pat, want *gty) bool {
	if pat == nil || want == nil {
		return pat == want
	}
	if pat.kind == tPoly {
		return true
	}
	if pat.kind != want.kind || pat.name != want.name || len(pat.args) != len(want.args) {
		return false
	}
	for i, a := range pat.args {
		if !matchT(a, want.args[i]) {
			return false
		}
	}
	return true
}

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
	name    string // field name: the record field / the `~name` label; "" positional
	ty      *gty
	labeled bool // declared `(~name: T)` — calls/patterns must write `~name:`
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
	g.topVal = map[string]bool{
		// The compiler-provided lowercase names: redeclaring one at
		// file scope either shadows a builtin (assert's lowering is
		// test-only) or mints E0204.
		"not": true, "todo": true, "assert": true, "assert_eq": true,
		// Reserved for the Deterministic epilogue's driver fns.
		"main": true, "answer": true,
	}
	g.topTy = map[string]bool{
		// Shadowing a builtin type constructor makes the constructs
		// that produce it unrepresentable (E0415) — dedup them out
		// of the generated namespace outright. "Go" is the reserved
		// qualifier of the Go namespace.
		"Unit": true, "Bool": true, "Int": true, "Int32": true, "Int64": true,
		"Float": true, "Char": true, "String": true, "GoError": true, "Go": true,
		"List": true, "Option": true, "Ref": true, "Array": true, "Ptr": true,
		"Chan": true, "RecvChan": true, "SendChan": true, "Task": true,
		"Result": true,
	}
	g.topCtor = map[string]bool{
		// The builtin Option/Result constructors share the ctor
		// namespace — a generated ctor spelling one redeclares it.
		"Some": true, "None": true, "Ok": true, "Err": true,
	}
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

// containsPh reports whether t still carries a ph hole — a
// generalized component a use site instantiates to a fresh meta.
func containsPh(t *gty) bool {
	if t == nil {
		return false
	}
	if t.kind == tPoly {
		return true
	}
	for _, a := range t.args {
		if containsPh(a) {
			return true
		}
	}
	return false
}

// varsOf collects in-scope bindings of the wanted kinds, innermost
// frames and latest declarations first. Same-block redeclares and
// nested-block shadows are both legal, so the checker resolves a
// name to its latest binding — seen[] drops the shadowed older
// copies, or the generator would emit a name meaning a different
// binding (a shadowing E0401).
//
// Under noTodo the position's expected type reaches the checker as
// a metavariable, so a binding whose pattern still carries a ph
// hole (`let xs = []` → xs : List ph) instantiates metas nothing
// pins — E0407. Such bindings are skipped under the flag; at a
// concrete expected type the position pins the hole itself.
func (g *Gen) varsOf(want *gty, kinds ...bindKind) []binding {
	var out []binding
	seen := map[string]bool{}
	for i := len(g.scope) - 1; i >= 0; i-- {
		frame := g.scope[i]
		for j := len(frame) - 1; j >= 0; j-- {
			b := frame[j]
			if seen[b.name] {
				continue
			}
			seen[b.name] = true
			for _, k := range kinds {
				if b.kind != k {
					continue
				}
				if g.noTodo > 0 && containsPh(b.t) {
					continue
				}
				if want == nil || matchT(b.t, want) {
					out = append(out, b)
				}
			}
		}
	}
	return out
}

// concreteVarsOf is varsOf minus bindings whose pattern type still
// carries a ph hole — for positions whose type is inferred from the
// value itself rather than constrained by an expected type: a for
// iterable (`for v in xs` leaves xs's element meta free → E0407)
// and a match scrutinee (the element pins only through pattern-var
// uses, which the arm bodies do not guarantee).
func (g *Gen) concreteVarsOf(want *gty, kinds ...bindKind) []binding {
	var out []binding
	for _, b := range g.varsOf(want, kinds...) {
		if !containsPh(b.t) {
			out = append(out, b)
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
		// The deterministic subset has no tests — the evaluator
		// runs test blocks but `gom build` drops them, so the
		// asymmetry would fabricate divergences. Substitute an
		// ordinary let; test-decl is unreachable in mechanical
		// mode (altWeight 0), so this only fires under Typed.
		if g.opts.Deterministic {
			g.typedLetDecl()
			return true
		}
		g.typedTestDecl()
	case "import-decl":
		g.typedImportDecl()
	case "extern-decl":
		// An extern item invents a Go signature the real package
		// need not provide — the generated call may fail to build
		// for user-level reasons, which is not a diff case.
		if g.opts.Deterministic {
			g.typedLetDecl()
			return true
		}
		g.typedExternDecl()
	case "expr":
		// Reached inside string interpolation and any mechanical
		// spot that still asks for an expression under Typed mode.
		// A splice is checked against nil — a `todo ()` here would
		// leave its meta unbound, so suppress it.
		g.noTodo++
		g.exprOf(g.pickType(0), 2)
		g.noTodo--
	case "head-expr":
		// Same nil-expectation situation as "expr" — a head can
		// still carry an unbound meta (a bare `None` head).
		g.noTodo++
		g.headOf(g.pickType(0), 2)
		g.noTodo--
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

// freshIn draws candidate names until one is absent from used, then
// records it — the file-scope dedup for top-level declarations. The
// pool is finite, so after enough misses it falls back to a numeric
// suffix keyed by the set size, which stays deterministic.
func (g *Gen) freshIn(used map[string]bool, draw func() string) string {
	for i := 0; i < 64; i++ {
		n := draw()
		if !used[n] {
			used[n] = true
			return n
		}
	}
	for i := len(used); ; i++ {
		n := draw() + fmt.Sprint(i)
		if !used[n] {
			used[n] = true
			return n
		}
	}
}

// bindTopName is typedLetDecl's top-level-name bookkeeping: records a
// file-scope value binding into topVal so a later decl cannot
// redeclare it.
func (g *Gen) bindTopName(name string) {
	g.topVal[name] = true
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
	name := g.freshIn(g.topVal, g.freshFn)
	var params []paramDef
	switch g.rng.IntN(6) {
	case 0:
		params = []paramDef{{unit: true}}
	default:
		n := 1 + g.rng.IntN(2)
		seen := map[string]bool{}
		for i := 0; i < n; i++ {
			p := paramDef{label: g.freshIn(seen, g.freshVar), ty: g.pickType(1)}
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

// letName picks a binding name for typedLetDecl: at file scope it is
// deduped against every declared value-level name (a redeclared top
// let is E0204); inside a block sibling shadowing is legal so the
// plain fresh draw is enough. seen dedups names within one pattern.
func (g *Gen) letName(seen map[string]bool) string {
	if len(g.scope) == 1 {
		return g.freshIn(g.topVal, g.freshVar)
	}
	if seen == nil {
		return g.freshVar() // nothing to dedup against
	}
	return g.freshIn(seen, g.freshVar)
}

// bindLet records a let's binding into the scope and, at file scope,
// into topVal.
func (g *Gen) bindLet(b binding) {
	g.bind(b)
	if len(g.scope) == 1 {
		g.bindTopName(b.name)
	}
}

// typedLetDecl emits a top-level or block-level let:
//
//	let x : T = <e>      let x = <e>      let (a, b) : (T, U) = <e>
//	let _ = <e>          let {x = a} : R = <e>      let x = <poly value>
//
// The annotation is the pin: `let x : T = e` hands e a concrete
// expected type, so e's inner metas all resolve. An unannotated
// let's expected type is a bare meta — and a block-level let never
// generalizes — so its value is emitted under noTodo (free-meta
// leaves would strand an unsolved meta — E0407, an accident, not a
// slip). Only a top-level unannotated let may take a still-meta
// syntactic value, which generalizes into a polymorphic binding
// recorded as a pattern (ph marks the quantified positions).
func (g *Gen) typedLetDecl() {
	g.emit("let")
	top := len(g.scope) == 1
	switch g.rng.IntN(12) {
	case 0, 1, 2, 3, 4:
		// Annotated — the value position is concrete.
		name := g.letName(nil)
		t := g.pickType(1)
		g.emitFrag(name + " : " + t.text())
		g.emit("=")
		g.exprOf(t, 2)
		g.bindLet(binding{name: name, t: t, kind: bVar})
	case 5, 6:
		// Unannotated — the value must pin its own metas.
		name := g.letName(nil)
		t := g.pickType(1)
		g.emit(name)
		g.emit("=")
		g.noTodo++
		g.exprOf(t, 2)
		g.noTodo--
		g.bindLet(binding{name: name, t: t, kind: bVar})
	case 7:
		// Generalizing value-let — `let xs = []` — file scope
		// only; a block let is monomorphic and the same metas
		// would strand (E0407).
		if top {
			name := g.letName(nil)
			g.emit(name)
			g.emit("=")
			pat := g.polyValOf(1)
			g.bindLet(binding{name: name, t: pat, kind: bVar})
			break
		}
		fallthrough
	case 8:
		seen := map[string]bool{}
		a, b := g.letName(seen), g.letName(seen)
		ta, tb := g.pickType(0), g.pickType(0)
		g.emitFrag(fmt.Sprintf("(%s, %s) : %s", a, b, tupleT(ta, tb).text()))
		g.emit("=")
		g.exprOf(tupleT(ta, tb), 2)
		g.bindLet(binding{name: a, t: ta, kind: bVar})
		g.bindLet(binding{name: b, t: tb, kind: bVar})
	case 9:
		g.emit("_")
		if g.rng.IntN(2) == 0 {
			t := g.pickType(1)
			g.emitFrag(": " + t.text())
			g.emit("=")
			g.exprOf(t, 2)
			break
		}
		g.emit("=")
		g.noTodo++
		g.exprOf(g.pickType(1), 2)
		g.noTodo--
	case 10:
		// A record-pattern let when a record type exists —
		// "let {x = a, y = b} : R = rec" binds a/b to the field
		// types; the annotation keeps the value position concrete.
		recs := g.recordNames()
		if len(recs) > 0 {
			rname := recs[g.rng.IntN(len(recs))]
			fields := g.records[rname]
			seen := map[string]bool{}
			var binds []binding
			var parts []string
			for _, f := range fields {
				if g.rng.Float64() < 0.6 {
					v := g.letName(seen)
					parts = append(parts, f.name+" = "+v)
					binds = append(binds, binding{name: v, t: f.ty, kind: bVar})
				}
			}
			if len(parts) == 0 {
				v := g.letName(seen)
				parts = append(parts, fields[0].name+" = "+v)
				binds = append(binds, binding{name: v, t: fields[0].ty, kind: bVar})
			}
			g.emitFrag("{ " + strings.Join(parts, ", ") + " }")
			g.emitFrag(": " + rname)
			g.emit("=")
			g.exprOf(&gty{kind: tRecord, name: rname}, 2)
			for _, b := range binds {
				g.bindLet(b)
			}
			break
		}
		fallthrough
	default:
		name := g.letName(nil)
		t := g.pickType(0)
		g.emit(name)
		g.emitFrag(": " + t.text())
		g.emit("=")
		g.exprOf(t, 2)
		g.bindLet(binding{name: name, t: t, kind: bVar})
	}
}

// polyValOf emits the value of a top-level unannotated let —
// `let x = <v>` — and returns the binding's type PATTERN. Only a
// top-level `let x = <value>` generalizes; a block let would
// strand the same metas as E0407. The checker generalizes only
// syntactic values (isValue); a generalized meta becomes a ph hole
// in the pattern — `let xs = []` binds xs : List ph. A concrete
// var is also safe: `let y = v` binds y at v's own type with no
// metas to strand.
func (g *Gen) polyValOf(d int) *gty {
	if vars := g.concreteVarsOf(nil, bVar); len(vars) > 0 && g.rng.Float64() < 0.15 {
		b := vars[g.rng.IntN(len(vars))]
		g.emit(b.name)
		return b.t
	}
	return g.polyValueOf(d)
}

// nullaryCtors lists in-scope constructors usable as bare values —
// a fielded ctor binds at a fnT signature, so `b.t.kind != tFn`
// selects exactly the constants.
func (g *Gen) nullaryCtors() []binding {
	var out []binding
	for _, b := range g.varsOf(nil, bCtor) {
		if b.t.kind != tFn {
			out = append(out, b)
		}
	}
	return out
}

// polyValueOf emits a syntactic value the checker can generalize
// (isValue): leaves are literals, `[]`, `None`, or a bare nullary
// ctor; composites wrap only values — `[v]`, `(a, b)`, `(Some v)`,
// `(Ok v)`, `(Err v)` — so the whole stays a value. A non-value
// anywhere (a bare var, an application) would stop generalization
// and strand every ph in the pattern as an unsolved meta — E0407.
// The returned pattern marks each meta with ph.
func (g *Gen) polyValueOf(d int) *gty {
	if d > 0 {
		switch g.rng.IntN(6) {
		case 0:
			g.emit("[")
			p := g.polyValueOf(d - 1)
			g.emit("]")
			return listT(p)
		case 1:
			g.emit("(")
			a := g.polyValueOf(d - 1)
			g.emit(",")
			b := g.polyValueOf(d - 1)
			g.emit(")")
			return tupleT(a, b)
		case 2:
			g.emit("(")
			g.emit("Some")
			p := g.polyValueOf(d - 1)
			g.emit(")")
			return optionT(p)
		case 3:
			g.emit("(")
			g.emit("Ok")
			p := g.polyValueOf(d - 1)
			g.emit(")")
			return resultT(p, ph)
		case 4:
			g.emit("(")
			g.emit("Err")
			p := g.polyValueOf(d - 1)
			g.emit(")")
			return resultT(ph, p)
		default:
			if cs := g.nullaryCtors(); len(cs) > 0 && g.rng.IntN(2) == 0 {
				c := cs[g.rng.IntN(len(cs))]
				g.emit(c.name)
				return c.t
			}
			g.emit("None")
			return optionT(ph)
		}
	}
	switch g.rng.IntN(9) {
	case 0:
		g.emit("[]")
		return listT(ph)
	case 1:
		g.emit("None")
		return optionT(ph)
	case 2:
		g.genInt()
		return intT
	case 3:
		g.genFloat()
		return floatT
	case 4:
		g.genString()
		return stringT
	case 5:
		if g.rng.IntN(2) == 0 {
			g.emit("true")
		} else {
			g.emit("false")
		}
		return boolT
	case 6:
		g.genChar()
		return charT
	case 7:
		if cs := g.nullaryCtors(); len(cs) > 0 {
			c := cs[g.rng.IntN(len(cs))]
			g.emit(c.name)
			return c.t
		}
		g.emit("()")
		return unitT
	default:
		g.emit("()")
		return unitT
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
	name := g.freshIn(g.topTy, g.freshTypeName)
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
			c := ctorDef{name: g.freshIn(g.topCtor, func() string {
				return uidentPool[g.rng.IntN(len(uidentPool))] + fmt.Sprint(i)
			}), ret: t}
			g.emit(c.name)
			nf := g.rng.IntN(3)
			labels := map[string]bool{}
			for j := 0; j < nf; j++ {
				ft := g.pickType(0)
				if g.rng.Float64() < 0.3 {
					label := g.freshIn(labels, g.freshVar)
					// `(~l: T)` is a LABELED field — `(l: T)`
					// without the ~ declares a positional field
					// that merely has a name, and `C ~l:` would
					// mint E0406 on it.
					g.emitFrag(fmt.Sprintf("(~%s: %s)", label, ft.text()))
					c.fields = append(c.fields, fieldDef{name: label, ty: ft, labeled: true})
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
		labels := map[string]bool{}
		for i := 0; i < nf; i++ {
			if i > 0 {
				g.emit(",")
			}
			f := fieldDef{name: g.freshIn(labels, g.freshVar), ty: g.pickType(0)}
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
			// assert_eq's operands share a free type variable —
			// a `todo ()` operand would leave it unbound (E0407),
			// so the args stay concrete.
			g.emit("assert_eq")
			t := g.pickType(0)
			g.noTodo++
			g.atomOf(t, 1)
			g.atomOf(t, 1)
			g.noTodo--
		}
	}
	g.popScope()
	g.newline()
	g.emit("}")
}

// typedImportDecl emits an import. `import go "pkg"` picks from a
// stdlib-ish allowlist so the compile canary's loader can resolve it;
// a bare `import "x"` stays rare — it exercises the E0203 user-diagnostic
// path (a deliberate slip) instead. Re-importing the same package
// would redeclare its module name, so imported paths are deduped.
func (g *Gen) typedImportDecl() {
	g.emit("import")
	if g.rng.Float64() < 0.85 {
		var free []string
		for _, p := range goPkgs {
			if !g.imports[p] && !g.topVal[p] {
				free = append(free, p)
			}
		}
		if len(free) == 0 {
			// Every allowlisted package is imported already — a
			// second "fmt" would mint E0204. Emit it anyway and
			// record the slip.
			g.slip()
			g.emit("go")
			g.emitRaw(fmt.Sprintf("%q", goPkgs[g.rng.IntN(len(goPkgs))]))
			return
		}
		g.emit("go")
		pkg := free[g.rng.IntN(len(free))]
		g.emitRaw(fmt.Sprintf("%q", pkg))
		g.imports[pkg] = true
		g.topVal[pkg] = true
		g.bindTop(binding{name: pkg, kind: bModule})
	} else {
		// The invented path cannot resolve — E0203 is the expected
		// diagnostic.
		g.slip()
		g.emitRaw(fmt.Sprintf("%q", "gen"+fmt.Sprint(g.rng.IntN(100))))
	}
}

var goPkgs = []string{"fmt", "strconv", "strings", "math", "errors"}

// typedExternDecl emits a plausible extern block. The signatures are
// invented — the named Go member may not exist, so a diagnostic is
// expected often enough that the program cannot carry the guarantee:
// mark the slip up front.
func (g *Gen) typedExternDecl() {
	g.slip()
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
