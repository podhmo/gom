package build_test

// infer_test.go is the HM-inference property test (TODO "V-4 HM 推論
// のプロパティテスト", docs/decisions/2026-11-02-fuzz-strategy.md).
// Where TestGeneratedCompileCanary asks "did the compiler keep its
// contract on this generated program", this test asks a sharper
// question about check itself: gomgen's typed mode emits each
// expression aiming at a known generator-side type, so for every
// emitted span we know the type the program was WRITTEN to have.
// check is then required to infer a compatible type — an inference
// that reports E0401 on a guaranteed program, or lands a witnessed
// expression at a different type, is an inference bug candidate.
//
// The oracle mechanics live in internal/gomgen: GenerateResult
// returns the source plus a Witness list (emitted span + wanted
// surface type) and a Guaranteed flag that is false exactly when the
// walk emitted a deliberate near-miss (a "slip" — an else-less if on
// a non-Unit want, a bare fmt.Println in a Unit slot, an invented
// import path). A guaranteed program carrying any diagnostic is
// itself a finding.
//
// Matching: for each Witness the test finds the OUTERMOST typed
// expression node covering exactly that span and compares its
// inferred type against the wanted one as an instance check — the
// wanted type may be instantiated from a generalized binding (a
// `let xs = []` generalizes to `List a`, and a use at `List Int`
// matches). An unbound meta, a missing node, or a structural
// mismatch is a failure; rigid type variables match anything (they
// are the generalization the witness instantiates).
//
// Programs still go through the same fuzzutil.Sandbox contract as
// the compile canary before the in-process check run — a check hang
// or panic is a sandbox failure first and never reaches the matcher.
// Failures whose signature matches knownInferBugs are logged, not
// counted; new failures fail with mode+seed+source for replay.

import (
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/fuzzutil"
	"github.com/podhmo/gom/internal/gomgen"
	"github.com/podhmo/gom/internal/types"
)

// knownInferBugs is capped — same policy as knownCompileBugs; 16
// leaves headroom for the classes a first sweep may find while still
// forcing the list to be pruned rather than grown forever.
const knownInferBugsCap = 16

var knownInferBugs = map[[32]byte]fuzzutil.KnownBug{}

// TestKnownInferBugs pins the registry bookkeeping: under cap and
// every entry links a live TODO.md item.
func TestKnownInferBugs(t *testing.T) {
	fuzzutil.CheckRegistry(t, knownInferBugs, knownInferBugsCap, "../../TODO.md")
}

// inferSeedCount is the per-mode seed budget — GOM_INFER_SEEDS
// widens it for a longer opt-in sweep, the same convention
// GOM_DIFF_SEEDS uses for the differential test.
func inferSeedCount(t *testing.T) int {
	t.Helper()
	const def = 60
	if v := os.Getenv("GOM_INFER_SEEDS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			t.Fatalf("GOM_INFER_SEEDS=%q: not a positive integer", v)
		}
		return n
	}
	return def
}

