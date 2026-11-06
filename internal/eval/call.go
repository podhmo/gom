package eval

import (
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/types"
)

// fnKind classifies function values.
type fnKind int

const (
	// fnLambda is a lambda or top-level FnDecl body with params.
	fnLambda fnKind = iota + 1
	// fnBuiltin is a host/builtin function evaluated in Go.
	fnBuiltin
	// fnCtor is an ADT constructor used as a function.
	fnCtor
	// fnCompose is `f >> g` / `g << f` — the input feeds f, its
	// result feeds g, and further arguments (when g takes more than
	// one parameter) keep applying to the result.
	fnCompose
	// fnGoPartial is a cute `_`-placeholder Go call awaiting its one
	// remaining argument.
	fnGoPartial
)

// goCall is a deferred Go call created by a `_` placeholder: the
// non-hole argument values are already evaluated (spec 8.2: cute
// semantics — evaluated once at function creation).
type goCall struct {
	sp     diag.Span
	callee *VFunc
	args   []Value // evaluated non-hole args in declaration order
	holeAt int     // the hole's declaration-order parameter position
}

// deepArg is a labeled argument whose parameter belongs to a level
// deeper than the function value being applied — the `~b:` of
// `f ~b: v` on `f (~a) -> ~b -> T`. CallInfo.ArgToParam indexes the
// callee's whole flattened parameter list (docs/decisions/
// 2026-11-06-label-args-into-returned-functions.md); the arg binds
// once application reaches the level that owns it.
type deepArg struct {
	pos int   // the arg's flat position in the call's parameter list
	arg Value // the already-evaluated argument
}

// applyArgs applies a function value to args whose positions index
// the call's flattened parameter list (per check.CallInfo.
// ArgToParam, identity when nil). Positions inside this VFunc's own
// level bind now — via Remaining(), the open slots in declaration
// order; positions past it are deep args the result levels answer.
// Extra args beyond the remaining arity are applied one at a time to
// whatever the saturated call returns (over-application, spec
// section 1.3 currying semantics).
func (e *interp) applyArgs(f *VFunc, positions []int, args []Value, sp diag.Span) Value {
	nf := cloneFunc(f)
	rem := nf.Remaining()
	var deep []deepArg
	i := 0
	for ; i < len(args); i++ {
		if positions == nil && i >= len(rem) {
			break // over-application: saturate this level first
		}
		pos := i
		if positions != nil {
			pos = positions[i]
		}
		if pos < 0 {
			failData(CodeInternal, "argument position out of range", sp,
				"position", pos)
		}
		if pos >= len(rem) {
			// The arg names a parameter of a level this VFunc
			// does not span — it binds when the chain reaches
			// that level.
			deep = append(deep, deepArg{pos: pos, arg: args[i]})
			continue
		}
		nf.bound[rem[pos]] = slot{set: true, v: args[i]}
	}
	if len(deep) == 0 {
		var v Value
		if len(nf.Remaining()) == 0 {
			v = e.invoke(nf, sp)
		} else {
			v = nf
		}
		for ; i < len(args); i++ {
			v = e.applyOne(v, args[i], sp)
		}
		return v
	}
	if len(nf.Remaining()) == 0 {
		// This level saturated: invoke, then feed the deep args
		// to the levels the result carries.
		v := e.invoke(nf, sp)
		return e.applyDeep(v, deep, f.Arity(), sp)
	}
	// A gap stays open at this level — the call is a partial
	// application. Eta-expand: the returned function takes this
	// level's remaining parameters in remaining order, invokes,
	// then feeds the deep args to the result's levels — the
	// evaluator's twin of the Closure partialCall emits.
	rem2 := nf.Remaining()
	ps := make([]types.FuncParam, len(rem2))
	for k, r := range rem2 {
		if r < len(nf.sig.Params) {
			ps[k] = nf.sig.Params[r]
		}
	}
	consumed := f.Arity()
	pending := deep
	g := funcOfType(nf.name, fnBuiltin, &types.Func{Params: ps, Ret: types.Unit})
	g.builtin = func(etas []Value) (Value, error) {
		for k, a := range etas {
			nf.bound[rem2[k]] = slot{set: true, v: a}
		}
		v := e.invoke(nf, sp)
		return e.applyDeep(v, pending, consumed, sp), nil
	}
	return g
}

// applyDeep feeds deep args to the function levels a result value
// carries: a flat position of the call's parameter list is position
// pos-consumed of the chain left after this level. Positions still
// out of range keep descending — `f ~c: v` on `(~a) -> (~b) ->
// (~c) -> T` passes through every level until `~c`'s.
func (e *interp) applyDeep(v Value, deep []deepArg, consumed int, sp diag.Span) Value {
	if len(deep) == 0 {
		return v
	}
	f, ok := v.(*VFunc)
	if !ok {
		failData(CodeInternal, "labeled argument applied to a non-function result", sp,
			"value", v.String())
	}
	positions := make([]int, len(deep))
	args := make([]Value, len(deep))
	for i, d := range deep {
		positions[i] = d.pos - consumed
		args[i] = d.arg
	}
	return e.applyArgs(f, positions, args, sp)
}

// applyOne applies one positional argument to the next remaining
// parameter of v.
func (e *interp) applyOne(v Value, arg Value, sp diag.Span) Value {
	f, ok := v.(*VFunc)
	if !ok {
		failData(CodeInternal, "applied a non-function value", sp,
			"value", v.String())
	}
	return e.applyArgs(f, nil, []Value{arg}, sp)
}

