package lower

import (
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// Function parameters declared with an arrow chain of two or more
// take the flat Go representation `func(T1', ..., Tk') R'` instead
// of the canonical one-Go-func-per-arrow value. The convention
// (docs/decisions/2026-11-18-flat-fn-params.md) exists so a declared
// higher-order function — `List.Fold`'s `b -> a -> b`, a `@extern`
// handler — applies its callback without per-element closure
// allocation. It is positional: the parameter slot is flat, while
// every *value* of the same gom type stays canonical. Lowering keeps
// the two representations apart:
//
//   - l.flat records the declared flat parameters of the enclosing
//     FuncDecl (bound symbol → arity k)
//   - a flat parameter applied to arguments fuses into one CallFunc
//     carrying FlatArity — `f(acc, x)` instead of `f(acc)(x)`
//   - a flat parameter in value position is wrapped back into the
//     canonical chain `func(p0) func(p1) { f(p0, p1) }`
//   - an argument feeding a flat parameter slot adapts through
//     core.Uncurry: a lambda literal flattens directly, a declared
//     function reference emits its flat `func f_(a, b)` face, and any
//     other canonical value wraps as `func(p0, ...) { v(p0)... }`

// flatParamArity reports a declared parameter's flat arity — the
// count of its whole arrow chain when it is a function of two or
// more parameters. 0 for non-functions and arity-1 arrows (those
// keep the canonical repr in parameter position too).
func flatParamArity(t types.Type) int {
	fs := flatSig(t)
	if fs == nil || len(fs.Params) < 2 {
		return 0
	}
	return len(fs.Params)
}

// flatAtomArity reports the flat arity of an atom — >0 only for a
// Var naming a bound flat-repr symbol.
func (l *lowerer) flatAtomArity(a core.Atom) int {
	if v, ok := a.(*core.Var); ok && v.Sym != nil {
		return l.flat[v.Sym]
	}
	return 0
}

// flatCalleeAtom keeps the flat Var when an application callee is a
// declared flat parameter — verbatim so the call fuses into one
// `f(a, b, ...)` — and reports nil for every other callee, letting
// the caller take the canonical path.
func (l *lowerer) flatCalleeAtom(fn check.Expr) core.Atom {
	v, ok := fn.(*check.Var)
	if !ok || v.Sym == nil {
		return nil
	}
	sym := l.substSym(v.Sym)
	if l.flat[sym] == 0 {
		return nil
	}
	return &core.Var{Sp: v.Span(), Typ: v.Type(), Sym: sym}
}

// curryEta wraps a flat-repr function value in the canonical
// one-Go-func-per-arrow representation — the eta-expansion
// `func(p0) func(p1) ... { f(p0, p1, ...) }`. It is how a declared
// flat parameter stands where a *value* of its type is needed (a
// `let` binding, a payload slot, a canonical parameter).
func (l *lowerer) curryEta(sp diag.Span, typ types.Type, fn core.Atom, k int) core.Expr {
	fs := flatSig(typ)
	if fs == nil || len(fs.Params) != k {
		// Type and marker disagree — fall back to the atom itself
		// rather than guessing at a chain shape.
		return fn
	}
	return l.closureChain(sp, typ, fs.Params, func(bs *seq, pv []core.Atom) core.Expr {
		return &core.CallFunc{Sp: sp, Typ: fs.Ret, Fn: fn, Args: pv, FlatArity: k}
	})
}

// flatArg lowers a call argument bound for a ≥2-arrow parameter slot
// — the flat representation the callee's parameter carries.
// The adaptation keeps the direct shapes verbatim where the source
// already is or directly yields the flat literal; every other
// canonical value wraps through core.Uncurry.
func (l *lowerer) flatArg(s *seq, a check.Arg, arity int) core.Atom {
	v := a.Value
	switch v := v.(type) {
	case *check.Var:
		sym := l.substSym(v.Sym)
		if k := l.flat[sym]; k > 0 {
			// A declared flat parameter passes verbatim — already
			// in the target repr.
			return &core.Var{Sp: v.Span(), Typ: v.Type(), Sym: sym}
		}
		if sym != nil && sym.Kind == resolve.KindFunc && !sym.IsGo {
			// `fold add` — the declaration is already flat at the
			// matching arity: the reference emits `add_` verbatim.
			decl := l.funcArity(sym, v.Type())
			return l.asAtom(s, v.Span(), &core.Uncurry{
				Base:  core.Base{Sp: v.Span(), Typ: v.Type()},
				Arity: arity,
				X: &core.FuncRef{Base: core.Base{Sp: v.Span(), Typ: v.Type()},
					Sym: sym, Arity: decl, TypeArgs: v.TypeArgs,
					FlatParams: declFlatArities(l.declParamTypes(sym, v.Type()), decl)},
			})
		}
	case *check.Lambda:
		// `fold (fn acc x -> e)` — the lambda flattens into a
		// single `func(acc, x) { e }` literal, no adapters.
		return l.asAtom(s, v.Span(), &core.Uncurry{
			Base:  core.Base{Sp: v.Span(), Typ: v.Type()},
			Arity: arity,
			X:     l.lambdaExpr(v),
		})
	case *check.Apply, *check.Ascribe, *check.Binary, *check.Block,
		*check.FieldAssign, *check.For, *check.GoExpr, *check.Hole,
		*check.If, *check.Ignore, *check.Let, *check.ListLit,
		*check.Lit, *check.Match, *check.MemberExpr, *check.RecordLit,
		*check.SelectExpr, *check.Spread, *check.StrInterp, *check.Try,
		*check.TupleLit, *check.Unary:
		// Every other argument lowers through the generic atom path
		// below — spelled out so the sumtype check verifies the
		// split (Var and Lambda above return early).
	}
	atom := l.atom(s, v)
	if l.flatAtomArity(atom) > 0 {
		return atom
	}
	return l.asAtom(s, v.Span(), &core.Uncurry{
		Base:  core.Base{Sp: v.Span(), Typ: v.Type()},
		Arity: arity,
		X:     atom,
	})
}

// ensureFlatAtom guarantees an atom bound for a declared ≥2-arrow
// parameter slot carries the flat repr: flat-bound Vars pass
// verbatim; every other atom adapts through core.Uncurry. Used for
// argument positions the written-argument path could not adapt —
// eta-expansion parameters of a partial application and atoms that
// arrive through pipes.
func (l *lowerer) ensureFlatAtom(s *seq, sp diag.Span, a core.Atom, arity int) core.Atom {
	if a == nil || l.flatAtomArity(a) > 0 {
		return a
	}
	return l.asAtom(s, sp, &core.Uncurry{
		Base:  core.Base{Sp: sp, Typ: a.Type()},
		Arity: arity,
		X:     a,
	})
}

// flatifyCallArgs adapts the atoms bound for a declared callee's
// ≥2-arrow parameter slots — reorder-domain (parameter) indexes, so
// positions beyond len(sig.Params) and nil holes pass through.
func (l *lowerer) flatifyCallArgs(s *seq, sp diag.Span, params []types.Type, args []core.Atom, n int) []core.Atom {
	for i := 0; i < n && i < len(args) && i < len(params); i++ {
		if k := flatParamArity(params[i]); k >= 2 {
			args[i] = l.ensureFlatAtom(s, sp, args[i], k)
		}
	}
	return args
}

// declFlatArities maps a declared callee's parameter types to the
// per-parameter flat arity core.FuncRef carries — the declared
// flat-slot convention codegen reads instead of re-deriving it
// from the instantiated reference type. The result is exactly decl
// entries; a slot the declared signature does not cover stays 0
// (canonical).
func declFlatArities(dps []types.Type, decl int) []int {
	out := make([]int, decl)
	for i := 0; i < decl && i < len(dps); i++ {
		out[i] = flatParamArity(dps[i])
	}
	return out
}

// funcParamTypes unwraps a *types.Func's parameter types — the
// GoSig.Params slice carries bare types.Type, a flattened arrow's
// Params carry FuncParam.
func funcParamTypes(fs *types.Func) []types.Type {
	if fs == nil {
		return nil
	}
	out := make([]types.Type, len(fs.Params))
	for i, p := range fs.Params {
		out[i] = p.Type
	}
	return out
}
