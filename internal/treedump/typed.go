package treedump

import (
	"fmt"
	"strings"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/types"
)

// DumpTyped renders a type-checked file — the output of phase 4.
// Every typed node carries its resolved type as a `:T` annotation,
// applications show their structured CallInfo as `(call ...)`, and
// resolved symbols appear as `name%<n>` references collected in the
// trailing `(syms)` section.
func DumpTyped(f *check.File) string {
	d := newDumper()
	text := "file" + at(f.Sp)
	if f.Package != "" {
		text += " " + atom(f.Package) + at(f.PkgSp)
	} else {
		text += " -"
	}
	kids := make([]*form, 0, len(f.Imports)+len(f.Decls)+1)
	for _, im := range f.Imports {
		text := "import" + at(im.Sp) + " " + quote(im.Path) + " " + d.symRef(im.Sym)
		if im.IsGo {
			text += " :go"
		}
		kids = append(kids, nodef(text))
	}
	for _, decl := range f.Decls {
		kids = append(kids, d.tdecl(decl))
	}
	kids = append(kids, d.syms())
	return render(nodef(text, kids...))
}

// DumpType renders a types.Type in its surface syntax — the same
// stable rendering diagnostics use, so dump and message agree.
func DumpType(t types.Type) string {
	return tatom(t)
}

// head builds the `name@span :Type` prefix every typed node shares.
func head(name string, b check.Base) string {
	return name + at(b.Sp) + " " + typ(b.Typ)
}

// --- typed declarations ---

func (d *dumper) tdecl(dd check.Decl) *form {
	switch dd := dd.(type) {
	case *check.FnDecl:
		text := "fn" + at(dd.Sp) + " " + d.symRef(dd.Sym)
		if dd.Sig != nil {
			text += " " + typ(dd.Sig)
		}
		return nodef(text, d.tparams(dd.Params), d.texpr(dd.Body))
	case *check.LetDecl:
		return nodef("let"+at(dd.Sp), d.tpat(dd.Pat), d.texpr(dd.Value))
	case *check.TypeDecl:
		// Intern the declared type's symbol before walking the body so
		// %n indices follow source order (type name before members).
		text := "type" + at(dd.Sp) + " " + d.symRef(dd.Sym)
		kids := []*form{}
		if len(dd.TypeParams) > 0 {
			ps := make([]*form, 0, len(dd.TypeParams))
			for _, p := range dd.TypeParams {
				ps = append(ps, atomf(atom(p.String())))
			}
			kids = append(kids, nodef("tparams", ps...))
		}
		kids = append(kids, d.ttypebody(dd.Body))
		return nodef(text, kids...)
	case *check.TestDecl:
		return nodef("test"+at(dd.Sp)+" "+quote(dd.Name)+at(dd.NameSp), d.texpr(dd.Body))
	default:
		panic("unreachable")
	}
}

func (d *dumper) ttypebody(b check.TypeBody) *form {
	switch b := b.(type) {
	case *check.SumType:
		kids := make([]*form, 0, len(b.Variants))
		for _, v := range b.Variants {
			// Intern the ctor symbol before its fields: the variant
			// name precedes the payload in source.
			vtext := "variant" + at(v.Sp) + " " + d.symRef(v.Sym)
			vk := make([]*form, 0, len(v.Fields))
			for _, f := range v.Fields {
				vk = append(vk, d.tfield(f))
			}
			kids = append(kids, nodef(vtext, vk...))
		}
		return nodef("sum"+at(b.Sp), kids...)
	case *check.RecordType:
		kids := make([]*form, 0, len(b.Fields))
		for _, f := range b.Fields {
			kids = append(kids, d.tfield(f))
		}
		return nodef("record"+at(b.Sp), kids...)
	case *check.AliasType:
		return nodef("alias"+at(b.Sp), atomf(typ(b.Type)))
	case *check.AbstractType:
		return nodef("abstract" + at(b.Sp))
	default:
		panic("unreachable")
	}
}

// tfield renders a resolved payload slot: `(field@sp :T name -> sym :i0)`
// with the written name/label first, then its field symbol and index.
func (d *dumper) tfield(f check.TField) *form {
	text := "field" + at(f.Sp) + " " + typ(f.Type)
	switch {
	case f.Label != "":
		text += " ~" + atom(f.Label)
	case f.Name != "":
		text += " " + atom(f.Name)
	}
	if f.Sym != nil {
		text += " -> " + d.symRef(f.Sym)
	}
	return nodef(text + fmt.Sprintf(" :i%d", f.Index))
}

func (d *dumper) tparams(ps []check.Param) *form {
	kids := make([]*form, 0, len(ps))
	for _, p := range ps {
		text := "param"
		if p.Label != "" {
			text += " ~" + atom(p.Label)
		}
		kids = append(kids, nodef(text, d.tpat(p.Pat)))
	}
	return nodef("params", kids...)
}

// --- call info ---

