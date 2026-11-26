package build_test

import (
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/eval"
)

const shortCircuitSrc = `package main

import go "fmt"

fn boom () -> Bool = todo ()
fn no () -> Bool = false
fn yes () -> Bool = true

fn answer () -> String = {
  let a = no () && boom ()
  let b = yes () || boom ()
  let c = yes () && true
  let d = no () || false
  if a { "bad-a" } else {
    if b {
      if c {
        if d { "bad-d" } else { "ok" }
      } else { "bad-c" }
    } else { "bad-b" }
  }
}

fn main () -> Unit = {
  let _ = fmt.Println (answer ())
  ()
}
`

func TestBooleanShortCircuitEvalCompiled(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 0.35)
	}

	t.Parallel()

	r, err := build.Compile(build.Input{Path: "shortcircuit.gom", Src: []byte(shortCircuitSrc)}, suiteOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range r.Diagnostics {
		if d.Severity == diag.SeverityError {
			t.Fatalf("compile: %s: %s", d.Code, d.Message)
		}
	}

	ev := eval.New(r.Typed)
	fn := ev.Global("answer")
	if fn == nil {
		t.Fatal("no top-level `answer` binding for the evaluator")
	}
	res := ev.Apply(fn, &eval.VUnit{})
	if !res.OK() {
		t.Fatalf("eval answer failed: %+v", res.Diags)
	}
	if res.Value.String() != `"ok"` {
		t.Fatalf("eval answer = %v, want \"ok\"", res.Value)
	}

	out := runCompiled(t, r.Output.Source)
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("compiled output = %q, want ok", out)
	}
}
