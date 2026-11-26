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
// emitted `pkg.F[T1, T2]`. The admitted constraint set of spec
// section 8.5 — `any`, `comparable`, `~[]E`, `~map[K]V` and
// concrete-term unions — is callable; other constraints hide the
// symbol with the reason E0506 surfaces.
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
		for i := range n {
			tp := sig.TypeParams().At(i)
			k, reason := callableConstraint(tp.Constraint(), i)
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
	for v := range params.Variables() {
		pt := v.Type()
		mt, reason := l.mapBoundaryType(pt, false)
		if reason != "" {
			return check.Signature{}, lower.GoSig{}, hiddenSym{reason: fmt.Sprintf("parameter %s: %s", v.Name(), reason)}
		}
		rt, reason := l.mapType(pt, true, true)
		if reason != "" {
			return check.Signature{}, lower.GoSig{}, hiddenSym{reason: fmt.Sprintf("parameter %s: %s", v.Name(), reason)}
		}
		if !viewsCompatible(mt, rt) {
			return check.Signature{}, lower.GoSig{}, hiddenSym{reason: fmt.Sprintf("parameter %s: %s", v.Name(), divergentReason(pt))}
		}
		ps = append(ps, gt.P(mt))
		rawP = append(rawP, rt)
	}

	results := sig.Results()
	rawR := make([]gt.Type, 0, results.Len())
	for i := 0; i < results.Len(); i++ {
		rt, reason := l.mapType(results.At(i).Type(), true, true)
		if reason != "" {
			return check.Signature{}, lower.GoSig{}, hiddenSym{reason: fmt.Sprintf("result %d: %s", i, reason)}
		}
		rawR = append(rawR, rt)
	}
	var ret gt.Type
	var shape core.GoResults
	var resNilable bool
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
			if methodIface(t0) {
				// `(method-bearing interface, error)` —
				// `(error, error)`, `(net.Error, error)`,
				// `(io.Reader, error)`, user interfaces: the Ok
				// payload is nil-able and a nil method-bearing
				// interface is unobservable-dangerous — every
				// Gom operation on it (a Go method call)
				// panics on nil — so the honest derived view
				// wraps it; `Ok None` reads `(nil, nil)`
				// instead of smuggling a nil interface into
				// `Ok`. The never-nil claim `Result I
				// GoError` stays available as a *declared*
				// view, verified at the boundary
				// (docs/decisions/
				// 2026-11-05-error-pair-ok-payload.md,
				// 2026-11-06-nilable-iface-pair-ok-payload.md).
				// `any` and named empty interfaces take the
				// verbatim view below — their nil is inert.
				ret = gt.Result(gt.Option(m0), gt.GoError)
				shape = core.GoResultsErr
				resNilable = true
				break
			}
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
		// Three or more results — the N-ary mapping of spec
		// section 8.3 (docs/decisions/2026-11-18-nary-go-results.md):
		// a trailing `error` folds the payload into
		// `Result (Tuple ...) GoError`, anything else becomes a
		// plain N-tuple. The cap is rt.Tuple8's arity.
		if results.Len() > 8 {
			return check.Signature{}, lower.GoSig{}, hiddenSym{reason: fmt.Sprintf("%d results — at most 8 are representable", results.Len())}
		}
		n := results.Len()
		lastIsErr := isError(results.At(n - 1).Type())
		payload := n
		if lastIsErr {
			payload = n - 1
		}
		elems := make([]gt.Type, payload)
		for i := 0; i < payload; i++ {
			ti := results.At(i).Type()
			mi, reason := l.mapBoundaryType(ti, false)
			if reason != "" {
				return check.Signature{}, lower.GoSig{}, hiddenSym{reason: fmt.Sprintf("result %d: %s", i, reason)}
			}
			if !viewsCompatible(mi, rawR[i]) {
				return check.Signature{}, lower.GoSig{}, hiddenSym{reason: fmt.Sprintf("result %d: %s", i, divergentReason(ti))}
			}
			// Inside a Result payload, a method-bearing interface
			// element is nil-able and dangerous, so the honest
			// derived view wraps it in Option — the same rule the
			// `(T, error)` pair applies to its payload (an
			// `(error, error, error)` result is
			// `Result (Tuple (Option GoError) GoError) GoError`).
			// Plain tuples wrap nothing, like `(A, B)` pairs.
			if lastIsErr && methodIface(ti) {
				mi = gt.Option(mi)
			}
			elems[i] = mi
		}
		if lastIsErr {
			ret = gt.Result(gt.Tup(elems...), gt.GoError)
			shape = core.GoResultsTupErr
		} else {
			ret = gt.Tup(elems...)
			shape = core.GoResultsNary
		}
	}

	gs := lower.GoSig{
		Arity:      params.Len(),
		Variadic:   sig.Variadic(),
		Results:    shape,
		Params:     rawP,
		ResTypes:   rawR,
		ResNilable: resNilable,
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
// the callable set of spec section 8.5: `any`, `comparable`, the
// structural `~[]E`/`~map[K]V` shapes whose operands are other type
// parameters of the same signature, and unions of concrete terms
// (cmp.Ordered's basic terms, `[]byte | string`). Everything else —
// method sets, unions mentioning type parameters beyond those two
// shapes, intersections, `~` over other forms — hides the symbol
// with the reason E0506 surfaces, named by class so the hidden set
// never silently widens.
func callableConstraint(c types.Type, self int) (check.GoConstraint, string) {
	iface, ok := types.Unalias(c).Underlying().(*types.Interface)
	if !ok {
		return check.GoConstraint{}, fmt.Sprintf("constraint %s is not an interface", c)
	}
	switch {
	case iface.Empty():
		return check.GoConstraint{Kind: check.GoConstraintAny}, ""
	case types.Identical(iface, universeComparable):
		return check.GoConstraint{Kind: check.GoConstraintComparable}, ""
	}
	// IsMethodSet covers the all-methods spelling; a constraint
	// mixing terms AND methods has IsMethodSet false but is a
	// method-set constraint all the same — its type set requires
	// the methods, so admitting it would drop that requirement.
	if iface.IsMethodSet() || iface.NumMethods() > 0 {
		return check.GoConstraint{}, fmt.Sprintf("constraint %s carries a method set", c)
	}
	terms, ok := unionTerms(iface)
	if !ok {
		return check.GoConstraint{}, fmt.Sprintf("constraint %s is an intersection outside the admitted set", c)
	}
	if gc, ok := structuralConstraint(terms, self); ok {
		gc.Desc = c.String()
		return gc, ""
	}
	for _, tm := range terms {
		if hasTypeParam(tm.Type) {
			return check.GoConstraint{}, fmt.Sprintf("constraint %s mixes type parameters into a union", c)
		}
	}
	return check.GoConstraint{Kind: check.GoConstraintUnion, Terms: terms, Desc: c.String()}, ""
}

// unionTerms collects a constraint interface's type-set terms,
// flattening embedded (possibly named) constraint interfaces such
// as `interface{ cmp.Ordered }`. It fails on embedded shapes that
// are not plain union members — `comparable`, method-bearing named
// interfaces, type parameters — so intersections land on the
// hidden set instead of being silently weakened to their union
// part.
func unionTerms(iface *types.Interface) ([]check.GoUnionTerm, bool) {
	// More than one embedded element — or any explicit method —
	// makes the interface an intersection: flattening every
	// embedded union would admit the disjunction of all parts,
	// a strictly larger set than the constraint's own.
	if iface.NumMethods() > 0 || iface.NumEmbeddeds() != 1 {
		return nil, false
	}
	var terms []check.GoUnionTerm
	for i := 0; i < iface.NumEmbeddeds(); i++ {
		switch e := types.Unalias(iface.EmbeddedType(i)).(type) {
		case *types.Union:
			for j := 0; j < e.Len(); j++ {
				tm := e.Term(j)
				terms = append(terms, check.GoUnionTerm{Type: tm.Type(), Approx: tm.Tilde()})
			}
		case *types.Interface:
			ts, ok := unionTerms(e)
			if !ok {
				return nil, false
			}
			terms = append(terms, ts...)
		case *types.Named:
			i2, ok := e.Underlying().(*types.Interface)
			if !ok {
				// An embedded concrete named type is an exact
				// one-type term.
				terms = append(terms, check.GoUnionTerm{Type: e})
				continue
			}
			// A named union interface keeps its terms — `cmp.Ordered`
			// is itself comparable, which describes its terms, not a
			// reason to hide it. A method-bearing named interface is
			// not a union and stays hidden.
			if i2.IsMethodSet() {
				return nil, false
			}
			ts, ok := unionTerms(i2)
			if !ok {
				return nil, false
			}
			terms = append(terms, ts...)
		case *types.Basic:
			terms = append(terms, check.GoUnionTerm{Type: e})
		default:
			return nil, false
		}
	}
	return terms, true
}

// structuralConstraint recognizes the two single-term `~` shapes
// the callable set admits: `~[]E` and `~map[K]V`, where E/K/V are
// type parameters of the same signature (TypeParam.Index is the
// declared-order position, matching Signature.TypeParams).
func structuralConstraint(terms []check.GoUnionTerm, self int) (check.GoConstraint, bool) {
	if len(terms) != 1 || !terms[0].Approx {
		return check.GoConstraint{}, false
	}
	// An operand naming the constrained parameter itself
	// (`S ~[]S`) is a recursive constraint — no Gom argument
	// can build an element of its own shape.
	switch t := terms[0].Type.(type) {
	case *types.Slice:
		if tp, ok := t.Elem().(*types.TypeParam); ok && tp.Index() != self {
			return check.GoConstraint{Kind: check.GoConstraintSlice, Elem: tp.Index(), Key: -1, Val: -1}, true
		}
	case *types.Map:
		k, kok := t.Key().(*types.TypeParam)
		v, vok := t.Elem().(*types.TypeParam)
		if kok && vok && k.Index() != self && v.Index() != self {
			return check.GoConstraint{Kind: check.GoConstraintMap, Key: k.Index(), Val: v.Index(), Elem: -1}, true
		}
	}
	return check.GoConstraint{}, false
}

// hasTypeParam reports whether a union term mentions a type
// parameter — unions that do are outside the admitted set (the
// two structural shapes are intercepted before this check).
func hasTypeParam(t types.Type) bool {
	switch t := t.(type) {
	case *types.TypeParam:
		return true
	case *types.Slice:
		return hasTypeParam(t.Elem())
	case *types.Array:
		return hasTypeParam(t.Elem())
	case *types.Pointer:
		return hasTypeParam(t.Elem())
	case *types.Chan:
		return hasTypeParam(t.Elem())
	case *types.Map:
		return hasTypeParam(t.Key()) || hasTypeParam(t.Elem())
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if hasTypeParam(t.Field(i).Type()) {
				return true
			}
		}
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			if hasTypeParam(t.At(i).Type()) {
				return true
			}
		}
	case *types.Signature:
		return hasTypeParam(t.Params()) || hasTypeParam(t.Results())
	case *types.Union:
		for i := 0; i < t.Len(); i++ {
			if hasTypeParam(t.Term(i).Type()) {
				return true
			}
		}
	case *types.Named:
		if args := t.TypeArgs(); args != nil {
			for i := 0; i < args.Len(); i++ {
				if hasTypeParam(args.At(i)) {
					return true
				}
			}
		}
	case *types.Basic, *types.Interface:
	case nil:
	default:
	}
	return false
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
// viewsCompatible is the Loader's BindExtern-level view pairing:
// the shape rule of the package function plus the named-func
// equivalence — an extern declaring `pkg.F` (a method-less named
// func type's handle) pairs with the raw `Go.FuncN` the member's
// boundary view reads.
func (l *Loader) viewsCompatible(g, r gt.Type) bool {
	if viewsCompatible(g, r) {
		return true
	}
	if _, ok := gt.Chase(g).(*gt.Go); ok {
		if fn, ok := l.funcNOfNamed(g); ok && viewsCompatible(fn, r) {
			return true
		}
	}
	if _, ok := gt.Chase(r).(*gt.Go); ok {
		if fn, ok := l.funcNOfNamed(r); ok && viewsCompatible(g, fn) {
			return true
		}
	}
	return false
}

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
	// `Int64` spells the same int64 `Int` renders — an extern
	// declaration may write it for a raw `int` or `int64` (the
	// int64 case's raw view is Int itself, so only Go.Int needs
	// pairing; the Equal check above already covers Int).
	if gc.Qual == "" && gc.Name == "Int64" && len(gc.Args) == 0 &&
		(rc.Qual == "" || rc.Qual == "Go") && rc.Name == "Int" && len(rc.Args) == 0 {
		return true
	}
	// `Uint64` spells the same uint64 `Uint` renders — an extern
	// declaration may write it for a raw `uint` or `uint64`.
	if gc.Qual == "" && gc.Name == "Uint64" && len(gc.Args) == 0 &&
		(rc.Qual == "" || rc.Qual == "Go") && rc.Name == "Uint" && len(rc.Args) == 0 {
		return true
	}
	if gc.Qual == "" && (gc.Name == "Uint" || gc.Name == "Uint64") && len(gc.Args) == 0 &&
		rc.Qual == "Go" && rc.Name == "Uint" && len(rc.Args) == 0 {
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
	return fmt.Sprintf("type %s splits int/uint and their 64-bit spellings inside a container — element-wise boundary conversions are not supported yet", t)
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
			el, reason := l.mapType(s.Elem(), false, false)
			if reason != "" {
				return nil, reason
			}
			return gt.List(el), ""
		}
	}
	return l.mapType(t, goView, true)
}

