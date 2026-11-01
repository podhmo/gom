package build_test

import (
	"fmt"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/gomtest"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// These tests run the file-based suites under testdata/ end to end
// through the real driver. suiteOptions is shared by both suites.
//
// The `go:fmt` package is stubbed until internal/goimport lands
// (P1-9): fixtures import it, and a real compiler invocation without
// a loader would report E0203. The stub gives Println the signature
// `(args: ..Go.Slice a) -> Unit` — a fresh type variable per use so
// any single element type unifies — which is what the fixtures need
// to exercise `import go` through the whole pipeline. P1-9 should
// replace it with the real loader; fixtures that rely on the real
// (int, error) result shape will need updating then.
func suiteOptions() *build.Options {
	printlnSym := &resolve.Symbol{
		Name: "Println", Kind: resolve.KindFunc, IsGo: true, Package: "fmt",
	}
	return &build.Options{
		Loader: resolve.MapLoader{
			"go:fmt": {
				Path:    "fmt",
				Members: map[string]*resolve.Symbol{"Println": printlnSym},
			},
		},
		Sigs: check.MapSource{
			printlnSym: {
				Type:     types.Fn(types.Unit, types.P(types.GoSlice(&types.Var{ID: 1 << 21, Name: "a"}))),
				Variadic: true,
			},
		},
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
