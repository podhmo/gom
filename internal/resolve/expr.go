package resolve

import (
	"github.com/podhmo/gom/internal/syntax"
)

// expr resolves every identifier occurrence inside e under scope sc.
// The tv parameter is the implicit type-variable scope of the
// enclosing declaration's signature — ascriptions inside the body can
// refer to the signature's type variables and may introduce new
// implicit ones (docs/decisions/2026-11-02-name-resolution.md).
func (r *resolver) expr(e syntax.Expr, sc *scope) {
	switch n := e.(type) {
	case nil:
	case *syntax.Ident:
		r.useTerm(n, sc)
	case *syntax.Lit:
	case *syntax.StrInterp:
		for _, p := range n.Parts {
			switch part := p.(type) {
			case *syntax.StrText:
			case *syntax.StrExpr:
				r.expr(part.X, sc)
			default:
				panic("unreachable")
			}
		}
	case *syntax.Apply:
		r.expr(n.Fn, sc)
		for i := range n.Args {
			a := &n.Args[i]
			// a.Label is the call-site label, not a symbol
			// occurrence: resolving it against the callee's parameter
			// list needs the callee's type — check's job.
			r.expr(a.Value, sc)
		}
	case *syntax.Select:
		r.selectExpr(n, sc)
	case *syntax.Unary:
		r.expr(n.X, sc)
	case *syntax.Binary:
		r.expr(n.X, sc)
		r.expr(n.Y, sc)
	case *syntax.Hole:
	case *syntax.Lambda:
		// parameter annotations share one implicit type-variable
		// scope, like a signature's
		ltv := r.annScope()
		for i := range n.Params {
			if n.Params[i].Type != nil {
				r.typeExpr(n.Params[i].Type, sc, ltv)
			}
		}
		fn := pushScope(sc)
		r.bindParams(n.Params, fn)
		r.expr(n.Body, fn)
	case *syntax.If:
		r.expr(n.Cond, sc)
		r.expr(n.Then, sc)
		if n.Else != nil {
			r.expr(n.Else, sc)
		}
	case *syntax.Match:
		r.expr(n.Subj, sc)
		for i := range n.Arms {
			arm := &n.Arms[i]
			as := pushScope(sc)
			r.bindPat(arm.Pat, as)
			if arm.Guard != nil {
				r.expr(arm.Guard, as)
			}
			r.expr(arm.Body, as)
		}
	case *syntax.Block:
		r.block(n, sc)
	case *syntax.Let:
		// let bound directly in an expression position is unusual —
		// the grammar only produces it as a block item — but bind it
		// the same way so its binders still get Defs entries. The
		// value sees the annotation's type variables.
		ltv := r.annScope()
		if n.Type != nil {
			r.typeExpr(n.Type, sc, ltv)
		}
		if n.Value != nil {
			r.withTV(ltv, func() { r.expr(n.Value, sc) })
		}
		r.bindPat(n.Pat, pushScope(sc))
	case *syntax.Ignore:
		r.expr(n.X, sc)
	case *syntax.Try:
		r.expr(n.X, sc)
	case *syntax.GoExpr:
		r.expr(n.Body, sc)
	case *syntax.For:
		r.expr(n.Iter, sc)
		fs := pushScope(sc)
		r.bindPat(n.Pat, fs)
		r.expr(n.Body, fs)
	case *syntax.SelectExpr:
		for _, arm := range n.Arms {
			switch a := arm.(type) {
			case *syntax.RecvArm:
				// the channel expression is evaluated when the select
				// is entered, before the arm binds (spec 10.6)
				r.expr(a.Chan, sc)
				as := pushScope(sc)
				r.bindPat(a.Bind, as)
				r.expr(a.Body, as)
			case *syntax.SendArm:
				r.expr(a.Chan, sc)
				r.expr(a.Value, sc)
				r.expr(a.Body, sc)
			case *syntax.DefaultArm:
				r.expr(a.Body, sc)
			default:
				panic("unreachable")
			}
		}
	case *syntax.ListLit:
		for _, e := range n.Elems {
			r.expr(e, sc)
		}
	case *syntax.Spread:
		r.expr(n.X, sc)
	case *syntax.TupleLit:
		for _, e := range n.Elems {
			r.expr(e, sc)
		}
	case *syntax.Ascribe:
		r.expr(n.X, sc)
		if n.Type != nil {
			r.typeExpr(n.Type, sc, r.annScope())
		}
	case *syntax.RecordLit:
		if n.Base != nil {
			r.expr(n.Base, sc)
		}
		for i := range n.Fields {
			// FieldInit.Name is a record-field reference that only the
			// expected record type can resolve — left for check, like
			// a member selector on a non-module receiver.
			r.expr(n.Fields[i].Value, sc)
		}
	default:
		panic("unreachable")
	}
}