// TestGeneratedInfer runs the witness oracle over deterministic
// sweeps of typed-mode programs.
func TestGeneratedInfer(t *testing.T) {
	g, err := gomgen.LoadGrammar("../../docs/grammar.ebnf")
	if err != nil {
		t.Fatalf("gomgen.LoadGrammar: %v", err)
	}
	opts, _ := compileOracles(t)
	var checkOpts *build.Options
	if opts != nil {
		o := *opts
		o.CheckOnly = true
		checkOpts = &o
	} else {
		checkOpts = &build.Options{CheckOnly: true}
	}

	// The sandbox run is the same compile-canary contract — it is
	// what bounds a check panic or hang before the in-process
	// re-run below sees the typed tree.
	sb := fuzzutil.NewSandbox("TestFuzzCompileSandbox", sandboxTimeout, 3<<20)
	sb.SetPhaseTimeout("parse", 1500*time.Millisecond)
	suspect := map[string]time.Duration{"parse": 500 * time.Millisecond}
	defer sb.Close()

	seeds := inferSeedCount(t)
	for _, mode := range []struct {
		name string
		opts gomgen.Options
	}{
		// MinDecls keeps the sweep off the decl-free floor — a bare
		// geometric draw emits zero-witness programs ~40% of the
		// time, which the oracle has nothing to check on.
		{"typed", gomgen.Options{Typed: true, MinDecls: 3}},
		{"typed-det", gomgen.Options{Typed: true, Deterministic: true, MinDecls: 3}},
	} {
		var accepted, rejected, slipped, checked, contractViol, reported, noWit int
		for seed := uint64(0); seed < uint64(seeds); seed++ {
			rng := rand.New(rand.NewPCG(seed, 0x9e3779b9+seed))
			res := gomgen.GenerateResult(g, rng, mode.opts)
			src := res.Source
			if !res.Guaranteed {
				slipped++
			}
			var caps map[string]time.Duration
			if fuzzutil.LooksHangSuspect([]byte(src)) {
				caps = suspect
			}
			failures := sb.RunCapped(t, []byte(src), caps)
			var violations []string
			for _, f := range failures {
				if bug, ok := fuzzutil.BugOf(f, knownCompileBugs); ok {
					t.Logf("%s seed %d: contract failure matches known bug %q",
						mode.name, seed, bug.Desc)
					continue
				}
				if bug, ok := fuzzutil.BugOf(f, knownInferBugs); ok {
					t.Logf("%s seed %d: contract failure matches known infer bug %q",
						mode.name, seed, bug.Desc)
					continue
				}
				violations = append(violations, f)
			}
			if len(violations) != 0 {
				t.Errorf("%s seed %d: contract violations\n%s--- program ---\n%s",
					mode.name, seed, joinAll(violations), src)
				contractViol++
				continue
			}

			r, err := build.Compile(build.Input{Path: "gen.gom", Src: []byte(src)}, checkOpts)
			if err != nil || r == nil {
				t.Fatalf("%s seed %d: build.Compile: %v\n%s", mode.name, seed, err, src)
			}
			if len(r.Diagnostics) != 0 {
				rejected++
				if res.Guaranteed {
					// A guaranteed program must carry no
					// diagnostics — every one here is a finding:
					// an inference bug or a generator-side gap the
					// slip flags failed to mark.
					var codes []string
					for _, d := range r.Diagnostics {
						codes = append(codes, fmt.Sprintf("%s %s", d.Code, d.Message))
					}
					fail := fmt.Sprintf("%s seed %d: guaranteed program carries diagnostics: %s",
						mode.name, seed, strings.Join(codes, "; "))
					if bug, ok := fuzzutil.BugOf(fail, knownInferBugs); ok {
						t.Logf("%s seed %d: %s — matches %q", mode.name, seed, fail, bug.Desc)
					} else if reported < 12 {
						t.Errorf("%s\n--- program ---\n%s", fail, src)
						reported++
					}
				}
				continue
			}
			accepted++
			if !res.Guaranteed {
				// A slipped program that still typechecks offers no
				// reliable oracle — the slip's diagnostic may have
				// left recovery metas behind.
				continue
			}
			checked++
			if len(res.Witnesses) == 0 {
				noWit++
			}
			for _, failure := range checkWitnesses(r.Typed, res) {
				if bug, ok := fuzzutil.BugOf(failure, knownInferBugs); ok {
					t.Logf("%s seed %d: %s — matches %q", mode.name, seed, failure, bug.Desc)
					continue
				}
				if reported >= 12 {
					continue
				}
				t.Errorf("%s\n--- program ---\n%s", failure, src)
				reported++
			}
		}
		t.Logf("%s mode: %d programs — %d accepted (%d witness-checked, %d witness-free), %d rejected, %d slipped, %d contract violations",
			mode.name, seeds, accepted, checked, noWit, rejected, slipped, contractViol)
	}
}

