package build_test

// The case-runner harness: a differential suite that executes every
// generated program through `go run` pays a compile+link plus the
// first-exec penalty of a brand-new binary per case (~0.3-0.9s each,
// subprocess wait — workers cannot shrink it; see
// docs/decisions/2026-11-05-test-performance.md). Instead the suite
// rewrites each generated `package main` into a library package,
// links ALL cases into a single runner binary, and executes each
// case as its own `runner <name>` process — isolation, panic class
// and exit-code semantics identical to `go run`, but one link and
// one new binary total.
//
// The rewrite is AST-based: `package main` becomes `package cNNN`
// and `func main` becomes the exported `func Run`. Everything else —
// `//line` directives included — is preserved so runtime failures
// still map back to the .gom source.

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// runnerCase is one registered case pending in the runner module.
type runnerCase struct {
	name string // subtest/dispatch name
	pkg  string // generated package name, also the import tail
}

// caseRunner accumulates rewritten sources for one scratch module and
// produces the single dispatch binary.
type caseRunner struct {
	dir   string
	cases []runnerCase
}

func newCaseRunner(t *testing.T) *caseRunner {
	t.Helper()
	dir := t.TempDir()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	gomod := "module e2e\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.0\n\nreplace github.com/podhmo/gom => " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	return &caseRunner{dir: dir}
}

// add rewrites the generated program source into a library package
// and registers it. Serial use only — build must run after every add.
func (r *caseRunner) add(t *testing.T, idx int, name string, src []byte) runnerCase {
	t.Helper()
	rc := runnerCase{name: name, pkg: fmt.Sprintf("c%03d", idx)}
	out, err := rewriteMain(src, rc.pkg)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	pkgDir := filepath.Join(r.dir, "cases", rc.pkg)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "main.go"), out, 0o644); err != nil {
		t.Fatal(err)
	}
	r.cases = append(r.cases, rc)
	return rc
}

// goImportRe finds `import go "path"` in a .gom source so the runner
// module can prewarm a shared goimport loader.
var goImportRe = regexp.MustCompile(`import go "([^"]+)"`)

// rewriteMain turns the generated `package main` / `func main` into
// `package pkg` / `func Run` so many generated programs can link into
// one runner binary.
func rewriteMain(src []byte, pkg string) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("generated source does not parse: %w", err)
	}
	f.Name.Name = pkg
	n := 0
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "main" {
			fn.Name.Name = "Run"
			n++
		}
	}
	if n != 1 {
		return nil, fmt.Errorf("expected one func main in generated source, found %d", n)
	}
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// build writes the dispatch main, builds the runner binary once, and
// returns its path. Call after every add.
func (r *caseRunner) build(t *testing.T, gobin string) string {
	t.Helper()
	cases := append([]runnerCase(nil), r.cases...)
	sort.Slice(cases, func(i, j int) bool { return cases[i].name < cases[j].name })

	var b strings.Builder
	b.WriteString("package main\n\nimport \"os\"\n")
	for _, c := range cases {
		fmt.Fprintf(&b, "import %s \"e2e/cases/%s\"\n", c.pkg, c.pkg)
	}
	b.WriteString("\nvar cases = map[string]func(){\n")
	for _, c := range cases {
		fmt.Fprintf(&b, "\t%q: %s.Run,\n", c.name, c.pkg)
	}
	b.WriteString("}\n\nfunc main() {\n\tif f, ok := cases[os.Args[1]]; ok { f(); return }\n\tos.Exit(3)\n}\n")
	runDir := filepath.Join(r.dir, "runner")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "main.go"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(r.dir, "runner.bin")
	cmd := exec.Command(gobin, "build", "-o", bin, "./runner")
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runner build failed: %v\n%s", err, out)
	}
	return bin
}

// run executes one case in its own process — the same observable
// contract as `go run .`: stdout/stderr bytes, panic class, exit code.
func (r *caseRunner) run(bin, name string) (string, error) {
	cmd := exec.Command(bin, name)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
