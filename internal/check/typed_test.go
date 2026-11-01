package check

import (
	"testing"

	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

func base() Base { return Base{Sp: sp, Typ: types.Int} }

// exprKind names each typed Expr variant with the required
// unreachable default.
func exprKind(e Expr) string {
	switch e.(type) {
	case *Lit:
		return "lit"
	case *Var:
		return "var"
	case *Apply:
		return "apply"
	case *Hole:
		return "hole"
	case *MemberExpr:
		return "member"
	case *Unary:
		return "unary"
	case *Binary:
		return "binary"
	case *StrInterp:
		return "strinterp"
	case *Lambda:
		return "lambda"
	case *If:
		return "if"
	case *Match:
		return "match"
	case *Block:
		return "block"
	case *Let:
		return "let"
	case *Ignore:
		return "ignore"
	case *Try:
		return "try"
	case *GoExpr:
		return "go"
	case *For:
		return "for"
	case *SelectExpr:
		return "selectexpr"
	case *ListLit:
		return "list"
	case *Spread:
		return "spread"
	case *TupleLit:
		return "tuple"
	case *RecordLit:
		return "record"
	case *Ascribe:
		return "ascribe"
	default:
		panic("unreachable")
	}
}

func TestTypedExprKinds(t *testing.T) {
	sym := &resolve.Symbol{Name: "x", Kind: resolve.KindVar}
	v := &Var{Base: base(), Sym: sym}
	exprs := []struct {
		expr Expr
		kind string
	}{
		{&Lit{Base: base(), Kind: syntax.LitInt, Text: "1"}, "lit"},
		{v, "var"},
		{&Apply{Base: base(), Fn: v, Call: CallInfo{Kind: CallUnknown, HoleIndex: -1}}, "apply"},
		{&Hole{Base: base()}, "hole"},
		{&MemberExpr{Base: base(), Recv: v, Sel: &syntax.Ident{Sp: sp, Name: "f"},
			Sym: &resolve.Symbol{Name: "f", Kind: resolve.KindField}, Index: 0}, "member"},
		{&Unary{Base: base(), Op: syntax.UnNeg, X: v}, "unary"},
		{&Binary{Base: base(), Op: syntax.BinPipeFwd, X: v, Y: v}, "binary"},
		{&StrInterp{Base: base(), Parts: []StrPart{&StrExpr{Sp: sp, X: v}}}, "strinterp"},
		{&Lambda{Base: base(), Params: []Param{{Pat: &VarPat{Base: base(), Sym: sym}}}, Body: v}, "lambda"},
		{&If{Base: base(), Cond: v, Then: v, Else: v}, "if"},
		{&Match{Base: base(), Subj: v, Arms: []MatchArm{{Sp: sp, Pat: &WildPat{Base: base()}, Body: v}}}, "match"},
		{&Block{Base: base(), Stmts: []Expr{v}}, "block"},
		{&Let{Base: base(), Pat: &VarPat{Base: base(), Sym: sym}, Value: v}, "let"},
		{&Ignore{Base: base(), X: v}, "ignore"},
		{&Try{Base: base(), X: v}, "try"},
		{&GoExpr{Base: base(), Body: &Block{Base: base()}}, "go"},
		{&For{Base: base(), Pat: &VarPat{Base: base(), Sym: sym}, Iter: v, Body: &Block{Base: base()}}, "for"},
		{&SelectExpr{Base: base(), Arms: []SelectArm{&DefaultArm{Sp: sp, Body: v}}}, "selectexpr"},
		{&ListLit{Base: base(), Elems: []Expr{v, &Spread{Base: base(), X: v}}}, "list"},
		{&Spread{Base: base(), X: v}, "spread"},
		{&TupleLit{Base: base(), Elems: []Expr{v, v}}, "tuple"},
		{&RecordLit{Base: base(), With: v,
			Fields: []FieldInit{{Sp: sp, Name: "x", Sym: &resolve.Symbol{Name: "x", Kind: resolve.KindField}, Value: v}}}, "record"},
		{&Ascribe{Base: base(), X: v}, "ascribe"},
	}
	for _, tc := range exprs {
		if got := exprKind(tc.expr); got != tc.kind {
			t.Errorf("exprKind = %q, want %q", got, tc.kind)
		}
		if got := tc.expr.Span(); got != sp {
			t.Errorf("%s: Span() = %v, want %v", tc.kind, got, sp)
		}
		if got := tc.expr.Type(); !types.Equal(got, types.Int) {
			t.Errorf("%s: Type() = %v, want Int", tc.kind, got)
		}
	}
}

// patKind names each typed Pat variant with the required unreachable
// default.
func patKind(p Pat) string {
	switch p.(type) {
	case *WildPat:
		return "wild"
	case *VarPat:
		return "var"
	case *LitPat:
		return "lit"
	case *CtorPat:
		return "ctor"
	case *TuplePat:
		return "tuple"
	case *RecordPat:
		return "record"
	case *ListPat:
		return "list"
	case *OrPat:
		return "or"
	case *AsPat:
		return "as"
	default:
		panic("unreachable")
	}
}

func TestTypedPatKinds(t *testing.T) {
	sym := &resolve.Symbol{Name: "x", Kind: resolve.KindVar}
	ctor := &resolve.Symbol{Name: "Some", Kind: resolve.KindCtor}
	field := &resolve.Symbol{Name: "name", Kind: resolve.KindField}
	pats := []struct {
		pat  Pat
		kind string
	}{
		{&WildPat{Base: base()}, "wild"},
		{&VarPat{Base: base(), Sym: sym}, "var"},
		{&LitPat{Base: base(), Kind: syntax.LitInt, Text: "0"}, "lit"},
		{&CtorPat{Base: base(), Name: &syntax.Ident{Sp: sp, Name: "Some"}, Ctor: ctor,
			Args: []CtorArg{{Pat: &WildPat{Base: base()}}}}, "ctor"},
		{&TuplePat{Base: base(), Elems: []Pat{&WildPat{Base: base()}}}, "tuple"},
		{&RecordPat{Base: base(), Fields: []FieldPat{{Sp: sp, Name: "name", Sym: field, Index: 0, Pat: &VarPat{Base: base(), Sym: sym}}}, Rest: true}, "record"},
		{&ListPat{Base: base(), Elems: []Pat{&VarPat{Base: base(), Sym: sym}}, Rest: &VarPat{Base: base(), Sym: sym}}, "list"},
		{&OrPat{Base: base(), Alts: []Pat{&WildPat{Base: base()}}}, "or"},
		{&AsPat{Base: base(), Pat: &WildPat{Base: base()}, Sym: sym}, "as"},
	}
	for _, tc := range pats {
		if got := patKind(tc.pat); got != tc.kind {
			t.Errorf("patKind = %q, want %q", got, tc.kind)
		}
		if got := tc.pat.Type(); !types.Equal(got, types.Int) {
			t.Errorf("%s: Type() = %v, want Int", tc.kind, got)
		}
	}
}

func declKind(d Decl) string {
	switch d.(type) {
	case *FnDecl:
		return "fn"
	case *LetDecl:
		return "let"
	case *TypeDecl:
		return "type"
	case *TestDecl:
		return "test"
	default:
		panic("unreachable")
	}
}

func TestTypedDeclsAndFile(t *testing.T) {
	fnSym := &resolve.Symbol{Name: "area", Kind: resolve.KindFunc}
	sig := types.Fn(types.Float, types.P(&types.Con{Name: "Shape"}))
	decls := []struct {
		decl Decl
		kind string
	}{
		{&FnDecl{Sp: sp, Sym: fnSym, Sig: sig,
			Params: []Param{{Pat: &VarPat{Base: base(), Sym: &resolve.Symbol{Name: "s", Kind: resolve.KindVar}}}},
			Body:   &Lit{Base: base(), Kind: syntax.LitFloat, Text: "1.0"}}, "fn"},
		{&LetDecl{Sp: sp, Pat: &VarPat{Base: base(), Sym: &resolve.Symbol{Name: "c", Kind: resolve.KindVar}},
			Value: &Lit{Base: base(), Kind: syntax.LitInt, Text: "1"}}, "let"},
		{&TypeDecl{Sp: sp, Sym: &resolve.Symbol{Name: "Shape", Kind: resolve.KindType},
			Body: &SumType{Sp: sp, Variants: []Variant{{
				Sp:  sp,
				Sym: &resolve.Symbol{Name: "Circle", Kind: resolve.KindCtor},
				Fields: []TField{
					{Sp: sp, Name: "radius", Index: 0,
						Sym: &resolve.Symbol{Name: "radius", Kind: resolve.KindField}, Type: types.Float},
					{Sp: sp, Label: "to", Index: 1, Type: types.Float},
				}}}}}, "type"},
		{&TestDecl{Sp: sp, Name: "t", Body: &Block{Base: base()}}, "test"},
	}
	for _, tc := range decls {
		if got := declKind(tc.decl); got != tc.kind {
			t.Errorf("declKind = %q, want %q", got, tc.kind)
		}
		if got := tc.decl.Span(); got != sp {
			t.Errorf("%s: Span() = %v, want %v", tc.kind, got, sp)
		}
	}

	modSym := &resolve.Symbol{Name: "fmt", Kind: resolve.KindModule, IsGo: true, Package: "fmt"}
	f := &File{
		Sp:      sp,
		Package: "shapes",
		PkgSp:   sp,
		Imports: []Import{{Sp: sp, Path: "fmt", IsGo: true, Sym: modSym}},
		Decls:   []Decl{decls[0].decl},
	}
	if f.Package != "shapes" || len(f.Imports) != 1 || !f.Imports[0].Sym.IsGo {
		t.Error("typed file lost its imports or package")
	}
}

func TestTypeBodyKinds(t *testing.T) {
	bodies := []TypeBody{
		&SumType{Sp: sp},
		&RecordType{Sp: sp},
		&AliasType{Sp: sp, Type: types.Int},
		&AbstractType{Sp: sp},
	}
	for _, b := range bodies {
		switch b.(type) {
		case *SumType, *RecordType, *AliasType, *AbstractType:
		default:
			panic("unreachable")
		}
		if b.Span() != sp {
			t.Error("body span lost")
		}
	}
}
