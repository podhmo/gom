package gomimport

// Loader unit tests: module lookup boundaries, the one-.gomi rule,
// member extraction kinds (fn/thunk arity, let vars, type requalify,
// ctor fields), caching (positive and negative), the in-flight
// cycle guard, and nested .gomi-side imports.

import (
	"os"
	"path/filepath"
	"reflect"
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

func TestModuleAcceptsTrailingComment(t *testing.T) {
	root := writeMod(t, "example.com/m", nil)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/m// trailing comment\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := New(&Config{Dir: root})
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

// A .gomi `@method(GoName)` supplies the verbatim method name to the
// conformance table an importing package sees — ExportName would
// only reach `ServeHttp`.
func TestGomiMethodVerbatimName(t *testing.T) {
	root := writeMod(t, "example.com/m", map[string]string{
		"lib/lib.gomi": "package lib\n\ntype Handler = { tag: String }\n\n@method(ServeHTTP)\nfn (h: Handler) serve_http (w: Int) -> Unit\n",
	})
	l := New(&Config{Dir: root})
	if _, err := l.Load("example.com/m/lib", false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ms, ok := l.GomMethodsOf(&types.Con{Qual: "example.com/m/lib", Name: "Handler"})
	if !ok || len(ms) != 1 {
		t.Fatalf("GomMethodsOf = %v, ok=%v", ms, ok)
	}
	if ms[0].Name != "ServeHTTP" {
		t.Fatalf("method name = %q, want ServeHTTP", ms[0].Name)
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

// An external module's Gom package resolves through `go list` in
// the consumer's module context — a replaced sibling carrying a
// .gomi is a Gom package (the old "main module only" boundary
// reported E0203 here).
func TestLoadExternalModulePackage(t *testing.T) {
	if testing.Short() {
		t.Skip("runs `go list`")
	}
	gomod := `module example.com/ext

go 1.27

require example.com/lib v0.0.0

replace example.com/lib => ../lib
`
	dir := t.TempDir()
	ext := filepath.Join(dir, "ext")
	if err := os.MkdirAll(ext, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ext, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	// The lib module sits next to ext for the relative replace.
	libDir := filepath.Join(dir, "lib")
	if err := os.MkdirAll(filepath.Join(libDir, "shapes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "go.mod"),
		[]byte("module example.com/lib\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "shapes", "shapes.gomi"),
		[]byte(shapesGomi), 0o644); err != nil {
		t.Fatal(err)
	}

	l := New(&Config{Dir: ext})
	p, err := l.Load("example.com/lib/shapes", false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.Members["area"] == nil {
		t.Fatal("area missing — external module's .gomi did not load")
	}
}

// `import "list"` unfolds to the toolchain's stdlib path in any
// module: the members carry the canonical package path (which is
// what the generated Go import names) while the clause binds the
// short tail `list`.
func TestLoadStdlibShortPath(t *testing.T) {
	if testing.Short() {
		t.Skip("runs `go list`")
	}
	root := repoRoot(t)
	dir := t.TempDir()
	gomod := `module example.com/ext

go 1.27

require github.com/podhmo/gom v0.0.0

replace github.com/podhmo/gom => ` + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	l := New(&Config{Dir: dir})
	p, err := l.Load("list", false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.Path != "github.com/podhmo/gom/stdlib/list" {
		t.Fatalf("package path = %q — want the canonical toolchain path", p.Path)
	}
	if p.Members["fold"] == nil {
		t.Fatal("fold missing — list.gomi did not load")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

// Vendored dependencies resolve from vendor/ directly — `go list
// -m all` cannot see the build list under vendoring, and the
// module cache may never have held the source.
func TestLoadVendoredModulePackage(t *testing.T) {
	if testing.Short() {
		t.Skip("runs `go list`")
	}
	dir := t.TempDir()
	ext := filepath.Join(dir, "ext")
	vendored := filepath.Join(ext, "vendor", "example.com", "lib", "shapes")
	if err := os.MkdirAll(vendored, 0o755); err != nil {
		t.Fatal(err)
	}
	gomod := `module example.com/ext

go 1.27

require example.com/lib v0.0.0
`
	if err := os.WriteFile(filepath.Join(ext, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ext, "vendor", "modules.txt"),
		[]byte("# example.com/lib v0.0.0\n## explicit\nexample.com/lib/shapes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendored, "shapes.gomi"),
		[]byte(shapesGomi), 0o644); err != nil {
		t.Fatal(err)
	}

	l := New(&Config{Dir: ext})
	p, err := l.Load("example.com/lib/shapes", false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.Members["area"] == nil {
		t.Fatal("area missing — vendored .gomi did not load")
	}
}

// GOFLAGS=-mod=mod disables vendoring for the go command — the
// vendored copy must be ignored then too, or its stale interface
// would disagree with the source the generated Go import uses.
func TestLoadVendorDisabledByModFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("runs `go list`")
	}
	t.Setenv("GOFLAGS", "-mod=mod")
	dir := t.TempDir()
	ext := filepath.Join(dir, "ext")
	vendored := filepath.Join(ext, "vendor", "example.com", "lib", "shapes")
	if err := os.MkdirAll(vendored, 0o755); err != nil {
		t.Fatal(err)
	}
	gomod := `module example.com/ext

go 1.27

require example.com/lib v0.0.0

replace example.com/lib => ../lib
`
	if err := os.WriteFile(filepath.Join(ext, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ext, "vendor", "modules.txt"),
		[]byte("# example.com/lib v0.0.0\n## explicit\nexample.com/lib/shapes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The vendored copy is stale — it lacks `area`.
	if err := os.WriteFile(filepath.Join(vendored, "shapes.gomi"),
		[]byte("package shapes\n\nfn legacy () -> Int\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	libDir := filepath.Join(dir, "lib")
	if err := os.MkdirAll(filepath.Join(libDir, "shapes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "go.mod"),
		[]byte("module example.com/lib\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "shapes", "shapes.gomi"),
		[]byte(shapesGomi), 0o644); err != nil {
		t.Fatal(err)
	}

	l := New(&Config{Dir: ext})
	p, err := l.Load("example.com/lib/shapes", false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.Members["area"] == nil {
		t.Fatal("area missing — stale vendored .gomi won over the -mod=mod source")
	}
}

// Workspace vendoring materializes at <workroot>/vendor, not the
// module's own vendor/ — under a go.work the loader must look
// there both for package resolution and for the -mod=readonly
// retry when `go list -m all` refuses the vendor directory.
func TestLoadWorkspaceVendoredPackage(t *testing.T) {
	if testing.Short() {
		t.Skip("runs `go list`/`go env`")
	}
	dir := t.TempDir()
	ext := filepath.Join(dir, "ext")
	vendored := filepath.Join(dir, "vendor", "example.com", "lib", "shapes")
	otherp := filepath.Join(ext, "other", "p")
	for _, d := range []string{ext, vendored, otherp, filepath.Join(ext, "libsrc")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.work"),
		[]byte("go 1.27\n\nuse ./ext\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// lib reaches the build list through a replace so the
	// -mod=readonly retry can resolve it — the workroot vendor
	// still wins its resolution, exactly as the go command's
	// vendoring does. other is a non-vendored dependency: it is
	// importable only through the retried module graph.
	gomod := `module example.com/ext

go 1.27

require example.com/lib v0.0.0

require example.com/other v0.0.0

replace example.com/lib => ./libsrc

replace example.com/other => ./other
`
	if err := os.WriteFile(filepath.Join(ext, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	for mod, mdir := range map[string]string{
		"example.com/lib":   filepath.Join(ext, "libsrc"),
		"example.com/other": filepath.Join(ext, "other"),
	} {
		if err := os.WriteFile(filepath.Join(mdir, "go.mod"),
			[]byte("module "+mod+"\n\ngo 1.27\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Mirror a real `go work vendor` modules.txt: "## workspace"
	// marker, then per vendored module the requirement with its
	// replacement, "## explicit", the vendored package paths, and
	// the bare-replacement trailer line.
	if err := os.WriteFile(filepath.Join(dir, "vendor", "modules.txt"),
		[]byte("## workspace\n# example.com/lib v0.0.0 => ./libsrc\n## explicit; go 1.27\nexample.com/lib/shapes\n# example.com/lib => ./libsrc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendored, "shapes.gomi"),
		[]byte(shapesGomi), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherp, "p.gomi"),
		[]byte("package p\n\nfn pick (b: Bool) (x: a) (y: a) -> a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	l := New(&Config{Dir: ext})
	p, err := l.Load("example.com/lib/shapes", false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.Members["area"] == nil {
		t.Fatal("area missing — workspace-vendored .gomi did not load")
	}
	// A non-vendored dependency resolves only after the
	// -mod=readonly retry reopens the module graph `go list -m
	// all` refuses under workspace vendoring.
	q, err := l.Load("example.com/other/p", false)
	if err != nil {
		t.Fatalf("Load non-vendored dep: %v", err)
	}
	if q.Members["pick"] == nil {
		t.Fatal("pick missing — -mod=readonly retry did not run")
	}
	exts, err := ExternalPackages(ext, "")
	if err != nil {
		t.Fatalf("ExternalPackages: %v", err)
	}
	want := []string{"example.com/lib/shapes", "example.com/other/p"}
	if !reflect.DeepEqual(exts, want) {
		t.Fatalf("ExternalPackages = %v, want %v", exts, want)
	}
}

func TestVendorEnabledLastModWins(t *testing.T) {
	for _, tc := range []struct {
		flags string
		want  bool
	}{
		{"", true},
		{"-mod=vendor", true},
		{"-mod=mod", false},
		{"-mod=readonly", false},
		{"-mod mod", false},
		{"-mod=mod -mod=vendor", true},
		{"-mod=vendor -mod=mod", false},
		{"-tags netgo", true},
	} {
		if got := vendorEnabled(tc.flags); got != tc.want {
			t.Errorf("GOFLAGS=%q: vendorEnabled() = %v, want %v", tc.flags, got, tc.want)
		}
	}
}

func TestLoadNoescape(t *testing.T) {
	root := writeMod(t, "example.com/m", map[string]string{
		"shapes/shapes.gomi": `package shapes

//gom:noescape twice 0
fn twice (f: a -> a) (x: a) -> a
fn keep (f: a -> a) -> a
`,
	})
	l := New(&Config{Dir: root})
	p, err := l.Load("example.com/m/shapes", false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	twice := p.Members["twice"]
	if twice == nil {
		t.Fatal("twice member missing")
	}
	if len(twice.Escapes) != 2 || twice.Escapes[0] || !twice.Escapes[1] {
		t.Fatalf("twice.Escapes = %v, want [false true]", twice.Escapes)
	}
	if keep := p.Members["keep"]; keep == nil || keep.Escapes != nil {
		t.Fatalf("keep.Escapes = %v, want nil (assume escaping)", keep.Escapes)
	}
}

func TestLoadResultNotaint(t *testing.T) {
	root := writeMod(t, "example.com/m", map[string]string{
		"shapes/shapes.gomi": `package shapes

//gom:result-notaint twice 1
fn twice (f: a -> a) (x: a) -> a
fn keep (f: a -> a) -> a
`,
	})
	l := New(&Config{Dir: root})
	p, err := l.Load("example.com/m/shapes", false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	twice := p.Members["twice"]
	if twice == nil {
		t.Fatal("twice member missing")
	}
	if len(twice.ResultTaint) != 2 || !twice.ResultTaint[0] || twice.ResultTaint[1] {
		t.Fatalf("twice.ResultTaint = %v, want [true false]", twice.ResultTaint)
	}
	if keep := p.Members["keep"]; keep == nil || keep.ResultTaint != nil {
		t.Fatalf("keep.ResultTaint = %v, want nil (assume tainted)", keep.ResultTaint)
	}
}

func TestLoadDirectiveInsideBlockComment(t *testing.T) {
	root := writeMod(t, "example.com/m", map[string]string{
		"shapes/shapes.gomi": `package shapes

/*
//gom:noescape twice 0
//gom:result-notaint twice 1
*/
fn twice (f: a -> a) (x: a) -> a
`,
	})
	l := New(&Config{Dir: root})
	p, err := l.Load("example.com/m/shapes", false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	twice := p.Members["twice"]
	if twice == nil {
		t.Fatal("twice member missing")
	}
	if twice.Escapes != nil {
		t.Fatalf("twice.Escapes = %v, want nil — directives inside /* */ are commented out", twice.Escapes)
	}
	if twice.ResultTaint != nil {
		t.Fatalf("twice.ResultTaint = %v, want nil — directives inside /* */ are commented out", twice.ResultTaint)
	}
}

func TestLoadMutable(t *testing.T) {
	root := writeMod(t, "example.com/m", map[string]string{
		"q/q.gomi": `package q

//gom:mutable Queue
type Queue a

/*
//gom:mutable Blocked
*/
type Blocked a

//gom:mutable value
let value : Int
`,
	})
	l := New(&Config{Dir: root})
	p, err := l.Load("example.com/m/q", false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if q := p.Members["Queue"]; q == nil || !q.Mutable {
		t.Fatalf("Queue.Mutable = %v, want true — //gom:mutable Queue", q)
	}
	if b := p.Members["Blocked"]; b == nil || b.Mutable {
		t.Fatalf("Blocked.Mutable = %v, want false — directives inside /* */ are commented out", b)
	}
	if v := p.Members["value"]; v == nil || v.Mutable {
		t.Fatalf("value.Mutable = %v, want false — the directive marks type members only", v)
	}
}

func TestWorkUseHonorsGoworkFile(t *testing.T) {
	// workUse reads the go.work GOWORK actually names — the file
	// need not be `go.work`, `use` entries may be absolute, and a
	// member's `module` directive tolerates tabs and comments.
	root := t.TempDir()
	write := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ext := filepath.Join(root, "ext")
	abs := filepath.Join(root, "abs")
	write(filepath.Join(ext, "go.mod"), "module\texample.com/ext // trailing\n")
	write(filepath.Join(abs, "go.mod"), "module example.com/abs\n")
	main := filepath.Join(root, "main")
	write(filepath.Join(main, "go.mod"), "module example.com/m\n\ngo 1.27\n")
	wf := filepath.Join(root, "go.work.dev")
	write(wf, "go 1.27\n\nuse (\n\t\"./ext\"\n\t\""+filepath.ToSlash(abs)+"\"\n)\n")
	t.Setenv("GOWORK", wf)
	l := New(&Config{Dir: main})
	got := l.workUse()
	if got["example.com/ext"] != ext {
		t.Fatalf("workUse ext = %q, want %q", got["example.com/ext"], ext)
	}
	if got["example.com/abs"] != abs {
		t.Fatalf("workUse abs = %q, want %q", got["example.com/abs"], abs)
	}
}

func TestTypeDeclsForScopesToImportClosure(t *testing.T) {
	// A shared loader accumulates every loaded package's decls —
	// TypeDeclsFor answers only the closure reachable through the
	// file's own imports, so a package loaded for another file's
	// sake stays invisible to this one.
	root := writeMod(t, "example.com/m", map[string]string{
		"a/a.gomi": "package a\n\nimport \"example.com/m/b\"\n\ntype A = { b : b.B }\n",
		"b/b.gomi": "package b\n\ntype B = { v : Int }\n",
		"c/c.gomi": "package c\n\ntype C = { v : Int }\n",
	})
	l := New(&Config{Dir: root})
	if _, err := l.Load("example.com/m/a", false); err != nil {
		t.Fatalf("Load a: %v", err)
	}
	if _, err := l.Load("example.com/m/c", false); err != nil {
		t.Fatalf("Load c: %v", err)
	}
	if got := len(l.TypeDecls()); got != 3 {
		t.Fatalf("TypeDecls = %d, want 3", got)
	}
	names := func(ds []*check.TypeDecl) map[string]bool {
		out := map[string]bool{}
		for _, d := range ds {
			out[d.Sym.Package+"/"+d.Sym.Name] = true
		}
		return out
	}
	got := names(l.TypeDeclsFor([]string{"example.com/m/a"}))
	if !got["example.com/m/a/A"] || !got["example.com/m/b/B"] || got["example.com/m/c/C"] {
		t.Fatalf("TypeDeclsFor(a) = %v, want A+B without C", got)
	}
	got = names(l.TypeDeclsFor([]string{"example.com/m/c"}))
	if len(got) != 1 || !got["example.com/m/c/C"] {
		t.Fatalf("TypeDeclsFor(c) = %v, want C only", got)
	}
}
