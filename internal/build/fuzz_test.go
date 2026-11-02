package build_test

// Compile-canary fuzz target (docs/decisions/2026-11-02-fuzz-strategy.md).
// Every input goes through internal/build.Compile — the full pipeline
// — under the same oracles cmd/gom wires (an internal/goimport
// Loader for `import go`). The contract:
//
//   - never panic, never return an internal error, never hang
//   - every diagnostic is well-formed AND none carries E0901 — a
//     "core invariant violation" reaching the user is always a
//     compiler bug, whatever the input
//   - produced output implies error-free diagnostics (driver
//     invariant), and the generated Go must PARSE and TYPECHECK —
//     go/types over the same `go list -export` mechanism the
//     interop loader uses. `:=` shadowing, nonexistent types and
//     `==` on slices all surface here without a full `go build`.
//
// `import go` resolves only an allowlist of std packages plus this
// module's own — every other path errors quickly in resolve (E0203)
// instead of spending a `go list` subprocess per mutated path.
// Without a `go` toolchain on PATH the canary degrades to nil
// options + parse-only output checking.
//
// Bugs already filed in TODO.md live in knownCompileBugs (see
// internal/fuzzutil); seeds that reproduce them run for coverage but
// their recorded failure is skipped.

import (
	"fmt"
	"go/ast"
	"go/importer"
	goparser "go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/fuzzutil"
	"github.com/podhmo/gom/internal/goimport"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
)

// knownCompileBugs is capped — see internal/fuzzutil. The initial
// fuzz sweep registered ~17 bug classes; 24 leaves headroom for new
// classes while still forcing the list to be pruned rather than
// grown forever.
const knownBugsCap = 24

var knownCompileBugs = map[[32]byte]fuzzutil.KnownBug{
	// (The registry is empty for now — recent fixes retired every
	// entry. The parse-phase bugs (ctor-pattern '-' loop, the
	// E0102 negative span, raw token text in messages) were fixed
	// in internal/syntax; the zero-argument Go call, the
	// module-name-in-value-position family, and the memberExpr
	// panic were fixed by fix-lower-bugs; and the type-variable
	// clusters — the unbound `A`/`B00`/`C` of polymorphic
	// signatures and the type-var-vs-body "differs from" E0901s —
	// were fixed in P2-1, which emits FnDecl.TypeParams as Go
	// `[A any]` and reports the mismatch as E0401/E0412 in check.
	// Those corpus files and seeds run live as regression seeds.)
	// (The raw-byte-in-string-literal pair — `fmt.Print("\xeb")` and
	// `"\x00"` — is fixed in the lexer: E0105 rejects the literal at
	// lex time, so the corpus files now run live.)
	// (`0.A00()` — member access on a numeric literal — is fixed in
	// check: selections on an unresolved receiver meta defer to
	// finalize, where the literal's Int default reports E0401
	// instead of a nil-symbol panic in lower.memberExpr.)
	// (`let a=080` is fixed in the parser: a leading-zero decimal
	// literal is normalized to its value's decimal spelling — `80` —
	// so the corpus file now compiles cleanly.)
	// (`type Point = {x: Point}` — a record type that contains
	// itself by value — is fixed in check: E0413 rejects by-value
	// record recursion, including mutual cycles and cycles through
	// Result/Task/tuple payloads; recursion across a sum-type
	// variant, a function type, or a reference-shaped builtin stays
	// legal. The corpus file now reports the diagnostic instead of
	// emitting an invalid Go struct. Found by the gomgen canary.)
	// (`match (Some 1)` — a constructor-application scrutinee whose
	// `tmp := Option_Some[T]{...}` let bound the concrete variant
	// struct, breaking the emitted type switch — is fixed in
	// codegen: the let now converts the RHS to the binder's
	// declared sum-interface type. The seed runs live in
	// compileSeeds.)
	// (`fmt.Println (match ...)` / `fmt.Println (if ...)` — a
	// branching expression nested in a variadic `...any` argument
	// whose node type unified to Go.Any and tripped lower's
	// invariants — is fixed in check: `Go.Any` as the expected
	// type no longer propagates into If/Match/SelectExpr, which
	// infer their own type and conform to `any`. The seed runs
	// live in compileSeeds.)
}

