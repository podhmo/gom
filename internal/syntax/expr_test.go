package syntax

import (
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

var sp = diag.SpanIn("test.gom", 10, 20)

// exprKind names each Expr variant — an example of the type switch
// every consumer writes, with the required unreachable default.
func exprKind(e Expr) string {
	switch e.(type) {
	case *Ident:
		return "ident"
	case *Lit:
		return "lit"
	case *StrInterp:
		return "strinterp"
	case *Apply:
		return "apply"
	case *Select:
		return "select"
	case *Unary:
		return "unary"
	case *Binary:
		return "binary"
	case *Hole:
		return "hole"
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

func TestExprKindsAndSpans(t *testing.T) {
	id := &Ident{Sp: sp, Name: "x"}
	exprs := []struct {
		expr Expr
		kind string
	}{
		{id, "ident"},
		{&Lit{Sp: sp, Kind: LitInt, Text: "42"}, "lit"},
		{&StrInterp{Sp: sp, Parts: []StrPart{
			&StrText{Sp: sp, Text: "a="},
			&StrExpr{Sp: sp, X: id},
		}}, "strinterp"},
		{&Apply{Sp: sp, Fn: id, Args: []Arg{{Sp: sp, Value: id}}}, "apply"},
		{&Select{Sp: sp, Recv: id, Sel: &Ident{Sp: sp, Name: "f"}}, "select"},
		{&Unary{Sp: sp, Op: UnNeg, X: id}, "unary"},
		{&Binary{Sp: sp, Op: BinAdd, X: id, Y: id}, "binary"},
		{&Hole{Sp: sp}, "hole"},
		{&Lambda{Sp: sp, Params: []Param{{Sp: sp, Pat: &VarPat{Sp: sp, Name: id}}}, Body: id}, "lambda"},
		{&If{Sp: sp, Cond: id, Then: id, Else: id}, "if"},
		{&Match{Sp: sp, Subj: id, Arms: []MatchArm{{Sp: sp, Pat: &WildPat{Sp: sp}, Body: id}}}, "match"},
		{&Block{Sp: sp, Stmts: []Expr{id}}, "block"},
		{&Let{Sp: sp, Pat: &VarPat{Sp: sp, Name: id}, Value: id}, "let"},
		{&Ignore{Sp: sp, X: id}, "ignore"},
		{&Try{Sp: sp, X: id}, "try"},
		{&GoExpr{Sp: sp, Body: &Block{Sp: sp}}, "go"},
		{&For{Sp: sp, Pat: &VarPat{Sp: sp, Name: id}, Iter: id, Body: &Block{Sp: sp}}, "for"},
		{&SelectExpr{Sp: sp, Arms: []SelectArm{&DefaultArm{Sp: sp, Body: id}}}, "selectexpr"},
		{&ListLit{Sp: sp, Elems: []Expr{id, &Spread{Sp: sp, X: id}}}, "list"},
		{&Spread{Sp: sp, X: id}, "spread"},
		{&TupleLit{Sp: sp, Elems: []Expr{id, id}}, "tuple"},
		{&RecordLit{Sp: sp, Fields: []FieldInit{{Sp: sp, Name: id, Value: id}}}, "record"},
		{&Ascribe{Sp: sp, X: &Apply{Sp: sp, Fn: &Select{Sp: sp, Recv: &Ident{Sp: sp, Name: "Go"},
			Sel: &Ident{Sp: sp, Name: "conv"}}, Args: []Arg{{Sp: sp, Value: id}}},
			Type: &TypeName{Sp: sp, Name: &Ident{Sp: sp, Name: "Int"}}}, "ascribe"},
	}
	for _, tc := range exprs {
		if got := exprKind(tc.expr); got != tc.kind {
			t.Errorf("exprKind = %q, want %q", got, tc.kind)
		}
		if got := tc.expr.Span(); got != sp {
			t.Errorf("%s: Span() = %v, want %v", tc.kind, got, sp)
		}
	}
}

func TestArgLabeled(t *testing.T) {
	positional := Arg{Sp: sp, Value: &Ident{Sp: sp, Name: "x"}}
	if positional.Labeled() {
		t.Error("positional arg reported labeled")
	}
	labeled := Arg{Sp: sp, Label: &Ident{Sp: sp, Name: "to"}, Value: &Ident{Sp: sp, Name: "y"}}
	if !labeled.Labeled() {
		t.Error("labeled arg reported unlabeled")
	}
	punned := Arg{Sp: sp, Label: &Ident{Sp: sp, Name: "to"}, Value: &Ident{Sp: sp, Name: "to"}, Punned: true}
	if !punned.Labeled() || !punned.Punned {
		t.Error("punned arg lost its markers")
	}
}

func TestBinOp(t *testing.T) {
	spellings := map[BinOp]string{
		BinAdd:         "+",
		BinPipeFwd:     "|>",
		BinPipeBack:    "<|",
		BinPipeTry:     "|?>",
		BinComposeFwd:  ">>",
		BinComposeBack: "<<",
	}
	for op, want := range spellings {
		if got := op.String(); got != want {
			t.Errorf("BinOp(%d).String() = %q, want %q", int(op), got, want)
		}
	}
	for _, op := range []BinOp{BinPipeFwd, BinPipeBack, BinPipeTry, BinComposeFwd, BinComposeBack} {
		if !op.Sugared() {
			t.Errorf("%s not reported sugared", op)
		}
	}
	for _, op := range []BinOp{BinAdd, BinEq, BinAnd} {
		if op.Sugared() {
			t.Errorf("%s reported sugared", op)
		}
	}
}

func TestSelectArmsAndStrPartsAreSealed(t *testing.T) {
	// Type assertions, not switches: a partial type switch over a
	// sealed interface fails the exhaustiveness check
	// (scripts/check-sumtype.sh); an assertion is outside its scope.
	var arm SelectArm = &RecvArm{Sp: sp, Bind: &VarPat{Sp: sp, Name: &Ident{Sp: sp, Name: "x"}}}
	if _, ok := arm.(*RecvArm); !ok {
		t.Fatalf("RecvArm is not a SelectArm")
	}
	var part StrPart = &StrExpr{Sp: sp, X: &Ident{Sp: sp, Name: "x"}}
	if _, ok := part.(*StrExpr); !ok {
		t.Fatalf("StrExpr is not a StrPart")
	}
}
