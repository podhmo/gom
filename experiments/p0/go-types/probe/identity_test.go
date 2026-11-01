package probe

import (
	"go/importer"
	"go/types"
	"testing"
)

// Q3: is the same nominal type obtained via different paths the same
// object? design.md 2.5 requires go/types identity to be preserved.

// io.Reader reached three ways through ONE loader is the same object.
func TestIdentityWithinOneLoader(t *testing.T) {
	l := NewLoader()

	viaScope, err := l.Lookup("io", "Reader")
	if err != nil {
		t.Fatal(err)
	}

	// Via io.Copy's second parameter: func Copy(dst Writer, src Reader).
	copyObj, err := l.Lookup("io", "Copy")
	if err != nil {
		t.Fatal(err)
	}
	copySig := copyObj.Type().(*types.Signature)
	viaParam := copySig.Params().At(1).Type()

	// Via io.ReadWriter's embedded types.
	rwObj, err := l.Lookup("io", "ReadWriter")
	if err != nil {
		t.Fatal(err)
	}
	rwIface := types.Unalias(rwObj.Type()).Underlying().(*types.Interface)
	viaEmbed := rwIface.EmbeddedType(0)

	r1 := viaScope.Type()
	if r1 != viaParam || r1 != viaEmbed {
		t.Fatalf("io.Reader via scope/param/embed differ: %p %p %p", r1, viaParam, viaEmbed)
	}
	if !types.Identical(r1, viaParam) || !types.Identical(r1, viaEmbed) {
		t.Fatal("types.Identical should hold for the same *types.Named")
	}
	t.Logf("io.Reader: same *types.Named pointer via scope, io.Copy param, ReadWriter embed")

	// An alias declared in source resolves to the very same object when
	// the source is checked through the same importer.
	src := `package p

import "io"

type R = io.Reader
`
	pkg, _, err := l.CheckSource("example.com/p", "p.go", src)
	if err != nil {
		t.Fatal(err)
	}
	viaAlias := pkg.Scope().Lookup("R").Type()
	if types.Unalias(viaAlias) != types.Unalias(r1) {
		t.Fatal("alias R should unwrap to the same io.Reader object")
	}
	t.Logf("type R = io.Reader: Unalias -> same *types.Named")
}

// Two independent importers produce DISTINCT objects for the same
// nominal type — and types.Identical is false, because nominal
// identity compares the *types.TypeName object pointers.
// Consequence: the whole compilation must share ONE loading session.
func TestIdentityAcrossLoaders(t *testing.T) {
	l1, l2 := NewLoader(), NewLoader()
	r1, err := l1.Lookup("io", "Reader")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := l2.Lookup("io", "Reader")
	if err != nil {
		t.Fatal(err)
	}
	if r1.Type() == r2.Type() {
		t.Fatal("two loaders returned the same *types.Named?")
	}
	if types.Identical(r1.Type(), r2.Type()) {
		t.Fatal("io.Reader from two importers must not be Identical (nominal identity)")
	}
	t.Logf("two Loader instances: io.Reader are different objects AND non-Identical")

	// Raw importer.Default() behaves the same: each call is a fresh
	// importer with its own package map.
	i1, i2 := importer.Default(), importer.Default()
	p1, _ := i1.Import("io")
	p2, _ := i2.Import("io")
	if p1.Scope().Lookup("Reader").Type() == p2.Scope().Lookup("Reader").Type() {
		t.Fatal("importer.Default() calls should not share package objects")
	}

	// Structural types ARE identical across importers — only nominal
	// (named) types break. So Gom can safely key its Go-type cache on
	// the types.Type value itself within one session.
	s1 := types.NewSlice(types.Typ[types.Byte])
	s2, _, err := l2.CheckSource("example.com/q", "q.go", "package q\n\ntype B []byte\n")
	if err != nil {
		t.Fatal(err)
	}
	bt := s2.Scope().Lookup("B").Type().Underlying()
	if !types.Identical(s1, bt) {
		t.Error("[]byte should be Identical across importers (structural identity)")
	}
}

// The predeclared error type is a universe singleton: results typed
// `error` from any package are the same *types.Named.
func TestErrorIsSingleton(t *testing.T) {
	l := NewLoader()
	atoi, err := l.Lookup("strconv", "Atoi")
	if err != nil {
		t.Fatal(err)
	}
	errResult := atoi.Type().(*types.Signature).Results().At(1).Type()
	if errResult != ErrorType() {
		t.Fatal("strconv.Atoi's error result is not the universe error object")
	}
	t.Logf("error result of strconv.Atoi: %p == universe error %p", errResult, ErrorType())
}

// Can types.Type be used as a map key? All concrete node types are
// pointers, so identity-keyed caching is possible — the natural
// representation for internal/types' "Go nominal type" table.
func TestTypeAsMapKey(t *testing.T) {
	l := NewLoader()
	m := map[types.Type]string{}
	r, _ := l.Lookup("io", "Reader")
	m[r.Type()] = "io.Reader"
	copyObj, _ := l.Lookup("io", "Copy")
	param := copyObj.Type().(*types.Signature).Params().At(1).Type()
	if m[param] != "io.Reader" {
		t.Fatal("map lookup by types.Type identity failed")
	}
}
