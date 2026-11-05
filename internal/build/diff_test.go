package build_test

// diff_test.go is the eval-vs-compiled differential test (TODO V-3):
// gomgen's Deterministic subset generates programs that are meant to
// run identically twice, every generated program the compiler accepts
// is then executed BOTH by internal/eval and by the generated Go
// binary, and the two observable behaviors — stdout bytes and the
// clean/panic/nontermination exit class — are compared. A difference
// is a semantics-bug candidate: evaluation-order violations,
// structural-equality mismatches, pipe-direction errors, TailLoop
// lowering bugs, thunk forcing mistakes — the divergences the whole
// fuzzing stack exists to find.
//
// The input contract mirrors the compile canary: sources go through
// fuzzutil.Sandbox first (a compile hang or panic is a contract
// violation, never a divergence), then a second deterministic
// build.Compile for the typed file and generated Go. Programs
// carrying user diagnostics are skipped on BOTH sides — a rejected
// program compares nothing.
//
// Evaluator safety: a leaked goroutine cannot be killed, so the eval
// side is bounded structurally — eval.SetMaxSteps gives every
// generated program a fuel budget (E9006) instead of a wall clock,
// and the evaluator's built-in call-depth bound (defaultMaxDepth,
// same E9006 class) cuts runaway recursion off far below the fatal
// goroutine-stack ceiling fuel alone would hit first.
// The compiled side runs the built binary under a real timeout,
// twice, which doubles as the determinism check the Deterministic
// subset promises. `fmt.Println` and the other deterministic Go
// members gomgen can emit are bound as eval host functions — the
// eval side's "stdout" is the captured Println stream, so the byte
// comparison covers every observable print, not just `answer`.
// Go members with no faithful stub (mutating sorts, format-string
// printing) stay unbound: the evaluator reports E9001 and the
// program is counted as unexecutable rather than compared.

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/eval"
	"github.com/podhmo/gom/internal/fuzzutil"
	"github.com/podhmo/gom/internal/gomgen"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/testbin"
	"github.com/podhmo/gom/internal/types"
)

// diffEvalFuel is the eval-side step budget: a tree-walking
// interpreter cannot be timed out from outside, so a diverging
// generated program is cut off by fuel instead of growing the Go
// stack. Sized so a legit generated program (bounded lists, shallow
// recursion) never trips it while a runaway one costs ~100 ms.
const diffEvalFuel = 3_000_000

// diffRunTimeout bounds one compiled binary run; diffBuildTimeout
// bounds the `go build` of the scratch module (the first build pays
// for compiling gom's rt into the shared cache).
const (
	diffRunTimeout   = 4 * time.Second
	diffBuildTimeout = 90 * time.Second
)

// diffSeedsEnv/diffBudgetEnv size the bounded session. The committed
// defaults keep `go test ./...` reasonable; the validation runs use
// GOM_DIFF_SEEDS=200 GOM_DIFF_BUDGET=8m for the ~200-seed session.
const (
	diffSeedsEnv  = "GOM_DIFF_SEEDS"
	diffBudgetEnv = "GOM_DIFF_BUDGET"
	diffSeedsDef  = 40
	diffBudgetDef = 90 * time.Second
)

// diffClass is the compared exit class of one side.
type diffClass int

const (
	// dClean means the program ran to completion with exit code 0 /
	// no error diagnostics.
	dClean diffClass = iota + 1
	// dPanic is a Gom-level panic class: eval's E9002/E9003/E9005,
	// or a nonzero process exit on the compiled side.
	dPanic
	// dNonterm is nontermination: eval fuel exhausted (E9006) or a
	// wall-clock timeout on the binary. Compared as one class on
	// both sides; a one-sided dNonterm is a termination divergence.
	dNonterm
	// dUnsupported means the evaluator declined (E9001 — an
	// unstubbed Go member or a construct eval does not implement).
	// Not compared: there is no eval-side behavior to judge.
	dUnsupported
	// dInternal is an eval/compiler invariant break (E9004) — a bug
	// regardless of what the other side did.
	dInternal
)

// diffOutcome is one side's observable result.
type diffOutcome struct {
	class  diffClass
	stdout string
	detail string // diagnostic codes / stderr tail, for the report
}

// knownDivergences is the registered-bug map for SEMANTIC
// divergences — deliberately not knownCompileBugs: those are
// compiler-contract bugs (crashes, bad diagnostics, invalid
// generated Go), these are two-clean-executions-disagree findings.
// Keyed by the SHA-256 of the generated source, with the same
// Match/TODO/Desc bookkeeping (see fuzzutil.KnownBug); every entry
// needs an open TODO.md item and CheckRegistry keeps the list
// bounded.
var knownDivergences = map[[32]byte]fuzzutil.KnownBug{}

const knownDivergencesCap = 24

// TestKnownDivergences pins the registry bookkeeping.
func TestKnownDivergences(t *testing.T) {
	fuzzutil.CheckRegistry(t, knownDivergences, knownDivergencesCap, "../../TODO.md")
}