// checkWitnesses compares every recorded witness against the typed
// tree and returns one failure text per anomaly (bounded — a single
// bug tends to report many witnesses).
func checkWitnesses(tf *check.File, res gomgen.Result) []string {
	if tf == nil {
		return []string{"typed file missing on a diagnostic-free program"}
	}
	// Index the typed exprs by exact span, outermost first (WalkExpr
	// is parent-before-child, so the first record wins ties).
	bySpan := map[[2]int]check.Expr{}
	for _, d := range tf.Decls {
		check.WalkDecl(d, func(e check.Expr) {
			sp := e.Span()
			k := [2]int{sp.Start, sp.End}
			if _, ok := bySpan[k]; !ok {
				bySpan[k] = e
			}
		})
	}
	var failures []string
	for _, w := range res.Witnesses {
		want, err := parseWant(w.Type)
		if err != nil {
			failures = append(failures,
				fmt.Sprintf("witness %q at %d..%d: want parse failed: %v", w.Type, w.Start, w.End, err))
			continue
		}
		frag := res.Source[w.Start:w.End]
		node := witnessedNode(bySpan, w)
		if node == nil {
			failures = append(failures,
				fmt.Sprintf("witness %q at %d..%d: no typed node covering emitted %q", w.Type, w.Start, w.End, frag))
			continue
		}
		switch cls := classify(node.Type(), want); cls {
		case "":
			// instance match — the property holds.
		case "unbound meta":
			failures = append(failures,
				fmt.Sprintf("witness %q at %d..%d: inferred %q left an unbound meta (%q)", w.Type, w.Start, w.End, node.Type(), frag))
		default:
			failures = append(failures,
				fmt.Sprintf("witness %q at %d..%d: inferred %q (%q)", w.Type, w.Start, w.End, node.Type(), frag))
		}
		if len(failures) >= 3 {
			break
		}
	}
	return failures
}

// witnessedNode finds the typed node a witness refers to. Grouping
// parens produce no syntax (or typed) node — `(x)` keeps only x's own
// span — so the emitted span can be strictly larger than its node's.
// The wanted node is therefore the largest expr wholly contained in
// the witness span: the emission's root node always lies inside its
// own witness span, and no child can match its length, so the
// argmax is exactly that root. Equal-length candidates are decided
// by the smallest start — distinct same-length spans inside one
// witness are possible and map iteration must not pick between them.
func witnessedNode(bySpan map[[2]int]check.Expr, w gomgen.Witness) check.Expr {
	var best check.Expr
	bestLen, bestStart := 0, 0
	for k, e := range bySpan {
		if k[0] < w.Start || k[1] > w.End {
			continue
		}
		l := k[1] - k[0]
		if l > bestLen || (l == bestLen && l > 0 && k[0] < bestStart) {
			best, bestLen, bestStart = e, l, k[0]
		}
	}
	return best
}

// classify reports "" when the inferred type is an instance of the
// wanted type, "unbound meta" when inference left a metavariable
// unsolved, and a mismatch description otherwise.
func classify(inferred types.Type, want *wantExpr) string {
	var probe struct {
		meta bool
		bad  bool
	}
	var walk func(t types.Type, w *wantExpr)
	walk = func(t types.Type, w *wantExpr) {
		t = types.Chase(t)
		switch t := t.(type) {
		case *types.Meta:
			probe.meta = true
		case *types.Var:
			// A rigid variable is the generalization the wanted
			// type instantiates — it matches anything.
		case *types.Con:
			if w.name == "" || w.tuple != nil || t.Name != w.name || len(t.Args) != len(w.args) {
				probe.bad = true
				return
			}
			for i := range t.Args {
				walk(t.Args[i], w.args[i])
			}
		case *types.Tuple:
			if w.tuple == nil || len(t.Elems) != len(w.tuple) {
				probe.bad = true
				return
			}
			for i := range t.Elems {
				walk(t.Elems[i], w.tuple[i])
			}
		case *types.Func, *types.Go:
			// No generated want names a function or a Go handle —
			// either shape here is a mismatch.
			probe.bad = true
		default:
			panic("unreachable")
		}
	}
	walk(inferred, want)
	switch {
	case probe.meta:
		return "unbound meta"
	case probe.bad:
		return "mismatch"
	}
	return ""
}

// wantExpr is the parsed form of a Witness.Type — the same surface
// shapes gty.text() emits: builtin and declared names, List/Option/
// Result applications, ()-Unit, and (a, b) tuples.
type wantExpr struct {
	name  string      // "" only for tuples
	args  []*wantExpr // type-arguments of the named constructor
	tuple []*wantExpr // non-nil for (a, b, ...)
}

// parseWant parses gty.text() output back into a wantExpr.
func parseWant(s string) (*wantExpr, error) {
	var toks []string
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == ' ' || c == '\t' || c == '\n':
			i++
		case c == '(' || c == ')' || c == ',':
			toks = append(toks, s[i:i+1])
			i++
		default:
			j := i
			for j < len(s) && s[j] != ' ' && s[j] != '\t' && s[j] != '\n' &&
				s[j] != '(' && s[j] != ')' && s[j] != ',' {
				j++
			}
			toks = append(toks, s[i:j])
			i = j
		}
	}
	p := &wantParser{toks: toks}
	w, err := p.ty()
	if err != nil {
		return nil, err
	}
	if p.pos != len(toks) {
		return nil, fmt.Errorf("trailing tokens in %q", s)
	}
	return w, nil
}