// call renders the structured call info of a typed Apply:
// `(call direct callee=f%1 arity=3 applied=2 unapplied=1
// argtoparam=[0,1,2] hole=0 variadic targs=[Int,(List Bool)])`.
// Zero-valued optional fields are omitted.
func (d *dumper) call(c check.CallInfo) *form {
	var b strings.Builder
	b.WriteString("call ")
	b.WriteString(c.Kind.String())
	if c.Callee != nil {
		fmt.Fprintf(&b, " callee=%s", d.symRef(c.Callee))
	}
	fmt.Fprintf(&b, " arity=%d applied=%d", c.Arity, c.Applied)
	if c.Unapplied != 0 {
		fmt.Fprintf(&b, " unapplied=%d", c.Unapplied)
	}
	if c.ArgToParam != nil {
		b.WriteString(" argtoparam=[")
		for i, p := range c.ArgToParam {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "%d", p)
		}
		b.WriteByte(']')
	}
	if c.HoleIndex >= 0 {
		fmt.Fprintf(&b, " hole=%d", c.HoleIndex)
	}
	if c.Variadic {
		b.WriteString(" variadic")
	}
	if len(c.TypeArgs) > 0 {
		b.WriteString(" targs=[")
		for i, t := range c.TypeArgs {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(tatom(t))
		}
		b.WriteByte(']')
	}
	return nodef(b.String())
}

// --- typed expressions ---

func (d *dumper) texpr(e check.Expr) *form {
	if e == nil {
		// .gomi signatures leave FnDecl.Body nil.
		return nodef("?")
	}
	switch e := e.(type) {
	case *check.Lit:
		return nodef(head("lit", e.Base) + " " + e.Kind.String() + " " + atom(e.Text))
	case *check.Var:
		return nodef(head("var", e.Base) + " " + d.symRef(e.Sym))
	case *check.Apply:
		kids := make([]*form, 0, len(e.Args)+2)
		kids = append(kids, d.call(e.Call), d.texpr(e.Fn))
		for _, a := range e.Args {
			kids = append(kids, d.targ(a))
		}
		return nodef(head("apply", e.Base), kids...)
	case *check.Hole:
		return nodef(head("hole", e.Base))
	case *check.MemberExpr:
		// The receiver precedes the selector in source — walk it
		// before interning the member symbol.
		recv := d.texpr(e.Recv)
		text := head("member", e.Base) + " " + d.nameText(e.Sel)
		if e.Sym != nil {
			text += " -> " + d.symRef(e.Sym)
		}
		return nodef(text+fmt.Sprintf(" :i%d", e.Index), recv)
	case *check.Unary:
		return nodef(head("unary", e.Base)+" "+e.Op.String(), d.texpr(e.X))
	case *check.Binary:
		return nodef(head("binary", e.Base)+" "+e.Op.String(), d.texpr(e.X), d.texpr(e.Y))
	case *check.StrInterp:
		kids := make([]*form, 0, len(e.Parts))
		for _, p := range e.Parts {
			kids = append(kids, d.tstrpart(p))
		}
		return nodef(head("strinterp", e.Base), kids...)
	case *check.Lambda:
		return nodef(head("lambda", e.Base), d.tparams(e.Params), d.texpr(e.Body))
	case *check.If:
		kids := []*form{d.texpr(e.Cond), d.texpr(e.Then)}
		if e.Else != nil {
			kids = append(kids, d.texpr(e.Else))
		}
		return nodef(head("if", e.Base), kids...)
	case *check.Match:
		kids := make([]*form, 0, len(e.Arms)+1)
		kids = append(kids, d.texpr(e.Subj))
		for _, a := range e.Arms {
			ak := []*form{d.tpat(a.Pat)}
			if a.Guard != nil {
				ak = append(ak, nodef("guard", d.texpr(a.Guard)))
			}
			ak = append(ak, d.texpr(a.Body))
			kids = append(kids, nodef("arm"+at(a.Sp), ak...))
		}
		return nodef(head("match", e.Base), kids...)
	case *check.Block:
		kids := make([]*form, 0, len(e.Stmts))
		for _, s := range e.Stmts {
			kids = append(kids, d.texpr(s))
		}
		return nodef(head("block", e.Base), kids...)
	case *check.Let:
		return nodef(head("let", e.Base), d.tpat(e.Pat), d.texpr(e.Value))
	case *check.Ignore:
		return nodef(head("ignore", e.Base), d.texpr(e.X))
	case *check.Try:
		return nodef(head("try", e.Base), d.texpr(e.X))
	case *check.GoExpr:
		return nodef(head("go", e.Base), d.texpr(e.Body))
	case *check.For:
		return nodef(head("for", e.Base), d.tpat(e.Pat), d.texpr(e.Iter), d.texpr(e.Body))
	case *check.SelectExpr:
		kids := make([]*form, 0, len(e.Arms))
		for _, a := range e.Arms {
			kids = append(kids, d.tselectArm(a))
		}
		return nodef(head("select", e.Base), kids...)
	case *check.ListLit:
		kids := make([]*form, 0, len(e.Elems))
		for _, el := range e.Elems {
			kids = append(kids, d.texpr(el))
		}
		return nodef(head("list", e.Base), kids...)
	case *check.Spread:
		return nodef(head("spread", e.Base), d.texpr(e.X))
	case *check.TupleLit:
		kids := make([]*form, 0, len(e.Elems))
		for _, el := range e.Elems {
			kids = append(kids, d.texpr(el))
		}
		return nodef(head("tuple", e.Base), kids...)
	case *check.RecordLit:
		kids := []*form{}
		if e.With != nil {
			kids = append(kids, nodef("with", d.texpr(e.With)))
		}
		for _, f := range e.Fields {
			text := "field" + at(f.Sp) + " " + atom(f.Name)
			if f.Sym != nil {
				text += " -> " + d.symRef(f.Sym)
			}
			kids = append(kids, nodef(text, d.texpr(f.Value)))
		}
		return nodef(head("record", e.Base), kids...)
	case *check.Ascribe:
		text := head("ascribe", e.Base)
		if e.Conv {
			text += " :conv"
		}
		return nodef(text, d.texpr(e.X))
	default:
		panic("unreachable")
	}
}

