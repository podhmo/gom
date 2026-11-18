package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/codegen"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/goimport"
	"github.com/podhmo/gom/internal/gomimport"
	"github.com/podhmo/gom/internal/iface"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/resolve"
	gomstd "github.com/podhmo/gom/internal/stdlib"
)

// goToolCompileOptions replicates cmd/gom's compileOptions for one
// package compile, the same shape the CompileCost test uses — the
// checkOnly half flips with the -go flag the CLI wires.
func goToolCompileOptions(firstPath string, checkOnly bool) *build.Options {
	d := filepath.Dir(firstPath)
	if abs, err := filepath.Abs(d); err == nil {
		d = abs
	}
	gl := goimport.New(&goimport.Config{Dir: d})
	mods := map[string]map[string]*resolve.Symbol{}
	ml := gomimport.New(&gomimport.Config{Dir: d, Go: gl, Sigs: gl, Modules: mods})
	return gomstd.Apply(&build.Options{
		Loader:         ml,
		Sigs:           ml,
		Modules:        mods,
		Lower:          &lower.Env{GoSig: gl.GoSigOf, Types: ml, FuncArity: ml.ArityOf},
		Exports:        iface.ExportNames,
		SelfImportPath: ml.EntryPath(),
		CheckOnly:      checkOnly,
	})
}

// writeModFile writes a go.mod requiring the toolchain's own module
// through a replace pointing at this worktree — the recipe the
// other go-tool tests use so generated `github.com/podhmo/gom/...`
// imports resolve offline.
func writeModFile(t *testing.T, dir string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	gomod := "module e2e\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.0\n\nreplace github.com/podhmo/gom => " + root + "\n"
	writeFile(t, filepath.Join(dir, "go.mod"), gomod)
}

// collectDiags gathers every publishDiagnostics notification's
// (uri, diagnostics) from a session's outputs.
func collectDiags(outs []map[string]any) map[string][]map[string]any {
	got := map[string][]map[string]any{}
	for _, o := range outs {
		if o["method"] != "textDocument/publishDiagnostics" {
			continue
		}
		params, _ := o["params"].(map[string]any)
		uri, _ := params["uri"].(string)
		var ds []map[string]any
		for _, d := range params["diagnostics"].([]any) {
			ds = append(ds, d.(map[string]any))
		}
		got[uri] = ds
	}
	return got
}

// TestGoToolFoldsBuildError exercises `gom lsp -go`: the package
// compiles clean in gom yet `go build` rejects the sibling .go —
// whose //line names the .gom input, the wave-14 fixture class —
// and the folded diagnostic lands on the .gom span marked
// source "go". A follow-up didChange whose package no longer
// builds the broken file retracts the set.
func TestGoToolFoldsBuildError(t *testing.T) {
	if testing.Short() {
		t.Skip("go-tool pass runs the real go toolchain")
	}
	dir := t.TempDir()
	writeModFile(t, dir)
	gomSrc := "package main\n\nfn main () -> Unit = ()\n"
	writeFile(t, filepath.Join(dir, "a.gom"), gomSrc)
	writeFile(t, filepath.Join(dir, "z.go"),
		"package main\n\n//line a.gom:2\nvar _ int = \"not-an-int\"\n")
	uriA := "file://" + filepath.Join(dir, "a.gom")

	compile := func(inputs []build.Input) ([]*build.Result, error) {
		return build.CompileFiles(inputs,
			goToolCompileOptions(inputs[0].Path, false))
	}
	srv := New(compile)
	srv.GoTool = NewGoTool()

	_, outs := runServer(t, srv,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uriA, gomSrc, 1),
	)
	var found map[string]any
	for uri, ds := range collectDiags(outs) {
		if uri != uriA {
			continue
		}
		for _, d := range ds {
			if d["source"] != "go" {
				continue
			}
			// `-go` runs build AND vet — vet re-reports the same
			// compile failure at a differently folded position, so
			// pick the entry the //line directive lands on.
			rng, _ := d["range"].(map[string]any)
			st, _ := rng["start"].(map[string]any)
			if st["line"].(float64) == 1 {
				found = d
			}
		}
	}
	if found == nil {
		t.Fatalf("no go-source diagnostic folded to .gom line 2: %v", collectDiags(outs))
	}
	if !strings.Contains(found["message"].(string), "not-an-int") &&
		!strings.Contains(found["message"].(string), "string") {
		t.Fatalf("go diagnostic message: %v", found["message"])
	}
	rng := found["range"].(map[string]any)
	start := rng["start"].(map[string]any)
	if start["line"].(float64) != 1 {
		t.Fatalf("go diagnostic folded to line %v, want .gom line 2: %v",
			start["line"], found)
	}

	// Fix the sibling and recompile — the go set retracts with it.
	writeFile(t, filepath.Join(dir, "z.go"), "package main\n")
	_, outs = runServer(t, srv,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uriA, gomSrc, 1),
		didChangeMsg(uriA, gomSrc+"\n", 2),
	)
	for uri, ds := range collectDiags(outs) {
		if uri != uriA {
			continue
		}
		for _, d := range ds {
			if d["source"] == "go" {
				t.Fatalf("stale go diagnostic after fix: %v", d)
			}
		}
	}
}

