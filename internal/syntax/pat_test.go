package syntax

import (
	"testing"
)

// patKind names each Pat variant with the required unreachable default.
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

func TestPatKindsAndSpans(t *testing.T) {
	id := &Ident{Sp: sp, Name: "x"}
	lit := &Lit{Sp: sp, Kind: LitInt, Text: "0"}
	pats := []struct {
		pat  Pat
		kind string
	}{
		{&WildPat{Sp: sp}, "wild"},
		{&VarPat{Sp: sp, Name: id}, "var"},
		{&LitPat{Sp: sp, Lit: lit}, "lit"},
		{&CtorPat{Sp: sp, Ctor: &Ident{Sp: sp, Name: "Some"},
			Args: []CtorArg{{Sp: sp, Pat: &WildPat{Sp: sp}}}}, "ctor"},
		{&CtorPat{Sp: sp, Ctor: &Ident{Sp: sp, Name: "Let"},
			Fields: []FieldPat{{Sp: sp, Name: id, Pat: &VarPat{Sp: sp, Name: id}}},
			Rest:   true}, "ctor"},
		{&TuplePat{Sp: sp, Elems: []Pat{&WildPat{Sp: sp}, &WildPat{Sp: sp}}}, "tuple"},
		{&RecordPat{Sp: sp, Fields: []FieldPat{{Sp: sp, Name: id}}, Rest: true}, "record"},
		{&ListPat{Sp: sp, Elems: []Pat{&VarPat{Sp: sp, Name: id}},
			Rest: &VarPat{Sp: sp, Name: &Ident{Sp: sp, Name: "rest"}}}, "list"},
		{&OrPat{Sp: sp, Alts: []Pat{&WildPat{Sp: sp}, litPat(lit)}}, "or"},
		{&AsPat{Sp: sp, Pat: &WildPat{Sp: sp}, Name: id}, "as"},
	}
	for _, tc := range pats {
		if got := patKind(tc.pat); got != tc.kind {
			t.Errorf("patKind = %q, want %q", got, tc.kind)
		}
		if got := tc.pat.Span(); got != sp {
			t.Errorf("%s: Span() = %v, want %v", tc.kind, got, sp)
		}
	}
}

func litPat(l *Lit) Pat { return &LitPat{Sp: l.Sp, Lit: l} }

func TestFieldPatPunned(t *testing.T) {
	id := &Ident{Sp: sp, Name: "name"}
	punned := FieldPat{Sp: sp, Name: id}
	if !punned.Punned() {
		t.Error("{ name } field not reported punned")
	}
	bound := FieldPat{Sp: sp, Name: id, Pat: &VarPat{Sp: sp, Name: id}}
	if bound.Punned() {
		t.Error("{ name = pat } field reported punned")
	}
}

func TestCtorArgLabeled(t *testing.T) {
	a := CtorArg{Sp: sp, Label: &Ident{Sp: sp, Name: "to"}, Pat: &WildPat{Sp: sp}}
	if !a.Labeled() {
		t.Error("~to: pat not reported labeled")
	}
}
