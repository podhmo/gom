package syntax

import (
	"fmt"
	"strings"
)

// dump renders a File (or one decl/expression when trimmed) as an
// s-expression, so that parser tests can compare tree shape
// literally. Spans are omitted — span correctness is checked
// separately — and every sealed interface switch carries the
// required unreachable default.

func dumpFile(f *File) string {
	var b strings.Builder
	b.WriteString("(file")
	if f.Package != nil {
		fmt.Fprintf(&b, " (package %s)", f.Package.Name)
	}
	for _, d := range f.Decls {
		b.WriteByte('\n')
		b.WriteString("  ")
		dumpDecl(&b, d)
	}
	b.WriteByte(')')
	return b.String()
}

func dumpDecl(b *strings.Builder, d Decl) {
	switch d := d.(type) {
	case *FnDecl:
		b.WriteString("(fn")
		if d.TestOnly {
			b.WriteString(" @test")
		}
		fmt.Fprintf(b, " %s", d.Name.Name)
		for _, prm := range d.Params {
			b.WriteByte(' ')
			dumpParam(b, prm)
		}
		b.WriteString(" -> ")
		dumpType(b, d.Ret)
		if d.Body != nil {
			b.WriteString(" = ")
			dumpExpr(b, d.Body)
		}
		b.WriteByte(')')
	case *LetDecl:
		b.WriteString("(let")
		if d.TestOnly {
			b.WriteString(" @test")
		}
		b.WriteByte(' ')
		dumpPat(b, d.Pat)
		if d.Type != nil {
			b.WriteString(" : ")
			dumpType(b, d.Type)
		}
		if d.Value != nil {
			b.WriteString(" = ")
			dumpExpr(b, d.Value)
		}
		b.WriteByte(')')
	case *TypeDecl:
		b.WriteString("(type ")
		b.WriteString(d.Name.Name)
		for _, p := range d.Params {
			fmt.Fprintf(b, " %s", p.Name)
		}
		b.WriteString(" = ")
		dumpTypeBody(b, d.Body)
		b.WriteByte(')')
	case *ImportDecl:
		if d.Go {
			b.WriteString("(import go")
		} else {
			b.WriteString("(import")
		}
		fmt.Fprintf(b, " %s %q)", d.Name.Name, d.Path)
	case *ExternDecl:
		fmt.Fprintf(b, "(extern go %q", d.Path)
		for _, e := range d.Entries {
			switch e := e.(type) {
			case *ExternFn:
				b.WriteString(" (fn ")
				b.WriteString(e.Name.Name)
				for _, prm := range e.Params {
					b.WriteByte(' ')
					dumpParam(b, prm)
				}
				b.WriteString(" -> ")
				dumpType(b, e.Ret)
				b.WriteByte(')')
			default:
				panic("unreachable")
			}
		}
		b.WriteByte(')')
	case *TestDecl:
		fmt.Fprintf(b, "(test %q ", d.Name)
		dumpExpr(b, d.Body)
		b.WriteByte(')')
	default:
		panic("unreachable")
	}
}

func dumpTypeBody(b *strings.Builder, body TypeBody) {
	switch body := body.(type) {
	case *SumBody:
		b.WriteString("(sum")
		for _, v := range body.Variants {
			fmt.Fprintf(b, " |%s", v.Name.Name)
			for _, f := range v.Params {
				b.WriteByte(' ')
				dumpField(b, f)
			}
		}
		b.WriteByte(')')
	case *RecordBody:
		b.WriteString("(record")
		for _, f := range body.Fields {
			b.WriteByte(' ')
			dumpField(b, f)
		}
		b.WriteByte(')')
	case *AliasBody:
		b.WriteString("(alias ")
		dumpType(b, body.Type)
		b.WriteByte(')')
	case *AbstractBody:
		b.WriteString("(abstract)")
	default:
		panic("unreachable")
	}
}

// dumpField renders a variant/record field: "T", "(name: T)" or
// "(~name: T)".
func dumpField(b *strings.Builder, f Field) {
	switch {
	case f.Label != nil:
		fmt.Fprintf(b, "(~%s: ", f.Label.Name)
		dumpType(b, f.Type)
		b.WriteByte(')')
	case f.Name != nil:
		fmt.Fprintf(b, "(%s: ", f.Name.Name)
		dumpType(b, f.Type)
		b.WriteByte(')')
	default:
		dumpType(b, f.Type)
	}
}

func dumpParam(b *strings.Builder, p Param) {
	b.WriteByte('(')
	if p.Label != nil {
		fmt.Fprintf(b, "~%s", p.Label.Name)
	} else {
		dumpPat(b, p.Pat)
	}
	if p.Type != nil {
		b.WriteString(" : ")
		dumpType(b, p.Type)
	}
	b.WriteByte(')')
}

