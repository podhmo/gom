package build_test

// gen_test.go wires internal/gomgen — the grammar-based generator
// from docs/grammar.ebnf — into the compile canary. Where
// FuzzCompile mutates corpus seeds byte-wise, this test generates
// whole programs grammar-aware: deterministic seeds walk fresh
// shapes (typed-ish and mechanical modes) through every pipeline
// phase. Each generated program is judged by the same checkCompile
// contract as fuzz corpus entries — no panic, no internal error,
// valid in-bounds diagnostics, no E0901, and generated Go that
// parses and typechecks. Generation side-effects like type errors
// (E0401/E0407 on semantically near-miss programs) are EXPECTED and
// only counted for the yield metric; contract violations fail.
//
// Generated programs run through the same fuzzutil.Sandbox helper as
// FuzzCompile: a parse/compile hang on generated input must kill a
// disposable child, not the test binary. Failures whose signature
// matches the knownCompileBugs registry are logged, not counted —
// deepening the canary later will not red-flag already-filed bugs.

import (
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/fuzzutil"
	"github.com/podhmo/gom/internal/gomgen"
)

// TestGeneratedCompileCanary compiles a deterministic sweep of
// generated programs through build.Compile under the real oracles
// (goimport loader + export importer, when a `go` binary is on
// PATH). User diagnostics are fine — the canary's contract is what
// FuzzCompile's is: the compiler must never crash, leak internal
// errors, or emit Go that does not typecheck.
func TestGeneratedCompileCanary(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 0.72)
	}

	t.Parallel()

	g, err := gomgen.LoadGrammar("../../docs/grammar.ebnf")
	if err != nil {
		t.Fatalf("gomgen.LoadGrammar: %v", err)
	}
	opts, imp := compileOracles(t)
	if imp != nil {
		warm := checkCompile([]byte("package m\n\nfn main () -> Unit = ()\n"), opts, imp)
		if len(warm) != 0 {
			t.Logf("warmup compile failures: %v", warm)
		}
	}

	sb := fuzzutil.NewSandbox("TestFuzzCompileSandbox", sandboxTimeout, 3<<20)
	sb.SetPhaseTimeout("parse", 1500*time.Millisecond)
	suspect := map[string]time.Duration{"parse": 500 * time.Millisecond}
	defer sb.Close()

	const seeds = 80
	for _, mode := range []struct {
		name string
		opts gomgen.Options
	}{
		{"mechanical", gomgen.Options{}},
		{"typed", gomgen.Options{Typed: true}},
	} {
		var contractOK, rejected, known, internal int
		for seed := uint64(0); seed < seeds; seed++ {
			rng := rand.New(rand.NewPCG(seed, 0x9e3779b9+seed))
			src := gomgen.Generate(g, rng, mode.opts)
			var caps map[string]time.Duration
			if fuzzutil.LooksHangSuspect([]byte(src)) {
				caps = suspect
			}
			failures := sb.RunCapped(t, []byte(src), caps)
			var violations []string
			for _, f := range failures {
				if bug, ok := fuzzutil.BugOf(f, knownCompileBugs); ok {
					t.Logf("%s seed %d: failure matches known bug %q — not a new finding",
						mode.name, seed, bug.Desc)
					known++
					continue
				}
				violations = append(violations, f)
			}
			if len(violations) != 0 {
				t.Errorf("%s seed %d: contract violations\n%s--- program ---\n%s",
					mode.name, seed, joinAll(violations), src)
				internal++
				continue
			}
			if hasDiagErrors(src, opts) {
				rejected++
				continue
			}
			contractOK++
		}
		if internal != 0 {
			t.Errorf("%s mode: %d/%d programs violated the compile contract", mode.name, internal, seeds)
		}
		t.Logf("%s mode: %d/%d fully clean, %d rejected with user diagnostics, %d known-bug signatures",
			mode.name, contractOK, seeds, rejected, known)
	}
}

// hasDiagErrors recompiles src just to read its diagnostics —
// checkCompile only reports contract violations, so the canary
// counts expected user-diagnostic rejections through the driver
// itself, under the same oracles (nil options would inflate the
// count: unresolvable `import go` paths read as extra E0203s).
// Panics and hangs are already caught by the sandbox run above;
// this second run is deterministic so neither can surprise here.
func hasDiagErrors(src string, opts *build.Options) bool {
	r, err := build.Compile(build.Input{Path: "gen.gom", Src: []byte(src)}, opts)
	if err != nil || r == nil {
		return false
	}
	return r.Diagnostics.HasError()
}

// joinAll flattens checkCompile's per-violation strings for a
// readable test failure.
func joinAll(failures []string) string {
	s := ""
	for i, f := range failures {
		s += fmt.Sprintf("  [%d] %s\n", i, f)
	}
	return s
}
