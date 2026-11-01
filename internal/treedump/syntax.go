package treedump

import (
	"sort"

	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
)

// DumpSyntax renders a parsed source file — the output of compiler
// phase 2 — including its comment and blank-line trivia. Trivia is
// merged into the declaration stream by position so the dump reads in
// source order.
func DumpSyntax(f *syntax.File) string {
	d := newDumper()
	return render(d.file(f))
}

// DumpResolved renders a resolved file — the output of phase 3. The
// surface tree is dumped with `name%<n>` identifiers bound through
// Defs/Uses, followed by the `(defs)`, `(uses)` and `(syms)` side
// tables. Table entries are sorted by the key identifier's span, so
// output never depends on map iteration order.
func DumpResolved(r *resolve.Resolved) string {
	d := newDumper()
	d.defs, d.uses = r.Defs, r.Uses
	return render(nodef("resolved",
		d.file(r.File),
		d.table("defs", "def", r.Defs),
		d.table("uses", "use", r.Uses),
		d.syms()))
}

// table renders one side table — `(defs (def@pos -> name%n) ...)` —
// sorted by key span, then by key name for identical spans.
func (d *dumper) table(head, entry string, m map[*syntax.Ident]*resolve.Symbol) *form {
	ids := make([]*syntax.Ident, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.SliceStable(ids, func(i, j int) bool {
		a, b := ids[i].Sp, ids[j].Sp
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		if a.End != b.End {
			return a.End < b.End
		}
		if ids[i].Name != ids[j].Name {
			return ids[i].Name < ids[j].Name
		}
		// Two distinct idents can share (span, name) — e.g. a punned
		// `~x` arg. Break the tie on the symbol's deterministic
		// fields so the row order never depends on map iteration.
		// (If every rendered field ties, the emitted rows are
		// byte-identical and the order cannot matter.)
		sa, sb := m[ids[i]], m[ids[j]]
		if sa == nil || sb == nil {
			return sa == nil && sb != nil
		}
		if sa.DefSpan().Start != sb.DefSpan().Start {
			return sa.DefSpan().Start < sb.DefSpan().Start
		}
		if sa.Name != sb.Name {
			return sa.Name < sb.Name
		}
		if sa.Kind != sb.Kind {
			return sa.Kind < sb.Kind
		}
		return sa.Package < sb.Package
	})
	kids := make([]*form, 0, len(ids))
	for _, id := range ids {
		kids = append(kids, nodef(entry+at(id.Sp)+" -> "+d.symRef(m[id])))
	}
	return nodef(head, kids...)
}

// file renders `(file@span pkg@span items...)` with decls, comments
// and blank lines merged into one position-sorted stream.
func (d *dumper) file(f *syntax.File) *form {
	text := "file" + at(f.Sp)
	if f.Package != nil {
		text += " " + d.nameText(f.Package)
	} else {
		text += " -"
	}
	type item struct {
		start int
		form  *form
	}
	items := make([]item, 0, len(f.Decls)+len(f.Comments)+len(f.Blanks))
	for _, decl := range f.Decls {
		items = append(items, item{decl.Span().Start, d.decl(decl)})
	}
	for _, c := range f.Comments {
		items = append(items, item{c.Sp.Start,
			nodef("comment" + at(c.Sp) + " " + c.Kind.String() + " " + quote(c.Text))})
	}
	for _, b := range f.Blanks {
		items = append(items, item{b.Start, nodef("blank" + at(b))})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].start < items[j].start })
	kids := make([]*form, 0, len(items))
	for _, it := range items {
		kids = append(kids, it.form)
	}
	return nodef(text, kids...)
}

// --- declarations ---

