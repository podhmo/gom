package check

import (
	"fmt"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// checkApply types a juxtaposition application `f a b ~l: c` and
// attaches the validated CallInfo — the calling-convention decision
// the rest of the pipeline consumes.
//
// Classification:
//
//   - the callee resolves to a Go function or method (KindFunc /
//     KindMethod flagged IsGo) → CallGo: Go calling convention, no
//     currying; the surface argument tuple is unpacked into the
//     individual Go arguments.
//   - a known Gom callee (a declared or imported fn, a variant
//     constructor, a builtin) saturated or over-applied → CallDirect;
//     under-applied → CallPartial.
//   - anything else — a local variable holding a function, the result
//     of an expression — → CallUnknown: curried application.
//
// `Go.conv` is not an ordinary call: `Go.conv e : T` reaches the
// checker as an ascription over this node, so a bare `Go.conv e`
// reports E0507.
func (c *checker) checkApply(e *syntax.Apply) Expr {
	fn, callee, goCall := c.calleeOf(e.Fn)
	if callee != nil && isConv(callee) {
		return c.checkConvCall(e, fn)
	}
	params, ret := flattenFunc(fn.Type())
	if goCall {
		sig, _ := c.sigOf(callee, e.Sp)
		arity := c.declArity(sig, len(params))
		return c.checkGoApply(e, fn, callee, params, ret, sig.Variadic, arity)
	}
	arity := len(params)
	if callee != nil {
		if sig, ok := c.sigOf(callee, e.Sp); ok {
			arity = c.declArity(sig, arity)
		}
	}
	return c.checkGomApply(e, fn, callee, params, ret, arity)
}

// declArity returns the callee's own declared parameter count — what
// CallInfo.Arity records. When the signature keeps the declared
// (unflattened) shape its outer parameter list is the declaration;
// DeclArity pins it explicitly for sources that hand out flattened
// types (e.g. an imported `fn mk () -> (Int -> Int)`).
func (c *checker) declArity(sig Signature, flat int) int {
	if sig.DeclArity > 0 {
		if sig.DeclArity < flat {
			return sig.DeclArity
		}
		return flat
	}
	if f, ok := types.Chase(sig.Type).(*types.Func); ok {
		if n := f.Arity(); n <= flat {
			return n
		}
	}
	return flat
}

// checkConvCall handles `Go.conv e` written without the mandatory
// `: T` ascription — always an error, kept recoverable. The E0507
// itself was already reported by checkSelect, which types the conv
// member in expression position on the way through calleeOf.
func (c *checker) checkConvCall(e *syntax.Apply, fn Expr) Expr {
	out := &Apply{Base: baseOf(e.Sp, c.quietMeta(e.Sp, "the conversion's result")), Fn: fn}
	for _, a := range e.Args {
		var v Expr
		if a.Value != nil {
			v = c.checkExpr(a.Value, nil)
		} else {
			v = c.checkExpr(nil, nil)
		}
		out.Args = append(out.Args, Arg{Value: v})
	}
	out.Call = CallInfo{Kind: CallUnknown, Arity: 0, Applied: len(out.Args), HoleIndex: -1}
	return out
}

// calleeOf resolves the function part of an application. It returns
// the typed callee expression, the callee's symbol when it names a
// known function (nil for an arbitrary function-typed value), and
// whether the Go calling convention applies.
func (c *checker) calleeOf(e syntax.Expr) (Expr, *resolve.Symbol, bool) {
	fn := c.checkExpr(e, nil)
	var sym *resolve.Symbol
	switch fn := fn.(type) {
	case *Var:
		sym = fn.Sym
	case *MemberExpr:
		sym = fn.Sym
	case *Apply, *Ascribe, *Binary, *Block, *For, *GoExpr, *Hole,
		*If, *Ignore, *Lambda, *Let, *ListLit, *Lit, *Match,
		*RecordLit, *SelectExpr, *Spread, *StrInterp, *Try,
		*TupleLit, *Unary:
		// Not a named callee: an arbitrary function-typed value.
	default:
		panic("unreachable")
	}
	if sym == nil {
		return fn, nil, false
	}
	// Only Go *functions* (and methods) use the Go calling
	// convention — a Go package variable or constant is a plain
	// value: `os.Args ()` must fall through to ordinary
	// application diagnostics, not become a CallGo that lowering
	// rejects as an E0901 contract gap.
	if sym.IsGo && (sym.Kind == resolve.KindFunc || sym.Kind == resolve.KindMethod) {
		return fn, sym, true
	}
	switch sym.Kind {
	case resolve.KindFunc, resolve.KindCtor, resolve.KindBuiltin:
		return fn, sym, false
	default:
		// A variable or other value holding a function.
		return fn, nil, false
	}
}

// checkGomApply matches surface arguments against the callee's
// flattened parameter list — positional arguments fill unlabeled
// parameters in declaration order (a positional argument may never
// fill a labeled parameter) and `~label:` arguments fill labeled
// parameters by name — then classifies the call against the callee's
// *declared* arity: under-application is CallPartial, saturation or
// over-application is CallDirect.
func (c *checker) checkGomApply(e *syntax.Apply, fn Expr, callee *resolve.Symbol, params []types.FuncParam, ret types.Type, declArity int) Expr {
	flat := len(params)
	if declArity > flat {
		declArity = flat
	}
	used := make([]bool, flat)
	posIdx := 0
	argToParam := make([]int, 0, len(e.Args))
	var extras []Expr // positional args beyond the flat parameter list
	overDeclared := false

	out := &Apply{Base: baseOf(e.Sp, nil), Fn: fn}

	for _, a := range e.Args {
		label := ""
		var labelSp diag.Span
		if a.Label != nil {
			label = a.Label.Name
			labelSp = a.Label.Sp
		}
		pi := -1
		if label != "" {
			pi = c.paramByLabel(params, label)
			if pi < 0 {
				if _, unresolved := types.Chase(fn.Type()).(*types.Meta); unresolved || flat == 0 {
					// A labeled call needs the callee's parameter
					// list: on an unresolved function type that is
					// E0408, not "unknown label".
					c.diags.Add(diag.Error("E0408",
						"a labeled call requires the callee's type to be known").
						At(labelSpan(labelSp, a.Sp)).Set("label", label))
				} else {
					c.diags.Add(diag.Error("E0406",
						fmt.Sprintf("the callee has no parameter labeled `~%s`", label)).
						At(labelSpan(labelSp, a.Sp)).Set("label", label))
				}
			} else if used[pi] {
				c.diags.Add(diag.Error("E0405",
					fmt.Sprintf("the label `~%s` is supplied twice", label)).
					At(labelSpan(labelSp, a.Sp)).Set("label", label))
				pi = -1
			}
		} else {
			for posIdx < flat && (used[posIdx] || params[posIdx].Label != "") {
				posIdx++
			}
			if posIdx < flat {
				pi = posIdx
				posIdx++
			}
		}
		var pt types.Type
		switch {
		case pi >= 0:
			used[pi] = true
			pt = params[pi].Type
		case label == "":
			// A positional argument beyond the callee's parameters
			// over-applies into the returned function value.
			pt = c.newMeta(a.Sp, "an over-applied argument's parameter")
		default:
			// The label already reported E0406/E0408 — the
			// argument's parameter slot is recovery.
			pt = c.quietMeta(a.Sp, fmt.Sprintf("the argument `~%s`", label))
		}
		var argExpr Expr
		switch av := a.Value.(type) {
		case nil:
			// A missing argument value (recovery hole in the
			// surface tree) — same handling as the Go path.
			argExpr = c.checkExpr(nil, pt)
		case *syntax.Hole:
			c.diags.Add(diag.Error("E0504",
				"`_` is only allowed as an argument of a Go call").At(av.Sp))
			// A Hole in Args would fail Apply.Validate on a non-Go
			// call — recover with a unit literal of the slot type.
			argExpr = &Lit{Base: baseOf(av.Sp, pt), Kind: syntax.LitUnit, Text: "()"}
		case *syntax.Spread:
			x := c.checkExpr(av.X, nil)
			c.diags.Add(diag.Error("E0505",
				"`..e` is only allowed as the last argument of a variadic Go call").At(av.Sp))
			argExpr = &Spread{Base: baseOf(av.Sp, x.Type()), X: x}
		case *syntax.Ident, *syntax.Lit, *syntax.StrInterp,
			*syntax.Apply, *syntax.Select, *syntax.Unary,
			*syntax.Binary, *syntax.Lambda, *syntax.If, *syntax.Match,
			*syntax.Block, *syntax.Let, *syntax.Ignore, *syntax.Try,
			*syntax.GoExpr, *syntax.For, *syntax.SelectExpr,
			*syntax.ListLit, *syntax.TupleLit, *syntax.Ascribe,
			*syntax.RecordLit:
			argExpr = c.checkExpr(a.Value, pt)
		default:
			panic("unreachable")
		}
		out.Args = append(out.Args, Arg{Label: label, LabelSp: labelSp, Value: argExpr})
		argToParam = append(argToParam, pi)
		if pi < 0 {
			if label == "" {
				extras = append(extras, argExpr)
			}
			overDeclared = true
		} else if pi >= declArity {
			// The arg filled a parameter of the returned function —
			// still an over-application in CallInfo terms.
			overDeclared = true
		}
	}

	if callee != nil && callee.Kind == resolve.KindBuiltin && callee.Name == "assert_eq" && len(params) > 0 {
		c.constrain(params[0].Type, e.Sp, "the operands of `assert_eq`", func(r *metaRec) { r.eq = true })
	}

	// Positional arguments that outlive the callee's parameters are
	// applied to the returned function; the result type is whatever
	// remains.
	var res types.Type
	if len(extras) > 0 {
		res = c.overApply(ret, extras, e.Sp)
	} else {
		res = applyResidual(fn.Type(), used, 0)
	}
	out.Base = baseOf(e.Sp, res)

	// CallInfo: classify against the *declared* arity. Under-
	// application is CallPartial; saturation and over-application are
	// CallDirect. Unapplied counts by arity minus applied rather than
	// by resolved slots so a call carrying unresolvable arguments
	// (an unknown label already reported as E0406) still yields a
	// structurally valid record. ArgToParam is only representable
	// when every argument hit a declared parameter — over-applied
	// calls (Applied > Arity) and error recoveries leave it nil.
	out.Call = CallInfo{
		Callee:    callee,
		Applied:   len(out.Args),
		Arity:     declArity,
		HoleIndex: -1,
		TypeArgs:  targsOf(fn),
	}
	allMapped := !overDeclared
	if allMapped {
		for _, pi := range argToParam {
			if pi < 0 || pi >= declArity {
				allMapped = false
				break
			}
		}
	}
	switch {
	case callee == nil:
		out.Call.Kind = CallUnknown
		out.Call.Arity = flat
		if allMapped {
			out.Call.ArgToParam = normalizeMapping(argToParam)
		}
	case out.Call.Applied < declArity:
		out.Call.Kind = CallPartial
		out.Call.Unapplied = declArity - out.Call.Applied
		if allMapped {
			out.Call.ArgToParam = normalizeMapping(argToParam)
		}
	default:
		out.Call.Kind = CallDirect
		if allMapped {
			out.Call.ArgToParam = normalizeMapping(argToParam)
		}
	}
	if err := out.Validate(); err != nil {
		panic(fmt.Sprintf("check: invalid CallInfo for %T: %v", e.Fn, err))
	}
	return out
}

// applyResidual rebuilds an application's result type from the
// callee's own — possibly nested — arrow structure. `used` marks the
// flattened parameters (in flattenFunc order) the call filled; the
// residual keeps the declared split level by level: `add3 40` is
// `Int -> (Int -> Int)`, so a later `g 2 0` chunks at immediate
// arity 1 the way the emitted Go value demands. types.Fn would
// flatten that to `Int -> Int -> Int`, and rebuilding from the
// flattened parameter list loses splits declared inside Ret.
func applyResidual(t types.Type, used []bool, off int) types.Type {
	fs, ok := types.Chase(t).(*types.Func)
	if !ok {
		return t
	}
	var rem []types.FuncParam
	for i, p := range fs.Params {
		if off+i >= len(used) || !used[off+i] {
			rem = append(rem, p)
		}
	}
	r := applyResidual(fs.Ret, used, off+len(fs.Params))
	if len(rem) == 0 {
		return r
	}
	return &types.Func{Params: rem, Ret: r}
}

// labelSpan returns a label's own span, falling back to the arg's
// when the label carries no span (hand-built input).
func labelSpan(labelSp, argSp diag.Span) diag.Span {
	if labelSp.File == "" && labelSp.Start == 0 && labelSp.End == 0 {
		return argSp
	}
	return labelSp
}

// overApply applies extra arguments to the function value a saturated
// call returns and yields the remaining result type.
//
// Positional extras pair off with the returned parameters in order —
// including labeled ones, which a positional argument may not fill
// (spec section 2 rule 3). Whether that should be an error or leave
// the labeled parameter in the result is open; see
// docs/problems/2026-11-03-overapply-labeled-params.md.
func (c *checker) overApply(ret types.Type, extras []Expr, sp diag.Span) types.Type {
	rparams, rret := flattenFunc(ret)
	i := 0
	for ; i < len(extras); i++ {
		if i < len(rparams) {
			c.unify(rparams[i].Type, extras[i].Type(), extras[i].Span())
			continue
		}
		// The returned function itself is exhausted: extend the
		// result with a fresh curried step. A failed unify leaves
		// next unsolved — the mismatch already reported it.
		next := c.newMeta(extras[i].Span(), "the over-applied result")
		if !c.unify(rret, types.Fn(next, types.P(extras[i].Type())), extras[i].Span()) {
			c.markQuiet(next)
		}
		rret = next
	}
	if i < len(rparams) {
		return types.Fn(rret, rparams[i:]...)
	}
	return rret
}

// paramByLabel finds the index of a labeled parameter, or -1.
func (c *checker) paramByLabel(params []types.FuncParam, label string) int {
	for i, p := range params {
		if p.Label == label {
			return i
		}
	}
	return -1
}

// normalizeMapping returns the ArgToParam slice, or nil when the
// mapping is the identity or cannot be represented (a -1 entry: an
// argument with no parameter). Validity (distinct, in-range entries)
// is the caller's; ArgToParam is only consulted in call shapes where
// every entry resolved to a declared parameter.
func normalizeMapping(m []int) []int {
	out := make([]int, len(m))
	identity := true
	for i, v := range m {
		out[i] = v
		if v != i {
			identity = false
		}
	}
	if identity {
		return nil
	}
	return out
}

// checkGoApply types a call to a Go function or method — the tuple
// argument convention of spec section 8.1. The surface argument list
// `f(a, b)` (one TupleLit argument) unpacks into the individual Go
// arguments; `f()` passes none. Go calls never curry: every declared
// parameter must be supplied — a labeled one left unfilled is E0404 —
// except a `_` placeholder, which turns the call into a one-argument
// function (the "cute" partial application of spec section 8.2).
func (c *checker) checkGoApply(e *syntax.Apply, fn Expr, callee *resolve.Symbol, params []types.FuncParam, ret types.Type, variadic bool, declArity int) Expr {
	if declArity > len(params) {
		declArity = len(params)
	}
	goParams := params[:declArity]
	arity := declArity
	variadic = variadic && arity > 0

	// Unpack the surface argument form into the Go argument list;
	// labels may arrive on the top-level arguments.
	type garg struct {
		label   string
		labelSp diag.Span
		sp      diag.Span
		x       syntax.Expr
	}
	var gargs []garg
	one := len(e.Args) == 1
	for _, a := range e.Args {
		var label string
		var lsp diag.Span
		if a.Label != nil {
			label, lsp = a.Label.Name, a.Label.Sp
		}
		v := a.Value
		if one && label == "" && v != nil {
			switch v := v.(type) {
			case *syntax.TupleLit:
				for _, el := range v.Elems {
					gargs = append(gargs, garg{sp: el.Span(), x: el})
				}
				continue
			case *syntax.Lit:
				if v.Kind == syntax.LitUnit {
					continue // `f()` — no Go arguments
				}
			case *syntax.Ident, *syntax.StrInterp, *syntax.Apply,
				*syntax.Select, *syntax.Unary, *syntax.Binary,
				*syntax.Hole, *syntax.Lambda, *syntax.If,
				*syntax.Match, *syntax.Block, *syntax.Let,
				*syntax.Ignore, *syntax.Try, *syntax.GoExpr,
				*syntax.For, *syntax.SelectExpr, *syntax.ListLit,
				*syntax.Spread, *syntax.Ascribe, *syntax.RecordLit:
				// An ordinary argument.
			default:
				panic("unreachable")
			}
		}
		gargs = append(gargs, garg{label: label, labelSp: lsp, sp: a.Sp, x: v})
	}

	used := make([]bool, arity)
	posIdx := 0
	tooMany := 0
	posArgs := 0
	hole := -1
	var holeT types.Type
	mapping := make([]int, 0, len(gargs))
	out := &Apply{Base: baseOf(e.Sp, nil), Fn: fn}

	for gi, g := range gargs {
		last := gi == len(gargs)-1
		pi := -1
		if g.label != "" {
			pi = c.paramByLabel(goParams, g.label)
			if pi < 0 {
				c.diags.Add(diag.Error("E0406",
					fmt.Sprintf("the Go callee has no parameter labeled `~%s`", g.label)).
					At(labelSpan(g.labelSp, g.sp)).Set("label", g.label))
			} else if used[pi] {
				c.diags.Add(diag.Error("E0405",
					fmt.Sprintf("the label `~%s` is supplied twice", g.label)).
					At(labelSpan(g.labelSp, g.sp)).Set("label", g.label))
				pi = -1
			}
		} else {
			posArgs++
			for posIdx < arity && (used[posIdx] || goParams[posIdx].Label != "") {
				posIdx++
			}
			if posIdx < arity {
				pi = posIdx
				posIdx++
			} else if variadic {
				pi = arity - 1
			} else {
				tooMany++
			}
		}
		var pt types.Type
		if pi >= 0 {
			used[pi] = true
			pt = goParams[pi].Type
			if variadic && pi == arity-1 {
				// Arguments of the variadic tail have the element
				// type of the declared last (slice) parameter.
				pt = sliceElemOf(pt, g.sp, c)
			}
		} else {
			// Unresolvable slot (unknown label, over-arity,
			// duplicate): E0405/E0406/the arity diagnostic already
			// reported the cause.
			pt = c.quietMeta(g.sp, "the Go argument's parameter type")
		}
		var argExpr Expr
		switch av := g.x.(type) {
		case nil:
			argExpr = c.checkExpr(nil, pt)
		case *syntax.Hole:
			if hole >= 0 {
				c.diags.Add(diag.Error("E0504",
					"at most one `_` placeholder is allowed in a call").At(av.Sp))
				// A second Hole node in Args would fail
				// Apply.Validate (exactly one hole at
				// HoleIndex); recover as an ordinary argument.
				argExpr = &Lit{Base: baseOf(av.Sp, pt), Kind: syntax.LitUnit, Text: "()"}
			} else {
				hole = len(out.Args)
				holeT = pt
				argExpr = &Hole{Base: baseOf(av.Sp, pt)}
			}
		case *syntax.Spread:
			if !variadic || !last || g.label != "" {
				c.diags.Add(diag.Error("E0505",
					"`..e` is only allowed as the last argument of a variadic Go call").At(av.Sp))
				x := c.checkExpr(av.X, nil)
				argExpr = &Spread{Base: baseOf(av.Sp, x.Type()), X: x}
			} else {
				x := c.checkExpr(av.X, nil)
				c.spreadElem(x.Type(), pt, av.Sp)
				argExpr = &Spread{Base: baseOf(av.Sp, x.Type()), X: x}
			}
		case *syntax.Ident, *syntax.Lit, *syntax.StrInterp,
			*syntax.Apply, *syntax.Select, *syntax.Unary,
			*syntax.Binary, *syntax.Lambda, *syntax.If, *syntax.Match,
			*syntax.Block, *syntax.Let, *syntax.Ignore, *syntax.Try,
			*syntax.GoExpr, *syntax.For, *syntax.SelectExpr,
			*syntax.ListLit, *syntax.TupleLit, *syntax.Ascribe,
			*syntax.RecordLit:
			argExpr = c.checkExpr(av, pt)
		default:
			panic("unreachable")
		}
		out.Args = append(out.Args, Arg{Label: g.label, LabelSp: g.labelSp, Value: argExpr})
		mapping = append(mapping, pi)
	}

	// Saturation check: unfilled unlabeled params join a single arity
	// diagnostic; unfilled labeled params report E0404 each — a Go
	// call cannot be partial.
	missing := 0
	posParams := 0
	for i, p := range goParams {
		if p.Label == "" {
			posParams++
		}
		if used[i] || (variadic && i == arity-1) {
			continue
		}
		if p.Label != "" {
			c.diags.Add(diag.Error("E0404",
				fmt.Sprintf("the Go callee requires `~%s:`, which the call does not supply", p.Label)).
				At(e.Sp).Set("label", p.Label))
		} else {
			missing++
		}
	}
	if missing > 0 || tooMany > 0 {
		if _, unknown := types.Chase(fn.Type()).(*types.Meta); !unknown {
			// A callee whose signature never resolved (a hidden or
			// unknown Go symbol) has no arity to argue with — its
			// own diagnostic already reported the cause, and "the
			// Go callee takes 0 argument(s)" would only mislead.
			// The counts compare positional arguments against
			// positional (unlabeled) parameters: labeled arguments
			// and unfilled labeled parameters carry their own
			// per-label diagnostics (E0404–E0406), and counting
			// them on either side yields self-contradicting
			// messages like "takes 1 argument(s), got 1".
			required := posParams
			if variadic {
				// The variadic tail is optional; only the
				// fixed prefix must be supplied.
				required--
				if required < 0 {
					required = 0
				}
			}
			want := fmt.Sprintf("%d", required)
			if variadic {
				want = fmt.Sprintf("at least %d", required)
			}
			c.diags.Add(diag.Error("E0401",
				fmt.Sprintf("the Go callee takes %s argument(s), got %d", want, posArgs)).
				At(e.Sp).Set("expected", want).Set("actual", fmt.Sprintf("%d", posArgs)))
		}
	}

	// ArgToParam is representable only when every argument resolved
	// to a distinct declared parameter — a repeated variadic slot
	// cannot be expressed and identity mappings stay nil.
	var argToParam []int
	seen := make([]bool, arity)
	ok := true
	for _, pi := range mapping {
		if pi < 0 || seen[pi] {
			ok = false
			break
		}
		seen[pi] = true
	}
	if ok {
		argToParam = normalizeMapping(mapping)
	}

	res := ret
	if hole >= 0 {
		// The cute placeholder: the call is a one-argument function.
		// A literal Func (not types.Fn) keeps a function-typed ret
		// nested — the emitted closure really is `func(hole) ret`.
		res = &types.Func{Params: []types.FuncParam{types.P(holeT)}, Ret: ret}
	}
	out.Base = baseOf(e.Sp, res)
	out.Call = CallInfo{
		Kind:       CallGo,
		Callee:     callee,
		Arity:      arity,
		Applied:    len(out.Args),
		ArgToParam: argToParam,
		HoleIndex:  hole,
		Variadic:   variadic,
		TypeArgs:   targsOf(fn),
	}
	if err := out.Validate(); err != nil {
		panic(fmt.Sprintf("check: invalid CallInfo for %T: %v", e.Fn, err))
	}
	return out
}

// sliceElemOf returns the element type of the declared variadic
// parameter — a GoSlice — or a fresh meta when the shape is unusual.
// A file-local `type List` is not the builtin the rule names.
func sliceElemOf(t types.Type, sp diag.Span, c *checker) types.Type {
	if con, ok := types.Chase(t).(*types.Con); ok && len(con.Args) == 1 &&
		((con.Qual == "Go" && con.Name == "Slice") ||
			(con.Qual == "" && con.Name == "List" && !c.shadowedBuiltin("List"))) {
		return con.Args[0]
	}
	return c.newMeta(sp, "the variadic element type")
}

// spreadElem checks the collection a `..e` spread expands: a List or
// a GoSlice whose element type unifies with the variadic parameter's
// element type. A file-local `type List` shadows the builtin the
// spread needs (E0415).
func (c *checker) spreadElem(t types.Type, elem types.Type, sp diag.Span) {
	switch t := types.Chase(t).(type) {
	case *types.Con:
		if len(t.Args) == 1 && t.Qual == "Go" && t.Name == "Slice" {
			c.unify(elem, t.Args[0], sp)
			return
		}
		if len(t.Args) == 1 && t.Qual == "" && t.Name == "List" {
			if c.shadowedBuiltin("List") {
				c.shadowedErr(sp, "the spread `..e`", "List")
				return
			}
			c.unify(elem, t.Args[0], sp)
			return
		}
	case *types.Meta:
		c.bind(t, types.GoSlice(elem), sp)
		return
	case *types.Var, *types.Func, *types.Tuple, *types.Go:
		// Not a spreadable collection.
	default:
		panic("unreachable")
	}
	c.mismatch(sp, "a List or Go.Slice", t)
}