// fuzzGoPkgs bounds which `import go` paths the loader will resolve
// — everything else errors in resolve (E0203) instead of spending a
// `go list -export` subprocess per mutated path. Module-internal
// paths always pass (they resolve locally, no network possible).
var fuzzGoPkgs = map[string]bool{
	"bytes": true, "errors": true, "fmt": true, "io": true,
	"math": true, "os": true, "slices": true, "sort": true,
	"strconv": true, "strings": true, "time": true, "unicode/utf8": true,
}

// allowLoader forwards to the shared goimport.Loader only for
// allowlisted paths.
type allowLoader struct{ inner *goimport.Loader }

func (a allowLoader) Load(path string, goPkg bool) (*resolve.Package, error) {
	if goPkg && !fuzzGoPkgs[path] && !strings.HasPrefix(path, "github.com/podhmo/gom/") {
		return nil, fmt.Errorf("fuzz: package %q outside the allowlist", path)
	}
	return a.inner.Load(path, goPkg)
}

// newExportImporter typechecks generated code: a gc importer whose
// lookup asks `go list -export` for each path once (memoized — the
// same mechanism internal/goimport uses, so what typechecks here is
// what `go build` would see).
func newExportImporter(gobin, dir string) types.Importer {
	var mu sync.Mutex
	cache := map[string]string{}
	errs := map[string]error{}
	open := func(path string) (io.ReadCloser, error) {
		mu.Lock()
		export, ok := cache[path]
		err, failed := errs[path]
		mu.Unlock()
		if !ok && !failed {
			export, err = goExport(gobin, dir, path)
			mu.Lock()
			if err != nil {
				errs[path] = err
			} else {
				cache[path] = export
			}
			mu.Unlock()
		}
		if err != nil {
			return nil, err
		}
		return os.Open(export)
	}
	return importer.ForCompiler(token.NewFileSet(), "gc", open)
}

// goExport is internal/goimport's own lookup, duplicated small: ask
// the toolchain where the compiled export data for path lives.
func goExport(gobin, dir, path string) (string, error) {
	cmd := exec.Command(gobin, "list", "-export", "-f", "{{.Export}}", path)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("go list -export %s: %s", path, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("go list -export %s: %w", path, err)
	}
	export := strings.TrimSpace(string(out))
	if export == "" {
		return "", fmt.Errorf("go list -export %s: no export data", path)
	}
	return export, nil
}

