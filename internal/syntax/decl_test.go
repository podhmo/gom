package syntax

import (
	"testing"
)

// declKind names each Decl variant with the required unreachable default.
func declKind(d Decl) string {
	switch d.(type) {
	case *FnDecl:
		return "fn"
	case *LetDecl:
		return "let"
	case *TypeDecl:
		return "type"
	case *ImportDecl:
		return "import"
	case *ExternDecl:
		return "extern"
	case *TestDecl:
		return "test"
	default:
		panic("unreachable")
	}
}

func bodyKind(b TypeBody) string {
	switch b.(type) {
	case *SumBody:
		return "sum"
	case *RecordBody:
		return "record"
	case *AliasBody:
		return "alias"
	case *AbstractBody:
		return "abstract"
	default:
		panic("unreachable")
	}
}

func TestDeclKindsAndSpans(t *testing.T) {
	id := &Ident{Sp: sp, Name: "x"}
	decls := []struct {
		decl Decl
		kind string
	}{
		{&FnDecl{Sp: sp, Name: id, Params: []Param{{Sp: sp, Pat: &VarPat{Sp: sp, Name: id}}}, Body: id}, "fn"},
		{&LetDecl{Sp: sp, Pat: &VarPat{Sp: sp, Name: id}, Value: id}, "let"},
		{&TypeDecl{Sp: sp, Name: id, Body: &AbstractBody{Sp: sp}}, "type"},
		{&ImportDecl{Sp: sp, Go: true, Path: "strconv", Name: id}, "import"},
		{&ExternDecl{Sp: sp, Path: "flag", Entries: []ExternEntry{
			&ExternFn{Sp: sp, Name: id}}}, "extern"},
		{&TestDecl{Sp: sp, Name: "rect area", Body: &Block{Sp: sp}}, "test"},
	}
	for _, tc := range decls {
		if got := declKind(tc.decl); got != tc.kind {
			t.Errorf("declKind = %q, want %q", got, tc.kind)
		}
		if got := tc.decl.Span(); got != sp {
			t.Errorf("%s: Span() = %v, want %v", tc.kind, got, sp)
		}
	}
}

func TestTypeBodyKinds(t *testing.T) {
	intTy := &TypeName{Sp: sp, Name: &Ident{Sp: sp, Name: "Int"}}
	bodies := []struct {
		body TypeBody
		kind string
	}{
		{&SumBody{Sp: sp, Variants: []Variant{{Sp: sp, Name: &Ident{Sp: sp, Name: "None"}}}}, "sum"},
		{&RecordBody{Sp: sp, Fields: []Field{{Sp: sp, Name: &Ident{Sp: sp, Name: "x"}, Type: intTy}}}, "record"},
		{&AliasBody{Sp: sp, Type: intTy}, "alias"},
		{&AbstractBody{Sp: sp}, "abstract"},
	}
	for _, tc := range bodies {
		if got := bodyKind(tc.body); got != tc.kind {
			t.Errorf("bodyKind = %q, want %q", got, tc.kind)
		}
	}
}

func TestParamAndFieldHelpers(t *testing.T) {
	labeled := Param{Sp: sp, Label: &Ident{Sp: sp, Name: "to"},
		Pat: &VarPat{Sp: sp, Name: &Ident{Sp: sp, Name: "to"}}}
	if !labeled.Labeled() {
		t.Error("labeled param not reported labeled")
	}
	pos := Field{Sp: sp, Type: &TypeName{Sp: sp, Name: &Ident{Sp: sp, Name: "Int"}}}
	if got := pos.ParamName(); got != "" {
		t.Errorf("positional field ParamName = %q, want \"\"", got)
	}
	lf := Field{Sp: sp, Label: &Ident{Sp: sp, Name: "to"}}
	if got := lf.ParamName(); got != "to" {
		t.Errorf("labeled field ParamName = %q, want \"to\"", got)
	}
}

func TestFileAndComment(t *testing.T) {
	f := &File{Sp: sp, Package: &Ident{Sp: sp, Name: "shapes"},
		Comments: []*Comment{
			{Sp: sp, Kind: CommentDoc, Text: "doc"},
			{Sp: sp, Kind: CommentLine, Text: "line"},
		}}
	if got := f.PackageName(); got != "shapes" {
		t.Errorf("PackageName = %q, want %q", got, "shapes")
	}
	if !f.Comments[0].Doc() {
		t.Error("doc comment not reported Doc()")
	}
	if f.Comments[1].Doc() {
		t.Error("line comment reported Doc()")
	}
	empty := &File{}
	if got := empty.PackageName(); got != "" {
		t.Errorf("missing package clause gave %q", got)
	}
}
