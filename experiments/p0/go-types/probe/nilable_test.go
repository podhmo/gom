package probe

import (
	"go/types"
	"testing"
)

// Q6: which types can be nil? Two ways to answer:
//   - NilableByAssignability: types.AssignableTo(untyped nil, T) — the
//     authoritative check, same rule the Go spec defines.
//   - NilableByShape: a manual enumeration over the underlying type
//     (pointer, slice, map, chan, func/signature, interface,
//     unsafe.Pointer which is *types.Basic).
// They must agree on every case — including the named-type nuance:
// `type F func()` is nilable because nilability is a property of the
// UNDERLYING type, not of the nominal wrapper.

func TestNilableEnumeration(t *testing.T) {
	l := NewLoader()
	src := `package p

type F func() int
type S struct{ X int }
type Str string
type Ptr *int
type Sl []byte
type I interface{ M() }
`
	pkg, _, err := l.CheckSource("example.com/p", "p.go", src)
	if err != nil {
		t.Fatal(err)
	}
	get := func(name string) types.Type { return pkg.Scope().Lookup(name).Type() }

	reader, _ := l.Lookup("io", "Reader")
	dur, _ := l.Lookup("time", "Duration")
	str, _ := l.Lookup("strings", "Builder") // struct
	file, _ := l.Lookup("os", "File")        // struct
	hash, _ := l.Lookup("hash", "Hash")      // interface
	atoiSig := sigOf(t, l, "strconv", "Atoi")

	cases := []struct {
		label string
		typ   types.Type
		want  bool
	}{
		{"*int", types.NewPointer(types.Typ[types.Int]), true},
		{"[]byte", types.NewSlice(types.Typ[types.Byte]), true},
		{"map[string]int", types.NewMap(types.Typ[types.String], types.Typ[types.Int]), true},
		{"chan int", types.NewChan(types.SendRecv, types.Typ[types.Int]), true},
		{"func(string)(int,error)", atoiSig, true},
		{"interface{M()}", get("I").Underlying(), true},
		{"io.Reader (named interface)", reader.Type(), true},
		{"hash.Hash (named interface)", hash.Type(), true},
		{"unsafe.Pointer", types.Typ[types.UnsafePointer], true},
		{"error", ErrorType(), true},
		{"type F func() (named)", get("F"), true},
		{"type Ptr *int (named)", get("Ptr"), true},
		{"type Sl []byte (named)", get("Sl"), true},
		{"int", types.Typ[types.Int], false},
		{"string", types.Typ[types.String], false},
		{"bool", types.Typ[types.Bool], false},
		{"[3]int (array)", types.NewArray(types.Typ[types.Int], 3), false},
		{"struct{X int}", get("S").Underlying(), false},
		{"type S struct (named)", get("S"), false},
		{"type Str string (named)", get("Str"), false},
		{"time.Duration (named int64)", dur.Type(), false},
		{"strings.Builder (named struct)", str.Type(), false},
		{"os.File (named struct)", file.Type(), false},
	}
	for _, tc := range cases {
		got := NilableByAssignability(tc.typ)
		got2 := NilableByShape(tc.typ)
		if got != tc.want || got2 != tc.want {
			t.Errorf("%s: assignable=%v shape=%v, want %v",
				tc.label, got, got2, tc.want)
		}
		t.Logf("%-32v nilable=%v (type %T)", tc.label, got, tc.typ)
	}
}

// Type parameters are never nilable, even when every type in the
// constraint's type set could be — `var x T = nil` is illegal in Go.
func TestNilableTypeParam(t *testing.T) {
	l := NewLoader()
	src := `package p

func f[T any](v T) T { return v }
`
	pkg, _, err := l.CheckSource("example.com/p", "p.go", src)
	if err != nil {
		t.Fatal(err)
	}
	tp := pkg.Scope().Lookup("f").Type().(*types.Signature).TypeParams().At(0)
	if NilableByAssignability(tp) || NilableByShape(tp) {
		t.Error("type parameter should not be nilable")
	}
	t.Logf("type param T: nilable=%v — Gom's `a` is never nil", false)
}

// unsafe.Pointer is a *types.Basic, not a *types.Pointer — the one case
// where the manual enumeration must look at Kind().
func TestUnsafePointerIsBasic(t *testing.T) {
	up := types.Typ[types.UnsafePointer] // already *types.Basic
	if up.Kind() != types.UnsafePointer {
		t.Fatalf("unsafe.Pointer: got kind %v, want UnsafePointer", up.Kind())
	}
	t.Logf("unsafe.Pointer: *types.Basic kind=%v (also: unsupported by spec 8.5)", up.Kind())
}

// Go-derived values of nilable Go type are where spec 8.6's nil
// handling attaches: a `*T` return or an interface result may be nil,
// and Gom's `Ptr T` / nominal interface inherits that.
func TestNilableFromSignature(t *testing.T) {
	l := NewLoader()
	// os.Open: func(name string) (*File, error) — *File is nilable.
	open := sigOf(t, l, "os", "Open")
	if !NilableByAssignability(open.Results().At(0).Type()) {
		t.Error("*os.File result should be nilable")
	}
	// strconv.Itoa: func(int) string — string is not.
	itoa := sigOf(t, l, "strconv", "Itoa")
	if NilableByAssignability(itoa.Results().At(0).Type()) {
		t.Error("string result should not be nilable")
	}
}