// checkCompile runs the contract on one input and returns one
// failure description per violation (empty = clean). opts may be nil
// (no loader); imp may be nil (no typecheck step).
func checkCompile(src []byte, opts *build.Options, imp types.Importer) (failures []string) {
	var r *build.Result
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				failures = append(failures, fmt.Sprintf("build.Compile panicked\npanic: %v\n%s",
					rec, debug.Stack()))
			}
		}()
		var err error
		r, err = build.Compile(build.Input{Path: "fuzz.gom", Src: src}, opts)
		if err != nil {
			failures = append(failures, fmt.Sprintf(
				"build.Compile returned internal error: %v", err))
		}
	}()
	if len(failures) != 0 {
		return failures
	}

	for i, d := range r.Diagnostics {
		if err := d.Validate(); err != nil {
			failures = append(failures, fmt.Sprintf(
				"diagnostic %d fails validation: %v\ndiag: %+v", i, err, d))
			continue
		}
		if !d.Span.IsZero() && d.Span.End > len(src) {
			failures = append(failures, fmt.Sprintf(
				"diagnostic %d has out-of-bounds span %v (input length %d)\ndiag: %s",
				i, d.Span, len(src), d.Message))
		}
		// E0901 is the internal "core invariant violation" code —
		// surfacing it to a user is always a compiler bug.
		if d.Code == "E0901" {
			failures = append(failures, fmt.Sprintf(
				"diagnostic %d carries internal code E0901: %s\ndiag: %+v",
				i, d.Message, d))
		}
	}

	if r.Output == nil {
		// Rejected input is fine — but it must not have produced
		// output artifacts anyway (driver error-boundary contract).
		return failures
	}
	if r.Diagnostics.HasError() {
		failures = append(failures, fmt.Sprintf(
			"output produced alongside error diagnostics: %v", r.Diagnostics))
	}

	files := map[string][]byte{"gen.go": r.Output.Source}
	if len(r.Output.TestSource) != 0 {
		files["gen_test.go"] = r.Output.TestSource
	}
	fset := token.NewFileSet()
	var parsed []*ast.File
	var pkgName string
	for name, code := range files {
		gf, err := goparser.ParseFile(fset, name, code, 0)
		if err != nil {
			failures = append(failures, fmt.Sprintf(
				"generated Go fails to parse (%s): %v\n---\n%s", name, err, code))
			continue
		}
		pkgName = gf.Name.Name
		parsed = append(parsed, gf)
	}
	if len(parsed) == 0 || imp == nil {
		return failures
	}
	var typeErrs []string
	conf := types.Config{Importer: imp, Error: func(err error) {
		typeErrs = append(typeErrs, err.Error())
	}}
	if _, err := conf.Check(pkgName, fset, parsed, nil); err != nil {
		var sb strings.Builder
		for _, e := range typeErrs {
			sb.WriteString("\n  " + e)
		}
		failures = append(failures, fmt.Sprintf(
			"generated Go fails to typecheck:%s\n---\n%s",
			sb.String(), r.Output.Source))
	}
	return failures
}

