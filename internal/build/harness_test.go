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
	gl := goimport.New(&goimport.Config{Dir: "../.."})
	return gomstd.Apply(&build.Options{
		Loader:  gl,
		Sigs:    gl,
		Lower:   &lower.Env{GoSig: gl.GoSigOf},
		Exports: iface.ExportNames,
	})
}

// specCompile drives the full pipeline — the superset of what
// `gom check` reports — and returns the collected diagnostics. A
// case's files compile as one package unit (build.CompileFiles), so
// directory cases exercise the same shared scope and unit checks the
// CLI gives a real package; a single-file case degenerates to
// build.Compile.
func specCompile(fs []gomtest.File) ([]diag.Diagnostic, error) {
	ins := make([]build.Input, len(fs))
	for i, f := range fs {
		ins[i] = build.Input{Path: f.Path, Src: f.Src}
	}
	rs, err := build.CompileFiles(ins, suiteOptions())
	if err != nil {
		return nil, err
	}
	var out []diag.Diagnostic
	for _, r := range rs {
		out = append(out, r.Diagnostics...)
	}
	return out, nil
}

// goldenGenerate drives the pipeline to codegen and returns the
// generated package source (the _test.go half is codegen's own
// suite's concern).
func goldenGenerate(f gomtest.File) ([]byte, error) {
	r, err := build.Compile(build.Input{Path: f.Path, Src: f.Src}, suiteOptions())
	if err != nil {
		return nil, err
	}
	if r.Output == nil {
		return nil, fmt.Errorf("no output: %d diagnostic(s)", len(r.Diagnostics))
	}
	return r.Output.Source, nil
}

func TestSpec(t *testing.T) {
	gomtest.RunSpecTests(t, "", specCompile)
}

func TestGolden(t *testing.T) {
	gomtest.RunGoldenTests(t, "", goldenGenerate)
}
