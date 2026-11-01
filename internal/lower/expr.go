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

// varRef lowers a KindVar reference. A Go package variable or
// constant (`math.Pi`) is a bare qualified name in the output, so
// IsGo symbols pass through; a variable of a Gom package cannot be a
// bare core.Var — cross-package lowering is out of scope for this
// phase, so that gap stays a diagnostic.
func (l *lowerer) varRef(base core.Base, sym *resolve.Symbol) *core.Var {
	if sym != nil && sym.Package != "" && !sym.IsGo {
		l.err(base.Sp, fmt.Sprintf("variable %s.%s belongs to another package — cross-package lowering is not supported yet",
			sym.Package, sym.Name))
	}
	return &core.Var{Base: base, Sym: l.substSym(sym)}
}

// varExpr lowers a value-position reference. Substitution resolves
// pattern variables bound by match temporaries.
func (l *lowerer) varExpr(e *check.Var) core.Expr {
	sym := e.Sym
	switch sym.Kind {
	case resolve.KindVar:
		return l.varRef(baseOf(e), sym)
	case resolve.KindFunc:
		if sym.IsGo {
			return l.goFuncRefOrAdapter(e.Span(), e.Type(), sym, nil)
		}
		return l.funcRefOrFlatten(e, sym)
	case resolve.KindCtor:
		if sigOf(e.Type()) != nil {
			return &core.CtorRef{Base: baseOf(e), Sym: sym}
		}
		// Nullary constructor in value position: a saturated
		// application of zero arguments.
		return &core.CtorApp{Base: baseOf(e), Ctor: sym, Arity: 0}
	case resolve.KindBuiltin:
		l.err(e.Sp, fmt.Sprintf("builtin %s used as a value", sym.Name))
		return &core.CallBuiltin{Base: baseOf(e), Sym: sym}
	default:
		l.err(e.Sp, fmt.Sprintf("%s %s is not an expression", sym.Kind, sym.Name))
		return l.litUnit(e.Sp)
	}
}