// compileSeeds is the spec/tutorial corpus plus handwritten programs
// chosen to reach codegen through different feature paths. Seeds
// that reproduce a filed bug stay — they exercise the staleness
// check.
func compileSeeds(f *testing.F) [][]byte {
	f.Helper()
	seeds := fuzzutil.ReadSeeds(f,
		"../../testdata/spec/*.gom",
		"../../testdata/golden/*.gom",
		"../../tutorial/*/main.gom",
		"../../tutorial/*/*.gom",
		"../syntax/testdata/spec/*.gom",
	)
	hand := []string{
		// ADT + match + guard + ctor patterns
		`package m

type Shape =
  | Circle (r: Float)
  | Rect (w: Float) (h: Float)
  | Empty

fn area (s: Shape) -> Float =
  match s {
    Circle r if r > 0.0 -> 3.14159 * r * r
    Rect w h -> w * h
    _ -> 0.0
  }
`,
		// generics + list literal + recursion
		`package m

fn len2 (xs: List a) -> Int =
  match xs {
    [] -> 0
    [_, ..rest] -> 1 + len2 rest
  }
`,
		// tuple + record + labeled args + string interpolation
		`package m

type Point = { x: Float, y: Float }

fn dist (a: Point) -> (b: Point) -> Float = {
  let dx = a.x - b.x
  let dy = a.y - b.y
  dx * dx + dy * dy
}

fn show (p: Point) -> String = "(${p.x}, ${p.y})"
`,
		// try + |?> + Result
		`package m

fn div (a: Int) (b: Int) -> Result Int String =
  if b == 0 { Err "zero" } else { Ok (a / b) }

fn twice (a: Int) (b: Int) -> Result Int String = {
  let x = try (div a b)
  div x b
}
`,
		// go interop: Result-returning Go call + |?> chain
		`package m

import go "fmt"
import go "strconv"

fn parse (s: String) -> Result Int GoError =
  strconv.Atoi s

fn main () -> Unit = {
  let _ = fmt.Println(parse "42")
  ()
}
`,
		// test block → TestSource emission
		`package m

fn double (x: Int) -> Int = x * 2

test "double works" = {
  assert_eq (double 2) 4
}
`,
		// if-expr + block items + shadowing
		`package m

fn f (x: Int) -> Int = {
  let y = if x > 0 { x } else { 0 - x }
  let y = y + 1
  y
}
`,
		// go func taking/returning funcs + partial application
		`package m

fn add (a: Int) -> Int -> Int = fn b -> a + b

fn mainish () -> Int = add 2 3
`,
		// Regression seeds for bugs fixed on main (formerly knownBugs
		// repros): they now compile cleanly — keeping them as plain
		// seeds means a re-regression reports a NEW failure signature
		// instead of matching a stale registry entry.
		"package m\n\nfn f () -> Bool = [1, 2] == [1, 2]\n",
		"package m\n\nfn f () -> Int = todo ()\n",
		"package m\n\nimport go \"os\"\n\nfn args () -> Go.Slice String = os.Args\n",
		"package m\n\nfn f () -> Unit = select {}\n",
		// P2-1 fixes: generic signatures now emit Go type parameters,
		// and a type-var-vs-body mismatch is E0401, not E0901.
		"package a\nfn helper()->c=helper()",
		"package a\nfn a()->a={}",
		"package a000\nimport go\"fmt\"\nfn a0000000(n:Int)->a00000=if 0>0{}else{}",
		"package a0\nimport go \"fmt\"\nfn a000(a:b00) (b:Int)->Int=00\nfn a000000(a:b00) -> Int =0\nfn a00000(a0000:b)(a00000000:b)->Unit={} ",
		// Former knownBugs repro — the module-misuse family is now
		// E0201 at resolve, so this runs live as a regression seed.
		"package a\nimport go \"fmt\"\nfn a () -> a = {fmt}\n",
		// V-3 diff fixes: the ctor-application scrutinee now binds
		// at the sum-interface type (codegen letConv), and a
		// branching expression in a `...any` argument keeps its own
		// inferred type instead of unifying to Go.Any (check).
		"package m\n\nfn f () -> Int = match (Some 1) {\n  Some x -> x\n  None -> 0\n}\n",
		"package main\nimport go \"fmt\"\n\nfn f () -> Unit = ignore (fmt.Println (match true { true -> \"a\"; false -> \"b\" }))\n",
		"package main\nimport go \"fmt\"\n\nfn f () -> Unit = ignore (fmt.Println (if true { \"a\" } else { \"b\" }))\n",
	}
	for _, s := range hand {
		seeds = append(seeds, []byte(s))
	}
	return seeds
}

// phaseParse is phase gate 1 inside the sandbox: syntax.Parse alone
// so a hang reports WHICH phase hung — the known infinite-loop bug
// lives in the parser and a hang anywhere else is a different bug
// that must not be suppressed under its signature. A parse panic
// reports the same way checkCompile would report it.
func phaseParse(src []byte) (failures []string) {
	defer func() {
		if rec := recover(); rec != nil {
			failures = append(failures, fmt.Sprintf(
				"syntax.Parse panicked\npanic: %v\n%s", rec, debug.Stack()))
		}
	}()
	syntax.Parse(src, "fuzz.gom")
	return nil
}

// compileOracles builds the same oracles cmd/gom wires: a real
// goimport.Loader behind an allowlist, plus a `go list -export`
// typecheck importer for the generated code. Without a `go`
// toolchain on PATH it returns nil oracles — the canary degrades to
// the self-contained pipeline and parse-only output checking.
// Shared by FuzzCompile and its sandbox helper so both paths judge
// an input identically.
func compileOracles(t testing.TB) (opts *build.Options, imp types.Importer) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	gobin, gerr := exec.LookPath("go")
	if gerr != nil {
		t.Logf("no `go` binary on PATH: FuzzCompile runs without `import go` resolution and without typechecking of generated code")
		return nil, nil
	}
	gl := goimport.New(&goimport.Config{Dir: root, GoBin: gobin})
	opts = &build.Options{
		Loader: allowLoader{gl},
		Sigs:   gl,
		Lower:  &lower.Env{GoSig: gl.GoSigOf},
	}
	imp = newExportImporter(gobin, root)
	return opts, imp
}

