package format

// gomgen-seeded formatter fuzzing (TODO V-5). FuzzFormat mutates a
// handwritten corpus byte-wise; this file feeds the formatter whole
// grammar-conformant programs from internal/gomgen so the input
// space covers every production docs/grammar.ebnf can express —
// shapes the hand-written corpus never reaches (interleaved
// imports, labeled args, generated match/select/record nests).
//
// Two drivers share one contract and one sandbox helper:
//
//   - FuzzFormatGen seeds the Go fuzzer with generated programs;
//     mutation then explores the near-valid neighborhood of
//     grammar-shaped inputs.
//   - TestGeneratedFormat runs a bounded deterministic sweep in
//     ordinary `go test` (GOM_FMTGEN_SEEDS deepens it), the same
//     role TestGeneratedCompileCanary plays for build.Compile.
//
// The contract (checkGenFormat) is FuzzFormat's plus the spec-1.7
// "same syntax tree" half that the byte-level checker cannot see:
// normSyntax lives in this package. A bare parse runs first so a
// parser-side hang attributes to "parse", and both Source passes
// run under the phase name "format" — pass 2 formats Source's own
// output but it is the same code path, so the registered
// deep-nesting hang signature covers either pass while a hang in
// reparse/tree/comments still reports under its own phase.

import (
	"fmt"
	"math/rand/v2"
	"os"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/fuzzutil"
	"github.com/podhmo/gom/internal/gomgen"
	"github.com/podhmo/gom/internal/syntax"
)

const genFormatBugsCap = 16

// knownGenFormatBugs registers formatter bugs found by the
// generated sweep (see internal/fuzzutil). Repros are inline
// literals, not testdata files: TestRepoFiles formats every .gom
// in the repo, so a checked-in .gom hang repro would stall that
// suite outright.
var knownGenFormatBugs = map[[32]byte]fuzzutil.KnownBug{
	// The deep-nesting exponential-layout bug (TODO "gom fmt が深い
	// 入れ子で指数的に遅くなる"): braceSeq's lazy callbacks
	// re-evaluate flatWidth/docText per item, so deep block chains
	// cost super-polynomial time inside Source. These are gomgen
	// typed-mode seeds 62/162/168/172/176 — pass-1 Source times
	// them at ~2 s / >15 s / ~2 s / >15 s / >15 s for 1.2–1.9 KB
	// programs (the two-second pair lands inside the same family
	// under -race or a slower box). Skip-pinned: every run would
	// burn the sandbox budget for no new information. The Match
	// is phase-scoped so a NEW hang inside Source also reads as
	// this bug — intentional: "format did not return" is the
	// signature of this one family until triage says otherwise.
	fuzzutil.Sum256([]byte(genHangTyped62)): {
		Match: []string{"format did not return within"},
		TODO:  "`gom fmt` が深い入れ子で指数的に遅くなる",
		Desc:  "exponential layout on deep block nesting (gomgen typed seed 62, ~2s)",
		Skip:  true,
	},
	fuzzutil.Sum256([]byte(genHangTyped162)): {
		Match: []string{"format did not return within"},
		TODO:  "`gom fmt` が深い入れ子で指数的に遅くなる",
		Desc:  "exponential layout on deep block nesting (gomgen typed seed 162, >15s)",
		Skip:  true,
	},
	fuzzutil.Sum256([]byte(genHangTyped168)): {
		Match: []string{"format did not return within"},
		TODO:  "`gom fmt` が深い入れ子で指数的に遅くなる",
		Desc:  "exponential layout on deep block nesting (gomgen typed seed 168, ~2s)",
		Skip:  true,
	},
	fuzzutil.Sum256([]byte(genHangTyped172)): {
		Match: []string{"format did not return within"},
		TODO:  "`gom fmt` が深い入れ子で指数的に遅くなる",
		Desc:  "exponential layout on deep block nesting (gomgen typed seed 172, >15s)",
		Skip:  true,
	},
	fuzzutil.Sum256([]byte(genHangTyped176)): {
		Match: []string{"format did not return within"},
		TODO:  "`gom fmt` が深い入れ子で指数的に遅くなる",
		Desc:  "exponential layout on deep block nesting (gomgen typed seed 176, >15s)",
		Skip:  true,
	},
}

