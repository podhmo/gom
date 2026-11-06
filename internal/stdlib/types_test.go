package stdlib

// Parity between a type member of a builtin-scope module and the
// same .gomi loaded as an imported package: `Shapes.Shape` and
// `shapes.Shape` (and the `pkg.Type.Ctor` spelling) must resolve
// identically through the Modules+Loader seams and through
// gomimport's member table.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/gomimport"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

const shapesGomi = `package shapes

type Shape =
  | Circle (radius: Float)
  | Rect (w: Float) (h: Float)

fn area (s: Shape) -> Float

let unit_circle : Shape
let transform : Shape -> Shape
`

// writeMod builds a throwaway module: go.mod plus the given files,
// and returns the module root directory.
func writeMod(t *testing.T, module string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	gomod := "module " + module + "\n\ngo 1.27\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestLoadTypeMembers loads the same interface through a synthetic
// module env and through gomimport's Loader, and compares the
// member tables, ctor tables and signatures the two paths produce.
// The module name must be one resolve's builtin scope binds — the
// hardcoded List/Option/Result set — since Config.Modules only
// injects member tables; "List" serves the synthetic package fine.
func TestLoadTypeMembers(t *testing.T) {
	e := newEnv()
	if err := e.loadPackage("List", "shapes/shapes.gomi",
		"example.com/m/shapes", []byte(shapesGomi)); err != nil {
		t.Fatalf("loadPackage: %v", err)
	}
	table := e.modules["List"]

	root := writeMod(t, "example.com/m", map[string]string{
		"shapes/shapes.gomi": shapesGomi,
	})
	l := gomimport.New(&gomimport.Config{Dir: filepath.Join(root, "app")})
	p, err := l.Load("example.com/m/shapes", false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Same members, same kinds — type, ctors and fn all register.
	wantKinds := map[string]resolve.Kind{
		"Shape":       resolve.KindType,
		"Circle":      resolve.KindCtor,
		"Rect":        resolve.KindCtor,
		"area":        resolve.KindFunc,
		"unit_circle": resolve.KindVar,
		"transform":   resolve.KindVar,
	}
	for name, want := range wantKinds {
		ms, gs := table[name], p.Members[name]
		if ms == nil || gs == nil {
			t.Fatalf("member %s: module=%v import=%v", name, ms, gs)
		}
		if ms.Kind != want || gs.Kind != want {
			t.Errorf("member %s kind: module=%v import=%v want %v",
				name, ms.Kind, gs.Kind, want)
		}
		if ms.Package != "example.com/m/shapes" || gs.Package != ms.Package {
			t.Errorf("member %s package not requalified: %q vs %q",
				name, ms.Package, gs.Package)
		}
	}

	// The `pkg.Type.Ctor` table agrees with the loader's.
	shape, gshape := table["Shape"], p.Members["Shape"]
	ctors, gctors := e.typeCtors[shape], l.CtorsOf(gshape)
	if len(ctors) != len(gctors) {
		t.Fatalf("ctor tables differ: module=%v import=%v", ctors, gctors)
	}
	for name := range gctors {
		if ctors[name] == nil {
			t.Errorf("module ctor table missing %s", name)
		}
	}

	// The type's own signature and a constructor's signature
	// requalify to the same import path on both sides.
	msig, mok := e.sigs.SignatureOf(shape)
	gsig, gok := l.SignatureOf(gshape)
	if !mok || !gok {
		t.Fatalf("Shape signature: module=%v import=%v", mok, gok)
	}
	if !types.Equal(msig.Type, gsig.Type) {
		t.Errorf("Shape signature differs: module=%v import=%v",
			msig.Type, gsig.Type)
	}
	csig, _ := e.sigs.SignatureOf(ctors["Circle"])
	gcsig, _ := l.SignatureOf(gctors["Circle"])
	if !types.Equal(csig.Type, gcsig.Type) {
		t.Errorf("Circle signature differs: module=%v import=%v",
			csig.Type, gcsig.Type)
	}
	// A fn member whose signature mentions the interface's own
	// type requalifies to the import path on both sides.
	asig, aok := e.sigs.SignatureOf(table["area"])
	gasig, gaok := l.SignatureOf(p.Members["area"])
	if !aok || !gaok {
		t.Fatalf("area signature: module=%v import=%v", aok, gaok)
	}
	if !types.Equal(asig.Type, gasig.Type) {
		t.Errorf("area signature differs: module=%v import=%v",
			asig.Type, gasig.Type)
	}
	// Let members requalify likewise — a function-valued let's
	// signature mentions the interface's own type on both sides.
	tsig, tok := e.sigs.SignatureOf(table["transform"])
	gtsig, gtok := l.SignatureOf(p.Members["transform"])
	if !tok || !gtok {
		t.Fatalf("transform signature: module=%v import=%v", tok, gtok)
	}
	if !types.Equal(tsig.Type, gtsig.Type) {
		t.Errorf("transform signature differs: module=%v import=%v",
			tsig.Type, gtsig.Type)
	}
	// And neither path records a declared arity for a let member —
	// lowering's FuncArity falls back to the flat arity of the
	// member's type on both.
	if _, ok := e.arities[table["transform"]]; ok {
		t.Error("module path recorded an arity for a let member")
	}
	if a := l.ArityOf(p.Members["transform"]); a != -1 {
		t.Errorf("import path ArityOf(let member) = %d, want -1", a)
	}
	if len(e.tdecls) != 1 || len(l.TypeDecls()) != 1 {
		t.Fatalf("tdecls: module=%d import=%d", len(e.tdecls), len(l.TypeDecls()))
	}
	if e.ctorFields[ctors["Circle"]]["radius"] == nil {
		t.Error("Circle's labeled payload slot is not in ctorFields")
	}
}

// TestTypeMemberParity compiles the same program against the type
// member through both wirings — `List.X` resolving through the
// builtin module table plus the wrapped Loader seam (the module
// name is fixed by resolve's builtin scope, so the synthetic
// package rides on List), and `shapes.X` through a real gomimport
// Loader — and requires the emitted Go to name the same
// constructors.
func TestTypeMemberParity(t *testing.T) {
	e := newEnv()
	if err := e.loadPackage("List", "shapes/shapes.gomi",
		"example.com/m/shapes", []byte(shapesGomi)); err != nil {
		t.Fatalf("loadPackage: %v", err)
	}
	modSrc := `package main

fn describe (s: List.Shape) -> String = match s {
  List.Shape.Circle r -> "circle ${r}"
  List.Shape.Rect w h -> "rect ${w} ${h}"
}

fn mk () -> List.Shape = List.Shape.Circle 1.0
`
	r, err := build.Compile(build.Input{Path: "app.gom", Src: []byte(modSrc)},
		e.apply(&build.Options{}))
	if err != nil {
		t.Fatalf("Compile (module): %v", err)
	}
	if len(r.Diagnostics) != 0 {
		t.Fatalf("module-path diagnostics: %v", r.Diagnostics)
	}

	root := writeMod(t, "example.com/m", map[string]string{
		"shapes/shapes.gomi": shapesGomi,
	})
	l := gomimport.New(&gomimport.Config{Dir: filepath.Join(root, "app")})
	impSrc := `package main

import "example.com/m/shapes"

fn describe (s: shapes.Shape) -> String = match s {
  shapes.Shape.Circle r -> "circle ${r}"
  shapes.Shape.Rect w h -> "rect ${w} ${h}"
}

fn mk () -> shapes.Shape = shapes.Shape.Circle 1.0
`
	g, err := build.Compile(
		build.Input{Path: filepath.Join(root, "app", "app.gom"), Src: []byte(impSrc)},
		&build.Options{Loader: l, Sigs: l,
			Lower: &lower.Env{Types: l, FuncArity: l.ArityOf}})
	if err != nil {
		t.Fatalf("Compile (import): %v", err)
	}
	if len(g.Diagnostics) != 0 {
		t.Fatalf("import-path diagnostics: %v", g.Diagnostics)
	}

	for _, want := range []string{
		"shapes.Shape_Circle{", "case shapes.Shape_Circle:", "case shapes.Shape_Rect:",
	} {
		if !strings.Contains(string(r.Output.Source), want) {
			t.Errorf("module-path output lacks %q\n%s", want, r.Output.Source)
		}
		if !strings.Contains(string(g.Output.Source), want) {
			t.Errorf("import-path output lacks %q\n%s", want, g.Output.Source)
		}
	}
}