// cloneFunc copies f so further binding does not mutate the shared
// function value.
func cloneFunc(f *VFunc) *VFunc {
	nf := *f
	nf.bound = make([]slot, len(f.bound))
	copy(nf.bound, f.bound)
	return &nf
}

// invoke runs a saturated function: all bound slots are filled. It
// is also the call-depth boundary — every saturated call is one
// frame of Go-stack recursion, so the depth budget lives here; the
// deferred decrement keeps the count exact under failData panics.
func (e *interp) invoke(f *VFunc, sp diag.Span) Value {
	if e.maxDepth > 0 {
		e.depth++
		defer func() { e.depth-- }()
		if e.depth > e.maxDepth {
			// Same E9006 class as the step budget: an unbounded
			// recursion stopped structurally, orders of magnitude
			// before the goroutine stack ceiling would kill the
			// process unrecoverably.
			failData(CodeBudget, "evaluation call depth budget exhausted", sp,
				"budget", e.maxDepth)
		}
	}
	switch f.kind {
	case fnLambda:
		if len(f.params) > len(f.bound) {
			failData(CodeInternal,
				"parameter list is longer than the bound slots", sp,
				"function", f.name)
		}
		env := f.env.Child()
		for i, p := range f.params {
			if !e.matchPat(p.Pat, f.bound[i].v, env) {
				failData(CodeInternal,
					"irrefutable parameter pattern failed", p.Pat.Span(),
					"function", f.name)
			}
		}
		return runBody(func() Value {
			return e.eval(f.body, env)
		})
	case fnBuiltin:
		args := make([]Value, len(f.bound))
		for i, s := range f.bound {
			args[i] = s.v
		}
		v, err := f.builtin(args)
		if err != nil {
			failData(CodePanic, err.Error(), sp, "function", f.name)
		}
		return v
	case fnCtor:
		fields := make([]VField, len(f.bound))
		for i, s := range f.bound {
			fi := fieldInfo{index: i}
			if i < len(f.ctor.fields) {
				fi = f.ctor.fields[i]
			}
			fields[i] = VField{Index: i, Name: fi.name, Sym: fi.sym, V: s.v}
		}
		return &VVariant{Type: f.ctor.typ, Ctor: f.ctor.sym, Name: f.ctor.name, Fields: fields}
	case fnCompose:
		// `f >> g` applied to x, y, ... runs `g (f x) y ...`: the
		// first bound slot feeds compose[0], its result feeds
		// compose[1], and the remaining bound slots keep applying
		// to whatever that returns (a multi-parameter second
		// function composes into a multi-parameter result).
		if len(f.bound) == 0 {
			failData(CodeInternal, "composition has no parameter to feed", sp,
				"type", f.sig.String())
		}
		v := e.applyOne(f.compose[1], e.applyOne(f.compose[0], f.bound[0].v, sp), sp)
		for _, s := range f.bound[1:] {
			v = e.applyOne(v, s.v, sp)
		}
		return v
	case fnGoPartial:
		g := f.gocall
		full := make([]Value, len(g.args)+1)
		j := 0
		for i := range full {
			if i == g.holeAt {
				full[i] = f.bound[0].v
			} else {
				full[i] = g.args[j]
				j++
			}
		}
		return e.invokeGo(g.callee, full, g.sp)
	default:
		panic("unreachable")
	}
}

// invokeGo calls a host-provided Go function directly with positional
// args (no currying, variadic included — spec section 8.1). The
// callee must be a fnBuiltin bound by the harness.
func (e *interp) invokeGo(f *VFunc, args []Value, sp diag.Span) Value {
	if f.kind != fnBuiltin {
		return e.applyArgs(f, nil, args, sp)
	}
	v, err := f.builtin(args)
	if err != nil {
		failData(CodePanic, err.Error(), sp, "function", f.name)
	}
	return v
}

// funcOfType builds a bare VFunc of signature sig: all params unbound.
func funcOfType(name string, kind fnKind, sig *types.Func) *VFunc {
	if sig == nil {
		sig = &types.Func{Ret: types.Unit}
	}
	return &VFunc{name: name, kind: kind, sig: sig, bound: make([]slot, len(sig.Params))}
}

// methodValueSig flattens a bound method's value type — the member
// selection's checked type, e.g. `Int -> Int -> Int -> Time` —
// into the flat signature a VFunc curries over. The host stub the
// bound method wraps takes the receiver as an extra first argument,
// so the receiver is not among these params.
func methodValueSig(t types.Type) *types.Func {
	var params []types.FuncParam
	for {
		f, ok := types.Chase(t).(*types.Func)
		if !ok {
			return &types.Func{Params: params, Ret: t}
		}
		params = append(params, f.Params...)
		t = f.Ret
	}
}

// NewHostFunc binds a Go-implemented function for tests: it takes all
// arguments at once (arity positional parameters) and either returns
// a value or fails — a non-nil error becomes a CodePanic diagnostic,
// matching a Go-level panic. Use it to stub Go imports
// (`strconv.Atoi`, ...) and stdlib functions in hand-built programs.
func NewHostFunc(name string, arity int, fn func(args []Value) (Value, error)) *VFunc {
	sig := &types.Func{Params: make([]types.FuncParam, arity), Ret: types.Unit}
	for i := range sig.Params {
		sig.Params[i] = types.P(types.Unit)
	}
	f := funcOfType(name, fnBuiltin, sig)
	f.builtin = fn
	return f
}
