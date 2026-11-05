package build_test

import (
	"fmt"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/goimport"
	"github.com/podhmo/gom/internal/gomtest"
	"github.com/podhmo/gom/internal/iface"
	"github.com/podhmo/gom/internal/lower"
	gomstd "github.com/podhmo/gom/internal/stdlib"
	// Keep the named-interface golden helper in the Go test dependency graph.
	_ "github.com/podhmo/gom/testdata/goiface"
	// Generated programs import these outside the test process. Keep
	// their sources in the outer Go test result-cache dependency graph.
	_ "github.com/podhmo/gom/rt"
	_ "github.com/podhmo/gom/stdlib/list"
	_ "github.com/podhmo/gom/stdlib/option"
	_ "github.com/podhmo/gom/stdlib/result"
)

// These tests run the file-based suites under testdata/ end to end
// through the real driver. suiteOptions is shared by both suites.
//
// `import go` resolves through a real internal/goimport Loader —
// the same object fills the resolve loader, the signature oracle
// and lowering's GoSig hook, mirroring cmd/gom's wiring. Its
// `go list -export` runs in the repository root so imported packages
// resolve in this module's context.
func suiteOptions() *build.Options {
	return suiteOptionsWith(goimport.New(&goimport.Config{Dir: "../.."}))
}

// suiteOptionsWith wires the given loader the same way as
// suiteOptions. A prewarmed loader shared across cases removes the
// per-compile `go list -export` subprocess (~45ms each) that
// dominates the in-process compile phase; sharing is only safe when
// every `import go` path the sources can touch has been loaded
// beforehand (or all use is serial), because the loader's caches are
// plain maps.
func suiteOptionsWith(gl *goimport.Loader) *build.Options {
	return gomstd.Apply(&build.Options{
		Loader:  gl,
		Sigs:    gl,
		Lower:   &lower.Env{GoSig: gl.GoSigOf},
		Exports: iface.ExportNames,
	})
}

// sharedSuiteLoader returns a loader already warmed with every
// `import go` path appearing in the given sources, so subsequent
// compiles never spawn `go list -export`. Serial use only — the
// caches are plain maps.
func sharedSuiteLoader(t *testing.T, srcs ...string) *goimport.Loader {
	t.Helper()
	gl := goimport.New(&goimport.Config{Dir: "../.."})
	seen := map[string]bool{}
	for _, src := range srcs {
		for _, m := range goImportRe.FindAllStringSubmatch(src, -1) {
			path := m[1]
			if !seen[path] {
				seen[path] = true
				if _, err := gl.Load(path, true); err != nil {
					t.Fatalf("prewarm go import %q: %v", path, err)
				}
			}
		}
	}
	return gl
}

// specCompile drives the full pipeline — the superset of what
// `gom check` reports — and returns the collected diagnostics. A
// case's files compile as one package unit (build.CompileFiles), so
// directory cases exercise the same shared scope and unit checks the
// CLI gives a real package; a single-file case degenerates to
// build.Compile. The loader is taken (not created) so a whole suite
// can share one Go-import session: suites are serial, and after the
// first `import go` lookup the per-package cache removes every
// further `go list -export` subprocess.
func specCompile(gl *goimport.Loader) func([]gomtest.File) ([]diag.Diagnostic, error) {
	return func(fs []gomtest.File) ([]diag.Diagnostic, error) {
		ins := make([]build.Input, len(fs))
		for i, f := range fs {
			ins[i] = build.Input{Path: f.Path, Src: f.Src}
		}
		rs, err := build.CompileFiles(ins, suiteOptionsWith(gl))
		if err != nil {
			return nil, err
		}
		var out []diag.Diagnostic
		for _, r := range rs {
			out = append(out, r.Diagnostics...)
		}
		return out, nil
	}
}

// goldenGenerate drives the pipeline to codegen and returns the
// generated package source (the _test.go half is codegen's own
// suite's concern). Same shared-loader rationale as specCompile.
func goldenGenerate(gl *goimport.Loader) func(gomtest.File) ([]byte, error) {
	return func(f gomtest.File) ([]byte, error) {
		r, err := build.Compile(build.Input{Path: f.Path, Src: f.Src}, suiteOptionsWith(gl))
		if err != nil {
			return nil, err
		}
		if r.Output == nil {
			return nil, fmt.Errorf("no output: %d diagnostic(s)", len(r.Diagnostics))
		}
		return r.Output.Source, nil
	}
}

func TestSpec(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 3.56)
	}

	gomtest.RunSpecTests(t, "", specCompile(goimport.New(&goimport.Config{Dir: "../.."})))
}

func TestGolden(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 1.01)
	}

	gomtest.RunGoldenTests(t, "", goldenGenerate(goimport.New(&goimport.Config{Dir: "../.."})))
}
