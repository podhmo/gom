package check

import (
	"fmt"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// goCalleeSym returns the Go symbol a bare pipe callee denotes —
// a Var or MemberExpr bound to a Go function or a method — or nil.
// It is the bare-reference counterpart of calleeOf's classification.
func goCalleeSym(e Expr) *resolve.Symbol {
	var sym *resolve.Symbol
	switch e := e.(type) {
	case *Var:
		sym = e.Sym
	case *MemberExpr:
		sym = e.Sym
	case *Apply, *Ascribe, *Binary, *Block, *FieldAssign, *For,
		*GoExpr, *Hole, *If, *Ignore, *Lambda, *Let, *ListLit,
		*Lit, *Match, *RecordLit, *SelectExpr, *Spread,
		*StrInterp, *Try, *TupleLit, *Unary:
		return nil
	default:
		panic("unreachable")
	}
	if sym != nil && (sym.Kind == resolve.KindMethod || sym.IsGo) {
		return sym
	}
	return nil
}

// checkPipeCallee checks the callee half of a pipe. Exact record
// expectations use the pipe result as the callee residual's hard type;
// a Go.Slice expectation instead stays a soft propagation hint because
// the pipe may produce a List and cross the boundary only afterwards.
func (c *checker) checkPipeCallee(callee, arg syntax.Expr, want types.Type, sp diag.Span) Expr {
	if want == nil || !c.containsRecordType(want) ||
		(!applyHasAnonRecordLit(callee) && !anonRecordLitInResultPosition(arg)) {
		return c.checkExpr(callee, nil)
	}
	if len(c.boundaryProducerWants(want)) == 0 {
		input := c.newMeta(sp, "the piped argument's type")
		return c.checkExpr(callee, types.Fn(want, types.P(input)))
	}
	if _, ok := callee.(*syntax.Apply); !ok {
		return c.checkExpr(callee, nil)
	}
	savedWant := c.pipeApplyExpectation
	savedBoundary := c.pipeApplyBoundary
	c.pipeApplyExpectation = types.Fn(want, types.P(c.untypedMeta()))
	c.pipeApplyBoundary = true
	out := c.checkExpr(callee, nil)
	c.pipeApplyExpectation = savedWant
	c.pipeApplyBoundary = savedBoundary
	return out
}

// pipeArgExpectation treats the pipe itself as the application `fn arg`
// for record-context propagation. This covers a bare higher-order
// callee whose callback is the piped operand (`Scope.run <| fn _ -> ...`):
// the statically known pipe result can solve the callback's result slot
// before that operand is checked.
func (c *checker) pipeArgExpectation(fn Expr, arg syntax.Expr, want types.Type, sp diag.Span) types.Type {
	if want != nil && anonRecordLitInResultPosition(arg) {
		params, _ := flattenFunc(fn.Type())
		if len(params) > 0 {
			expectations := c.applyParamExpectationsImpl(fn.Type(), params, []syntax.Arg{{Value: arg}}, want, sp, true)
			if got, ok := expectations.at(0, params[0].Type); ok {
				return got
			}
		}
	}
	return c.pipeArgWant(fn)
}

func applyHasAnonRecordLit(e syntax.Expr) bool {
	ap, ok := e.(*syntax.Apply)
	if !ok {
		return false
	}
	for _, a := range ap.Args {
		if anonRecordLitInResultPosition(a.Value) || applyHasAnonRecordLit(a.Value) {
			return true
		}
	}
	return false
}

// pipeArgWant is the want a piped argument checks under — the
// callee's first positional parameter when it carries `Go.Any` or a
// record type and the callee takes the generic application path,
// mirroring checkGomApply's `checkExpr(arg, paramType)`. The record
// seed lets a type-prefixed or anonymous record literal settle before
// pipeCall applies the callee. When that seed itself fails, the
// caller passes the fact to pipeCall so the same mismatch is not
// echoed at the pipe's span. A bare Go callee or a saturated Go call
// result answers nil: pipeCall's Go-boundary branches perform their
// own conformExpr under goWant, and a premature want would skip the
// live-parameter mark chanDirConv needs.
func (c *checker) pipeArgWant(fn Expr) types.Type {
	if fs, ok := types.Chase(fn.Type()).(*types.Func); ok &&
		len(fs.Params) > 0 && fs.Params[0].Label == "" {
		param := fs.Params[0].Type
		// A higher-order callback whose result carries a record type
		// needs the seed even when the callee itself is a Go boundary
		// function (`Scope.run`): the boundary conversion special case
		// below does not apply to the callback value, and without the
		// seed its result-position record literal cannot settle.
		if _, callback := types.Chase(param).(*types.Func); callback && c.containsRecordType(param) {
			return param
		}
		saturatedGoCall := false
		if ap, ok := fn.(*Apply); ok && ap.Call.Kind == CallGo && ap.Call.HoleIndex < 0 {
			saturatedGoCall = true
		}
		if goCalleeSym(fn) == nil && !saturatedGoCall &&
			(containsGoAny(types.Chase(param)) || c.containsRecordType(param)) {
			return param
		}
	}
	return nil
}

// checkPipeOperand checks one piped operand and reports whether its
// trailing conformance to a record seed actually failed. The failure
// is observed directly rather than inferred from type inequality:
// successful conformance can leave nested function labels different
// through introduction, and that successful value must still go
// through pipeCall's ordinary application judgment.
func (c *checker) checkPipeOperand(e syntax.Expr, want types.Type) (Expr, bool) {
	if want == nil || !c.isRecordType(want) {
		return c.checkExpr(e, want), false
	}
	sink := map[Expr]bool{}
	saved := c.wantFailureSink
	c.wantFailureSink = sink
	out := c.checkExpr(e, want)
	c.wantFailureSink = saved
	return out, sink[out]
}

// pipeCall types `x |> f` and `f BACKPIPE x`: f applied to x.
// arg/fn are the checked operands in application roles (for BACKPIPE
// they arrive swapped relative to source order).
//
// An ordinary application would make x the first argument of a
// fresh call; two Go-callee shapes bend that rule so the spelling
// matches the calling convention lowering emits:
//
//   - A bare variadic callee of arity 1 (`fmt.Println`) receives x
//     as one variadic element — checked against the element type of
//     the declared slice parameter, never the slice itself — and the
//     pipe has the callee's return type. `Println BACKPIPE "hi"` is
//     `Println("hi")`, not `Println("hi"...)`.
//   - A partially applied Go call (`Println "a" BACKPIPE x`)
//     appends x as the next argument — again as one variadic
//     element when the open slot is the variadic parameter. A
//     saturated non-variadic call cannot take another argument —
//     the same arity diagnostic an over-applied Go call reports.
//
// Everything else goes through the generic function-type rule:
// f must be `x's type -> ret` for a fresh ret meta.
func (c *checker) pipeCall(e *syntax.Binary, arg, fn Expr, seededParamFailed bool) Expr {
	out := &Binary{Base: baseOf(e.Sp, nil), Op: e.Op, X: arg, Y: fn}
	if e.Op == syntax.BinPipeBack {
		out.X, out.Y = fn, arg
	}
	if sym := goCalleeSym(fn); sym != nil {
		if sig, ok := c.sigOf(sym, e.Sp); ok {
			params, ret := flattenFunc(fn.Type())
			arity := c.declArity(sig, len(params))
			switch {
			case sig.Variadic && arity == 1 && len(params) == 1:
				c.unify(sliceElemOf(params[0].Type, arg.Span(), c), arg.Type(), arg.Span())
				out.Base = baseOf(e.Sp, ret)
				return out
			case arity == 0 && !sig.Variadic:
				// `x |> f` for a Go callee taking no argument is
				// the one-argument over-application — the same
				// diagnostic the application form reports.
				c.diags.Add(diag.Error("E0401",
					"the Go callee takes 0 argument(s), got 1").
					At(e.Sp).Set("expected", "0").Set("actual", "1"))
				out.Base = baseOf(e.Sp, ret)
				return out
			case arity == 1 && len(params) == 1:
				// `x |> f` where f is a one-parameter Go callee:
				// the piped value conforms to the declared
				// parameter as a Go-boundary argument — `Chan a`
				// narrows to `chan<-`/`<-chan` (chanDirConv), a
				// `List` copies to `[]T`, Go interfaces assign —
				// exactly what `f x` admits (spec 8.5/10.2). The
				// generic path below applies the callee by plain
				// unify and would bypass every conversion.
				undo := c.goWant(params[0].Type)
				conv := c.conformExpr(arg, params[0].Type, arg.Span())
				undo()
				if e.Op == syntax.BinPipeBack {
					out.Y = conv
				} else {
					out.X = conv
				}
				out.Base = baseOf(e.Sp, ret)
				return out
			}
		}
	}
	// A call carrying a `_` placeholder keeps a function residual
	// (`holeT -> ret`), so it takes the generic path like any other
	// function value — the pipe fills the hole at lowering. The
	// special cases below are the *saturated* Go calls: a variadic
	// one can absorb x as another element; a non-variadic one is
	// over-applied — the same diagnostic the application form gives.
	if ap, ok := fn.(*Apply); ok && ap.Call.Kind == CallGo && ap.Call.HoleIndex < 0 {
		params, _ := flattenFunc(ap.Fn.Type())
		switch {
		case ap.Call.Variadic && ap.Call.Arity > 0 && len(params) == ap.Call.Arity:
			// The open slot is the variadic parameter: x is one
			// element of it.
			c.unify(sliceElemOf(params[ap.Call.Arity-1].Type, arg.Span(), c), arg.Type(), arg.Span())
		default:
			// A saturated non-variadic Go call — over-applied. A
			// partial one (Applied < Arity) already carries its own
			// arity diagnostic; another would only echo it with a
			// self-contradicting "got" count.
			if ap.Call.Applied >= ap.Call.Arity {
				c.diags.Add(diag.Error("E0401",
					fmt.Sprintf("the Go callee takes %d argument(s), got %d", ap.Call.Arity, ap.Call.Applied+1)).
					At(e.Sp).Set("expected", fmt.Sprintf("%d", ap.Call.Arity)).Set("actual", fmt.Sprintf("%d", ap.Call.Applied+1)))
			}
		}
		out.Base = baseOf(e.Sp, ap.Type())
		return out
	}
	ret := c.newMeta(e.Sp, "the pipe's result type")
	// The piped argument conforms to the callee's first parameter
	// the way `fn arg` would — a `Go.Any` parameter boxes (a
	// numeric materializes first), an interface parameter takes
	// the boundary conversion; the type-only application check
	// never wrapped the argument. A failed check already reported
	// the mismatch — wrapping again would echo it at the arg span.
	// When the operand already failed the record seed, skip only
	// the duplicate parameter judgment but still settle the result
	// tail, so recovery does not pile an E0407 onto the one honest
	// mismatch.
	if seededParamFailed {
		c.conformCalleeAfterParamFailure(fn.Type(), ret, e.Sp)
	} else if c.conformCallee(fn.Type(), arg.Type(), ret, e.Sp, true, arg) {
		conv := c.conformFirstArg(fn.Type(), arg)
		if e.Op == syntax.BinPipeBack {
			out.Y = conv
		} else {
			out.X = conv
		}
	}
	out.Base = baseOf(e.Sp, ret)
	return out
}

// checkPipeTry types the Result-bind pipeline `x |?> f`: when x is
// `Result a e` it unwraps to a, f must be `a -> Result b e`, and the
// whole expression is `Result b e`. When x is not a Result it is
// wrapped in Ok first, per spec 9.2.7.
func (c *checker) checkPipeTry(e *syntax.Binary) Expr {
	x := c.checkExpr(e.X, nil)
	f := c.checkExpr(e.Y, nil)
	// `|?>` wraps the step in the builtin Result and unwraps the
	// operand through its tag machinery — both meanings are the
	// file-local `type Result` when one shadows it, so the whole
	// pipeline is E0415.
	if c.shadowedBuiltin("Result") {
		c.shadowedErr(e.Sp, "`|?>`", "Result")
	}
	var argT, errT types.Type
	// Numeric/ordered metas cannot be Result. Unlike try, a pipeline
	// accepts these as its initial unwrapped value (spec 9.2.7).
	canBeResult := true
	if m, ok := types.Chase(x.Type()).(*types.Meta); ok {
		r := c.recOf(m)
		canBeResult = !r.num && !r.ord && !r.flo && !r.intg
	}
	var a, er types.Type
	var isResult bool
	if canBeResult {
		a, er, isResult = c.resultArgs(x.Type(), e.X.Span())
	}
	if isResult {
		argT, errT = a, er
	} else {
		// Non-Result left side: the value is used as-is (Ok-wrapped
		// at evaluation). The step still returns a Result.
		argT = x.Type()
		errT = c.newMeta(e.Sp, "the pipeline error type")
	}
	resT := c.newMeta(e.Sp, "the |?> result type")
	// A step whose own signature returns a concrete non-Result type
	// can never satisfy `a -> Result b e` — name the real mistake
	// (spec 9.2.8) instead of leaving an opaque `Result ?1 ?2`
	// mismatch. Quieting the metas first suppresses the generic
	// E0401 conformCallee would emit for the same unification;
	// argument-side mismatches still report.
	if rt, isType := nonResultStepRet(f.Type()); isType && !c.shadowedBuiltin("Result") {
		c.markQuiet(resT)
		c.markQuiet(errT)
		name := "the step"
		if s, ok := f.(*Var); ok {
			name = "`" + s.Sym.Name + "`"
		} else if s, ok := f.(*MemberExpr); ok {
			name = "`" + s.Sym.Name + "`"
		}
		rtName := types.Display(rt, metaNamer())
		c.diags.Add(diag.Error("E0401",
			fmt.Sprintf("a |?> step must return Result, but %s returns %s", name, rtName)).
			At(e.Y.Span()).Set("expected", "Result").Set("actual", rtName))
	}
	// The step's argument is the unwrapped payload — a value the
	// lowering produces, not an expression the Ascribe can hang on —
	// so boundary conversions stay unify-only here (conv=false).
	if !c.conformCallee(f.Type(), argT, types.Result(resT, errT), e.Y.Span(), false, nil) {
		c.markQuiet(resT)
		c.markQuiet(errT)
	}
	return &Binary{Base: baseOf(e.Sp, types.Result(resT, errT)), Op: e.Op, X: x, Y: f}
}

// nonResultStepRet returns the step's ultimate return type when it
// is a concrete type that cannot be the builtin `Result` — a Con of
// another name, a tuple, a function, or a Go type. Metas and rigid
// variables stay undecided for conformCallee.
func nonResultStepRet(t types.Type) (types.Type, bool) {
	f, ok := types.Chase(t).(*types.Func)
	if !ok {
		return nil, false
	}
	ret := f.Ret
	for {
		if nf, ok := types.Chase(ret).(*types.Func); ok {
			ret = nf.Ret
			continue
		}
		break
	}
	switch rt := types.Chase(ret).(type) {
	case *types.Con:
		if rt.Qual == "" && rt.Name == "Result" && len(rt.Args) == 2 {
			return nil, false
		}
		return rt, true
	case *types.Tuple, *types.Go:
		return rt, true
	case *types.Meta, *types.Var, *types.Func:
		return nil, false
	default:
		panic("unreachable")
	}
}
