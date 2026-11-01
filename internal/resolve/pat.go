package resolve

import (
	"maps"

	"github.com/podhmo/gom/internal/syntax"
)

// bindPat binds every variable a pattern introduces into sc and
// resolves the constructor references inside it. Variable binders are
// recorded in Defs; two variables of the same name bound along one
// alternative path of a pattern (as in `(x, x)` or `A x x`) are a
// duplicate definition. Binding continues after errors so the tables
// stay complete.
//
// Pattern binding is case-unambiguous: the grammar only produces
// VarPat for lowercase identifiers and CtorPat heads are always
// UIdent, so `x` in pattern position is always a binder and `Some` is
// always a constructor reference — there is no var-vs-ctor
// disambiguation to defer to check.
func (r *resolver) bindPat(p syntax.Pat, sc *scope) {
	r.bindPatSeen(p, sc, make(map[string]*syntax.Ident), nil)
}

// bindPatSeen is the working form of bindPat.
//
//   - seen maps each name bound along the current alternative path to
//     its first binder, for duplicate detection. Or-pattern
//     alternatives clone it, so bindings in different alternatives
//     never collide; names bound by every alternative are merged back
//     afterwards (a following same-name binder then collides under
//     every reading — the conservative choice).
//   - merge is the or-pattern sharing table: nil outside or-patterns.
//     Inside an or-pattern the first binder of each name allocates the
//     symbol and later same-named binders — in any alternative, at any
//     nesting depth — adopt it, so the arm body sees one variable per
//     name (the equal-set/equal-type rule itself is E0304, check's
//     job). Nested or-patterns reuse the outer merge table.
func (r *resolver) bindPatSeen(p syntax.Pat, sc *scope, seen map[string]*syntax.Ident, merge map[string]*Symbol) {
	switch n := p.(type) {
	case nil:
	case *syntax.WildPat, *syntax.LitPat:
	case *syntax.VarPat:
		r.bindVar(n.Name, n, sc, seen, merge)
	case *syntax.CtorPat:
		r.ctorRef(n.Ctor, sc)
		for i := range n.Args {
			a := &n.Args[i]
			if a.Label != nil {
				r.fieldRef(n.Ctor, a.Label, sc)
			}
			r.bindPatSeen(a.Pat, sc, seen, merge)
		}
		for i := range n.Fields {
			f := &n.Fields[i]
			if f.Punned() {
				// `{ name }` both names a field and binds a variable;
				// the ident records the binder (Defs) and check
				// recovers the field through the constructor's type.
				r.bindVar(f.Name, f, sc, seen, merge)
			} else {
				r.fieldRef(n.Ctor, f.Name, sc)
				r.bindPatSeen(f.Pat, sc, seen, merge)
			}
		}
	case *syntax.TuplePat:
		for _, e := range n.Elems {
			r.bindPatSeen(e, sc, seen, merge)
		}
	case *syntax.RecordPat:
		for i := range n.Fields {
			f := &n.Fields[i]
			if f.Punned() {
				r.bindVar(f.Name, f, sc, seen, merge)
			} else {
				// no constructor head: which record type `name`
				// belongs to is type-directed — left for check.
				r.bindPatSeen(f.Pat, sc, seen, merge)
			}
		}
	case *syntax.ListPat:
		for _, e := range n.Elems {
			r.bindPatSeen(e, sc, seen, merge)
		}
		if n.Rest != nil {
			r.bindPatSeen(n.Rest, sc, seen, merge)
		}
	case *syntax.OrPat:
		if merge == nil {
			merge = make(map[string]*Symbol)
		}
		altSeens := make([]map[string]*syntax.Ident, 0, len(n.Alts))
		for _, alt := range n.Alts {
			altSeen := maps.Clone(seen)
			r.bindPatSeen(alt, sc, altSeen, merge)
			altSeens = append(altSeens, altSeen)
		}
		intersectSeen(seen, altSeens)
	case *syntax.AsPat:
		r.bindPatSeen(n.Pat, sc, seen, merge)
		if n.Name != nil {
			r.bindVar(n.Name, n, sc, seen, merge)
		}
	default:
		panic("unreachable")
	}
}

// bindVar binds one pattern variable ident to a fresh — or an
// or-pattern-shared — Var symbol. def is the node recorded as the
// symbol's definition: the VarPat itself, or the enclosing FieldPat
// / AsPat for punned fields and `as` names.
//
// The checks run in order: a second binding on one alternative path
// is a duplicate (seen); an earlier alternative's same-named binder
// shares its symbol (merge); and a name already bound in THIS scope
// level by an earlier sibling pattern — a previous parameter or a
// previous top-level let — is a duplicate (the scope slot itself).
func (r *resolver) bindVar(id *syntax.Ident, def syntax.Node, sc *scope, seen map[string]*syntax.Ident, merge map[string]*Symbol) {
	if id == nil {
		return
	}
	if prev, dup := seen[id.Name]; dup {
		// A second binding of the same name on one path: report it,
		// and still record a Defs entry so the ident is resolved.
		sym := merge[id.Name]
		if sym == nil {
			sym = &Symbol{Name: id.Name, Kind: KindVar, Def: def}
		}
		r.defs[id] = sym
		r.dupDefName(id.Name, id, prev.Sp)
		return
	}
	seen[id.Name] = id
	if sym := merge[id.Name]; sym != nil {
		// or-pattern shared binder — the scope already maps the name
		// to the shared symbol from the first alternative.
		r.defs[id] = sym
		return
	}
	if prev := sc.lookupThisTerm(id.Name); prev != nil {
		r.defs[id] = &Symbol{Name: id.Name, Kind: KindVar, Def: def}
		r.dupDefName(id.Name, id, prev.DefSpan())
		return
	}
	sym := &Symbol{Name: id.Name, Kind: KindVar, Def: def}
	if merge != nil {
		merge[id.Name] = sym
	}
	sc.defineTerm(id.Name, sym)
	r.defs[id] = sym
}

