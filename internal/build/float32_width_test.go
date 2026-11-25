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
