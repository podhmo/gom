package gomgen

// Expression-level typed emitters. Every function emits one
// grammatical unit — exprOf for a full expr, headOf for a head-expr
// (never a leading "{"), atomOf/appArgOf for application operands —
// so the surrounding grammar positions stay satisfied by
// construction: control expressions appear only where exprs are
// allowed, and operands stay atoms.
//
// The emitters aim at a wanted type but do not guarantee it:
// best-effort is the contract, semantic mistakes land as ordinary
// check/resolve diagnostics — which is exactly what the compile
// canary consumes anyway.

import (
	"fmt"
)

// ------------------------------------------------------------------
// exprOf — the workhorse
// ------------------------------------------------------------------

// exprOf emits one expr aiming at type want. d is the remaining
// nesting budget; at 0 only leaf forms are emitted.
func (g *Gen) exprOf(want *gty, d int) {
	if g.left <= 0 {
		d = 0
	}
	if d <= 0 {
		g.leafOf(want)
		return
	}
	// Weighted menu. Entries gated on availability (a var of the
	// right type, a matchable scrutinee, ...) contribute only when
	// their gate is satisfied.
	type choice struct {
		w int
		f func()
	}
	var menu []choice
	vars := g.varsOf(want, bVar, bCtor)
	fns := g.fnOf(want)
	if len(vars) > 0 {
		menu = append(menu, choice{6, func() {
			g.emit(vars[g.rng.IntN(len(vars))].name)
		}})
	}
	if len(fns) > 0 {
		menu = append(menu, choice{4, func() {
			g.appOf(fns[g.rng.IntN(len(fns))], d)
		}})
	}
	if g.isLiteralable(want) {
		menu = append(menu, choice{4, func() { g.leafOf(want) }})
	}
	menu = append(menu,
		choice{2, func() { g.ifOf(want, d) }},
		choice{2, func() { g.blockOf(want, d) }},
		choice{1, func() { g.matchOf(want, d) }},
		choice{1, func() { g.pipeOf(want, d) }},
	)
	if c := g.ctorChoiceFor(want); c != nil {
		menu = append(menu, choice{3, c})
	}
	if want.kind == tBool {
		menu = append(menu, choice{4, func() { g.boolOpOf(d) }})
	}
	if want.kind == tInt || want.kind == tFloat {
		menu = append(menu, choice{4, func() { g.arithOf(want, d) }})
	}
	if want.kind == tUnit {
		menu = append(menu,
			choice{3, func() { g.forOf(d) }},
			choice{1, func() { g.goExprOf(d) }},
		)
		if g.imports["fmt"] {
			menu = append(menu, choice{3, func() {
				g.emitFrag("fmt.Println")
				g.tupleCallOf([]*gty{g.pickType(0)}, d-1)
			}})
		}
	}
	if want.kind == tString && g.imports["strconv"] && g.rng.Float64() < 0.5 {
		menu = append(menu, choice{2, func() {
			g.emitFrag("strconv.Itoa")
			g.tupleCallOf([]*gty{intT}, d-1)
		}})
	}
	if g.curRet != nil && g.curRet.kind == tResult {
		menu = append(menu, choice{2, func() {
			g.emit("try")
			g.emit("(")
			g.exprOf(resultT(want, g.pickType(0)), d-1)
			g.emit(")")
		}})
	}
	if g.rng.Float64() < 0.06 {
		menu = append(menu, choice{1, func() {
			g.selectOf(want, d)
		}})
	}
	menu = append(menu, choice{1, func() { g.todoOf() }})

	total := 0
	for _, c := range menu {
		total += c.w
	}
	r := g.rng.IntN(total)
	for _, c := range menu {
		if r < c.w {
			c.f()
			return
		}
		r -= c.w
	}
}

// todoOf is the universal escape hatch: `todo ()` typechecks at any
// wanted type (it diverges).
func (g *Gen) todoOf() { g.emitFrag("todo ()") }

// isLiteralable reports whether want has a literal form.
func (g *Gen) isLiteralable(want *gty) bool {
	switch want.kind {
	case tInt, tFloat, tBool, tString, tChar, tUnit:
		return true
	}
	return false
}