func dumpExpr(b *strings.Builder, e Expr) {
	switch e := e.(type) {
	case *Ident:
		b.WriteString(e.Name)
	case *Lit:
		b.WriteString(e.Text)
	case *StrInterp:
		b.WriteString("(interp")
		for _, part := range e.Parts {
			b.WriteByte(' ')
			switch part := part.(type) {
			case *StrText:
				fmt.Fprintf(b, "%q", part.Text)
			case *StrExpr:
				b.WriteString("$(")
				dumpExpr(b, part.X)
				b.WriteByte(')')
			default:
				panic("unreachable")
			}
		}
		b.WriteByte(')')
	case *Apply:
		b.WriteString("(apply ")
		dumpExpr(b, e.Fn)
		for _, a := range e.Args {
			b.WriteByte(' ')
			dumpArg(b, a)
		}
		b.WriteByte(')')
	case *Select:
		b.WriteByte('(')
		dumpExpr(b, e.Recv)
		b.WriteByte('.')
		b.WriteString(e.Sel.Name)
		b.WriteByte(')')
	case *Unary:
		fmt.Fprintf(b, "(%s ", e.Op)
		dumpExpr(b, e.X)
		b.WriteByte(')')
	case *Binary:
		fmt.Fprintf(b, "(%s ", e.Op)
		dumpExpr(b, e.X)
		b.WriteByte(' ')
		dumpExpr(b, e.Y)
		b.WriteByte(')')
	case *Hole:
		b.WriteByte('_')
	case *Lambda:
		b.WriteString("(fn")
		for _, prm := range e.Params {
			b.WriteByte(' ')
			dumpParam(b, prm)
		}
		b.WriteString(" -> ")
		dumpExpr(b, e.Body)
		b.WriteByte(')')
	case *If:
		b.WriteString("(if ")
		dumpExpr(b, e.Cond)
		b.WriteByte(' ')
		dumpExpr(b, e.Then)
		if e.Else != nil {
			b.WriteString(" else ")
			dumpExpr(b, e.Else)
		}
		b.WriteByte(')')
	case *Match:
		b.WriteString("(match ")
		dumpExpr(b, e.Subj)
		for _, a := range e.Arms {
			b.WriteString(" (")
			dumpPat(b, a.Pat)
			if a.Guard != nil {
				b.WriteString(" if ")
				dumpExpr(b, a.Guard)
			}
			b.WriteString(" -> ")
			dumpExpr(b, a.Body)
			b.WriteByte(')')
		}
		b.WriteByte(')')
	case *Block:
		b.WriteString("(block")
		for _, s := range e.Stmts {
			b.WriteByte(' ')
			dumpExpr(b, s)
		}
		b.WriteByte(')')
	case *Let:
		b.WriteString("(let ")
		dumpPat(b, e.Pat)
		if e.Type != nil {
			b.WriteString(" : ")
			dumpType(b, e.Type)
		}
		b.WriteString(" = ")
		dumpExpr(b, e.Value)
		b.WriteByte(')')
	case *Ignore:
		b.WriteString("(ignore ")
		dumpExpr(b, e.X)
		b.WriteByte(')')
	case *Try:
		b.WriteString("(try ")
		dumpExpr(b, e.X)
		b.WriteByte(')')
	case *GoExpr:
		b.WriteString("(go ")
		dumpExpr(b, e.Body)
		b.WriteByte(')')
	case *For:
		b.WriteString("(for ")
		dumpPat(b, e.Pat)
		b.WriteString(" in ")
		dumpExpr(b, e.Iter)
		b.WriteByte(' ')
		dumpExpr(b, e.Body)
		b.WriteByte(')')
	case *SelectExpr:
		b.WriteString("(select")
		for _, a := range e.Arms {
			b.WriteByte(' ')
			switch a := a.(type) {
			case *RecvArm:
				b.WriteString("(recv ")
				dumpPat(b, a.Bind)
				b.WriteString(" <- ")
				dumpExpr(b, a.Chan)
				b.WriteString(" -> ")
				dumpExpr(b, a.Body)
				b.WriteByte(')')
			case *SendArm:
				b.WriteString("(send ")
				dumpExpr(b, a.Chan)
				b.WriteByte(' ')
				dumpExpr(b, a.Value)
				b.WriteString(" -> ")
				dumpExpr(b, a.Body)
				b.WriteByte(')')
			case *DefaultArm:
				b.WriteString("(default -> ")
				dumpExpr(b, a.Body)
				b.WriteByte(')')
			default:
				panic("unreachable")
			}
		}
		b.WriteByte(')')
	case *ListLit:
		b.WriteString("(list")
		for _, x := range e.Elems {
			b.WriteByte(' ')
			dumpExpr(b, x)
		}
		b.WriteByte(')')
	case *Spread:
		b.WriteString("(..")
		dumpExpr(b, e.X)
		b.WriteByte(')')
	case *TupleLit:
		b.WriteString("(tuple")
		for _, x := range e.Elems {
			b.WriteByte(' ')
			dumpExpr(b, x)
		}
		b.WriteByte(')')
	case *Ascribe:
		b.WriteString("(: ")
		dumpExpr(b, e.X)
		b.WriteByte(' ')
		dumpType(b, e.Type)
		b.WriteByte(')')
	case *RecordLit:
		b.WriteString("(rec")
		if e.Base != nil {
			b.WriteByte(' ')
			dumpExpr(b, e.Base)
			b.WriteString(" with")
		}
		for _, f := range e.Fields {
			fmt.Fprintf(b, " %s = ", f.Name.Name)
			dumpExpr(b, f.Value)
		}
		b.WriteByte(')')
	default:
		panic("unreachable")
	}
}

