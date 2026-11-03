package goimport

// go/types → Gom type mapping (docs/decisions/2026-11-03-go-types-mapping.md).
//
// Every loaded parameter and result is mapped twice: the Gom view
// (what check.Signature.Type carries and users see) and the Go view
// (what the generated code needs, carried by lower.GoSig's
// Params/ResTypes). The views agree everywhere except `int`, whose
// Gom spelling `Int` renders `int64` — the Go view keeps `Go.Int`
// there so lowering can record the real conversion.
//
// Types the mapping cannot express are not silently approximated:
// mapType reports a reason and the owning symbol becomes Hidden —
// the checker surfaces that reason as E0501 at every use site.

import (
	"fmt"
	"go/types"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/lower"
	gt "github.com/podhmo/gom/internal/types"
)

// mapSignature derives the two signature views of a Go callable.
// reason is "" on success.
//
// A generic Go function ([T any], [E comparable]) maps to a Gom
// signature over rigid type variables in declared order — recorded
// on Signature.TypeParams so CallInfo.TypeArgs lines up with the
// emitted `pkg.F[T1, T2]`. Only `any` and `comparable` constraints
// are callable (spec section 8.5); other constraints hide the
// symbol with the reason E0501 surfaces.
// hiddenSym is why a member stays unexposed in Gom — the reason the
// checker reports at each use site plus the diagnostic code to report
// it under ("" = the E0501 default).
type hiddenSym struct {
	reason string
	code   string
}

func (l *Loader) mapSignature(sig *types.Signature) (check.Signature, lower.GoSig, hiddenSym) {
	if sig.RecvTypeParams().Len() > 0 {
		return check.Signature{}, lower.GoSig{}, hiddenSym{reason: "generic receiver type parameters are not supported yet"}
	}
	var tvars []*gt.Var
	var gcons []check.GoConstraint
	if n := sig.TypeParams().Len(); n > 0 {
		l.tps = map[types.Object]*gt.Var{}
		defer func() { l.tps = nil }()
		tvars = make([]*gt.Var, n)
		gcons = make([]check.GoConstraint, n)
		for i := 0; i < n; i++ {
			tp := sig.TypeParams().At(i)
			k, reason := callableConstraint(tp.Constraint())
			if reason != "" {
				return check.Signature{}, lower.GoSig{}, hiddenSym{
					reason: fmt.Sprintf("type parameter %s: %s", tp.Obj().Name(), reason),
					code:   "E0506",
				}
			}
			v := &gt.Var{ID: goTypeParamVarBase + l.nextVar, Name: tp.Obj().Name()}
			l.nextVar++
			tvars[i] = v
			gcons[i] = k
			l.tps[tp.Obj()] = v
		}
	}
	params := sig.Params()
	ps := make([]gt.FuncParam, 0, params.Len())
	rawP := make([]gt.Type, 0, params.Len())
	for i := 0; i < params.Len(); i++ {
		pt := params.At(i).Type()
		mt, reason := l.mapBoundaryType(pt, false)
		if reason != "" {
			return check.Signature{}, lower.GoSig{}, hiddenSym{reason: fmt.Sprintf("parameter %s: %s", params.At(i).Name(), reason)}
		}
		rt, reason := l.mapType(pt, true)
		if reason != "" {
			return check.Signature{}, lower.GoSig{}, hiddenSym{reason: fmt.Sprintf("parameter %s: %s", params.At(i).Name(), reason)}
		}
		if !viewsCompatible(mt, rt) {
			return check.Signature{}, lower.GoSig{}, hiddenSym{reason: fmt.Sprintf("parameter %s: %s", params.At(i).Name(), divergentReason(pt))}
		}
		ps = append(ps, gt.P(mt))
		rawP = append(rawP, rt)
	}

	results := sig.Results()
	rawR := make([]gt.Type, 0, results.Len())
	for i := 0; i < results.Len(); i++ {
		rt, reason := l.mapType(results.At(i).Type(), true)
		if reason != "" {
			return check.Signature{}, lower.GoSig{}, hiddenSym{reason: fmt.Sprintf("result %d: %s", i, reason)}
		}
		rawR = append(rawR, rt)
	}
	var ret gt.Type
	var shape core.GoResults
	switch results.Len() {
	case 0:
		ret = gt.Unit
		shape = core.GoResultsNone
	case 1:
		t0 := results.At(0).Type()
		if isError(t0) {
			ret = gt.Result(gt.Unit, gt.GoError)
			shape = core.GoResultsErrOnly
			break
		}
		mt, reason := l.mapBoundaryType(t0, false)
		if reason != "" {
			return check.Signature{}, lower.GoSig{}, hiddenSym{reason: fmt.Sprintf("result 0: %s", reason)}
		}
		if !viewsCompatible(mt, rawR[0]) {
			return check.Signature{}, lower.GoSig{}, hiddenSym{reason: fmt.Sprintf("result 0: %s", divergentReason(t0))}
		}
		ret = mt
		shape = core.GoResultsOne
	case 2:
		t0, t1 := results.At(0).Type(), results.At(1).Type()
		m0, reason := l.mapBoundaryType(t0, false)
		if reason != "" {
			return check.Signature{}, lower.GoSig{}, hiddenSym{reason: fmt.Sprintf("result 0: %s", reason)}
		}
		if !viewsCompatible(m0, rawR[0]) {
			return check.Signature{}, lower.GoSig{}, hiddenSym{reason: fmt.Sprintf("result 0: %s", divergentReason(t0))}
		}
		if isError(t1) {
			ret = gt.Result(m0, gt.GoError)
			shape = core.GoResultsErr
			break
		}
		m1, reason := l.mapBoundaryType(t1, false)
		if reason != "" {
			return check.Signature{}, lower.GoSig{}, hiddenSym{reason: fmt.Sprintf("result 1: %s", reason)}
		}
		if !viewsCompatible(m1, rawR[1]) {
			return check.Signature{}, lower.GoSig{}, hiddenSym{reason: fmt.Sprintf("result 1: %s", divergentReason(t1))}
		}
		ret = gt.Tup(m0, m1)
		shape = core.GoResultsPair
	default:
		return check.Signature{}, lower.GoSig{}, hiddenSym{reason: fmt.Sprintf("%d results — only 0, 1 or 2 are representable", results.Len())}
	}

	gs := lower.GoSig{
		Arity:    params.Len(),
		Variadic: sig.Variadic(),
		Results:  shape,
		Params:   rawP,
		ResTypes: rawR,
		// TypeParams records the callee's own rigid variables in
		// declared order — lowering substitutes the call site's
		// type arguments (CallInfo.TypeArgs) into Params/ResTypes
		// before picking conversions.
		TypeParams: tvars,
	}
	ck := check.Signature{
		Type:     gt.Fn(ret, ps...),
		Variadic: sig.Variadic(),
		// DeclArity derives from Type: the mapped signature is flat
		// and never returns a function (func-typed results are
		// rejected below), so the derived arity is the declared one.
		DeclArity: 0,
		// TypeParams/GoConstraints carry the generic callee's
		// declared order and constraint kinds — instantiateSig
		// orders CallInfo.TypeArgs by them and the post-inference
		// pass validates the solved arguments (E0508).
		TypeParams:    tvars,
		GoConstraints: gcons,
	}
	return ck, gs, hiddenSym{}
}