// leafOf emits a value-level expr of type want: a literal, an
// in-scope var, an empty/atomic composite — or todo () when nothing
// else applies. Never recurses; always emits something.
func (g *Gen) leafOf(want *gty) {
	vars := g.varsOf(want, bVar, bCtor)
	if len(vars) > 0 && g.rng.Float64() < 0.6 {
		g.emit(vars[g.rng.IntN(len(vars))].name)
		return
	}
	switch want.kind {
	case tInt:
		g.genInt()
	case tFloat:
		g.genFloat()
	case tBool:
		if g.rng.IntN(2) == 0 {
			g.emit("true")
		} else {
			g.emit("false")
		}
	case tString:
		g.genString()
	case tChar:
		g.genChar()
	case tUnit:
		g.emit("()")
	case tList:
		if g.rng.Float64() < 0.5 {
			g.emit("[]")
		} else {
			g.emit("[")
			g.exprOf(want.args[0], 0)
			g.emit("]")
		}
	case tOption:
		if g.rng.IntN(2) == 0 {
			g.emit("None")
		} else {
			g.emit("Some")
			g.atomOf(want.args[0], 0)
		}
	case tResult:
		if g.rng.IntN(2) == 0 {
			g.emit("Ok")
			g.atomOf(want.args[0], 0)
		} else {
			g.emit("Err")
			g.atomOf(want.args[1], 0)
		}
	case tTuple:
		g.tupleLitOf(want)
	case tADT:
		ctors := g.ctorOf(want)
		var free []ctorDef
		for _, c := range ctors {
			if len(c.fields) == 0 {
				free = append(free, c)
			}
		}
		if len(free) > 0 {
			g.emit(free[g.rng.IntN(len(free))].name)
		} else {
			g.todoOf()
		}
	case tRecord:
		g.recordLitOf(want, 0)
	case tFn, tNamed:
		g.todoOf()
	default:
		panic("unreachable")
	}
}

// atomOf emits a simple-expr (atom + optional member access) aiming
// at want — safe inside application argument positions. Composite
// exprs get parenthesized, which is always an atom.
func (g *Gen) atomOf(want *gty, d int) {
	if d <= 0 || g.left <= 0 {
		g.atomLeafOf(want)
		return
	}
	vars := g.varsOf(want, bVar, bCtor)
	switch {
	case len(vars) > 0 && g.rng.Float64() < 0.5:
		v := vars[g.rng.IntN(len(vars))]
		g.emit(v.name)
		g.maybeMemberOf(v, want)
	case want.kind == tADT && g.rng.Float64() < 0.4:
		ctors := g.ctorOf(want)
		c := ctors[g.rng.IntN(len(ctors))]
		if len(c.fields) == 0 {
			g.emit(c.name)
			break
		}
		// A field-carrying ctor application "C a b" is not an
		// atom — bare in argument position it would read as extra
		// args of the enclosing call. The parens keep it one
		// operand.
		g.emit("(")
		save := g.headDepth
		g.headDepth = 0
		g.ctorAppOf(c, d)
		g.headDepth = save
		g.emit(")")
	case g.rng.Float64() < 0.35:
		g.parenOf(want, d-1)
	default:
		g.atomLeafOf(want)
	}
}

// parenOf emits "( expr )" — inside the parens the head-expr brace
// ban lifts, so the flag is cleared for the inner expression.
func (g *Gen) parenOf(want *gty, d int) {
	g.emit("(")
	save := g.headDepth
	g.headDepth = 0
	g.exprOf(want, d)
	g.headDepth = save
	g.emit(")")
}

// maybeMemberOf emits ".field" postfix access when v is a record
// whose field has the wanted type — "p.x" is how records usually
// appear in expressions.
func (g *Gen) maybeMemberOf(v binding, want *gty) {
	if v.t.kind != tRecord {
		return
	}
	var names []string
	for _, f := range g.records[v.t.name] {
		if f.ty.eq(want) {
			names = append(names, f.name)
		}
	}
	if len(names) > 0 && g.rng.Float64() < 0.7 {
		g.emitFrag("." + names[g.rng.IntN(len(names))])
	}
}