type wantParser struct {
	toks []string
	pos  int
}

func (p *wantParser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}

// ty parses one wanted type: "(...)" (Unit, a parenthesized
// composite, or a tuple) or a name with atomic arguments
// ("Result Int (Option Bool)"). A `->` is never part of a wanted
// type — the generator's wants name no function type, so the token
// is a malformed want, not an identifier.
func (p *wantParser) ty() (*wantExpr, error) {
	if p.peek() == "(" {
		return p.paren()
	}
	w := &wantExpr{name: p.peek()}
	if !isWantIdent(w.name) {
		return nil, fmt.Errorf("expected a type name, got %q", p.peek())
	}
	p.pos++
	for p.peek() == "(" || isWantIdent(p.peek()) {
		a, err := p.atom()
		if err != nil {
			return nil, err
		}
		w.args = append(w.args, a)
	}
	return w, nil
}

func (p *wantParser) atom() (*wantExpr, error) {
	if p.peek() == "(" {
		return p.paren()
	}
	if !isWantIdent(p.peek()) {
		return nil, fmt.Errorf("expected a type name, got %q", p.peek())
	}
	w := &wantExpr{name: p.peek()}
	p.pos++
	return w, nil
}

// paren parses "()" → Unit, "(t)" → t, "(a, b)" → a tuple.
func (p *wantParser) paren() (*wantExpr, error) {
	p.pos++ // "("
	if p.peek() == ")" {
		p.pos++
		return &wantExpr{name: "Unit"}, nil
	}
	first, err := p.ty()
	if err != nil {
		return nil, err
	}
	if p.peek() != "," {
		if p.peek() != ")" {
			return nil, fmt.Errorf("expected ')' or ',', got %q", p.peek())
		}
		p.pos++
		return first, nil
	}
	w := &wantExpr{tuple: []*wantExpr{first}}
	for p.peek() == "," {
		p.pos++
		e, err := p.ty()
		if err != nil {
			return nil, err
		}
		w.tuple = append(w.tuple, e)
	}
	if p.peek() != ")" {
		return nil, fmt.Errorf("expected ')', got %q", p.peek())
	}
	p.pos++
	return w, nil
}

func isWantIdent(tok string) bool {
	return tok != "" && tok != "(" && tok != ")" && tok != "," &&
		!strings.Contains(tok, "->")
}

// TestParseWantArrows pins the latent rejection: a `->` is never a
// wanted-type token (no generated want names a function type), so
// every spelling of an arrow is malformed input, not an identifier.
func TestParseWantArrows(t *testing.T) {
	for _, s := range []string{
		"Int -> Int",
		"Int->Int",
		"List Int -> Bool",
		"->",
		"(Int -> Int, Bool)",
	} {
		if _, err := parseWant(s); err == nil {
			t.Errorf("parseWant(%q) succeeded — the arrow read as an identifier", s)
		}
	}
	// Valid wants still parse.
	for _, s := range []string{"Int", "List (Option Bool)", "(Int, Result String GoError)", "()"} {
		if _, err := parseWant(s); err != nil {
			t.Errorf("parseWant(%q) = %v", s, err)
		}
	}
}

// TestWitnessedNodeDeterministic pins the argmax tie-break: two
// candidates of equal length inside one witness span resolve to the
// smallest start, not whatever order the map walked.
func TestWitnessedNodeDeterministic(t *testing.T) {
	mk := func(sp diag.Span) check.Expr {
		return &check.Lit{Base: check.Base{Sp: sp, Typ: types.Int}}
	}
	a := mk(diag.Span{Start: 2, End: 6})
	b := mk(diag.Span{Start: 8, End: 12})
	bySpan := map[[2]int]check.Expr{{2, 6}: a, {8, 12}: b}
	w := gomgen.Witness{Start: 0, End: 20}
	for i := 0; i < 32; i++ {
		if got := witnessedNode(bySpan, w); got != a {
			t.Fatalf("iteration %d: picked the later-start candidate", i)
		}
	}
}
