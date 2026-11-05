package build_test

// End-to-end source-mapping coverage (P4-5): a real `go run` of
// generated code panics inside `todo ()`, the runtime stack names
// the .gom positions through //line, and ResolveStack restores the
// frames to byte-offset spans. This is the full loop of
// docs/design.md section 2.2 — the table codegen exports, the
// directives the Go toolchain reads, and the index gom uses to
// interpret them.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
)

const panicSrc = `package main

fn boom (x: Int) -> Int = todo ()

fn main () -> Unit = {
  let _ = boom 1
  ()
}
`

func haveGoBin(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}
	return bin
}

// moduleFor writes a go.mod pointing at this worktree so generated
// code resolves gom's rt/stdlib packages.
func moduleFor(t *testing.T, dir string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	gomod := "module e2e\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.0\n\nreplace github.com/podhmo/gom => " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPanicStackResolvesToGom(t *testing.T) {
	gobin := haveGoBin(t)

	dir := t.TempDir()
	// Compile the same input path the index will later resolve. Relative
	// //line paths are made absolute against the compiler's working
	// directory, which differs from the generated program's directory.
	srcPath := filepath.Join(dir, "main.gom")
	r, err := build.Compile(build.Input{Path: srcPath, Src: []byte(panicSrc)}, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if r.Output == nil || len(r.Output.Source) == 0 {
		t.Fatalf("no output: %v", r.Diagnostics)
	}
	if r.Output.SourceMap == nil {
		t.Fatal("Result.Output.SourceMap is not set")
	}

	moduleFor(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), r.Output.Source, 0o644); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	prog := filepath.Join(dir, "prog")
	buildCmd := exec.Command(gobin, "build", "-o", prog, ".")
	buildCmd.Dir = dir
	buildCmd.Env = env
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	run := exec.Command(prog)
	run.Dir = dir
	out, err := run.CombinedOutput()
	if err == nil {
		t.Fatal("the program did not panic")
	}
	trace := string(out)
	// The runtime stack already names the .gom file — that is
	// //line doing its half of the work.
	for _, want := range []string{"panic: gom: todo", "main.gom:3", "main.gom:6"} {
		if !strings.Contains(trace, want) {
			t.Fatalf("panic trace lacks %q:\n%s", want, trace)
		}
	}

	idx := build.NewGoPosIndex(dir, map[string][]byte{srcPath: []byte(panicSrc)})
	frames := idx.ResolveStack(trace)
	if len(frames) == 0 {
		t.Fatalf("no frames parsed:\n%s", trace)
	}
	li := diag.NewLineIndex([]byte(panicSrc))
	var boom, mainFrame *build.StackPos
	for i := range frames {
		switch {
		case frames[i].Func == "main.boom(0x1)" || frames[i].Func == "main.boom(...)":
			boom = &frames[i]
		case frames[i].Func == "main.main()":
			mainFrame = &frames[i]
		}
	}
	if boom == nil {
		t.Fatalf("no main.boom frame in %+v", frames)
	}
	ls, _ := li.LineBounds(3) // `fn boom ... = todo ()`
	if boom.Span != diag.Point(srcPath, ls) {
		t.Errorf("boom frame = %v, want the fn boom line head %d", boom.Span, ls)
	}
	if mainFrame == nil {
		t.Fatalf("no main.main frame in %+v", frames)
	}
	ls, _ = li.LineBounds(6) // `let _ = boom 1`
	if mainFrame.Span != diag.Point(srcPath, ls) {
		t.Errorf("main frame = %v, want line 6 head %d", mainFrame.Span, ls)
	}
}
