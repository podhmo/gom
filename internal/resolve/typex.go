package resolve

import (
	"github.com/podhmo/gom/internal/syntax"
)

// typeExpr resolves a type expression under scope sc with type
// variables resolved in tv. See tvScope: a signature's tv scope is
// implicit (a fresh lowercase name binds a KindTypeVar at first use);
// a type declaration's is closed (undeclared variables are E0201).
func (r *resolver) typeExpr(e syntax.TypeExpr, sc *scope, tv *tvScope) {
	r.typeExprCtx(e, sc, tv, nil)
}

// typeExprExtern resolves a type expression inside an `extern go`
// entry: unqualified names mean members of the extern'd package first
// (`extern go "io" { fn Copy (~dst: Writer) }` — Writer is io.Writer),
// falling back to the ordinary type scope (spec section 8.4).
func (r *resolver) typeExprExtern(e syntax.TypeExpr, sc *scope, tv *tvScope, members map[string]*Symbol) {
	r.typeExprCtx(e, sc, tv, members)
}

// typeExprCtx is the working form. extern, when non-nil, is the
// member table of the enclosing `extern go` block consulted before
// the ordinary scope for unqualified type names.
func (r *resolver) typeExprCtx(e syntax.TypeExpr, sc *scope, tv *tvScope, extern map[string]*Symbol) {
	switch n := e.(type) {
	case nil:
	case *syntax.TypeVar:
		r.typeVar(n, tv)
	case *syntax.TypeName:
		r.typeName(n, sc, extern)
	case *syntax.TypeApp:
		r.typeExprCtx(n.Ctor, sc, tv, extern)
		for _, a := range n.Args {
			r.typeExprCtx(a, sc, tv, extern)
		}
	case *syntax.TypeFn:
		for i := range n.Params {
			p := &n.Params[i]
			// p.Label is the parameter's external name, not a symbol
			// occurrence — like a call-site `~label`.
			r.typeExprCtx(p.Type, sc, tv, extern)
		}
		r.typeExprCtx(n.Ret, sc, tv, extern)
	case *syntax.TypeTuple:
		for _, e := range n.Elems {
			r.typeExprCtx(e, sc, tv, extern)
		}
	case *syntax.TypeRecord:
		r.bindFields(n.Fields)
		for i := range n.Fields {
			r.typeExprCtx(n.Fields[i].Type, sc, tv, extern)
		}
	default:
		panic("unreachable")
	}
}

// typeVar resolves a type variable `a`: an existing binding wins,
// otherwise the innermost implicit scope binds it (its first
// occurrence is the binder and goes into Defs), otherwise E0201.
func (r *resolver) typeVar(n *syntax.TypeVar, tv *tvScope) {
	if n.Name == nil {
		return
	}
	if sym := tv.lookupVar(n.Name.Name); sym != nil {
		r.use(n.Name, sym)
		return
	}
	if scope := tv.implicitScope(); scope != nil {
		sym := &Symbol{Name: n.Name.Name, Kind: KindTypeVar, Def: n}
		scope.defineVar(n.Name.Name, sym)
		r.defs[n.Name] = sym
		return
	}
	r.undefinedTypeVar(n.Name)
}

// typeName resolves `T` or `q.T`. Qualified names resolve the
// qualifier as a module and select the member; unqualified names try
// the extern member table (inside `extern go` only) then the type
// scope chain.
func (r *resolver) typeName(n *syntax.TypeName, sc *scope, extern map[string]*Symbol) {
	if n.Name == nil {
		return
	}
	if n.Qual != nil {
		r.qualifiedTypeName(n, sc)
		return
	}
	if extern != nil {
		if member, ok := extern[n.Name.Name]; ok {
			r.use(n.Name, member)
			return
		}
	}
	sym := sc.lookupType(n.Name.Name)
	if sym == nil {
		r.undefinedType(n.Name)
		return
	}
	r.use(n.Name, sym)
}

// qualifiedTypeName resolves `q.T`: q must be a module, T a member of
// it. A member of any kind resolves — `pkg.Type` in type position is
// normally a KindType member, but the resolver does not police member
// kinds: an extern block can put whatever the package exports in the
// table and check judges fitness for use.
func (r *resolver) qualifiedTypeName(n *syntax.TypeName, sc *scope) {
	sym := sc.lookupTerm(n.Qual.Name)
	if sym == nil {
		r.undefinedQual(n.Qual)
		return
	}
	r.use(n.Qual, sym)
	if sym.Kind != KindModule {
		r.notModule(n.Qual, sym)
		return
	}
	table, builtin := r.memberTable(sym)
	if table == nil && !builtin {
		return // package failed to load — diagnosed at the import
	}
	if member, ok := table[n.Name.Name]; ok {
		r.use(n.Name, member)
		return
	}
	r.undefinedMember(sym, n.Name, builtin)
}
