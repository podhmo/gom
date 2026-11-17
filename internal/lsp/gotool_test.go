package lsp

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/podhmo/gom/internal/build"
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
			if d["source"] == "go" {
				found = d
			}
		}
	}
	if found == nil {
		t.Fatalf("no go-source diagnostic published: %v", collectDiags(outs))
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
