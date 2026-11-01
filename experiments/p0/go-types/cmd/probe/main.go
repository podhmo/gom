// Command probe prints a human-readable report of the go/types facts
// that Gom's Go-interop boundary depends on (TODO.md P0-5).
// The authoritative answers live in ../probe's tests; this program is
// the readable dump.
package main

import (
	"fmt"
	"go/types"
	"os"

	"github.com/podhmo/gom/experiments/p0/go-types/probe"
)

func main() {
	l := probe.NewLoader()

	section("Q1 interfaces: *types.Named over *types.Interface")
	for _, path := range [][2]string{
		{"io", "Reader"}, {"io", "Writer"}, {"io", "ReadWriter"},
		{"fmt", "Stringer"}, {"sort", "Interface"},
	} {
		obj := must(l.Lookup(path[0], path[1]))
		named := types.Unalias(obj.Type()).(*types.Named)
		iface := named.Underlying().(*types.Interface)
		fmt.Printf("%s.%s: Named -> Interface  methods=%d embeddeds=%d\n",
			path[0], path[1], iface.NumMethods(), iface.NumEmbeddeds())
		for i := 0; i < iface.NumMethods(); i++ {
			m := iface.Method(i)
			fmt.Printf("    %s %s\n", m.Name(), types.TypeString(m.Type(), q))
		}
		for i := 0; i < iface.NumEmbeddeds(); i++ {
			fmt.Printf("    embeds %s\n", types.TypeString(iface.EmbeddedType(i), q))
		}
	}
	errObj := types.Universe.Lookup("error")
	fmt.Printf("error: %T -> %v (underlying %T)\n",
		errObj.Type(), errObj.Type(), errObj.Type().Underlying())
	anyObj := types.Universe.Lookup("any")
	fmt.Printf("any:   %T (alias) -> %v\n", anyObj.Type(), types.Unalias(anyObj.Type()))

	file := must(l.Lookup("os", "File")).Type()
	reader := must(l.Lookup("io", "Reader")).Type()
	fmt.Printf("AssignableTo(*os.File, io.Reader) = %v\n", types.AssignableTo(types.NewPointer(file), reader))
	fmt.Printf("AssignableTo( os.File, io.Reader) = %v\n", types.AssignableTo(file, reader))

	section("Q2 named types: nominal identity via *types.TypeName")
	for _, path := range [][2]string{
		{"time", "Duration"}, {"html/template", "HTML"},
		{"time", "Time"}, {"net/http", "MethodGet"},
	} {
		obj := must(l.Lookup(path[0], path[1]))
		t := obj.Type()
		fmt.Printf("%s.%s: object %T  type %T (%v)  underlying %T (%v)\n",
			path[0], path[1], obj, types.Unalias(t), t,
			types.Unalias(t).Underlying(), t.Underlying())
	}
	dur := must(l.Lookup("time", "Duration")).Type()
	fmt.Printf("Identical(Duration, int64)=%v  ConvertibleTo=%v\n",
		types.Identical(dur, types.Typ[types.Int64]),
		types.ConvertibleTo(dur, types.Typ[types.Int64]))
	pkg := mustPkg(l.CheckSource("example.com/p", "p.go",
		"package p\n\ntype D int64\ntype A = int64\n"))
	fmt.Printf("type D int64:  IsAlias=%v node=%T\n",
		pkg.Scope().Lookup("D").(*types.TypeName).IsAlias(), pkg.Scope().Lookup("D").Type())
	fmt.Printf("type A = int64: IsAlias=%v node=%T identical-to-int64=%v\n",
		pkg.Scope().Lookup("A").(*types.TypeName).IsAlias(), pkg.Scope().Lookup("A").Type(),
		types.Identical(pkg.Scope().Lookup("A").Type(), types.Typ[types.Int64]))

	section("Q3 identity: stable within ONE importer, not across")
	copySig := must(l.Lookup("io", "Copy")).Type().(*types.Signature)
	fmt.Printf("io.Reader via scope %p == via io.Copy param %p: %v\n",
		reader, copySig.Params().At(1).Type(), reader == copySig.Params().At(1).Type())
	l2 := probe.NewLoader()
	r2 := must(l2.Lookup("io", "Reader")).Type()
	fmt.Printf("second loader: same object=%v Identical=%v\n",
		reader == r2, types.Identical(reader, r2))

	section("Q4 signatures")
	for _, path := range [][2]string{
		{"strconv", "Atoi"}, {"os", "ReadFile"}, {"encoding/json", "Marshal"},
		{"fmt", "Sprintf"}, {"fmt", "Println"}, {"os", "LookupEnv"}, {"io", "Copy"},
	} {
		sig := must(l.Lookup(path[0], path[1])).Type().(*types.Signature)
		f := probe.DeconstructSignature(sig)
		fmt.Printf("%s.%s: %s\n    variadic=%v result-shape=%s\n",
			path[0], path[1], types.TypeString(sig, q), f.Variadic, probe.ClassifyResults(sig))
		for i, p := range f.Params {
			fmt.Printf("    param[%d]  %-8s %-12s node=%-18s err=%v any=%v nil=%v\n",
				i, p.Name, p.Type, p.Concrete, p.IsError, p.IsAny, p.Nilable)
		}
		for i, p := range f.Results {
			fmt.Printf("    result[%d] %-8s %-12s node=%-18s err=%v any=%v nil=%v\n",
				i, p.Name, p.Type, p.Concrete, p.IsError, p.IsAny, p.Nilable)
		}
	}

	section("Q5 generics: constraints classify the v1 boundary")
	for _, path := range [][2]string{
		{"slices", "Sort"}, {"slices", "Contains"}, {"unique", "Make"},
		{"weak", "Make"}, {"maps", "All"}, {"cmp", "Or"},
	} {
		obj := must(l.Lookup(path[0], path[1]))
		sig := obj.Type().(*types.Signature)
		f := probe.DeconstructSignature(sig)
		fmt.Printf("%s.%s:\n", path[0], path[1])
		for _, tp := range f.TypeParam {
			fmt.Printf("    [%s %s] -> %s\n", tp.Name, tp.Constraint, tp.ConstraintClass)
		}
		fmt.Printf("    strict v1 callable=%v\n", probe.IsCallableGeneric(sig, true))
	}

	section("Q6 nil-able kinds")
	nilcases := []struct {
		label string
		typ   types.Type
	}{
		{"*int", types.NewPointer(types.Typ[types.Int])},
		{"[]byte", types.NewSlice(types.Typ[types.Byte])},
		{"map[string]int", types.NewMap(types.Typ[types.String], types.Typ[types.Int])},
		{"chan int", types.NewChan(types.SendRecv, types.Typ[types.Int])},
		{"func()", types.NewSignatureType(nil, nil, nil, nil, nil, false)},
		{"error", probe.ErrorType()},
		{"io.Reader", reader},
		{"unsafe.Pointer", types.Typ[types.UnsafePointer]},
		{"int", types.Typ[types.Int]},
		{"string", types.Typ[types.String]},
		{"time.Duration", dur},
		{"os.File", file},
	}
	for _, c := range nilcases {
		fmt.Printf("%-16v nilable=%v\n", c.label, probe.NilableByAssignability(c.typ))
	}
}

func section(title string) {
	fmt.Printf("\n=== %s ===\n", title)
}

func q(p *types.Package) string {
	if p == nil {
		return ""
	}
	return p.Name()
}

func must[T any](v T, err error) T {
	if err != nil {
		fmt.Fprintln(os.Stderr, "probe:", err)
		os.Exit(1)
	}
	return v
}

func mustPkg[T, U any](v T, _ U, err error) T {
	return must(v, err)
}
