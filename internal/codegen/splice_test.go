package codegen

// spliceLines unit tests: the sentinel rewriter must (a) pair each
// rewritten //line with its mark as an anchor, and (b) pass
// malformed sentinels through verbatim — never index marks out of
// range, never panic on hand-shaped input.

import (
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

func TestSpliceLinesWellFormed(t *testing.T) {
	marks := []diag.Span{
		diag.SpanIn("x.gom", 10, 20),
		diag.SpanIn("x.gom", 30, 40),
	}
	src := "package m\n" +
		"//gomsrc:0:3:x.gom\n" +
		"func f() {\n" +
		"\t//gomsrc:1:7:x.gom\n" +
		"\treturn\n" +
		"}\n"
	out, sm := spliceLines([]byte(src), marks)
	want := "package m\n" +
		"//line x.gom:3\n" +
		"func f() {\n" +
		"//line x.gom:7\n" + // directives are rewritten at column 0
		"\treturn\n" +
		"}\n"
	if string(out) != want {
		t.Errorf("spliced output =\n%s\nwant:\n%s", out, want)
	}
	if len(sm.Anchors) != 2 {
		t.Fatalf("anchors = %d, want 2", len(sm.Anchors))
	}
	if sm.Anchors[0].Line != 3 || sm.Anchors[0].Span != marks[0] {
		t.Errorf("anchor[0] = %+v", sm.Anchors[0])
	}
	if sm.Anchors[1].Line != 5 || sm.Anchors[1].Span != marks[1] {
		t.Errorf("anchor[1] = %+v", sm.Anchors[1])
	}
}

// Back-to-back sentinels: the first directive's pinned line is itself
// a directive — both still anchor correctly and keep ascending.
func TestSpliceLinesBackToBack(t *testing.T) {
	marks := []diag.Span{diag.SpanIn("x.gom", 1, 5), diag.SpanIn("x.gom", 6, 9)}
	src := "//gomsrc:0:1:x.gom\n//gomsrc:1:2:x.gom\ncode()\n"
	out, sm := spliceLines([]byte(src), marks)
	if string(out) != "//line x.gom:1\n//line x.gom:2\ncode()\n" {
		t.Fatalf("output =\n%s", out)
	}
	if len(sm.Anchors) != 2 || sm.Anchors[0].Line != 2 || sm.Anchors[1].Line != 3 {
		t.Fatalf("anchors = %+v", sm.Anchors)
	}
}

// Malformed sentinels are passed through verbatim: no anchor, no
// panic, output keeps the original bytes.
func TestSpliceLinesMalformed(t *testing.T) {
	marks := []diag.Span{diag.SpanIn("x.gom", 1, 5)}
	for _, line := range []string{
		"//gomsrc:-1:1:x.gom",  // negative index — must not index marks
		"//gomsrc:9:1:x.gom",   // index past len(marks)
		"//gomsrc:abc:1:x.gom", // non-numeric index
		"//gomsrc:0:x.gom",     // missing line field
		"//gomsrc:0:3:",        // empty file field
		"//gomsrc:0",           // no fields at all
		"//gomsrc:",            // bare marker
		"//gomsrc:0:3",         // no file field
		"//gomsrc:0x1:3:x.gom", // hex-looking index (Atoi rejects)
		"//gomsrc: 0:3:x.gom",  // space in index field
	} {
		src := "a\n" + line + "\nb\n"
		out, sm := spliceLines([]byte(src), marks)
		if string(out) != src {
			t.Errorf("%q: output changed\n got: %q\nwant: %q", line, out, src)
		}
		if len(sm.Anchors) != 0 {
			t.Errorf("%q: produced %d anchors", line, len(sm.Anchors))
		}
	}
}

// Indentation does not confuse the check: gofmt may re-indent a
// sentinel inside a func body — it is still rewritten.
func TestSpliceLinesIndented(t *testing.T) {
	marks := []diag.Span{diag.SpanIn("x.gom", 1, 5)}
	src := "\t\t//gomsrc:0:9:x.gom\n"
	out, sm := spliceLines([]byte(src), marks)
	if string(out) != "//line x.gom:9\n" {
		t.Errorf("output = %q", out)
	}
	if len(sm.Anchors) != 1 {
		t.Fatalf("anchors = %+v", sm.Anchors)
	}
}

// A non-sentinel line containing the marker mid-text is untouched.
func TestSpliceLinesMarkerInText(t *testing.T) {
	src := "s := \"//gomsrc:0:1:x.gom\"\n"
	out, sm := spliceLines([]byte(src), []diag.Span{diag.SpanIn("x.gom", 1, 5)})
	if string(out) != src {
		t.Errorf("output changed: %q", out)
	}
	if len(sm.Anchors) != 0 {
		t.Errorf("anchors = %+v", sm.Anchors)
	}
}