// TestGoToolFoldsVetError: `gom lsp -go` runs `go vet` beside
// `go build` — a sibling .go that compiles clean but trips vet
// (Printf arg count) lands as a go-source diagnostic folded back
// onto the .gom span its //line names.
func TestGoToolFoldsVetError(t *testing.T) {
	if testing.Short() {
		t.Skip("go-tool pass runs the real go toolchain")
	}
	dir := t.TempDir()
	writeModFile(t, dir)
	gomSrc := "package main\n\nfn main () -> Unit = ()\n"
	writeFile(t, filepath.Join(dir, "a.gom"), gomSrc)
	writeFile(t, filepath.Join(dir, "z.go"),
		"package main\n\nimport \"fmt\"\n\n//line a.gom:2\nvar _ = fmt.Sprintf(\"%d %d\", 1)\n")
	gen := []byte("package main\n\nfunc main() {}\n")
	ds := NewGoTool()(
		[]build.Input{{Path: filepath.Join(dir, "a.gom"),
			Src: []byte(gomSrc)}},
		[]*build.Result{fakeResult(gen)})
	if len(ds) == 0 {
		t.Fatal("no diagnostics — vet finding did not fold")
	}
	var sawVet bool
	for _, d := range ds {
		if d.Source == diag.SourceGo {
			sawVet = true
		}
	}
	if !sawVet {
		t.Fatalf("no go-source diagnostic: %v", ds)
	}
}

// TestGoToolSkipsStaleTestSibling: staging skips a *_test.go that
// carries the generated marker — this pass only regenerates
// production Go, so a generated test file left over from an
// earlier `gom test` is stale by construction (references renamed
// decls) and must not feed `go vet`. A handwritten *_test.go
// sibling is real package content: it stages, and its vet
// findings surface.
func TestGoToolSkipsStaleTestSibling(t *testing.T) {
	if testing.Short() {
		t.Skip("go-tool pass runs the real go toolchain")
	}
	dir := t.TempDir()
	writeModFile(t, dir)
	gomSrc := "package main\n\nfn main () -> Unit = ()\n"
	writeFile(t, filepath.Join(dir, "a.gom"), gomSrc)
	writeFile(t, filepath.Join(dir, "z_test.go"),
		"// Code generated by gom. DO NOT EDIT.\n\npackage main\n\nimport \"testing\"\n\nfunc TestStale(t *testing.T) { renamedAway() }\n")
	writeFile(t, filepath.Join(dir, "h_test.go"),
		"package main\n\nimport \"testing\"\n\nfunc TestHandwritten(t *testing.T) { alsoRenamedAway() }\n")
	gen := []byte("package main\n\nfunc main() {}\n")
	ds := NewGoTool()(
		[]build.Input{{Path: filepath.Join(dir, "a.gom"),
			Src: []byte(gomSrc)}},
		[]*build.Result{fakeResult(gen)})
	if len(ds) != 1 || ds[0].Source != diag.SourceGo ||
		!strings.Contains(ds[0].Message, "alsoRenamedAway") {
		t.Fatalf("want exactly the handwritten sibling's vet finding, got %v", ds)
	}
}