// atomLeafOf is atomOf's non-recursive fallback: anything emitted is
// already an atom (literal, name, parenthesized todo).
func (g *Gen) atomLeafOf(want *gty) {
	vars := g.varsOf(want, bVar, bCtor)
	if len(vars) > 0 && g.rng.Float64() < 0.5 {
		g.emit(vars[g.rng.IntN(len(vars))].name)
		return
	}
	switch want.kind {
	case tInt:
		g.genInt()
	case tFloat:
		g.genFloat()
	case tBool:
		if g.rng.IntN(2) == 0 {
			g.emit("true")
		} else {
			g.emit("false")
		}
	case tString:
		g.genString()
	case tChar:
		g.genChar()
	case tUnit:
		g.emit("()")
	case tList:
		g.emit("[]")
	case tOption:
		g.emit("None")
	case tResult:
		if g.rng.IntN(2) == 0 {
			g.emit("(")
			g.emit("Ok")
			g.atomOf(want.args[0], 0)
			g.emit(")")
		} else {
			g.emit("(")
			g.emit("Err")
			g.atomOf(want.args[1], 0)
			g.emit(")")
		}
	case tTuple:
		g.tupleLitOf(want)
	case tRecord:
		if g.headDepth > 0 {
			// A record literal starts with "{" — banned as a
			// head-level atom; parenthesizing makes it legal.
			g.parenOf(want, 0)
		} else {
			g.recordLitOf(want, 0)
		}
	case tADT:
		ctors := g.ctorOf(want)
		var free []ctorDef
		for _, c := range ctors {
			if len(c.fields) == 0 {
				free = append(free, c)
			}
		}
		if len(free) > 0 {
			g.emit(free[g.rng.IntN(len(free))].name)
		} else {
			g.emit("(")
			g.todoOf()
			g.emit(")")
		}
	case tFn, tNamed:
		g.emit("(")
		g.todoOf()
		g.emit(")")
	default:
		panic("unreachable")
	}
}

// tupleLitOf emits "(e1, e2, ...)".
func (g *Gen) tupleLitOf(want *gty) {
	g.emit("(")
	for i, e := range want.args {
		if i > 0 {
			g.emit(",")
		}
		g.exprOf(e, 1)
	}
	g.emit(")")
}

// recordLitOf emits "{ f = e, ... }" or a "{ r with f = e }" update
// when a record-typed var is in scope.
func (g *Gen) recordLitOf(want *gty, d int) {
	fields := g.records[want.name]
	vars := g.varsOf(want, bVar)
	if len(vars) > 0 && len(fields) > 0 && g.rng.Float64() < 0.25 {
		g.emit("{")
		g.emit(vars[g.rng.IntN(len(vars))].name)
		g.emit("with")
		f := fields[g.rng.IntN(len(fields))]
		g.emitFrag(f.name + " =")
		g.exprOf(f.ty, d)
		g.emit("}")
		return
	}
	g.emit("{")
	for i, f := range fields {
		if i > 0 {
			g.emit(",")
		}
		g.emitFrag(f.name + " =")
		g.exprOf(f.ty, d)
	}
	g.emit("}")
}

// ctorChoiceFor returns a menu fn emitting a constructor application
// for want — ADTs plus builtin Option/Result — or nil when want has
// no ctors.
func (g *Gen) ctorChoiceFor(want *gty) func() {
	ctors := g.ctorOf(want)
	if len(ctors) == 0 {
		return nil
	}
	return func() { g.ctorAppOf(ctors[g.rng.IntN(len(ctors))], 2) }
}

// ctorAppOf emits "C a b" / "C ~f: a" / bare "C". Field args are
// atoms so the application stays grammatical.
func (g *Gen) ctorAppOf(c ctorDef, d int) {
	g.emit(c.name)
	for _, f := range c.fields {
		if f.name != "" && g.rng.Float64() < 0.5 {
			g.emitFrag("~" + f.name + ":")
		}
		g.atomOf(f.ty, d-1)
	}
}

// appOf emits a full application of f: "f a b", "f ~l: a", or the
// "f ()" form for the unit parameter. Labeled params get ~-args.
func (g *Gen) appOf(f fnDef, d int) {
	g.emit(f.name)
	for _, p := range f.params {
		if p.unit {
			g.emit("()")
			continue
		}
		if p.labeled {
			g.emitFrag("~" + p.label + ":")
		}
		g.atomOf(p.ty, d-1)
	}
}