// dumpArg renders an application argument: "v", "~l: v" or "~l".
func dumpArg(b *strings.Builder, a Arg) {
	switch {
	case a.Punned:
		fmt.Fprintf(b, "~%s", a.Label.Name)
	case a.Label != nil:
		fmt.Fprintf(b, "~%s: ", a.Label.Name)
		dumpExpr(b, a.Value)
	default:
		dumpExpr(b, a.Value)
	}
}

func dumpPat(b *strings.Builder, p Pat) {
	switch p := p.(type) {
	case *WildPat:
		b.WriteByte('_')
	case *VarPat:
		b.WriteString(p.Name.Name)
	case *LitPat:
		b.WriteString(p.Lit.Text)
	case *CtorPat:
		b.WriteString("(ctor ")
		dumpExpr(b, p.Ctor)
		for _, a := range p.Args {
			b.WriteByte(' ')
			if a.Label != nil {
				fmt.Fprintf(b, "~%s: ", a.Label.Name)
			}
			dumpPat(b, a.Pat)
		}
		for _, f := range p.Fields {
			b.WriteByte(' ')
			dumpFieldPat(b, f)
		}
		if p.Rest {
			b.WriteString(" ..")
		}
		b.WriteByte(')')
	case *TuplePat:
		b.WriteString("(,pat")
		for _, x := range p.Elems {
			b.WriteByte(' ')
			dumpPat(b, x)
		}
		b.WriteByte(')')
	case *RecordPat:
		b.WriteString("(recpat")
		for _, f := range p.Fields {
			b.WriteByte(' ')
			dumpFieldPat(b, f)
		}
		if p.Rest {
			b.WriteString(" ..")
		}
		b.WriteByte(')')
	case *ListPat:
		b.WriteString("(listpat")
		for _, x := range p.Elems {
			b.WriteByte(' ')
			dumpPat(b, x)
		}
		if p.Rest != nil {
			b.WriteString(" ..")
			dumpPat(b, p.Rest)
		}
		b.WriteByte(')')
	case *OrPat:
		b.WriteString("(or")
		for _, x := range p.Alts {
			b.WriteByte(' ')
			dumpPat(b, x)
		}
		b.WriteByte(')')
	case *AsPat:
		b.WriteString("(as ")
		dumpPat(b, p.Pat)
		fmt.Fprintf(b, " %s)", p.Name.Name)
	default:
		panic("unreachable")
	}
}

// dumpFieldPat renders "f" (punned) or "f = p".
func dumpFieldPat(b *strings.Builder, f FieldPat) {
	if f.Punned() {
		b.WriteString(f.Name.Name)
		return
	}
	fmt.Fprintf(b, "%s = ", f.Name.Name)
	dumpPat(b, f.Pat)
}

func dumpType(b *strings.Builder, t TypeExpr) {
	if t == nil {
		b.WriteString("<?>")
		return
	}
	switch t := t.(type) {
	case *TypeVar:
		b.WriteString(t.Name.Name)
	case *TypeName:
		if t.Qual != nil {
			fmt.Fprintf(b, "(%s.%s)", t.Qual.Name, t.Name.Name)
		} else {
			b.WriteString(t.Name.Name)
		}
	case *TypeApp:
		b.WriteByte('(')
		dumpType(b, t.Ctor)
		for _, a := range t.Args {
			b.WriteByte(' ')
			dumpType(b, a)
		}
		b.WriteByte(')')
	case *TypeFn:
		b.WriteString("(->")
		for _, prm := range t.Params {
			b.WriteByte(' ')
			if prm.Label != nil {
				fmt.Fprintf(b, "~%s: ", prm.Label.Name)
			}
			dumpType(b, prm.Type)
		}
		b.WriteString(" -> ")
		dumpType(b, t.Ret)
		b.WriteByte(')')
	case *TypeTuple:
		b.WriteString("(,T")
		for _, e := range t.Elems {
			b.WriteByte(' ')
			dumpType(b, e)
		}
		b.WriteByte(')')
	case *TypeRecord:
		b.WriteString("(rectype")
		for _, f := range t.Fields {
			b.WriteByte(' ')
			dumpField(b, f)
		}
		b.WriteByte(')')
	default:
		panic("unreachable")
	}
}
