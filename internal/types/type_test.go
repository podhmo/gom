package types

import (
	"testing"
)

// kind names each Type variant — the sealed-interface type switch
// with the required unreachable default.
func kind(t Type) string {
	switch t.(type) {
	case *Con:
		return "con"
	case *Var:
		return "var"
	case *Meta:
		return "meta"
	case *Func:
		return "func"
	case *Tuple:
		return "tuple"
	case *Go:
		return "go"
	default:
		panic("unreachable")
	}
}

func TestTypeVariants(t *testing.T) {
	vars := []struct {
		typ  Type
		kind string
	}{
		{Int, "con"},
		{List(Int), "con"},
		{&Var{ID: 1, Name: "a"}, "var"},
		{&Meta{ID: 2}, "meta"},
		{Fn(Int, P(Int)), "func"},
		{Tup(Int, Bool), "tuple"},
		{GoNamed("io", "Reader"), "go"},
	}
	for _, tc := range vars {
		if got := kind(tc.typ); got != tc.kind {
			t.Errorf("kind = %q, want %q", got, tc.kind)
		}
	}
}

func TestStringForms(t *testing.T) {
	cases := []struct {
		typ  Type
		want string
	}{
		{Int, "Int"},
		{List(Int), "List Int"},
		{Result(Int, GoError), "Result Int GoError"},
		{Option(List(Int)), "Option (List Int)"},
		{&Con{Qual: "io", Name: "Option", Args: []Type{Int}}, "io.Option Int"},
		{Tup(Int, String), "(Int, String)"},
		{Fn(Int, P(Int), P(Bool)), "Int -> Bool -> Int"},
		{Fn(Int, P(Int), LP("to", String)), "Int -> ~to: String -> Int"},
		{Fn(Int, P(Fn(Int, P(Int)))), "(Int -> Int) -> Int"},
		{List(Fn(Int, P(Int))), "List (Int -> Int)"},
		{List(Tup(Int, Bool)), "List (Int, Bool)"},
		{&Var{ID: 1, Name: "a"}, "a"},
		{&Var{ID: 7}, "t7"},
		{GoNamed("io", "Reader"), "io.Reader"},
		{&Go{Ref: NamedRef{Path: "pkg", Name: "Box"}, Args: []Type{Int}}, "pkg.Box[Int]"},
		{GoMap(String, Int), "Go.Map String Int"},
	}
	for _, tc := range cases {
		if got := tc.typ.String(); got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
	}
}

func TestEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b Type
		want bool
	}{
		{"same builtin", Int, &Con{Name: "Int"}, true},
		{"different builtin", Int, String, false},
		{"applied equal", List(Int), List(&Con{Name: "Int"}), true},
		{"applied unequal", List(Int), List(Bool), false},
		{"arity differs", List(Int), Result(Int, Int), false},
		{"var id", &Var{ID: 1, Name: "a"}, &Var{ID: 1, Name: "b"}, true},
		{"var id differs", &Var{ID: 1, Name: "a"}, &Var{ID: 2, Name: "a"}, false},
		{"tuple", Tup(Int, Bool), Tup(Int, Bool), true},
		{"tuple differs", Tup(Int, Bool), Tup(Bool, Int), false},
		{"go ref equal", GoNamed("io", "Reader"), GoNamed("io", "Reader"), true},
		{"go ref differs", GoNamed("io", "Reader"), GoNamed("io", "Writer"), false},
		{"gom con vs go named", &Con{Qual: "io", Name: "Reader"}, GoNamed("io", "Reader"), false},
		{"func equal", Fn(Int, P(Int)), Fn(Int, P(Int)), true},
		{"func ret differs", Fn(Int, P(Int)), Fn(Bool, P(Int)), false},
		// `A -> B -> R` and `A -> (B -> R)` are the same curried type
		{"flat vs nested func",
			Fn(Int, P(Int), P(Int)),
			&Func{Params: []FuncParam{P(Int)}, Ret: Fn(Int, P(Int))},
			true},
		{"nested ret differs",
			Fn(Int, P(Int), P(Int)),
			&Func{Params: []FuncParam{P(Int)}, Ret: Fn(Bool, P(Int))},
			false},
		// spec 2.1: consecutive labeled params compare up to reordering
		{
			"labeled run reordered",
			Fn(Int, LP("a", Int), LP("b", Bool)),
			Fn(Int, LP("b", Bool), LP("a", Int)),
			true,
		},
		{
			"label names differ",
			Fn(Int, LP("a", Int), LP("b", Bool)),
			Fn(Int, LP("a", Bool), LP("b", Int)),
			false,
		},
		{
			// runs separated by a positional parameter do not merge
			"non-consecutive labels stay ordered",
			Fn(Int, LP("a", Int), P(Bool), LP("b", Int)),
			Fn(Int, LP("b", Int), P(Bool), LP("a", Int)),
			false,
		},
		{"positional vs labeled", Fn(Int, P(Int)), Fn(Int, LP("x", Int)), false},
	}
	for _, tc := range cases {
		if got := Equal(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: Equal(%s, %s) = %v, want %v",
				tc.name, tc.a, tc.b, got, tc.want)
		}
	}
}

