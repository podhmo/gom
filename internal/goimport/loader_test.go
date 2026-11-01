package goimport

import (
	"testing"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/resolve"
	gt "github.com/podhmo/gom/internal/types"
)

// loaderFor builds a Loader that resolves packages through the
// ambient toolchain (`go` on PATH, current directory as the module
// context). Standard-library packages work from anywhere; module
// packages need the repository module — tests run inside it.
func loaderFor(t *testing.T) *Loader {
	t.Helper()
	return New(nil)
}

// membersOf loads path and fails the test on error.
func membersOf(t *testing.T, l *Loader, path string) *resolve.Package {
	t.Helper()
	pkg, err := l.Load(path, true)
	if err != nil {
		t.Fatalf("Load(%q) failed: %v", path, err)
	}
	return pkg
}

func TestLoadStrconvMembers(t *testing.T) {
	l := loaderFor(t)
	pkg := membersOf(t, l, "strconv")
	for _, name := range []string{"Atoi", "Itoa", "ParseBool", "FormatInt"} {
		if pkg.Members[name] == nil {
			t.Errorf("member %s missing from strconv", name)
		}
	}
	atoi := pkg.Members["Atoi"]
	if atoi == nil {
		t.Fatal("Atoi missing")
	}
	if !atoi.IsGo || atoi.Package != "strconv" || atoi.Kind != resolve.KindFunc {
		t.Errorf("Atoi symbol = %+v, want an IsGo KindFunc of package strconv", atoi)
	}
	if atoi.Hidden != "" {
		t.Errorf("Atoi hidden: %s", atoi.Hidden)
	}

	sig, ok := l.SignatureOf(atoi)
	if !ok {
		t.Fatal("SignatureOf(Atoi) not found")
	}
	if got := sig.Type.String(); got != "String -> Result Int GoError" {
		t.Errorf("Atoi signature = %s, want `String -> Result Int GoError`", got)
	}

	gs, ok := l.GoSigOf(atoi)
	if !ok {
		t.Fatal("GoSigOf(Atoi) not found")
	}
	if gs.Arity != 1 || gs.Variadic || gs.Results != core.GoResultsErr {
		t.Errorf("Atoi GoSig = %+v, want arity 1 non-variadic (T,error)", gs)
	}
	// The raw result is Go's `int` while the Gom view says Int —
	// the `int`/`int64` conversion lowering must record.
	if len(gs.ResTypes) != 2 || !gt.Equal(gs.ResTypes[0], gt.GoInt) {
		t.Errorf("Atoi raw results = %v, want [Go.Int, GoError]", gs.ResTypes)
	}
	if len(gs.Params) != 1 || !gt.Equal(gs.Params[0], gt.String) {
		t.Errorf("Atoi raw params = %v, want [String]", gs.Params)
	}
}

func TestLoadItoaRawInt(t *testing.T) {
	l := loaderFor(t)
	pkg := membersOf(t, l, "strconv")
	itoa := pkg.Members["Itoa"]
	sig, ok := l.SignatureOf(itoa)
	if !ok {
		t.Fatal("SignatureOf(Itoa) not found")
	}
	if got := sig.Type.String(); got != "Int -> String" {
		t.Errorf("Itoa signature = %s, want `Int -> String`", got)
	}
	gs, _ := l.GoSigOf(itoa)
	if gs.Results != core.GoResultsOne || len(gs.Params) != 1 || !gt.Equal(gs.Params[0], gt.GoInt) {
		t.Errorf("Itoa GoSig params = %v, want [Go.Int]", gs.Params)
	}
}

// A variadic signature keeps the Go slice in both views; the arity
// counts the single declared (variadic) parameter.
func TestLoadSprintfVariadic(t *testing.T) {
	l := loaderFor(t)
	pkg := membersOf(t, l, "fmt")
	sprintf := pkg.Members["Sprintf"]
	if sprintf == nil {
		t.Skip("Sprintf not exported")
	}
	sig, ok := l.SignatureOf(sprintf)
	if !ok {
		t.Fatal("SignatureOf(Sprintf) not found")
	}
	if !sig.Variadic {
		t.Error("Sprintf signature must be variadic")
	}
	if got := sig.Type.String(); got != "String -> Go.Slice Go.Any -> String" {
		t.Errorf("Sprintf signature = %s, want `String -> Go.Slice Go.Any -> String`", got)
	}
	gs, _ := l.GoSigOf(sprintf)
	if !gs.Variadic || gs.Arity != 2 {
		t.Errorf("Sprintf GoSig = %+v, want arity 2 variadic", gs)
	}
}

// Members Gom cannot represent stay bound but Hidden — references
// produce E0501 with the reason.
func TestHiddenMembers(t *testing.T) {
	l := loaderFor(t)
	pkg := membersOf(t, l, "strconv")
	// AppendUint writes into a []byte — byte/uint8 is unmapped.
	au := pkg.Members["AppendUint"]
	if au == nil {
		t.Skip("AppendUint not exported")
	}
	if au.Hidden == "" {
		t.Errorf("AppendUint should be Hidden, got %+v", au)
	}
	if _, ok := l.SignatureOf(au); ok {
		t.Error("SignatureOf must not answer a hidden member")
	}
}

