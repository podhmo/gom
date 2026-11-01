package probe

import (
	"go/types"
	"testing"
)

// Q4: *types.Signature — param/result tuples, variadic flag,
// "(T, error)" detection, `any` params, method receivers.

func sigOf(t *testing.T, l *Loader, pkg, name string) *types.Signature {
	t.Helper()
	obj, err := l.Lookup(pkg, name)
	if err != nil {
		t.Fatal(err)
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		t.Fatalf("%s.%s: expected *types.Func, got %T", pkg, name, obj)
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		t.Fatalf("%s.%s: type is %T", pkg, name, fn.Type())
	}
	return sig
}

func logSig(t *testing.T, label string, sig *types.Signature) {
	f := DeconstructSignature(sig)
	t.Logf("%s: variadic=%v shape=%s", label, f.Variadic, ClassifyResults(sig))
	for i, p := range f.Params {
		t.Logf("  param[%d] %-8s %-14s node=%s error=%v any=%v nilable=%v",
			i, p.Name, p.Type, p.Concrete, p.IsError, p.IsAny, p.Nilable)
	}
	for i, p := range f.Results {
		t.Logf("  result[%d] %-8s %-14s node=%s error=%v any=%v nilable=%v",
			i, p.Name, p.Type, p.Concrete, p.IsError, p.IsAny, p.Nilable)
	}
}

func TestSignatureProbing(t *testing.T) {
	l := NewLoader()
	for _, tc := range []struct {
		pkg, name string
		shape     ResultShape
	}{
		{"strconv", "Atoi", ResultValueErr},          // func(string) (int, error)
		{"os", "ReadFile", ResultValueErr},           // func(string) ([]byte, error)
		{"encoding/json", "Marshal", ResultValueErr}, // func(any) ([]byte, error)
		{"fmt", "Sprintf", ResultValue},              // func(string, ...any) string
		{"fmt", "Println", ResultValueErr},           // func(...any) (int, error)
		{"os", "LookupEnv", ResultValueBool},         // func(string) (string, bool)
		{"io", "Copy", ResultValueErr},               // func(Writer, Reader) (int64, error)
		{"time", "Now", ResultValue},                 // func() Time — one result
		{"strings", "NewReplacer", ResultValue},      // variadic ...string -> *Replacer
	} {
		sig := sigOf(t, l, tc.pkg, tc.name)
		logSig(t, tc.pkg+"."+tc.name, sig)
		if got := ClassifyResults(sig); got != tc.shape {
			t.Errorf("%s.%s: result shape %s, want %s", tc.pkg, tc.name, got, tc.shape)
		}
	}
}

// strconv.Atoi — the canonical (T, error) shape.
func TestAtoiIsResultValueErr(t *testing.T) {
	l := NewLoader()
	sig := sigOf(t, l, "strconv", "Atoi")
	if sig.Recv() != nil {
		t.Error("Atoi is a plain function, recv should be nil")
	}
	if sig.Params().Len() != 1 || sig.Results().Len() != 2 {
		t.Fatalf("Atoi shape: %d params, %d results", sig.Params().Len(), sig.Results().Len())
	}
	if !LastResultIsError(sig) {
		t.Fatal("Atoi's last result must be exactly `error`")
	}
	// Param name is preserved ("s") — usable for generated docs.
	if sig.Params().At(0).Name() == "" {
		t.Log("note: param names unavailable for strconv.Atoi")
	}
}

