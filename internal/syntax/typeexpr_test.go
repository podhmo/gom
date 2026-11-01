package syntax

import (
	"testing"
)

// typeKind names each TypeExpr variant with the required unreachable
// default.
func typeKind(t TypeExpr) string {
	switch t.(type) {
	case *TypeVar:
		return "var"
	case *TypeName:
		return "name"
	case *TypeApp:
		return "app"
	case *TypeFn:
		return "fn"
	case *TypeTuple:
		return "tuple"
	case *TypeRecord:
		return "record"
	default:
		panic("unreachable")
	}
}

func TestTypeExprKindsAndSpans(t *testing.T) {
	intTy := &TypeName{Sp: sp, Name: &Ident{Sp: sp, Name: "Int"}}
	exprs := []struct {
		expr TypeExpr
		kind string
	}{
		{&TypeVar{Sp: sp, Name: &Ident{Sp: sp, Name: "a"}}, "var"},
		{intTy, "name"},
		{&TypeName{Sp: sp, Qual: &Ident{Sp: sp, Name: "io"},
			Name: &Ident{Sp: sp, Name: "Reader"}}, "name"},
		{&TypeApp{Sp: sp, Ctor: &TypeName{Sp: sp, Name: &Ident{Sp: sp, Name: "List"}},
			Args: []TypeExpr{intTy}}, "app"},
		{&TypeFn{Sp: sp, Params: []TypeParam{{Sp: sp, Type: intTy}}, Ret: intTy}, "fn"},
		{&TypeTuple{Sp: sp, Elems: []TypeExpr{intTy, intTy}}, "tuple"},
		{&TypeRecord{Sp: sp, Fields: []Field{{Sp: sp, Name: &Ident{Sp: sp, Name: "x"}, Type: intTy}}}, "record"},
	}
	for _, tc := range exprs {
		if got := typeKind(tc.expr); got != tc.kind {
			t.Errorf("typeKind = %q, want %q", got, tc.kind)
		}
		if got := tc.expr.Span(); got != sp {
			t.Errorf("%s: Span() = %v, want %v", tc.kind, got, sp)
		}
	}
}

func TestTypeParamLabeled(t *testing.T) {
	intTy := &TypeName{Sp: sp, Name: &Ident{Sp: sp, Name: "Int"}}
	p := TypeParam{Sp: sp, Label: &Ident{Sp: sp, Name: "to"}, Type: intTy}
	if !p.Labeled() {
		t.Error("~to: Int not reported labeled")
	}
}
