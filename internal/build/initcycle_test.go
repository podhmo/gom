package build_test

// P2-2c regressions — initialization cycles through aggregate
// values (docs/decisions/2026-11-05-aggregate-function-bindings.md):
// a function value a top-level destructuring binds becomes a named
// wrapper that calls the referenced function directly, so the legal
// delayed-reference cycle compiles and runs; the aggregate's
// initializer keeps its eager evaluation; and a variable cycle that
// no wrapper can cut is reported as E0907 against the .gom
// bindings — not left for `go build` to name `tmp4`.

import (
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
)

// The motivating reproduction: `count` refers to `again` before the
// tuple initializer runs, and the wrapper keeps the variable out of
// the dependency graph entirely.
func TestAggregateAliasRuns(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 0.33)
	}

	src := `package main
import go "fmt"

fn count (n: Int) -> Int = if n == 0 { 0 } else { again (n - 1) + 1 }
let (again, _) = (count, 0)

type Pair = {f: Int -> Int, x: Int}
let {f = ragain, x = rx} = {f = count, x = 7}

type Box = | Box (Int -> Int)
let Box g = Box count

fn apply (f: Int -> Int) (x: Int) -> Int = f x
fn answer () -> Int = apply g 5
fn main () -> Unit = {
  ignore (fmt.Println (count 3 + ragain rx + g 2 + answer ()))
  ()
}
`
	r, err := build.Compile(build.Input{Path: "initcycle.gom", Src: []byte(src)}, suiteOptions())
	if err != nil {
		t.Fatal(err)
	}
	if r.Output == nil {
		t.Fatalf("compile diagnostics: %+v", r.Diagnostics)
	}
	gobin, err := lookupGo(t)
	if err != nil {
		t.Skip(err.Error())
	}
	out, err := runCompiledCmd(t, gobin, r.Output.Source)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	// count 3 + count 7 + count 2 + apply g 5 = 3 + 7 + 2 + 5
	if out != "17\n" {
		t.Fatalf("output = %q, want %q", out, "17\n")
	}
}

// The aggregate initializer still evaluates eagerly — and in
// dependency order — even though the function binding it carries
// became a wrapper: `mark` prints during package initialization,
// before main runs.
func TestAggregateAliasEagerInit(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 0.35)
	}

	src := `package main
import go "fmt"

fn count (n: Int) -> Int = if n == 0 { 0 } else { again (n - 1) + 1 }
fn mark (x: Int) -> Int = { ignore (fmt.Println "init"); x }
let (again, x) = (count, mark 0)

fn main () -> Unit = {
  ignore (fmt.Println "main")
  ignore (fmt.Println (again 3 + x))
  ()
}
`
	r, err := build.Compile(build.Input{Path: "initcycle_eager.gom", Src: []byte(src)}, suiteOptions())
	if err != nil {
		t.Fatal(err)
	}
	if r.Output == nil {
		t.Fatalf("compile diagnostics: %+v", r.Diagnostics)
	}
	gobin, err := lookupGo(t)
	if err != nil {
		t.Skip(err.Error())
	}
	out, err := runCompiledCmd(t, gobin, r.Output.Source)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	const want = "init\nmain\n3\n"
	if out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
}

// `let g = { …; f }`: the trace peels the block to reach `f`, but
// the block's statements still carry effects — the initializer is
// emitted eagerly under a hidden variable (the same role the
// destructure's shared temporary plays) while `g` is the wrapper.
// Regression: an earlier cut emitted the wrapper alone, and `mark`'s
// print during package initialization silently disappeared.
func TestAggregateAliasVarBlockRuns(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 0.37)
	}

	src := `package main
import go "fmt"

fn count (n: Int) -> Int = if n == 0 { 0 } else { g (n - 1) + 1 }
fn mark (x: Int) -> Int = { ignore (fmt.Println "init"); x }
let g = { ignore (mark 0); count }

fn main () -> Unit = { ignore (fmt.Println (g 3)); () }
`
	r, err := build.Compile(build.Input{Path: "initcycle_varblock.gom", Src: []byte(src)}, suiteOptions())
	if err != nil {
		t.Fatal(err)
	}
	if r.Output == nil {
		t.Fatalf("compile diagnostics: %+v", r.Diagnostics)
	}
	gobin, err := lookupGo(t)
	if err != nil {
		t.Skip(err.Error())
	}
	out, err := runCompiledCmd(t, gobin, r.Output.Source)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	const want = "init\n3\n"
	if out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
}

// The rejected shapes: each still declares a variable cycle the
// wrapper rule cannot remove, so lowering reports E0907 naming the
// .gom bindings and produces no output for go build to choke on.
func TestInitCycleRejected(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"non_function_binding", `package ex
fn g (n: Int) -> Int = x + n
let (f, x) = (g, 41)
`},
		{"computed_element", `package ex
fn mk () -> (Int -> Int) = f
fn count (n: Int) -> Int = if n == 0 { 0 } else { f (n - 1) + 1 }
let (f, _) = (mk (), 0)
`},
		{"var_rhs", `package ex
let pair = (count, 0)
fn count (n: Int) -> Int = if n == 0 { 0 } else { a (n - 1) + 1 }
let (a, b) = pair
`},
		{"self_call", `package ex
fn f (n: Int) -> Int = if n == 0 { r } else { n }
let r = f 0
`},
		{"var_block_untraceable", `package ex
fn mk () -> (Int -> Int) = g
let h = mk ()
let g = { h }

fn main () -> Int = g 1
`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := build.Compile(build.Input{Path: c.name + ".gom", Src: []byte(c.src)}, suiteOptions())
			if err != nil {
				t.Fatal(err)
			}
			var saw bool
			for _, d := range r.Diagnostics {
				if d.Code == "E0907" {
					saw = true
					if strings.Contains(d.Message, "tmp") {
						t.Errorf("diagnostic leaked a generated name: %s", d.Message)
					}
				}
			}
			if !saw {
				t.Fatalf("no E0907; diagnostics = %+v", r.Diagnostics)
			}
			if r.Output != nil {
				t.Errorf("output generated despite the cycle")
			}
		})
	}
}