// universeComparable is the type set a `comparable` type parameter
// must match exactly — `interface{comparable}` spells the same set
// and is admitted, while `~int`, `int|string` and
// `interface{comparable; ~int}` are proper subsets IsComparable alone
// cannot tell apart: their elements are all comparable, but the
// generated `pkg.F[int64]` instantiation would violate the
// constraint. Identity, not the IsComparable flag, is the test.
var universeComparable = types.Universe.Lookup("comparable").Type().Underlying()

// callableConstraint classifies a Go type-parameter constraint for
// the v1 callable set — `any` and `comparable` only (spec section
// 8.5). Everything else — method sets, unions, `~` approximations,
// intersections — hides the symbol with the reason E0506 surfaces.
// The strictness is deliberate: widening to `IsMethodSet`-style or
// structural `~[]E` constraints is a spec amendment, not an
// oversight.
func callableConstraint(c types.Type) (check.GoConstraint, string) {
	iface, ok := types.Unalias(c).Underlying().(*types.Interface)
	if !ok {
		return 0, fmt.Sprintf("constraint %s is not an interface", c)
	}
	switch {
	case iface.Empty():
		return check.GoConstraintAny, ""
	case types.Identical(iface, universeComparable):
		return check.GoConstraintComparable, ""
	default:
		return 0, fmt.Sprintf("constraint %s is not supported (only any and comparable are)", c)
	}
}

