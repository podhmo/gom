package diag

import (
	"reflect"
	"testing"
)

// The shape `go run` prints for a panic in generated code — the
// //line-adjusted frames name the .gom file, runtime frames name real
// Go files.
const sampleTrace = `panic: gom: todo

goroutine 1 [running]:
github.com/podhmo/gom/rt.Todo[...](...)
	/work/gom/rt/rt.go:25 +0x30
main.main()
	/tmp/x/prog.gom:6 +0x40
exit status 2
`

func TestParseStack(t *testing.T) {
	frames := ParseStack(sampleTrace)
	want := []StackFrame{
		{Func: "github.com/podhmo/gom/rt.Todo[...](...)", File: "/work/gom/rt/rt.go", Line: 25},
		{Func: "main.main()", File: "/tmp/x/prog.gom", Line: 6},
	}
	if !reflect.DeepEqual(frames, want) {
		t.Errorf("ParseStack = %+v, want %+v", frames, want)
	}
}

// A goroutine-dump shape with `created by` frames and noise around —
// go test output interleaves framing and the goroutine list of a
// timed-out test.
func TestParseStackGoroutineDump(t *testing.T) {
	trace := `goroutine 6 [chan receive]:
m.work(0x1)
	/tmp/x/m.gom:12 +0x10
created by m.main in goroutine 1
	/tmp/x/m.gom:30 +0x20

goroutine 1 [running]:
	m.gom:9
`
	want := []StackFrame{
		{Func: "m.work(0x1)", File: "/tmp/x/m.gom", Line: 12},
		{Func: "created by m.main in goroutine 1", File: "/tmp/x/m.gom", Line: 30},
		{Func: "goroutine 1 [running]:", File: "m.gom", Line: 9},
	}
	// The third frame's "call" is the goroutine header line — a
	// hand-shaped trace can put a position there; it parses
	// verbatim for the caller to filter.
	got := ParseStack(trace)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseStack = %+v, want %+v", got, want)
	}
}

func TestParseStackNoise(t *testing.T) {
	for _, text := range []string{
		"",
		"panic: x\n",
		"just text\nwith\ttabs\n",
		"\tno-colon-here\n",
		"main.f()\n\tnot-a-position\n",
	} {
		if frames := ParseStack(text); len(frames) != 0 {
			t.Errorf("ParseStack(%q) = %v, want no frames", text, frames)
		}
	}
}

// A file operand with a trailing "+0x" and one with a colon in the
// path both resolve — the last numeric field wins.
func TestParseStackPos(t *testing.T) {
	for _, tt := range []struct {
		in       string
		wantFile string
		wantLine int
		wantOK   bool
	}{
		{"/a/b.gom:7 +0x20", "/a/b.gom", 7, true},
		{"rel/x.gom:12", "rel/x.gom", 12, true},
		{`C:\w\x.gom:3 +0x1`, `C:\w\x.gom`, 3, true},
		// Spaces in the file operand: only the " +0x" trailer is
		// stripped — a path with spaces keeps its frame.
		{"/my dir/x.gom:4 +0x20", "/my dir/x.gom", 4, true},
		{"/my dir/x.gom:4", "/my dir/x.gom", 4, true},
		{"a +0x1.gom:5 +0x20", "a +0x1.gom", 5, true}, // +0x inside the name survives
		{"x.gom:0", "x.gom", 0, true},
		{"x.gom:abc", "", 0, false},
		{"x.gom", "", 0, false},
		{":5", "", 5, true}, // empty file still parses — resolver decides
	} {
		file, line, ok := ParseStackPos(tt.in)
		if ok != tt.wantOK || file != tt.wantFile || line != tt.wantLine {
			t.Errorf("ParseStackPos(%q) = (%q, %d, %v), want (%q, %d, %v)",
				tt.in, file, line, ok, tt.wantFile, tt.wantLine, tt.wantOK)
		}
	}
}