// An untyped int constant outside int64's range cannot back the
// `Int` (`int64`) the Gom view would claim — `math.MaxUint` is
// `1<<64 - 1`, and `int64(math.MaxUint)` does not compile — so the
// member stays bound but Hidden (E0501 at use sites). The in-range
// untyped int consts (`math.MaxInt`, `math.MinInt`, `strconv.IntSize`)
// stay exposed.
func TestOverflowIntConstHidden(t *testing.T) {
	l := loaderFor(t)
	mp := membersOf(t, l, "math")
	mx := mp.Members["MaxUint"]
	if mx == nil {
		t.Skip("MaxUint not exported")
	}
	if mx.Hidden == "" {
		t.Errorf("MaxUint should be Hidden, got %+v", mx)
	}
	if _, ok := l.SignatureOf(mx); ok {
		t.Error("SignatureOf must not answer a hidden member")
	}
	for _, name := range []string{"MaxInt", "MinInt"} {
		if s := mp.Members[name]; s == nil {
			t.Skip(name + " not exported")
		} else if s.Hidden != "" {
			t.Errorf("%s should stay exposed, hidden: %s", name, s.Hidden)
		}
	}
}

// A Go named type maps to a types.Go NamedRef with method lookup.
func TestLoadNamedTypeAndMethods(t *testing.T) {
	l := loaderFor(t)
	pkg := membersOf(t, l, "bytes")
	bufSym := pkg.Members["Buffer"]
	if bufSym == nil {
		t.Fatal("Buffer missing from bytes")
	}
	sig, ok := l.SignatureOf(bufSym)
	if !ok {
		t.Fatal("SignatureOf(Buffer) not found")
	}
	bufT, isGo := sig.Type.(*gt.Go)
	if !isGo || bufT.Ref.Path != "bytes" || bufT.Ref.Name != "Buffer" {
		t.Fatalf("Buffer type = %v, want bytes.Buffer handle", sig.Type)
	}
	msym, msig, ok := l.MethodOf(bufT, "WriteString")
	if !ok {
		t.Fatal("MethodOf(bytes.Buffer, WriteString) not found")
	}
	if msym.Kind != resolve.KindMethod || !msym.IsGo || msym.Package != "bytes" {
		t.Errorf("WriteString symbol = %+v, want an IsGo KindMethod of bytes", msym)
	}
	// (int, error) → Result Int GoError.
	if got := msig.Type.String(); got != "String -> Result Int GoError" {
		t.Errorf("WriteString signature = %s, want `String -> Result Int GoError`", got)
	}
	// The Go view keeps the raw `int` result.
	gs, ok := l.GoSigOf(msym)
	if !ok || gs.Results != core.GoResultsErr || !gt.Equal(gs.ResTypes[0], gt.GoInt) {
		t.Errorf("WriteString GoSig = %+v, want (T,error) with Go.Int payload", gs)
	}
	// Second lookup reuses the symbol.
	msym2, _, ok := l.MethodOf(bufT, "WriteString")
	if !ok || msym2 != msym {
		t.Error("MethodOf must intern method symbols per (package, type, name)")
	}
	// A pointer receiver method surfaces through a Ptr receiver.
	gtPtr := gt.Ptr(bufT)
	if _, msig2, ok := l.MethodOf(gtPtr, "Reset"); !ok {
		t.Error("MethodOf(*bytes.Buffer, Reset) not found")
	} else if got := msig2.Type.String(); got != " -> Unit" {
		t.Errorf("Reset signature = %s, want ` -> Unit`", got)
	}
}

// A Go const/var member maps like a value — math.Pi is an untyped
// float const that behaves as `float64`.
func TestLoadConst(t *testing.T) {
	l := loaderFor(t)
	pkg := membersOf(t, l, "math")
	pi := pkg.Members["Pi"]
	if pi == nil {
		t.Skip("Pi not exported")
	}
	sig, ok := l.SignatureOf(pi)
	if !ok {
		t.Fatal("SignatureOf(Pi) not found")
	}
	if !gt.Equal(sig.Type, gt.Float) {
		t.Errorf("Pi type = %s, want Float", sig.Type)
	}
}