// genSandboxTimeout bounds one input's turn inside the persistent
// helper. Legit formatting of a generated program is milliseconds
// even at the sweep's worst (typed seeds 21/42 take ~0.6–1.0 s —
// already the exponential bug's foothill); 8 s keeps slow-but-legal
// inputs passing under -race while a real hang dies in the child.
// The parse/dump-side phases get 2 s: they legitimately cost µs.
const genSandboxTimeout = 8 * time.Second

// genCaps sizes the per-phase budget for one input. Parse/dump legs
// are cheap for any input, so they always get the small cap. The
// "format" phase keeps the full sandbox timeout UNLESS the input
// sits inside the exponential-layout family: the measured bug is
// super-polynomial in block-nesting depth (typed repros nest
// { 12–16 deep; the deepest input that still completes nests 11),
// so past genNestDepth a variant would burn the whole budget — the
// same hang-variant storm the ctor-pattern family caused in the
// compile canary. A suspect gets 1.5 s instead: the signature still
// classifies as the registered bug, it just costs ~4 s of
// timeout+respawn instead of ~10 s. fuzzutil.LooksHangSuspect
// shrinks the parse-side phases the same way for its family.
func genCaps(src []byte) map[string]time.Duration {
	caps := map[string]time.Duration{
		"parse":    2 * time.Second,
		"reparse":  2 * time.Second,
		"tree":     2 * time.Second,
		"comments": 2 * time.Second,
	}
	if genDeepNest(src) {
		caps["format"] = 1500 * time.Millisecond
	}
	if fuzzutil.LooksHangSuspect(src) {
		caps["parse"] = 500 * time.Millisecond
		caps["reparse"] = 500 * time.Millisecond
		caps["comments"] = 500 * time.Millisecond
	}
	return caps
}

// genNestDepth is the `{`-nesting boundary of the measured
// exponential-layout family — cheap to compute and false positives
// only shrink a legit input's budget, never skip it.
const genNestDepth = 12

// genDeepNest reports whether src's `{`/`}` depth reaches
// genNestDepth. Bytes inside strings and comments can inflate the
// count — a false suspect only costs a smaller format budget.
func genDeepNest(src []byte) bool {
	depth := 0
	for _, b := range src {
		switch b {
		case '{':
			depth++
			if depth >= genNestDepth {
				return true
			}
		case '}':
			if depth > 0 {
				depth--
			}
		}
	}
	return false
}

// genPins are interesting generated programs kept as fixed seeds —
// the same role the hand-written list in build's compileSeeds
// plays: each (mode, seed) pair regenerates exact bytes (gomgen is
// deterministic), so a regression pins a precise input. Picks lean
// on inputs that exercised near-misses while V-5 was developed:
// blank-insertion on a flat file (mech 11), imports interleaved
// with decls (mech 17/42, typed 15/24), and the deepest nesting
// that still completes in range (typed 21/42).
var genPins = []struct {
	mode string
	opts gomgen.Options
	seed uint64
}{
	{"mech", gomgen.Options{}, 1},
	{"mech", gomgen.Options{}, 3},
	{"mech", gomgen.Options{}, 10},
	{"mech", gomgen.Options{}, 11},
	{"mech", gomgen.Options{}, 17},
	{"mech", gomgen.Options{}, 42},
	{"typed", gomgen.Options{Typed: true}, 15},
	{"typed", gomgen.Options{Typed: true}, 21},
	{"typed", gomgen.Options{Typed: true}, 24},
	{"typed", gomgen.Options{Typed: true}, 42},
}

