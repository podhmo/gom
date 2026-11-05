package stdlib_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/goimport"
	"github.com/podhmo/gom/internal/gomimport"
	"github.com/podhmo/gom/internal/iface"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/resolve"
	gomstd "github.com/podhmo/gom/internal/stdlib"
)

// This file is the stdlib artifact freshness gate (docs/design.md:
// `stdlib/<name>` generated `.go`/`_test.go` are gom test artifacts
// committed to the repo). Every `stdlib/<pkg>/*.gom` directory is
// recompiled exactly as `gom test` would compile it — the CLI's
// loader wiring plus lower.Env.Tests — and the pipeline output is
// compared byte-for-byte with the committed files. A diff means
// someone changed the .gom source or the generator without
// regenerating the artifacts: rerun `gom test stdlib/<pkg>` and
// commit the result. The test only reads the checkout; it writes
// nothing.
//
// Byte-for-byte comparison is only stable while no stdlib package
// writes `import go`: generated imports of Go packages would bake
// the goimport loader's view of the toolchain into the committed
// files, making the gate Go-version (and platform) sensitive. If a
// package ever gains `import go`, the comparison must soften to
// whatever invariance survives that — pin it here before adding
// the import.

// repoRoot resolves the repository root — this test runs with the
// package directory (internal/stdlib) as its working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

// stdlibDirs returns the stdlib package directories — every
// `stdlib/<pkg>/` that holds at least one .gom source — as pairs of
// the repo-relative directory name and its inputs, in directory
// order. Globbing keeps the gate automatic: a new stdlib package is
// gated without a table entry here.
func stdlibDirs(t *testing.T, root string) []struct {
	dir    string
	inputs []string
} {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "stdlib", "*", "*.gom"))
	if err != nil {
		t.Fatalf("glob stdlib sources: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no stdlib .gom sources found — wrong repo root?")
	}
	var out []struct {
		dir    string
		inputs []string
	}
	byDir := map[string]int{}
	for _, m := range matches {
		rel, err := filepath.Rel(root, m)
		if err != nil {
			t.Fatalf("rel %s: %v", m, err)
		}
		rel = filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(rel))
		i, ok := byDir[dir]
		if !ok {
			i = len(out)
			byDir[dir] = i
			out = append(out, struct {
				dir    string
				inputs []string
			}{dir: dir})
		}
		out[i].inputs = append(out[i].inputs, rel)
	}
	return out
}

// testCompileOptions replicates cmd/gom's compileOptions for a
// stdlib directory with lower.Env.Tests set — the `gom test` mode
// that produced the committed artifacts (the committed `.go` files
// carry the test-mode gomEq helpers `gom build` does not emit).
// Input paths stay repo-relative so emitted //line operands match
// the committed files; the filesystem-facing hooks (Exports, the
// loaders' working dir) get the real absolute directory.
func testCompileOptions(t *testing.T, root, relDir string) *build.Options {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join(root, relDir))
	if err != nil {
		t.Fatalf("abs %s: %v", relDir, err)
	}
	gl := goimport.New(&goimport.Config{Dir: dir})
	mods := map[string]map[string]*resolve.Symbol{}
	ml := gomimport.New(&gomimport.Config{Dir: dir, Go: gl, Sigs: gl, Modules: mods})
	return gomstd.Apply(&build.Options{
		Loader:  ml,
		Sigs:    ml,
		Modules: mods,
		Lower: &lower.Env{
			GoSig:     gl.GoSigOf,
			Types:     ml,
			FuncArity: ml.ArityOf,
			Tests:     true,
		},
		Exports: func(dir, pkg string) map[string]bool {
			return iface.ExportNames(filepath.Join(root, dir), pkg)
		},
		SelfImportPath: gomstd.SelfPath(relDir + "/x.gom"),
	})
}

// TestStdlibGeneratedFilesAreFresh regenerates every stdlib package
// and compares the pipeline output with the committed `<name>.go`
// and `<name>_test.go`. Run `gom test stdlib/<pkg>` and commit to
// fix a diff this reports.
func TestStdlibGeneratedFilesAreFresh(t *testing.T) {
	root := repoRoot(t)
	for _, d := range stdlibDirs(t, root) {
		t.Run(d.dir, func(t *testing.T) {
			ins := make([]build.Input, len(d.inputs))
			for i, rel := range d.inputs {
				src, err := os.ReadFile(filepath.Join(root, rel))
				if err != nil {
					t.Fatalf("read %s: %v", rel, err)
				}
				ins[i] = build.Input{Path: rel, Src: src}
			}
			rs, err := build.CompileFiles(ins, testCompileOptions(t, root, d.dir))
			if err != nil {
				t.Fatalf("compile %s: %v", d.dir, err)
			}
			for i, r := range rs {
				if r.Diagnostics.HasError() {
					var b bytes.Buffer
					ds := r.Diagnostics
					ds.Sort()
					for _, dg := range ds {
						b.WriteString(string(dg.Code) + ": " + dg.Message + "\n")
					}
					t.Fatalf("%s: compile has error diagnostics:\n%s", d.inputs[i], b.String())
				}
				if r.Output == nil {
					t.Fatalf("%s: no output despite clean diagnostics", d.inputs[i])
				}
				want := filepath.Join(root, filepath.ToSlash(d.dir),
					baseNoExt(d.inputs[i])+".go")
				compareFresh(t, want, r.Output.Source)
				testWant := filepath.Join(root, filepath.ToSlash(d.dir),
					baseNoExt(d.inputs[i])+"_test.go")
				_, statErr := os.Stat(testWant)
				switch {
				case statErr == nil:
					// A committed test artifact exists — the
					// regenerated test source must exist too
					// and match it byte-for-byte.
					if r.Output.TestSource == nil {
						t.Fatalf("%s: committed %s exists but compile produced no test source",
							d.inputs[i], filepath.Base(testWant))
					}
					compareFresh(t, testWant, r.Output.TestSource)
				case errors.Is(statErr, os.ErrNotExist):
					// No committed artifact — the regenerated
					// output must not produce one either.
					if r.Output.TestSource != nil {
						t.Fatalf("%s: compile produced test source but no committed %s exists",
							d.inputs[i], filepath.Base(testWant))
					}
				default:
					// Any other stat failure is neither
					// "present" nor "absent" — an unreadable
					// artifact must not masquerade as missing.
					t.Fatalf("stat %s: %v", testWant, statErr)
				}
			}
		})
	}
}

func baseNoExt(path string) string {
	b := filepath.Base(path)
	return b[:len(b)-len(filepath.Ext(b))]
}

// compareFresh reports a byte diff between the committed file and
// regenerated output as the first differing line with a few lines
// of context — enough to see which side drifted.
func compareFresh(t *testing.T, wantPath string, got []byte) {
	t.Helper()
	want, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read committed %s: %v", wantPath, err)
	}
	if bytes.Equal(want, got) {
		return
	}
	wl := bytes.Split(want, []byte("\n"))
	gl := bytes.Split(got, []byte("\n"))
	i := 0
	for i < len(wl) && i < len(gl) && bytes.Equal(wl[i], gl[i]) {
		i++
	}
	t.Errorf("%s differs from regenerated output at line %d\n  committed: %s\n  generated: %s\nregenerate with `gom test` and commit",
		filepath.Base(wantPath), i+1, clip(wl, i), clip(gl, i))
}

func clip(lines [][]byte, i int) string {
	if i >= len(lines) {
		return "<eof>"
	}
	s := string(lines[i])
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}