// A Go package var or const answers SignatureOf with the Gom view
// and GoSigOf with the raw Go type — a zero-arity, single-result
// "signature" — so lowering can place the int/int64 conversion a
// reference needs (spec 8.1.5). Regression: a reference used to die
// on an internal E0901 in core validation.
func TestLoadPkgVars(t *testing.T) {
	l := loaderFor(t)
	os := membersOf(t, l, "os")

	// O_RDONLY is a typed `int` const: Int to Gom, Go.Int raw —
	// the reference must lower to `int64(os.O_RDONLY)`.
	rdonly := os.Members["O_RDONLY"]
	if rdonly == nil {
		t.Skip("O_RDONLY not exported")
	}
	if rdonly.Kind != resolve.KindVar || !rdonly.IsGo || rdonly.Package != "os" {
		t.Errorf("O_RDONLY symbol = %+v, want an IsGo KindVar of package os", rdonly)
	}
	sig, ok := l.SignatureOf(rdonly)
	if !ok || !gt.Equal(sig.Type, gt.Int) {
		t.Errorf("O_RDONLY signature = %v, want Int", sig)
	}
	gs, ok := l.GoSigOf(rdonly)
	if !ok || gs.Results != core.GoResultsOne || len(gs.ResTypes) != 1 || !gt.Equal(gs.ResTypes[0], gt.GoInt) {
		t.Errorf("O_RDONLY GoSig = %+v, want a single Go.Int result", gs)
	}

	// Args is a real var of type []string — the same type in both
	// views, so a reference lowers to the bare `os.Args`.
	args := os.Members["Args"]
	if args == nil {
		t.Skip("Args not exported")
	}
	sig, ok = l.SignatureOf(args)
	if !ok || sig.Type.String() != "Go.Slice String" {
		t.Errorf("Args signature = %v, want `Go.Slice String`", sig.Type)
	}
	gs, ok = l.GoSigOf(args)
	if !ok || len(gs.ResTypes) != 1 || !gt.Equal(gs.ResTypes[0], sig.Type) {
		t.Errorf("Args GoSig = %+v, want a single Go.Slice String result", gs)
	}

	// Untyped consts take their Go default type — and the untyped
	// int default is `int`, so strconv.IntSize splits the views
	// like a typed `int`: Int to Gom, Go.Int raw, the reference
	// lowered to `int64(strconv.IntSize)`.
	sp := membersOf(t, l, "strconv")
	isz := sp.Members["IntSize"]
	if isz == nil {
		t.Skip("IntSize not exported")
	}
	sig, ok = l.SignatureOf(isz)
	if !ok || !gt.Equal(sig.Type, gt.Int) {
		t.Errorf("IntSize signature = %v, want Int", sig.Type)
	}
	gs, ok = l.GoSigOf(isz)
	if !ok || len(gs.ResTypes) != 1 || !gt.Equal(gs.ResTypes[0], gt.GoInt) {
		t.Errorf("IntSize GoSig = %+v, want a single Go.Int result", gs)
	}

	// math.Pi is an untyped float const: Float in both views.
	mp := membersOf(t, l, "math")
	pi := mp.Members["Pi"]
	if pi == nil {
		t.Skip("Pi not exported")
	}
	gs, ok = l.GoSigOf(pi)
	if !ok || len(gs.ResTypes) != 1 || !gt.Equal(gs.ResTypes[0], gt.Float) {
		t.Errorf("Pi GoSig = %+v, want a single Float result", gs)
	}
}

// The universe `error` interface's method set answers a GoError
// receiver — the GoError builtin is the one Go-typed receiver that
// has no types.Named entry in l.named. `e.Error()` used to fail
// E0401 "GoError has no field or method".
func TestMethodOfGoError(t *testing.T) {
	l := loaderFor(t)
	msym, msig, ok := l.MethodOf(gt.GoError, "Error")
	if !ok {
		t.Fatal("MethodOf(GoError, Error) not found")
	}
	if msym.Kind != resolve.KindMethod || !msym.IsGo {
		t.Errorf("Error symbol = %+v, want an IsGo KindMethod", msym)
	}
	if got := msig.Type.String(); got != " -> String" {
		t.Errorf("Error signature = %s, want ` -> String`", got)
	}
	gs, ok := l.GoSigOf(msym)
	if !ok || gs.Arity != 0 || len(gs.ResTypes) != 1 || !gt.Equal(gs.ResTypes[0], gt.String) {
		t.Errorf("Error GoSig = %+v, want a single String result", gs)
	}
	// Second lookup reuses the interned symbol.
	msym2, _, ok := l.MethodOf(gt.GoError, "Error")
	if !ok || msym2 != msym {
		t.Error("MethodOf must intern method symbols")
	}
	// A `*error` receiver reaches the same interface method through
	// Go's implicit deref.
	if _, _, ok := l.MethodOf(gt.Ptr(gt.GoError), "Error"); !ok {
		t.Error("MethodOf(*GoError, Error) not found")
	}
	if _, _, ok := l.MethodOf(gt.GoError, "Nosuch"); ok {
		t.Error("MethodOf(GoError, Nosuch) must not answer")
	}
}

// A plain `import` never falls back to Go loading — E0203.
func TestLoadRefusesGomPackages(t *testing.T) {
	l := loaderFor(t)
	if _, err := l.Load("strconv", false); err == nil {
		t.Error("Load(strconv, goPkg=false) must fail")
	}
}