// viewsCompatible reports whether the two views of one boundary type
// can reach each other through the conversions Core IR can express:
// Equal views need none, the top-level int ↔ Go.Int split is a
// numeric conversion, and the List ↔ Go.Slice boundary pair is a
// copy — legal only when the elements' views are Equal, because an
// element-wise conversion inside a container cannot be spelled yet.
// Anything else (int nested in a slice/map/pointer/channel, a nested
// slice pair, …) hides the symbol rather than surfacing as an
// internal lowering error.
func viewsCompatible(g, r gt.Type) bool {
	g, r = gt.Chase(g), gt.Chase(r)
	if gt.Equal(g, r) {
		return true
	}
	gc, gok := g.(*gt.Con)
	rc, rok := r.(*gt.Con)
	if !gok || !rok {
		return false
	}
	if gc.Qual == "" && gc.Name == "Int" && len(gc.Args) == 0 &&
		rc.Qual == "Go" && rc.Name == "Int" && len(rc.Args) == 0 {
		return true
	}
	if gc.Qual == "" && gc.Name == "List" && len(gc.Args) == 1 &&
		rc.Qual == "Go" && rc.Name == "Slice" && len(rc.Args) == 1 {
		return gt.Equal(gc.Args[0], rc.Args[0])
	}
	return false
}

// divergentReason explains the hidden reason for a boundary type
// whose Gom and Go views split inside a container — the element-wise
// conversion `[]int` or `map[string]int` would need cannot be
// spelled in Core IR yet, so the symbol stays unexposed like any
// other unrepresentable member.
func divergentReason(t types.Type) string {
	return fmt.Sprintf("type %s splits int and int64 inside a container — element-wise boundary conversions are not supported yet", t)
}

// gomType maps a go/types type to the Gom view — the entry used for
// non-function members (types, vars, consts).
func (l *Loader) gomType(t types.Type) (gt.Type, string) {
	return l.mapBoundaryType(t, false)
}

// mapBoundaryType maps a Go type standing at a signature boundary —
// a parameter, a result, or a member's declared type. In the Gom
// view a top-level `[]T` is `List T` (the boundary copies of spec
// section 8.5); nested `[]T` keeps the Go-owned `Go.Slice T` spelling
// of mapType. The raw Go view (goView) maps like mapType.
func (l *Loader) mapBoundaryType(t types.Type, goView bool) (gt.Type, string) {
	if !goView {
		if s, ok := t.(*types.Slice); ok {
			el, reason := l.mapType(s.Elem(), false)
			if reason != "" {
				return nil, reason
			}
			return gt.List(el), ""
		}
	}
	return l.mapType(t, goView)
}

// mapType maps a go/types type in a nested (non-boundary) position.
// goView selects the Go spelling where it differs from the Gom one —
// `int` → `Go.Int`, and `[]T` keeps the Go-owned `Go.Slice T` view
// (mapBoundaryType owns the boundary's `List` view).
func (l *Loader) mapType(t types.Type, goView bool) (gt.Type, string) {
	switch t := t.(type) {
	case *types.Basic:
		if t.Kind() == types.Int {
			// The one type whose Gom and Go spellings differ:
			// `int` is `Int` (`int64`) to Gom, `Go.Int` (`int`) to
			// the generated code.
			if goView {
				return gt.GoInt, ""
			}
			return gt.Int, ""
		}
		return basicType(t, goView)
	case *types.Named:
		return l.namedType(t)
	case *types.Alias:
		return l.mapType(types.Unalias(t), goView)
	case *types.Pointer:
		el, reason := l.mapType(t.Elem(), goView)
		if reason != "" {
			return nil, reason
		}
		return gt.Ptr(el), ""
	case *types.Slice:
		el, reason := l.mapType(t.Elem(), goView)
		if reason != "" {
			return nil, reason
		}
		return gt.GoSlice(el), ""
	case *types.Map:
		k, reason := l.mapType(t.Key(), goView)
		if reason != "" {
			return nil, reason
		}
		v, reason := l.mapType(t.Elem(), goView)
		if reason != "" {
			return nil, reason
		}
		return gt.GoMap(k, v), ""
	case *types.Chan:
		el, reason := l.mapType(t.Elem(), goView)
		if reason != "" {
			return nil, reason
		}
		switch t.Dir() {
		case types.SendRecv:
			return gt.Chan(el), ""
		case types.RecvOnly:
			return gt.RecvChan(el), ""
		case types.SendOnly:
			return gt.SendChan(el), ""
		default:
			return nil, fmt.Sprintf("unsupported channel direction %d", t.Dir())
		}
	case *types.Interface:
		if isErrorIface(t) {
			return gt.GoError, ""
		}
		if t.Empty() {
			return gt.GoAny, ""
		}
		return nil, "anonymous interfaces with methods are not supported yet"
	case *types.Signature:
		return nil, "func-typed parameters and results are not supported yet"
	case *types.Array:
		return nil, "Go array types are not supported yet"
	case *types.TypeParam:
		// Inside a generic signature's scope the type parameter is
		// the rigid variable mapSignature minted — the generic
		// callee's `T` is a Gom `a` the caller instantiates.
		if l.tps != nil {
			if v, ok := l.tps[t.Obj()]; ok {
				return v, ""
			}
		}
		return nil, "generic Go types are not supported yet"
	case *types.Tuple:
		return nil, "tuple types only appear inside signatures"
	default:
		return nil, fmt.Sprintf("unsupported Go type %s", t)
	}
}

