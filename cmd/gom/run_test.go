package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const runMainGom = `package main

import go "fmt"
import go "os"

fn main () -> Unit = {
  let _ = fmt.Println "argc" (List.length (Go.conv os.Args : List String))
  os.Exit (if List.length (Go.conv os.Args : List String) > 2 { 7 } else { 0 })
  ()
}
`

// TestRunExecutes: `gom run` compiles, links and execs the package —
// program output reaches stdout unadorned, args after "--" reach
// argv, and the child's exit code is the command's.
func TestRunExecutes(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: go build + exec, skipping in short mode")
	}
	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{"m.gom": runMainGom})
	testModule(t, dir)

	code, out, _ := invoke("run", filepath.Join(dir, "m.gom"), "--", "a", "b")
	if code != 7 {
		t.Fatalf("exit = %d, want 7 (the program's own)", code)
	}
	if !strings.Contains(out, "argc 3") {
		t.Fatalf("stdout = %q, want program output (binary + a + b = argc 3)", out)
	}
}

// TestRunModuleLocalDependency: `gom run` discovers a same-module
// .gom import transitively, emits that package beside its sources,
// and links the single user-facing main package in one invocation.
func TestRunModuleLocalDependency(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: go build + exec, skipping in short mode")
	}
	t.Parallel()

	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	copyFixture(t, filepath.Join(root, "testdata", "multifile"), dir)
	if !goAtLeast(gobin, dir, 1, 27) {
		t.Skip("fixture requires go >= 1.27 (testdata/multifile/go.mod)")
	}
	gomodPath := filepath.Join(dir, "go.mod")
	gomod, err := os.ReadFile(gomodPath)
	if err != nil {
		t.Fatal(err)
	}
	gomod = []byte(strings.ReplaceAll(string(gomod), "=> ../..", "=> "+root))
	if err := os.WriteFile(gomodPath, gomod, 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errout := invoke("run", filepath.Join(dir, "gomapp"))
	if code != 0 {
		t.Fatalf("gom run: exit %d; stdout=%q stderr=%q", code, out, errout)
	}
	want := "12.56636\ncircle 4\n2\n6.28318\n[]\n[]\n7\n"
	if out != want {
		t.Fatalf("gom run stdout = %q, want %q", out, want)
	}
	for _, rel := range []string{"shapes/shapes.go", "shapes/ops.go", "gomapp/main.go"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("expected generated file %s: %v", rel, err)
		}
	}
}

// TestRunCompileFailure: a .gom that fails checking reports the
// usual message stream and does not run.
func TestRunCompileFailure(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{
		"m.gom": "package main\n\nfn main () -> Unit = oops\n",
	})
	testModule(t, dir)

	code, out, _ := invoke("run", filepath.Join(dir, "m.gom"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	var sawDiag, sawSummary bool
	for _, l := range jsonLines(t, out) {
		if l["kind"] == "diagnostic" {
			sawDiag = true
		}
		if l["kind"] == "summary" {
			sawSummary = true
			if l["ok"] != false {
				t.Fatalf("summary ok = %v", l["ok"])
			}
		}
	}
	if !sawDiag || !sawSummary {
		t.Fatalf("stream missing diagnostic/summary: %q", out)
	}
}

// TestRunNotMain: a package that is not `package main` is refused
// before the link step — `go run` has the same restriction.
func TestRunNotMain(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: gom info + compile, skipping in short mode")
	}
	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{
		"lib/f.gom": "package lib\n\nfn f () -> Int = 1\n",
	})
	testModule(t, dir)
	// Non-main packages carry a .gomi contract — draft it so the
	// compile itself is clean and the refusal is the main check.
	if code, out, _ := invoke("info", filepath.Join(dir, "lib", "f.gom")); code != 0 {
		t.Fatalf("gom info exit = %d: %q", code, out)
	}

	code, _, errOut := invoke("run", filepath.Join(dir, "lib", "f.gom"))
	if code != 2 {
		t.Fatalf("exit = %d, want 2; err=%q", code, errOut)
	}
	if !strings.Contains(errOut, "not a package main") {
		t.Fatalf("stderr = %q", errOut)
	}
}

// TestRunMultipleDirs: inputs spanning several directories name
// several packages — refused like `go run` refusing ./a ./b.
func TestRunMultipleDirs(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{
		"a/m.gom": "package main\n\nfn main () -> Unit = ()\n",
		"b/m.gom": "package main\n\nfn main () -> Unit = ()\n",
	})

	code, _, errOut := invoke("run", filepath.Join(dir, "a"), filepath.Join(dir, "b"))
	if code != 2 {
		t.Fatalf("exit = %d, want 2; err=%q", code, errOut)
	}
	if !strings.Contains(errOut, "single package directory") {
		t.Fatalf("stderr = %q", errOut)
	}
}

// TestRunBuildErrorFoldsSpan: a package gom accepts but `go build`
// rejects — via a sibling Go file whose //line directive names the
// .gom input — folds the link failure back onto the .gom span, the
// same fold `gom build -go` uses.
func TestRunBuildErrorFoldsSpan(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: go build invocation, skipping in short mode")
	}
	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{
		"m.gom": "package main\n\nfn f () -> Int = 1\nfn main () -> Unit = { ignore (f ()); () }\n",
		"z.go":  "package main\n\n//line m.gom:2\nvar _ int = \"not-an-int\"\n",
	})
	testModule(t, dir)

	code, out, _ := invoke("run", filepath.Join(dir, "m.gom"))
	if code == 0 {
		t.Fatalf("exit = %d, want failure; out=%q", code, out)
	}
	var saw bool
	for _, l := range jsonLines(t, out) {
		if l["kind"] != "diagnostic" {
			continue
		}
		if l["source"] != "go" {
			continue
		}
		span, _ := l["span"].(map[string]any)
		if span == nil || span["file"] != filepath.Join(dir, "m.gom") {
			t.Fatalf("diagnostic lost the .gom span: %v", l)
		}
		saw = true
	}
	if !saw {
		t.Fatalf("no go-source diagnostic folded: %q", out)
	}
}

