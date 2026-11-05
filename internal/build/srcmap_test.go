package build_test

// Golden-suite source-map sweep (P4-5, docs/design.md section 2.2):
// every testdata/golden case is compiled through the real pipeline
// and its exported anchor table is checked against the generated
// file's own //line directives under go/token — the same reading
// the Go toolchain applies. This pins `//line` precision on real
// inputs, complementing the synthetic-span checks in codegen.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/goimport"
)

func TestGoldenSourceMap(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 1.02)
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := filepath.Glob(filepath.Join(root, "testdata", "golden", "*.gom"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("no golden cases under testdata/golden: %v", err)
	}
	// One serial suite shares one loader: the first `import go` pays
	// `go list -export`, every later case hits the package cache.
	gl := goimport.New(&goimport.Config{Dir: "../.."})
	for _, path := range entries {
		name := filepath.Base(path)
		t.Run(strings.TrimSuffix(name, ".gom"), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			r, err := build.Compile(build.Input{Path: name, Src: src}, suiteOptionsWith(gl))
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			if r.Output == nil {
				t.Fatalf("no output: %v", r.Diagnostics)
			}
			checkMap(t, name, src, r.Output.Source, r.Output.SourceMap)
			if len(r.Output.TestSource) != 0 {
				if r.Output.TestSourceMap == nil {
					t.Error("test source emitted without TestSourceMap")
				} else {
					checkMap(t, name, src, r.Output.TestSource, r.Output.TestSourceMap)
				}
			}
		})
	}
}

// checkMap asserts sm agrees with src's own //line directives: one
// anchor per directive, and each anchored line resolves under
// go/token to the anchor span's file and start line.
func checkMap(t *testing.T, name string, gomSrc, gen []byte, sm *diag.SourceMap) {
	t.Helper()
	if sm == nil {
		t.Fatal("Output.SourceMap is not set")
	}
	directives := 0
	for _, l := range strings.Split(string(gen), "\n") {
		if strings.HasPrefix(l, "//line ") {
			directives++
		}
	}
	if directives != len(sm.Anchors) {
		t.Errorf("%d anchors for %d //line directives", len(sm.Anchors), directives)
	}
	li := diag.NewLineIndex(gomSrc)

	fset := token.NewFileSet()
	gf, err := parser.ParseFile(fset, "gen.go", gen, parser.ParseComments)
	if err != nil {
		t.Fatalf("generated source does not parse: %v", err)
	}
	tokFile := fset.File(gf.Package)

	for _, a := range sm.Anchors {
		if !a.Span.Valid() || a.Span.File != name {
			t.Errorf("anchor %d span = %v, want a %s span", a.Line, a.Span, name)
		}
		if a.Line > tokFile.LineCount() {
			t.Fatalf("anchor line %d beyond generated length", a.Line)
		}
		p := fset.Position(tokFile.LineStart(a.Line))
		want := li.Pos(a.Span.Start).Line
		if p.Filename != a.Span.File || p.Line != want {
			t.Errorf("gen line %d resolves to %s:%d, want %s:%d",
				a.Line, p.Filename, p.Line, a.Span.File, want)
		}
	}
}
