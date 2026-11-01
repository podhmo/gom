package probe

import (
	"go/types"
	"testing"
)

// Q1: what does *types.Interface look like for io.Reader / io.Writer /
// error / fmt.Stringer, and how is "value assignable to interface"
// detected?

func ifaceOf(t *testing.T, obj types.Object) (*types.Named, *types.Interface) {
	t.Helper()
	named, ok := types.Unalias(obj.Type()).(*types.Named)
	if !ok {
		t.Fatalf("%s: expected *types.Named, got %T", obj.Name(), types.Unalias(obj.Type()))
	}
	iface, ok := named.Underlying().(*types.Interface)
	if !ok {
		t.Fatalf("%s: underlying is %T, want *types.Interface", obj.Name(), named.Underlying())
	}
	return named, iface
}

func TestInterfaceShapes(t *testing.T) {
	l := NewLoader()
	for _, tc := range []struct {
		pkg, name string
	}{
		{"io", "Reader"},
		{"io", "Writer"},
		{"fmt", "Stringer"},
		{"sort", "Interface"},
	} {
		obj, err := l.Lookup(tc.pkg, tc.name)
		if err != nil {
			t.Fatal(err)
		}
		named, iface := ifaceOf(t, obj)
		t.Logf("%s.%s: *types.Named -> *types.Interface, NumMethods=%d NumEmbeddeds=%d IsMethodSet=%v",
			tc.pkg, tc.name, iface.NumMethods(), iface.NumEmbeddeds(), iface.IsMethodSet())
		for i := 0; i < iface.NumMethods(); i++ {
			m := iface.Method(i)
			t.Logf("  method[%d] %s %s", i, m.Name(), types.TypeString(m.Type(), qualifier))
		}
		_ = named
	}

	// error: predeclared, *types.Named over *types.Interface, 1 method.
	errObj := types.Universe.Lookup("error")
	named, iface := ifaceOf(t, errObj)
	if iface.NumMethods() != 1 || iface.Method(0).Name() != "Error" {
		t.Fatalf("unexpected error interface: %v", named)
	}
	t.Logf("error: Named=%v underlying method %s", named, iface.Method(0))

	// any: *types.Alias over the empty interface.
	anyObj := types.Universe.Lookup("any")
	tn, ok := anyObj.(*types.TypeName)
	if !ok || !tn.IsAlias() {
		t.Fatalf("any: expected alias TypeName, got %T", anyObj)
	}
	if !IsAny(anyObj.Type()) {
		t.Fatal("any should be the empty interface")
	}
	t.Logf("any: %T (alias) -> %T", anyObj.Type(), types.Unalias(anyObj.Type()))
}

// io.ReadWriter embeds io.Reader and io.Writer: embedded types appear
// through EmbeddedTypes(); NumMethods flattens them.
func TestEmbeddedInterface(t *testing.T) {
	l := NewLoader()
	obj, err := l.Lookup("io", "ReadWriter")
	if err != nil {
		t.Fatal(err)
	}
	_, iface := ifaceOf(t, obj)
	t.Logf("io.ReadWriter: NumMethods=%d NumExplicitMethods=%d NumEmbeddeds=%d",
		iface.NumMethods(), iface.NumExplicitMethods(), iface.NumEmbeddeds())
	if iface.NumEmbeddeds() != 2 {
		t.Fatalf("ReadWriter embeds %d types, want 2", iface.NumEmbeddeds())
	}
	for i := 0; i < iface.NumEmbeddeds(); i++ {
		et := iface.EmbeddedType(i)
		t.Logf("  embedded[%d]: %s (%T)", i, types.TypeString(et, qualifier), et)
	}
	// The flattened method list is still authoritative for assignability.
	if iface.NumMethods() != 2 {
		t.Fatalf("ReadWriter flattens to %d methods, want 2", iface.NumMethods())
	}
}