func (d *dumper) decl(dd syntax.Decl) *form {
	switch dd := dd.(type) {
	case *syntax.FnDecl:
		text := "fn" + at(dd.Sp) + " " + d.nameText(dd.Name)
		if dd.TestOnly {
			text += " :test"
		}
		kids := []*form{}
		if dd.Doc != nil {
			kids = append(kids, d.doc(dd.Doc))
		}
		kids = append(kids, d.params(dd.Params))
		if dd.Ret != nil {
			kids = append(kids, nodef("->", d.typeexpr(dd.Ret)))
		}
		if dd.Body != nil {
			kids = append(kids, d.expr(dd.Body))
		}
		return nodef(text, kids...)
	case *syntax.LetDecl:
		text := "let" + at(dd.Sp)
		if dd.TestOnly {
			text += " :test"
		}
		kids := []*form{}
		if dd.Doc != nil {
			kids = append(kids, d.doc(dd.Doc))
		}
		kids = append(kids, d.pat(dd.Pat))
		if dd.Type != nil {
			kids = append(kids, nodef(":", d.typeexpr(dd.Type)))
		}
		if dd.Value != nil {
			kids = append(kids, d.expr(dd.Value))
		}
		return nodef(text, kids...)
	case *syntax.TypeDecl:
		text := "type" + at(dd.Sp) + " " + d.nameText(dd.Name)
		kids := []*form{}
		if dd.Doc != nil {
			kids = append(kids, d.doc(dd.Doc))
		}
		if len(dd.Params) > 0 {
			ps := make([]*form, 0, len(dd.Params))
			for _, p := range dd.Params {
				ps = append(ps, d.ident(p))
			}
			kids = append(kids, nodef("tparams", ps...))
		}
		kids = append(kids, d.typebody(dd.Body))
		return nodef(text, kids...)
	case *syntax.ImportDecl:
		text := "import" + at(dd.Sp) + " " + quote(dd.Path) + at(dd.PathSp)
		if dd.Name != nil {
			text += " " + d.nameText(dd.Name)
		}
		if dd.Go {
			text += " :go"
		}
		if dd.Alias {
			text += " :alias"
		}
		return nodef(text)
	case *syntax.ExternDecl:
		kids := make([]*form, 0, len(dd.Entries))
		for _, e := range dd.Entries {
			kids = append(kids, d.externEntry(e))
		}
		return nodef("extern"+at(dd.Sp)+" "+quote(dd.Path)+at(dd.PathSp), kids...)
	case *syntax.TestDecl:
		return nodef("test"+at(dd.Sp)+" "+quote(dd.Name)+at(dd.NameSp), d.expr(dd.Body))
	default:
		panic("unreachable")
	}
}

func (d *dumper) doc(c *syntax.Comment) *form {
	return nodef("doc" + at(c.Sp) + " " + quote(c.Text))
}

func (d *dumper) externEntry(e syntax.ExternEntry) *form {
	switch e := e.(type) {
	case *syntax.ExternFn:
		kids := []*form{}
		if e.Doc != nil {
			kids = append(kids, d.doc(e.Doc))
		}
		kids = append(kids, d.params(e.Params))
		if e.Ret != nil {
			kids = append(kids, nodef("->", d.typeexpr(e.Ret)))
		}
		return nodef("externfn"+at(e.Sp)+" "+d.nameText(e.Name), kids...)
	default:
		panic("unreachable")
	}
}

func (d *dumper) typebody(b syntax.TypeBody) *form {
	switch b := b.(type) {
	case *syntax.SumBody:
		kids := make([]*form, 0, len(b.Variants))
		for _, v := range b.Variants {
			vk := []*form{}
			if v.Doc != nil {
				vk = append(vk, d.doc(v.Doc))
			}
			for _, f := range v.Params {
				vk = append(vk, d.field(f))
			}
			kids = append(kids, nodef("variant"+at(v.Sp)+" "+d.nameText(v.Name), vk...))
		}
		return nodef("sum"+at(b.Sp), kids...)
	case *syntax.RecordBody:
		kids := make([]*form, 0, len(b.Fields))
		for _, f := range b.Fields {
			kids = append(kids, d.field(f))
		}
		return nodef("record"+at(b.Sp), kids...)
	case *syntax.AliasBody:
		return nodef("alias"+at(b.Sp), d.typeexpr(b.Type))
	case *syntax.AbstractBody:
		return nodef("abstract" + at(b.Sp))
	default:
		panic("unreachable")
	}
}

