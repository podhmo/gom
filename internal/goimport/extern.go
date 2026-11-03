package goimport

// `extern go` binding (docs/spec.md section 8.4).
//
// An extern entry redeclares one exported function of the package
// the block names: the declared signature must agree with the raw
// Go signature in parameter count and view-compatible parameter
// types, and the declared result must be one the result shape
// admits — `Option T` over a (T, bool) comma-ok pair or a *T
// result, `Result T GoError` over (T, error), `(A, B)` over any
// other two-result shape, `T` over a single result, `Unit` over
// none.
//
// On a clean bind the declared signature becomes the member's
// Gom-facing view and the classified GoSig — the raw parameter and
// result types plus the result shape the conversion needs — is
// registered under the extern's own symbol, so lowering reads the
// override through GoSigOf exactly like a derived member's facts.

import (
	"fmt"
	"go/types"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/resolve"
	gt "github.com/podhmo/gom/internal/types"
)

// BindExtern implements check.ExternSource: it validates the
// declared extern signature against the Go member the entry
// overrides and registers the override's Go-view facts on the
// extern symbol. The checker reports each returned issue at the
// entry (E0509); a failed bind installs no facts — lowering sees no
// GoSig for the symbol and derives the result shape from the
// declared view (lower.go's goResultsOf heuristic), so the call
// still lowers against the signature the user wrote while the
// entry-level diagnostic explains the entry.
func (l *Loader) BindExtern(sym *resolve.Symbol, decl check.Signature) (check.Signature, []check.ExternIssue) {
	var sig *types.Signature
	issue := func(format string, args ...any) (check.Signature, []check.ExternIssue) {
		data := map[string]any{"declared": fmt.Sprint(decl.Type)}
		if sig != nil {
			data["go"] = sig.String()
		}
		return check.Signature{}, []check.ExternIssue{{Message: fmt.Sprintf(format, args...), Data: data}}
	}
	fn, ok := gt.Chase(decl.Type).(*gt.Func)
	if !ok {
		return issue("extern `%s` must declare a function signature", sym.Name)
	}
	if len(gt.FreeVars(fn)) > 0 {
		// A Go member is monomorphic — a type variable in the
		// declaration could be instantiated to shapes the raw
		// signature does not have.
		return issue("extern `%s` must be monomorphic — a declared type variable has no Go-side meaning", sym.Name)
	}
	pkg := l.pkgs[sym.Package]
	if pkg == nil {
		return issue("extern `%s`: Go package `%s` was not loaded", sym.Name, sym.Package)
	}
	msym := pkg.Members[sym.Name]
	if msym == nil {
		return issue("extern `fn %s` names no exported member of Go package `%s`", sym.Name, sym.Package)
	}
	m := l.syms[msym]
	if m == nil || m.sig == nil {
		if msym.Hidden != "" {
			return issue("extern `fn %s`: `%s.%s` is not exposed: %s", sym.Name, sym.Package, sym.Name, msym.Hidden)
		}
		return issue("extern `fn %s`: `%s.%s` is a %s, not a function", sym.Name, sym.Package, sym.Name, msym.Kind)
	}
	if msym.Hidden != "" {
		return issue("extern `fn %s`: `%s.%s` is not exposed: %s", sym.Name, sym.Package, sym.Name, msym.Hidden)
	}
	sig = m.sig
	if sig.TypeParams().Len() > 0 || sig.RecvTypeParams().Len() > 0 {
		// The instantiation argument for a generic callee comes
		// from the Gom signature's type variables — which an
		// extern may not declare — so there is nothing to emit
		// into `pkg.F[T]`.
		return issue("extern `fn %s`: generic Go functions cannot be overridden yet", sym.Name)
	}
	params := fn.Params
	zeroArg := sig.Params().Len() == 0 && len(params) == 1 &&
		gt.Equal(gt.Chase(params[0].Type), gt.Unit)
	if zeroArg {
		// `fn F ()` — or the explicit `(u: Unit)` — spells the
		// zero-argument Go function: the sole Unit parameter is
		// the `f ()` call form's Unit literal, not a Go argument.
		params = nil
	}
	if sig.Params().Len() != len(params) {
		return issue("extern `fn %s` declares %d parameter(s); `%s.%s` takes %d",
			sym.Name, len(params), sym.Package, sym.Name, sig.Params().Len())
	}
	rawP := make([]gt.Type, sig.Params().Len())
	for i := 0; i < sig.Params().Len(); i++ {
		rt, reason := l.mapType(sig.Params().At(i).Type(), true)
		if reason != "" {
			return issue("extern `fn %s`: the Go parameter %d is not representable: %s", sym.Name, i, reason)
		}
		rawP[i] = rt
		pt := gt.Chase(params[i].Type)
		if !groundType(pt) {
			// A meta from an unannotated (hand-built) parameter —
			// the checker's own unsolved-meta diagnostic covers it.
			continue
		}
		if !viewsCompatible(pt, rt) {
			return issue("extern `fn %s` declares parameter %d as %s, but `%s.%s` takes %s",
				sym.Name, i, pt, sym.Package, sym.Name, sig.Params().At(i).Type())
		}
	}

	res := sig.Results()
	rawR := make([]gt.Type, res.Len())
	for i := 0; i < res.Len(); i++ {
		rt, reason := l.mapType(res.At(i).Type(), true)
		if reason != "" {
			return issue("extern `fn %s`: the Go result %d is not representable: %s", sym.Name, i, reason)
		}
		rawR[i] = rt
	}
	ret := gt.Chase(fn.Ret)
	var shape core.GoResults
	switch res.Len() {
	case 0:
		if !gt.Equal(ret, gt.Unit) {
			return issue("extern `fn %s` declares %s, but `%s.%s` returns nothing — declare Unit",
				sym.Name, ret, sym.Package, sym.Name)
		}
		shape = core.GoResultsNone
	case 1:
		t0 := res.At(0).Type()
		if isError(t0) {
			if a, e, ok := resultArgs(ret); !ok || a == nil || !gt.Equal(a, gt.Unit) || !gt.Equal(e, gt.GoError) {
				return issue("extern `fn %s`: `%s.%s` returns error — declare `Result Unit GoError`, not %s",
					sym.Name, sym.Package, sym.Name, ret)
			}
			shape = core.GoResultsErrOnly
			break
		}
		if payload, ok := optionArgs(ret); ok {
			if _, isPtr := t0.(*types.Pointer); !isPtr {
				return issue("extern `fn %s` declares an Option result, but `%s.%s` returns %s — Option needs a (T, bool) or *T result",
					sym.Name, sym.Package, sym.Name, t0)
			}
			if !viewsCompatible(payload, rawR[0]) {
				return issue("extern `fn %s` declares Option payload %s, but `%s.%s` returns %s — the payload keeps the pointer, declare `Option (Ptr T)`",
					sym.Name, payload, sym.Package, sym.Name, t0)
			}
			shape = core.GoResultsNil
			break
		}
		if !viewsCompatible(ret, rawR[0]) {
			return issue("extern `fn %s` declares result %s, but `%s.%s` returns %s",
				sym.Name, ret, sym.Package, sym.Name, t0)
		}
		shape = core.GoResultsOne
	case 2:
		t0, t1 := res.At(0).Type(), res.At(1).Type()
		switch {
		case isError(t1):
			a, e, ok := resultArgs(ret)
			if !ok || !gt.Equal(e, gt.GoError) || !viewsCompatible(a, rawR[0]) {
				return issue("extern `fn %s`: `%s.%s` returns (%s, error) — declare `Result T GoError`, not %s",
					sym.Name, sym.Package, sym.Name, t0, ret)
			}
			shape = core.GoResultsErr
		case isBoolType(t1):
			if payload, ok := optionArgs(ret); ok {
				if !viewsCompatible(payload, rawR[0]) {
					return issue("extern `fn %s` declares Option payload %s, but `%s.%s` returns %s",
						sym.Name, payload, sym.Package, sym.Name, t0)
				}
				shape = core.GoResultsOK
			} else if els, ok := tupleArgs(ret); ok && len(els) == 2 &&
				viewsCompatible(els[0], rawR[0]) && viewsCompatible(els[1], rawR[1]) {
				shape = core.GoResultsPair
			} else {
				return issue("extern `fn %s`: `%s.%s` returns (%s, bool) — declare `Option T` or `(T, Bool)`, not %s",
					sym.Name, sym.Package, sym.Name, t0, ret)
			}
		default:
			if els, ok := tupleArgs(ret); ok && len(els) == 2 &&
				viewsCompatible(els[0], rawR[0]) && viewsCompatible(els[1], rawR[1]) {
				shape = core.GoResultsPair
			} else {
				return issue("extern `fn %s`: `%s.%s` returns (%s, %s) — declare a 2-tuple, not %s",
					sym.Name, sym.Package, sym.Name, t0, t1, ret)
			}
		}
	default:
		return issue("extern `fn %s`: `%s.%s` has %d results — only 0, 1 or 2 are representable",
			sym.Name, sym.Package, sym.Name, res.Len())
	}

	declType := decl.Type
	if zeroArg {
		// The bound signature drops the Unit parameter outright —
		// the same shape mapSignature gives a derived `func() T`
		// member, so `f ()` calls check and lower identically.
		declType = gt.Fn(fn.Ret)
	}
	ck := check.Signature{Type: declType, Variadic: sig.Variadic()}
	l.syms[sym] = &member{
		obj: m.obj,
		sig: sig,
		ck:  ck,
		gs: lower.GoSig{
			Arity:    len(rawP),
			Variadic: sig.Variadic(),
			Results:  shape,
			Params:   rawP,
			ResTypes: rawR,
		},
	}
	return ck, nil
}