// TestGoToolStagesTestCompanion: `gom test` writes the package's
// `@test` helpers and TestXxx decls into x_test.go — staging must
// include that fresh TestSource, or a handwritten *_test.go that
// reaches for an @test helper reports it as undefined while the
// real `go test` passes.
func TestGoToolStagesTestCompanion(t *testing.T) {
	if testing.Short() {
		t.Skip("go-tool pass runs the real go toolchain")
	}
	dir := t.TempDir()
	writeModFile(t, dir)
	gomSrc := "package main\n\nfn main () -> Unit = ()\n"
	writeFile(t, filepath.Join(dir, "a.gom"), gomSrc)
	writeFile(t, filepath.Join(dir, "h_test.go"),
		"package main\n\nimport \"testing\"\n\nfunc TestHandwritten(t *testing.T) { _ = testHelper() }\n")
	gen := []byte("package main\n\nfunc main() {}\n")
	testSrc := []byte("package main\n\nimport \"testing\"\n\nfunc testHelper() int64 { return 0 }\n\nfunc TestGom(t *testing.T) { _ = testHelper() }\n")
	res := &build.Result{Output: &codegen.Output{Source: gen, TestSource: testSrc}}
	ds := NewGoTool()(
		[]build.Input{{Path: filepath.Join(dir, "a.gom"),
			Src: []byte(gomSrc)}},
		[]*build.Result{res})
	for _, d := range ds {
		if d.Source == diag.SourceGo {
			t.Fatalf("unexpected go diagnostic on valid test companion use: %v", d)
		}
	}
}

// TestGoToolSilentOnClean: the pass publishes nothing when go build
// is happy — the flag's cost is latency, not noise.
func TestGoToolSilentOnClean(t *testing.T) {
	if testing.Short() {
		t.Skip("go-tool pass runs the real go toolchain")
	}
	dir := t.TempDir()
	writeModFile(t, dir)
	gomSrc := "package main\n\nfn main () -> Unit = ()\n"
	writeFile(t, filepath.Join(dir, "a.gom"), gomSrc)
	uriA := "file://" + filepath.Join(dir, "a.gom")

	compile := func(inputs []build.Input) ([]*build.Result, error) {
		return build.CompileFiles(inputs,
			goToolCompileOptions(inputs[0].Path, false))
	}
	srv := New(compile)
	srv.GoTool = NewGoTool()

	_, outs := runServer(t, srv,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uriA, gomSrc, 1),
	)
	for _, ds := range collectDiags(outs) {
		for _, d := range ds {
			if d["source"] == "go" {
				t.Fatalf("unexpected go diagnostic on clean package: %v", d)
			}
		}
	}
}

// TestGoToolSkipsOnGomError: while the gom compile reports errors
// the pass stages nothing — partial generated output would emit
// go-tool noise over the real gom diagnostics.
func TestGoToolSkipsOnGomError(t *testing.T) {
	if testing.Short() {
		t.Skip("go-tool pass runs the real go toolchain")
	}
	dir := t.TempDir()
	writeModFile(t, dir)
	bad := "package main\n\nfn main () -> Int = \"no\"\n"
	writeFile(t, filepath.Join(dir, "a.gom"), bad)
	uriA := "file://" + filepath.Join(dir, "a.gom")

	compile := func(inputs []build.Input) ([]*build.Result, error) {
		return build.CompileFiles(inputs,
			goToolCompileOptions(inputs[0].Path, false))
	}
	srv := New(compile)
	srv.GoTool = NewGoTool()

	_, outs := runServer(t, srv,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uriA, bad, 1),
	)
	gotGom := false
	for _, ds := range collectDiags(outs) {
		for _, d := range ds {
			if d["source"] == "go" {
				t.Fatalf("go diagnostic staged on a broken unit: %v", d)
			}
			if d["source"] == "gom" {
				gotGom = true
			}
		}
	}
	if !gotGom {
		t.Fatal("gom diagnostics missing on the broken unit")
	}
}

// TestFindModuleTabDirective: go.mod whitespace is Fields, not a
// literal "module " prefix — `module\te2e` governs the package.
func TestFindModuleTabDirective(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module\te2e\n\ngo 1.27\n")
	root, path, err := findModule(dir)
	if err != nil || root != dir || path != "e2e" {
		t.Fatalf("findModule = (%q, %q, %v), want (%q, e2e, nil)",
			root, path, err, dir)
	}
}

