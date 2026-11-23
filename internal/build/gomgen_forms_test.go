package build_test

// Coverage probe for the wave36 lane-I gomgen extensions — the
// shapes the typed generator previously could not emit
// (docs/sketch/2026-11-23-wave36-prompts.md lane I):
//
//   - `send`/recv `select` arms over a local `Chan.make`
//   - `Scope.run`/`Scope.defer`
//   - `Ptr.field = v` over `import go ".../testdata/goiface"`
//   - `import "list"`/`"option"`/`"result"`/`"cmp"` (gom stdlib
//     packages) plus the member calls they unlock
//
// Each class must appear in generated output AND compile cleanly
// (zero diagnostics) in at least one generated program — the new
// forms themselves are never deliberate slips, so a clean compile
// is the oracle that the emitted text is the intended syntax.

import (
	"math/rand/v2"
	"path/filepath"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/gomgen"
)

func TestGomgenNewFormsCompile(t *testing.T) {
	g, err := gomgen.LoadGrammar(filepath.Join("..", "..", "docs", "grammar.ebnf"))
	if err != nil {
		t.Fatalf("LoadGrammar: %v", err)
	}
	opts, imp := compileOracles(t)
	if opts == nil {
		t.Skip("no go toolchain — compile oracles unavailable")
	}

	classes := map[string][]string{
		"select-send":    {"send ch"},
		"select-recv":    {"<-ch"},
		"select-default": {"default->"},
		"scope-run":      {"Scope.run<|"},
		"scope-defer":    {"Scope.defer s"},
		"ptr-field":      {"w.Fn ="},
		"gom-import":     {`import"list"`, `import"option"`, `import"result"`, `import"cmp"`},
		"gom-member":     {"list.length", "list.rev", "list.is_empty", "list.head", "option.is_some", "option.is_none", "result.is_ok", "result.is_err"},
		"goiface-import": {"goiface"},
	}
	seen := map[string]bool{}

	for seed := uint64(0); seed < 3000 && len(seen) < len(classes); seed++ {
		rng := rand.New(rand.NewPCG(seed, 0x9e3779b9))
		src := gomgen.Generate(g, rng, gomgen.Options{Typed: true})
		var hits []string
		for name, marks := range classes {
			if seen[name] {
				continue
			}
			for _, m := range marks {
				if strings.Contains(src, m) {
					hits = append(hits, name)
					break
				}
			}
		}
		if len(hits) == 0 {
			continue
		}
		// Other statements may carry deliberate slips, so only a
		// diagnostic-free compile proves the new form's syntax.
		r, err := build.Compile(build.Input{Path: "gen.gom", Src: []byte(src)}, opts)
		if err != nil {
			t.Fatalf("build.Compile internal error: %v\n---\n%s", err, src)
		}
		for _, d := range r.Diagnostics {
			if d.Code == "E0901" {
				t.Fatalf("E0901 surfaced: %s\n---\n%s", d.Message, src)
			}
		}
		if r.Diagnostics.HasError() {
			continue
		}
		for _, name := range hits {
			seen[name] = true
			t.Logf("%s: clean compile at seed %d", name, seed)
		}
	}
	for name := range classes {
		if !seen[name] {
			t.Errorf("%s: no clean-compiling program in 3000 seeds", name)
		}
	}
	_ = imp
}
