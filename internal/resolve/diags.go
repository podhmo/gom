package resolve

import (
	"fmt"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
)

// Diagnostics emitted by the resolver. All codes come from the
// registered table in internal/diag (category 02, name resolution):
//
//	E0201 undefined name          — bare names, members, constructors,
//	                              type names, and wrong-kind uses that
//	                              have no legitimate reading
//	E0202 duplicate definition    — same scope, same name
//	E0203 cannot resolve import   — the Loader failed or is absent
//	E0802 @test referenced from non-test code
//
// The "namespace" data key classifies what the name was expected to
// be: "value", "type", "typevar", "module", "member", "constructor",
// or "field".

// undefinedTerm reports a term-level name with no binding in scope.
func (r *resolver) undefinedTerm(id *syntax.Ident) {
	d := diag.Error("E0201", fmt.Sprintf("undefined name `%s`", id.Name)).
		At(id.Sp).
		Set("name", id.Name).
		Set("namespace", "value")
	r.diags.Add(d)
}

// undefinedType reports a type-level name with no binding in scope.
func (r *resolver) undefinedType(id *syntax.Ident) {
	r.diags.Add(diag.Error("E0201", fmt.Sprintf("undefined type `%s`", id.Name)).
		At(id.Sp).
		Set("name", id.Name).
		Set("namespace", "type"))
}

// undefinedTypeVar reports a type variable that is not bound by the
// enclosing signature or declaration — used where implicit binding is
// not allowed (type declaration bodies).
func (r *resolver) undefinedTypeVar(id *syntax.Ident) {
	r.diags.Add(diag.Error("E0201", fmt.Sprintf("undefined type variable `%s`", id.Name)).
		At(id.Sp).
		Set("name", id.Name).
		Set("namespace", "typevar"))
}

// undefinedCtor reports a pattern-position constructor name that does
// not resolve to any constructor.
func (r *resolver) undefinedCtor(id *syntax.Ident) {
	r.diags.Add(diag.Error("E0201", fmt.Sprintf("undefined constructor `%s`", id.Name)).
		At(id.Sp).
		Set("name", id.Name).
		Set("namespace", "constructor"))
}

// undefinedQual reports `q.x` where the qualifier q names nothing —
// it would have to be a package or a type.
func (r *resolver) undefinedQual(id *syntax.Ident) {
	r.diags.Add(diag.Error("E0201", fmt.Sprintf("undefined name `%s`", id.Name)).
		At(id.Sp).
		Set("name", id.Name).
		Set("namespace", "module"))
}

// undefinedMember reports `mod.name` where mod is a loaded module
// that has no such member.
func (r *resolver) undefinedMember(mod *Symbol, id *syntax.Ident, builtin bool) {
	d := diag.Error("E0201", fmt.Sprintf("`%s` has no member `%s`", mod.Name, id.Name)).
		At(id.Sp).
		Set("name", id.Name).
		Set("in", mod.Name).
		Set("namespace", "member")
	if builtin {
		d = d.AddNote(fmt.Sprintf("builtin module `%s` exposes no members yet — "+
			"its standard library is not implemented (stdlib is task P4-8)", mod.Name))
	}
	r.diags.Add(d)
}

// notModule reports a qualifier (`pkg.name` or `pkg.Type`) that
// resolves to something other than a module.
func (r *resolver) notModule(id *syntax.Ident, sym *Symbol) {
	r.diags.Add(diag.Error("E0201", fmt.Sprintf("`%s` is not a package", id.Name)).
		At(id.Sp).
		Set("name", id.Name).
		Set("namespace", "module").
		Set("resolved_kind", sym.Kind.String()))
}

// notCtor reports a constructor-pattern head that resolves to a
// non-constructor symbol.
func (r *resolver) notCtor(id *syntax.Ident, sym *Symbol) {
	r.diags.Add(diag.Error("E0201", fmt.Sprintf("`%s` is not a constructor", id.Name)).
		At(id.Sp).
		Set("name", id.Name).
		Set("namespace", "constructor").
		Set("resolved_kind", sym.Kind.String()))
}

// noSuchCtor reports `T.C` where T is a type that has no constructor
// named C.
func (r *resolver) noSuchCtor(tsym *Symbol, id *syntax.Ident) {
	r.diags.Add(diag.Error("E0201", fmt.Sprintf("`%s` has no constructor `%s`", tsym.Name, id.Name)).
		At(id.Sp).
		Set("name", id.Name).
		Set("in", tsym.Name).
		Set("namespace", "constructor"))
}

// noSuchField reports a `{ name = pat }` field or `~label:` argument
// that the resolved constructor does not declare.
func (r *resolver) noSuchField(ctor *Symbol, id *syntax.Ident) {
	r.diags.Add(diag.Error("E0201", fmt.Sprintf("`%s` has no field or label `%s`", ctor.Name, id.Name)).
		At(id.Sp).
		Set("name", id.Name).
		Set("in", ctor.Name).
		Set("namespace", "field"))
}

// dupDef reports a same-scope duplicate definition at id, where prev
// is the earlier binding's symbol.
func (r *resolver) dupDef(id *syntax.Ident, prev *Symbol) {
	r.dupDefName(id.Name, id, prev.DefSpan())
}

// dupDefName reports a same-scope duplicate definition when the
// offending site is not an *Ident (a Field or a whole pattern).
func (r *resolver) dupDefName(name string, site syntax.Node, firstSp diag.Span) {
	d := diag.Error("E0202", fmt.Sprintf("duplicate definition of `%s`", name)).
		At(site.Span()).
		Set("name", name)
	if firstSp.Valid() {
		d = d.Set("first_defined_at", map[string]any{
			"file":  firstSp.File,
			"start": firstSp.Start,
			"end":   firstSp.End,
		})
	}
	r.diags.Add(d)
}

// testOnlyRef reports a reference to a @test symbol from non-test
// code (spec section 11.3).
func (r *resolver) testOnlyRef(id *syntax.Ident, sym *Symbol) {
	r.diags.Add(diag.Error("E0802", fmt.Sprintf("`%s` is test-only and cannot be referenced from non-test code", id.Name)).
		At(id.Sp).
		Set("name", id.Name))
}