// block resolves a `{ ... }` block: one fresh scope for the block,
// then sequential binding — each `let` resolves its right-hand side
// and annotation in the current scope, then pushes a new scope
// carrying its pattern binders that covers exactly the following
// statements (spec 1.1.4: "以降のスコープ").
func (r *resolver) block(b *syntax.Block, sc *scope) {
	cur := pushScope(sc)
	for _, stmt := range b.Stmts {
		if let, ok := stmt.(*syntax.Let); ok {
			// annotation and initializer share one implicit
			// type-variable scope, like a signature's
			ltv := r.annScope()
			if let.Type != nil {
				r.typeExpr(let.Type, cur, ltv)
			}
			if let.Value != nil {
				r.withTV(ltv, func() { r.expr(let.Value, cur) })
			}
			next := pushScope(cur)
			r.bindPat(let.Pat, next)
			cur = next
			continue
		}
		r.expr(stmt, cur)
	}
}

// selectExpr resolves `recv.sel`. Only one shape is decided at this
// phase — `pkg.member` where recv is a bare identifier that resolves
// to a module — plus the type-qualified constructor form `T.Ctor` as
// a fallback when the identifier resolves to a type. Every other
// shape (field access, method selection, qualified names deeper than
// one level) is type-directed and is left for check: Sel then has no
// Uses entry.
func (r *resolver) selectExpr(n *syntax.Select, sc *scope) {
	if n.Sel == nil {
		r.expr(n.Recv, sc)
		return
	}
	recv, ok := n.Recv.(*syntax.Ident)
	if !ok {
		r.expr(n.Recv, sc)
		return
	}
	sym := sc.lookupTerm(recv.Name)
	if sym != nil {
		r.use(recv, sym)
		if sym.Kind != KindModule {
			// `value.sel` — field or method, decided by recv's type.
			return
		}
		r.selectMember(sym, n.Sel, sc)
		return
	}
	// The identifier did not resolve as a term; the `T.Ctor`
	// type-qualified constructor form resolves it as a type.
	if tsym := sc.lookupType(recv.Name); tsym != nil {
		r.use(recv, tsym)
		if tsym.Kind != KindType {
			r.notModule(recv, tsym)
			return
		}
		r.selectTypeCtor(tsym, n.Sel)
		return
	}
	r.undefinedQual(recv)
}

// memberTable returns the member table of a module symbol. builtin is
// true for a builtin-scope module with no injected table (stdlib
// modules whose members are not implemented yet) — member misses on
// those are diagnosed with the stdlib note. A nil table with
// builtin=false marks a package that failed to load: member lookups
// are left unresolved without further diagnostics, since the import
// site already reported E0203.
func (r *resolver) memberTable(mod *Symbol) (table map[string]*Symbol, builtin bool) {
	if table, known := r.members[mod]; known {
		return table, false
	}
	if t, ok := builtinModuleTable(mod, &r.cfg); ok {
		return t, false
	}
	return nil, true
}

// selectMember resolves `mod.sel` against a module's member table.
// When the table has no such member, `T.sel` is retried as a
// type-qualified constructor — a module and a type can share a name
// (`Option.Some` reaches the builtin Option type's ctors even though
// the builtin Option module shadows it in term position).
func (r *resolver) selectMember(mod *Symbol, sel *syntax.Ident, sc *scope) {
	table, builtin := r.memberTable(mod)
	if table == nil && !builtin {
		return // package failed to load — diagnosed at the import
	}
	if member, ok := table[sel.Name]; ok {
		r.use(sel, member)
		return
	}
	if tsym := sc.lookupType(mod.Name); tsym != nil && tsym.Kind == KindType {
		if ct, ok := r.ctorsOf[tsym]; ok {
			if cs, ok := ct[sel.Name]; ok {
				r.use(sel, cs)
				return
			}
		}
	}
	r.undefinedMember(mod, sel, builtin)
}

// selectTypeCtor resolves `T.sel` as a constructor of type tsym.
func (r *resolver) selectTypeCtor(tsym *Symbol, sel *syntax.Ident) {
	if table, ok := r.ctorsOf[tsym]; ok {
		if cs, ok := table[sel.Name]; ok {
			r.use(sel, cs)
			return
		}
	}
	r.noSuchCtor(tsym, sel)
}