// sandboxTimeout bounds one input's turn inside the persistent
// helper subprocess. It must cover cold `go list` lookups inside
// the helper (a few hundred ms; the child's loader memoizes across
// inputs); hangs are killed and the child respawned, so nothing
// leaks into this process (see fuzzutil.Sandbox).
const sandboxTimeout = 8 * time.Second

func FuzzCompile(f *testing.F) {
	if len(knownCompileBugs) > knownBugsCap {
		f.Fatalf("knownBugs has %d entries, cap is %d", len(knownCompileBugs), knownBugsCap)
	}
	opts, imp := compileOracles(f)
	if imp != nil {
		// Warm the subprocess caches once at setup: the first
		// `go list -export` per package is a cold toolchain call
		// that should not eat into any input's budget.
		warm := checkCompile([]byte("package m\n\nimport go \"fmt\"\n\nfn main () -> Unit = {\n  let _ = fmt.Println(1)\n  ()\n}\n"), opts, imp)
		f.Logf("warmup compile failures: %v", warm)
	}
	for _, seed := range compileSeeds(f) {
		f.Add(seed)
	}
	sb := fuzzutil.NewSandbox("TestFuzzCompileSandbox", sandboxTimeout, 3<<20)
	// The parse gate gets a tighter bound than the compile-phase
	// default: a legit parse is milliseconds, and the known
	// ctor-pattern hang family (which hangs IN parse) would
	// otherwise burn the full 8 s compile budget per variant.
	sb.SetPhaseTimeout("parse", 1500*time.Millisecond)
	// Suspects (the same family's shape) get a further-shrunk parse
	// cap: the mutator regenerates variants constantly and each
	// costs timeout+respawn — observed whole minutes at 0 execs/s.
	suspect := map[string]time.Duration{"parse": 500 * time.Millisecond}
	defer sb.Close()
	f.Fuzz(func(t *testing.T, src []byte) {
		if bug, ok := fuzzutil.SkipInput(src, knownCompileBugs); ok {
			t.Logf("input pinned as unsafe-to-run known bug %q — skipped", bug.Desc)
			return
		}
		var caps map[string]time.Duration
		if fuzzutil.LooksHangSuspect(src) {
			caps = suspect
		}
		fuzzutil.Report(t, src, sb.RunCapped(t, src, caps), knownCompileBugs)
	})
}

// TestFuzzCompileSandbox is the child side of fuzzutil.Sandbox: it
// loops length-prefixed inputs on stdin and runs the same checks as
// a direct call would, printing PHASE markers and FAIL lines. The
// oracles are built once — the persistent child's loader and
// typecheck importer memoize `go list` results across inputs.
// Skipped unless spawned via EnvSandbox.
func TestFuzzCompileSandbox(t *testing.T) {
	if os.Getenv(fuzzutil.EnvSandbox) != "1" {
		t.Skip("sandbox helper — spawned by FuzzCompile")
	}
	opts, imp := compileOracles(t)
	fuzzutil.ServeSandbox(t, func(src []byte) {
		fuzzutil.SandboxPhase("parse")
		for _, s := range phaseParse(src) {
			fuzzutil.SandboxFail(s)
		}
		fuzzutil.SandboxPhase("compile")
		for _, s := range checkCompile(src, opts, imp) {
			fuzzutil.SandboxFail(s)
		}
	})
}

// TestKnownBugsCompile pins the bookkeeping rules.
func TestKnownBugsCompile(t *testing.T) {
	fuzzutil.CheckRegistry(t, knownCompileBugs, knownBugsCap, "../../TODO.md")
}