// TestEvalCompiledDiff is the bounded differential session: for each
// deterministic seed, compile through the sandbox contract, and on a
// clean compile run the program under eval and under the generated
// Go, comparing stdout and exit class.
func TestEvalCompiledDiff(t *testing.T) {
	g, err := gomgen.LoadGrammar("../../docs/grammar.ebnf")
	if err != nil {
		t.Fatalf("gomgen.LoadGrammar: %v", err)
	}
	opts, imp := compileOracles(t)
	if opts == nil || imp == nil {
		t.Skip("no go toolchain on PATH — the compiled side cannot run")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH — the compiled side cannot run")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	sb := fuzzutil.NewSandbox("TestFuzzCompileSandbox", sandboxTimeout, 3<<20)
	sb.SetPhaseTimeout("parse", 1500*time.Millisecond)
	suspect := map[string]time.Duration{"parse": 500 * time.Millisecond}
	defer sb.Close()

	seeds := diffSeeds(t)
	budget := diffBudget(t)
	deadline := time.Now().Add(budget)
	// Compiler sessions and eval stay on this goroutine. Only isolated
	// native builds/runs overlap; collect outcomes in seed order below.
	workers := make(chan struct{}, 4)
	var running sync.WaitGroup
	defer running.Wait()
	type nativeResult struct {
		out              diffOutcome
		nondeterministic bool
		err              string
	}
	type nativeJob struct {
		seed   uint64
		src    string
		eval   diffOutcome
		result <-chan nativeResult
	}
	for _, mode := range []struct {
		name string
		opts gomgen.Options
	}{
		{"typed", gomgen.Options{Typed: true, Deterministic: true}},
		{"mechanical", gomgen.Options{Deterministic: true}},
	} {
		var generated, contractBad, rejected, compared int
		var evalUnsupported, evalInternal, evalFuel, compiledTimeout int
		var nondet, divergent, known int
		var jobs []nativeJob
		seed := uint64(0)
		for ; seed < uint64(seeds); seed++ {
			if time.Now().After(deadline) {
				t.Logf("%s mode: budget %s exhausted at seed %d/%d",
					mode.name, budget, seed, seeds)
				break
			}
			generated++
			rng := rand.New(rand.NewPCG(seed, 0x9e3779b9+seed))
			src := gomgen.Generate(g, rng, mode.opts)

			// Contract first: a compile hang/crash/internal-error is
			// a canary finding, not a divergence.
			var caps map[string]time.Duration
			if fuzzutil.LooksHangSuspect([]byte(src)) {
				caps = suspect
			}
			var violations []string
			for _, f := range sb.RunCapped(t, []byte(src), caps) {
				if bug, ok := fuzzutil.BugOf(f, knownCompileBugs); ok {
					t.Logf("%s seed %d: compile failure matches known bug %q",
						mode.name, seed, bug.Desc)
					continue
				}
				violations = append(violations, f)
			}
			if len(violations) != 0 {
				t.Errorf("%s seed %d: compile contract violations\n%s--- program ---\n%s",
					mode.name, seed, joinAll(violations), src)
				contractBad++
				continue
			}

			// The sandbox run ended cleanly, so this compile is
			// deterministic and safe in-process.
			r, cerr := build.Compile(build.Input{Path: "gen.gom", Src: []byte(src)}, opts)
			if cerr != nil || r == nil {
				t.Errorf("%s seed %d: second compile failed where the sandboxed one did not: %v",
					mode.name, seed, cerr)
				contractBad++
				continue
			}
			if r.Diagnostics.HasError() || r.Output == nil {
				rejected++
				continue
			}

			// Eval side first: an unsupported program costs no `go
			// build`, and its eval fuel bound is cheap.
			eo := evalDiff(t, r, src)
			switch eo.class {
			case dUnsupported:
				evalUnsupported++
				continue
			case dNonterm:
				evalFuel++
			case dInternal:
				evalInternal++
			}

			dir := t.TempDir()
			result := make(chan nativeResult, 1)
			workers <- struct{}{}
			running.Add(1)
			go func(code []byte) {
				defer running.Done()
				defer func() { <-workers }()
				co, nondeterministic, err := compiledDiffInDir(gobin, root, dir, code)
				result <- nativeResult{co, nondeterministic, err}
			}(r.Output.Source)
			jobs = append(jobs, nativeJob{seed, src, eo, result})
		}
		for _, job := range jobs {
			seed, src, eo := job.seed, job.src, job.eval
			result := <-job.result
			co, nondeterministic, buildErr := result.out, result.nondeterministic, result.err
			if buildErr != "" {
				// Generated Go that typechecks under the sandbox's
				// gc importer but fails `go build` is a
				// contract-side finding, not a divergence — and it
				// goes through the same knownCompileBugs matching,
				// since a registered bug can surface at either
				// typecheck gate.
				failure := fmt.Sprintf("%s seed %d: generated Go failed go build after passing the contract\n%s",
					mode.name, seed, buildErr)
				if bug, ok := fuzzutil.BugOf(failure, knownCompileBugs); ok {
					t.Logf("%s — matches known bug %q", failure, bug.Desc)
				} else {
					t.Errorf("%s\n--- program ---\n%s", failure, src)
				}
				contractBad++
				continue
			}
			if nondeterministic {
				nondet++
				if diffReport(t, mode.name, seed, src, "compiled output differs between two identical runs",
					eo, co) {
					known++
				}
				continue
			}
			if co.class == dNonterm {
				compiledTimeout++
			}
			if div := diffCompare(eo, co); div != "" {
				divergent++
				if diffReport(t, mode.name, seed, src, div, eo, co) {
					known++
				}
				continue
			}
			compared++
		}
		t.Logf(`%s mode: %d generated, %d compared cleanly, %d divergent
    filtered out: %d user diagnostics, %d eval-unsupported (unstubbed Go member), %d compile-contract
    resource bounds: %d eval fuel exhausted, %d compiled timeouts; %d nondeterministic programs, %d eval-internal, %d known-divergence hits`,
			mode.name, generated, compared, divergent,
			rejected, evalUnsupported, contractBad,
			evalFuel, compiledTimeout, nondet, evalInternal, known)
	}
}

// TestDiffEvalRecursion is the regression probe for the
// interpreter's fatal failure mode: `f x = f x` nests a Go frame
// per saturated call, and before the call-depth bound existed it
// reached the unrecoverable goroutine-stack ceiling (~300K frames,
// below any throughput-sized fuel budget) and killed the whole test
// binary. The verdict must now be an attributable dNonterm — the
// same class the compiled side reports when the lowered tail loop
// spins into its timeout — never a dead process.
func TestDiffEvalRecursion(t *testing.T) {
	opts, _ := compileOracles(t)
	if opts == nil {
		t.Skip("no go toolchain on PATH — cannot typecheck the probe")
	}
	src := "package main\nfn main () -> Unit = main ()\n"
	r, cerr := build.Compile(build.Input{Path: "rec.gom", Src: []byte(src)}, opts)
	if cerr != nil {
		t.Fatalf("compile: %v", cerr)
	}
	if r.Diagnostics.HasError() || r.Output == nil {
		t.Fatalf("probe program rejected: %v", r.Diagnostics)
	}
	if o := evalDiff(t, r); o.class != dNonterm {
		t.Fatalf("eval class = %s (%s), want nonterm (E9006 depth/fuel budget)",
			className(o.class), o.detail)
	}
}

// TestDiffTailRec: a program under `@tailrec` must produce the same
// answer under the interpreter and under the compiled `for` loop —
// the attribute is a requirement check (E0902–E0905), not a
// semantics change.
func TestDiffTailRec(t *testing.T) {
	opts, _ := compileOracles(t)
	if opts == nil {
		t.Skip("no go toolchain on PATH — cannot typecheck the probe")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH — the compiled side cannot run")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	src := `package main

import go "fmt"

@tailrec
fn sum (n: Int) (acc: Int) -> Int =
  if n <= 0 { acc } else { sum (n - 1) (acc + n) }

fn main () -> Unit = {
  let _ = fmt.Println(sum 100 0)
  ()
}
`
	r, cerr := build.Compile(build.Input{Path: "tailrec.gom", Src: []byte(src)}, opts)
	if cerr != nil {
		t.Fatalf("compile: %v", cerr)
	}
	if r.Diagnostics.HasError() || r.Output == nil {
		t.Fatalf("probe program rejected: %v", r.Diagnostics)
	}
	eo := evalDiff(t, r, src)
	if eo.class == dUnsupported {
		t.Skipf("eval cannot stub the probe's Go calls: %s", eo.detail)
	}
	co, nondeterministic, buildErr := compiledDiff(t, gobin, root, r.Output.Source)
	if buildErr != "" {
		t.Fatalf("generated Go failed go build:\n%s", buildErr)
	}
	if nondeterministic {
		t.Skip("compiled output is nondeterministic")
	}
	if div := diffCompare(eo, co); div != "" {
		t.Fatalf("eval/compiled divergence on @tailrec program: %s\neval=%s %q compiled=%s %q\n--- program ---\n%s",
			div, className(eo.class), eo.stdout, className(co.class), co.stdout, src)
	}
	if eo.stdout != "5050\n" {
		t.Fatalf("stdout = %q, want %q", eo.stdout, "5050\n")
	}
}

// TestDiffGoCallSpread: `..e` inside a Go call splices the list into
// the variadic tail — Go's `f(xs...)`. The evaluator must produce
// the same flat argument list the compiled `xs...` does: the stub
// and fmt.Println both render their arguments space-separated.
func TestDiffGoCallSpread(t *testing.T) {
	opts, _ := compileOracles(t)
	if opts == nil {
		t.Skip("no go toolchain on PATH — cannot typecheck the probe")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH — the compiled side cannot run")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	src := `package main

import go "fmt"

fn main () -> Unit = {
  let xs = ["a", "b"]
  let ys : List String = []
  let _ = fmt.Println(..xs)
  let _ = fmt.Println(..ys)
  ()
}
`
	r, cerr := build.Compile(build.Input{Path: "gospread.gom", Src: []byte(src)}, opts)
	if cerr != nil {
		t.Fatalf("compile: %v", cerr)
	}
	if r.Diagnostics.HasError() || r.Output == nil {
		t.Fatalf("probe program rejected: %v", r.Diagnostics)
	}
	eo := evalDiff(t, r, src)
	if eo.class == dUnsupported {
		t.Skipf("eval cannot stub the probe's Go calls: %s", eo.detail)
	}
	co, nondeterministic, buildErr := compiledDiff(t, gobin, root, r.Output.Source)
	if buildErr != "" {
		t.Fatalf("generated Go failed go build:\n%s", buildErr)
	}
	if nondeterministic {
		t.Skip("compiled output is nondeterministic")
	}
	if div := diffCompare(eo, co); div != "" {
		t.Fatalf("eval/compiled divergence on spread program: %s\neval=%s %q compiled=%s %q\n--- program ---\n%s",
			div, className(eo.class), eo.stdout, className(co.class), co.stdout, src)
	}
	if eo.stdout != "a b\n\n" {
		t.Fatalf("stdout = %q, want %q", eo.stdout, "a b\n\n")
	}
}

// diffCompare reduces two outcomes to a divergence description —
// "" means the observations agree.
func diffCompare(e, c diffOutcome) string {
	if e.class == dInternal {
		return "evaluator hit an internal invariant (" + e.detail + ") on a program that compiled cleanly"
	}
	if e.class == dNonterm || c.class == dNonterm {
		if e.class == dNonterm && c.class == dNonterm {
			return ""
		}
		return fmt.Sprintf("termination differs: eval=%s compiled=%s (fuel/timeout budget, retriage before filing)",
			className(e.class), className(c.class))
	}
	if e.class != c.class {
		return fmt.Sprintf("exit class differs: eval=%s compiled=%s",
			className(e.class), className(c.class))
	}
	if e.stdout != c.stdout {
		return fmt.Sprintf("stdout differs: eval=%q compiled=%q", e.stdout, c.stdout)
	}
	return ""
}

// className renders a class for reports.
func className(c diffClass) string {
	switch c {
	case dClean:
		return "clean"
	case dPanic:
		return "panic"
	case dNonterm:
		return "nonterm"
	case dUnsupported:
		return "eval-unsupported"
	case dInternal:
		return "eval-internal"
	default:
		return "?"
	}
}

// diffReport applies the knownDivergences policy and emits the
// finding: a registered signature logs (and reports true), a new one
// fails the test with the full program for triage.
func diffReport(t *testing.T, mode string, seed uint64, src, what string, e, c diffOutcome) (known bool) {
	t.Helper()
	failure := fmt.Sprintf("%s seed %d divergence: %s\neval:    %s %q (%s)\ncompiled: %s %q (%s)",
		mode, seed, what,
		className(e.class), e.stdout, e.detail,
		className(c.class), c.stdout, c.detail)
	if bug, ok := fuzzutil.BugOf(failure, knownDivergences); ok {
		t.Logf("%s — matches known divergence %q (%s)", failure, bug.Desc, bug.TODO)
		return true
	}
	t.Errorf("%s\n--- program ---\n%s", failure, src)
	return false
}

// diffSeeds reads GOM_DIFF_SEEDS or returns the committed default.
func diffSeeds(t *testing.T) int {
	t.Helper()
	if s := os.Getenv(diffSeedsEnv); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return diffSeedsDef
}

// diffBudget reads GOM_DIFF_BUDGET (a Go duration) or returns the
// committed default.
func diffBudget(t *testing.T) time.Duration {
	t.Helper()
	if s := os.Getenv(diffBudgetEnv); s != "" {
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			return d
		}
	}
	return diffBudgetDef
}

// ------------------------------------------------------------------
// eval side
// ------------------------------------------------------------------

// diffCapture carries the state the host stubs write to: the
// collected stdout bytes and the "saw an arg the renderer cannot
// reproduce" flag that turns the run into unexecutable rather than
// wrong.
type diffCapture struct {
	out        *strings.Builder
	unfaithful bool
}

// evalDiff runs the typed file under the evaluator with the fuel
// budget and the Go-member stubs, and classifies the outcome. A
// panic escaping the evaluator — an interpreter bug, not a Gom
// failure — is attributed to this seed as dInternal instead of
// crashing the whole diff run (the call-depth bound covers the
// unrecoverable stack-exhaustion case; this net is for everything
// else).
func evalDiff(t *testing.T, r *build.Result, sources ...string) (o diffOutcome) {
	t.Helper()
	var out strings.Builder
	capture := &diffCapture{out: &out}
	defer func() {
		if rec := recover(); rec != nil {
			o = diffOutcome{class: dInternal, stdout: out.String(),
				detail: fmt.Sprintf("evaluator panicked: %v", rec)}
		}
	}()
	ev := eval.New(r.Typed)
	if len(sources) != 0 {
		ev.SetLineIndex(diag.NewLineIndex([]byte(sources[0])))
	}
	ev.SetMaxSteps(diffEvalFuel)
	bindGoStubs(t, r.Typed, ev, capture)
	res := ev.Run()

	// An unfaithful print poisons EVERY outcome class, not only the
	// clean one: the captured stdout has a hole where the
	// unrenderable value belonged, so comparing it — even under a
	// matching panic/nonterm class — fabricates a "stdout differs"
	// divergence from bytes the harness itself failed to produce.
	if capture.unfaithful {
		return diffOutcome{class: dUnsupported, stdout: out.String(),
			detail: "a Go call observed a value the harness cannot render faithfully"}
	}

	if diag.List(res.Diags).HasError() {
		code := ""
		var msgs []string
		for _, d := range res.Diags {
			if d.Severity == diag.SeverityError {
				if code == "" {
					code = string(d.Code)
				}
				msgs = append(msgs, fmt.Sprintf("%s %s", d.Code, d.Message))
			}
		}
		detail := strings.Join(msgs, "; ")
		switch diag.Code(code) {
		case eval.CodeUnsupported:
			return diffOutcome{class: dUnsupported, stdout: out.String(), detail: detail}
		case eval.CodeMatchFail, eval.CodePanic, eval.CodeTestFail:
			return diffOutcome{class: dPanic, stdout: out.String(), detail: detail}
		case eval.CodeBudget:
			return diffOutcome{class: dNonterm, stdout: out.String(), detail: detail}
		default:
			return diffOutcome{class: dInternal, stdout: out.String(), detail: detail}
		}
	}
	o = diffOutcome{class: dClean, stdout: out.String()}
	if res.Value != nil {
		o.detail = res.Value.String()
	}
	return o
}

// bindGoStubs walks the typed file for Go member references — the
// callee symbols of CallGo applications plus member/variable
// expressions whose symbol carries IsGo — and binds each one the
// stub table covers into the evaluator's environment. Members with
// no faithful stub stay unbound so evalMember's E9001 classifies the
// program as unexecutable.
//
// Method symbols key differently: a method belongs to a receiver
// type, not the package namespace, so the stub name is
// `pkg.Type.Method` derived from the receiver's checked type — the
// same triple the loader interns it under — and the stub receives
// the receiver as its first argument.
func bindGoStubs(t *testing.T, f *check.File, ev *eval.Evaluator, capture *diffCapture) {
	t.Helper()
	bindNamed := func(sym *resolve.Symbol, name string, arity int) {
		if sym == nil || !sym.IsGo {
			return
		}
		if fn, ok := diffGoStubs[name]; ok {
			ev.Env().Bind(sym, eval.NewHostFunc(name, arity, fn(capture)))
			return
		}
		if v, ok := diffGoValues[name]; ok {
			ev.Env().Bind(sym, v)
		}
	}
	bind := func(sym *resolve.Symbol, arity int) {
		if sym == nil {
			return
		}
		bindNamed(sym, sym.Package+"."+sym.Name, arity)
	}
	for _, d := range f.Decls {
		check.WalkDecl(d, func(x check.Expr) {
			// Single-case assertions, not a type switch: most Expr
			// kinds carry no Go reference and the sumtype check
			// would demand a case for every one.
			if a, ok := x.(*check.Apply); ok && a.Call.Kind == check.CallGo {
				arity := a.Call.Arity
				if arity < 1 {
					arity = 1
				}
				if me, ok := a.Fn.(*check.MemberExpr); ok && a.Call.Callee != nil && a.Call.Callee.Kind == resolve.KindMethod {
					bindNamed(a.Call.Callee, methodStubName(me), arity+1)
				} else {
					bind(a.Call.Callee, arity)
				}
			}
			if m, ok := x.(*check.MemberExpr); ok {
				if m.Sym != nil && m.Sym.Kind == resolve.KindMethod {
					// A method reference's arity is its value-type
					// parameter count; the stub adds the receiver.
					p, _ := flattenType(m.Type())
					bindNamed(m.Sym, methodStubName(m), len(p)+1)
				} else {
					bind(m.Sym, 1)
				}
			}
			if v, ok := x.(*check.Var); ok {
				bind(v.Sym, 1)
			}
		})
	}
}

// methodStubName derives the diffGoStubs key for a method member:
// `pkg.Type.Method` — the receiver type's package path and name plus
// the method name, matching the triple goimport interns the symbol
// under. A non-Go receiver shape (the GoError builtin, or a Ptr to a
// named type) reduces to the same dotted name.
func methodStubName(me *check.MemberExpr) string {
	t := me.Recv.Type()
	for {
		if c, ok := types.Chase(t).(*types.Con); ok && c.Qual == "" && c.Name == "Ptr" && len(c.Args) == 1 {
			t = c.Args[0]
			continue
		}
		break
	}
	if g, ok := types.Chase(t).(*types.Go); ok {
		return g.Ref.Path + "." + g.Ref.Name + "." + me.Sym.Name
	}
	return me.Sym.Package + "." + me.Sym.Name
}

// flattenType unrolls a function type into its parameter list and
// result — the checker's own flattenFunc, unexported there.
func flattenType(t types.Type) ([]types.FuncParam, types.Type) {
	var params []types.FuncParam
	for {
		f, ok := types.Chase(t).(*types.Func)
		if !ok {
			return params, t
		}
		params = append(params, f.Params...)
		t = f.Ret
	}
}

// diffGoStubs is the audited table of Go members the harness can
// reproduce faithfully under the evaluator. Each entry receives the
// capture so output functions can collect stdout. Anything not
// listed — mutating sorts, format-string printing, clock/env reads —
// is left unbound on purpose (E9001 → unexecutable). Implementations
// delegate to the real Go functions so behavior matches byte for
// byte where the host function is pure.
var diffGoStubs = map[string]func(capture *diffCapture) func([]eval.Value) (eval.Value, error){
	"fmt.Println": func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			return goPrint(capture, args, true), nil
		}
	},
	"fmt.Print": func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			return goPrint(capture, args, false), nil
		}
	},
	"strconv.Itoa": func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			if i, ok := args[0].(*eval.VInt); ok {
				return &eval.VStr{S: strconv.Itoa(int(i.N))}, nil
			}
			return goBadArgs("strconv.Itoa", args)
		}
	},
	"strconv.Atoi": func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			s, ok := args[0].(*eval.VStr)
			if !ok {
				return goBadArgs("strconv.Atoi", args)
			}
			n, err := strconv.Atoi(s.S)
			if err != nil {
				return goErrVariant(err.Error()), nil
			}
			return goOkVariant(&eval.VInt{N: int64(n)}), nil
		}
	},
	"strings.ToUpper":   strStub(strings.ToUpper),
	"strings.ToLower":   strStub(strings.ToLower),
	"strings.TrimSpace": strStub(strings.TrimSpace),
	"strings.Repeat": func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			s, ok1 := args[0].(*eval.VStr)
			n, ok2 := args[1].(*eval.VInt)
			if !ok1 || !ok2 {
				return goBadArgs("strings.Repeat", args)
			}
			return &eval.VStr{S: strings.Repeat(s.S, int(n.N))}, nil
		}
	},
	"strings.Contains":  strPred(strings.Contains),
	"strings.HasPrefix": strPred(strings.HasPrefix),
	"strings.HasSuffix": strPred(strings.HasSuffix),
	"strings.Join": func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			xs, ok1 := args[0].(*eval.VList)
			sep, ok2 := args[1].(*eval.VStr)
			if !ok1 || !ok2 {
				return goBadArgs("strings.Join", args)
			}
			var parts []string
			for _, e := range xs.Elems {
				s, ok := e.(*eval.VStr)
				if !ok {
					return goBadArgs("strings.Join", args)
				}
				parts = append(parts, s.S)
			}
			return &eval.VStr{S: strings.Join(parts, sep.S)}, nil
		}
	},
	"math.Abs":   floatStub(math.Abs),
	"math.Floor": floatStub(math.Floor),
	"math.Ceil":  floatStub(math.Ceil),
	"math.Trunc": floatStub(math.Trunc),
	"math.Sqrt":  floatStub(math.Sqrt),
	"math.Max": func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			a, ok1 := args[0].(*eval.VFloat)
			b, ok2 := args[1].(*eval.VFloat)
			if !ok1 || !ok2 {
				return goBadArgs("math.Max", args)
			}
			return &eval.VFloat{F: math.Max(a.F, b.F)}, nil
		}
	},
	"math.Min": func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			a, ok1 := args[0].(*eval.VFloat)
			b, ok2 := args[1].(*eval.VFloat)
			if !ok1 || !ok2 {
				return goBadArgs("math.Min", args)
			}
			return &eval.VFloat{F: math.Min(a.F, b.F)}, nil
		}
	},
	"errors.New": func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			s, ok := args[0].(*eval.VStr)
			if !ok {
				return goBadArgs("errors.New", args)
			}
			return &eval.VGoErr{Text: s.S}, nil
		}
	},
	"slices.Contains": func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			xs, ok := args[0].(*eval.VList)
			if !ok || len(args) != 2 {
				return goBadArgs("slices.Contains", args)
			}
			for _, e := range xs.Elems {
				if eval.Equal(e, args[1]) {
					return &eval.VBool{B: true}, nil
				}
			}
			return &eval.VBool{}, nil
		}
	},
	"unicode/utf8.RuneCountInString": func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			s, ok := args[0].(*eval.VStr)
			if !ok {
				return goBadArgs("utf8.RuneCountInString", args)
			}
			return &eval.VInt{N: int64(utf8.RuneCountInString(s.S))}, nil
		}
	},
	// Methods key as `pkg.Type.Method` (see methodStubName); the
	// receiver arrives as args[0] ahead of the declared parameters.
	// Receivers are VHost values produced by earlier host calls.
	"time.Unix": func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			sec, ok1 := args[0].(*eval.VInt)
			nsec, ok2 := args[1].(*eval.VInt)
			if !ok1 || !ok2 {
				return goBadArgs("time.Unix", args)
			}
			return &eval.VHost{V: time.Unix(sec.N, nsec.N)}, nil
		}
	},
	"time.Time.AddDate": func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			recv, ok := args[0].(*eval.VHost)
			if !ok || len(args) != 4 {
				return goBadArgs("time.Time.AddDate", args)
			}
			t, ok := recv.V.(time.Time)
			if !ok {
				return goBadArgs("time.Time.AddDate", args)
			}
			y, ok1 := args[1].(*eval.VInt)
			m, ok2 := args[2].(*eval.VInt)
			d, ok3 := args[3].(*eval.VInt)
			if !ok1 || !ok2 || !ok3 {
				return goBadArgs("time.Time.AddDate", args)
			}
			return &eval.VHost{V: t.AddDate(int(y.N), int(m.N), int(d.N))}, nil
		}
	},
	"time.Time.Format": func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			recv, ok := args[0].(*eval.VHost)
			if !ok || len(args) != 2 {
				return goBadArgs("time.Time.Format", args)
			}
			t, ok := recv.V.(time.Time)
			if !ok {
				return goBadArgs("time.Time.Format", args)
			}
			layout, ok := args[1].(*eval.VStr)
			if !ok {
				return goBadArgs("time.Time.Format", args)
			}
			return &eval.VStr{S: t.Format(layout.S)}, nil
		}
	},
	"strings.Builder.WriteString": func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			recv, ok := args[0].(*eval.VHost)
			if !ok || len(args) != 2 {
				return goBadArgs("strings.Builder.WriteString", args)
			}
			b, ok := recv.V.(*strings.Builder)
			if !ok {
				return goBadArgs("strings.Builder.WriteString", args)
			}
			s, ok := args[1].(*eval.VStr)
			if !ok {
				return goBadArgs("strings.Builder.WriteString", args)
			}
			n, err := b.WriteString(s.S)
			if err != nil {
				return goErrVariant(err.Error()), nil
			}
			return goOkVariant(&eval.VInt{N: int64(n)}), nil
		}
	},
	// The builtin GoError receiver's only method: the checker's
	// synthetic symbol is Package "builtin", Name "Error" — the
	// methodStubName fallback keys it here. Its receiver is a VGoErr.
	"builtin.Error": func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			recv, ok := args[0].(*eval.VGoErr)
			if !ok || len(args) != 1 {
				return goBadArgs("builtin.Error", args)
			}
			return &eval.VStr{S: recv.Text}, nil
		}
	},
}

// diffGoValues holds the pure constants a generated program can
// reach through a Go module member in value position (math.Pi and
// friends). Functions absent here and in diffGoStubs stay unbound.
var diffGoValues = map[string]eval.Value{
	"math.Pi": &eval.VFloat{F: math.Pi},
	"math.E":  &eval.VFloat{F: math.E},
}

// strStub wraps a pure string→string function.
func strStub(f func(string) string) func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
	return func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			s, ok := args[0].(*eval.VStr)
			if !ok {
				return goBadArgs("stub", args)
			}
			return &eval.VStr{S: f(s.S)}, nil
		}
	}
}

// strPred wraps a pure (string, string)→bool function.
func strPred(f func(string, string) bool) func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
	return func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			a, ok1 := args[0].(*eval.VStr)
			b, ok2 := args[1].(*eval.VStr)
			if !ok1 || !ok2 {
				return goBadArgs("stub", args)
			}
			return &eval.VBool{B: f(a.S, b.S)}, nil
		}
	}
}

// floatStub wraps a pure float64→float64 function.
func floatStub(f func(float64) float64) func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
	return func(capture *diffCapture) func([]eval.Value) (eval.Value, error) {
		return func(args []eval.Value) (eval.Value, error) {
			a, ok := args[0].(*eval.VFloat)
			if !ok {
				return goBadArgs("stub", args)
			}
			return &eval.VFloat{F: f(a.F)}, nil
		}
	}
}

// goPrint is the fmt.Print-family stub: each argument renders the
// way Go's %v renders the type the Gom value lowers to, args join by
// single spaces, and Println adds the trailing newline. The return
// mirrors `func(...) (n int, err error)` mapped to Result — Ok with
// the byte count — so `try` and `let _ =` see the shape check
// assigned.
func goPrint(capture *diffCapture, args []eval.Value, nl bool) eval.Value {
	var b strings.Builder
	for i, a := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		s, ok := goDisplay(a)
		if !ok {
			capture.unfaithful = true
		}
		b.WriteString(s)
	}
	if nl {
		b.WriteByte('\n')
	}
	text := b.String()
	capture.out.WriteString(text)
	return goOkVariant(&eval.VInt{N: int64(len(text))})
}

// goDisplay renders v the way Go's fmt %v renders the representation
// v's type lowers to: Int→int64, Float→float64 ('g' shortest), Char
// prints its code point (rune is numeric), String unquoted, Unit the
// empty struct. Composites follow the Go shapes — a Gom list is a
// slice `[a b]`, a tuple and a record are structs `{a b}` — while
// values whose Go representation the harness cannot reproduce (ADTs,
// function pointers) report unfaithful so the run is counted
// unexecutable rather than divergent.
func goDisplay(v eval.Value) (string, bool) {
	switch v := v.(type) {
	case *eval.VUnit:
		return "{}", true
	case *eval.VBool:
		return strconv.FormatBool(v.B), true
	case *eval.VInt:
		return strconv.FormatInt(v.N, 10), true
	case *eval.VFloat:
		return strconv.FormatFloat(v.F, 'g', -1, 64), true
	case *eval.VChar:
		return strconv.FormatInt(int64(v.R), 10), true
	case *eval.VStr:
		return v.S, true
	case *eval.VGoErr:
		return v.Text, true
	case *eval.VHost:
		return fmt.Sprintf("%v", v.V), true
	case *eval.VList:
		var b strings.Builder
		b.WriteByte('[')
		for i, e := range v.Elems {
			if i > 0 {
				b.WriteByte(' ')
			}
			s, ok := goDisplay(e)
			if !ok {
				return "", false
			}
			b.WriteString(s)
		}
		b.WriteByte(']')
		return b.String(), true
	case *eval.VTuple:
		return goBrace(v.Elems)
	case *eval.VRecord:
		var b strings.Builder
		b.WriteByte('{')
		for i, f := range v.Fields {
			if i > 0 {
				b.WriteByte(' ')
			}
			s, ok := goDisplay(f.V)
			if !ok {
				return "", false
			}
			b.WriteString(s)
		}
		b.WriteByte('}')
		return b.String(), true
	case *eval.VVariant, *eval.VFunc:
		return "", false
	default:
		return "", false
	}
}

// goBrace renders a tuple's `{a b}` Go-struct form.
func goBrace(elems []eval.Value) (string, bool) {
	var b strings.Builder
	b.WriteByte('{')
	for i, e := range elems {
		if i > 0 {
			b.WriteByte(' ')
		}
		s, ok := goDisplay(e)
		if !ok {
			return "", false
		}
		b.WriteString(s)
	}
	b.WriteByte('}')
	return b.String(), true
}

// goOkVariant builds the `Ok v` of a Result value the Go-call
// boundary returns (Type is left nil: matchPat and IsResult resolve
// the constructor by name).
func goOkVariant(v eval.Value) eval.Value {
	return &eval.VVariant{Name: "Ok", Fields: []eval.VField{{Index: 0, V: v}}}
}

// goErrVariant builds `Err e` wrapping a Go error text — the shape
// result.FromErr produces on the compiled side.
func goErrVariant(text string) eval.Value {
	return &eval.VVariant{Name: "Err", Fields: []eval.VField{{Index: 0, V: &eval.VGoErr{Text: text}}}}
}

// goBadArgs reports an argument shape the stub does not model —
// surfaced as a Go-level panic class (a wrong-typed call is a check
// error upstream, so this only fires on a checker gap).
func goBadArgs(name string, args []eval.Value) (eval.Value, error) {
	var kinds []string
	for _, a := range args {
		kinds = append(kinds, a.String())
	}
	return nil, fmt.Errorf("%s: unmodeled argument values %s", name, strings.Join(kinds, ", "))
}

// ------------------------------------------------------------------
// compiled side
// ------------------------------------------------------------------

// compiledDiff drops the generated file into the same scratch-module
// shape as runCompiled, `go build`s it once, then runs the binary
// twice under a wall-clock timeout — the second run is the
// determinism check the Deterministic subset promises. A nonempty
// buildErr means the toolchain rejected or failed to build the
// generated file — a contract-side finding, not a divergence.
func compiledDiff(t *testing.T, gobin, root string, src []byte) (out diffOutcome, nondeterministic bool, buildErr string) {
	t.Helper()
	return compiledDiffInDir(gobin, root, t.TempDir(), src)
}

// compiledDiffInDir does not use testing.T, so bounded workers can run it
// without sharing compiler state or calling FailNow from a child goroutine.
func compiledDiffInDir(gobin, root, dir string, src []byte) (out diffOutcome, nondeterministic bool, buildErr string) {
	gomod := "module e2e\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.0\n\nreplace github.com/podhmo/gom => " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		return diffOutcome{}, false, fmt.Sprintf("writing generated fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), src, 0o644); err != nil {
		return diffOutcome{}, false, fmt.Sprintf("writing generated fixture: %v", err)
	}
	env := append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")

	bctx, bcancel := context.WithTimeout(context.Background(), diffBuildTimeout)
	binary, berr := testbin.Build(bctx, gobin, dir, env)
	bout := binary.Output
	bcancel()
	if berr != nil {
		// The generated source goes into the failure text (bounded
		// by gomgen's token budget): the sandbox's typecheck report
		// embeds it too, so known-bug signatures can discriminate
		// on emitted constructs — a bad type switch's line carries
		// ".(type)", an invalid type assertion's does not — that
		// both report "is not an interface".
		return diffOutcome{}, false, fmt.Sprintf("%v\n%.500s\n--- generated ---\n%s",
			berr, bout, src)
	}
	prog := binary.Path
	r1 := runOnce(dir, env, prog)
	r2 := runOnce(dir, env, prog)
	// Determinism check: every fully-observed output must be
	// byte-identical between the two runs — a dPanic run's stdout is
	// as much program output as a clean run's, so it is compared
	// too. A timeout's capture is skipped: it is truncated wherever
	// the kill landed, so identical programs legitimately produce
	// different byte counts and comparing them fabricates
	// nondeterminism.
	if r1.class != r2.class ||
		(r1.class != dNonterm && r1.stdout != r2.stdout) {
		return r1, true, ""
	}
	return r1, false, ""
}

// runOnce executes the compiled binary under diffRunTimeout and
// classifies the run: clean exit, nonzero exit (panic class), or
// wall-clock timeout. stdout is captured separately from stderr so a
// print-then-panic program still reports the stdout it produced —
// the evaluator's side mirrors the same split.
func runOnce(dir string, env []string, prog string) diffOutcome {
	ctx, cancel := context.WithTimeout(context.Background(), diffRunTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, prog)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return diffOutcome{class: dNonterm, detail: "binary exceeded " + diffRunTimeout.String()}
	}
	if err != nil {
		return diffOutcome{class: dPanic, stdout: stdout.String(),
			detail: fmt.Sprintf("exit error: %v\n%.400s", err, tail(stderr.Bytes(), 400))}
	}
	return diffOutcome{class: dClean, stdout: stdout.String()}
}

// tail keeps the last n bytes of b — panic output puts the message
// early but the goroutine dump can be long, so keep both ends small.
func tail(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n/2]) + "..." + string(b[len(b)-n/2:])
}
