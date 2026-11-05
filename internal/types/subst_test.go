package types

import "testing"

func TestSubstSharesUnchangedBranches(t *testing.T) {
	a := &Var{ID: 1, Name: "a"}
	ground := List(Int)
	original := &Con{Qual: "example", Name: "Box", Args: []Type{ground, Tup(a)}}
	unchanged := Subst{2: String}.Apply(original)
	if unchanged != original {
		t.Fatal("unaffected substitution rebuilt the type")
	}
	rewritten := Subst{1: String}.Apply(original).(*Con)
	if !Equal(rewritten, &Con{Qual: "example", Name: "Box", Args: []Type{ground, Tup(String)}}) {
		t.Fatalf("rewritten = %s", rewritten)
	}
	if rewritten.Args[0] != ground {
		t.Fatal("unaffected sibling was rebuilt")
	}
	if !Equal(original.Args[1], Tup(a)) {
		t.Fatal("substitution mutated the original argument slice")
	}
}

func TestSubstStillChasesNestedMetas(t *testing.T) {
	m := &Meta{ID: 1}
	for _, wrap := range []func(Type) Type{
		List,
		func(t Type) Type { return Tup(t, Int) },
		func(t Type) Type { return &Go{Args: []Type{t}} },
	} {
		original := wrap(m)
		if Subst(nil).Apply(original) != original {
			t.Fatal("unbound meta caused a rebuild")
		}
	}
	m.Bind(Int)
	for _, wrap := range []func(Type) Type{
		List,
		func(t Type) Type { return Tup(t, Int) },
		func(t Type) Type { return &Go{Args: []Type{t}} },
	} {
		original := wrap(m)
		got := Subst(nil).Apply(original)
		if got == original || !Equal(got, wrap(Int)) {
			t.Fatalf("empty substitution did not chase meta: %s", got)
		}
	}
}

func BenchmarkSubstUnchanged(b *testing.B) {
	ground := Type(Int)
	for range 16 {
		ground = List(ground)
	}
	sub := Subst{999: String}
	b.ReportAllocs()
	for b.Loop() {
		if sub.Apply(ground) != ground {
			b.Fatal("unchanged type was rebuilt")
		}
	}
}