// "Can a Gom value of Go type T be passed where interface I is expected?"
// is just types.AssignableTo(T, I) — go/types performs the method-set
// check, including the pointer-receiver distinction.
func TestInterfaceAssignability(t *testing.T) {
	l := NewLoader()
	readerObj, err := l.Lookup("io", "Reader")
	if err != nil {
		t.Fatal(err)
	}
	reader := readerObj.Type() // *types.Named
	readerIface := types.Unalias(reader).Underlying().(*types.Interface)

	fileObj, err := l.Lookup("os", "File")
	if err != nil {
		t.Fatal(err)
	}
	file := fileObj.Type()            // *types.Named (struct)
	filePtr := types.NewPointer(file) // *os.File

	// *os.File implements io.Reader; os.File does not (Read is on *File).
	if !types.AssignableTo(filePtr, reader) {
		t.Error("*os.File should be assignable to io.Reader")
	}
	if types.AssignableTo(file, reader) {
		t.Error("os.File (value) should NOT be assignable to io.Reader")
	}
	if !types.Implements(filePtr, readerIface) {
		t.Error("types.Implements(*os.File, io.Reader) = false, want true")
	}
	if types.Implements(file, readerIface) {
		t.Error("types.Implements(os.File, io.Reader) = true, want false")
	}

	// io.Reader is itself assignable to io.ReadWriter? No — Reader
	// lacks Write. interface-to-interface assignability is a subset
	// check on the method sets.
	rwObj, err := l.Lookup("io", "ReadWriter")
	if err != nil {
		t.Fatal(err)
	}
	if types.AssignableTo(reader, rwObj.Type()) {
		t.Error("io.Reader should not be assignable to io.ReadWriter")
	}
	if !types.AssignableTo(rwObj.Type(), reader) {
		t.Error("io.ReadWriter should be assignable to io.Reader")
	}

	// Everything is assignable to any.
	anyT := types.Universe.Lookup("any").Type()
	if !types.AssignableTo(file, anyT) {
		t.Error("os.File should be assignable to any")
	}
	t.Logf("AssignableTo(*os.File, io.Reader)=%v  AssignableTo(os.File, io.Reader)=%v",
		types.AssignableTo(filePtr, reader), types.AssignableTo(file, reader))
}

// Method sets: types.NewMethodSet is the operation the checker needs
// when it wants to look up a method by name (x.M(args), spec 8.1.3).
func TestMethodSets(t *testing.T) {
	l := NewLoader()
	fileObj, err := l.Lookup("os", "File")
	if err != nil {
		t.Fatal(err)
	}
	file := fileObj.Type()

	valSet := types.NewMethodSet(file)
	ptrSet := types.NewMethodSet(types.NewPointer(file))
	t.Logf("method sets of os.File: value=%d ptr=%d", valSet.Len(), ptrSet.Len())

	var readOnValue, readOnPtr bool
	for i := 0; i < valSet.Len(); i++ {
		m := valSet.At(i)
		t.Logf("  value method: %s (recv %v, exported=%v)",
			m.Obj().Name(), m.Recv(), m.Obj().Exported())
		if m.Obj().Name() == "Read" {
			readOnValue = true
		}
	}
	for i := 0; i < ptrSet.Len(); i++ {
		if ptrSet.At(i).Obj().Name() == "Read" {
			readOnPtr = true
		}
	}
	if readOnValue || !readOnPtr {
		t.Errorf("Read: value=%v ptr=%v; want value=false ptr=true", readOnValue, readOnPtr)
	}
	// The value method set of os.File is just {close} — an UNEXPORTED
	// value-receiver method. Method sets include unexported methods,
	// which is also why a Gom type can never implement Go interfaces
	// that mention unexported methods (moot in v1 anyway).
	if valSet.Len() != 1 || valSet.At(0).Obj().Name() != "close" {
		t.Errorf("os.File value method set = %d methods, want 1 (close)", valSet.Len())
	}

	// LookupFieldOrMethod is the single-call version used for x.M.
	// Read is found but index=[5]: it is PROMOTED from the embedded
	// unexported field *file — the index is a field-access path, not
	// a flat method index.
	obj, index, indirect := types.LookupFieldOrMethod(file, true, nil, "Read")
	if obj == nil {
		t.Fatal("LookupFieldOrMethod(*os.File-addressable, Read) not found")
	}
	t.Logf("LookupFieldOrMethod(File, Read): obj=%s index=%v indirect=%v",
		types.ObjectString(obj, qualifier), index, indirect)
	if len(index) != 1 || index[0] != 5 {
		t.Logf("note: expected promoted method at index [5], got %v", index)
	}
}

// Representation implication: a Go interface is a NOMINAL Gom type
// (io.Reader is a name, not a structural shape). The *types.Named that
// wraps the interface is the identity key; the underlying *types.Interface
// is only needed for assignability questions, which go/types answers.
func TestInterfaceNominalNote(t *testing.T) {
	l := NewLoader()
	r1, _ := l.Lookup("io", "Reader")
	r2, _ := l.Lookup("io", "ReadWriter")
	u1 := types.Unalias(r1.Type()).Underlying().(*types.Interface)
	u2 := types.Unalias(r2.Type()).Underlying().(*types.Interface)
	if u1 == u2 {
		t.Fatal("Reader and ReadWriter share an underlying object?")
	}
}
