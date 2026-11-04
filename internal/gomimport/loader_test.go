package gomimport

// Loader unit tests: module lookup boundaries, the one-.gomi rule,
// member extraction kinds (fn/thunk arity, let vars, type requalify,
// ctor fields), caching (positive and negative), the in-flight
// cycle guard, and nested .gomi-side imports.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

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

func TestModuleFindsNearestGoMod(t *testing.T) {
	root := writeMod(t, "example.com/m", nil)
	l := New(&Config{Dir: filepath.Join(root, "sub", "dir")})
	path, dir, err := l.module()
	if err != nil {
		t.Fatalf("module: %v", err)
	}
	if path != "example.com/m" || dir != root {
		t.Fatalf("module = (%q, %q), want (example.com/m, %q)", path, dir, root)
	}
}

func TestModuleOutsideModule(t *testing.T) {
	l := New(&Config{Dir: t.TempDir()})
	if _, _, err := l.module(); err == nil {
		t.Fatal("expected a no-go.mod error")
	}
}

func TestDirOf(t *testing.T) {
	root := writeMod(t, "example.com/m", nil)
	l := New(&Config{Dir: root})
	dir, err := l.dirOf("example.com/m")
	if err != nil || dir != root {
		t.Fatalf("dirOf(module root) = %q, %v", dir, err)
	}
	dir, err = l.dirOf("example.com/m/sub/pkg")
	if err != nil || dir != filepath.Join(root, "sub", "pkg") {
		t.Fatalf("dirOf(sub) = %q, %v", dir, err)
	}
	if _, err := l.dirOf("example.com/other/pkg"); err == nil {
		t.Fatal("expected outside-module error for a sibling module path")
	}
	if _, err := l.dirOf("example.com/m2/x"); err == nil {
		t.Fatal("expected outside-module error for a same-prefix module path")
	}
}

