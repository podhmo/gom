package treedump

import (
	"testing"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// sp and id build source positions and identifiers for hand-built
// trees — the file name is fixed so `@start:end` atoms stay readable.
func sp(a, b int) diag.Span               { return diag.SpanIn("t.gom", a, b) }
func id(a, b int, n string) *syntax.Ident { return &syntax.Ident{Sp: sp(a, b), Name: n} }

// tn builds an unqualified *syntax.TypeName — the common case in
// annotation positions.
func tn(a, b int, n string) *syntax.TypeName {
	return &syntax.TypeName{Sp: sp(a, b), Name: id(a, b, n)}
}

func TestDumpType(t *testing.T) {
	for _, tc := range []struct {
		ty   types.Type
		want string
	}{
		{types.Int, "Int"},
		{types.List(types.Int), "(List Int)"},
		{types.Result(types.Unit, &types.Con{Name: "TxError"}), "(Result Unit TxError)"},
		{types.Tup(types.Int, types.Bool), "((Int, Bool))"},
		{types.GoNamed("io", "Reader"), "io.Reader"},
		{types.GoMap(types.String, types.GoSlice(types.Int)), "(Go.Map String (Go.Slice Int))"},
		{types.Fn(types.Int, types.P(types.Int), types.LP("to", types.Int)),
			"(Int -> ~to: Int -> Int)"},
		{&types.Var{ID: 7}, "t7"},
		{&types.Var{ID: 1, Name: "a"}, "a"},
		{&types.Meta{ID: 3}, "?3"},
		{nil, "?"},
	} {
		if got := DumpType(tc.ty); got != tc.want {
			t.Errorf("DumpType(%v) = %q, want %q", tc.ty, got, tc.want)
		}
	}
}

func TestAtomEscaping(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"x", "x"},
		{"42", "42"},
		{"hi there", `"hi there"`},
		{"", `""`},
		{`a"b`, `"a\"b"`},
		{"(x)", `"(x)"`},
		{"a+b", "a+b"},
		{"日本語", "日本語"},
		{"line\nbreak", `"line\nbreak"`},
	} {
		if got := atom(tc.in); got != tc.want {
			t.Errorf("atom(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTypAnnotation(t *testing.T) {
	for _, tc := range []struct {
		ty   types.Type
		want string
	}{
		{types.Int, ":Int"},
		{types.List(types.Int), ":(List Int)"},
		{nil, ":?"},
	} {
		if got := typ(tc.ty); got != tc.want {
			t.Errorf("typ(%v) = %q, want %q", tc.ty, got, tc.want)
		}
	}
}
