package build_test

import (
	"fmt"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/goimport"
	"github.com/podhmo/gom/internal/gomtest"
	"github.com/podhmo/gom/internal/lower"
	// Keep the named-interface golden helper in the Go test dependency graph.
	_ "github.com/podhmo/gom/testdata/goiface"
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
	return &build.Options{
		Loader: gl,
		Sigs:   gl,
		Lower:  &lower.Env{GoSig: gl.GoSigOf},
	}
}

// specCompile drives the full pipeline — the superset of what
// `gom check` reports — and returns the collected diagnostics.
func specCompile(f gomtest.File) ([]diag.Diagnostic, error) {
	r, err := build.Compile(build.Input{Path: f.Path, Src: f.Src}, suiteOptions())
	if err != nil {
		return nil, err
	}
	return r.Diagnostics, nil
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
