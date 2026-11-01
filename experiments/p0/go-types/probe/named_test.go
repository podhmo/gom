package probe

import (
	"go/constant"
	"go/types"
	"testing"
)

// Q2: named types — how does go/types identity work for
// time.Duration (named int64), a named string type, and custom named
// types with methods? How is `type D int64` distinguished from
// `type A = int64` (alias)?

func TestNamedDuration(t *testing.T) {
	l := NewLoader()
	obj, err := l.Lookup("time", "Duration")
	if err != nil {
		t.Fatal(err)
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		t.Fatalf("time.Duration: expected *types.TypeName, got %T", obj)
	}
	if tn.IsAlias() {
		t.Fatal("time.Duration must NOT be an alias")
	}
	named, ok := tn.Type().(*types.Named)
	if !ok {
		t.Fatalf("time.Duration: expected *types.Named, got %T", tn.Type())
	}
	basic, ok := named.Underlying().(*types.Basic)
	if !ok || basic.Kind() != types.Int64 {
		t.Fatalf("Duration underlying = %v, want int64", named.Underlying())
	}
	t.Logf("time.Duration: *types.TypeName(obj) -> *types.Named, underlying %v, NumMethods=%d",
		named.Underlying(), named.NumMethods())

	// Identity: Named is NOT identical to its underlying type — this is
	// exactly why design.md 2.5 says keep go/types identity, not names.
	if types.Identical(named, basic) {
		t.Error("types.Identical(Duration, int64) = true; named types are nominal")
	}
	// But conversion is legal — the Go-side check behind Gom's Go.conv.
	if !types.ConvertibleTo(named, basic) {
		t.Error("types.ConvertibleTo(Duration, int64) = false")
	}

	// Methods on the named type: Named.NumMethods/Method list declared
	// methods (both value- and pointer-receiver declarations live on the
	// Named object).
	for i := 0; i < named.NumMethods() && i < 4; i++ {
		m := named.Method(i)
		t.Logf("  method %s %s", m.Name(), types.TypeString(m.Type(), qualifier))
	}
	if named.NumMethods() == 0 {
		t.Error("time.Duration should declare methods (Hours, String, ...)")
	}
}

// A named string type (html/template.HTML) — and the correction to the
// task's premise: net/http does NOT have `type Method string`; the
// method names are untyped string constants.
func TestNamedStringAndHTTPMethod(t *testing.T) {
	l := NewLoader()

	obj, err := l.Lookup("html/template", "HTML")
	if err != nil {
		t.Fatal(err)
	}
	named, ok := obj.Type().(*types.Named)
	if !ok {
		t.Fatalf("template.HTML: got %T", obj.Type())
	}
	b, _ := named.Underlying().(*types.Basic)
	if b == nil || b.Kind() != types.String {
		t.Fatalf("template.HTML underlying = %v, want string", named.Underlying())
	}
	t.Logf("html/template.HTML: Named over %v (named string)", b)

	// net/http method names are *types.Const with an untyped basic type.
	c, err := l.Lookup("net/http", "MethodGet")
	if err != nil {
		t.Fatal(err)
	}
	cn, ok := c.(*types.Const)
	if !ok {
		t.Fatalf("http.MethodGet: expected *types.Const, got %T", c)
	}
	ub, ok := cn.Type().(*types.Basic)
	if !ok || ub.Info()&types.IsUntyped == 0 {
		t.Fatalf("http.MethodGet type = %v (%T), want untyped string", cn.Type(), cn.Type())
	}
	if cn.Val().Kind() != constant.String || constant.StringVal(cn.Val()) != "GET" {
		t.Fatalf("http.MethodGet value = %v", cn.Val())
	}
	t.Logf("net/http.MethodGet: *types.Const of %v = %q — spec 8.1.5 untyped const rule applies",
		cn.Type(), constant.StringVal(cn.Val()))
}

// `type D int64` vs `type A = int64`: Named vs Alias.
func TestNamedVsAlias(t *testing.T) {
	l := NewLoader()
	src := `package p

import "time"

type D int64
type A = int64
type MyDuration = time.Duration
`
	pkg, _, err := l.CheckSource("example.com/p", "p.go", src)
	if err != nil {
		t.Fatal(err)
	}

	dObj := pkg.Scope().Lookup("D").(*types.TypeName)
	aObj := pkg.Scope().Lookup("A").(*types.TypeName)
	mdObj := pkg.Scope().Lookup("MyDuration").(*types.TypeName)

	if dObj.IsAlias() {
		t.Error("type D int64 should be a *types.Named, not alias")
	}
	if !aObj.IsAlias() {
		t.Error("type A = int64 should be an alias")
	}
	if !mdObj.IsAlias() {
		t.Error("type MyDuration = time.Duration should be an alias")
	}

	// Alias nodes materialize as *types.Alias (default since Go 1.23).
	aliasT := aObj.Type()
	if _, ok := aliasT.(*types.Alias); !ok {
		t.Logf("note: alias type is %T (Alias nodes may be disabled)", aliasT)
	} else {
		t.Logf("A: *types.Alias, Rhs=%v, Obj=%v", aliasT.(*types.Alias).Rhs(), aliasT.(*types.Alias).Obj().Name())
	}

	// Aliases are transparent to identity: A === int64, MyDuration === time.Duration.
	if !types.Identical(aObj.Type(), types.Typ[types.Int64]) {
		t.Error("alias A should be Identical to int64")
	}
	dur, _ := l.Lookup("time", "Duration")
	if !types.Identical(mdObj.Type(), dur.Type()) {
		t.Error("alias MyDuration should be Identical to time.Duration")
	}
	if types.Unalias(mdObj.Type()) != types.Unalias(dur.Type()) {
		t.Error("Unalias(MyDuration) should be the same *types.Named as Duration")
	}
	// And a defined type is NOT identical to its underlying.
	if types.Identical(dObj.Type(), types.Typ[types.Int64]) {
		t.Error("type D int64 should NOT be identical to int64")
	}
	if !types.ConvertibleTo(dObj.Type(), types.Typ[types.Int64]) {
		t.Error("D should be convertible to int64 — Go.conv's check")
	}
}

