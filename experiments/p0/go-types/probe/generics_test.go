package probe

import (
	"go/types"
	"testing"
)

// Q5: generics in Go 1.27 — how type parameters and their constraints
// appear through go/types, and whether the spec boundary
// (only `any` / `comparable` constraints, spec 8.5) is detectable.

// slices.Sort[S ~[]E, E cmp.Ordered]: the canonical example.
func TestSlicesSortShape(t *testing.T) {
	l := NewLoader()
	obj, err := l.Lookup("slices", "Sort")
	if err != nil {
		t.Fatal(err)
	}
	sig := obj.Type().(*types.Signature)

	tps := sig.TypeParams()
	if tps.Len() != 2 {
		t.Fatalf("slices.Sort: %d type params, want 2", tps.Len())
	}
	for i := 0; i < tps.Len(); i++ {
		tp := tps.At(i)
		c := tp.Constraint()
		t.Logf("  T%d %s constraint %v (node %T, class %s)",
			tp.Index(), tp.Obj().Name(),
			types.TypeString(c, qualifier), c, ClassifyConstraint(c))
	}
	// S's constraint ~[]E is a type-set constraint; E's cmp.Ordered
	// is a named interface with union type elements. Both land in
	// ConstraintTypeSet -> slices.Sort is NOT callable in v1.
	if IsCallableGeneric(sig, true) {
		t.Error("slices.Sort should be outside the strict v1 boundary")
	}
	if IsCallableGeneric(sig, false) {
		t.Error("slices.Sort should be outside even the liberal boundary")
	}
}

// How each constraint shape looks — from a checked source file so all
// variants appear: any, comparable, method-set interface, ~T union.
func TestConstraintClasses(t *testing.T) {
	l := NewLoader()
	src := `package p

type Stringish interface{ String() string }
type Num interface{ ~int | ~int64 }

func Ident[T any](v T) T              { return v }
func First[K comparable, V any](m map[K]V) (K, bool) { var k K; return k, true }
func Show[T Stringish](v T) string    { return v.String() }
func Sum[T ~int | ~int64](a, b T) T   { return a + b }
func SumNum[T Num](a, b T) T          { return a + b }
`
	pkg, _, err := l.CheckSource("example.com/p", "p.go", src)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]ConstraintClass{
		"Ident":  {ConstraintAny},
		"First":  {ConstraintComparable, ConstraintAny},
		"Show":   {ConstraintMethodSet},
		"Sum":    {ConstraintTypeSet},
		"SumNum": {ConstraintTypeSet},
	}
	for name, classes := range want {
		sig := pkg.Scope().Lookup(name).Type().(*types.Signature)
		tps := sig.TypeParams()
		if tps.Len() != len(classes) {
			t.Fatalf("%s: %d type params", name, tps.Len())
		}
		for i, wantClass := range classes {
			tp := tps.At(i)
			got := ClassifyConstraint(tp.Constraint())
			t.Logf("%s[%s]: constraint=%v class=%s strict-callable=%v liberal-callable=%v",
				name, tp.Obj().Name(), types.TypeString(tp.Constraint(), qualifier),
				got, IsCallableGeneric(sig, true), IsCallableGeneric(sig, false))
			if got != wantClass {
				t.Errorf("%s param %d: class %s, want %s", name, i, got, wantClass)
			}
		}
	}
	// The boundary check itself:
	if !IsCallableGeneric(pkg.Scope().Lookup("Ident").Type().(*types.Signature), true) {
		t.Error("Ident[T any] should be callable in v1")
	}
	if !IsCallableGeneric(pkg.Scope().Lookup("First").Type().(*types.Signature), true) {
		t.Error("First[K comparable, V any] should be callable in v1")
	}
	if IsCallableGeneric(pkg.Scope().Lookup("Show").Type().(*types.Signature), true) {
		t.Error("Show[T Stringish]: strict boundary excludes method-set constraints")
	}
	if !IsCallableGeneric(pkg.Scope().Lookup("Show").Type().(*types.Signature), false) {
		t.Error("Show[T Stringish]: liberal boundary allows method-set constraints")
	}
}