func fakeResult(gen []byte) *build.Result {
	return &build.Result{Output: &codegen.Output{Source: gen}}
}

// TestGoToolSelfModuleGoPackage: a generated import of a same-module
// Go package that exists on disk is not a sibling Gom package —
// inside gom's own repo every generated file imports <mod>/rt, so a
// blanket prefix check would silence the pass exactly where its
// authors work.
func TestGoToolSelfModuleGoPackage(t *testing.T) {
	if testing.Short() {
		t.Skip("go-tool pass runs the real go toolchain")
	}
	dir := t.TempDir()
	writeModFile(t, dir)
	helper := filepath.Join(dir, "helper")
	if err := os.MkdirAll(helper, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(helper, "helper.go"),
		"package helper\n\nvar _ int = \"broken\"\n")
	gen := []byte("package main\n\nimport _ \"e2e/helper\"\n\nfunc main() {}\n")
	ds := NewGoTool()(
		[]build.Input{{Path: filepath.Join(dir, "a.gom"),
			Src: []byte("package main\n")}},
		[]*build.Result{fakeResult(gen)})
	if len(ds) == 0 {
		t.Fatal("no diagnostics — same-module Go import skipped the pass")
	}
}

// TestGoToolSkipsGomSibling pins the half of the self-import check
// that must stay: a same-module import resolving to a directory
// with only .gom sources has no compiled form — skip, don't
// fabricate import errors.
func TestGoToolSkipsGomSibling(t *testing.T) {
	dir := t.TempDir()
	writeModFile(t, dir)
	sibling := filepath.Join(dir, "other")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sibling, "other.gom"), "package other\n")
	gen := []byte("package main\n\nimport _ \"e2e/other\"\n\nfunc main() {}\n")
	ds := NewGoTool()(
		[]build.Input{{Path: filepath.Join(dir, "a.gom"),
			Src: []byte("package main\n")}},
		[]*build.Result{fakeResult(gen)})
	if len(ds) != 0 {
		t.Fatalf("expected silent skip for gom sibling, got %v", ds)
	}
}

// TestGoToolCopiesEmbedAssets: a sibling .go file's //go:embed
// assets are part of the package — staging must copy them or a
// clean package fails the staged build.
func TestGoToolCopiesEmbedAssets(t *testing.T) {
	if testing.Short() {
		t.Skip("go-tool pass runs the real go toolchain")
	}
	dir := t.TempDir()
	writeModFile(t, dir)
	writeFile(t, filepath.Join(dir, "z.go"),
		"package main\n\nimport _ \"embed\"\n\n//go:embed data.txt\nvar data string\n")
	writeFile(t, filepath.Join(dir, "data.txt"), "x")
	gen := []byte("package main\n\nfunc main() {}\n")
	ds := NewGoTool()(
		[]build.Input{{Path: filepath.Join(dir, "a.gom"),
			Src: []byte("package main\n")}},
		[]*build.Result{fakeResult(gen)})
	if len(ds) != 0 {
		t.Fatalf("embed asset missing from stage — false build error: %v", ds)
	}
}

// TestGoToolSkipsMixedGomSibling: a directory mixing .gom sources
// with handwritten .go helpers is still a Gom package — the
// helpers don't supply the generated implementations, so the pass
// must skip rather than publish false undefined-symbol errors.
func TestGoToolSkipsMixedGomSibling(t *testing.T) {
	dir := t.TempDir()
	writeModFile(t, dir)
	sibling := filepath.Join(dir, "other")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sibling, "other.gom"), "package other\n")
	writeFile(t, filepath.Join(sibling, "helper.go"),
		"package other\n\nfunc Helper() {}\n")
	gen := []byte("package main\n\nimport \"e2e/other\"\n\nvar _ = other.F\n\nfunc main() {}\n")
	ds := NewGoTool()(
		[]build.Input{{Path: filepath.Join(dir, "a.gom"),
			Src: []byte("package main\n")}},
		[]*build.Result{fakeResult(gen)})
	if len(ds) != 0 {
		t.Fatalf("expected silent skip for mixed gom sibling, got %v", ds)
	}
}