// field renders a payload slot / record field: `(field@sp name T)`,
// `(field@sp ~label T)` or `(field@sp T)` for positional payloads.
func (d *dumper) field(f syntax.Field) *form {
	text := "field" + at(f.Sp)
	switch {
	case f.Label != nil:
		text += " ~" + d.nameText(f.Label)
	case f.Name != nil:
		text += " " + d.nameText(f.Name)
	}
	return nodef(text, d.typeexpr(f.Type))
}

func (d *dumper) params(ps []syntax.Param) *form {
	kids := make([]*form, 0, len(ps))
	for _, p := range ps {
		kids = append(kids, d.param(p))
	}
	return nodef("params", kids...)
}

func (d *dumper) param(p syntax.Param) *form {
	text := "param" + at(p.Sp)
	if p.Label != nil {
		text += " ~" + d.nameText(p.Label)
	}
	kids := []*form{d.pat(p.Pat)}
	if p.Type != nil {
		kids = append(kids, nodef(":", d.typeexpr(p.Type)))
	}
	return nodef(text, kids...)
}

// --- type expressions ---

func (d *dumper) typeexpr(t syntax.TypeExpr) *form {
	switch t := t.(type) {
	case *syntax.TypeVar:
		// A leading ' marks a type variable, keeping it distinct from
		// a lowercase named type: 'a@10:11.
		return atomf("'" + d.bareIdent(t.Name) + at(t.Sp))
	case *syntax.TypeName:
		s := ""
		if t.Qual != nil {
			s = d.bareIdent(t.Qual) + "."
		}
		s += d.bareIdent(t.Name)
		return atomf(s + at(t.Sp))
	case *syntax.TypeApp:
		kids := make([]*form, 0, len(t.Args)+1)
		kids = append(kids, d.typeexpr(t.Ctor))
		for _, a := range t.Args {
			kids = append(kids, d.typeexpr(a))
		}
		return nodef("tapp"+at(t.Sp), kids...)
	case *syntax.TypeFn:
		kids := make([]*form, 0, len(t.Params)+1)
		for _, p := range t.Params {
			kids = append(kids, d.tparam(p))
		}
		kids = append(kids, nodef("->", d.typeexpr(t.Ret)))
		return nodef("tfn"+at(t.Sp), kids...)
	case *syntax.TypeTuple:
		kids := make([]*form, 0, len(t.Elems))
		for _, e := range t.Elems {
			kids = append(kids, d.typeexpr(e))
		}
		return nodef("ttuple"+at(t.Sp), kids...)
	case *syntax.TypeRecord:
		kids := make([]*form, 0, len(t.Fields))
		for _, f := range t.Fields {
			kids = append(kids, d.field(f))
		}
		return nodef("trecord"+at(t.Sp), kids...)
	default:
		panic("unreachable")
	}
}

func (d *dumper) tparam(p syntax.TypeParam) *form {
	text := "tparam" + at(p.Sp)
	if p.Label != nil {
		text += " ~" + d.nameText(p.Label)
	}
	return nodef(text, d.typeexpr(p.Type))
}

// --- patterns ---