func TestFnFlattensCurriedReturn(t *testing.T) {
	// Fn is the canonical constructor: Ret is never a *Func.
	got := Fn(Fn(Int, P(Int)), P(Int))
	if _, nested := got.Ret.(*Func); nested {
		t.Fatalf("Fn produced nested Ret: %#v", got)
	}
	if got.Arity() != 2 {
		t.Errorf("flattened arity = %d, want 2", got.Arity())
	}
	// labels ride along with the absorbed parameters
	labeled := Fn(Fn(Int, LP("b", Bool)), P(Int))
	if labeled.Arity() != 2 || labeled.Params[1].Label != "b" {
		t.Errorf("labeled flatten = %#v", labeled.Params)
	}
	// a bound meta in tail position still flattens for Equal
	flat := Fn(Int, P(Int), P(Int))
	m := &Meta{ID: 1}
	m.Bind(Fn(Int, P(Int)))
	withMeta := &Func{Params: []FuncParam{P(Int)}, Ret: m}
	if !Equal(withMeta, flat) {
		t.Error("meta-bound nested tail not Equal to flat form")
	}
	// substitution keeps the canonical form when Ret instantiates
	// to a function type
	res := Subst{2: Fn(Int, P(Int))}.Apply(Fn(&Var{ID: 2}, P(Int)))
	f, ok := res.(*Func)
	if !ok || f.Arity() != 2 {
		t.Fatalf("Subst.Apply = %s, want a flat 2-ary func", res)
	}
	if _, nested := f.Ret.(*Func); nested {
		t.Errorf("Subst.Apply produced nested Ret: %#v", f)
	}
	if !Equal(res, flat) {
		t.Errorf("Subst.Apply = %s, want %s", res, flat)
	}
}

func TestMetaDoubleBindPanics(t *testing.T) {
	m := &Meta{ID: 1}
	m.Bind(Int)
	defer func() {
		if recover() == nil {
			t.Error("second Bind did not panic")
		}
	}()
	m.Bind(Int)
}

func TestMetaChaseInEqualAndString(t *testing.T) {
	m := &Meta{ID: 1}
	if m.Bound() {
		t.Error("fresh meta bound")
	}
	if got := m.String(); got != "?1" {
		t.Errorf("unbound meta String = %q, want ?1", got)
	}
	m.Bind(List(Int))
	if !m.Bound() || m.Solution() == nil {
		t.Error("bound meta has no solution")
	}
	if got := m.String(); got != "List Int" {
		t.Errorf("bound meta String = %q, want List Int", got)
	}
	if !Equal(m, List(Int)) {
		t.Error("bound meta not equal to its solution")
	}
	other := &Meta{ID: 2}
	if Equal(m, other) {
		t.Error("bound meta equal to unbound meta")
	}
}

func TestSubst(t *testing.T) {
	a := &Var{ID: 1, Name: "a"}
	b := &Var{ID: 2, Name: "b"}
	fun := Fn(Result(a, b), P(a), LP("e", b))
	sub := Subst{1: Int, 2: GoError}
	got := sub.Apply(fun)
	want := Fn(Result(Int, GoError), P(Int), LP("e", GoError))
	if !Equal(got, want) {
		t.Errorf("Apply = %s, want %s", got, want)
	}
	// untouched vars stay
	got2 := Subst{}.Apply(fun)
	if !Equal(got2, fun) {
		t.Errorf("empty subst changed %s", got2)
	}
	// substitution rewrites inside tuples and cons
	partial := Subst{1: String}
	if got := partial.Apply(Tup(a, b)); !Equal(got, Tup(String, b)) {
		t.Errorf("partial Apply = %s", got)
	}
	// and inside bound metas
	m := &Meta{ID: 9}
	m.Bind(List(a))
	if got := partial.Apply(m); !Equal(got, List(String)) {
		t.Errorf("meta Apply = %s", got)
	}
}

func TestFreeVars(t *testing.T) {
	a := &Var{ID: 1, Name: "a"}
	b := &Var{ID: 2, Name: "b"}
	fun := Fn(Result(a, Int), P(a), LP("e", b))
	fv := FreeVars(fun)
	if len(fv) != 2 || fv[1] != a || fv[2] != b {
		t.Errorf("FreeVars = %v", fv)
	}
	if got := FreeVars(Fn(Int, P(Int))); len(got) != 0 {
		t.Errorf("monomorphic type has free vars: %v", got)
	}
}