func (d *dumper) targ(a check.Arg) *form {
	text := "arg" + at(a.Span())
	if a.Label != "" {
		text += " ~" + atom(a.Label) + at(a.LabelSp)
	}
	return nodef(text, d.texpr(a.Value))
}

func (d *dumper) tstrpart(p check.StrPart) *form {
	switch p := p.(type) {
	case *check.StrText:
		return nodef("strtext" + at(p.Sp) + " " + quote(p.Text))
	case *check.StrExpr:
		return nodef("splice"+at(p.Sp), d.texpr(p.X))
	default:
		panic("unreachable")
	}
}

func (d *dumper) tselectArm(a check.SelectArm) *form {
	switch a := a.(type) {
	case *check.RecvArm:
		return nodef("recv"+at(a.Sp), d.tpat(a.Bind), d.texpr(a.Chan), d.texpr(a.Body))
	case *check.SendArm:
		return nodef("send"+at(a.Sp), d.texpr(a.Chan), d.texpr(a.Value), d.texpr(a.Body))
	case *check.DefaultArm:
		return nodef("default"+at(a.Sp), d.texpr(a.Body))
	default:
		panic("unreachable")
	}
}

// --- typed patterns ---

func (d *dumper) tpat(p check.Pat) *form {
	switch p := p.(type) {
	case *check.WildPat:
		return nodef(head("wildpat", p.Base))
	case *check.VarPat:
		return nodef(head("varpat", p.Base) + " " + d.symRef(p.Sym))
	case *check.LitPat:
		return nodef(head("litpat", p.Base) + " " + p.Kind.String() + " " + atom(p.Text))
	case *check.CtorPat:
		// The written constructor name is a small expression (an
		// *Ident or *Select) — render it inline in the head, then the
		// resolved ctor symbol: `(ctorpat@10:20 :T Some@10:14 -> Some%2)`.
		text := head("ctorpat", p.Base) + " " + inline(d.expr(p.Name)) + " -> " + d.symRef(p.Ctor)
		kids := []*form{}
		for _, a := range p.Args {
			atext := "carg"
			if a.Label != "" {
				atext += " ~" + atom(a.Label)
			}
			kids = append(kids, nodef(atext, d.tpat(a.Pat)))
		}
		for _, f := range p.Fields {
			kids = append(kids, d.tfpat(f))
		}
		if p.Rest {
			kids = append(kids, atomf(":rest"))
		}
		return nodef(text, kids...)
	case *check.TuplePat:
		kids := make([]*form, 0, len(p.Elems))
		for _, el := range p.Elems {
			kids = append(kids, d.tpat(el))
		}
		return nodef(head("tuplepat", p.Base), kids...)
	case *check.RecordPat:
		kids := make([]*form, 0, len(p.Fields)+1)
		for _, f := range p.Fields {
			kids = append(kids, d.tfpat(f))
		}
		if p.Rest {
			kids = append(kids, atomf(":rest"))
		}
		return nodef(head("recordpat", p.Base), kids...)
	case *check.ListPat:
		kids := make([]*form, 0, len(p.Elems)+1)
		for _, el := range p.Elems {
			kids = append(kids, d.tpat(el))
		}
		if p.Rest != nil {
			kids = append(kids, nodef("rest", d.tpat(p.Rest)))
		}
		return nodef(head("listpat", p.Base), kids...)
	case *check.OrPat:
		kids := make([]*form, 0, len(p.Alts))
		for _, a := range p.Alts {
			kids = append(kids, d.tpat(a))
		}
		return nodef(head("orpat", p.Base), kids...)
	case *check.AsPat:
		return nodef(head("aspat", p.Base), d.tpat(p.Pat), atomf("->"), atomf(d.symRef(p.Sym)))
	default:
		panic("unreachable")
	}
}

func (d *dumper) tfpat(f check.FieldPat) *form {
	text := "fpat" + at(f.Sp) + " " + atom(f.Name)
	if f.Sym != nil {
		text += " -> " + d.symRef(f.Sym)
	}
	return nodef(text+fmt.Sprintf(" :i%d", f.Index), d.tpat(f.Pat))
}
