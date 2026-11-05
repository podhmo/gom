package lower

import (
	"fmt"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// seq accumulates the ordered Lets of one body under construction.
// Every complex operand is emitted here, in source evaluation order,
// before the expression that consumes it — this is the whole ANF
// discipline: l.expr returns atoms-or-complex-exprs, and only the
// caller decides whether the complex result lives at a Let.X, a
// Body.Tail, or is forced into a fresh temporary by asAtom.
type seq struct {
	lets []*core.Let
}

func (s *seq) emit(b *core.Bind, x core.Expr) {
	s.lets = append(s.lets, &core.Let{Sp: x.Span(), Name: b, X: x})
}

// exprBody lowers e as a complete Core body: its complex work lands
// in Lets in source order and its result-producing tail sits at Tail.
func (l *lowerer) exprBody(e check.Expr) *core.Body {
	s := &seq{}
	tail := l.expr(s, e)
	return &core.Body{Lets: s.lets, Tail: tail}
}

// stmt lowers a non-tail block statement: the value may be dropped
// but the evaluation must still run, so a complex result binds to a
// discard slot.
func (l *lowerer) stmt(s *seq, e check.Expr) {
	x := l.expr(s, e)
	if _, ok := x.(core.Atom); ok {
		return
	}
	s.emit(l.discardBind(e.Span(), x.Type()), x)
}

// atom lowers e to an operand — binding it to a fresh temporary when
// it is not already an atom.
func (l *lowerer) atom(s *seq, e check.Expr) core.Atom {
	return l.asAtom(s, e.Span(), l.expr(s, e))
}

func (l *lowerer) asAtom(s *seq, sp diag.Span, x core.Expr) core.Atom {
	if a, ok := x.(core.Atom); ok {
		return a
	}
	b := l.tmpBind(sp, x.Type())
	s.emit(b, x)
	return varOf(sp, b)
}

func baseOf(e check.Expr) core.Base {
	return core.Base{Sp: e.Span(), Typ: e.Type()}
}

func (l *lowerer) litUnit(sp diag.Span) *core.Lit {
	return &core.Lit{Base: core.Base{Sp: sp, Typ: types.Unit}, Kind: syntax.LitUnit, Text: "()"}
}

// expr is the expression dispatcher. The result is an Atom or a
// complex Expr — never two; callers in operand position force it
// through asAtom.
func (l *lowerer) expr(s *seq, e check.Expr) core.Expr {
	switch e := e.(type) {
	case *check.Lit:
		return &core.Lit{Base: baseOf(e), Kind: e.Kind, Text: e.Text}
	case *check.Var:
		return l.varExpr(e)
	case *check.Hole:
		l.err(e.Sp, "stray _ placeholder outside a cute Go call")
		return l.litUnit(e.Sp)
	case *check.MemberExpr:
		return l.memberExpr(s, e)
	case *check.Unary:
		return &core.Unary{Base: baseOf(e), Op: e.Op, X: l.atom(s, e.X)}
	case *check.Binary:
		return l.binaryExpr(s, e)
	case *check.StrInterp:
		return l.strInterp(s, e)
	case *check.Lambda:
		return l.lambdaExpr(e)
	case *check.Apply:
		return l.applyCall(s, e)
	case *check.If:
		var els *core.Body
		if e.Else != nil {
			els = l.exprBody(e.Else)
		}
		return &core.If{
			Base: baseOf(e),
			Cond: l.atom(s, e.Cond),
			Then: l.exprBody(e.Then),
			Else: els,
		}
	case *check.Match:
		return l.matchExpr(s, e)
	case *check.Block:
		return l.blockExpr(s, e)
	case *check.Let:
		v := l.expr(s, e.Value)
		l.letPat(s, e.Pat, v)
		return l.litUnit(e.Sp)
	case *check.Ignore:
		s.emit(l.discardBind(e.Sp, e.X.Type()), l.expr(s, e.X))
		return l.litUnit(e.Sp)
	case *check.Try:
		return l.tryExpr(s, e)
	case *check.GoExpr:
		return &core.Spawn{Base: baseOf(e), Body: l.exprBody(e.Body)}
	case *check.For:
		return l.forExpr(s, e)
	case *check.SelectExpr:
		return l.selectExpr(s, e)
	case *check.ListLit:
		return l.listLit(s, e)
	case *check.Spread:
		l.err(e.Sp, "stray ..spread outside a list or call argument")
		return l.litUnit(e.Sp)
	case *check.TupleLit:
		var elems []core.Atom
		for _, el := range e.Elems {
			elems = append(elems, l.atom(s, el))
		}
		return &core.TupleNew{Base: baseOf(e), Elems: elems}
	case *check.RecordLit:
		return l.recordLit(s, e)
	case *check.Ascribe:
		return l.ascribe(s, e)
	default:
		panic("unreachable")
	}
}

// blockExpr flattens a block into the enclosing sequence: statements
// emit ordered work, the last one yields the block's value.
func (l *lowerer) blockExpr(s *seq, e *check.Block) core.Expr {
	if len(e.Stmts) == 0 {
		return l.litUnit(e.Sp)
	}
	for _, st := range e.Stmts[:len(e.Stmts)-1] {
		l.stmt(s, st)
	}
	return l.expr(s, e.Stmts[len(e.Stmts)-1])
}

// --- references ---

// varRef lowers a KindVar reference. A symbol carrying a package
// (`os.Args`, an imported Gom `let` member) emits its qualified
// `pkg.Name` — package members are package-level values, no binder
// is needed; a package-less symbol is a local/sibling-package
// binding resolved by scope (File.Unit carries sibling lets).
func (l *lowerer) varRef(base core.Base, sym *resolve.Symbol) *core.Var {
	return &core.Var{Base: base, Sym: l.substSym(sym)}
}

// varExpr lowers a value-position reference. Substitution resolves
// pattern variables bound by match temporaries.
func (l *lowerer) varExpr(e *check.Var) core.Expr {
	sym := e.Sym
	switch sym.Kind {
	case resolve.KindVar:
		if sym.IsGo {
			return l.goVarRef(e.Span(), e.Type(), sym)
		}
		return l.varRef(baseOf(e), sym)
	case resolve.KindFunc:
		if sym.IsGo {
			return l.goFuncRefOrAdapter(e.Span(), e.Type(), sym, nil, e.TypeArgs)
		}
		return l.funcRefOrFlatten(e, sym)
	case resolve.KindCtor:
		if sig := sigOf(e.Type()); sig != nil {
			return &core.CtorRef{Base: baseOf(e), Sym: sym, TypeArgs: e.TypeArgs,
				Fields: l.ctorFields(sym, sig.Ret)}
		}
		// Nullary constructor in value position: a saturated
		// application of zero arguments.
		return &core.CtorApp{Base: baseOf(e), Ctor: sym, Arity: 0, TypeArgs: e.TypeArgs}
	case resolve.KindBuiltin:
		return l.builtinRef(e.Sp, e.Type(), sym)
	default:
		l.err(e.Sp, fmt.Sprintf("%s %s is not an expression", sym.Kind, sym.Name))
		return l.litUnit(e.Sp)
	}
}

// builtinRef lowers a builtin function name used as a value. A
// builtin is a function like a declared one — `let f = not` must
// denote `Bool -> Bool` — but it has no Go-level name to reference,
// so the value is an eta-expansion: a closure over the builtin call
// (`func(b bool) bool { return !b }`). The reference type supplies
// the parameter list.
func (l *lowerer) builtinRef(sp diag.Span, typ types.Type, sym *resolve.Symbol) core.Expr {
	if sig := sigOf(typ); sig != nil {
		return l.closureChainType(sp, sig, func(bs *seq, pv []core.Atom) core.Expr {
			return l.builtinCall(sp, sig.Ret, sym, pv)
		})
	}
	l.err(sp, fmt.Sprintf("builtin %s used as a non-function value", sym.Name))
	return &core.CallBuiltin{Base: core.Base{Sp: sp, Typ: typ}, Sym: sym}
}

// funcRefOrFlatten lowers a Gom function reference. The emitted
// declaration takes its declared arity — `func f_(a, b) R` — while
// a *value* of its type is the canonical one-func-per-arrow chain,
// so a bare FuncRef is legal only at declared arity 1 (`func f_(a)
// R'` already is the chain, whatever shape R' has). Any larger
// declared arity gets the eta adapter `func(p0) func(p1) ... {
// f_(p0, p1, ...) }` — the adapter's own arrows match the gom
// semantics (each supplies one argument; the declaration's chunk
// fires when its last parameter arrives). A reference whose symbol
// is a generalized value-let thunk is the call `x[T]()`.
func (l *lowerer) funcRefOrFlatten(e *check.Var, sym *resolve.Symbol) core.Expr {
	if l.thunks[sym] {
		// A generalized `let x = <value>` lowered to a niladic
		// generic declaration (P2-1): the reference is the value
		// itself, so it emits the call `x[T]()`.
		return &core.CallDirect{
			Base:   core.Base{Sp: e.Sp, Typ: e.Type()},
			Callee: sym, TypeArgs: e.TypeArgs,
			Arity: 0,
		}
	}
	decl := l.funcArity(sym, e.Type())
	flat := flatArity(e.Type())
	if decl == 1 {
		return &core.FuncRef{Base: baseOf(e), Sym: sym, Arity: decl, TypeArgs: e.TypeArgs}
	}
	if decl > flat {
		l.err(e.Sp, fmt.Sprintf("function %s has declared arity %d exceeding its type arity %d", sym.Name, decl, flat))
		return &core.FuncRef{Base: baseOf(e), Sym: sym, Arity: decl, TypeArgs: e.TypeArgs}
	}
	// Declared arity >= 2: the declaration's `func f_(p0, p1, ...)`
	// is the direct-call shape, not the value representation —
	// adapt through one arrow per parameter.
	sig := flatSig(e.Type())
	if sig == nil {
		l.err(e.Sp, fmt.Sprintf("function %s has non-function type %s", sym.Name, e.Type()))
		return l.litUnit(e.Sp)
	}
	return l.closureChainType(e.Sp, sig, func(bs *seq, pv []core.Atom) core.Expr {
		// Saturate the declared arity first; any flat-arity
		// remainder applies the returned function value. The call is
		// flagged Adapter: it is the firing end of a function *value*
		// escape, not a user-written call site — @tailrec's R3 reads
		// it the way it reads a FuncRef.
		prefix := &core.CallDirect{
			Base:   core.Base{Sp: e.Sp, Typ: retAfter(sig, decl)},
			Callee: sym, TypeArgs: e.TypeArgs,
			Args: pv[:decl], Arity: decl,
			Adapter: true,
		}
		rest := pv[decl:]
		if len(rest) == 0 {
			return prefix
		}
		fn := l.asAtom(bs, e.Sp, prefix)
		return l.callFuncChunks(bs, e.Sp, fn, rest)
	})
}

// memberExpr lowers `recv.sel` — field projection, module member, or
// Go bound-method selection.
func (l *lowerer) memberExpr(s *seq, e *check.MemberExpr) core.Expr {
	if e.Sym == nil {
		// Check defers selections on an unresolved receiver type to
		// finalize; a MemberExpr that still has no member here means
		// the deferred pass ran without diagnosing — an internal gap,
		// not a user error shape.
		l.err(e.Sp, "member selection with no resolved symbol")
		return l.litUnit(e.Sp)
	}
	switch e.Sym.Kind {
	case resolve.KindField:
		if e.GoFieldType != nil {
			raw := &core.FieldGet{Base: core.Base{Sp: e.Sp, Typ: e.GoFieldType}, Recv: l.atom(s, e.Recv), Field: e.Sym, Index: e.Index}
			// Go fields may mutate (and nil pointer reads may panic). Snapshot
			// the selector now, before later source operands execute.
			bind := l.tmpBind(e.Sp, e.GoFieldType)
			s.emit(bind, raw)
			return l.goPayloadConv(s, e.Sp, e.GoFieldType, e.Type(), varOf(e.Sp, bind))
		}
		idx := e.Index
		if idx < 0 {
			idx = l.fieldIndex(e.Recv.Type(), e.Sym)
			if idx < 0 {
				l.err(e.Sp, fmt.Sprintf("record field %s has no known index", e.Sym.Name))
				idx = 0
			}
		}
		return &core.FieldGet{
			Base:  baseOf(e),
			Recv:  l.atom(s, e.Recv),
			Field: e.Sym, Index: idx, Ctor: e.Ctor,
		}
	case resolve.KindFunc:
		if e.Sym.IsGo {
			return l.goFuncRefOrAdapter(e.Sp, e.Type(), e.Sym, nil, e.TypeArgs)
		}
		return l.funcRefOrFlatten(&check.Var{Base: check.Base{Sp: e.Sp, Typ: e.Type()}, Sym: e.Sym, TypeArgs: e.TypeArgs}, e.Sym)
	case resolve.KindVar:
		if e.Sym.IsGo {
			return l.goVarRef(e.Sp, e.Type(), e.Sym)
		}
		return l.varRef(baseOf(e), e.Sym)
	case resolve.KindCtor:
		if sig := sigOf(e.Type()); sig != nil {
			return &core.CtorRef{Base: baseOf(e), Sym: e.Sym, TypeArgs: e.TypeArgs,
				Fields: l.ctorFields(e.Sym, sig.Ret)}
		}
		return &core.CtorApp{Base: baseOf(e), Ctor: e.Sym, Arity: 0, TypeArgs: e.TypeArgs}
	case resolve.KindMethod:
		recv := l.atom(s, e.Recv)
		return l.goFuncRefOrAdapter(e.Sp, e.Type(), e.Sym, recv, e.TypeArgs)
	case resolve.KindBuiltin:
		// A function-valued builtin member lowers like a builtin
		// name in value position (an eta-expansion). `Go.conv`
		// never reaches here: check reports E0507 for it in
		// expression position.
		return l.builtinRef(e.Sp, e.Type(), e.Sym)
	default:
		l.err(e.Sp, fmt.Sprintf("member %s is not an expression", e.Sym.Kind))
		return l.litUnit(e.Sp)
	}
}

// goVarRef lowers a reference to a Go package variable or constant
// (`os.Args`, `math.Pi`): a bare qualified name. When the Go view of
// the member's type — the Env.GoSig hook's single ResTypes entry —
// differs from the Gom view (a typed `int` member like `os.O_RDONLY`
// reads as `Int` but emits `int`), the reference binds at the raw
// type inside an explicit Conv so generated code carries the
// conversion (`int64(os.O_RDONLY)`), per the boundary rules of spec
// section 8.1.5.
func (l *lowerer) goVarRef(sp diag.Span, typ types.Type, sym *resolve.Symbol) core.Expr {
	raw := typ
	if sig := l.goSig(sym, typ); len(sig.ResTypes) == 1 {
		raw = sig.ResTypes[0]
	}
	ref := &core.Var{Base: core.Base{Sp: sp, Typ: raw}, Sym: l.substSym(sym)}
	if types.Equal(raw, typ) {
		return ref
	}
	k, ok := l.goConvKindOf(raw, typ)
	if !ok {
		l.err(sp, fmt.Sprintf("Go member %s.%s has type %s, which cannot convert to %s — the boundary needs a value copy not yet supported",
			sym.Package, sym.Name, typeName(raw), typeName(typ)))
		return &core.Var{Base: core.Base{Sp: sp, Typ: typ}, Sym: l.substSym(sym)}
	}
	return &core.Conv{Base: core.Base{Sp: sp, Typ: typ}, Kind: k, X: ref}
}

// goFuncRefOrAdapter lowers a Go function or method in value
// position: a bare GoFuncRef exactly when the Go signature needs no
// adaptation (one parameter, one non-error result, no boundary
// conversion between the views), otherwise an adapter Closure whose
// body is a CallGo (P0-2, spec 8).
func (l *lowerer) goFuncRefOrAdapter(sp diag.Span, typ types.Type, sym *resolve.Symbol, recv core.Atom, targs []types.Type) core.Expr {
	sig := instantiateGoSig(l.goSig(sym, typ), targs)
	// The flat view: a method's member type can arrive as nested
	// arrows (`Int -> Int -> Int -> T`), and the adapter must bind
	// every parameter — sigOf would report only the outermost level.
	fs := flatSig(typ)
	if goBareRefOK(sig, fs) {
		return &core.GoFuncRef{
			Base: core.Base{Sp: sp, Typ: typ}, Sym: sym,
			Recv: recv, Arity: sig.Arity, Results: sig.Results,
			TypeArgs: targs,
		}
	}
	if fs == nil {
		l.err(sp, fmt.Sprintf("Go function %s has non-function type %s", sym.Name, typ))
		return l.litUnit(sp)
	}
	return l.closureChain(sp, typ, fs.Params, func(bs *seq, pv []core.Atom) core.Expr {
		call := &core.CallGo{
			Base:   core.Base{Sp: sp, Typ: l.goCallTyp(sig, fs.Ret)},
			Callee: sym, Recv: recv, TypeArgs: targs,
			Args:     l.convGoArgs(bs, sig, pv, false),
			Arity:    sig.Arity,
			Variadic: sig.Variadic,
			Results:  sig.Results,
		}
		x := l.wrapGoResult(bs, call, fs.Ret)
		if sig.Results == core.GoResultsOne {
			return l.goPayloadConv(bs, sp, call.Typ, fs.Ret, x)
		}
		return x
	})
}

// goBareRefOK reports whether a Go function reference can stand as
// the bare qualified name — one parameter, one result, no variadic,
// and no `int`/`int64`-style conversion between the Gom view of the
// signature and the real Go signature.
func goBareRefOK(sig GoSig, fs *types.Func) bool {
	if fs == nil || sig.Arity != 1 || sig.Results != core.GoResultsOne ||
		sig.Variadic || len(fs.Params) != 1 {
		return false
	}
	if len(sig.Params) > 0 && !types.Equal(sig.Params[0], fs.Params[0].Type) {
		return false
	}
	if len(sig.ResTypes) > 0 && !types.Equal(sig.ResTypes[0], fs.Ret) {
		return false
	}
	return true
}

// goCallTyp is the raw Go-side result type of a CallGo. ResTypes
// carries the callee's real Go signature when the import loader
// provided it — `strconv.Atoi` reports Go.Int there while the
// Gom-facing resTyp is Int (an `int`/`int64` gap the consumer
// converts). Without ResTypes the Gom view is decomposed per result
// shape — `(T, error)` reports T, `Result Unit GoError` reports
// Unit, a pair reports the tuple.
func (l *lowerer) goCallTyp(sig GoSig, resTyp types.Type) types.Type {
	raw := sig.ResTypes
	switch sig.Results {
	case core.GoResultsNone:
		return types.Unit
	case core.GoResultsErr:
		if len(raw) > 0 {
			return raw[0]
		}
		if a, _, ok := l.resultArgs(resTyp); ok {
			// A `Result (Option T) GoError` view still calls the
			// raw nil-able slot — the Option wraps it after the
			// boundary (ConvOptNil), so the call's raw type
			// unwraps it here.
			if c, isOpt := isCon(a, "", "Option"); isOpt && l.isOptionCon(c) && len(c.Args) == 1 {
				return c.Args[0]
			}
			return a
		}
	case core.GoResultsErrOnly:
		return types.Unit
	case core.GoResultsOK:
		if len(raw) > 0 {
			return raw[0]
		}
		if c, ok := isCon(resTyp, "", "Option"); ok && l.isOptionCon(c) && len(c.Args) == 1 {
			return c.Args[0]
		}
	case core.GoResultsNil:
		// A single `*T`/`error` result an extern or standard
		// binding declared `Option` for — the raw type is the
		// nil-able the Option's payload spells.
		if len(raw) == 1 {
			return raw[0]
		}
		if c, ok := isCon(resTyp, "", "Option"); ok && l.isOptionCon(c) && len(c.Args) == 1 {
			return c.Args[0]
		}
	case core.GoResultsErrValue:
		// A single `error` result a binding declared `GoError`
		// for — the raw type is the error the Gom view also spells.
		if len(raw) == 1 {
			return raw[0]
		}
		return resTyp
	case core.GoResultsPair:
		// TupleFromGo's operand reports the tuple itself — the raw
		// element pair when ResTypes is known.
		if len(raw) == 2 {
			return &types.Tuple{Elems: raw}
		}
	case core.GoResultsOne:
		if len(raw) == 1 {
			return raw[0]
		}
	default:
		panic("unreachable")
	}
	return resTyp
}

// wrapGoResult gives a CallGo its single mandatory consumer when it
// produces multiple Go values; a one-result call stands on its own.
// A zero-result call executes in the sequence and yields explicit Unit.
// resTyp is the Gom-facing type the consumer must produce. When the
// raw Go result (call.Typ) differs from the Gom-facing payload —
// `int` vs `int64` — the consumer records the conversion so codegen
// renders `result.FromGo[int64](int64(v), err)` and friends.
func (l *lowerer) wrapGoResult(s *seq, call *core.CallGo, resTyp types.Type) core.Expr {
	base := core.Base{Sp: call.Sp, Typ: resTyp}
	switch call.Results {
	case core.GoResultsNone:
		s.emit(l.discardBind(call.Sp, types.Unit), call)
		return l.litUnit(call.Sp)
	case core.GoResultsOne:
		return call
	case core.GoResultsErr, core.GoResultsErrOnly:
		var vc core.ConvKind
		var guard bool
		if a, _, ok := l.resultArgs(resTyp); !ok {
			l.err(call.Sp, fmt.Sprintf("Go (T, error) call typed %s, not a Result", resTyp))
		} else if call.Results == core.GoResultsErr {
			vc = l.goConvKind(call, call.Typ, a)
			// A `GoError` payload over an `error`-typed first
			// result is the never-nil claim — verify it at the
			// boundary (spec section 8.6).
			guard = l.isGoError(a) && l.isGoError(call.Typ)
		}
		return &core.ResultFromGo{Base: base, Call: call, ValConv: vc, NilGuard: guard}
	case core.GoResultsOK:
		if c, ok := isCon(resTyp, "", "Option"); ok && l.isOptionCon(c) && len(c.Args) == 1 {
			return &core.OptionFromGo{Base: base, Call: call,
				ValConv: l.goConvKind(call, call.Typ, c.Args[0])}
		}
		return &core.TupleFromGo{Base: base, Call: call,
			ElemConv: l.goElemConvs(call, resTyp)}
	case core.GoResultsNil:
		// `v := f(...)` where f returns *T or error and the
		// binding declared Option — OptionFromGo tests
		// `v != nil`. A non-Option resTyp contradicts the
		// recorded shape.
		if c, ok := isCon(resTyp, "", "Option"); ok && l.isOptionCon(c) && len(c.Args) == 1 {
			return &core.OptionFromGo{Base: base, Call: call,
				ValConv: l.goConvKind(call, call.Typ, c.Args[0])}
		}
		l.err(call.Sp, fmt.Sprintf("Go nil-able call typed %s, not an Option", resTyp))
		return call
	case core.GoResultsErrValue:
		// `v := f(...)` where the binding declared `-> GoError` —
		// ErrorFromGo emits the never-nil boundary check. A
		// non-GoError resTyp contradicts the recorded shape.
		if !l.isGoError(resTyp) {
			l.err(call.Sp, fmt.Sprintf("Go error-value call typed %s, not GoError", resTyp))
		}
		return &core.ErrorFromGo{Base: base, Call: call}
	case core.GoResultsPair:
		return &core.TupleFromGo{Base: base, Call: call,
			ElemConv: l.goElemConvs(call, resTyp)}
	default:
		panic("unreachable")
	}
}

// goElemConvs maps each tuple element to the conversion from the
// raw Go result element (call.Typ's tuple) to the Gom-facing one.
func (l *lowerer) goElemConvs(call *core.CallGo, resTyp types.Type) []core.ConvKind {
	tup, ok := types.Chase(resTyp).(*types.Tuple)
	if !ok {
		return nil
	}
	raw, _ := types.Chase(call.Typ).(*types.Tuple)
	out := make([]core.ConvKind, len(tup.Elems))
	for i, el := range tup.Elems {
		var rt types.Type = el
		if raw != nil && i < len(raw.Elems) {
			rt = raw.Elems[i]
		}
		out[i] = l.goConvKind(call, rt, el)
	}
	return out
}

// goConvKind is the conversion a Go result value needs to become the
// Gom-facing payload; the ConvKind zero value when the
// representations agree. A
// pair the boundary cannot convert is a contract-gap diagnostic —
// the checker unified the Gom views, so only the numeric and the
// boxing-any pairs are expected here.
func (l *lowerer) goConvKind(call *core.CallGo, from, to types.Type) core.ConvKind {
	k, ok := l.goConvKindOf(from, to)
	if !ok {
		l.err(call.Sp, fmt.Sprintf("Go result of type %s cannot convert to %s — the boundary needs a value copy not yet supported",
			typeName(from), typeName(to)))
	}
	return k
}

// goConvKindOf picks the conversion between a Go-side type view and
// the Gom-facing type, or reports the pair unsupported. Both
// directions pass through here: argument positions ask for the
// Gom-to-Go direction (a List argument feeding a Go.Slice
// parameter), result positions the Go-to-Gom one (a []T result
// becoming a List). Slice pairs need the element types to agree —
// an any-element Go slice accepts anything through boxing.
func (l *lowerer) goConvKindOf(from, to types.Type) (core.ConvKind, bool) {
	from = types.Chase(from)
	to = types.Chase(to)
	if types.Equal(from, to) {
		return 0, true
	}
	if isGoAny(to) {
		return core.ConvBoxAny, true
	}
	if l.isNumericCon(from) && l.isNumericCon(to) {
		return core.ConvNum, true
	}
	if tc, tok := to.(*types.Con); tok && l.isOptionCon(tc) && len(tc.Args) == 1 &&
		types.Equal(from, tc.Args[0]) {
		// A nil-able Go result value (`*T` or `error`) reaching an
		// `Option T` position — the derived `(error, error)` view's
		// payload read and the declared `Result (Option T)
		// GoError` view share it: `option.FromGo(v, v != nil)`.
		return core.ConvOptNil, true
	}
	fc, fok := from.(*types.Con)
	tc, tok := to.(*types.Con)
	if !fok || !tok || len(fc.Args) == 0 || len(tc.Args) == 0 {
		return 0, false
	}
	toSlice := tc.Qual == "Go" && tc.Name == "Slice" && len(tc.Args) == 1
	fromSlice := fc.Qual == "Go" && fc.Name == "Slice" && len(fc.Args) == 1
	toList := tc.Qual == "" && tc.Name == "List" && len(tc.Args) == 1 && !l.declaredT(tc)
	fromList := fc.Qual == "" && fc.Name == "List" && len(fc.Args) == 1 && !l.declaredT(fc)
	switch {
	case toSlice && fromList && types.Equal(fc.Args[0], tc.Args[0]):
		// A Gom list feeding a Go slice — copy in (`.ToSlice()`).
		return core.ConvSliceToGo, true
	case toSlice && (fromList || fromSlice) &&
		isGoAny(tc.Args[0]) && !isGoAny(fc.Args[0]):
		// A slice or list feeding a Go `[]any` — copy and box each
		// element (`rt.BoxSlice`), the spread `..xs` needs.
		return core.ConvSliceBox, true
	case toList && fromSlice && types.Equal(fc.Args[0], tc.Args[0]):
		// A Go slice reaching a List position — copy out
		// (`list.FromSlice`).
		return core.ConvSliceFromGo, true
	case l.chanDirPair(from, to):
		// A `Chan a` feeding a directional channel — the implicit
		// narrowing of spec section 10.2.
		return core.ConvChanDir, true
	}
	return 0, false
}

// isGoAny reports whether t is Go's empty interface — the catch-all
// a Go-side `any` parameter or result carries.
func isGoAny(t types.Type) bool {
	c, ok := types.Chase(t).(*types.Con)
	return ok && c.Qual == "Go" && c.Name == "Any"
}

// convGoArgs wraps arguments whose lowered value type differs from
// the Go parameter's raw type — an `Int` (`int64`) value reaching a
// Go `int` parameter — in explicit Convs bound to temporaries.
// sig.Params is the raw Go parameter list; nil means the Gom view
// already matches and every argument passes through.
func (l *lowerer) convGoArgs(s *seq, sig GoSig, args []core.Atom, spread bool) []core.Atom {
	if len(sig.Params) == 0 {
		return args
	}
	out := make([]core.Atom, len(args))
	for i, a := range args {
		out[i] = l.convGoArg(s, sig, i, a, spread && i == len(args)-1)
	}
	return out
}

// convGoArg is convGoArgs for one argument position: a value whose
// lowered type differs from the Go parameter's raw type is wrapped
// in an explicit Conv bound to a temporary — in the sequence s, so a
// conversion emitted outside a closure runs once at creation while
// one emitted inside the body runs per call.
func (l *lowerer) convGoArg(s *seq, sig GoSig, i int, a core.Atom, spreadArg bool) core.Atom {
	if a == nil {
		return nil
	}
	pt := l.goParamTypeAt(sig, i, spreadArg)
	at := types.Chase(a.Type())
	if pt == nil || types.Equal(at, pt) {
		return a
	}
	k, ok := l.goConvKindOf(at, pt)
	if !ok {
		l.err(a.Span(), fmt.Sprintf("Go call argument of type %s cannot convert to parameter type %s — the boundary needs a value copy not yet supported",
			typeName(a.Type()), typeName(pt)))
		return a
	}
	return l.asAtom(s, a.Span(), &core.Conv{
		Base: core.Base{Sp: a.Span(), Typ: pt}, Kind: k, X: l.convBoxSrc(s, a, k),
	})
}

// convBoxSrc materializes a numeric atom before a ConvBoxAny: a bare
// `any(1)` would box Go's default `int`, not the `int64` Gom's `Int`
// renders as — `any` supplies no concrete numeric context (the same
// gap the interface-ascription path already closes, spec section
// 8.3). The inner conversion emits `int64(x)` so the boxed dynamic
// type is the Gom type's Go representation.
func (l *lowerer) convBoxSrc(s *seq, a core.Atom, k core.ConvKind) core.Atom {
	if k == core.ConvBoxAny && l.isNumericCon(a.Type()) {
		return l.asAtom(s, a.Span(), &core.Conv{
			Base: core.Base{Sp: a.Span(), Typ: a.Type()}, Kind: core.ConvNum, X: a,
		})
	}
	return a
}

// goParamTypeAt is the raw Go parameter type the argument at index i
// feeds. Arguments at or beyond the last parameter of a variadic
// function feed the variadic element — except a `..xs` spread, which
// feeds the variadic slice itself.
func (l *lowerer) goParamTypeAt(sig GoSig, i int, spreadArg bool) types.Type {
	if sig.Arity == 0 || len(sig.Params) < sig.Arity {
		return nil
	}
	j := i
	if j > sig.Arity-1 {
		j = sig.Arity - 1
	}
	pt := types.Chase(sig.Params[j])
	if sig.Variadic && j == sig.Arity-1 && !spreadArg {
		if c, ok := pt.(*types.Con); ok && len(c.Args) == 1 &&
			((c.Qual == "Go" && c.Name == "Slice") ||
				(c.Qual == "" && c.Name == "List" && !l.declaredT(c))) {
			return c.Args[0]
		}
		return nil
	}
	return pt
}

// goPayloadConv wraps a raw Go-produced value in a Conv when its raw
// type differs from the Gom-facing want — the `try` payload of
// strconv.Atoi is `int` where Gom expects `Int` (`int64`). The
// conversion binds through a temporary, keeping the raw value an
// atom.
func (l *lowerer) goPayloadConv(s *seq, sp diag.Span, raw, want types.Type, x core.Expr) core.Expr {
	if types.Equal(raw, want) {
		return x
	}
	k, ok := l.goConvKindOf(raw, want)
	if !ok {
		l.err(sp, fmt.Sprintf("Go result of type %s cannot convert to %s — the boundary needs a value copy not yet supported",
			typeName(raw), typeName(want)))
		return x
	}
	a := l.asAtom(s, sp, x)
	return &core.Conv{Base: core.Base{Sp: sp, Typ: want}, Kind: k, X: l.convBoxSrc(s, a, k)}
}

// convAtomTo is goPayloadConv for a value already bound to an atom —
// the escape-produced `in` of |?> and the Ok payload of escapeStep.
func (l *lowerer) convAtomTo(s *seq, sp diag.Span, a core.Atom, want types.Type) core.Atom {
	if types.Equal(a.Type(), want) {
		return a
	}
	k, ok := l.goConvKindOf(a.Type(), want)
	if !ok {
		l.err(sp, fmt.Sprintf("Go result of type %s cannot convert to %s — the boundary needs a value copy not yet supported",
			typeName(a.Type()), typeName(want)))
		return a
	}
	return l.asAtom(s, sp, &core.Conv{
		Base: core.Base{Sp: sp, Typ: want}, Kind: k, X: l.convBoxSrc(s, a, k),
	})
}

// --- calls ---

// applyCall lowers a typed application per its CallInfo — the one
// place the call contract is honoured.
func (l *lowerer) applyCall(s *seq, e *check.Apply) core.Expr {
	ci := e.Call
	recv := l.methodRecv(s, e.Fn, ci)
	// The callee expression evaluates first — but only when the call
	// shape actually consumes a function value; a statically known
	// callee is a pure ref (emitted work is nil).
	fnExpr := l.calleeExpr(s, e.Fn, ci, recv)
	args := l.argAtoms(s, e.Args)
	return l.finishCall(s, e.Sp, e.Type(), e.Fn, fnExpr, recv, ci, args, hasSpread(e.Args))
}

// methodRecv extracts a method call's receiver — evaluated first,
// it is the callee position's effect. Nil for every other callee
// shape, and for a method callee that did not arrive as a MemberExpr
// (calleeExpr then takes the error path).
func (l *lowerer) methodRecv(s *seq, fn check.Expr, ci check.CallInfo) core.Atom {
	if ci.Callee != nil && ci.Callee.Kind == resolve.KindMethod {
		if m, ok := fn.(*check.MemberExpr); ok {
			return l.atom(s, m.Recv)
		}
	}
	return nil
}

// calleeExpr lowers a call's callee expression only where the call
// kind consumes the result: CallUnknown — the callee *is* a
// function-typed value — and a method callee that did not arrive as
// a MemberExpr, so no receiver was extracted (the value path reports
// the shape it cannot handle). Every other kind names a statically
// known callee — a Var or package-member MemberExpr — which is a
// pure reference carrying no evaluation; lowering it would push
// symbols that are not values (KindBuiltin) through varExpr and
// report them as misplaced values.
func (l *lowerer) calleeExpr(s *seq, fn check.Expr, ci check.CallInfo, recv core.Atom) core.Expr {
	if recv != nil {
		return nil
	}
	if ci.Kind == check.CallUnknown {
		// The callee evaluates before the arguments (spec section
		// 2 rule 4 — left-to-right; eval's evalApply agrees): a
		// complex callee must be bound here, ahead of argAtoms'
		// Lets — deferring the bind to unknownCall would emit it
		// after the arguments it must precede. Atoms pass through
		// asAtom unchanged, so a Var or FieldGet callee stays
		// unbound as before.
		return l.asAtom(s, fn.Span(), l.expr(s, fn))
	}
	if ci.Callee != nil && ci.Callee.Kind == resolve.KindMethod {
		return l.expr(s, fn)
	}
	return nil
}

// argAtoms lowers call arguments in source order. Holes and the
// spread marker contribute no atom of their own — the spread flag is
// recovered on the CallGo.
func (l *lowerer) argAtoms(s *seq, args []check.Arg) []core.Atom {
	out := make([]core.Atom, len(args))
	for i, a := range args {
		// Partial probes stay as if-assertions: check.Expr is a
		// sumtype and the checker requires exhaustive switches.
		if _, ok := a.Value.(*check.Hole); ok {
			continue
		}
		if v, ok := a.Value.(*check.Spread); ok {
			out[i] = l.atom(s, v.X)
			continue
		}
		out[i] = l.atom(s, a.Value)
	}
	return out
}

// finishCall dispatches on the checked call kind. fnExpr is the
// already-lowered callee expression (nil for method calls whose
// receiver was extracted); it supplies the function value for
// CallUnknown and is otherwise discarded — it was pure.
func (l *lowerer) finishCall(s *seq, sp diag.Span, resTyp types.Type, callee check.Expr, fnExpr core.Expr, recv core.Atom, ci check.CallInfo, args []core.Atom, spread bool) core.Expr {
	switch ci.Kind {
	case check.CallDirect:
		return l.directCall(s, sp, resTyp, callee, ci, args, spread)
	case check.CallPartial:
		return l.partialCall(s, sp, resTyp, callee, ci, args, spread)
	case check.CallUnknown:
		return l.unknownCall(s, sp, resTyp, callee, fnExpr, ci, args)
	case check.CallGo:
		return l.goCall(s, sp, resTyp, ci, args, recv, spread)
	default:
		panic("unreachable")
	}
}

// declOrderArgs reorders source-order argument atoms into
// declaration order per ArgToParam. The result has room for at least
// minLen entries — positions no argument filled stay nil — plus any
// over-application extras, which keep source order. Holes leave nil
// slots. CallDirect and CallPartial pass the declared arity: their
// call contract saturates every parameter slot. The Go call sites
// pass 0: CallGo.Args are exactly the supplied operands, and a
// variadic callee may legitimately be given fewer than its arity (a
// zero-argument `fmt.Print()` is legal) — padding to Arity would
// plant nil atoms the Core contract rejects.
func declOrderArgs(ci check.CallInfo, args []core.Atom, minLen int) []core.Atom {
	n := len(args)
	if minLen > n {
		n = minLen
	}
	out := make([]core.Atom, n)
	for i, a := range args {
		pos := i
		if i < len(ci.ArgToParam) {
			pos = ci.ArgToParam[i]
		}
		if pos >= 0 && pos < len(out) {
			out[pos] = a
		}
	}
	return out
}

// filledParams is the set of parameter indexes already supplied by
// the written arguments.
func filledParams(ci check.CallInfo, args []core.Atom) map[int]bool {
	out := map[int]bool{}
	for i := range args {
		pos := i
		if i < len(ci.ArgToParam) {
			pos = ci.ArgToParam[i]
		}
		out[pos] = true
	}
	return out
}

func (l *lowerer) directCall(s *seq, sp diag.Span, resTyp types.Type, callee check.Expr, ci check.CallInfo, args []core.Atom, spread bool) core.Expr {
	sym := ci.Callee
	if sym == nil {
		l.err(sp, "CallDirect with no callee")
		return l.litUnit(sp)
	}
	ordered := declOrderArgs(ci, args, ci.Arity)
	switch sym.Kind {
	case resolve.KindFunc:
		if sym.IsGo {
			l.err(sp, fmt.Sprintf("Go function %s classified CallDirect", sym.Name))
			return l.goCall(s, sp, resTyp, ci, args, nil, spread)
		}
		if ci.Applied <= ci.Arity {
			return &core.CallDirect{
				Base:   core.Base{Sp: sp, Typ: resTyp},
				Callee: sym, TypeArgs: ci.TypeArgs,
				Args: ordered[:ci.Arity], Arity: ci.Arity,
			}
		}
		// Over-application: saturate the declared arity, then apply
		// the returned function value to the extras one argument per
		// CallFunc, like CallUnknown.
		first := &core.CallDirect{
			Base:   core.Base{Sp: sp, Typ: overAppRetType(callee, ci)},
			Callee: sym, TypeArgs: ci.TypeArgs,
			Args: ordered[:ci.Arity], Arity: ci.Arity,
		}
		t := l.asAtom(s, sp, first)
		return l.callFuncChunks(s, sp, t, ordered[ci.Arity:])
	case resolve.KindCtor:
		if ci.Applied != ci.Arity {
			l.err(sp, fmt.Sprintf("constructor %s application is not saturated (%d/%d)", sym.Name, ci.Applied, ci.Arity))
		}
		return &core.CtorApp{
			Base: core.Base{Sp: sp, Typ: resTyp},
			Ctor: sym, TypeArgs: ci.TypeArgs,
			Args: ordered[:ci.Arity], Arity: ci.Arity,
			Fields: l.ctorFields(sym, resTyp),
		}
	case resolve.KindBuiltin:
		var flat []core.Atom
		for _, a := range ordered {
			if a != nil {
				flat = append(flat, a)
			}
		}
		return l.builtinCall(sp, resTyp, sym, flat)
	default:
		l.err(sp, fmt.Sprintf("CallDirect callee %s has kind %s", sym.Name, sym.Kind))
		return l.litUnit(sp)
	}
}

// builtinCall builds the CallBuiltin for a user-visible builtin
// call. assert and assert_eq emit `rt.Assert*(t, ...)` — `t` is the
// *testing.T parameter only a generated test function binds — so a
// call outside a test block cannot be emitted. Whether assertions
// in ordinary code are a user diagnostic is an open semantic
// question (docs/problems/2026-11-02-assert-outside-test.md); until
// check settles it, lowering reports the shape rather than produce
// uncompilable Go.
func (l *lowerer) builtinCall(sp diag.Span, typ types.Type, sym *resolve.Symbol, args []core.Atom) core.Expr {
	if !l.inTest && (sym.Name == "assert" || sym.Name == "assert_eq") {
		l.err(sp, fmt.Sprintf("builtin %s may only be called inside a test block", sym.Name))
	}
	cb := &core.CallBuiltin{
		Base: core.Base{Sp: sp, Typ: typ},
		Sym:  sym, Args: args,
	}
	// assert_eq carries the operands' comparison recipe — the same
	// judgment `x == y` applies, rendered by codegen as the
	// pre-computed operand of rt.AssertEq — and the operand type's
	// display recipe, rendered as the reported values
	// (docs/design.md section 4.8).
	if sym.Name == "assert_eq" && len(args) == 2 {
		cb.Eq = l.eqPlan(sp, args[0].Type())
		cb.Show = l.showPlan(sp, args[0].Type())
	}
	return cb
}

// overAppRetType is the function type the saturated prefix of an
// over-applied call returns — the callee's remaining arrow chain.
// The flat view sees through a nested declared Ret: `add3 (a) (b)
// -> (Int -> Int)` reports `Int -> Int`, not the whole signature.
func overAppRetType(callee check.Expr, ci check.CallInfo) types.Type {
	sig := flatSig(callee.Type())
	if sig == nil || len(sig.Params) <= ci.Arity {
		return callee.Type()
	}
	return &types.Func{Params: sig.Params[ci.Arity:], Ret: sig.Ret}
}

// partialCall lowers an under-applied known call to an explicit
// Closure over the saturated call — never a half-filled call node.
func (l *lowerer) partialCall(s *seq, sp diag.Span, resTyp types.Type, callee check.Expr, ci check.CallInfo, args []core.Atom, spread bool) core.Expr {
	sym := ci.Callee
	if sym == nil {
		l.err(sp, "CallPartial with no callee")
		return l.litUnit(sp)
	}
	if sym.IsGo || sym.Kind == resolve.KindMethod {
		return l.goCall(s, sp, resTyp, ci, args, nil, spread)
	}
	sig := sigOf(resTyp)
	if sig == nil {
		l.err(sp, fmt.Sprintf("partial application typed %s, not a function", resTyp))
		return l.litUnit(sp)
	}
	calleeSig := sigOf(callee.Type())
	filled := filledParams(ci, args)
	var unapplied []int
	for i := 0; i < ci.Arity; i++ {
		if !filled[i] {
			unapplied = append(unapplied, i)
		}
	}
	var paramTyps []types.FuncParam
	for _, i := range unapplied {
		var pt types.Type
		if calleeSig != nil && i < len(calleeSig.Params) {
			pt = calleeSig.Params[i].Type
		}
		paramTyps = append(paramTyps, types.FuncParam{
			Label: paramLabel(calleeSig, i), Type: pt,
		})
	}
	return l.closureChain(sp, resTyp, paramTyps, func(bs *seq, pv []core.Atom) core.Expr {
		ordered := declOrderArgs(ci, args, ci.Arity)
		for k, i := range unapplied {
			ordered[i] = pv[k]
		}
		switch sym.Kind {
		case resolve.KindFunc:
			return &core.CallDirect{
				Base:   core.Base{Sp: sp, Typ: retAfter(calleeSig, ci.Arity)},
				Callee: sym, TypeArgs: ci.TypeArgs,
				Args: ordered[:ci.Arity], Arity: ci.Arity,
			}
		case resolve.KindCtor:
			return &core.CtorApp{
				Base: core.Base{Sp: sp, Typ: retAfter(calleeSig, ci.Arity)},
				Ctor: sym, TypeArgs: ci.TypeArgs,
				Args: ordered[:ci.Arity], Arity: ci.Arity,
				Fields: l.ctorFields(sym, retAfter(calleeSig, ci.Arity)),
			}
		case resolve.KindBuiltin:
			var flat []core.Atom
			for _, a := range ordered[:ci.Arity] {
				if a != nil {
					flat = append(flat, a)
				}
			}
			return l.builtinCall(sp, retAfter(calleeSig, ci.Arity), sym, flat)
		default:
			panic("unreachable")
		}
	})
}

func paramLabel(sig *types.Func, i int) string {
	if sig != nil && i < len(sig.Params) {
		return sig.Params[i].Label
	}
	return ""
}

// retAfter is the type a saturated call of a `sig` callee produces —
// the arrow chain left after consuming `arity` parameters.
func retAfter(sig *types.Func, arity int) types.Type {
	if sig == nil {
		return nil
	}
	if len(sig.Params) <= arity {
		return sig.Ret
	}
	return &types.Func{Params: sig.Params[arity:], Ret: sig.Ret}
}

// unknownCall applies a function value one argument per CallFunc —
// the canonical chain means every application step is a real Go
// call and a partial application is simply a shorter chain.
//
// The CallFunc chain applies arguments in declaration order, so a
// labeled argument reorders the operand list through
// CallInfo.ArgToParam: `f ~b: v ~a: w` emits `f(w)(v)`. When a
// labeled argument fills a *later* slot while an earlier one stays
// open — `partial ~amount` — no CallFunc can express the result
// (skipping a curried parameter is not a Go call): the application
// eta-expands into a Closure over the remaining parameters in
// declaration order, the same shape partialCall builds for a known
// callee.
func (l *lowerer) unknownCall(s *seq, sp diag.Span, resTyp types.Type, callee check.Expr, fnExpr core.Expr, ci check.CallInfo, args []core.Atom) core.Expr {
	if fnExpr == nil {
		l.err(sp, "CallUnknown with no callee value")
		return l.litUnit(sp)
	}
	fn := l.asAtom(s, sp, fnExpr)
	switch fn.(type) {
	case *core.FuncRef, *core.CtorRef:
		l.err(sp, "CallUnknown on a statically known callee")
	case *core.Var, *core.Lit, *core.FieldGet, *core.GoFuncRef:
	default:
		panic("unreachable")
	}
	if len(ci.ArgToParam) == 0 {
		// The identity mapping (or an unrepresentable one, which
		// the checker already reported) keeps the plain chunk.
		return l.callFuncChunks(s, sp, fn, args)
	}
	ordered := declOrderArgs(ci, args, ci.Arity)
	filled := filledParams(ci, args)
	var unapplied []int
	for i := 0; i < ci.Arity; i++ {
		if !filled[i] {
			unapplied = append(unapplied, i)
		}
	}
	if len(unapplied) == 0 {
		// Saturated in declaration order — the reordered chain
		// consumes every parameter positionally.
		return l.callFuncChunks(s, sp, fn, ordered[:ci.Arity])
	}
	// A non-identity ArgToParam with unfilled slots is always a
	// non-prefix fill (a prefix fill *is* the identity): the result
	// is a function awaiting the earlier parameters.
	sig := flatSig(callee.Type())
	if sig == nil || len(sig.Params) != ci.Arity {
		l.err(sp, fmt.Sprintf("CallUnknown arity %d does not match callee type %s", ci.Arity, callee.Type()))
		return l.litUnit(sp)
	}
	params := make([]types.FuncParam, 0, len(unapplied))
	for _, i := range unapplied {
		params = append(params, sig.Params[i])
	}
	switch fn.(type) {
	case *core.Var, *core.Lit:
		// Stable references need no snapshot.
	default:
		// A FieldGet or GoFuncRef callee inside the Closure would
		// re-evaluate at every call; the callee's own evaluation
		// happens once, at application time — capture it now.
		b := l.tmpBind(sp, fn.Type())
		s.emit(b, fn)
		fn = varOf(sp, b)
	}
	return l.closureChain(sp, resTyp, params, func(bs *seq, pv []core.Atom) core.Expr {
		full := make([]core.Atom, ci.Arity)
		copy(full, ordered[:ci.Arity])
		for k, i := range unapplied {
			full[i] = pv[k]
		}
		return l.callFuncChunks(bs, sp, fn, full)
	})
}

// goCall lowers a Go call. A `_` hole becomes the parameter of a
// cute-style Closure; a multi-result call is wrapped in its consumer.
func (l *lowerer) goCall(s *seq, sp diag.Span, resTyp types.Type, ci check.CallInfo, args []core.Atom, recv core.Atom, spread bool) core.Expr {
	sig := instantiateGoSig(l.goSig(ci.Callee, resTyp), ci.TypeArgs)
	if sig.Arity == 0 && !sig.Variadic {
		// The Env oracle did not know the signature: trust CallInfo.
		sig.Arity = ci.Arity
		sig.Variadic = ci.Variadic
		if sig.Results == 0 {
			// goSig always answers a shape (Env's or the
			// goResultsOf heuristic) — Results == 0 means an
			// oracle answered a GoSig without one (a partial
			// stub); fill it from the declared result view.
			sig.Results = l.goResultsOf(resTyp)
		}
	}
	ordered := l.convGoArgs(s, sig, declOrderArgs(ci, args, 0), spread)

	if ci.HoleIndex >= 0 {
		// Cute partial application: the hole is the closure's single
		// parameter; the other (already evaluated) arguments are
		// captured by the generated closure body.
		remTyp := sigOf(resTyp)
		if remTyp == nil || len(remTyp.Params) != 1 {
			l.err(sp, fmt.Sprintf("cute _ call typed %s, not a one-parameter function", resTyp))
			return l.litUnit(sp)
		}
		// The hole's declaration-order slot — the position
		// declOrderArgs left nil in ordered. HoleIndex counts in
		// *written* argument order, so the parameter must not be
		// placed directly: labeled arguments map through ArgToParam
		// or the hole lands on the wrong slot.
		holePos := ci.HoleIndex
		if ci.HoleIndex < len(ci.ArgToParam) {
			holePos = ci.ArgToParam[ci.HoleIndex]
		}
		return l.closureOfType(sp, remTyp, func(bs *seq, pv []core.Atom) core.Expr {
			// `ordered` carries the non-hole arguments: evaluated —
			// and converted — once, at closure creation, per the
			// cute semantics of spec section 8.2. Only the hole's
			// own value arrives per call, so only its conversion
			// emits inside the body.
			filled := append([]core.Atom{}, ordered...)
			for len(filled) <= holePos {
				filled = append(filled, nil)
			}
			filled[holePos] = l.convGoArg(bs, sig, holePos, pv[0],
				spread && holePos == len(filled)-1)
			call := &core.CallGo{
				Base:   core.Base{Sp: sp, Typ: l.goCallTyp(sig, remTyp.Ret)},
				Callee: ci.Callee, Recv: recv, TypeArgs: ci.TypeArgs,
				Args:  filled,
				Arity: ci.Arity, Variadic: ci.Variadic,
				Spread: spread, Results: sig.Results,
			}
			x := l.wrapGoResult(bs, call, remTyp.Ret)
			if sig.Results == core.GoResultsOne {
				return l.goPayloadConv(bs, sp, call.Typ, remTyp.Ret, x)
			}
			return x
		})
	}

	call := &core.CallGo{
		Base:   core.Base{Sp: sp, Typ: l.goCallTyp(sig, resTyp)},
		Callee: ci.Callee, Recv: recv, TypeArgs: ci.TypeArgs,
		Args:  ordered,
		Arity: ci.Arity, Variadic: ci.Variadic,
		Spread: spread, Results: sig.Results,
	}
	x := l.wrapGoResult(s, call, resTyp)
	if sig.Results == core.GoResultsOne {
		return l.goPayloadConv(s, sp, call.Typ, resTyp, x)
	}
	return x
}

// hasSpread reports whether the written argument list carried a
// `..xs` splice — detected on the typed arguments before their
// Spread wrappers were unwrapped.
func hasSpread(args []check.Arg) bool {
	for _, a := range args {
		if _, ok := a.Value.(*check.Spread); ok {
			return true
		}
	}
	return false
}

// goCallNode lowers a saturated Go call directly into a *core.CallGo
// — for the escape consumers (EscapeCall) that need the node itself,
// not its wrapped Gom-facing value.
func (l *lowerer) goCallNode(s *seq, ap *check.Apply) *core.CallGo {
	ci := ap.Call
	recv := l.methodRecv(s, ap.Fn, ci)
	l.calleeExpr(s, ap.Fn, ci, recv)
	args := l.argAtoms(s, ap.Args)
	sig := instantiateGoSig(l.goSig(ci.Callee, ap.Type()), ci.TypeArgs)
	if sig.Results == 0 {
		// As in goCall: an oracle answered without a result
		// shape — complete it from the declared result view.
		sig.Results = l.goResultsOf(ap.Type())
	}
	spread := hasSpread(ap.Args)
	return &core.CallGo{
		Base:   core.Base{Sp: ap.Sp, Typ: l.goCallTyp(sig, ap.Type())},
		Callee: ci.Callee, Recv: recv, TypeArgs: ci.TypeArgs,
		Args:  l.convGoArgs(s, sig, declOrderArgs(ci, args, 0), spread),
		Arity: ci.Arity, Variadic: ci.Variadic,
		Spread:  spread,
		Results: sig.Results,
	}
}

// closureOf builds a Closure taking the given parameters; body is
// built by build against a fresh body-sequence and the parameter
// atoms. The closure's own *core.Func is pushed onto the boundary
// stack for the body's escapes.
func (l *lowerer) closureOf(sp diag.Span, typ types.Type, params []types.FuncParam, build func(bs *seq, pv []core.Atom) core.Expr) *core.Closure {
	// Sig takes the flat arrow chain — typ can be a residual like
	// `Int -> (Int -> Int)` whose nested Ret names the rest of the
	// chain; Params still binds only `params` (the arity split).
	fs := flatSig(typ)
	if fs == nil {
		fs = types.Fn(types.Unit, params...)
	}
	fn := &core.Func{Sp: sp, Sig: fs}
	var pv []core.Atom
	for i, p := range params {
		b := l.tmpBind(sp, p.Type)
		fn.Params = append(fn.Params, b)
		pv = append(pv, varOf(sp, b))
		_ = i
	}
	l.fnStack = append(l.fnStack, fn)
	bs := &seq{}
	tail := build(bs, pv)
	l.fnStack = l.fnStack[:len(l.fnStack)-1]
	fn.Body = &core.Body{Lets: bs.lets, Tail: tail}
	// The expression's own type records the emitted level split —
	// `Func{bound, ResidualType}` — so arity readers see the
	// immediate count: `add3 40`'s residual closure reports
	// `Int -> (Int -> Int)`, not the flat `Int -> Int -> Int` both
	// spellings Equal-compare to (check.go keeps declared sigs
	// nested for the same reason; this literal preserves the split).
	exprTyp := &types.Func{Params: params, Ret: fn.ResidualType()}
	return &core.Closure{Base: core.Base{Sp: sp, Typ: exprTyp}, Fn: fn}
}

// closureOfType is closureOf for a signature the lowering already
// holds — the parameter types come from the Func itself.
func (l *lowerer) closureOfType(sp diag.Span, sig *types.Func, build func(bs *seq, pv []core.Atom) core.Expr) *core.Closure {
	return l.closureOf(sp, sig, sig.Params, build)
}

// closureChain builds a Closure in the canonical value
// representation: one Go func per parameter — `func(p0) func(p1)
// ... { body }` — nested so each level is an arity-1 core.Func.
// build runs at the innermost level and receives every parameter
// atom in declaration order. Callers that produce a *function-typed
// value* must use this (or an equivalent arity-1 closureOf): a
// multi-parameter core.Func emits `func(p0, p1, ...)`, which is not
// a value of the gom function type — it is the Go-facing flat form
// used only inside boundary adapters.
func (l *lowerer) closureChain(sp diag.Span, typ types.Type, params []types.FuncParam, build func(bs *seq, pv []core.Atom) core.Expr) *core.Closure {
	if len(params) <= 1 {
		return l.closureOf(sp, typ, params, build)
	}
	resTyp := typ
	if fs := flatSig(typ); fs != nil {
		resTyp = &types.Func{Params: fs.Params[1:], Ret: fs.Ret}
	}
	return l.closureOf(sp, typ, params[:1], func(bs *seq, pv1 []core.Atom) core.Expr {
		return l.closureChain(sp, resTyp, params[1:], func(bs2 *seq, pvRest []core.Atom) core.Expr {
			return build(bs2, append(append([]core.Atom{}, pv1...), pvRest...))
		})
	})
}

// closureChainType is closureChain for a signature the lowering
// already holds — the parameter list comes from the Func itself.
func (l *lowerer) closureChainType(sp diag.Span, sig *types.Func, build func(bs *seq, pv []core.Atom) core.Expr) *core.Closure {
	if sig == nil {
		return l.closureOf(sp, nil, nil, build)
	}
	return l.closureChain(sp, sig, sig.Params, build)
}

// applyAtom applies an atom of function type to argument atoms —
// used by pipes and composition, where no CallInfo exists because
// the check stage never saw a single application node. The convention
// still follows CallInfo's: FuncRef/CtorRef/GoFuncRef callees take
// direct nodes, everything else is CallFunc.
func (l *lowerer) applyAtom(s *seq, sp diag.Span, resTyp types.Type, fn core.Atom, args []core.Atom) core.Expr {
	switch f := fn.(type) {
	case *core.FuncRef:
		switch {
		case len(args) == f.Arity:
			return &core.CallDirect{
				Base:   core.Base{Sp: sp, Typ: resTyp},
				Callee: f.Sym, TypeArgs: f.TypeArgs,
				Args: args, Arity: f.Arity,
			}
		case len(args) > f.Arity:
			first := &core.CallDirect{
				Base:   core.Base{Sp: sp, Typ: overAppRet(f, f.Arity)},
				Callee: f.Sym, TypeArgs: f.TypeArgs,
				Args: args[:f.Arity], Arity: f.Arity,
			}
			t := l.asAtom(s, sp, first)
			return l.callFuncChunks(s, sp, t, args[f.Arity:])
		default:
			return l.closureChainType(sp, resFunc(sigOf(f.Type()), len(args), f.Type()), func(bs *seq, pv []core.Atom) core.Expr {
				return &core.CallDirect{
					Base:   core.Base{Sp: sp, Typ: retAfter(sigOf(f.Type()), f.Arity)},
					Callee: f.Sym, TypeArgs: f.TypeArgs,
					Args:  append(append([]core.Atom{}, args...), pv...),
					Arity: f.Arity,
				}
			})
		}
	case *core.CtorRef:
		arity := flatArity(f.Type())
		if len(args) == arity {
			return &core.CtorApp{
				Base: core.Base{Sp: sp, Typ: resTyp},
				Ctor: f.Sym, TypeArgs: f.TypeArgs,
				Args: args, Arity: arity,
				Fields: f.Fields,
			}
		}
		if len(args) > arity {
			l.err(sp, fmt.Sprintf("constructor %s over-applied (%d/%d)", f.Sym.Name, len(args), arity))
			return l.litUnit(sp)
		}
		return l.closureChainType(sp, resFunc(sigOf(f.Type()), len(args), f.Type()), func(bs *seq, pv []core.Atom) core.Expr {
			return &core.CtorApp{
				Base: core.Base{Sp: sp, Typ: retAfter(sigOf(f.Type()), arity)},
				Ctor: f.Sym, TypeArgs: f.TypeArgs,
				Args:   append(append([]core.Atom{}, args...), pv...),
				Arity:  arity,
				Fields: f.Fields,
			}
		})
	case *core.GoFuncRef:
		if len(args) != f.Arity {
			l.err(sp, fmt.Sprintf("Go function %s applied at %d args, arity %d — partial Go application needs a Closure", f.Sym.Name, len(args), f.Arity))
			return l.litUnit(sp)
		}
		call := &core.CallGo{
			Base:   core.Base{Sp: sp, Typ: l.goCallTyp(GoSig{Results: f.Results}, resTyp)},
			Callee: f.Sym, Recv: f.Recv, TypeArgs: f.TypeArgs,
			Args:    args,
			Arity:   f.Arity,
			Results: f.Results,
		}
		return l.wrapGoResult(s, call, resTyp)
	case *core.Var, *core.Lit, *core.FieldGet:
		return l.callFuncChunks(s, sp, fn, args)
	default:
		panic("unreachable")
	}
}

// callFuncChunks applies an unknown function value one argument per
// CallFunc — the canonical value representation is one Go func per
// arrow, so `f a b` emits `f(a)(b)` the way the evaluator's
// one-argument apply steps run it. Intermediates bind to temporaries
// so each application's type (the residual arrow) stays an atom's.
func (l *lowerer) callFuncChunks(s *seq, sp diag.Span, fn core.Atom, args []core.Atom) core.Expr {
	for len(args) > 0 {
		fs, ok := types.Chase(fn.Type()).(*types.Func)
		if !ok {
			l.err(sp, fmt.Sprintf("applying a non-function value of type %s", fn.Type()))
			return l.litUnit(sp)
		}
		// The residual after one argument: a nested Ret names the
		// rest of the chain; a multi-parameter chunk keeps its tail.
		resTyp := fs.Ret
		if len(fs.Params) > 1 {
			resTyp = &types.Func{Params: fs.Params[1:], Ret: fs.Ret}
		}
		call := &core.CallFunc{
			Base: core.Base{Sp: sp, Typ: resTyp},
			Fn:   fn, Args: args[:1],
		}
		args = args[1:]
		if len(args) == 0 {
			return call
		}
		fn = l.asAtom(s, sp, call)
	}
	return fn
}

// sigOf chases bound metas, then returns the Func behind t as
// stored — the *immediate* parameter chunk: `Int -> (Int -> Int)`
// reports arity 1 and a function-typed Ret. That is the view call
// chunking and declared-arity comparisons need; use flatSig where
// the whole arrow chain is wanted.
func sigOf(t types.Type) *types.Func {
	fs, _ := types.Chase(t).(*types.Func)
	return fs
}

// flatSig is sigOf's canonical flat form: the whole arrow chain is
// one Func whose chased Ret is never itself a function. types.Fn
// already flattens a literal nested Func, but two spellings reach
// lowering unflattened — a declared `fn mk () -> (Int -> Int)`
// keeps its Ret nested so `mk ()` is CallDirect (check.go), and
// inference can hide a function behind a bound Meta in Ret position
// (`fn k -> fn y -> k + y` types as `?k -> ?r`). Core's Func.Sig
// demands the flat form — the arity split lives in Func.Params —
// so the chain is spliced here; Equal treats the spellings as the
// same type. nil for non-function types.
func flatSig(t types.Type) *types.Func {
	fs := sigOf(t)
	if fs == nil {
		return nil
	}
	var tail []types.FuncParam
	ret := fs.Ret
	for {
		inner, ok := types.Chase(ret).(*types.Func)
		if !ok {
			break
		}
		tail = append(tail, inner.Params...)
		ret = inner.Ret
	}
	if tail == nil {
		return fs
	}
	params := make([]types.FuncParam, 0, len(fs.Params)+len(tail))
	params = append(params, fs.Params...)
	params = append(params, tail...)
	return &types.Func{Params: params, Ret: ret}
}

// overAppRet is the function type returned by the saturated prefix
// of an over-applied FuncRef call. The flat view sees through a
// nested declared Ret — `add3` applied past its declared arity
// still reports the residual `Int -> Int`.
func overAppRet(f *core.FuncRef, arity int) types.Type {
	fs := flatSig(f.Type())
	if fs == nil || len(fs.Params) <= arity {
		return f.Type()
	}
	return &types.Func{Params: fs.Params[arity:], Ret: fs.Ret}
}

// resFunc is the residual function type after supplying `consumed`
// of a callee's flat parameters.
func resFunc(sig *types.Func, consumed int, fallback types.Type) *types.Func {
	if sig == nil {
		return sigOf(fallback)
	}
	if len(sig.Params) <= consumed {
		return types.Fn(sig.Ret)
	}
	return &types.Func{Params: sig.Params[consumed:], Ret: sig.Ret}
}

// --- sugared operators ---

// binaryExpr handles the operators still present in the typed tree.
// Plain binary ops map straight through; the pipeline/composition
// operators are desugared here — they do not exist in Core.
func (l *lowerer) binaryExpr(s *seq, e *check.Binary) core.Expr {
	switch e.Op {
	case syntax.BinPipeFwd:
		// `x |> rhs` — evaluate x, then rhs, then append x as the
		// lowest unfilled parameter of rhs's application.
		xAtom := l.atom(s, e.X)
		return l.pipeApply(s, e, e.Y, xAtom)
	case syntax.BinPipeBack:
		// `rhs <| x` — evaluate rhs (and its args), then x, append.
		return l.pipeApplyBack(s, e)
	case syntax.BinPipeTry:
		return l.pipeTry(s, e)
	case syntax.BinComposeFwd:
		// `f >> g` is `fn x -> g (f x)` — f applies first, and the
		// operands evaluate in source order: f then g.
		return l.compose(s, e, l.atom(s, e.X), l.atom(s, e.Y), e.X.Span(), e.Y.Span())
	case syntax.BinComposeBack:
		// `g << f` is `fn x -> g (f x)` — f applies first, but the
		// operands still evaluate in source order: g then f.
		gAtom := l.atom(s, e.X)
		fAtom := l.atom(s, e.Y)
		return l.compose(s, e, fAtom, gAtom, e.Y.Span(), e.X.Span())
	case syntax.BinEq, syntax.BinNe:
		return l.eqExpr(s, e)
	default:
		x := l.atom(s, e.X)
		y := l.atom(s, e.Y)
		if l.litFoldDiverges(e.Op, e.X.Type(), x, y) {
			x = l.bindAtom(s, e.X.Span(), x)
		}
		return &core.Binary{Base: baseOf(e), Op: e.Op, X: x, Y: y}
	}
}

// litFoldDiverges reports whether `x op y` would emit a Go constant
// expression whose folded value can differ from the machine
// operation the language specifies (decision
// 2026-11-05-numeric-arithmetic-per-operation). That needs two
// things: both operands lowered to literals — the only atomic core
// node Go reads as a constant (Unary/Binary/Conv operands are not
// atoms, so asAtom has already routed them through variables, and
// Vars name variables, which Go never folds) — and an operator on a
// type whose fold is not the machine operation: Float arithmetic
// and Float comparisons fold in exact rationals, which both rounds
// once instead of once per operation and merges literals a single
// float64 also merges; Int-family arithmetic folds to a value the
// emitted Go type may reject as a constant overflow. `%` never
// overflows (|a % b| < |b|), integer comparisons are exact, and
// every non-numeric fold agrees with the runtime value — none of
// those can diverge.
//
// Keep this op/type table in step with codegen.untypedConst's
// node-shape table (internal/codegen/gen.go): they are the two
// enumerations of "what emits a Go constant expression". A new
// fold-divergent op or a new constant-shaped core node must be
// reflected in both.
func (l *lowerer) litFoldDiverges(op syntax.BinOp, t types.Type, x, y core.Atom) bool {
	if _, ok := x.(*core.Lit); !ok {
		return false
	}
	if _, ok := y.(*core.Lit); !ok {
		return false
	}
	c, ok := types.Chase(t).(*types.Con)
	if !ok || c.Qual != "" || l.declaredT(c) {
		return false
	}
	switch c.Name {
	case "Float":
		switch op {
		case syntax.BinAdd, syntax.BinSub, syntax.BinMul, syntax.BinDiv,
			syntax.BinLt, syntax.BinLe, syntax.BinGt, syntax.BinGe,
			syntax.BinEq, syntax.BinNe:
			return true
		}
	case "Int", "Int32", "Int64":
		switch op {
		case syntax.BinAdd, syntax.BinSub, syntax.BinMul, syntax.BinDiv:
			return true
		}
	}
	return false
}

// bindAtom emits `tmp = a` and answers a reference to tmp —
// materializing an atom through a variable where the emitted shape
// itself matters (defeating Go's constant fold for an all-literal
// numeric operation).
func (l *lowerer) bindAtom(s *seq, sp diag.Span, a core.Atom) core.Atom {
	b := l.tmpBind(sp, a.Type())
	s.emit(b, a)
	return varOf(sp, b)
}

// (The `==`/`!=` lowering lives in equal.go: it emits a core.Equal
// whose recipe — native Go `==`, the rt.Equal fallback, the GoError
// comparison, or a generated comparator call — the static operand
// type decides here, so codegen only renders the plan it is given.)

// appendArg adjusts a CallInfo for the pipe-synthesized trailing
// argument: one more argument applied, filling the lowest unfilled
// parameter (ArgToParam stays nil once the call is over-applied).
func appendArg(ci *check.CallInfo) {
	ci.Applied++
	if len(ci.ArgToParam) > 0 || ci.Applied <= ci.Arity {
		pos := lowestUnfilled(*ci)
		ci.ArgToParam = append(append([]int{}, ci.ArgToParam...), pos)
	}
	// A partial application saturated by the piped argument is an
	// ordinary direct call now — no residual Closure.
	if ci.Kind == check.CallPartial && ci.Applied >= ci.Arity {
		ci.Kind = check.CallDirect
		ci.Unapplied = 0
	}
}

// pipeApply lowers `x |> rhs` once x is an atom: rhs applied to its
// written arguments plus the piped value as the lowest unfilled
// parameter. resTyp is the pipe node's own type.
func (l *lowerer) pipeApply(s *seq, e *check.Binary, rhs check.Expr, piped core.Atom) core.Expr {
	if ap, ok := rhs.(*check.Apply); ok {
		return l.pipeApplyInto(s, rhs.Span(), ap, func() core.Atom { return piped }, e.Type())
	}
	// A bare variadic-arity-1 Go callee: the checker typed the piped
	// value as one variadic element — emit the CallGo of that
	// element call rather than an application of the slice-typed
	// function value.
	if out, ok := l.pipeGoElemCall(s, rhs, func() core.Atom { return piped }, e.Type()); ok {
		return out
	}
	fn := l.atom(s, rhs)
	return l.applyAtom(s, rhs.Span(), e.Type(), fn, []core.Atom{piped})
}

// pipeApplyBack lowers `rhs PIPEBACK x` — identical convention to
// the forward pipe, evaluated left to right: the callee side first,
// then the argument.
func (l *lowerer) pipeApplyBack(s *seq, e *check.Binary) core.Expr {
	if ap, ok := e.X.(*check.Apply); ok {
		return l.pipeApplyInto(s, e.X.Span(), ap, func() core.Atom { return l.atom(s, e.Y) }, e.Type())
	}
	if out, ok := l.pipeGoElemCall(s, e.X, func() core.Atom { return l.atom(s, e.Y) }, e.Type()); ok {
		return out
	}
	fn := l.atom(s, e.X)
	return l.applyAtom(s, e.Sp, e.Type(), fn, []core.Atom{l.atom(s, e.Y)})
}

// pipeApplyInto lowers a pipe whose callee side is itself an
// application: the piped value joins the written arguments — into
// the `_` slot when the call carries a placeholder, else as the
// lowest unfilled parameter. piped produces the argument atom; it
// runs after the callee side's own lowering so a back pipe keeps
// left-to-right evaluation (a forward pipe's argument is the left
// operand, already bound before this call).
func (l *lowerer) pipeApplyInto(s *seq, sp diag.Span, ap *check.Apply, piped func() core.Atom, resTyp types.Type) core.Expr {
	ci := ap.Call
	recv := l.methodRecv(s, ap.Fn, ci)
	fnExpr := l.calleeExpr(s, ap.Fn, ci, recv)
	args := l.argAtoms(s, ap.Args)
	atom := piped()
	if ci.HoleIndex >= 0 {
		// The piped value fills the placeholder: it takes the
		// hole's written slot and the call is saturated — no
		// residual Closure.
		if ci.HoleIndex < len(args) {
			args[ci.HoleIndex] = atom
		} else {
			args = append(args, atom)
		}
		ci.HoleIndex = -1
	} else {
		args = append(args, atom)
		appendArg(&ci)
	}
	return l.finishCall(s, sp, resTyp, ap.Fn, fnExpr, recv, ci, args, hasSpread(ap.Args))
}

// pipeGoElemCall lowers a bare variadic-arity-1 Go callee a pipe
// hands one value to. The checker types the pipe as a one-element
// variadic call for exactly that shape (check.pipeCall), so the piped
// value is the whole argument list and the result is the callee's
// own — emitting an ordinary CallGo rather than applying the
// slice-typed function value (which would pass the slice itself).
// piped produces the argument atom and runs after the receiver, so a
// back pipe still evaluates left to right. Reports false for every
// other operand shape.
func (l *lowerer) pipeGoElemCall(s *seq, callee check.Expr, piped func() core.Atom, resTyp types.Type) (core.Expr, bool) {
	var sym *resolve.Symbol
	if v, ok := callee.(*check.Var); ok {
		sym = v.Sym
	} else if m, ok := callee.(*check.MemberExpr); ok {
		sym = m.Sym
	}
	if sym == nil || (sym.Kind != resolve.KindMethod && !sym.IsGo) {
		return nil, false
	}
	sig := l.goSig(sym, callee.Type())
	if !sig.Variadic || sig.Arity != 1 {
		return nil, false
	}
	var recv core.Atom
	if m, ok := callee.(*check.MemberExpr); ok && sym.Kind == resolve.KindMethod {
		recv = l.atom(s, m.Recv)
	}
	ci := check.CallInfo{
		Kind: check.CallGo, Callee: sym,
		Arity: 1, Applied: 1, HoleIndex: -1, Variadic: true,
	}
	return l.goCall(s, callee.Span(), resTyp, ci, []core.Atom{piped()}, recv, false), true
}

// lowestUnfilled is the smallest callee-parameter index no written
// argument fills — where a pipe-appended argument lands.
func lowestUnfilled(ci check.CallInfo) int {
	used := map[int]bool{}
	for _, p := range ci.ArgToParam {
		used[p] = true
	}
	for i := 0; i < ci.Arity; i++ {
		if !used[i] {
			return i
		}
	}
	return ci.Arity
}

// compose lowers `f >> g` / `g << f` into a closure
// `func(p...) { second (first p...) p... }`. first and second are
// the *application-order* function atoms, already evaluated in source
// order by the caller; the applications happen per call.
func (l *lowerer) compose(s *seq, e *check.Binary, first, second core.Atom, firstSp, secondSp diag.Span) core.Expr {
	sig := sigOf(e.Type())
	if sig == nil {
		l.err(e.Sp, fmt.Sprintf("composition typed %s, not a function", e.Type()))
		return l.litUnit(e.Sp)
	}
	k := flatArity(first.Type())
	if k <= 0 || k > len(sig.Params) {
		k = 1
	}
	return l.closureChain(e.Sp, e.Type(), sig.Params, func(bs *seq, pv []core.Atom) core.Expr {
		mid := l.applyAtom(bs, firstSp, retAfter(sigOf(first.Type()), k), first, pv[:k])
		midAtom := l.asAtom(bs, firstSp, mid)
		rest := append([]core.Atom{midAtom}, pv[k:]...)
		return l.applyAtom(bs, secondSp, sig.Ret, second, rest)
	})
}

// --- escape expressions ---

// tryExpr lowers `try e`: a direct Go (T,error) call becomes the
// fused EscapeCall; anything else is EscapeResult over the bound
// Result value.
func (l *lowerer) tryExpr(s *seq, e *check.Try) core.Expr {
	if ap, ok := e.X.(*check.Apply); ok && ap.Call.Kind == check.CallGo && ap.Call.HoleIndex < 0 {
		call := l.goCallNode(s, ap)
		if call.Results == core.GoResultsErr || call.Results == core.GoResultsErrOnly {
			fn := l.curFn()
			if fn == nil {
				// Report once and degrade to the unescaped value —
				// an EscapeCall with no target is a second,
				// redundant violation.
				l.err(e.Sp, "try escapes with no enclosing function boundary")
				return l.wrapGoResult(s, call, ap.Type())
			}
			return l.goPayloadConv(s, e.Sp, call.Typ, e.Type(), &core.EscapeCall{
				Base: core.Base{Sp: e.Sp, Typ: call.Typ},
				Call: call, To: fn, Wrap: "",
				NilGuard: l.payloadNilGuard(call, ap.Type()),
			})
		}
		// Non-escapable shape — fall through to EscapeResult, which
		// the validator will report against the operand's type.
		return &core.EscapeResult{
			Base: core.Base{Sp: e.Sp, Typ: e.Type()},
			X:    l.asAtom(s, e.Sp, l.wrapGoResult(s, call, ap.Type())),
			To:   l.curFn(), Wrap: "",
		}
	}
	operand := l.atom(s, e.X)
	fn := l.curFn()
	if fn == nil {
		l.err(e.Sp, "try escapes with no enclosing function boundary")
		return operand
	}
	typ := e.Type()
	if a, _, ok := l.resultArgs(operand.Type()); ok {
		typ = a
	}
	return &core.EscapeResult{
		Base: core.Base{Sp: e.Sp, Typ: typ},
		X:    operand, To: fn, Wrap: "",
	}
}

// pipeTry returns a Result value. Only the compiler-created local
// boundary exits on failure; the surrounding user function continues.
//
// A `try` that no lambda shields — one whose escape boundary is the
// user function being lowered — cannot move into the synthesized
// boundary closure without retargeting its escape (spec section 6).
// When the chain holds such a try it takes the segmented path
// (pipeTryMixed); otherwise every step is safe inside one boundary
// closure and the chain fuses outright — a `try` under a lambda keeps
// its own boundary wherever the lambda is applied, so it does not
// force the segmented path.
func (l *lowerer) pipeTry(s *seq, e *check.Binary) core.Expr {
	if hasUnguardedTry(e) {
		return l.pipeTryMixed(s, e)
	}
	cl := l.closureOf(e.Sp, types.Fn(e.Type(), types.P(types.Unit)),
		[]types.FuncParam{types.P(types.Unit)}, func(bs *seq, _ []core.Atom) core.Expr {
			payload := l.pipePayload(bs, e)
			return l.pipeOk(e, payload)
		})
	return &core.CallFunc{Base: baseOf(e), Fn: l.asAtom(s, e.Sp, cl), Args: []core.Atom{l.litUnit(e.Sp)}}
}

// pipePayload fuses a left-associated chain inside one local boundary.
// Intermediate successful pipeline stages never construct Result values.
func (l *lowerer) pipePayload(s *seq, e *check.Binary) core.Atom {
	var in core.Atom
	if prev, ok := e.X.(*check.Binary); ok && prev.Op == syntax.BinPipeTry {
		in = l.pipePayload(s, prev)
	} else {
		in = l.pipeInflowAtom(s, e)
	}
	return l.escapeStepPayload(s, e, l.stepApply(s, e, e.Y, in))
}

// pipeInflowAtom lowers the non-|?> left operand of a chain level
// inside a boundary sequence: a Go (T,error) call fuses to EscapeCall,
// a stored Result unwraps through EscapeResult, and any other value —
// including a non-Result first value (spec 9.2.7) — feeds the step
// directly. The escapes target the enclosing boundary (curFn), so this
// may only run inside a synthesized boundary sequence.
func (l *lowerer) pipeInflowAtom(s *seq, e *check.Binary) core.Atom {
	fn := l.curFn()
	var in core.Atom
	if ap, ok := e.X.(*check.Apply); ok && ap.Call.Kind == check.CallGo && ap.Call.HoleIndex < 0 {
		call := l.goCallNode(s, ap)
		if call.Results == core.GoResultsErr || call.Results == core.GoResultsErrOnly {
			in = l.asAtom(s, e.X.Span(), &core.EscapeCall{
				Base: core.Base{Sp: e.X.Span(), Typ: call.Typ}, Call: call, To: fn,
				NilGuard: l.payloadNilGuard(call, ap.Type()),
			})
			in = l.convAtomTo(s, e.X.Span(), in, l.payloadOf(ap.Type()))
		} else {
			x := l.wrapGoResult(s, call, ap.Type())
			// A single Go value retains its raw Go type (notably int).
			// Multi-result consumers already carry their element conversions.
			if call.Results == core.GoResultsOne {
				x = l.goPayloadConv(s, e.X.Span(), call.Typ, ap.Type(), x)
			}
			in = l.asAtom(s, e.X.Span(), x)
		}
	}
	if in == nil {
		in = l.pipeInputAtom(s, e.X)
		if _, _, isRes := l.resultArgs(in.Type()); isRes {
			in = l.asAtom(s, e.X.Span(), &core.EscapeResult{
				Base: core.Base{Sp: e.X.Span(), Typ: l.payloadOf(in.Type())}, X: in, To: fn,
			})
		}
	}
	return in
}

// pipeInputAtom preserves the effects of Unit-valued inflow expressions
// without assigning a zero-result Go call, including the explicit-try path.
func (l *lowerer) pipeInputAtom(s *seq, e check.Expr) core.Atom {
	if l.isUnitCon(e.Type()) {
		l.stmt(s, e)
		return l.litUnit(e.Span())
	}
	return l.atom(s, e)
}

func (l *lowerer) pipeOk(e *check.Binary, payload core.Atom) core.Expr {
	return &core.CtorApp{Base: baseOf(e), Ctor: l.ctorSym("Ok"), Args: []core.Atom{payload}, Arity: 1}
}

// hasUnguardedTry reports whether e contains a `try` no lambda
// shields — a try whose escape boundary is the function the chain is
// being lowered into. Such a `try` cannot be moved into a synthesized
// boundary closure without changing its escape target. A `try` under
// a lambda escapes the lambda itself wherever the lambda is applied,
// so it does not force the conservative path; `go { ... }` is not a
// boundary (the checker already rejects a try escaping across it) and
// does not shield. The walk mirrors check.WalkExpr's tree, minus the
// descent into lambda bodies.
func hasUnguardedTry(e check.Expr) bool {
	found := false
	var walk func(x check.Expr)
	walk = func(x check.Expr) {
		if x == nil || found {
			return
		}
		switch x := x.(type) {
		case *check.Lit, *check.Var, *check.Hole:
		case *check.Lambda:
			// A function boundary: trys inside target the lambda.
		case *check.Try:
			found = true
		case *check.Apply:
			walk(x.Fn)
			for _, a := range x.Args {
				walk(a.Value)
			}
		case *check.MemberExpr:
			walk(x.Recv)
		case *check.Unary:
			walk(x.X)
		case *check.Binary:
			walk(x.X)
			walk(x.Y)
		case *check.StrInterp:
			for _, p := range x.Parts {
				switch p := p.(type) {
				case *check.StrExpr:
					walk(p.X)
				case *check.StrText:
				default:
					panic("unreachable")
				}
			}
		case *check.If:
			walk(x.Cond)
			walk(x.Then)
			walk(x.Else)
		case *check.Match:
			walk(x.Subj)
			for _, a := range x.Arms {
				walk(a.Guard)
				walk(a.Body)
			}
		case *check.Block:
			for _, st := range x.Stmts {
				walk(st)
			}
		case *check.Let:
			walk(x.Value)
		case *check.Ignore:
			walk(x.X)
		case *check.GoExpr:
			walk(x.Body)
		case *check.For:
			walk(x.Iter)
			walk(x.Body)
		case *check.SelectExpr:
			for _, a := range x.Arms {
				switch a := a.(type) {
				case *check.RecvArm:
					walk(a.Chan)
					walk(a.Body)
				case *check.SendArm:
					walk(a.Chan)
					walk(a.Value)
					walk(a.Body)
				case *check.DefaultArm:
					walk(a.Body)
				default:
					panic("unreachable")
				}
			}
		case *check.ListLit:
			for _, el := range x.Elems {
				walk(el)
			}
		case *check.TupleLit:
			for _, el := range x.Elems {
				walk(el)
			}
		case *check.Spread:
			walk(x.X)
		case *check.RecordLit:
			walk(x.With)
			for _, f := range x.Fields {
				walk(f.Value)
			}
		case *check.Ascribe:
			walk(x.X)
		default:
			panic("unreachable")
		}
	}
	walk(e)
	return found
}

// pipeTryMixed lowers a |?> chain holding at least one unguarded `try`.
// Such a try must keep the user function as its escape boundary, so the
// chain splits into segments at the steps that carry one: each maximal
// run of guarded steps fuses inside one boundary closure (a Go
// (T,error) call fuses to EscapeCall and no intermediate Result is
// materialized between steps — the same emission pipePayload produces),
// while an unguarded step's application is evaluated in the enclosing
// sequence — the top one or a match arm's — where its trys still target
// the user function. A decision-tree match hands a segment's Ok payload
// to the next unguarded step, so a failure skips every remaining step.
// The semantics are the conservative path's — escape destinations,
// wrap-once names and evaluation order are unchanged; only the number
// of synthesized closures and materialized Results shrinks.
func (l *lowerer) pipeTryMixed(s *seq, e *check.Binary) core.Expr {
	// Flatten the left-associated chain: levels[0] is the innermost |?>.
	var rev []*check.Binary
	for x := e; ; {
		rev = append(rev, x)
		prev, ok := x.X.(*check.Binary)
		if !ok || prev.Op != syntax.BinPipeTry {
			break
		}
		x = prev
	}
	levels := make([]*check.Binary, len(rev))
	for i, x := range rev {
		levels[len(rev)-1-i] = x
	}
	unguarded := make([]bool, len(levels))
	for i, lv := range levels {
		unguarded[i] = hasUnguardedTry(lv.Y)
	}

	in := levels[0].X
	if unguarded[0] {
		// No leading segment exists: the inflow evaluates in s — a
		// sequence of the user boundary — and a Result input is
		// matched for its payload like any boundary Result.
		inAtom := l.pipeInputAtom(s, in)
		if _, _, isRes := l.resultArgs(inAtom.Type()); isRes {
			return l.pipeFeed(s, e, inAtom, func(as *seq, p core.Atom) core.Expr {
				return l.pipeRest(as, e, levels, unguarded, 0, p)
			})
		}
		return l.pipeRest(s, e, levels, unguarded, 0, inAtom)
	}

	// The chain opens with a boundary segment. A guarded inflow is
	// lowered inside it (Go calls fuse); an unguarded inflow evaluates
	// in s and is inspected there.
	var enter func(bs *seq) core.Atom
	if hasUnguardedTry(in) {
		inAtom := l.pipeInputAtom(s, in)
		if _, _, isRes := l.resultArgs(inAtom.Type()); isRes {
			enter = func(bs *seq) core.Atom {
				return l.asAtom(bs, in.Span(), &core.EscapeResult{
					Base: core.Base{Sp: in.Span(), Typ: l.payloadOf(inAtom.Type())},
					X:    inAtom, To: l.curFn(),
				})
			}
		} else {
			enter = func(bs *seq) core.Atom { return inAtom }
		}
	} else {
		enter = func(bs *seq) core.Atom { return l.pipeInflowAtom(bs, levels[0]) }
	}
	j := 0
	for j < len(levels) && !unguarded[j] {
		j++
	}
	seg := l.pipeSegment(s, levels[j-1], enter, levels, 0, j)
	if j == len(levels) {
		return seg
	}
	res := l.asAtom(s, levels[j-1].Sp, seg)
	return l.pipeFeed(s, e, res, func(as *seq, p core.Atom) core.Expr {
		return l.pipeRest(as, e, levels, unguarded, j, p)
	})
}

// pipeRest continues a mixed chain from step i given the payload atom
// p — already unwrapped and bound in cs, a sequence of the user
// boundary. Guarded steps fuse inside one boundary segment; an
// unguarded step's arguments evaluate in cs so its trys keep the user
// boundary, and its Result either enters the next segment for
// inspection (when the step name wraps the error) or feeds a plain
// decision-tree match. A Go (T,error) call splits further: only the
// argument evaluation stays in cs — the call proper joins the next
// segment as a fused EscapeCall, so no Result materializes between
// the two sides.
func (l *lowerer) pipeRest(cs *seq, e *check.Binary, levels []*check.Binary, unguarded []bool, i int, p core.Atom) core.Expr {
	n := len(levels)
	if i >= n {
		return l.pipeOk(e, p)
	}
	if !unguarded[i] {
		j := i + 1
		for j < n && !unguarded[j] {
			j++
		}
		seg := l.pipeSegment(cs, levels[j-1], func(bs *seq) core.Atom { return p }, levels, i, j)
		if j == n {
			return seg
		}
		res := l.asAtom(cs, levels[j-1].Sp, seg)
		return l.pipeFeed(cs, e, res, func(as *seq, q core.Atom) core.Expr {
			return l.pipeRest(as, e, levels, unguarded, j, q)
		})
	}
	lv := levels[i]
	step := l.stepApply(cs, lv, lv.Y, p)
	if wrap := l.pipeWrap(lv); wrap != "" {
		// The wrap lives only on EscapeResult/EscapeCall — inspect
		// the step's Result inside the next boundary segment, which
		// also runs the guarded steps that follow it.
		j := i + 1
		for j < n && !unguarded[j] {
			j++
		}
		var enter func(bs *seq) core.Atom
		if call, ok := unwrapToCallGo(step); ok &&
			(call.Results == core.GoResultsErr || call.Results == core.GoResultsErrOnly) {
			// The step is a Go (T,error) call: its receiver and
			// arguments — including every unguarded try among them —
			// already evaluated in cs, so the call proper moves
			// inside the segment as a fused EscapeCall and no
			// Result materializes between the two sides.
			enter = func(bs *seq) core.Atom {
				payload := l.asAtom(bs, lv.Y.Span(), &core.EscapeCall{
					Base: core.Base{Sp: lv.Y.Span(), Typ: call.Typ},
					Call: call, To: l.curFn(), Wrap: wrap,
					NilGuard: l.payloadNilGuard(call, lv.Type()),
				})
				return l.convAtomTo(bs, lv.Y.Span(), payload, l.payloadOf(lv.Type()))
			}
		} else {
			val := l.asAtom(cs, lv.Y.Span(), step)
			enter = func(bs *seq) core.Atom {
				return l.asAtom(bs, lv.Y.Span(), &core.EscapeResult{
					Base: core.Base{Sp: lv.Y.Span(), Typ: l.payloadOf(val.Type())},
					X:    val, To: l.curFn(), Wrap: wrap,
				})
			}
		}
		seg := l.pipeSegment(cs, levels[j-1], enter, levels, i+1, j)
		if j == n {
			return seg
		}
		res := l.asAtom(cs, levels[j-1].Sp, seg)
		return l.pipeFeed(cs, e, res, func(as *seq, q core.Atom) core.Expr {
			return l.pipeRest(as, e, levels, unguarded, j, q)
		})
	}
	// No wrap to apply (the chain's error type is not GoError): a
	// plain match continues the chain — no boundary is needed to
	// rebuild the same Err value.
	return l.pipeFeed(cs, e, l.asAtom(cs, lv.Y.Span(), step), func(as *seq, q core.Atom) core.Expr {
		return l.pipeRest(as, e, levels, unguarded, i+1, q)
	})
}

// pipeSegment emits one immediately-applied boundary closure running
// the guarded steps levels[lo:hi]: inside it each step's Result is
// unwrapped by EscapeResult (or fused to EscapeCall for a Go (T,error)
// call) and no intermediate Ok is rebuilt. enter produces the entry
// payload inside the boundary sequence — it may create escapes
// targeting the segment's own Func. The closure's return type is
// resLevel's, the last level the segment covers.
func (l *lowerer) pipeSegment(cs *seq, resLevel *check.Binary, enter func(bs *seq) core.Atom, levels []*check.Binary, lo, hi int) core.Expr {
	cl := l.closureOf(resLevel.Sp, types.Fn(resLevel.Type(), types.P(types.Unit)),
		[]types.FuncParam{types.P(types.Unit)}, func(bs *seq, _ []core.Atom) core.Expr {
			p := enter(bs)
			for k := lo; k < hi; k++ {
				p = l.escapeStepPayload(bs, levels[k], l.stepApply(bs, levels[k], levels[k].Y, p))
			}
			return l.pipeOk(resLevel, p)
		})
	return &core.CallFunc{
		Base: baseOf(resLevel),
		Fn:   l.asAtom(cs, resLevel.Sp, cl),
		Args: []core.Atom{l.litUnit(resLevel.Sp)},
	}
}

// pipeFeed emits the decision-tree match that hands a Result subject's
// Ok payload to ok — built against the Ok arm's sequence — and rebuilds
// `Err e` at the chain's type in the Err arm so a failure skips the
// remaining steps. Invalid subjects reach the DPanic default, matching
// the boundary escapes' invalid-Result panic.
func (l *lowerer) pipeFeed(cs *seq, e *check.Binary, subj core.Atom, ok func(as *seq, p core.Atom) core.Expr) core.Expr {
	argT, errT, isRes := l.resultArgs(subj.Type())
	if !isRes {
		l.err(subj.Span(), fmt.Sprintf("|?> chain feeds %s, not a Result", typeName(subj.Type())))
		return subj
	}
	okBind := l.tmpBind(subj.Span(), argT)
	errBind := l.tmpBind(subj.Span(), errT)
	as := &seq{}
	tail := ok(as, varOf(subj.Span(), okBind))
	return &core.Match{Base: baseOf(e), Tree: &core.DSwitch{
		Base: baseOf(e), Subj: subj, Kind: core.SwitchTag,
		Cases: []core.DCase{
			{Sp: subj.Span(), Test: &core.DCtorTest{Sp: subj.Span(), Ctor: l.ctorSym("Ok")}, Binds: []*core.Bind{okBind}, Body: &core.Body{Lets: as.lets, Tail: tail}},
			{Sp: subj.Span(), Test: &core.DCtorTest{Sp: subj.Span(), Ctor: l.ctorSym("Err")}, Binds: []*core.Bind{errBind}, Body: &core.Body{Tail: &core.CtorApp{Base: baseOf(e), Ctor: l.ctorSym("Err"), Args: []core.Atom{varOf(e.Sp, errBind)}, Arity: 1}}},
		},
		Default: l.dpanic(subj, e.Type()),
	}}
}

// pipeWrap is a chain level's %w wrap name — the step's source name
// when the chain's error type is the builtin GoError, "" otherwise.
// The same rule escapeStepPayload applies; a non-Result level type
// (broken input) wraps nothing.
func (l *lowerer) pipeWrap(e *check.Binary) string {
	if _, e2, ok := l.resultArgs(e.Type()); ok && l.isGoError(e2) {
		return l.stepName(e.Y)
	}
	return ""
}

// stepApply applies a |?> step to the unwrapped payload — the same
// shape as |>: an Apply rhs takes the payload per the pipe
// convention (fill the hole, else the lowest unfilled parameter),
// anything else is a function value applied once. resTyp is the |?>
// node's type — the step's full-application result.
func (l *lowerer) stepApply(s *seq, e *check.Binary, rhs check.Expr, in core.Atom) core.Expr {
	// A known unary Go step is a call, not a function value. Keep its
	// raw result pair available to EscapeCall instead of manufacturing
	// an adapter Closure that would materialize a Result first.
	var sym *resolve.Symbol
	var targs []types.Type
	var recv core.Atom
	if v, ok := rhs.(*check.Var); ok {
		sym, targs = v.Sym, v.TypeArgs
	} else if m, ok := rhs.(*check.MemberExpr); ok {
		sym, targs = m.Sym, m.TypeArgs
	}
	if sym != nil && sym.IsGo && (sym.Kind == resolve.KindFunc || sym.Kind == resolve.KindMethod) {
		sig := l.goSig(sym, rhs.Type())
		if sig.Arity == 1 && !sig.Variadic {
			if m, ok := rhs.(*check.MemberExpr); ok && sym.Kind == resolve.KindMethod {
				recv = l.atom(s, m.Recv)
			}
			ci := check.CallInfo{Kind: check.CallGo, Callee: sym, TypeArgs: targs, Arity: 1, Applied: 1, HoleIndex: -1}
			return l.goCall(s, rhs.Span(), e.Type(), ci, []core.Atom{in}, recv, false)
		}
	}
	if ap, ok := rhs.(*check.Apply); ok {
		return l.pipeApplyInto(s, rhs.Span(), ap, func() core.Atom { return in }, e.Type())
	}
	fn := l.atom(s, rhs)
	return l.applyAtom(s, rhs.Span(), e.Type(), fn, []core.Atom{in})
}

// escapeStepPayload unwraps one step under the local pipeline boundary.
// The caller wraps only the final payload in Ok. Direct Go error-returning
// calls fuse to EscapeCall without materializing an intermediate Result.
// The escapes and their bindings take the step operand's span — the
// same convention pipeRest's unguarded side and pipeInflowAtom follow.
func (l *lowerer) escapeStepPayload(s *seq, e *check.Binary, step core.Expr) core.Atom {
	wrap := l.pipeWrap(e)
	fn := l.curFn()
	var payload core.Atom
	if call, ok := unwrapToCallGo(step); ok &&
		(call.Results == core.GoResultsErr || call.Results == core.GoResultsErrOnly) {
		payload = l.asAtom(s, e.Y.Span(), &core.EscapeCall{
			Base: core.Base{Sp: e.Y.Span(), Typ: call.Typ},
			Call: call, To: fn, Wrap: wrap,
			NilGuard: l.payloadNilGuard(call, e.Type()),
		})
		payload = l.convAtomTo(s, e.Y.Span(), payload, l.payloadOf(e.Type()))
	} else {
		stepAtom := l.asAtom(s, e.Y.Span(), step)
		payload = l.asAtom(s, e.Y.Span(), &core.EscapeResult{
			Base: core.Base{Sp: e.Y.Span(), Typ: l.payloadOf(stepAtom.Type())},
			X:    stepAtom, To: fn, Wrap: wrap,
		})
	}
	return payload
}

// unwrapToCallGo returns the CallGo under a single consumer wrapper
// (the one-result Go call is returned bare; a multi-result one keeps
// its mandatory consumer — EscapeCall cannot fuse through it).
func unwrapToCallGo(x core.Expr) (*core.CallGo, bool) {
	// Partial probes stay as if-assertions: core.Expr is a sumtype.
	if x, ok := x.(*core.CallGo); ok {
		return x, true
	}
	if x, ok := x.(*core.ResultFromGo); ok {
		return x.Call, true
	}
	return nil, false
}

// payloadOf returns the Ok payload type of a *builtin* Result —
// the type itself otherwise. A file-local `type Result` reports
// itself: it carries no builtin payload to unwrap.
func (l *lowerer) payloadOf(t types.Type) types.Type {
	if a, _, ok := l.resultArgs(t); ok {
		return a
	}
	return t
}

// payloadNilGuard reports whether a `(T, error)` Go call's payload
// carries the declared never-nil claim — `Result GoError GoError`
// over `(error, error)` — the boundary verifies with `if v == nil
// { panic }` (spec section 8.6). want is the Gom-facing Result
// type; the claim only exists when both the declared payload and
// the raw first result spell `error`.
func (l *lowerer) payloadNilGuard(call *core.CallGo, want types.Type) bool {
	if call.Results != core.GoResultsErr || !l.isGoError(call.Typ) {
		return false
	}
	return l.isGoError(l.payloadOf(want))
}

// isGoError reports whether the error half of a Result is the
// builtin GoError type — the only wrap-eligible shape (the Core
// validator rejects a non-empty Wrap for any other error type). A
// file-local `type GoError` spells the same name but is not the
// builtin, so the check consults the declaration index.
func (l *lowerer) isGoError(t types.Type) bool {
	c, ok := types.Chase(t).(*types.Con)
	return ok && c.Qual == "" && c.Name == "GoError" && !l.declaredT(c)
}

// stepName names a |?> step for its wrap text: the written head name,
// or "file:line" for a nameless step (a lambda).
func (l *lowerer) stepName(e check.Expr) string {
	if e, ok := e.(*check.Var); ok {
		return e.Sym.Name
	}
	if e, ok := e.(*check.MemberExpr); ok {
		name := e.Sel.Name
		if v, ok := e.Recv.(*check.Var); ok {
			return v.Sym.Name + "." + name
		}
		return name
	}
	if e, ok := e.(*check.Apply); ok {
		return l.stepName(e.Fn)
	}
	{
		sp := e.Span()
		if l.env.Lines != nil {
			pos := l.env.Lines.Pos(sp.Start)
			return fmt.Sprintf("%s:%d", sp.File, pos.Line)
		}
		return fmt.Sprintf("%s:%d", sp.File, sp.Start)
	}
}

// --- statements and composite expressions ---

// matchExpr lowers `match subj { arms }` to a decision tree — see
// pat.go for the compiler.
func (l *lowerer) matchExpr(s *seq, e *check.Match) core.Expr {
	subj := l.atom(s, e.Subj)
	var states []*armState
	for i := range e.Arms {
		arm := &e.Arms[i]
		states = append(states, &armState{
			src:  arm,
			sp:   arm.Sp,
			todo: []oblig{{subj: subj, pat: arm.Pat}},
		})
	}
	tree := l.compile(states, subj, s, e.Type())
	if dl, ok := tree.(*core.DLeaf); ok {
		s.lets = append(s.lets, dl.Body.Lets...)
		return dl.Body.Tail
	}
	return &core.Match{Base: baseOf(e), Tree: tree}
}

// forExpr lowers `for pat in iter { body }`.
func (l *lowerer) forExpr(s *seq, e *check.For) core.Expr {
	iter := l.atom(s, e.Iter)
	kind := core.ForList
	if c, ok := types.Chase(e.Iter.Type()).(*types.Con); ok && c.Qual == "" && !l.declaredT(c) {
		// The channel names count only when the file does not
		// declare them — a same-file `type Chan` is an ADT, not a
		// receivable channel.
		switch c.Name {
		case "Chan", "RecvChan":
			kind = core.ForChan
		}
	}
	var elem *core.Bind
	var prelude func(bs *seq)
	// Partial probes stay as if-assertions: check.Pat is a sumtype.
	if pat, ok := e.Pat.(*check.VarPat); ok {
		elem = l.namedBind(pat.Sp, pat.Sym, pat.Type())
	} else if pat, ok := e.Pat.(*check.WildPat); ok {
		elem = l.discardBind(pat.Sp, pat.Type())
	} else {
		b := l.tmpBind(e.Pat.Span(), e.Pat.Type())
		elem = b
		prelude = func(bs *seq) { l.destructure(bs, e.Pat, varOf(e.Pat.Span(), b)) }
	}
	bs := &seq{}
	if prelude != nil {
		prelude(bs)
	}
	tail := l.expr(bs, e.Body)
	return &core.ForRange{
		Base: baseOf(e), Kind: kind,
		Iter: iter, Elem: elem,
		Body: &core.Body{Lets: bs.lets, Tail: tail},
	}
}

// selectExpr lowers `select { arms }`. Every arm's channel expression
// — and every send arm's value — is evaluated once, in source order,
// before the select node itself: that is the spec's evaluation-order
// rule and ANF makes it literal.
func (l *lowerer) selectExpr(s *seq, e *check.SelectExpr) core.Expr {
	type armParts struct {
		src  check.SelectArm
		ch   core.Atom
		val  core.Atom
		bind *core.Bind
	}
	var parts []armParts
	for _, a := range e.Arms {
		switch a := a.(type) {
		case *check.RecvArm:
			p := armParts{src: a, ch: l.atom(s, a.Chan)}
			// Partial probes stay as if-assertions: check.Pat is a sumtype.
			if b, ok := a.Bind.(*check.VarPat); ok {
				p.bind = l.namedBind(b.Sp, b.Sym, b.Type())
			} else if b, ok := a.Bind.(*check.WildPat); ok {
				p.bind = l.discardBind(b.Sp, b.Type())
			} else {
				l.err(a.Sp, "select receive arm binding is not a variable or _")
				var bt types.Type = types.Unit
				if a.Bind != nil {
					bt = a.Bind.Type()
				}
				p.bind = l.discardBind(a.Sp, bt)
			}
			parts = append(parts, p)
		case *check.SendArm:
			parts = append(parts, armParts{
				src: a,
				ch:  l.atom(s, a.Chan),
				val: l.atom(s, a.Value),
			})
		case *check.DefaultArm:
			parts = append(parts, armParts{src: a})
		default:
			panic("unreachable")
		}
	}
	var arms []core.SelArm
	for _, p := range parts {
		switch a := p.src.(type) {
		case *check.RecvArm:
			arms = append(arms, &core.SelRecv{
				Sp: a.Sp, Chan: p.ch, Bind: p.bind, Body: l.exprBody(a.Body),
			})
		case *check.SendArm:
			arms = append(arms, &core.SelSend{
				Sp: a.Sp, Chan: p.ch, Value: p.val, Body: l.exprBody(a.Body),
			})
		case *check.DefaultArm:
			arms = append(arms, &core.SelDefault{
				Sp: a.Sp, Body: l.exprBody(a.Body),
			})
		default:
			panic("unreachable")
		}
	}
	return &core.SelectExpr{Base: baseOf(e), Arms: arms}
}

// listLit lowers `[e, ..rest]` — the `..rest` tail is a Spread
// element in the typed list.
func (l *lowerer) listLit(s *seq, e *check.ListLit) core.Expr {
	var elems []core.Atom
	var rest core.Atom
	for _, el := range e.Elems {
		if sp, ok := el.(*check.Spread); ok {
			rest = l.atom(s, sp.X)
			continue
		}
		elems = append(elems, l.atom(s, el))
	}
	return &core.ListNew{Base: baseOf(e), Elems: elems, Rest: rest}
}

// recordLit lowers `{ name = e, ... }` / `{ base with name = e }`.
// Field values evaluate in source order; the node itself stores
// fields in declaration order.
func (l *lowerer) recordLit(s *seq, e *check.RecordLit) core.Expr {
	type fi struct {
		src check.FieldInit
		val core.Atom
	}
	var evaluated []fi
	for _, f := range e.Fields {
		evaluated = append(evaluated, fi{src: f, val: l.atom(s, f.Value)})
	}
	fields := make([]core.FieldInit, 0, len(evaluated))
	for _, v := range evaluated {
		idx := l.fieldIndex(e.Type(), v.src.Sym)
		fields = append(fields, core.FieldInit{
			Sp: v.src.Sp, Sym: v.src.Sym, Index: idx, Value: v.val,
		})
	}
	for i := 0; i < len(fields); i++ {
		for j := i + 1; j < len(fields); j++ {
			if fields[j].Index >= 0 && (fields[i].Index < 0 || fields[j].Index < fields[i].Index) {
				fields[i], fields[j] = fields[j], fields[i]
			}
		}
	}
	if e.With != nil {
		return &core.RecordUpdate{
			Base: baseOf(e), Rec: l.atom(s, e.With), Fields: fields,
		}
	}
	return &core.RecordNew{Base: baseOf(e), Fields: fields}
}

// ascribe lowers `e : T` / `Go.conv e : T`: a no-op when checked,
// an explicit Conv when the interop conversion flag is set.
func (l *lowerer) ascribe(s *seq, e *check.Ascribe) core.Expr {
	if !e.Conv {
		return l.expr(s, e.X)
	}
	kind := l.convKind(e.X.Type(), e.Type())
	x := l.atom(s, e.X)
	if e.Iface {
		kind = core.ConvIface
		if l.isNumericCon(x.Type()) {
			// A Go interface supplies no concrete numeric context: an untyped
			// literal would otherwise acquire Go's default dynamic type (int).
			// Materialize the oracle-validated Gom type before interface boxing.
			x = l.asAtom(s, e.Sp, &core.Conv{Base: core.Base{Sp: e.Sp, Typ: x.Type()}, Kind: core.ConvNum, X: x})
		}
	}
	if e.Slice {
		// The checker validated the List ↔ Go.Slice pair; the
		// direction (and any `[]any` boxing) is read off the types.
		// Element metas solved since check time can collapse the
		// views to the same type — an identity conversion emits no
		// node at all.
		if k, ok := l.goConvKindOf(e.X.Type(), e.Type()); ok {
			if k == 0 {
				return x
			}
			kind = k
		} else {
			l.err(e.Sp, fmt.Sprintf("slice boundary conversion %s -> %s is not supported",
				e.X.Type(), e.Type()))
		}
	}
	return &core.Conv{
		Base: baseOf(e),
		Kind: kind,
		X:    x,
	}
}

// convKind picks the conversion kind for `Go.conv` — numeric between
// the numeric constructors, the boundary slice copies of spec 8.5
// (a `Go.conv` between `List` and `Go.Slice` copies like the
// implicit boundary conformance — decisions/2026-11-04-goconv-list-
// slice-copies), channel-direction narrowing, and named-type
// conversion otherwise (the checked legality table is the spec 8.4
// contract). goConvKindOf answers first: the pairs it accepts carry
// the same ConvKinds here as at an implicit boundary, and its misses
// are the named/numeric pairs only `Go.conv` admits.
func (l *lowerer) convKind(from, to types.Type) core.ConvKind {
	if k, ok := l.goConvKindOf(from, to); ok && k != 0 {
		return k
	}
	if l.isNumericCon(from) && l.isNumericCon(to) {
		return core.ConvNum
	}
	if l.chanDirPair(from, to) {
		return core.ConvChanDir
	}
	return core.ConvNamed
}

// chanDirPair reports whether (from, to) names the channel direction
// narrowing spec section 10.2 admits — a builtin `Chan a` feeding a
// `SendChan a` or `RecvChan a` of the same element type (Go's
// channel-direction assignability). A file-local `type Chan` (or a
// local directional type) is an ADT, not a channel — declaredT keeps
// the pair from matching it.
func (l *lowerer) chanDirPair(from, to types.Type) bool {
	fc, ok := types.Chase(from).(*types.Con)
	if !ok || fc.Qual != "" || fc.Name != "Chan" || len(fc.Args) != 1 || l.declaredT(fc) {
		return false
	}
	tc, ok := types.Chase(to).(*types.Con)
	if !ok || tc.Qual != "" || len(tc.Args) != 1 || l.declaredT(tc) {
		return false
	}
	if tc.Name != "SendChan" && tc.Name != "RecvChan" {
		return false
	}
	return types.Equal(fc.Args[0], tc.Args[0])
}

// isNumericCon reports whether t is one of the numeric constructors
// a Go-boundary conversion may widen — Gom's `Int` (`int64`) and the
// oracle's raw `Go.Int` (`int`) both spell `Int`. Only an
// *unqualified* name can be shadowed by a file-local `type Int`:
// that Con is an ADT, not `int64`-shaped, so it is excluded.
// Qualified names keep their reading — `Go.Int` is the numeric raw
// type and a foreign `pkg.Int` converts like its underlying type.
func (l *lowerer) isNumericCon(t types.Type) bool {
	c, ok := types.Chase(t).(*types.Con)
	if !ok || (c.Qual == "" && l.declaredT(c)) {
		return false
	}
	switch c.Name {
	case "Int", "Int32", "Int64", "Float", "Char":
		return true
	}
	return false
}

// strInterp lowers an interpolated string to builtin calls: each
// splice is displayed, then all parts are concatenated. The builtin
// names are part of the codegen contract — `display` renders a value
// per its static type, `str_concat` joins the parts.
func (l *lowerer) strInterp(s *seq, e *check.StrInterp) core.Expr {
	var parts []core.Atom
	for _, p := range e.Parts {
		switch p := p.(type) {
		case *check.StrText:
			parts = append(parts, &core.Lit{
				Base: core.Base{Sp: p.Sp, Typ: types.String},
				Kind: syntax.LitStr, Text: `"` + p.Text + `"`,
			})
		case *check.StrExpr:
			x := l.atom(s, p.X)
			d := l.asAtom(s, p.Sp, &core.CallBuiltin{
				Base: core.Base{Sp: p.Sp, Typ: types.String},
				Sym:  l.builtinSym("display"), Args: []core.Atom{x},
			})
			parts = append(parts, d)
		default:
			panic("unreachable")
		}
	}
	return &core.CallBuiltin{
		Base: core.Base{Sp: e.Sp, Typ: types.String},
		Sym:  l.builtinSym("str_concat"), Args: parts,
	}
}

// lambdaExpr lowers `fn p... -> body` to a Closure. A multi-parameter
// lambda emits the canonical value representation — one core.Func per
// parameter, `func(p0) func(p1) ... { body }` — because the lambda is
// already a value: `fn a b -> e` partially applied (`f a`) must be a
// `func(b)` Go value, not an uncallable half of `func(a, b)`. Each
// parameter's destructuring prelude sits at its own level.
func (l *lowerer) lambdaExpr(e *check.Lambda) core.Expr {
	// The Closure's Sig is the canonical flat arrow chain — core
	// keeps the declared arity in Params instead of a nested Ret.
	// The flat chain may run *longer* than the written parameter
	// list — `fn a b -> fn c -> ...`, `fn a b -> add2`, `fn a b ->
	// add2 a` all return a function — and the level path still
	// applies: each level binds one written parameter and the body's
	// own lowering nests the remaining arrows. Only fewer signature
	// arrows than written parameters is a mismatch.
	sig := flatSig(e.Type())
	if sig == nil || len(e.Params) == 0 || len(sig.Params) < len(e.Params) {
		// Degenerate shape (non-function type or a param/sig
		// mismatch the validator reports) — emit the single level.
		fn := &core.Func{Sp: e.Sp, Sig: sig}
		fn.Params = l.paramBinds(e.Params, fn)
		l.fnStack = append(l.fnStack, fn)
		s := &seq{}
		l.paramPrelude(e.Params, fn.Params, s)
		tail := l.expr(s, e.Body)
		fn.Body = &core.Body{Lets: s.lets, Tail: tail}
		l.fnStack = l.fnStack[:len(l.fnStack)-1]
		return &core.Closure{Base: baseOf(e), Fn: fn}
	}
	var level func(i int) *core.Closure
	level = func(i int) *core.Closure {
		fs := &types.Func{Params: sig.Params[i:], Ret: sig.Ret}
		fn := &core.Func{Sp: e.Sp, Sig: fs}
		fn.Params = l.paramBinds(e.Params[i:i+1], fn)
		l.fnStack = append(l.fnStack, fn)
		s := &seq{}
		l.paramPrelude(e.Params[i:i+1], fn.Params, s)
		var tail core.Expr
		if i+1 < len(e.Params) {
			tail = level(i + 1)
		} else {
			tail = l.expr(s, e.Body)
		}
		fn.Body = &core.Body{Lets: s.lets, Tail: tail}
		l.fnStack = l.fnStack[:len(l.fnStack)-1]
		typ := types.Type(fs)
		if i == 0 {
			typ = e.Type()
		}
		return &core.Closure{Base: core.Base{Sp: e.Sp, Typ: typ}, Fn: fn}
	}
	return level(0)
}

// --- tail calls ---

// markTailCalls rewrites self-recursive CallDirect nodes sitting in
// the function's own tail position into TailCall, then sets TailLoop.
// Runs after the body is lowered so it sees the final tree shape;
// nested functions keep their own boundary — their CallDirect nodes
// are not in To's tail position. When the conversion applies,
// snapshotCapturedParams repairs captured parameters (tail.go).
//
// Only the identity self-instantiation converts: the TailCall loop
// reloads the same parameter registers, so the call's type arguments
// must be the function's own TypeParams in order. A permutation like
// `f y x` inside `fn f (x: a) (y: b)` calls `f[B,A]` — a legal
// recursive call Go accepts as ordinary recursion — but reloading
// `x, y = y, x` would move a B into an A. Those calls stay
// CallDirect; under `@tailrec` they surface E0902 like any other
// residual self-call.
func (l *lowerer) markTailCalls(fn *core.Func, self *resolve.Symbol, tparams []*types.Var) {
	found := false
	var body func(b *core.Body)
	var tail func(e core.Expr) core.Expr
	var tree func(t core.DTree)

	body = func(b *core.Body) {
		if b != nil && b.Tail != nil {
			b.Tail = tail(b.Tail)
		}
	}
	tree = func(t core.DTree) {
		switch t := t.(type) {
		case *core.DSwitch:
			for i := range t.Cases {
				body(t.Cases[i].Body)
			}
			if t.Default != nil {
				tree(t.Default)
			}
		case *core.DGuard:
			tree(t.Then)
			tree(t.Else)
		case *core.DLeaf:
			body(t.Body)
		case *core.DPanic:
		default:
			panic("unreachable")
		}
	}
	tail = func(e core.Expr) core.Expr {
		// Partial probes stay as if-assertions: core.Expr is a
		// sumtype and the checker requires exhaustive switches.
		if e, ok := e.(*core.CallDirect); ok {
			if e.Callee == self && len(e.Args) == len(fn.Params) &&
				identityInstantiation(e.TypeArgs, tparams) {
				found = true
				return &core.TailCall{
					Base: e.Base, To: fn, Args: e.Args,
				}
			}
			return e
		}
		if e, ok := e.(*core.If); ok {
			body(e.Then)
			if e.Else != nil {
				body(e.Else)
			}
			return e
		}
		if e, ok := e.(*core.Match); ok {
			tree(e.Tree)
			return e
		}
		if e, ok := e.(*core.SelectExpr); ok {
			for _, a := range e.Arms {
				switch a := a.(type) {
				case *core.SelRecv:
					body(a.Body)
				case *core.SelSend:
					body(a.Body)
				case *core.SelDefault:
					body(a.Body)
				default:
					panic("unreachable")
				}
			}
			return e
		}
		return e
	}
	body(fn.Body)
	fn.TailLoop = found
	if found {
		// The loop rebinds the parameter registers, so anything a
		// capture can reach needs its own per-iteration copy.
		l.snapshotCapturedParams(fn)
	}
}

// identityInstantiation reports whether targs instantiates the
// callee's type parameters with exactly those parameters in order —
// the only self-instantiation whose TailCall parameter reload is
// well-typed. Monomorphic calls (both sides empty) are trivially
// the identity. See the markTailCalls doc comment for why
// permutations and contractions stay CallDirect.
func identityInstantiation(targs []types.Type, tparams []*types.Var) bool {
	if len(targs) != len(tparams) {
		return false
	}
	for i, v := range tparams {
		if types.Chase(targs[i]) != v {
			return false
		}
	}
	return true
}
