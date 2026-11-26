package check

// `Go.funcN` — the explicit conversion of a Gom function value into
// a Go `func` value of type `Go.FuncN`
// (docs/decisions/2026-11-17-func-interop.md).
//
// The FuncN type's arguments are the *raw* Go views of the func
// type's parameter and result types — `func(i int) bool` is
// `Go.Func2 Go.Int Bool` — so the emitted literal's signature is a
// faithful rendering of the Con. The converter's *argument* is a
// Gom function in the Gom view (`Int -> Bool`); the conversions the
// wrapper needs inside its body are lowering's job (lower reads the
// two views off the checked types and records them on core.GoFunc).

import (
	"fmt"
	"strings"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// goFuncCallArity reports whether sym is a `Go.funcN` conversion
// builtin and returns its parameter count.
func goFuncCallArity(sym *resolve.Symbol) (int, bool) {
	if sym == nil || sym.Kind != resolve.KindBuiltin || sym.Package != "Go" ||
		!strings.HasPrefix(sym.Name, "func") {
		return 0, false
	}
	return goFuncArityDigit(sym.Name[4:])
}

// goFnCallArity reports whether sym is a `Go.fnN` conversion
// builtin — the `Go.FuncN` → Gom-fn direction
// (docs/decisions/2026-11-18-fn-interop.md) — and returns its
// parameter count.
func goFnCallArity(sym *resolve.Symbol) (int, bool) {
	if sym == nil || sym.Kind != resolve.KindBuiltin || sym.Package != "Go" ||
		!strings.HasPrefix(sym.Name, "fn") {
		return 0, false
	}
	return goFuncArityDigit(sym.Name[2:])
}

// funcCallMember reports whether sym is a `Go.funcN`/`Go.fnN`
// conversion member — a callee-only surface; checkSelect types it
// as an untyped meta because the apply computes the result type.
func funcCallMember(sym *resolve.Symbol) bool {
	if _, ok := goFuncCallArity(sym); ok {
		return true
	}
	_, ok := goFnCallArity(sym)
	return ok
}

// goFuncTypeArity reports whether name is a `Go.FuncN` type name and
// returns its parameter count.
func goFuncTypeArity(name string) (int, bool) {
	if !strings.HasPrefix(name, "Func") {
		return 0, false
	}
	return goFuncArityDigit(name[4:])
}

// goFuncArityDigit reads the single-digit arity suffix of a
// funcN/FuncN name — the parameter count lives in the name.
func goFuncArityDigit(s string) (int, bool) {
	if len(s) != 1 || s[0] < '0' || s[0] > '0'+byte(types.GoFuncMaxArity) {
		return 0, false
	}
	return int(s[0] - '0'), true
}

// checkGoFuncCall checks `Go.funcN f`. When the expected type is a
// `Go.FuncN` its raw views translate back to the Gom-facing
// function type the argument is checked against, so an inline
// lambda inherits concrete parameter types (`http.HandleFunc ("/",
// Go.func2 (fn w r -> ...))`). Without that expectation the
// argument's own type must already be concrete — an unsolved meta
// names no raw Go view.
func (c *checker) checkGoFuncCall(e *syntax.Apply, fn Expr, callee *resolve.Symbol, n int, want types.Type) Expr {
	out := &Apply{Base: baseOf(e.Sp, nil), Fn: fn}
	if len(e.Args) != 1 || e.Args[0].Label != nil {
		c.diags.Add(diag.Error("E0510",
			fmt.Sprintf("`Go.func%d` takes exactly one function argument", n)).At(e.Sp))
		for _, a := range e.Args {
			var v Expr
			if a.Value != nil {
				v = c.checkExpr(a.Value, nil)
			}
			out.Args = append(out.Args, Arg{Value: v})
		}
		out.Base = baseOf(e.Sp, c.quietMeta(e.Sp, "the conversion's result"))
		out.Call = CallInfo{Kind: CallUnknown, Applied: len(out.Args), HoleIndex: -1}
		return out
	}

	// Decode the expected `Go.FuncN` into the Gom-facing seed type —
	// an inference hint so an inline lambda's unannotated parameters
	// come out concrete. Conformance is decided afterwards by
	// comparing the conversion's *produced* FuncN to the expected
	// one with view-tolerant compatibility: `func(int)` admits a
	// `x: Int` or `x: Go.Int` callback alike, since both readings of
	// `int` are the boundary's.
	var seed types.Type
	var wCon *types.Con
	if w, ok := types.Chase(want).(*types.Con); ok && w.Qual == "Go" {
		if _, ok2 := goFuncTypeArity(w.Name); ok2 {
			wCon = w
		}
	}
	if wCon == nil {
		// A named func annotation wants the same shape — its handle
		// expands to the FuncN the lambda seeds from.
		if fn, ok := c.funcNOfNamed(types.Chase(want)); ok {
			wCon, _ = fn.(*types.Con)
		}
	}
	// The seed only serves an inline lambda — its parameters cannot
	// be annotated, so they take the expected FuncN's Gom views.
	// Anything else already knows its own type; seeding it would
	// only double-report the produced-vs-expected mismatch.
	_, isLambda := e.Args[0].Value.(*syntax.Lambda)
	if wCon != nil && isLambda {
		if wn, ok2 := goFuncTypeArity(wCon.Name); ok2 && wn == n &&
			len(wCon.Args) >= n && len(wCon.Args) <= n+8 {
			seed = &types.Func{
				Params: goFuncSeedParams(wCon.Args[:n]),
				Ret:    goFuncSeedRet(c, wCon.Args[n:], false),
			}
		}
	}
	arg := c.checkExpr(e.Args[0].Value, seed)
	out.Args = []Arg{{Value: arg}}

	at := types.Chase(arg.Type())
	if m, isMeta := at.(*types.Meta); isMeta && !m.Bound() && seed != nil {
		// `Go.funcN x` with x an unsolved meta under a FuncN want —
		// the seed pins the function shape.
		c.unify(at, seed, e.Sp)
		at = types.Chase(arg.Type())
	}
	// Flatten the curried arrow chain — `Int -> Int -> Int` is the
	// two-parameter callback `Go.func2` adapts.
	var ps []types.FuncParam
	var ret types.Type
	isFn := false
	for cur := at; ; {
		fn, ok := cur.(*types.Func)
		if !ok {
			ret = cur
			break
		}
		isFn = true
		ps = append(ps, fn.Params...)
		cur = types.Chase(fn.Ret)
	}
	// `Go.func0` adapts the niladic shape — a Gom `Unit -> R`
	// thunk is Go's `func() R`; the Unit parameter drops out.
	if n == 0 && len(ps) == 1 && isGoFuncUnit(ps[0].Type) {
		ps = nil
	}
	if !isFn {
		c.diags.Add(diag.Error("E0510",
			fmt.Sprintf("`Go.func%d` expects a function value, got %s", n, types.Display(arg.Type(), metaNamer()))).At(e.Sp))
		out.Base = baseOf(e.Sp, c.quietMeta(e.Sp, "the conversion's result"))
		out.Call = CallInfo{Kind: CallUnknown, Applied: 1, HoleIndex: -1}
		return out
	}
	if len(ps) != n {
		c.diags.Add(diag.Error("E0510",
			fmt.Sprintf("`Go.func%d` expects a function with %d parameter(s), got %s", n, n, types.Display(arg.Type(), metaNamer()))).At(e.Sp))
	}
	for _, p := range ps {
		if p.Label != "" {
			c.diags.Add(diag.Error("E0510",
				fmt.Sprintf("`Go.func%d` cannot convert a function with labeled parameters — Go func types have no labels", n)).At(e.Sp))
			break
		}
	}
	outTyp := c.goFuncNType(e, n, ps, ret)
	if wCon != nil {
		if pc, ok := types.Chase(outTyp).(*types.Con); ok &&
			!gomRawCompatible(pc, wCon) {
			c.diags.Add(diag.Error("E0401",
				fmt.Sprintf("type mismatch: `Go.func%d` produces `%s`, expected `%s`", n,
					types.Display(pc, metaNamer()), types.Display(wCon, metaNamer()))).At(e.Sp))
		}
		outTyp = wCon
	}
	out.Base = baseOf(e.Sp, outTyp)
	out.Call = CallInfo{
		Kind:      CallDirect,
		Callee:    callee,
		Arity:     1,
		Applied:   1,
		HoleIndex: -1,
	}
	if err := out.Validate(); err != nil {
		panic(fmt.Sprintf("check: invalid CallInfo for %T: %v", e.Fn, err))
	}
	return out
}

// checkGoFnCall checks `Go.fnN f` — the reverse conversion of
// `Go.funcN`: a `Go.FuncN` value becomes a Gom function value the
// caller applies normally. The produced type is computed from the
// argument's FuncN con alone — raw parameter views read as their
// Gom views (goFuncSeedParams) and the raw results read as the
// seed's result mapping (none → `Unit`, `error` → `Result Unit
// GoError`, `(T, error)` → `Result T GoError`, lone `T` → `T`,
// `(T, U, ..., error)` → `Result (Tuple ...) GoError`, other
// multi-results → a tuple):
// a lone `error` is always the honest Result, never the never-nil
// `GoError` claim an extern binding could declare.
func (c *checker) checkGoFnCall(e *syntax.Apply, fn Expr, callee *resolve.Symbol, n int) Expr {
	out := &Apply{Base: baseOf(e.Sp, nil), Fn: fn}
	if len(e.Args) != 1 || e.Args[0].Label != nil {
		c.diags.Add(diag.Error("E0510",
			fmt.Sprintf("`Go.fn%d` takes exactly one function argument", n)).At(e.Sp))
		for _, a := range e.Args {
			var v Expr
			if a.Value != nil {
				v = c.checkExpr(a.Value, nil)
			}
			out.Args = append(out.Args, Arg{Value: v})
		}
		out.Base = baseOf(e.Sp, c.quietMeta(e.Sp, "the conversion's result"))
		out.Call = CallInfo{Kind: CallUnknown, Applied: len(out.Args), HoleIndex: -1}
		return out
	}
	arg := c.checkExpr(e.Args[0].Value, nil)
	out.Args = []Arg{{Value: arg}}

	fc, isCon := types.Chase(arg.Type()).(*types.Con)
	wn, isFuncN := 0, false
	if isCon && fc.Qual == "Go" {
		wn, isFuncN = goFuncTypeArity(fc.Name)
	}
	if !isFuncN {
		// A method-less named func type's handle value converts the
		// same way — `Go.fn0 stop` on a `context.CancelFunc`-typed
		// value reads its FuncN spelling.
		if fn, ok := c.funcNOfNamed(types.Chase(arg.Type())); ok {
			if cn, ok2 := fn.(*types.Con); ok2 {
				if w, ok3 := goFuncTypeArity(cn.Name); ok3 {
					fc, wn, isFuncN = cn, w, true
				}
			}
		}
	}
	if !isFuncN {
		c.diags.Add(diag.Error("E0510",
			fmt.Sprintf("`Go.fn%d` expects a `Go.Func%d` value, got %s", n, n,
				types.Display(arg.Type(), metaNamer()))).At(e.Sp))
		out.Base = baseOf(e.Sp, c.quietMeta(e.Sp, "the conversion's result"))
		out.Call = CallInfo{Kind: CallUnknown, Applied: 1, HoleIndex: -1}
		return out
	}
	if wn != n {
		c.diags.Add(diag.Error("E0510",
			fmt.Sprintf("`Go.fn%d` converts a `Go.Func%d` value, got `%s`", n, n,
				types.Display(fc, metaNamer()))).At(e.Sp))
		out.Base = baseOf(e.Sp, c.quietMeta(e.Sp, "the conversion's result"))
		out.Call = CallInfo{Kind: CallUnknown, Applied: 1, HoleIndex: -1}
		return out
	}
	if len(fc.Args) < n || len(fc.Args) > n+8 {
		c.diags.Add(diag.Error("E0510",
			fmt.Sprintf("`Go.fn%d` cannot convert `%s` — not a `func` type's shape", n,
				types.Display(fc, metaNamer()))).At(e.Sp))
		out.Base = baseOf(e.Sp, c.quietMeta(e.Sp, "the conversion's result"))
		out.Call = CallInfo{Kind: CallUnknown, Applied: 1, HoleIndex: -1}
		return out
	}
	// Each seeded position must be one the boundary's conversions
	// can spell — `[]int` and friends split `int`/`int64` inside a
	// container where no element-wise conversion exists; admitting
	// them here would only fail at lowering (E0901). viewsCompatible
	// (internal/goimport/mapping.go) is the same rule for members.
	for _, r := range fc.Args {
		if !fnViewCompatible(goFuncGomView(r, true), r) {
			c.diags.Add(diag.Error("E0510",
				fmt.Sprintf("`Go.fn%d` cannot convert `%s` — its views split inside a container and the boundary needs an element-wise copy not yet supported", n,
					types.Display(fc, metaNamer()))).At(e.Sp))
			out.Base = baseOf(e.Sp, c.quietMeta(e.Sp, "the conversion's result"))
			out.Call = CallInfo{Kind: CallUnknown, Applied: 1, HoleIndex: -1}
			return out
		}
	}
	out.Base = baseOf(e.Sp, &types.Func{
		Params: goFuncSeedParams(fc.Args[:n]),
		Ret:    goFuncSeedRet(c, fc.Args[n:], true),
	})
	out.Call = CallInfo{
		Kind:      CallDirect,
		Callee:    callee,
		Arity:     1,
		Applied:   1,
		HoleIndex: -1,
	}
	if err := out.Validate(); err != nil {
		panic(fmt.Sprintf("check: invalid CallInfo for %T: %v", e.Fn, err))
	}
	return out
}

// goFuncNType computes the `Go.FuncN` type a `Go.funcN` conversion
// produces from the checked argument type — each parameter and
// result argument is the raw Go view of the corresponding
// Gom-facing type. A type with no raw view reports E0510.
func (c *checker) goFuncNType(e *syntax.Apply, n int, ps []types.FuncParam, ret types.Type) types.Type {
	args := make([]types.Type, 0, n+2)
	for _, p := range ps {
		rt, reason := c.goFuncRawView(p.Type)
		if reason != "" {
			c.diags.Add(diag.Error("E0510",
				fmt.Sprintf("`Go.func%d` cannot convert the parameter type %s: %s", n, types.Display(p.Type, metaNamer()), reason)).At(e.Sp))
			return c.quietMeta(e.Sp, "the conversion's result")
		}
		args = append(args, rt)
	}
	rs, reason := c.goFuncRawResults(types.Chase(ret))
	if reason != "" {
		c.diags.Add(diag.Error("E0510",
			fmt.Sprintf("`Go.func%d` cannot convert the result type %s: %s", n, types.Display(ret, metaNamer()), reason)).At(e.Sp))
		return c.quietMeta(e.Sp, "the conversion's result")
	}
	return types.GoFunc(n, append(args, rs...)...)
}

// goFuncSeedParams maps a FuncN con's raw parameter arguments back
// to the Gom-facing parameter types they pair with — the mirror of
// the boundary's import mapping (top-level `Go.Slice T` reads `List
// T`, `Go.Int` reads `Int`). A `Go.Func0` seeds `Unit ->` — the
// thunk's single Unit parameter.
func goFuncSeedParams(raws []types.Type) []types.FuncParam {
	if len(raws) == 0 {
		return []types.FuncParam{{Type: types.Unit}}
	}
	ps := make([]types.FuncParam, len(raws))
	for i, r := range raws {
		ps[i] = types.FuncParam{Type: goFuncGomView(r, true)}
	}
	return ps
}

// isGoFuncUnit reports whether t is the builtin `Unit` — a
// `Go.func0` argument's parameter.
func isGoFuncUnit(t types.Type) bool {
	cc, ok := types.Chase(t).(*types.Con)
	return ok && cc.Qual == "" && cc.Name == "Unit" && len(cc.Args) == 0
}

// goFuncSeedRet maps a FuncN con's raw result arguments back to the
// Gom-facing result: none → `Unit`, `error` → `Result Unit
// GoError`, `(T, error)` → `Result T GoError`, a lone `T` → `T`,
// and the N-ary shapes — a trailing `error` folds the payload into
// `Result (Tuple ...) GoError`, anything else a plain tuple
// (docs/decisions/2026-11-18-nary-go-results.md).
// optWrap asks for the import mapping's `Option` wrap on a
// method-bearing-interface payload (fnSeedNilable): `Go.fnN`'s
// derived type wants it so `(nil, nil)` materializes `Ok None`;
// `Go.funcN`'s lambda seed does not — there the verbatim
// `Result GoError GoError` is the simple, always-non-nil reading
// (`Option`-declared callbacks convert through goFuncRawResults'
// own Option case instead).
func goFuncSeedRet(c *checker, raws []types.Type, optWrap bool) types.Type {
	wrap := func(raw types.Type) types.Type {
		p := goFuncGomView(raw, true)
		// A method-bearing interface payload (`error`, `net.Error`,
		// `io.Reader`) carries the unobservable nil — the derived
		// view wraps it in Option exactly like the import mapping
		// (internal/goimport/mapping.go): `Ok None` reads
		// `(nil, nil)` instead of smuggling a nil interface in.
		if optWrap && c.fnSeedNilable(raw) {
			p = types.Option(p)
		}
		return p
	}
	switch len(raws) {
	case 0:
		return types.Unit
	case 1:
		if c.isBuiltinCon(types.Chase(raws[0]), "GoError") {
			return types.Result(types.Unit, types.GoError)
		}
		return goFuncGomView(raws[0], true)
	case 2:
		if isGoErrorCon(types.Chase(raws[1])) {
			return types.Result(wrap(raws[0]), types.GoError)
		}
		return types.Tup(goFuncGomView(raws[0], true), goFuncGomView(raws[1], true))
	default:
		if isGoErrorCon(types.Chase(raws[len(raws)-1])) {
			els := make([]types.Type, len(raws)-1)
			for i := range els {
				els[i] = wrap(raws[i])
			}
			return types.Result(types.Tup(els...), types.GoError)
		}
		els := make([]types.Type, len(raws))
		for i := range els {
			els[i] = goFuncGomView(raws[i], true)
		}
		return types.Tup(els...)
	}
}

// fnSeedNilable reports whether a FuncN con's first raw result
// spells a method-bearing interface — `error` (the builtin
// `GoError`) or a named Go type the source knows is one.
func (c *checker) fnSeedNilable(raw types.Type) bool {
	switch t := types.Chase(raw).(type) {
	case *types.Con:
		return c.isBuiltinCon(t, "GoError")
	case *types.Go:
		if ns, ok := c.src.(NilableSource); ok {
			return ns.NilableGoRef(t.Ref)
		}
		return false
	case *types.Meta, *types.Var, *types.Func, *types.Tuple:
		// A FuncN con's arguments are raw Go views — none of
		// these can appear, and none is nilable anyway.
		return false
	default:
		panic("unreachable")
	}
}

// goRawNilable reports whether a raw Go view can carry nil — the
// types whose zero value is nil: `error`/`GoError`, `any`,
// pointers, slices, maps, channels (each direction), func types
// (`Go.FuncN`), and named types whose underlying is nil-able
// (named empty interfaces, named slices, ... — answered through
// NilableSource's broader underlying question).
func (c *checker) goRawNilable(raw types.Type) bool {
	switch t := types.Chase(raw).(type) {
	case *types.Con:
		if c.isBuiltinCon(t, "GoError") || t.Name == "Any" {
			return true
		}
		switch t.Name {
		case "Ptr", "Slice", "Map", "Chan", "RecvChan", "SendChan":
			return true
		}
		if t.Qual == "Go" {
			_, isFn := goFuncTypeArity(t.Name)
			return isFn
		}
		return false
	case *types.Go:
		if ns, ok := c.src.(NilableSource); ok {
			return ns.NilableUnderlying(t.Ref)
		}
		return false
	case *types.Meta, *types.Var, *types.Func, *types.Tuple:
		return false
	default:
		panic("unreachable")
	}
}

// nilableType reports whether a solved type can hold nil — the
// `Go.nil` constraint (decision 2026-11-23-nil-spelling). It is
// goRawNilable's domain plus a builtin-identity guard: a file-local
// `type Chan a` does not spell the builtin channel type just
// because the names match.
func (c *checker) nilableType(t types.Type) bool {
	switch t := types.Chase(t).(type) {
	case *types.Con:
		if c.isBuiltinCon(t, "GoError") || c.isBuiltinCon(t, "Any") {
			return true
		}
		if t.Qual == "Go" {
			switch t.Name {
			case "Slice", "Map", "Any":
				return true
			}
			_, isFn := goFuncTypeArity(t.Name)
			return isFn
		}
		if t.Qual != "" {
			return false
		}
		switch t.Name {
		case "Ptr", "Slice", "Map", "Chan", "RecvChan", "SendChan":
			return !c.shadowedBuiltin(t.Name)
		}
		return false
	case *types.Go:
		if ns, ok := c.src.(NilableSource); ok {
			return ns.NilableUnderlying(t.Ref)
		}
		return false
	case *types.Meta, *types.Var, *types.Func, *types.Tuple:
		return false
	default:
		panic("unreachable")
	}
}

// fnViewCompatible reports whether the seeded Gom view of one
// FuncN position can reach its raw view through the conversions
// Core IR can express — viewsCompatible's rule (internal/goimport/
// mapping.go) mirrored check-side: Equal views, the top-level
// int ↔ Go.Int numeric pair, and the List ↔ Go.Slice boundary
// copy only when the elements' views are Equal (an element-wise
// conversion inside a container cannot be spelled yet).
func fnViewCompatible(g, r types.Type) bool {
	g, r = types.Chase(g), types.Chase(r)
	if types.Equal(g, r) {
		return true
	}
	gc, gok := g.(*types.Con)
	rc, rok := r.(*types.Con)
	if !gok || !rok {
		return false
	}
	if gc.Qual == "" && gc.Name == "Int" && len(gc.Args) == 0 &&
		rc.Qual == "Go" && rc.Name == "Int" && len(rc.Args) == 0 {
		return true
	}
	if gc.Qual == "" && gc.Name == "Int64" && len(gc.Args) == 0 &&
		(rc.Qual == "" || rc.Qual == "Go") && rc.Name == "Int" && len(rc.Args) == 0 {
		return true
	}
	if gc.Qual == "" && (gc.Name == "Uint" || gc.Name == "Uint64") && len(gc.Args) == 0 &&
		rc.Qual == "Go" && rc.Name == "Uint" && len(rc.Args) == 0 {
		return true
	}
	if gc.Qual == "" && gc.Name == "List" && len(gc.Args) == 1 &&
		rc.Qual == "Go" && rc.Name == "Slice" && len(rc.Args) == 1 {
		return types.Equal(gc.Args[0], rc.Args[0])
	}
	return false
}

// goFuncGomView is the inverse of the import mapping's view pair:
// the Gom-facing type a raw Go-view type corresponds to. boundary
// marks a signature-boundary position — only there does `Go.Slice
// T` read `List T`; nested it stays `Go.Slice`.
func goFuncGomView(t types.Type, boundary bool) types.Type {
	switch t := types.Chase(t).(type) {
	case *types.Con:
		if t.Qual == "Go" {
			switch t.Name {
			case "Int":
				return types.Int
			case "Uint":
				return types.Uint
			case "Slice":
				if boundary {
					return types.List(goFuncGomView(t.Args[0], false))
				}
				return types.GoSlice(goFuncGomView(t.Args[0], false))
			case "Map":
				return types.GoMap(goFuncGomView(t.Args[0], false), goFuncGomView(t.Args[1], false))
			}
			// Go.Any, Go.FuncN and friends keep their spelling —
			// they are already the same type in both views.
			return t
		}
		switch t.Name {
		case "Ptr":
			return types.Ptr(goFuncGomView(t.Args[0], false))
		case "Chan":
			return types.Chan(goFuncGomView(t.Args[0], false))
		case "RecvChan":
			return types.RecvChan(goFuncGomView(t.Args[0], false))
		case "SendChan":
			return types.SendChan(goFuncGomView(t.Args[0], false))
		}
		return t
	case *types.Go:
		if len(t.Args) == 0 {
			return t
		}
		args := make([]types.Type, len(t.Args))
		for i, a := range t.Args {
			args[i] = goFuncGomView(a, false)
		}
		return &types.Go{Ref: t.Ref, Args: args}
	case *types.Func, *types.Meta, *types.Tuple, *types.Var:
		// These never appear in a raw Go-view type — pass through.
		return t
	default:
		panic("unreachable")
	}
}

// goFuncRawView maps a Gom-facing type to the raw Go view the
// FuncN type argument names — `Int` is `Go.Int`, `List T` is
// `Go.Slice T'`, declared types pass through by their emitted
// name, and the Gom-only shapes (Option, Result, Tuple, Func)
// report why no Go func signature can carry them.
func (c *checker) goFuncRawView(t types.Type) (types.Type, string) {
	switch t := types.Chase(t).(type) {
	case *types.Con:
		if t.Qual == "Go" {
			switch t.Name {
			case "Slice":
				rt, reason := c.goFuncRawView(t.Args[0])
				if reason != "" {
					return nil, reason
				}
				return types.GoSlice(rt), ""
			case "Map":
				rk, reason := c.goFuncRawView(t.Args[0])
				if reason != "" {
					return nil, reason
				}
				rv, reason := c.goFuncRawView(t.Args[1])
				if reason != "" {
					return nil, reason
				}
				return types.GoMap(rk, rv), ""
			}
			// Go.Int/Go.Any/Go.FuncN and the other Go-owned
			// constructors are already raw views.
			return t, ""
		}
		// A declared type — a local `type` (including one
		// shadowing a builtin name) or an imported package's —
		// emits as a named Go type the raw signature can carry by
		// name (codegen writes `exportName(c.Name)` / the qualified
		// identifier). Declared names must win over the
		// builtin-name switch below: an imported `model.Int` is
		// not builtin `Int`, and a local `type Int` declares its
		// own emitted type (decision 2026-11-23-goslice-gom-elem).
		if t.Qual != "" {
			return t, ""
		}
		if ti, declared := c.tyCons[conKey{qual: "", name: t.Name}]; declared && ti.sym != nil {
			return t, ""
		}
		switch t.Name {
		case "Int", "Int64":
			// Both are int64 — the int64/int split only exists at
			// `Go.Int` (mapping.go's basicType).
			return types.Int, ""
		case "Uint", "Uint64":
			return types.Uint, ""
		case "Int32", "Float", "Bool", "String", "Char", "Byte", "GoError":
			return t, ""
		case "List":
			rt, reason := c.goFuncRawView(t.Args[0])
			if reason != "" {
				return nil, reason
			}
			return types.GoSlice(rt), ""
		case "Ptr":
			rt, reason := c.goFuncRawView(t.Args[0])
			if reason != "" {
				return nil, reason
			}
			return types.Ptr(rt), ""
		case "Chan":
			rt, reason := c.goFuncRawView(t.Args[0])
			if reason != "" {
				return nil, reason
			}
			return types.Chan(rt), ""
		case "RecvChan":
			rt, reason := c.goFuncRawView(t.Args[0])
			if reason != "" {
				return nil, reason
			}
			return types.RecvChan(rt), ""
		case "SendChan":
			rt, reason := c.goFuncRawView(t.Args[0])
			if reason != "" {
				return nil, reason
			}
			return types.SendChan(rt), ""
		}
		// Only the builtin cons left — `Option`, `Result` — have
		// no Go representation.
		return nil, "the type has no Go representation"
	case *types.Go:
		args := make([]types.Type, len(t.Args))
		for i, a := range t.Args {
			rt, reason := c.goFuncRawView(a)
			if reason != "" {
				return nil, reason
			}
			args[i] = rt
		}
		return &types.Go{Ref: t.Ref, Args: args}, ""
	case *types.Meta:
		return nil, "its type is not determined yet — add an annotation"
	case *types.Var:
		return nil, "a type variable names no concrete Go type"
	case *types.Func:
		return nil, "a curried Gom function type is not a Go func type — wrap it in `Go.funcN` first"
	case *types.Tuple:
		return nil, "tuples have no Go representation"
	default:
		panic("unreachable")
	}
}

// goFuncRawResults maps a Gom-facing result type to the FuncN's raw
// result arguments: `Unit` → none, `Result Unit GoError` → `error`,
// `Result T GoError` → `(T', error)`, a tuple → its raw elements,
// `Result (Tuple ...) GoError` → the raw elements plus `error`
// (docs/decisions/2026-11-18-nary-go-results.md), otherwise the
// lone raw view.
// An `Option` payload — or an Option tuple element — maps only
// when its raw view is nil-able — `Result (Option Int) GoError`
// has no honest `(int64, error)` reading (`None` would
// materialize as `0`).
func (c *checker) goFuncRawResults(ret types.Type) ([]types.Type, string) {
	// elemRaw reads one payload position: verbatim, or an Option
	// spelling over a nil-able raw view.
	elemRaw := func(el types.Type) (types.Type, string) {
		p := types.Chase(el)
		if oc, isOpt := p.(*types.Con); isOpt && oc.Qual == "" &&
			oc.Name == "Option" && len(oc.Args) == 1 &&
			!c.shadowedBuiltin("Option") {
			p = types.Chase(oc.Args[0])
			rt, reason := c.goFuncRawView(p)
			if reason != "" {
				return nil, reason
			}
			if !c.goRawNilable(rt) {
				return nil, fmt.Sprintf("`Option %s`'s payload is not nil-able — `None` cannot map to a Go value", types.Display(p, metaNamer()))
			}
			return rt, ""
		}
		return c.goFuncRawView(p)
	}
	if rc, ok := ret.(*types.Con); ok && rc.Qual == "" && !c.shadowedBuiltin(rc.Name) {
		if rc.Name == "Unit" && len(rc.Args) == 0 {
			return nil, ""
		}
		if rc.Name == "Result" && len(rc.Args) == 2 &&
			isGoErrorCon(rc.Args[1]) {
			p := types.Chase(rc.Args[0])
			if uc, ok := p.(*types.Con); ok &&
				uc.Qual == "" && uc.Name == "Unit" && len(uc.Args) == 0 &&
				!c.shadowedBuiltin("Unit") {
				return []types.Type{types.GoError}, ""
			}
			if tup, isTup := p.(*types.Tuple); isTup {
				// `Result (Tuple ...) GoError` — the N-ary
				// trailing-error fold: element raws then `error`.
				if len(tup.Elems)+1 > 8 {
					return nil, fmt.Sprintf("a `Result (Tuple ...) GoError` callback may carry at most 7 payload elements — %d plus the trailing `error` exceeds the 8-result `Go.FuncN` maximum", len(tup.Elems))
				}
				rs := make([]types.Type, 0, len(tup.Elems)+1)
				for _, el := range tup.Elems {
					rt, reason := elemRaw(el)
					if reason != "" {
						return nil, reason
					}
					rs = append(rs, rt)
				}
				return append(rs, types.GoError), ""
			}
			rt, reason := elemRaw(p)
			if reason != "" {
				return nil, reason
			}
			return []types.Type{rt, types.GoError}, ""
		}
		if rc.Name == "Result" {
			return nil, "only `Result Unit GoError`, `Result T GoError` and `Result (Tuple ...) GoError` map to Go results"
		}
	}
	if tup, ok := ret.(*types.Tuple); ok {
		if len(tup.Elems) > 8 {
			return nil, fmt.Sprintf("a %d-element tuple callback result exceeds the 8-result `Go.FuncN` maximum", len(tup.Elems))
		}
		rs := make([]types.Type, 0, len(tup.Elems))
		for _, el := range tup.Elems {
			rt, reason := elemRaw(el)
			if reason != "" {
				return nil, reason
			}
			rs = append(rs, rt)
		}
		if isGoErrorCon(types.Chase(rs[len(rs)-1])) {
			return nil, "a tuple result ending in `GoError` spells the same `func` signature as `Result ... GoError` — write the `Result` fold instead"
		}
		return rs, ""
	}
	rt, reason := c.goFuncRawView(ret)
	if reason != "" {
		return nil, reason
	}
	return []types.Type{rt}, ""
}

// gomRawCompatible reports whether two FuncN cons — or the types
// inside their arguments — name the same Go type through the
// boundary's two readings: `Go.Int`/`Int`/`Int64` interchange (all
// are `int` or `int64` spellings of the int/int64 pair the import
// mapping reads into `Int`), `List T`/`Go.Slice T` interchange, and
// otherwise equality recursing through type arguments. Both sides
// sit in the raw view; strict `types.Equal` alone would reject a
// callback written `x: Int` against a `func(int)` parameter.
func gomRawCompatible(a, b types.Type) bool {
	a, b = types.Chase(a), types.Chase(b)
	if types.Equal(a, b) {
		return true
	}
	ac, aok := a.(*types.Con)
	bc, bok := b.(*types.Con)
	if !aok || !bok {
		return false
	}
	// The int/int64 pair — `Go.Int` against either bare `Int`.
	if ac.Name == "Int" && len(ac.Args) == 0 && bc.Name == "Int" &&
		len(bc.Args) == 0 && (ac.Qual == "Go") != (bc.Qual == "Go") &&
		(ac.Qual == "" || ac.Qual == "Go") && (bc.Qual == "" || bc.Qual == "Go") {
		return true
	}
	// `Int64` spells the same int64 `Int` renders (viewsCompatible's
	// rule, mirrored).
	if ac.Qual == "" && ac.Name == "Int64" && len(ac.Args) == 0 &&
		bc.Name == "Int" && len(bc.Args) == 0 &&
		(bc.Qual == "" || bc.Qual == "Go") {
		return true
	}
	if bc.Qual == "" && bc.Name == "Int64" && len(bc.Args) == 0 &&
		ac.Name == "Int" && len(ac.Args) == 0 &&
		(ac.Qual == "" || ac.Qual == "Go") {
		return true
	}
	if ac.Name == "Uint" && len(ac.Args) == 0 && bc.Name == "Uint" &&
		len(bc.Args) == 0 && (ac.Qual == "Go") != (bc.Qual == "Go") &&
		(ac.Qual == "" || ac.Qual == "Go") && (bc.Qual == "" || bc.Qual == "Go") {
		return true
	}
	if ac.Qual == "" && ac.Name == "Uint64" && len(ac.Args) == 0 &&
		bc.Name == "Uint" && len(bc.Args) == 0 &&
		(bc.Qual == "" || bc.Qual == "Go") {
		return true
	}
	if bc.Qual == "" && bc.Name == "Uint64" && len(bc.Args) == 0 &&
		ac.Name == "Uint" && len(ac.Args) == 0 &&
		(ac.Qual == "" || ac.Qual == "Go") {
		return true
	}
	// `List T` against `Go.Slice T` — element-wise.
	if ac.Qual == "" && ac.Name == "List" && len(ac.Args) == 1 &&
		bc.Qual == "Go" && bc.Name == "Slice" && len(bc.Args) == 1 {
		return gomRawCompatible(ac.Args[0], bc.Args[0])
	}
	if bc.Qual == "" && bc.Name == "List" && len(bc.Args) == 1 &&
		ac.Qual == "Go" && ac.Name == "Slice" && len(ac.Args) == 1 {
		return gomRawCompatible(ac.Args[0], bc.Args[0])
	}
	// Same constructor, argument-wise.
	if ac.Qual == bc.Qual && ac.Name == bc.Name && len(ac.Args) == len(bc.Args) {
		for i := range ac.Args {
			if !gomRawCompatible(ac.Args[i], bc.Args[i]) {
				return false
			}
		}
		return true
	}
	return false
}

// isGoErrorCon reports whether t is the builtin `GoError`.
func isGoErrorCon(t types.Type) bool {
	cc, ok := types.Chase(t).(*types.Con)
	return ok && cc.Qual == "" && cc.Name == "GoError" && len(cc.Args) == 0
}