// basicType maps the Go builtin types; untyped kinds take their Go
// default type (a const's reported type is untyped — `math.Pi` is an
// untyped float constant and behaves as `float64` at use sites).
// goView selects the Go spelling where it differs from the Gom one —
// like the `types.Int` case in mapType, the untyped int default `int`
// is `Go.Int` there: `sz := strconv.IntSize` binds a real `int`.
func basicType(t *types.Basic, goView bool) (gt.Type, string) {
	switch t.Kind() {
	case types.Bool, types.UntypedBool:
		return gt.Bool, ""
	case types.UntypedInt:
		// An untyped int constant's default type is `int` — it
		// shares the `int` split view (`Int` / `Go.Int`), or a
		// `let`-bound reference would infer Go `int` while the
		// Gom view claims `Int` (`int64`).
		if goView {
			return gt.GoInt, ""
		}
		return gt.Int, ""
	case types.Int64:
		return gt.Int, ""
	case types.Int32:
		return gt.Int32, ""
	case types.Float64, types.UntypedFloat:
		return gt.Float, ""
	case types.String, types.UntypedString:
		return gt.String, ""
	case types.Uint8:
		return nil, "byte/uint8 is not supported yet"
	case types.Uint16, types.Uint32, types.Uint64, types.Uint, types.Uintptr:
		return nil, fmt.Sprintf("%s is not supported yet", t.Name())
	case types.Int8, types.Int16:
		return nil, fmt.Sprintf("%s is not supported yet", t.Name())
	case types.Float32:
		return nil, "float32 is not supported yet"
	case types.Complex64, types.Complex128, types.UntypedComplex:
		return nil, "complex numbers are not supported yet"
	case types.UnsafePointer:
		return nil, "unsafe.Pointer is not supported"
	case types.UntypedRune:
		return gt.Char, ""
	case types.UntypedNil:
		return nil, "untyped nil has no type"
	default:
		return nil, fmt.Sprintf("unsupported basic type %s", t.Name())
	}
}

// namedType maps a Go named type (or named interface) to its
// types.Go handle and registers the named for method lookup.
func (l *Loader) namedType(t *types.Named) (gt.Type, string) {
	obj := t.Obj()
	if obj.Pkg() == nil {
		// The universe-scope `error` named interface.
		if obj.Name() == "error" {
			return gt.GoError, ""
		}
		return nil, fmt.Sprintf("unsupported universe type %s", obj.Name())
	}
	if t.TypeParams().Len() > 0 {
		return nil, fmt.Sprintf("generic named type %s is not supported yet", obj.Name())
	}
	ref := gt.NamedRef{Path: obj.Pkg().Path(), Name: obj.Name()}
	l.named[ref] = t
	if t.TypeArgs().Len() > 0 {
		return nil, fmt.Sprintf("instantiated generic type %s is not supported yet", obj.Name())
	}
	return &gt.Go{Ref: ref}, ""
}

// isError reports whether t is Go's error — the named interface of
// the universe scope, or an interface equal to it.
func isError(t types.Type) bool {
	t = types.Unalias(t)
	if n, ok := t.(*types.Named); ok {
		o := n.Obj()
		return o.Pkg() == nil && o.Name() == "error"
	}
	if i, ok := t.(*types.Interface); ok {
		return isErrorIface(i)
	}
	return false
}

// isErrorIface reports whether i is the error interface — one method
// `Error() string`.
func isErrorIface(i *types.Interface) bool {
	if i.NumMethods() != 1 {
		return false
	}
	m := i.Method(0)
	if m.Name() != "Error" {
		return false
	}
	sig, ok := m.Type().(*types.Signature)
	if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return false
	}
	b, ok := sig.Results().At(0).Type().(*types.Basic)
	return ok && b.Kind() == types.String
}