// TestCopyEmbedAssetsRejectsTraversal: a `../` pattern — invalid to
// the compiler anyway — must not let the stage touch files outside
// itself before `go build` rejects the directive.
func TestCopyEmbedAssetsRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "pkg")
	dstDir := filepath.Join(root, "stage")
	victim := filepath.Join(root, "payload.txt")
	for _, d := range []string{srcDir, dstDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, victim, "keep me")
	writeFile(t, filepath.Join(srcDir, "evil.txt"), "attacker")
	copyEmbedAssets(srcDir, dstDir, "../payload.txt")
	if data, err := os.ReadFile(victim); err != nil ||
		string(data) != "keep me" {
		t.Fatalf("traversal pattern overwrote a workspace file: %s", data)
	}
	if _, err := os.ReadFile(filepath.Join(dstDir, "payload.txt")); err == nil {
		t.Fatal("traversal pattern copied into the stage")
	}
}

// TestGoToolNestedInternalImport: staging at the module root made
// a package's legal sub/internal import unbuildable — the stage
// lives inside the package's own directory so its internal
// visibility matches the real one.
func TestGoToolNestedInternalImport(t *testing.T) {
	if testing.Short() {
		t.Skip("go-tool pass runs the real go toolchain")
	}
	root := t.TempDir()
	writeModFile(t, root)
	pkg := filepath.Join(root, "sub", "pkg")
	in := filepath.Join(root, "sub", "internal", "x")
	for _, d := range []string{pkg, in} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(in, "x.go"), "package x\n\nconst X = 1\n")
	writeFile(t, filepath.Join(pkg, "z.go"),
		"package main\n\nimport _ \"e2e/sub/internal/x\"\n")
	gen := []byte("package main\n\nfunc main() {}\n")
	ds := NewGoTool()(
		[]build.Input{{Path: filepath.Join(pkg, "a.gom"),
			Src: []byte("package main\n")}},
		[]*build.Result{fakeResult(gen)})
	if len(ds) != 0 {
		t.Fatalf("legal sub/internal import rejected by stage placement: %v", ds)
	}
}

// TestGoToolCost logs the wall time of the -go pass's two added
// stages — full compile instead of CheckOnly plus the staged
// `go build` — so the decision's latency figure comes from a real
// run, not the wave-14 estimate. Run with -v and without -short.
func TestGoToolCost(t *testing.T) {
	if testing.Short() {
		t.Skip("cost measurement runs the real go toolchain")
	}
	dir := t.TempDir()
	writeModFile(t, dir)
	gomSrc := "package main\n\nimport go \"fmt\"\n\nfn main () -> Unit = {\n  ignore fmt.Println \"x\"\n  ()\n}\n"
	writeFile(t, filepath.Join(dir, "a.gom"), gomSrc)
	uriA := "file://" + filepath.Join(dir, "a.gom")

	var compileMs, toolMs []int64
	compile := func(inputs []build.Input) ([]*build.Result, error) {
		start := time.Now()
		rs, err := build.CompileFiles(inputs,
			goToolCompileOptions(inputs[0].Path, false))
		compileMs = append(compileMs, time.Since(start).Milliseconds())
		return rs, err
	}
	srv := New(compile)
	base := NewGoTool()
	srv.GoTool = func(inputs []build.Input, results []*build.Result) []diag.Diagnostic {
		start := time.Now()
		ds := base(inputs, results)
		toolMs = append(toolMs, time.Since(start).Milliseconds())
		return ds
	}

	_, outs := runServer(t, srv,
		requestMsg(1, "initialize", map[string]any{}),
		didOpenMsg(uriA, gomSrc, 1),
		didChangeMsg(uriA, strings.Replace(gomSrc, `"x"`, `"y"`, 1), 2),
		didChangeMsg(uriA, strings.Replace(gomSrc, `"x"`, `"z"`, 1), 3),
	)
	if len(outs) == 0 || len(compileMs) != 3 || len(toolMs) != 3 {
		t.Fatalf("runs: outs=%d compiles=%d tools=%d",
			len(outs), len(compileMs), len(toolMs))
	}
	t.Logf("full compile: open=%dms change1=%dms change2=%dms",
		compileMs[0], compileMs[1], compileMs[2])
	t.Logf("staged go build: open=%dms change1=%dms change2=%dms",
		toolMs[0], toolMs[1], toolMs[2])
}