func (d *dumper) pat(p syntax.Pat) *form {
	switch p := p.(type) {
	case *syntax.WildPat:
		return nodef("wildpat" + at(p.Sp))
	case *syntax.VarPat:
		return nodef("varpat" + at(p.Sp) + " " + d.nameText(p.Name))
	case *syntax.LitPat:
		return nodef("litpat" + at(p.Sp) + " " + litText(p.Lit))
	case *syntax.CtorPat:
		kids := []*form{d.expr(p.Ctor)}
		for _, a := range p.Args {
			kids = append(kids, d.carg(a))
		}
		for _, f := range p.Fields {
			kids = append(kids, d.fpat(f))
		}
		if p.Rest {
			kids = append(kids, atomf(":rest"))
		}
		return nodef("ctorpat"+at(p.Sp), kids...)
	case *syntax.TuplePat:
		kids := make([]*form, 0, len(p.Elems))
		for _, e := range p.Elems {
			kids = append(kids, d.pat(e))
		}
		return nodef("tuplepat"+at(p.Sp), kids...)
	case *syntax.RecordPat:
		kids := make([]*form, 0, len(p.Fields)+1)
		for _, f := range p.Fields {
			kids = append(kids, d.fpat(f))
		}
		if p.Rest {
			kids = append(kids, atomf(":rest"))
		}
		return nodef("recordpat"+at(p.Sp), kids...)
	case *syntax.ListPat:
		kids := make([]*form, 0, len(p.Elems)+1)
		for _, e := range p.Elems {
			kids = append(kids, d.pat(e))
		}
		if p.Rest != nil {
			kids = append(kids, nodef("rest", d.pat(p.Rest)))
		}
		return nodef("listpat"+at(p.Sp), kids...)
	case *syntax.OrPat:
		kids := make([]*form, 0, len(p.Alts))
		for _, a := range p.Alts {
			kids = append(kids, d.pat(a))
		}
		return nodef("orpat"+at(p.Sp), kids...)
	case *syntax.AsPat:
		return nodef("aspat"+at(p.Sp), d.pat(p.Pat), d.ident(p.Name))
	default:
		panic("unreachable")
	}
}

func (d *dumper) carg(a syntax.CtorArg) *form {
	text := "carg" + at(a.Sp)
	if a.Label != nil {
		text += " ~" + d.nameText(a.Label)
	}
	return nodef(text, d.pat(a.Pat))
}

func (d *dumper) fpat(f syntax.FieldPat) *form {
	text := "fpat" + at(f.Sp) + " " + d.nameText(f.Name)
	if f.Punned() {
		return nodef(text + " :pun")
	}
	return nodef(text, d.pat(f.Pat))
}

// litText renders a literal's kind and raw lexeme — shared between
// expression literals and literal patterns.
func litText(l *syntax.Lit) string {
	return l.Kind.String() + " " + atom(l.Text)
}

// --- expressions ---