// TestRunNativeScope: Scope bodies lowered to native Go defers (SC-3)
// behave like their runtime-stack twins — each `_rt` function first
// hands its Scope to a helper, which keeps SC-1's rt.Scope path.
func TestRunNativeScope(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: go build + exec, skipping in short mode")
	}
	t.Parallel()

	haveGo(t)
	src, err := os.ReadFile(filepath.Join("testdata", "native_scope.gom"))
	if err != nil {
		t.Fatal(err)
	}
	dir := writeFiles(t, map[string]string{"m.gom": string(src)})
	testModule(t, dir)

	code, out, errout := invoke("run", filepath.Join(dir, "m.gom"))
	if code != 0 {
		t.Fatalf("exit = %d; stderr=%q stdout=%q", code, errout, out)
	}
	want := "body\nb\na\n1\nbody\na\n1\nplain\nerr e2\ne1\nrollback\nerr body\nerr close\n4\n2\n"
	if out != "== native\n"+want+"== runtime\n"+want+"== fn result\n41 42 43\n" {
		t.Errorf("stdout = %q, want both sections %q", out, want)
	}
}

// TestRunOutsideModuleSynthesizesScratch: with no enclosing go.mod,
// `gom run` still compiles and executes by supplying an invocation-scoped
// scratch module. The temporary go.mod is removed afterward.
func TestRunOutsideModuleSynthesizesScratch(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: go build + exec, skipping in short mode")
	}
	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{
		"m.gom": `package main

import go "fmt"

fn main () -> Unit = {
  ignore (fmt.Println "outside")
  ()
}
`,
	})

	code, out, errout := invoke("run", filepath.Join(dir, "m.gom"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%q stderr=%q", code, out, errout)
	}
	if out != "outside\n" {
		t.Fatalf("stdout = %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); !os.IsNotExist(err) {
		t.Fatalf("scratch go.mod remained or stat failed: %v", err)
	}
}

// TestRunOutsideModuleLocalDependency: the scratch module is rooted at
// the run package directory. Imports under its fixed module path resolve
// module-local packages just as they do in a real module.
func TestRunOutsideModuleLocalDependency(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: go build + exec, skipping in short mode")
	}
	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{
		"m.gom": `package main

import go "fmt"
import "gom.local/scratch/lib"

fn main () -> Unit = {
  ignore (fmt.Println (lib.answer ()))
  ()
}
`,
		"lib/lib.gom": `package lib

fn answer () -> Int = 42
`,
		"lib/lib.gomi": `package lib

fn answer () -> Int
`,
	})

	code, out, errout := invoke("run", filepath.Join(dir, "m.gom"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%q stderr=%q", code, out, errout)
	}
	if out != "42\n" {
		t.Fatalf("stdout = %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "lib", "lib.go")); err != nil {
		t.Fatalf("expected generated dependency output: %v", err)
	}
}

// TestInitCreatesTemplates: `gom init` creates a module file carrying the
// requested path and a runnable main.gom template.
func TestInitCreatesTemplates(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{})
	code, out, errout := invoke("init", "-dir", dir, "example.com/hello")
	if code != 0 {
		t.Fatalf("exit = %d; stdout=%q stderr=%q", code, out, errout)
	}
	gomod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gomod), "module example.com/hello") {
		t.Fatalf("go.mod = %q", gomod)
	}
	mainGom, err := os.ReadFile(filepath.Join(dir, "main.gom"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mainGom), "package main") {
		t.Fatalf("main.gom = %q", mainGom)
	}
}

// TestInitDoesNotOverwrite: existing files are inputs the user may have
// edited. init fills only the missing side and leaves existing bytes alone.
func TestInitDoesNotOverwrite(t *testing.T) {
	t.Parallel()

	sentinel := "package main\n\nfn main () -> Unit = ()\n"
	dir := writeFiles(t, map[string]string{"main.gom": sentinel})
	code, _, errout := invoke("init", "-dir", dir, "example.com/hello")
	if code != 0 {
		t.Fatalf("exit = %d; stderr=%q", code, errout)
	}
	mainGom, err := os.ReadFile(filepath.Join(dir, "main.gom"))
	if err != nil {
		t.Fatal(err)
	}
	if string(mainGom) != sentinel {
		t.Fatalf("main.gom overwritten: %q", mainGom)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		t.Fatalf("go.mod not created: %v", err)
	}
}

// TestInitTemplateRuns: the generated pair is not just a textual template —
// it is a runnable project under the current toolchain.
func TestInitTemplateRuns(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: go build + exec, skipping in short mode")
	}
	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{})
	if code, out, errout := invoke("init", "-dir", dir, "example.com/hello"); code != 0 {
		t.Fatalf("gom init exit = %d; stdout=%q stderr=%q", code, out, errout)
	}
	code, out, errout := invoke("run", filepath.Join(dir, "main.gom"))
	if code != 0 {
		t.Fatalf("gom run exit = %d; stdout=%q stderr=%q", code, out, errout)
	}
	if out != "hello\n" {
		t.Fatalf("stdout = %q", out)
	}
}