// Value vs pointer receiver methods on a custom named type:
// Named.Methods lists all declared methods; method SETS differ.
func TestNamedTypeMethods(t *testing.T) {
	l := NewLoader()
	src := `package p

type Celsius float64

func (c Celsius) String() string { return "x" }
func (c *Celsius) Set(v float64) { *c = Celsius(v) }
`
	pkg, _, err := l.CheckSource("example.com/p", "p.go", src)
	if err != nil {
		t.Fatal(err)
	}
	named := pkg.Scope().Lookup("Celsius").Type().(*types.Named)

	// Declared methods (both receivers live on *types.Named).
	if named.NumMethods() != 2 {
		t.Fatalf("Celsius declares %d methods, want 2", named.NumMethods())
	}
	for i := 0; i < named.NumMethods(); i++ {
		m := named.Method(i)
		sig := m.Type().(*types.Signature)
		recv := sig.Recv()
		t.Logf("  declared method %s, recv %v (%s)", m.Name(), recv.Type(),
			types.TypeString(sig, qualifier))
	}

	// Method sets: value set has only String; pointer set has both.
	valSet := types.NewMethodSet(named)
	ptrSet := types.NewMethodSet(types.NewPointer(named))
	if valSet.Len() != 1 || ptrSet.Len() != 2 {
		t.Fatalf("method sets: value=%d ptr=%d, want 1 and 2", valSet.Len(), ptrSet.Len())
	}

	// Interface assignability follows the method set: only *Celsius
	// implements an interface requiring Set.
	stringer, _ := l.Lookup("fmt", "Stringer")
	if !types.AssignableTo(named, stringer.Type()) {
		t.Error("Celsius should implement fmt.Stringer (value receiver)")
	}
	setIface := types.NewInterfaceType([]*types.Func{
		types.NewFunc(0, nil, "Set", types.NewSignatureType(
			nil, nil, nil,
			types.NewTuple(types.NewVar(0, nil, "v", types.Typ[types.Float64])),
			nil, false)),
	}, nil).Complete()
	if types.AssignableTo(named, setIface) {
		t.Error("Celsius value should NOT satisfy interface{ Set(float64) }")
	}
	if !types.AssignableTo(types.NewPointer(named), setIface) {
		t.Error("*Celsius should satisfy interface{ Set(float64) }")
	}
}

// Go struct types: *types.Named over *types.Struct. Field access and
// composite literals are NOT covered by the spec 8.3 table — a gap to
// report. The fields themselves are inspectable via *types.Struct and
// LookupFieldOrMethod returns fields (Var with IsField) as well as
// methods.
func TestStructFields(t *testing.T) {
	l := NewLoader()
	obj, err := l.Lookup("net/http", "Server")
	if err != nil {
		t.Fatal(err)
	}
	st := obj.Type().Underlying().(*types.Struct)
	var addr *types.Var
	for i := 0; i < st.NumFields(); i++ {
		if st.Field(i).Name() == "Addr" {
			addr = st.Field(i)
		}
	}
	if addr == nil || !addr.IsField() || !addr.Exported() {
		t.Fatal("http.Server.Addr should be an exported field")
	}
	t.Logf("http.Server: %d fields; Addr is %v (exported)", st.NumFields(), addr.Type())

	// LookupFieldOrMethod resolves both fields and methods; the result
	// kind distinguishes them (Var vs Func).
	fobj, index, _ := types.LookupFieldOrMethod(obj.Type(), false, nil, "Addr")
	if v, ok := fobj.(*types.Var); !ok || !v.IsField() {
		t.Fatalf("Addr: got %T", fobj)
	}
	t.Logf("LookupFieldOrMethod(Server, Addr) -> field, index=%v", index)
}

// Identity key for a nominal Go type: the *types.TypeName object,
// which is unique per (package, name) inside one load session.
// Pkg().Path() + "." + Name() is the stable string key for caches.
func TestObjectIdentityKey(t *testing.T) {
	l := NewLoader()
	d1, _ := l.Lookup("time", "Duration")
	d2, _ := l.Lookup("time", "Duration")
	if d1 != d2 {
		t.Fatal("same package+name should give the same types.Object")
	}
	t.Logf("Duration object: %T name=%s pkg=%s path=%s",
		d1, d1.Name(), d1.Pkg().Name(), d1.Pkg().Path())
}