// funcRefOrFlatten lowers a Gom function reference: a FuncRef when
// the declared arity covers the whole arrow chain, otherwise the P0-2
// flatten closure `func(all...) { return f(all...) }`.
func (l *lowerer) funcRefOrFlatten(e *check.Var, sym *resolve.Symbol) core.Expr {
	decl := l.funcArity(sym, e.Type())
	flat := flatArity(e.Type())
	if decl == flat {
		return &core.FuncRef{Base: baseOf(e), Sym: sym, Arity: decl}
	}
	if decl > flat {
		l.err(e.Sp, fmt.Sprintf("function %s has declared arity %d exceeding its type arity %d", sym.Name, decl, flat))
		return &core.FuncRef{Base: baseOf(e), Sym: sym, Arity: decl}
	}
	// Function-returning function referenced at its full flat type:
	// wrap into one flat arity closure.
	sig := sigOf(e.Type())
	if sig == nil {
		l.err(e.Sp, fmt.Sprintf("function %s has non-function type %s", sym.Name, e.Type()))
		return l.litUnit(e.Sp)
	}
	return l.closureOf(e.Sp, e.Type(), sig.Params, func(bs *seq, pv []core.Atom) core.Expr {
		// Saturate the declared arity first; any flat-arity
		// remainder applies the returned function value.
		prefix := &core.CallDirect{
			Base:   core.Base{Sp: e.Sp, Typ: retAfter(sig, decl)},
			Callee: sym, Args: pv[:decl], Arity: decl,
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
	switch e.Sym.Kind {
	case resolve.KindField:
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
			Field: e.Sym, Index: idx,
		}
	case resolve.KindFunc:
		if e.Sym.IsGo {
			return l.goFuncRefOrAdapter(e.Sp, e.Type(), e.Sym, nil)
		}
		return l.funcRefOrFlatten(&check.Var{Base: check.Base{Sp: e.Sp, Typ: e.Type()}, Sym: e.Sym}, e.Sym)
	case resolve.KindVar:
		return l.varRef(baseOf(e), e.Sym)
	case resolve.KindCtor:
		if sigOf(e.Type()) != nil {
			return &core.CtorRef{Base: baseOf(e), Sym: e.Sym}
		}
		return &core.CtorApp{Base: baseOf(e), Ctor: e.Sym, Arity: 0}
	case resolve.KindMethod:
		recv := l.atom(s, e.Recv)
		return l.goFuncRefOrAdapter(e.Sp, e.Type(), e.Sym, recv)
	default:
		l.err(e.Sp, fmt.Sprintf("member %s is not an expression", e.Sym.Kind))
		return l.litUnit(e.Sp)
	}
}

// goFuncRefOrAdapter lowers a Go function or method in value
// position: a bare GoFuncRef exactly when the Go signature needs no
// adaptation (one parameter, one non-error result, no boundary
// conversion between the views), otherwise an adapter Closure whose
// body is a CallGo (P0-2, spec 8).
func (l *lowerer) goFuncRefOrAdapter(sp diag.Span, typ types.Type, sym *resolve.Symbol, recv core.Atom) core.Expr {
	sig := l.goSig(sym, typ)
	fs := sigOf(typ)
	if goBareRefOK(sig, fs) {
		return &core.GoFuncRef{
			Base: core.Base{Sp: sp, Typ: typ}, Sym: sym,
			Recv: recv, Arity: sig.Arity, Results: sig.Results,
		}
	}
	if fs == nil {
		l.err(sp, fmt.Sprintf("Go function %s has non-function type %s", sym.Name, typ))
		return l.litUnit(sp)
	}
	return l.closureOf(sp, typ, fs.Params, func(bs *seq, pv []core.Atom) core.Expr {
		call := &core.CallGo{
			Base:   core.Base{Sp: sp, Typ: goCallTyp(sig, fs.Ret)},
			Callee: sym, Recv: recv, Args: l.convGoArgs(bs, sig, pv, false),
			Arity:    sig.Arity,
			Variadic: sig.Variadic,
			Results:  sig.Results,
		}
		x := l.wrapGoResult(call, fs.Ret)
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
func goCallTyp(sig GoSig, resTyp types.Type) types.Type {
	raw := sig.ResTypes
	switch sig.Results {
	case core.GoResultsErr:
		if len(raw) > 0 {
			return raw[0]
		}
		if a, _, ok := resultArgs(resTyp); ok {
			return a
		}
	case core.GoResultsErrOnly:
		return types.Unit
	case core.GoResultsOK:
		if len(raw) > 0 {
			return raw[0]
		}
		if c, ok := isCon(resTyp, "", "Option"); ok && len(c.Args) == 1 {
			return c.Args[0]
		}
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
// resTyp is the Gom-facing type the consumer must produce. When the
// raw Go result (call.Typ) differs from the Gom-facing payload —
// `int` vs `int64` — the consumer records the conversion so codegen
// renders `rt.ResultFromGo[int64](int64(v), err)` and friends.
func (l *lowerer) wrapGoResult(call *core.CallGo, resTyp types.Type) core.Expr {
	base := core.Base{Sp: call.Sp, Typ: resTyp}
	switch call.Results {
	case core.GoResultsOne:
		return call
	case core.GoResultsErr, core.GoResultsErrOnly:
		var vc core.ConvKind
		if a, _, ok := resultArgs(resTyp); !ok {
			l.err(call.Sp, fmt.Sprintf("Go (T, error) call typed %s, not a Result", resTyp))
		} else if call.Results == core.GoResultsErr {
			vc = l.goConvKind(call, call.Typ, a)
		}
		return &core.ResultFromGo{Base: base, Call: call, ValConv: vc}
	case core.GoResultsOK:
		if c, ok := isCon(resTyp, "", "Option"); ok && len(c.Args) == 1 {
			return &core.OptionFromGo{Base: base, Call: call,
				ValConv: l.goConvKind(call, call.Typ, c.Args[0])}
		}
		return &core.TupleFromGo{Base: base, Call: call,
			ElemConv: l.goElemConvs(call, resTyp)}
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
	k, ok := goConvKindOf(from, to)
	if !ok {
		l.err(call.Sp, fmt.Sprintf("Go result of type %s cannot convert to %s — the boundary needs a value copy not yet supported",
			typeName(from), typeName(to)))
	}
	return k
}

// goConvKindOf picks the conversion from a raw Go type to the
// Gom-facing type, or reports the pair unsupported.
func goConvKindOf(from, to types.Type) (core.ConvKind, bool) {
	from = types.Chase(from)
	to = types.Chase(to)
	if types.Equal(from, to) {
		return 0, true
	}
	if isGoAny(to) {
		return core.ConvBoxAny, true
	}
	if isNumericCon(from) && isNumericCon(to) {
		return core.ConvNum, true
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
		out[i] = a
		if a == nil {
			continue
		}
		pt := goParamTypeAt(sig, i, spread && i == len(args)-1)
		at := types.Chase(a.Type())
		if pt == nil || types.Equal(at, pt) {
			continue
		}
		k, ok := goConvKindOf(at, pt)
		if !ok {
			l.err(a.Span(), fmt.Sprintf("Go call argument of type %s cannot convert to parameter type %s — the boundary needs a value copy not yet supported",
				typeName(a.Type()), typeName(pt)))
			continue
		}
		out[i] = l.asAtom(s, a.Span(), &core.Conv{
			Base: core.Base{Sp: a.Span(), Typ: pt}, Kind: k, X: a,
		})
	}
	return out
}

// goParamTypeAt is the raw Go parameter type the argument at index i
// feeds. Arguments at or beyond the last parameter of a variadic
// function feed the variadic element — except a `..xs` spread, which
// feeds the variadic slice itself.
func goParamTypeAt(sig GoSig, i int, spreadArg bool) types.Type {
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
			((c.Qual == "Go" && c.Name == "Slice") || (c.Qual == "" && c.Name == "List")) {
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
	k, ok := goConvKindOf(raw, want)
	if !ok {
		l.err(sp, fmt.Sprintf("Go result of type %s cannot convert to %s — the boundary needs a value copy not yet supported",
			typeName(raw), typeName(want)))
		return x
	}
	a := l.asAtom(s, sp, x)
	return &core.Conv{Base: core.Base{Sp: sp, Typ: want}, Kind: k, X: a}
}

// convAtomTo is goPayloadConv for a value already bound to an atom —
// the escape-produced `in` of |?> and the Ok payload of escapeStep.
func (l *lowerer) convAtomTo(s *seq, sp diag.Span, a core.Atom, want types.Type) core.Atom {
	if types.Equal(a.Type(), want) {
		return a
	}
	k, ok := goConvKindOf(a.Type(), want)
	if !ok {
		l.err(sp, fmt.Sprintf("Go result of type %s cannot convert to %s — the boundary needs a value copy not yet supported",
			typeName(a.Type()), typeName(want)))
		return a
	}
	return l.asAtom(s, sp, &core.Conv{
		Base: core.Base{Sp: sp, Typ: want}, Kind: k, X: a,
	})
}

// --- calls ---

// applyCall lowers a typed application per its CallInfo — the one
// place the call contract is honoured.
func (l *lowerer) applyCall(s *seq, e *check.Apply) core.Expr {
	ci := e.Call
	// A method callee evaluates its receiver first; the receiver is
	// the callee position's effect.
	var recv core.Atom
	isMethod := false
	if ci.Callee != nil && ci.Callee.Kind == resolve.KindMethod {
		if m, ok := e.Fn.(*check.MemberExpr); ok {
			isMethod = true
			recv = l.atom(s, m.Recv)
		}
	}
	// The callee expression evaluates first — for a statically known
	// callee it is a pure ref (emitted work is nil); for CallUnknown
	// the function value itself may be a complex expression.
	var fnExpr core.Expr
	if !isMethod {
		fnExpr = l.expr(s, e.Fn)
	}
	args := l.argAtoms(s, e.Args)
	return l.finishCall(s, e.Sp, e.Type(), e.Fn, fnExpr, recv, ci, args, hasSpread(e.Args))
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
		return l.unknownCall(s, sp, fnExpr, args)
	case check.CallGo:
		return l.goCall(s, sp, resTyp, ci, args, recv, spread)
	default:
		panic("unreachable")
	}
}

// declOrderArgs reorders source-order argument atoms into
// declaration order per ArgToParam. The result has room for Arity
// entries plus any over-application extras, which keep source order.
// Holes leave nil slots.
func declOrderArgs(ci check.CallInfo, args []core.Atom) []core.Atom {
	n := len(args)
	if ci.Arity > n {
		n = ci.Arity
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
	ordered := declOrderArgs(ci, args)
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
		// the returned function value to the extras — in flat-arity
		// chunks like CallUnknown, so a remainder under the return's
		// arity becomes a Closure rather than a partial CallFunc.
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
		}
	case resolve.KindBuiltin:
		var flat []core.Atom
		for _, a := range ordered {
			if a != nil {
				flat = append(flat, a)
			}
		}
		return &core.CallBuiltin{
			Base: core.Base{Sp: sp, Typ: resTyp},
			Sym:  sym, Args: flat,
		}
	default:
		l.err(sp, fmt.Sprintf("CallDirect callee %s has kind %s", sym.Name, sym.Kind))
		return l.litUnit(sp)
	}
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
	return l.closureOf(sp, resTyp, paramTyps, func(bs *seq, pv []core.Atom) core.Expr {
		ordered := declOrderArgs(ci, args)
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
			}
		case resolve.KindBuiltin:
			var flat []core.Atom
			for _, a := range ordered[:ci.Arity] {
				if a != nil {
					flat = append(flat, a)
				}
			}
			return &core.CallBuiltin{
				Base: core.Base{Sp: sp, Typ: retAfter(calleeSig, ci.Arity)},
				Sym:  sym, Args: flat,
			}
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

// unknownCall applies a function value in flat chunks of its own
// arity — never a curried intermediate, never a partial application.
func (l *lowerer) unknownCall(s *seq, sp diag.Span, fnExpr core.Expr, args []core.Atom) core.Expr {
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
	return l.callFuncChunks(s, sp, fn, args)
}

// goCall lowers a Go call. A `_` hole becomes the parameter of a
// cute-style Closure; a multi-result call is wrapped in its consumer.
func (l *lowerer) goCall(s *seq, sp diag.Span, resTyp types.Type, ci check.CallInfo, args []core.Atom, recv core.Atom, spread bool) core.Expr {
	sig := l.goSig(ci.Callee, resTyp)
	if sig.Arity == 0 && !sig.Variadic {
		// The Env oracle did not know the signature: trust CallInfo.
		sig.Arity = ci.Arity
		sig.Variadic = ci.Variadic
		if sig.Results == 0 {
			sig.Results = goResultsOf(resTyp)
		}
	}
	ordered := l.convGoArgs(s, sig, declOrderArgs(ci, args), spread)

	if ci.HoleIndex >= 0 {
		// Cute partial application: the hole is the closure's single
		// parameter; the other (already evaluated) arguments are
		// captured by the generated closure body.
		remTyp := sigOf(resTyp)
		if remTyp == nil || len(remTyp.Params) != 1 {
			l.err(sp, fmt.Sprintf("cute _ call typed %s, not a one-parameter function", resTyp))
			return l.litUnit(sp)
		}
		return l.closureOfType(sp, remTyp, func(bs *seq, pv []core.Atom) core.Expr {
			// HoleIndex counts in *written* argument order — fill
			// the parameter there before reordering into
			// declaration order, or labeled arguments pull the
			// hole to the wrong slot and leave a nil atom.
			written := append([]core.Atom{}, args...)
			if ci.HoleIndex < len(written) {
				written[ci.HoleIndex] = pv[0]
			} else {
				written = append(written, pv[0])
			}
			filled := l.convGoArgs(bs, sig, declOrderArgs(ci, written), spread)
			call := &core.CallGo{
				Base:   core.Base{Sp: sp, Typ: goCallTyp(sig, remTyp.Ret)},
				Callee: ci.Callee, Recv: recv, TypeArgs: ci.TypeArgs,
				Args:  filled,
				Arity: ci.Arity, Variadic: ci.Variadic,
				Spread: spread, Results: sig.Results,
			}
			x := l.wrapGoResult(call, remTyp.Ret)
			if sig.Results == core.GoResultsOne {
				return l.goPayloadConv(bs, sp, call.Typ, remTyp.Ret, x)
			}
			return x
		})
	}

	call := &core.CallGo{
		Base:   core.Base{Sp: sp, Typ: goCallTyp(sig, resTyp)},
		Callee: ci.Callee, Recv: recv, TypeArgs: ci.TypeArgs,
		Args:  ordered,
		Arity: ci.Arity, Variadic: ci.Variadic,
		Spread: spread, Results: sig.Results,
	}
	x := l.wrapGoResult(call, resTyp)
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
	var recv core.Atom
	if ci.Callee != nil && ci.Callee.Kind == resolve.KindMethod {
		if m, ok := ap.Fn.(*check.MemberExpr); ok {
			recv = l.atom(s, m.Recv)
		}
	} else {
		l.expr(s, ap.Fn)
	}
	args := l.argAtoms(s, ap.Args)
	sig := l.goSig(ci.Callee, ap.Type())
	if sig.Results == 0 {
		sig.Results = goResultsOf(ap.Type())
	}
	spread := hasSpread(ap.Args)
	return &core.CallGo{
		Base:   core.Base{Sp: ap.Sp, Typ: goCallTyp(sig, ap.Type())},
		Callee: ci.Callee, Recv: recv, TypeArgs: ci.TypeArgs,
		Args:  l.convGoArgs(s, sig, declOrderArgs(ci, args), spread),
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
	// The expression's own type spells the value's Go shape —
	// `func(bound) residual` — so arity readers see the immediate
	// count: `add3 40` emits `func(b) func(c)`, and its type must be
	// `Int -> (Int -> Int)`, not the flat `Int -> Int -> Int` that
	// both spellings Equal-compare to (check.go keeps declared sigs
	// nested for the same reason; this literal preserves the split).
	exprTyp := &types.Func{Params: params, Ret: fn.ResidualType()}
	return &core.Closure{Base: core.Base{Sp: sp, Typ: exprTyp}, Fn: fn}
}

// closureOfType is closureOf for a signature the lowering already
// holds — the parameter types come from the Func itself.
func (l *lowerer) closureOfType(sp diag.Span, sig *types.Func, build func(bs *seq, pv []core.Atom) core.Expr) *core.Closure {
	return l.closureOf(sp, sig, sig.Params, build)
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
			return l.closureOfType(sp, resFunc(sigOf(f.Type()), len(args), f.Type()), func(bs *seq, pv []core.Atom) core.Expr {
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
			}
		}
		if len(args) > arity {
			l.err(sp, fmt.Sprintf("constructor %s over-applied (%d/%d)", f.Sym.Name, len(args), arity))
			return l.litUnit(sp)
		}
		return l.closureOfType(sp, resFunc(sigOf(f.Type()), len(args), f.Type()), func(bs *seq, pv []core.Atom) core.Expr {
			return &core.CtorApp{
				Base: core.Base{Sp: sp, Typ: retAfter(sigOf(f.Type()), arity)},
				Ctor: f.Sym, TypeArgs: f.TypeArgs,
				Args:  append(append([]core.Atom{}, args...), pv...),
				Arity: arity,
			}
		})
	case *core.GoFuncRef:
		if len(args) != f.Arity {
			l.err(sp, fmt.Sprintf("Go function %s applied at %d args, arity %d — partial Go application needs a Closure", f.Sym.Name, len(args), f.Arity))
			return l.litUnit(sp)
		}
		call := &core.CallGo{
			Base:   core.Base{Sp: sp, Typ: goCallTyp(GoSig{Results: f.Results}, resTyp)},
			Callee: f.Sym, Recv: f.Recv, TypeArgs: f.TypeArgs,
			Args:    args,
			Arity:   f.Arity,
			Results: f.Results,
		}
		return l.wrapGoResult(call, resTyp)
	case *core.Var, *core.Lit, *core.FieldGet:
		return l.callFuncChunks(s, sp, fn, args)
	default:
		panic("unreachable")
	}
}

// callFuncChunks applies an unknown function value in arity-sized
// CallFunc chunks — the CallUnknown path for atoms.
func (l *lowerer) callFuncChunks(s *seq, sp diag.Span, fn core.Atom, args []core.Atom) core.Expr {
	for len(args) > 0 {
		fs, ok := types.Chase(fn.Type()).(*types.Func)
		if !ok {
			l.err(sp, fmt.Sprintf("applying a non-function value of type %s", fn.Type()))
			return l.litUnit(sp)
		}
		k := len(fs.Params)
		if len(args) > k {
			call := &core.CallFunc{
				Base: core.Base{Sp: sp, Typ: fs.Ret},
				Fn:   fn, Args: args[:k],
			}
			fn = l.asAtom(s, sp, call)
			args = args[k:]
			continue
		}
		if len(args) < k {
			rem := &types.Func{Params: fs.Params[len(args):], Ret: fs.Ret}
			full := append([]core.Atom{}, args...)
			return l.closureOfType(sp, rem, func(bs *seq, pv []core.Atom) core.Expr {
				return &core.CallFunc{
					Base: core.Base{Sp: sp, Typ: fs.Ret},
					Fn:   fn, Args: append(full, pv...),
				}
			})
		}
		return &core.CallFunc{
			Base: core.Base{Sp: sp, Typ: fs.Ret},
			Fn:   fn, Args: args,
		}
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
	default:
		return &core.Binary{
			Base: baseOf(e), Op: e.Op,
			X: l.atom(s, e.X), Y: l.atom(s, e.Y),
		}
	}
}

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
		ci := ap.Call
		var recv core.Atom
		if ci.Callee != nil && ci.Callee.Kind == resolve.KindMethod {
			if m, ok := ap.Fn.(*check.MemberExpr); ok {
				recv = l.atom(s, m.Recv)
			}
		}
		var fnExpr core.Expr
		if recv == nil {
			fnExpr = l.expr(s, ap.Fn)
		}
		args := l.argAtoms(s, ap.Args)
		args = append(args, piped)
		appendArg(&ci)
		return l.finishCall(s, rhs.Span(), e.Type(), ap.Fn, fnExpr, recv, ci, args, hasSpread(ap.Args))
	}
	fn := l.atom(s, rhs)
	return l.applyAtom(s, rhs.Span(), e.Type(), fn, []core.Atom{piped})
}

// pipeApplyBack lowers `rhs <| x` — identical convention to |>,
// evaluated left to right: the callee side first, then the argument.
func (l *lowerer) pipeApplyBack(s *seq, e *check.Binary) core.Expr {
	if ap, ok := e.X.(*check.Apply); ok {
		ci := ap.Call
		var recv core.Atom
		if ci.Callee != nil && ci.Callee.Kind == resolve.KindMethod {
			if m, ok := ap.Fn.(*check.MemberExpr); ok {
				recv = l.atom(s, m.Recv)
			}
		}
		var fnExpr core.Expr
		if recv == nil {
			fnExpr = l.expr(s, ap.Fn)
		}
		args := l.argAtoms(s, ap.Args)
		args = append(args, l.atom(s, e.Y))
		appendArg(&ci)
		return l.finishCall(s, e.X.Span(), e.Type(), ap.Fn, fnExpr, recv, ci, args, hasSpread(ap.Args))
	}
	fn := l.atom(s, e.X)
	return l.applyAtom(s, e.Sp, e.Type(), fn, []core.Atom{l.atom(s, e.Y)})
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
	return l.closureOf(e.Sp, e.Type(), sig.Params, func(bs *seq, pv []core.Atom) core.Expr {
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
				return l.wrapGoResult(call, ap.Type())
			}
			return l.goPayloadConv(s, e.Sp, call.Typ, e.Type(), &core.EscapeCall{
				Base: core.Base{Sp: e.Sp, Typ: call.Typ},
				Call: call, To: fn, Wrap: "",
			})
		}
		// Non-escapable shape — fall through to EscapeResult, which
		// the validator will report against the operand's type.
		return &core.EscapeResult{
			Base: core.Base{Sp: e.Sp, Typ: e.Type()},
			X:    l.asAtom(s, e.Sp, l.wrapGoResult(call, ap.Type())),
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
	if a, _, ok := resultArgs(operand.Type()); ok {
		typ = a
	}
	return &core.EscapeResult{
		Base: core.Base{Sp: e.Sp, Typ: typ},
		X:    operand, To: fn, Wrap: "",
	}
}

// pipeTry lowers `x |?> step`. Under the escape-boundary semantics
// (design.md section 4.6) the whole expression still evaluates to a
// `Result b e`: the incoming Result is unwrapped by an escape, the
// step is applied, and its Result is unwrapped by a second escape
// whose payload is re-wrapped in Ok. Errors escape the innermost
// function boundary — this is where `|?>` diverges from "evaluate to
// Err and continue" mid-body; see
// docs/problems/2026-11-02-pipe-try-semantics.md.
func (l *lowerer) pipeTry(s *seq, e *check.Binary) core.Expr {
	// The incoming value unwraps with a passthrough escape — the
	// |?>-step wrap applies to the step's own failure, never to the
	// inflow (spec 9.2 items 1, 5 and 7). A direct Go `(T, error)`
	// operand fuses to EscapeCall so no Result materialises.
	fn := l.curFn()
	if fn == nil {
		// Report once; the escapes below degrade to their unescaped
		// operands rather than emitting target-less nodes.
		l.err(e.Sp, "|?> escapes with no enclosing function boundary")
	}
	var in core.Atom
	if ap, ok := e.X.(*check.Apply); ok && ap.Call.Kind == check.CallGo && ap.Call.HoleIndex < 0 {
		call := l.goCallNode(s, ap)
		if fn != nil && (call.Results == core.GoResultsErr || call.Results == core.GoResultsErrOnly) {
			in = l.asAtom(s, e.X.Span(), &core.EscapeCall{
				Base: core.Base{Sp: e.X.Span(), Typ: call.Typ},
				Call: call, To: fn, Wrap: "",
			})
			in = l.convAtomTo(s, e.X.Span(), in, payloadOf(ap.Type()))
		} else {
			in = l.asAtom(s, e.X.Span(), l.wrapGoResult(call, ap.Type()))
		}
	}
	if in == nil {
		lv := l.atom(s, e.X)
		in = lv
		if _, _, isRes := resultArgs(lv.Type()); isRes && fn != nil {
			in = l.asAtom(s, e.Sp, &core.EscapeResult{
				Base: core.Base{Sp: e.X.Span(), Typ: payloadOf(lv.Type())},
				X:    lv, To: fn, Wrap: "",
			})
		}
	}
	step := l.stepApply(s, e, e.Y, in)
	return l.escapeStep(s, e, step)
}

// stepApply applies a |?> step to the unwrapped payload — the same
// shape as |>: an Apply rhs gets the payload appended, anything else
// is a function value applied once. resTyp is the |?> node's type —
// the step's full-application result.
func (l *lowerer) stepApply(s *seq, e *check.Binary, rhs check.Expr, in core.Atom) core.Expr {
	if ap, ok := rhs.(*check.Apply); ok {
		ci := ap.Call
		var recv core.Atom
		if ci.Callee != nil && ci.Callee.Kind == resolve.KindMethod {
			if m, ok := ap.Fn.(*check.MemberExpr); ok {
				recv = l.atom(s, m.Recv)
			}
		}
		var fnExpr core.Expr
		if recv == nil {
			fnExpr = l.expr(s, ap.Fn)
		}
		args := l.argAtoms(s, ap.Args)
		args = append(args, in)
		appendArg(&ci)
		return l.finishCall(s, rhs.Span(), e.Type(), ap.Fn, fnExpr, recv, ci, args, hasSpread(ap.Args))
	}
	fn := l.atom(s, rhs)
	return l.applyAtom(s, rhs.Span(), e.Type(), fn, []core.Atom{in})
}

// escapeStep turns the step's Result value into the |?> expression's
// own Result-typed value: `Ok (escape-with-wrap step)`. A direct Go
// `(T, error)` step fuses to EscapeCall instead of materializing the
// Result. The payload is then wrapped in the `Ok` constructor so the
// whole expression still has type `Result b e` at the boundary.
func (l *lowerer) escapeStep(s *seq, e *check.Binary, step core.Expr) core.Expr {
	wrap := ""
	if _, e2, ok := resultArgs(e.Type()); ok && isGoError(e2) {
		wrap = l.stepName(e.Y)
	}
	// pipeTry already reported a missing boundary; degrade to the
	// unescaped step value rather than emitting a target-less node.
	fn := l.curFn()
	var payload core.Atom
	if call, ok := unwrapToCallGo(step); ok && fn != nil &&
		(call.Results == core.GoResultsErr || call.Results == core.GoResultsErrOnly) {
		payload = l.asAtom(s, e.Sp, &core.EscapeCall{
			Base: core.Base{Sp: e.Sp, Typ: call.Typ},
			Call: call, To: fn, Wrap: wrap,
		})
		payload = l.convAtomTo(s, e.Sp, payload, payloadOf(e.Type()))
	} else {
		stepAtom := l.asAtom(s, e.Y.Span(), step)
		if fn == nil {
			payload = stepAtom
		} else {
			payload = l.asAtom(s, e.Sp, &core.EscapeResult{
				Base: core.Base{Sp: e.Sp, Typ: payloadOf(stepAtom.Type())},
				X:    stepAtom, To: fn, Wrap: wrap,
			})
		}
	}
	okSym := l.ctorSym("Ok")
	return &core.CtorApp{
		Base: core.Base{Sp: e.Sp, Typ: e.Type()},
		Ctor: okSym,
		Args: []core.Atom{payload}, Arity: 1,
	}
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

func payloadOf(t types.Type) types.Type {
	if a, _, ok := resultArgs(t); ok {
		return a
	}
	return t
}

// isGoError reports whether the error half of a Result is the
// builtin GoError type — the only wrap-eligible shape (the Core
// validator rejects a non-empty Wrap for any other error type).
func isGoError(t types.Type) bool {
	return types.Equal(t, types.GoError)
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
	if c, ok := types.Chase(e.Iter.Type()).(*types.Con); ok && c.Qual == "" {
		// Qual == "" narrows to builtin-space names — a same-file
		// `type Chan` still misclassifies (the checker's own
		// chanElem has the same ambiguity); filed as a contract
		// gap.
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
	return &core.Conv{
		Base: baseOf(e),
		Kind: convKind(e.X.Type(), e.Type()),
		X:    l.atom(s, e.X),
	}
}

// convKind picks the conversion kind for `Go.conv` — numeric between
// the numeric constructors, named-type conversion otherwise (the
// checked legality table is the spec 8.4 contract).
func convKind(from, to types.Type) core.ConvKind {
	if isNumericCon(from) && isNumericCon(to) {
		return core.ConvNum
	}
	return core.ConvNamed
}

// isNumericCon reports whether t is one of the numeric builtin
// constructors `Go.conv` may convert between.
func isNumericCon(t types.Type) bool {
	c, ok := types.Chase(t).(*types.Con)
	if !ok {
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

// lambdaExpr lowers `fn p... -> body` to a Closure.
func (l *lowerer) lambdaExpr(e *check.Lambda) core.Expr {
	// The Closure's Sig is the canonical flat arrow chain — core
	// keeps the declared arity in Params instead of a nested Ret.
	sig := flatSig(e.Type())
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

// --- tail calls ---

// markTailCalls rewrites self-recursive CallDirect nodes sitting in
// the function's own tail position into TailCall, then sets TailLoop.
// Runs after the body is lowered so it sees the final tree shape;
// nested functions keep their own boundary — their CallDirect nodes
// are not in To's tail position.
func (l *lowerer) markTailCalls(fn *core.Func, self *resolve.Symbol) {
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
			if e.Callee == self && len(e.Args) == len(fn.Params) {
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
}