// intersectSeen merges into dst the names bound along EVERY
// alternative: each altSeen is the seen map after binding one
// or-pattern alternative, so the intersection is the set of names a
// following binder would definitely collide with.
func intersectSeen(dst map[string]*syntax.Ident, altSeens []map[string]*syntax.Ident) {
	if len(altSeens) == 0 {
		return
	}
	for name, id := range altSeens[0] {
		all := true
		for _, s := range altSeens[1:] {
			if _, ok := s[name]; !ok {
				all = false
				break
			}
		}
		if all {
			dst[name] = id
		}
	}
}

// ctorRef resolves a constructor-pattern head: a bare `Some` or a
// qualified `pkg.Some` / `Type.Some`. It records Uses for the
// resolved ident(s) and reports constructor-specific diagnostics.
// (Not a type switch — a constructor head that is neither an Ident
// nor a Select is handled generically rather than being unreachable.)
func (r *resolver) ctorRef(e syntax.Expr, sc *scope) {
	if n, ok := e.(*syntax.Ident); ok {
		sym := sc.lookupTerm(n.Name)
		if sym == nil {
			r.undefinedCtor(n)
			return
		}
		r.use(n, sym)
		if sym.Kind != KindCtor {
			r.notCtor(n, sym)
			return
		}
		return
	}
	if n, ok := e.(*syntax.Select); ok {
		// qualified constructor `q.C`: q names a module or a type.
		// Uses[recv] is recorded only once the winning reading is
		// known — a non-module term that falls back to a same-named
		// type must point at the type so qualifier and selector agree.
		recv, ok := n.Recv.(*syntax.Ident)
		if !ok || n.Sel == nil {
			r.expr(e, sc)
			return
		}
		if sym := sc.lookupTerm(recv.Name); sym != nil {
			if sym.Kind == KindModule {
				r.use(recv, sym)
				r.selectMemberCtor(sym, n.Sel, sc)
				return
			}
			if tsym := sc.lookupType(recv.Name); tsym != nil && tsym.Kind == KindType {
				// the term winner is not a namespace, but a type of
				// the same name is — `T.C` reads as type-qualified
				r.use(recv, tsym)
				r.selectTypeCtor(tsym, n.Sel)
				return
			}
			// q is bound but names neither a package nor a type —
			// the reference resolves to the term found
			r.use(recv, sym)
			r.notModule(recv, sym)
			return
		}
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
		return
	}
	// a constructor head that is not a name or selector — resolve
	// its contents so nothing is missed; check diagnoses the shape
	// itself
	r.expr(e, sc)
}

// selectMemberCtor resolves `mod.C` in constructor position: the
// member must exist and be a constructor. As in expression position,
// a member miss falls back to the type of the same name
// (`Option.Some`).
func (r *resolver) selectMemberCtor(mod *Symbol, sel *syntax.Ident, sc *scope) {
	table, builtin := r.memberTable(mod)
	if table == nil && !builtin {
		return // package failed to load — diagnosed at the import
	}
	if member, ok := table[sel.Name]; ok {
		// a resolved member gets a Uses entry even when it is the
		// wrong kind — like `notModule`, the kind mismatch is a
		// separate diagnostic on top of the resolution
		r.use(sel, member)
		if member.Kind != KindCtor {
			r.notCtor(sel, member)
			return
		}
		return
	}
	// member miss — a module and a type may share the name:
	// `Option.Some` in pattern position reaches the Option type.
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

// fieldRef resolves a `{ name = pat }` field name or a `~label:`
// argument of a constructor pattern against the resolved
// constructor's named payload slots. It only resolves fields of
// file-local constructors — for imported and builtin constructors the
// field ident is left for check (the constructor's variant decl is
// not available here).
func (r *resolver) fieldRef(ctor syntax.Expr, id *syntax.Ident, sc *scope) {
	if id == nil {
		return
	}
	cs := ctorSymOf(ctor, r.uses)
	if cs == nil {
		return
	}
	fields, ok := r.ctorFields[cs]
	if !ok {
		return // foreign constructor — check resolves field names by type
	}
	if sym, ok := fields[id.Name]; ok {
		r.use(id, sym)
		return
	}
	r.noSuchField(cs, id)
}

// ctorSymOf returns the constructor symbol a CtorPat head resolved
// to, or nil — for Ident heads it is Uses[head], for `q.C` it is
// Uses[sel]. (Not a type switch — other head shapes are possible.)
func ctorSymOf(ctor syntax.Expr, uses map[*syntax.Ident]*Symbol) *Symbol {
	if n, ok := ctor.(*syntax.Ident); ok {
		return uses[n]
	}
	if n, ok := ctor.(*syntax.Select); ok && n.Sel != nil {
		return uses[n.Sel]
	}
	return nil
}