// Real stdlib examples of the supported subset:
// unique.Make[T comparable], weak.Make[T any] (Go 1.24+).
func TestStdlibCallableGenerics(t *testing.T) {
	l := NewLoader()
	for _, tc := range []struct {
		pkg, name string
		callable  bool
	}{
		{"unique", "Make", true},      // [T comparable]
		{"weak", "Make", true},        // [T any]
		{"slices", "Sort", false},     // [S ~[]E, E cmp.Ordered]
		{"slices", "Contains", false}, // [S ~[]E, E comparable] — ~[]E is a type set!
		{"maps", "All", false},        // [M ~map[K]V, K comparable, V any]
		{"cmp", "Or", true},           // [T comparable] — genuinely callable!
	} {
		sig := sigOf(t, l, tc.pkg, tc.name)
		got := IsCallableGeneric(sig, true)
		f := DeconstructSignature(sig)
		for _, tp := range f.TypeParam {
			t.Logf("%s.%s[%s]: %s class=%s", tc.pkg, tc.name, tp.Name, tp.Constraint, tp.ConstraintClass)
		}
		if got != tc.callable {
			t.Errorf("%s.%s: strict-callable=%v, want %v", tc.pkg, tc.name, got, tc.callable)
		}
	}
}

// types.Instantiate substitutes type arguments and (with validate=true)
// runs constraint checking — the mechanism Gom's checker would reuse
// for inference-checking instantiation.
func TestInstantiate(t *testing.T) {
	l := NewLoader()
	src := `package p

func First[K comparable, V any](m map[K]V) (K, bool) { var k K; return k, true }
`
	pkg, _, err := l.CheckSource("example.com/p", "p.go", src)
	if err != nil {
		t.Fatal(err)
	}
	gen := pkg.Scope().Lookup("First").Type()

	// First[string, int] — ok.
	inst, err := types.Instantiate(nil, gen,
		[]types.Type{types.Typ[types.String], types.Typ[types.Int]}, true)
	if err != nil {
		t.Fatalf("Instantiate(First[string,int]): %v", err)
	}
	t.Logf("First[string,int] => %v", types.TypeString(inst, qualifier))

	// First[[]byte, int] — []byte is not comparable: constraint failure.
	_, err = types.Instantiate(nil, gen,
		[]types.Type{types.NewSlice(types.Typ[types.Byte]), types.Typ[types.Int]}, true)
	if err == nil {
		t.Fatal("Instantiate(First[[]byte,int]) should fail: []byte is not comparable")
	}
	t.Logf("First[[]byte,int] => error: %v", err)

	// K can be a named type too: time.Duration is comparable.
	dur, _ := l.Lookup("time", "Duration")
	inst2, err := types.Instantiate(nil, gen,
		[]types.Type{dur.Type(), types.Typ[types.Int]}, true)
	if err != nil {
		t.Fatalf("Instantiate(First[Duration,int]): %v", err)
	}
	t.Logf("First[Duration,int] => %v", types.TypeString(inst2, qualifier))
}

// Generic named types: Named.TypeParams / TypeArgs / Origin.
func TestGenericNamedType(t *testing.T) {
	l := NewLoader()
	src := `package p

type Pair[A, B any] struct {
	Fst A
	Snd B
}
`
	pkg, _, err := l.CheckSource("example.com/p", "p.go", src)
	if err != nil {
		t.Fatal(err)
	}
	named := pkg.Scope().Lookup("Pair").Type().(*types.Named)
	if named.TypeParams().Len() != 2 {
		t.Fatalf("Pair: %d type params", named.TypeParams().Len())
	}
	if named.TypeArgs() != nil {
		t.Fatal("generic Pair has no type args until instantiated")
	}
	inst, err := types.Instantiate(nil, named,
		[]types.Type{types.Typ[types.Int], types.Typ[types.String]}, true)
	if err != nil {
		t.Fatal(err)
	}
	in := inst.(*types.Named)
	if in.TypeArgs().Len() != 2 || in.Origin() != named {
		t.Fatal("instantiated Pair should carry TypeArgs and Origin")
	}
	t.Logf("Pair[int,string]: origin=%v args=%v",
		in.Origin().Obj().Name(), in.TypeArgs())
}