func TestOneGomi(t *testing.T) {
	dir := t.TempDir()
	if _, err := oneGomi(dir); err == nil {
		t.Fatal("expected no-.gomi error")
	}
	for _, name := range []string{"a.gomi", "b.gomi"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := oneGomi(dir); err == nil {
		t.Fatal("expected several-.gomi error")
	}
	if err := os.Remove(filepath.Join(dir, "b.gomi")); err != nil {
		t.Fatal(err)
	}
	if p, err := oneGomi(dir); err != nil || filepath.Base(p) != "a.gomi" {
		t.Fatalf("oneGomi = %q, %v", p, err)
	}
}

// A full member surface: fn (declared arity), let (package var),
// a transparent sum type (member type + ctor members + payload
// fields), and a thunk-shaped fn member.
const shapesGomi = `package shapes

type Shape =
  | Circle (radius: Float)
  | Rect (w: Float) (h: Float)

fn area (s: Shape) -> Float
fn twice (f: a -> a) (x: a) -> a
let tau : Float
`

func TestLoadMembers(t *testing.T) {
	root := writeMod(t, "example.com/m", map[string]string{
		"shapes/shapes.gomi": shapesGomi,
	})
	l := New(&Config{Dir: root})
	p, err := l.Load("example.com/m/shapes", false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.Path != "example.com/m/shapes" {
		t.Fatalf("package path = %q", p.Path)
	}

	// fn member: packaged symbol, signature + declared arity.
	area := p.Members["area"]
	if area == nil || area.Package != "example.com/m/shapes" {
		t.Fatalf("area member = %+v", area)
	}
	sig, ok := l.SignatureOf(area)
	if !ok || sig.Type == nil || len(sig.Type.(*types.Func).Params) != 1 {
		t.Fatalf("area signature = %+v, ok=%v", sig, ok)
	}
	if sig.DeclArity != 1 || l.ArityOf(area) != 1 {
		t.Fatalf("area arity: sig %d, ArityOf %d", sig.DeclArity, l.ArityOf(area))
	}
	// The signature's Shape argument is the requalified own type.
	param := sig.Type.(*types.Func).Params[0].Type
	if c, ok := param.(*types.Con); !ok || c.Qual != "example.com/m/shapes" || c.Name != "Shape" {
		t.Fatalf("area param = %v, want shapes.Shape", param)
	}

	// let member: a KindVar packaged symbol with a plain type.
	tau := p.Members["tau"]
	if tau == nil || tau.Kind != resolve.KindVar || tau.Package != "example.com/m/shapes" {
		t.Fatalf("tau member = %+v", tau)
	}
	tausig, ok := l.SignatureOf(tau)
	if !ok {
		t.Fatal("tau has no signature")
	}
	if c, ok := tausig.Type.(*types.Con); !ok || c.Name != "Float" {
		t.Fatalf("tau type = %v, want Float", tausig.Type)
	}

	// type member + constructors, all carrying the package path.
	shape := p.Members["Shape"]
	if shape == nil || shape.Package != "example.com/m/shapes" {
		t.Fatalf("Shape member = %+v", shape)
	}
	circle := p.Members["Circle"]
	if circle == nil || circle.Kind != resolve.KindCtor || circle.Package != "example.com/m/shapes" {
		t.Fatalf("Circle member = %+v", circle)
	}
	fields := l.CtorFieldsOf(circle)
	if len(fields) != 1 || fields["radius"] == nil {
		t.Fatalf("Circle ctor fields = %v", fields)
	}
	rect := p.Members["Rect"]
	if len(l.CtorFieldsOf(rect)) != 2 {
		t.Fatalf("Rect ctor fields = %v", l.CtorFieldsOf(rect))
	}

	// The type-decl table and the lowering oracle.
	if len(l.TypeDecls()) != 1 {
		t.Fatalf("TypeDecls = %d, want 1", len(l.TypeDecls()))
	}
	vs := l.Variants(&types.Con{Qual: "example.com/m/shapes", Name: "Shape"})
	if len(vs) != 2 {
		t.Fatalf("Variants(Shape) = %d, want 2", len(vs))
	}
	if vs := l.Variants(&types.Con{Qual: "", Name: "Shape"}); vs != nil {
		t.Fatalf("Variants on unqualified Con = %v, want nil", vs)
	}

	// Symbol identity is stable across loads of one session.
	p2, err := l.Load("example.com/m/shapes", false)
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if p2 != p {
		t.Fatal("second Load returned a different *Package")
	}
	if p2.Members["Circle"] != circle {
		t.Fatal("second Load minted new member symbols")
	}
}

// A wildcard let binds no symbols — `let _ : T` is legal `.gomi`
// surface (E0602 only rejects multi-name patterns), so the member
// walk must treat WildPat/LitPat as contributing nothing rather
// than panicking.
func TestLoadWildcardLetMember(t *testing.T) {
	root := writeMod(t, "example.com/m", map[string]string{
		"p/p.gomi": "package p\n\nlet _ : Float\nlet keep : Int\n",
	})
	l := New(&Config{Dir: root})
	p, err := l.Load("example.com/m/p", false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(p.Members) != 1 || p.Members["keep"] == nil {
		t.Fatalf("members = %v, want just keep", p.Members)
	}
}

// A failed load latches: the same error object comes back without
// recompiling.
func TestLoadFailureCaches(t *testing.T) {
	root := writeMod(t, "example.com/m", nil)
	l := New(&Config{Dir: root})
	_, err1 := l.Load("example.com/m/missing", false)
	if err1 == nil {
		t.Fatal("expected a load failure")
	}
	_, err2 := l.Load("example.com/m/missing", false)
	if err2 == nil {
		t.Fatal("expected the cached failure")
	}
	if err1 != err2 {
		t.Fatalf("cached error differs: %v vs %v", err1, err2)
	}
	// Outside-module paths fail before any file lookup.
	if _, err := l.Load("example.com/other/pkg", false); err == nil {
		t.Fatal("expected outside-module failure")
	}
}

// Mutual .gomi imports hit the in-flight guard instead of
// recursing forever.
func TestLoadCycleDetected(t *testing.T) {
	root := writeMod(t, "example.com/m", map[string]string{
		"a/a.gomi": "package a\n\nimport \"example.com/m/b\"\n\nfn f (x: b.T) -> Int\n",
		"b/b.gomi": "package b\n\nimport \"example.com/m/a\"\n\ntype T =\n  | B Int\n",
	})
	l := New(&Config{Dir: root})
	if _, err := l.Load("example.com/m/a", false); err == nil {
		t.Fatal("expected the cyclic import to fail")
	}
}

// A .gomi's own `import "path"` resolves through the same loader:
// the nested package's types flow into the member signatures
// already carrying their package qualification.
func TestGomiSideImport(t *testing.T) {
	root := writeMod(t, "example.com/m", map[string]string{
		"base/base.gomi": "package base\n\ntype T =\n  | B Int\n",
		"a/a.gomi":       "package a\n\nimport \"example.com/m/base\"\n\nfn f (x: base.T) -> Int\n",
	})
	l := New(&Config{Dir: root})
	p, err := l.Load("example.com/m/a", false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	sig, ok := l.SignatureOf(p.Members["f"])
	if !ok || len(sig.Type.(*types.Func).Params) != 1 {
		t.Fatalf("f signature = %+v", sig)
	}
	c, ok := sig.Type.(*types.Func).Params[0].Type.(*types.Con)
	if !ok || c.Qual != "example.com/m/base" || c.Name != "T" {
		t.Fatalf("f param = %v, want base.T", sig.Type.(*types.Func).Params[0].Type)
	}
	// The nested package's decls joined the oracle tables too.
	if len(l.TypeDecls()) != 1 || l.TypeDecls()[0].Sym.Name != "T" {
		t.Fatalf("TypeDecls = %v", l.TypeDecls())
	}
}

// SignatureOf defers to Config.Sigs for symbols it did not mint.
type stubSigs struct{ got *resolve.Symbol }

func (s *stubSigs) SignatureOf(sym *resolve.Symbol) (check.Signature, bool) {
	s.got = sym
	return check.Signature{Type: &types.Con{Qual: "", Name: "Float"}}, true
}

func TestSignatureOfFallback(t *testing.T) {
	stub := &stubSigs{}
	l := New(&Config{Dir: t.TempDir(), Sigs: stub})
	other := &resolve.Symbol{Name: "x", Kind: resolve.KindVar, Package: "other"}
	sig, ok := l.SignatureOf(other)
	if !ok {
		t.Fatal("fallback signature missing")
	}
	if stub.got != other {
		t.Fatal("fallback did not see the queried symbol")
	}
	if c, ok := sig.Type.(*types.Con); !ok || c.Name != "Float" {
		t.Fatalf("fallback signature = %v", sig.Type)
	}
}

func TestArityOfUnknown(t *testing.T) {
	l := New(&Config{Dir: t.TempDir()})
	if got := l.ArityOf(&resolve.Symbol{Name: "nope"}); got != -1 {
		t.Fatalf("ArityOf(unknown) = %d, want -1", got)
	}
}

// requalify rewrites only the package's own type names.
func TestRequalify(t *testing.T) {
	own := map[string]bool{"Shape": true}
	shape := &types.Con{Qual: "", Name: "Shape"}
	if got := requalify(shape, own, "example.com/m/s"); got.(*types.Con).Qual != "example.com/m/s" {
		t.Fatalf("own type not qualified: %v", got)
	}
	builtin := &types.Con{Qual: "", Name: "Int"}
	if got := requalify(builtin, own, "example.com/m/s"); got.(*types.Con).Qual != "" {
		t.Fatalf("builtin wrongly qualified: %v", got)
	}
	foreign := &types.Con{Qual: "example.com/x", Name: "T"}
	if got := requalify(foreign, own, "example.com/m/s"); got.(*types.Con).Qual != "example.com/x" {
		t.Fatalf("foreign type rewritten: %v", got)
	}
	// Inside a constructor's argument list too.
	fn := &types.Func{
		Params: []types.FuncParam{{Type: &types.Con{Qual: "", Name: "Shape"}}},
		Ret:    &types.Con{Qual: "", Name: "Int"},
	}
	if got := requalify(fn, own, "q"); got.(*types.Func).Params[0].Type.(*types.Con).Qual != "q" {
		t.Fatalf("nested own type not qualified: %v", got)
	}
}

// A .gomi member whose emitted Go name another member of the same
// package claims misbinds every imported reference — `lib.foo`
// compiles to `lib.Foo`, the type's name. The loader marks the
// clash on the member symbol so resolve reports E0607 at the use
// site even when the declaring package was never checked.
func TestImportedMemberEmitClash(t *testing.T) {
	root := writeMod(t, "example.com/m", map[string]string{
		"lib/lib.gomi": `package lib

type Foo = { x: Int }
let foo : Foo
`,
	})
	r := compileEntry(t, root, filepath.Join(root, "a"), `package main

import "example.com/m/lib"

let v : lib.Foo = lib.foo
`)
	var codes []diag.Code
	for _, d := range r.Diagnostics {
		codes = append(codes, d.Code)
		if d.Code == "E0607" {
			if d.Data["member"] != "foo" || d.Data["collides_with"] != "Foo" || d.Data["emitted"] != "Foo" {
				t.Fatalf("E0607 data = %v", d.Data)
			}
		}
	}
	if len(codes) != 1 || codes[0] != "E0607" {
		t.Fatalf("codes = %v, want exactly E0607", codes)
	}
}

// The same member table without a clash marks nothing — a `let pi`
// that alone claims `Pi` stays a clean reference.
func TestImportedMemberNoEmitClash(t *testing.T) {
	root := writeMod(t, "example.com/m", map[string]string{
		"lib/lib.gomi": `package lib

let pi : Float
`,
	})
	r := compileEntry(t, root, filepath.Join(root, "a"), `package main

import "example.com/m/lib"

let v : Float = lib.pi
`)
	for _, d := range r.Diagnostics {
		if d.Code == "E0607" {
			t.Fatalf("spurious emit clash: %v", r.Diagnostics)
		}
	}
}

// Value-vs-value clashes mark the loser too — `my_fn` and `myFn`
// both emit `MyFn` and no member is literally named `MyFn`, so the
// mark has to replay codegen's (base, Gom name) claiming order.
func TestImportedMemberEmitClashValueValue(t *testing.T) {
	root := writeMod(t, "example.com/m", map[string]string{
		"lib/lib.gomi": `package lib

let my_fn : Int
let myFn : Int
`,
	})
	r := compileEntry(t, root, filepath.Join(root, "a"), `package main

import "example.com/m/lib"

let v : Int = lib.my_fn
`)
	var codes []diag.Code
	for _, d := range r.Diagnostics {
		codes = append(codes, d.Code)
	}
	if len(codes) != 1 || codes[0] != "E0607" {
		t.Fatalf("codes = %v, want exactly E0607", codes)
	}
	// The winner's reference stays clean.
	r2 := compileEntry(t, root, filepath.Join(root, "a"), `package main

import "example.com/m/lib"

let v : Int = lib.myFn
`)
	for _, d := range r2.Diagnostics {
		if d.Code == "E0607" {
			t.Fatalf("the winning member was marked: %v", r2.Diagnostics)
		}
	}
}