// json.Marshal's `any` parameter: detect "this position takes anything"
// for the spec 8.1.7 boxing rule. NOTE: go1.27.1 export data delivers
// `v any` as *types.Alias — while a handwritten `interface{}` param
// arrives as plain *types.Interface. IsAny must therefore be a
// STRUCTURAL check (Unalias → empty interface), which it is.
func TestAnyParamDetection(t *testing.T) {
	l := NewLoader()
	sig := sigOf(t, l, "encoding/json", "Marshal")
	p := sig.Params().At(0).Type()
	if _, ok := p.(*types.Alias); !ok {
		t.Fatalf("json.Marshal param node: got %T, want *types.Alias (export data preserves `any`)", p)
	}
	if !IsAny(p) {
		t.Fatalf("json.Marshal param type %v should be `any`", p)
	}
	t.Logf("json.Marshal param[0]: %v (raw node %T, unaliased %T)", p, p, types.Unalias(p))

	// The other spelling: a source-declared `interface{}` param arrives
	// as *types.Interface — the same structural check must catch it.
	src := `package p

func g(v interface{}) {}
`
	pkg, _, err := l.CheckSource("example.com/p", "p.go", src)
	if err != nil {
		t.Fatal(err)
	}
	gp := pkg.Scope().Lookup("g").Type().(*types.Signature).Params().At(0).Type()
	t.Logf("interface{} param: raw node %T", gp)
	if _, ok := gp.(*types.Interface); !ok {
		t.Logf("note: interface{} param arrived as %T", gp)
	}
	if !IsAny(gp) {
		t.Error("interface{} param should also be detected as `any`")
	}
}

// Variadic: Variadic() flag + last param is a *types.Slice whose Elem()
// is the per-argument type.
func TestVariadicSignature(t *testing.T) {
	l := NewLoader()
	sig := sigOf(t, l, "fmt", "Sprintf")
	if !sig.Variadic() {
		t.Fatal("fmt.Sprintf must be variadic")
	}
	n := sig.Params().Len()
	last := sig.Params().At(n - 1).Type()
	slice, ok := last.(*types.Slice)
	if !ok {
		t.Fatalf("variadic last param should be *types.Slice, got %T", last)
	}
	if !IsAny(slice.Elem()) {
		t.Fatalf("Sprintf variadic elem should be any, got %v", slice.Elem())
	}
	t.Logf("fmt.Sprintf: ...%v (slice %v)", slice.Elem(), slice)

	// Non-variadic last param is not a slice marker:
	sig2 := sigOf(t, l, "strings", "NewReplacer")
	if !sig2.Variadic() {
		t.Fatal("strings.NewReplacer must be variadic")
	}
	last2 := sig2.Params().At(0).Type().(*types.Slice)
	t.Logf("strings.NewReplacer: func(...%v) *Replacer", last2.Elem())
}

// (T, bool) stays a tuple per the boundary decision; only `error` in
// last position triggers Result conversion.
func TestCommaOkDetection(t *testing.T) {
	l := NewLoader()
	sig := sigOf(t, l, "os", "LookupEnv")
	if LastResultIsError(sig) {
		t.Fatal("LookupEnv must NOT be classified as (T, error)")
	}
	if ClassifyResults(sig) != ResultValueBool {
		t.Fatal("LookupEnv should be (T, bool) — comma-ok candidate")
	}
}

// Methods: the receiver lives on the *types.Signature as Recv().
// buf.WriteString is reached through the named type's method table.
func TestMethodSignature(t *testing.T) {
	l := NewLoader()
	bufObj, err := l.Lookup("bytes", "Buffer")
	if err != nil {
		t.Fatal(err)
	}
	named := bufObj.Type().(*types.Named)
	var writeString *types.Func
	for i := 0; i < named.NumMethods(); i++ {
		if named.Method(i).Name() == "WriteString" {
			writeString = named.Method(i)
		}
	}
	if writeString == nil {
		t.Fatal("bytes.Buffer.WriteString not found")
	}
	sig := writeString.Type().(*types.Signature)
	if sig.Recv() == nil {
		t.Fatal("method signature should carry a receiver")
	}
	t.Logf("Buffer.WriteString: recv=%v sig=%v shape=%s",
		sig.Recv().Type(), types.TypeString(sig, qualifier), ClassifyResults(sig))
	// WriteString returns (int, error) -> Result Int GoError.
	if !LastResultIsError(sig) {
		t.Error("WriteString should be (T, error)")
	}
}

// Signature as a nilable type: `func` types are nilable, which matters
// for Q6 and for Go-derived function values.
func TestSignatureNilable(t *testing.T) {
	l := NewLoader()
	atoi := sigOf(t, l, "strconv", "Atoi")
	if !NilableByAssignability(atoi) || !NilableByShape(atoi) {
		t.Error("function type should be nilable")
	}
}
