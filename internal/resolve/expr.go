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
		if inner, isSel := n.Recv.(*syntax.Select); isSel {
			// `pkg.Type.Ctor` — the inner select qualifies a
			// further member, so its type member is a namespace
			// hop, not a value: keep the notValue diagnostic for
			// genuine term positions only.
			r.inQual++
			r.expr(n.Recv, sc)
			r.inQual--
			// When the inner selector resolved to a type member
			// (`pkg.Type`), the outer selector names a constructor
			// of that type — declared types answer from the unit
			// table, imported types through the loader's per-type
			// table (Config.Ctors).
			if inner.Sel != nil {
				if tsym := r.uses[inner.Sel]; tsym != nil && tsym.Kind == KindType {
					r.selectTypeCtor(tsym, n.Sel)
				}
			}
			return
		}
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
// true for a builtin-scope module with no injected table — either an
// unimplemented stdlib module (Ref/Array/Chan/Task) or a stdlib
// module this compile was not wired for (undefinedMember picks the
// note). A nil table with builtin=false marks a package that failed
// to load: member lookups are left unresolved without further
// diagnostics, since the import site already reported E0203.
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
		// `pkg.member` emits `pkg.<ExportName>` — when the
		// member's own package binds that name to a *different*
		// member the reference would silently name it instead
		// (the `.gomi` loader marks the clash; E0607).
		if member.EmitClash != "" {
			r.emitClash(mod, member, sel)
		}
		// A type member (`Go.Slice`, `pkg.SomeRecord`) is a
		// namespace, not a value — like a bare module name it can
		// never be an expression. In qualifier position
		// (`pkg.Type.Ctor`) it is a namespace hop instead, so the
		// diagnostic is for genuine term positions only.
		// KindBuiltin members (`Go.conv`) are exempt either way:
		// whether the selection is a misuse depends on call-site
		// context the checker sees, not the member kind.
		if member.Kind == KindType && r.inQual == 0 {
			r.notValue(sel, member)
		}
		return
	}
	if tsym := sc.lookupType(mod.Name); tsym != nil && tsym.Kind == KindType {
		if cs := r.ctorTable(tsym)[sel.Name]; cs != nil {
			r.use(sel, cs)
			return
		}
	}
	r.undefinedMember(mod, sel, builtin)
}

// selectTypeCtor resolves `T.sel` (or the final selector of
// `pkg.T.sel`) as a constructor of type tsym.
func (r *resolver) selectTypeCtor(tsym *Symbol, sel *syntax.Ident) {
	if cs := r.ctorTable(tsym)[sel.Name]; cs != nil {
		r.use(sel, cs)
		return
	}
	r.noSuchCtor(tsym, sel)
}

// ctorTable returns the constructor name -> symbol table of a type:
// the unit's declared table first, then — once per type symbol —
// the loader-supplied table of Config.Ctors for types the unit does
// not declare (an imported package's `pkg.Type`). A miss caches an
// empty table so the hook is not re-consulted; a type with no
// constructors anywhere answers an empty table.
func (r *resolver) ctorTable(tsym *Symbol) map[string]*Symbol {
	if ct, ok := r.ctorsOf[tsym]; ok {
		return ct
	}
	ct := map[string]*Symbol{}
	if r.cfg.Ctors != nil {
		if loaded := r.cfg.Ctors(tsym); loaded != nil {
			ct = loaded
		}
	}
	r.ctorsOf[tsym] = ct
	return ct
}