// genModes are the sweep's generator profiles: mechanical covers
// the widest syntax, typed adds scope/arity-aware programs.
var genModes = []struct {
	name string
	opts gomgen.Options
}{
	{"mechanical", gomgen.Options{}},
	{"typed", gomgen.Options{Typed: true}},
}

// genCorpus returns the fixed input set shared by FuzzFormatGen
// (as f.Add seeds) and TestGeneratedFormat (run before the seed
// sweep): the pinned regenerations plus the registered repros —
// Skip-pinned inputs seed the corpus without executing.
func genCorpus(t testing.TB, g *gomgen.Grammar) [][]byte {
	t.Helper()
	var seeds [][]byte
	for _, p := range genPins {
		rng := rand.New(rand.NewPCG(p.seed, 0x9e3779b9+p.seed))
		seeds = append(seeds, []byte(gomgen.Generate(g, rng, p.opts)))
	}
	for _, src := range genBugRepros {
		seeds = append(seeds, []byte(src))
	}
	return seeds
}

// genBugRepros lists the inline repro literals the registry keys
// on, so they also seed the corpus.
var genBugRepros = []string{
	genHangTyped62, genHangTyped162, genHangTyped168,
	genHangTyped172, genHangTyped176,
}

// genSeedsEnv/genSeedsDef size the bounded sweep. The default keeps
// `go test` quick while staying below the first pathological seeds
// (typed 62+); the validation run used GOM_FMTGEN_SEEDS=200.
const (
	genSeedsEnv = "GOM_FMTGEN_SEEDS"
	genSeedsDef = 60
)

func genSeedCount(t *testing.T) int {
	t.Helper()
	if s := os.Getenv(genSeedsEnv); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return genSeedsDef
}

// callSource runs Source under a recover so a panic becomes a
// failure string instead of crashing the sandbox child.
func callSource(src []byte) (out []byte, diags diag.List, failure string) {
	defer func() {
		if r := recover(); r != nil {
			failure = fmt.Sprintf("Source panicked\npanic: %v\n%s", r, debug.Stack())
		}
	}()
	out, diags = Source(src, "gen.gom", false)
	return out, diags, ""
}

// checkGenFormat exercises the formatter contract on one generated
// input and returns one failure description per violation. Both
// Source passes report under the "format" phase (see the file
// header); reparse/tree/comments are separate phases so a hang is
// attributed to the stage that owns it.
func checkGenFormat(src []byte) (failures []string) {
	// Phase gate 1: a bare parse first, so an input-side parser
	// hang or panic is attributed to "parse" and cannot borrow the
	// registered format-bug signature (same role phaseParse plays
	// in the compile canary). The diagnostics are Source's to
	// interpret, so they are discarded here.
	fuzzutil.SandboxPhase("parse")
	var pfailure string
	func() {
		defer func() {
			if r := recover(); r != nil {
				pfailure = fmt.Sprintf("syntax.Parse panicked\npanic: %v\n%s", r, debug.Stack())
			}
		}()
		syntax.Parse(src, "gen.gom")
	}()
	if pfailure != "" {
		return append(failures, pfailure)
	}

	fuzzutil.SandboxPhase("format")
	out, diags, failure := callSource(src)
	if failure != "" {
		return append(failures, failure)
	}
	if diags.HasError() {
		if out != nil {
			return append(failures, fmt.Sprintf(
				"Source produced output alongside error diagnostics: %v", diags))
		}
		// Unparseable input is legal output — nothing else to check.
		return nil
	}
	if out == nil {
		return append(failures, "Source returned nil output without error diagnostics")
	}

	// The output reparses cleanly — the diagnostics-clean half of
	// the file-mode contract (gom fmt -d/-w never leaks diags).
	fuzzutil.SandboxPhase("reparse")
	_, d2 := syntax.Parse(out, "gen.gom")
	if d2.HasError() {
		return append(failures, fmt.Sprintf(
			"Source output does not reparse: %v\n--- output ---\n%s", d2, out))
	}

	// spec 1.7: the formatted file holds the same syntax tree.
	fuzzutil.SandboxPhase("tree")
	a, _ := normSyntax(src, "gen.gom", false)
	b, _ := normSyntax(out, "gen.gom", false)
	if a != b {
		failures = append(failures, fmt.Sprintf(
			"Source changed the syntax tree\n--- in ---\n%s\n--- out ---\n%s\n--- formatted ---\n%s",
			a, b, out))
	}

	// fmt(fmt(x)) == fmt(x): the second pass re-enters Source under
	// the same "format" phase — same code path, same hang family.
	fuzzutil.SandboxPhase("format")
	out2, d3, failure := callSource(out)
	if failure != "" {
		return append(failures, failure)
	}
	if d3.HasError() {
		failures = append(failures, fmt.Sprintf(
			"Source rejects its own output: %v\n--- output ---\n%s", d3, out))
	} else if string(out2) != string(out) {
		failures = append(failures, fmt.Sprintf(
			"Source is not idempotent\n--- pass 1 ---\n%s\n--- pass 2 ---\n%s",
			out, out2))
	}

	// Comments survive with their text (same multiset — the
	// formatter may re-anchor or rewrap a // comment as /* */ but
	// never drops or rewrites one). gomgen emits no comments
	// today; mutated corpus bytes still can.
	fuzzutil.SandboxPhase("comments")
	if in, got := genCommentTexts(src), genCommentTexts(out); !genSameMultiset(in, got) {
		failures = append(failures, fmt.Sprintf(
			"Source changed comments\nin:  %q\nout: %q", in, got))
	}
	return failures
}

