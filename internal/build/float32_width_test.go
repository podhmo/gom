package build_test

import (
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/eval"
)

// Negative control: without the width oracle the float32 cases
// diverge — proving the cases exercise the width machinery.
func TestFloat32WidthNegativeControl(t *testing.T) {
	for _, c := range compareCases {
		if !strings.HasPrefix(c.name, "float32_width") {
			continue
		}
		if c.name == "float32_width_interp_precision" {
			// The interpolation case pins the FORMATTING side:
			// oracle-less the value never rounds, so its shortest
			// float64 spelling coincides with the float32 one —
			// the positive compiled comparison is the proof there.
			continue
		}
		r, err := build.Compile(
			build.Input{Path: c.name + ".gom", Src: []byte(c.src)},
			suiteOptions(),
		)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if r.Diagnostics.HasError() {
			t.Fatalf("diags: %v", r.Diagnostics)
		}
		ev := eval.New(r.Typed) // no oracle — width 64 everywhere
		ev.SetLineIndex(diag.NewLineIndex([]byte(c.src)))
		bindGoStubs(t, ev, &diffCapture{out: &strings.Builder{}}, r.Typed)
		fn := ev.Global("answer")
		res := ev.Apply(fn, &eval.VUnit{})
		if !res.OK() {
			t.Fatalf("%s: eval diags %v", c.name, res.Diags)
		}
		s, ok := res.Value.(*eval.VStr)
		if !ok {
			t.Fatalf("%s: answer not a string: %v", c.name, res.Value)
		}
		t.Logf("%s: oracle-less eval answer = %q (width-aware want %q)", c.name, s.S, c.want)
		if s.S == c.want {
			t.Fatalf("%s: oracle-less eval agrees — the case does not exercise width", c.name)
		}
	}
}

// TestFloatWidthTagInvariant pins the width-tag invariant the
// boxed-any comparison depends on: only width-32 floats carry a
// tag. A width-64 literal leaves W zero exactly like an arithmetic
// result, so the structural DeepEqual of boxed aggregates cannot
// split values whose float64 bits are equal (review round —
// literals tagged W=64 compared false against untagged arithmetic).
func TestFloatWidthTagInvariant(t *testing.T) {
	src := `package main

import go "github.com/podhmo/gom/testdata/goiface"

fn plain () -> Float = 1.0
fn narrow () -> goiface.SmallRatio = 1.0
fn answer () -> String = ""
fn main () -> Unit = ()
`
	r, err := build.Compile(
		build.Input{Path: "widthtag.gom", Src: []byte(src)},
		suiteOptions(),
	)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if r.Diagnostics.HasError() {
		t.Fatalf("diags: %v", r.Diagnostics)
	}
	ev := eval.New(r.Typed)
	ev.SetLineIndex(diag.NewLineIndex([]byte(src)))
	ev.SetFloatWidthOracle(floatWidthOracle())
	for name, wantW := range map[string]int{"plain": 0, "narrow": 32} {
		res := ev.Apply(ev.Global(name), &eval.VUnit{})
		if !res.OK() {
			t.Fatalf("%s: eval diags %v", name, res.Diags)
		}
		f, ok := res.Value.(*eval.VFloat)
		if !ok {
			t.Fatalf("%s is %T", name, res.Value)
		}
		if f.W != wantW {
			t.Fatalf("%s: W=%d, want %d", name, f.W, wantW)
		}
	}
}