// optionArgs unpacks `Option a` by name and arity. The name match
// cannot tell the builtin Option from a file-local `type Option` —
// both carry Qual "": the checker reports the shadowing as E0415 at
// the entry (binding proceeds), so a local Option read here is
// recovery under a diagnostic already raised, not a silent misbind.
func optionArgs(t gt.Type) (gt.Type, bool) {
	c, ok := gt.Chase(t).(*gt.Con)
	if !ok || c.Qual != "" || c.Name != "Option" || len(c.Args) != 1 {
		return nil, false
	}
	return c.Args[0], true
}

// resultArgs unpacks `Result a e` like optionArgs does for Option.
func resultArgs(t gt.Type) (a, e gt.Type, ok bool) {
	c, is := gt.Chase(t).(*gt.Con)
	if !is || c.Qual != "" || c.Name != "Result" || len(c.Args) != 2 {
		return nil, nil, false
	}
	return c.Args[0], c.Args[1], true
}

// tupleArgs unpacks a declared tuple type.
func tupleArgs(t gt.Type) ([]gt.Type, bool) {
	tup, ok := gt.Chase(t).(*gt.Tuple)
	if !ok {
		return nil, false
	}
	return tup.Elems, true
}

// isBoolType reports whether a Go result type is `bool`.
func isBoolType(t types.Type) bool {
	b, ok := t.(*types.Basic)
	return ok && b.Kind() == types.Bool
}

// groundType reports whether t is fully concrete — no type
// variables and no unsolved metas. Positions failing it skip the
// view-compat check: the rigid-variable and unsolved-meta
// diagnostics belong to the checker's own machinery.
func groundType(t gt.Type) bool {
	switch t := gt.Chase(t).(type) {
	case *gt.Con:
		for _, a := range t.Args {
			if !groundType(a) {
				return false
			}
		}
		return true
	case *gt.Tuple:
		for _, e := range t.Elems {
			if !groundType(e) {
				return false
			}
		}
		return true
	case *gt.Func:
		for _, p := range t.Params {
			if !groundType(p.Type) {
				return false
			}
		}
		return groundType(t.Ret)
	case *gt.Go:
		return true
	case *gt.Var, *gt.Meta:
		return false
	default:
		panic("unreachable")
	}
}