// tupleCallOf emits a Go-style tuple application " (a, b)" — used
// for `import go` member calls, which are tuple applications.
func (g *Gen) tupleCallOf(args []*gty, d int) {
	g.emit("(")
	for i, t := range args {
		if i > 0 {
			g.emit(",")
		}
		g.exprOf(t, d)
	}
	g.emit(")")
}

// ------------------------------------------------------------------
// control forms
// ------------------------------------------------------------------

// ifOf emits "if <bool-head> {want} else {want}" — occasionally
// dropping the else for a Unit-want context where it typechecks
// loosely (an else-less if has Unit type; emitting one for non-Unit
// want is a deliberate rare semantic slip → user diagnostic).
func (g *Gen) ifOf(want *gty, d int) {
	g.emit("if")
	g.headOf(boolT, d-1)
	g.blockOf(want, d-1)
	if want.kind == tUnit && g.rng.Float64() < 0.3 {
		return
	}
	if g.rng.Float64() < 0.04 {
		// else-less if on a non-Unit want: a semantic near-miss that
		// the checker sorts out (kept rare).
		return
	}
	g.emit("else")
	g.blockOf(want, d-1)
}

// blockOf emits "{ items... ; last }" producing want. Middle items
// are lets and Unit-typed expressions — non-Unit middles are a
// check error, so the emitter keeps them out.
func (g *Gen) blockOf(want *gty, d int) {
	g.pushScope()
	defer g.popScope()
	g.emit("{")
	n := g.rng.IntN(3)
	for i := 0; i < n; i++ {
		if g.rng.Float64() < 0.7 {
			g.typedLetDecl()
		} else {
			g.unitItemOf(d - 1)
		}
		g.newline()
	}
	g.exprOf(want, d-1)
	g.newline()
	g.emit("}")
}

// unitItemOf emits a block-item that produces Unit. assert/assert_eq
// are emitted only inside test blocks — lowering reports them as
// E0901 outside tests (an open semantic question, not a user
// diagnostic; docs/problems/2026-11-02-assert-outside-test.md).
func (g *Gen) unitItemOf(d int) {
	switch g.rng.IntN(6) {
	case 0, 1:
		g.forOf(d - 1)
	case 2:
		if g.imports["fmt"] {
			g.emitFrag("fmt.Println")
			g.tupleCallOf([]*gty{g.pickType(0)}, d-1)
			return
		}
		g.emit("()")
	case 3, 4:
		if g.inTest > 0 {
			if g.rng.IntN(2) == 0 {
				g.emit("assert")
				g.atomOf(boolT, 0)
			} else {
				g.emit("assert_eq")
				t := g.pickComparable()
				g.atomOf(t, 0)
				g.atomOf(t, 0)
			}
			return
		}
		g.emit("()")
	default:
		g.emit("()")
	}
}

// matchOf emits "match <head> { ctor-pat -> want ... }". The
// scrutinee type is picked first, then a scrutinee of that type,
// then one arm per constructor (+ occasional wildcard) so most
// generated matches are exhaustive — the occasional missing arm is a
// deliberate E0301-shaped slip.
func (g *Gen) matchOf(want *gty, d int) {
	s := g.pickMatchableType()
	g.emit("match")
	g.headOf(s, d-1)
	g.emit("{")
	var arms []func()
	// Every arm binds its pattern names into a fresh scope for the
	// arm body — wrap emitters in push/pop once, here.
	wrap := func(pat func()) func() {
		return func() {
			g.pushScope()
			pat()
			g.emit("->")
			g.exprOf(want, d-1)
			g.popScope()
		}
	}
	switch s.kind {
	case tADT, tOption, tResult:
		for _, c := range g.ctorOf(s) {
			c := c
			arms = append(arms, wrap(func() { g.patOfCtor(c) }))
		}
	case tList:
		e := s.args[0]
		arms = append(arms,
			wrap(func() { g.emit("[]") }),
			wrap(func() {
				h, r := g.freshVar(), g.freshVar()
				g.emitFrag(fmt.Sprintf("[%s, ..%s]", h, r))
				g.bind(binding{name: h, t: e, kind: bVar})
				g.bind(binding{name: r, t: s, kind: bVar})
			}))
	case tBool:
		arms = append(arms,
			wrap(func() { g.emit("true") }),
			wrap(func() { g.emit("false") }))
	default:
		arms = append(arms, wrap(func() { g.emit("_") }))
	}
	// Occasionally prepend a guarded ident-pattern arm —
	// "v if <bool> -> ..." — to exercise the guard form; the bound
	// v is the scrutinee itself.
	if g.rng.Float64() < 0.3 {
		g.pushScope()
		v := g.freshVar()
		g.emit(v)
		g.bind(binding{name: v, t: s, kind: bVar})
		g.emit("if")
		g.exprOf(boolT, d-1)
		g.emit("->")
		g.exprOf(want, d-1)
		g.popScope()
		g.newline()
	}
	for i, arm := range arms {
		if i > 0 {
			g.newline()
		}
		arm()
	}
	// The arms are kept exhaustive on purpose most of the time — a
	// missing ctor would surface as E0301, which is also fine
	// canary input, just not something to force here.
	g.newline()
	g.emit("}")
}

