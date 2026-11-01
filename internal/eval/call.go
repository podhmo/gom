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
	args   []Value // evaluated non-hole args in source order
	holeAt int     // position of the hole in the full argument list
}

// applyArgs applies a function value to args whose positions index
// f.Remaining() (per check.CallInfo.ArgToParam, identity when nil).
// Extra args beyond the remaining arity are applied one at a time to
// whatever the saturated call returns (over-application, spec
// section 1.3 currying semantics).
func (e *interp) applyArgs(f *VFunc, positions []int, args []Value, sp diag.Span) Value {
	nf := cloneFunc(f)
	rem := nf.Remaining()
	i := 0
	for ; i < len(args); i++ {
		if i >= len(rem) {
			break // over-application: saturate first
		}
		pos := i
		if positions != nil {
			pos = positions[i]
		}
		if pos < 0 || pos >= len(rem) {
			failData(CodeInternal, "argument position out of range", sp,
				"position", pos)
		}
		nf.bound[rem[pos]] = slot{set: true, v: args[i]}
	}
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

// invoke runs a saturated function: all bound slots are filled.
func (e *interp) invoke(f *VFunc, sp diag.Span) Value {
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