func (d *dumper) expr(e syntax.Expr) *form {
	switch e := e.(type) {
	case *syntax.Ident:
		return d.ident(e)
	case *syntax.Lit:
		return nodef("lit" + at(e.Sp) + " " + litText(e))
	case *syntax.StrInterp:
		kids := make([]*form, 0, len(e.Parts))
		for _, p := range e.Parts {
			kids = append(kids, d.strpart(p))
		}
		return nodef("strinterp"+at(e.Sp), kids...)
	case *syntax.Apply:
		kids := make([]*form, 0, len(e.Args)+1)
		kids = append(kids, d.expr(e.Fn))
		for _, a := range e.Args {
			kids = append(kids, d.arg(a))
		}
		return nodef("apply"+at(e.Sp), kids...)
	case *syntax.Select:
		return nodef("sel"+at(e.Sp), d.expr(e.Recv), d.ident(e.Sel))
	case *syntax.Unary:
		return nodef("unary"+at(e.Sp)+" "+e.Op.String(), d.expr(e.X))
	case *syntax.Binary:
		return nodef("binary"+at(e.Sp)+" "+e.Op.String(), d.expr(e.X), d.expr(e.Y))
	case *syntax.Hole:
		return nodef("hole" + at(e.Sp))
	case *syntax.Lambda:
		return nodef("lambda"+at(e.Sp), d.params(e.Params), d.expr(e.Body))
	case *syntax.If:
		kids := []*form{d.expr(e.Cond), d.expr(e.Then)}
		if e.Else != nil {
			kids = append(kids, d.expr(e.Else))
		}
		return nodef("if"+at(e.Sp), kids...)
	case *syntax.Match:
		kids := make([]*form, 0, len(e.Arms)+1)
		kids = append(kids, d.expr(e.Subj))
		for _, a := range e.Arms {
			ak := []*form{d.pat(a.Pat)}
			if a.Guard != nil {
				ak = append(ak, nodef("guard", d.expr(a.Guard)))
			}
			ak = append(ak, d.expr(a.Body))
			kids = append(kids, nodef("arm"+at(a.Sp), ak...))
		}
		return nodef("match"+at(e.Sp), kids...)
	case *syntax.Block:
		kids := make([]*form, 0, len(e.Stmts))
		for _, s := range e.Stmts {
			kids = append(kids, d.expr(s))
		}
		return nodef("block"+at(e.Sp), kids...)
	case *syntax.Let:
		kids := []*form{d.pat(e.Pat)}
		if e.Type != nil {
			kids = append(kids, nodef(":", d.typeexpr(e.Type)))
		}
		kids = append(kids, d.expr(e.Value))
		return nodef("let"+at(e.Sp), kids...)
	case *syntax.Ignore:
		return nodef("ignore"+at(e.Sp), d.expr(e.X))
	case *syntax.Try:
		return nodef("try"+at(e.Sp), d.expr(e.X))
	case *syntax.GoExpr:
		return nodef("go"+at(e.Sp), d.expr(e.Body))
	case *syntax.For:
		return nodef("for"+at(e.Sp), d.pat(e.Pat), d.expr(e.Iter), d.expr(e.Body))
	case *syntax.SelectExpr:
		kids := make([]*form, 0, len(e.Arms))
		for _, a := range e.Arms {
			kids = append(kids, d.selectArm(a))
		}
		return nodef("select"+at(e.Sp), kids...)
	case *syntax.ListLit:
		kids := make([]*form, 0, len(e.Elems))
		for _, el := range e.Elems {
			kids = append(kids, d.expr(el))
		}
		return nodef("list"+at(e.Sp), kids...)
	case *syntax.Spread:
		return nodef("spread"+at(e.Sp), d.expr(e.X))
	case *syntax.TupleLit:
		kids := make([]*form, 0, len(e.Elems))
		for _, el := range e.Elems {
			kids = append(kids, d.expr(el))
		}
		return nodef("tuple"+at(e.Sp), kids...)
	case *syntax.Ascribe:
		return nodef("ascribe"+at(e.Sp), d.expr(e.X), nodef(":", d.typeexpr(e.Type)))
	case *syntax.RecordLit:
		kids := []*form{}
		if e.Base != nil {
			kids = append(kids, nodef("with", d.expr(e.Base)))
		}
		for _, f := range e.Fields {
			kids = append(kids, nodef("field"+at(f.Sp)+" "+d.nameText(f.Name), d.expr(f.Value)))
		}
		return nodef("record"+at(e.Sp), kids...)
	default:
		panic("unreachable")
	}
}

func (d *dumper) arg(a syntax.Arg) *form {
	text := "arg" + at(a.Sp)
	if a.Label != nil {
		text += " ~" + d.nameText(a.Label)
	}
	if a.Punned {
		text += " :pun"
	}
	return nodef(text, d.expr(a.Value))
}

func (d *dumper) strpart(p syntax.StrPart) *form {
	switch p := p.(type) {
	case *syntax.StrText:
		return nodef("strtext" + at(p.Sp) + " " + quote(p.Text))
	case *syntax.StrExpr:
		return nodef("splice"+at(p.Sp), d.expr(p.X))
	default:
		panic("unreachable")
	}
}

func (d *dumper) selectArm(a syntax.SelectArm) *form {
	switch a := a.(type) {
	case *syntax.RecvArm:
		return nodef("recv"+at(a.Sp), d.pat(a.Bind), d.expr(a.Chan), d.expr(a.Body))
	case *syntax.SendArm:
		return nodef("send"+at(a.Sp), d.expr(a.Chan), d.expr(a.Value), d.expr(a.Body))
	case *syntax.DefaultArm:
		return nodef("default"+at(a.Sp), d.expr(a.Body))
	default:
		panic("unreachable")
	}
}