// patOfCtor emits a pattern for constructor c — "C x y" positionally
// or "C ~f: x" for labeled fields — binding field names into the
// CURRENT scope frame (matchOf pushes the arm frame before calling).
func (g *Gen) patOfCtor(c ctorDef) {
	g.emit(c.name)
	for _, f := range c.fields {
		g.patFieldOf(f)
	}
}

// patFieldOf emits one constructor-field pattern and binds the name
// it introduces into the current scope frame.
func (g *Gen) patFieldOf(f fieldDef) {
	switch {
	case f.name != "" && g.rng.Float64() < 0.4:
		g.emitFrag("~" + f.name + ":")
		fallthrough
	default:
		if g.rng.Float64() < 0.25 {
			g.emit("_")
			return
		}
		name := g.freshVar()
		g.emit(name)
		g.bind(binding{name: name, t: f.ty, kind: bVar})
	}
}

// headOf emits a head-expr-safe expression of type want — never a
// leading "{". Used for if/match/for heads. It marks headDepth so
// nested emitters know "{"-led atoms are banned here; nested
// delimiters (parens, brackets, braces) clear the mark for their
// contents, matching grammar section 5.
func (g *Gen) headOf(want *gty, d int) {
	g.headDepth++
	defer func() { g.headDepth-- }()
	vars := g.varsOf(want, bVar, bCtor)
	if len(vars) > 0 && g.rng.Float64() < 0.6 {
		g.emit(vars[g.rng.IntN(len(vars))].name)
		return
	}
	switch {
	case want.kind == tBool && g.rng.Float64() < 0.6:
		g.boolOpOf(0)
	case want.kind == tADT && g.rng.Float64() < 0.4:
		ctors := g.ctorOf(want)
		g.ctorAppOf(ctors[g.rng.IntN(len(ctors))], 0)
	case g.rng.Float64() < 0.3 && d > 0:
		g.parenOf(want, d-1)
	default:
		g.atomLeafOf(want)
	}
}

// operandOf emits an operator operand. Control expressions (if,
// match, blocks, lambdas, select, go) cannot be operands of a binary
// operator — grammar section 5 puts expr at pipe level and below
// only for non-control forms — so operands are always parenthesized:
// "(e)" is an atom and atoms are legal operands at every level.
// Mostly a concrete leaf: a metavar operand (todo ()) leaves the
// operator's type unresolved, which is exactly the E0407 "type
// annotation required" diagnostic — real user diagnostics are fine,
// but leaves keep the canary yield high.
func (g *Gen) operandOf(want *gty, d int) {
	if g.rng.Float64() < 0.75 {
		g.emit("(")
		g.atomLeafOf(want)
		g.emit(")")
		return
	}
	g.parenOf(want, d)
}

// boolOpOf emits a Bool-producing operator expr.
func (g *Gen) boolOpOf(d int) {
	switch g.rng.IntN(6) {
	case 0, 1:
		t := g.pickComparable()
		g.operandOf(t, d)
		g.emit("==")
		g.operandOf(t, d)
	case 2:
		t := g.pickComparable()
		g.operandOf(t, d)
		g.emit("!=")
		g.operandOf(t, d)
	case 3:
		t := intT
		if g.rng.IntN(2) == 0 {
			t = floatT
		}
		g.operandOf(t, d)
		switch g.rng.IntN(4) {
		case 0:
			g.emit("<")
		case 1:
			g.emit("<=")
		case 2:
			g.emit(">")
		default:
			g.emit(">=")
		}
		g.operandOf(t, d)
	case 4:
		g.operandOf(boolT, d)
		if g.rng.IntN(2) == 0 {
			g.emit("&&")
		} else {
			g.emit("||")
		}
		g.operandOf(boolT, d)
	default:
		g.emitFrag("not")
		g.atomOf(boolT, d)
	}
}