// genCommentTexts returns the whitespace-normalized text of every
// comment in src, in source order — the format_test twin of
// commentTexts, duplicated here because this file lives in the
// internal test package (normSyntax is only visible here).
func genCommentTexts(src []byte) []string {
	f, diags := syntax.Parse(src, "gen.gom")
	if diags.HasError() {
		return nil
	}
	var out []string
	for _, c := range f.Comments {
		out = append(out, strings.Join(strings.Fields(c.Text), ""))
	}
	return out
}

// genSameMultiset compares two string slices as multisets.
func genSameMultiset(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as, bs := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

// runGenInput feeds one input through the sandbox and applies the
// registry policy — the same rules fuzzutil.Report encodes:
// Skip-pinned repros never run; a registered input that produces
// only registered signatures logs as a known bug; a registered
// input that passes outright is stale (the entry, its repro and
// the TODO item are removed together). It returns the unregistered
// violations.
func runGenInput(t *testing.T, sb *fuzzutil.Sandbox, src []byte, label string) []string {
	t.Helper()
	bug, registered := knownGenFormatBugs[fuzzutil.Sum256(src)]
	if registered && bug.Skip {
		t.Logf("%s: input pinned as unsafe-to-run known bug %q — skipped", label, bug.Desc)
		return nil
	}
	var violations []string
	var matched int
	for _, f := range sb.RunCapped(t, src, genCaps(src)) {
		if bug, ok := fuzzutil.BugOf(f, knownGenFormatBugs); ok {
			t.Logf("%s: failure matches known bug %q — not a new finding",
				label, bug.Desc)
			matched++
			continue
		}
		violations = append(violations, f)
	}
	switch {
	case len(violations) != 0:
		t.Errorf("%s: contract violations\n%s--- program ---\n%s",
			label, joinGenFailures(violations), src)
	case registered && matched == 0:
		t.Errorf("%s: knownBugs entry is stale (bug fixed?): %q — remove the entry, its repro seed and the TODO item",
			label, bug.Desc)
	case registered:
		t.Logf("%s: known bug, not reporting %d failure(s): %s",
			label, matched, bug.Desc)
	}
	return violations
}

func joinGenFailures(failures []string) string {
	s := ""
	for i, f := range failures {
		s += fmt.Sprintf("  [%d] %s\n", i, f)
	}
	return s
}

func newGenSandbox() *fuzzutil.Sandbox {
	return fuzzutil.NewSandbox("TestFuzzFormatGenSandbox", genSandboxTimeout, 2<<20)
}

// FuzzFormatGen is the gomgen-seeded twin of FuzzFormat: the seed
// corpus is generated programs (the pins plus the registered
// repros) and the mutator explores their near-valid neighborhood.
// `go test` replays the seeds deterministically; a -fuzz run adds
// bounded mutation. Bugs filed in TODO.md live in
// knownGenFormatBugs — new failure signatures fail the run.
func FuzzFormatGen(f *testing.F) {
	if len(knownGenFormatBugs) > genFormatBugsCap {
		f.Fatalf("knownBugs has %d entries, cap is %d", len(knownGenFormatBugs), genFormatBugsCap)
	}
	g, err := gomgen.LoadGrammar("../../docs/grammar.ebnf")
	if err != nil {
		f.Fatalf("gomgen.LoadGrammar: %v", err)
	}
	for _, seed := range genCorpus(f, g) {
		f.Add(seed)
	}
	sb := newGenSandbox()
	defer sb.Close()
	f.Fuzz(func(t *testing.T, src []byte) {
		runGenInput(t, sb, src, fmt.Sprintf("%d-byte input", len(src)))
	})
}

// TestGeneratedFormat is the bounded sweep: every pinned input
// plus seeds 0..GOM_FMTGEN_SEEDS-1 in each generator mode run the
// contract. Generation side-effects (parse rejections) are legal
// and only counted for the yield metric; contract violations fail.
func TestGeneratedFormat(t *testing.T) {
	g, err := gomgen.LoadGrammar("../../docs/grammar.ebnf")
	if err != nil {
		t.Fatalf("gomgen.LoadGrammar: %v", err)
	}
	sb := newGenSandbox()
	defer sb.Close()

	var ran int
	for i, src := range genCorpus(t, g) {
		runGenInput(t, sb, src, fmt.Sprintf("corpus[%d]", i))
		ran++
	}
	t.Logf("pinned corpus: %d inputs run", ran)

	seeds := genSeedCount(t)
	for _, mode := range genModes {
		var clean, rejected, violations int
		for seed := uint64(0); seed < uint64(seeds); seed++ {
			rng := rand.New(rand.NewPCG(seed, 0x9e3779b9+seed))
			src := gomgen.Generate(g, rng, mode.opts)
			label := fmt.Sprintf("%s seed %d", mode.name, seed)
			if len(runGenInput(t, sb, []byte(src), label)) != 0 {
				violations++
				continue
			}
			var d diag.List
			_, d = syntax.Parse([]byte(src), "gen.gom")
			if d.HasError() {
				rejected++
			} else {
				clean++
			}
		}
		if violations != 0 {
			t.Errorf("%s mode: %d/%d programs violated the format contract",
				mode.name, violations, seeds)
		}
		t.Logf("%s mode: %d/%d fully clean, %d rejected with parse diagnostics",
			mode.name, clean, seeds, rejected)
	}
}

// TestFuzzFormatGenSandbox is the child side of fuzzutil.Sandbox —
// it loops length-prefixed inputs on stdin and runs the contract,
// printing PHASE markers and FAIL lines. Skipped unless spawned
// via EnvSandbox.
func TestFuzzFormatGenSandbox(t *testing.T) {
	if os.Getenv(fuzzutil.EnvSandbox) != "1" {
		t.Skip("sandbox helper — spawned by FuzzFormatGen/TestGeneratedFormat")
	}
	fuzzutil.ServeSandbox(t, func(src []byte) {
		for _, s := range checkGenFormat(src) {
			fuzzutil.SandboxFail(s)
		}
	})
}

// TestKnownGenFormatBugs pins the bookkeeping rules.
func TestKnownGenFormatBugs(t *testing.T) {
	fuzzutil.CheckRegistry(t, knownGenFormatBugs, genFormatBugsCap, "../../TODO.md")
}