// mapType maps a go/types type. goView selects the Go spelling where
// it differs from the Gom one — `int` → `Go.Int`, and `[]T` keeps
// the Go-owned `Go.Slice T` view (mapBoundaryType owns the
// boundary's `List` view). assignable is true only at positions Go's
// assignability already pairs named and unnamed spellings — the top
// level of a signature parameter or result, a member's type, or a
// struct field — where a method-less named func type may read as its
// `Go.FuncN`. Inside a container (`[]F`, `*F`, `map[K]F`, `chan F`)
// or a nested signature the name is load-bearing (`[]F` and
// `[]func()` are different Go types), so the named handle stays.
func (l *Loader) mapType(t types.Type, goView, assignable bool) (gt.Type, string) {
	switch t := t.(type) {
	case *types.Basic:
		if t.Kind() == types.Int {
			// `int` is `Int` (`int64`) to Gom, `Go.Int` (`int`) to
			// the generated code; basicType owns the same split for
			// `uint` (`Uint`/`Go.Uint`).
			if goView {
				return gt.GoInt, ""
			}
			return gt.Int, ""
		}
		return basicType(t, goView)
	case *types.Named:
		return l.namedType(t, assignable)
	case *types.Alias:
		return l.mapType(types.Unalias(t), goView, assignable)
	case *types.Pointer:
		el, reason := l.mapType(t.Elem(), goView, false)
		if reason != "" {
			return nil, reason
		}
		return gt.Ptr(el), ""
	case *types.Slice:
		el, reason := l.mapType(t.Elem(), goView, false)
		if reason != "" {
			return nil, reason
		}
		return gt.GoSlice(el), ""
	case *types.Map:
		k, reason := l.mapType(t.Key(), goView, false)
		if reason != "" {
			return nil, reason
		}
		v, reason := l.mapType(t.Elem(), goView, false)
		if reason != "" {
			return nil, reason
		}
		return gt.GoMap(k, v), ""
	case *types.Chan:
		el, reason := l.mapType(t.Elem(), goView, false)
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
	case *types.Struct:
		if t.NumFields() == 0 {
			// `struct{}` is the element type of Go's signal
			// channels (`chan struct{}`, `<-chan struct{}` —
			// `ctx.Done()`); it maps to Unit, whose Go
			// representation is `struct{}` (spec section 10 —
			// `RecvChan Unit` receives `struct{}{}` values).
			return gt.Unit, ""
		}
		return nil, "Go struct types are not supported yet"
	case *types.Interface:
		if isErrorIface(t) {
			return gt.GoError, ""
		}
		if t.Empty() {
			return gt.GoAny, ""
		}
		return nil, "anonymous interfaces with methods are not supported yet"
	case *types.Signature:
		return l.mapFuncType(t)
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

// mapFuncType maps a Go func type — `func(A, B) R` — to the Go-owned
// `Go.FuncN` type: the name carries the parameter count and the type
// arguments are the raw Go views of the parameters followed by the
// results (docs/decisions/2026-11-17-func-interop.md). The Gom and
// Go views agree — both are the FuncN type — so it maps identically
// in either view; a value of it is produced only by `Go.funcN`.
//
// The admitted shape is bounded: at most gt.GoFuncMaxArity
// parameters, no variadic or generic signature, and at most eight
// results (rt.Tuple8's arity). A result list longer than one
// reads back through the N-ary folding — a trailing `error`
// spells `Result ... GoError`, anything else a tuple
// (docs/decisions/2026-11-18-nary-go-results.md). Anything else
// keeps the member hidden like any other unrepresentable type.
func (l *Loader) mapFuncType(sig *types.Signature) (gt.Type, string) {
	if sig.Variadic() {
		return nil, "variadic func types are not supported yet"
	}
	if sig.TypeParams().Len() > 0 || sig.RecvTypeParams().Len() > 0 {
		return nil, "generic func types are not supported yet"
	}
	n := sig.Params().Len()
	if n > gt.GoFuncMaxArity {
		return nil, fmt.Sprintf("func types with %d parameters are not supported yet (max %d)", n, gt.GoFuncMaxArity)
	}
	res := sig.Results()
	if res.Len() > 8 {
		return nil, fmt.Sprintf("func types with %d results are not supported yet (max %d)", res.Len(), 8)
	}
	args := make([]gt.Type, 0, n+res.Len())
	for v := range sig.Params().Variables() {
		rt, reason := l.mapType(v.Type(), true, false)
		if reason != "" {
			return nil, fmt.Sprintf("func parameter %s: %s", v.Name(), reason)
		}
		args = append(args, rt)
	}
	for i := 0; i < res.Len(); i++ {
		rt, reason := l.mapType(res.At(i).Type(), true, false)
		if reason != "" {
			return nil, fmt.Sprintf("func result %d: %s", i, reason)
		}
		args = append(args, rt)
	}
	return gt.GoFunc(n, args...), ""
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
		// `byte` and `uint8` are the same basic kind; both read
		// as `Byte` in the Gom view.
		return gt.Byte, ""
	case types.Uint64:
		return gt.Uint, ""
	case types.Uint:
		if goView {
			return gt.GoUint, ""
		}
		return gt.Uint, ""
	case types.Uint16, types.Uint32, types.Uintptr:
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
func (l *Loader) namedType(t *types.Named, assignable bool) (gt.Type, string) {
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
	if t.TypeArgs().Len() > 0 {
		return nil, fmt.Sprintf("instantiated generic type %s is not supported yet", obj.Name())
	}
	ref := gt.NamedRef{Path: obj.Pkg().Path(), Name: obj.Name()}
	l.named[ref] = t
	// A method-less named type over a signature the FuncN mapping
	// admits is the `Go.FuncN` itself — `context.CancelFunc` reads
	// `func()`, `fs.WalkDirFunc` its four-parameter shape — so both
	// directions (`Go.funcN` in, `Go.fnN` out) apply and the Go
	// boundary's own assignability rules pair the spellings. A
	// method-bearing one (`http.HandlerFunc`'s ServeHTTP) keeps the
	// named handle: the name owns the methods a FuncN cannot spell.
	// The l.mapping guard answers the named handle to a signature
	// that names itself (`type F func() F`) — the outer occurrence
	// still reads FuncN, its own reference the ref. assignable
	// confines the reading to positions where Go pairs the named
	// and unnamed spellings itself — a nested `[]F` would misread
	// as `[]func()` (different types) without it.
	if t.NumMethods() == 0 && assignable && !l.mapping[ref] {
		if sig, ok := t.Underlying().(*types.Signature); ok {
			if l.mapping == nil {
				l.mapping = map[gt.NamedRef]bool{}
			}
			l.mapping[ref] = true
			ft, reason := l.mapFuncType(sig)
			delete(l.mapping, ref)
			if reason == "" {
				return ft, ""
			}
		}
	}
	return &gt.Go{Ref: ref}, ""
}

// methodIface reports whether t's underlying is an interface with
// at least one method — `error`, `net.Error`, `io.Reader`, or a
// user interface. A nil of such a type is unobservable-dangerous:
// every Gom operation on it (a Go method call) panics on nil, so
// derived `(I, error)` views wrap the payload and declared
// verbatim views carry the boundary-checked never-nil claim. `any`
// and named empty interfaces return false — their nil is inert.
func methodIface(t types.Type) bool {
	i, ok := types.Unalias(t).Underlying().(*types.Interface)
	return ok && i.NumMethods() > 0
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