// arithOf emits an Int/Float-producing operator expr.
func (g *Gen) arithOf(want *gty, d int) {
	switch g.rng.IntN(5) {
	case 0:
		g.operandOf(want, d-1)
		g.emit("+")
		g.operandOf(want, d-1)
	case 1:
		g.operandOf(want, d-1)
		g.emit("-")
		g.operandOf(want, d-1)
	case 2:
		g.operandOf(want, d-1)
		g.emit("*")
		g.operandOf(want, d-1)
	case 3:
		if g.rng.Float64() < 0.5 {
			g.emit("-")
			g.atomOf(want, d-1)
		} else {
			g.operandOf(want, d-1)
			g.emit("/")
			g.operandOf(want, d-1)
		}
	default:
		g.operandOf(want, d-1)
		g.emit("%")
		g.operandOf(want, d-1)
	}
}

// pipeOf emits "(e) |> f" / "(fn v -> e) <| a" / "(e) |?> fn x -> ..."
// forms — operands are parenthesized because control expressions
// cannot be pipe operands either. "|?>" needs a Result on the left
// and produces one on the right.
func (g *Gen) pipeOf(want *gty, d int) {
	if want.kind == tResult && g.rng.Float64() < 0.5 {
		src := resultT(g.pickType(0), want.args[1])
		g.operandOf(src, d-1)
		g.emit("|?>")
		v := g.freshVar()
		g.emitFrag("fn " + v + " ->")
		g.pushScope()
		g.bind(binding{name: v, t: src.args[0], kind: bVar})
		g.exprOf(want, d-1)
		g.popScope()
		return
	}
	arg := g.pickType(0)
	if g.rng.IntN(3) == 0 {
		// Back-pipe: "f <| a" applies the
		// function on the LEFT, so the lambda goes there —
		// parenthesized, since control expressions cannot be
		// bare operator operands. The argument stays on the
		// right (pipe-rhs).
		g.emit("(")
		save := g.headDepth
		g.headDepth = 0
		v := g.freshVar()
		g.emitFrag("fn " + v + " ->")
		g.pushScope()
		g.bind(binding{name: v, t: arg, kind: bVar})
		g.exprOf(want, d-1)
		g.popScope()
		g.headDepth = save
		g.emit(")")
		g.emit("<|")
		g.operandOf(arg, d-1)
		return
	}
	g.operandOf(arg, d-1)
	g.emit("|>")
	// The right side: a lambda "fn x -> want" keeps types aligned.
	v := g.freshVar()
	g.emitFrag("fn " + v + " ->")
	g.pushScope()
	g.bind(binding{name: v, t: arg, kind: bVar})
	g.exprOf(want, d-1)
	g.popScope()
}

// forOf emits "for x in <List t> { <unit items> }" — a Unit
// expression.
func (g *Gen) forOf(d int) {
	e := g.pickType(0)
	v := g.freshVar()
	g.emit("for")
	g.emit(v)
	g.emit("in")
	g.headOf(listT(e), d-1)
	g.pushScope()
	g.bind(binding{name: v, t: e, kind: bVar})
	g.blockOf(unitT, d-1)
	g.popScope()
}

// goExprOf emits "go { <unit items> }" — spec 10.1. It produces a
// Task, so it is only ever emitted where a Unit-typed statement was
// asked for *and* it occasionally slips to other contexts as a
// near-miss (still fine canary input).
func (g *Gen) goExprOf(d int) {
	g.emit("go")
	g.blockOf(unitT, d-1)
}

// selectOf emits a minimal select: one default arm only, which is
// the shape that typechecks without channels.
func (g *Gen) selectOf(want *gty, d int) {
	g.emit("select")
	g.emit("{")
	g.emit("default")
	g.emit("->")
	g.exprOf(want, d-1)
	g.newline()
	g.emit("}")
}
