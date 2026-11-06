package diag

import (
	"strings"
	"testing"
)

func TestSourceMapSpanAt(t *testing.T) {
	m := &SourceMap{Anchors: []Anchor{
		{Line: 5, Span: SpanIn("a.gom", 10, 20)},
		{Line: 9, Span: SpanIn("a.gom", 30, 40)},
		{Line: 10, Span: SpanIn("a.gom", 50, 60)}, // back-to-back directives: line 9 is both pinned and a directive line
		{Line: 20, Span: SpanIn("a.gom", 70, 80)},
	}}
	for _, tt := range []struct {
		line   int
		want   Span
		wantOK bool
	}{
		{1, Span{}, false}, // header/imports: before every anchor
		{4, Span{}, false}, // still before the first anchor
		{5, SpanIn("a.gom", 10, 20), true},
		{8, SpanIn("a.gom", 10, 20), true}, // governed by the line-5 anchor
		{9, SpanIn("a.gom", 30, 40), true}, // the line-9 anchor owns its pinned line
		{10, SpanIn("a.gom", 50, 60), true},
		{19, SpanIn("a.gom", 50, 60), true},
		{20, SpanIn("a.gom", 70, 80), true},
		{100, SpanIn("a.gom", 70, 80), true},
	} {
		sp, ok := m.SpanAt(tt.line)
		if ok != tt.wantOK || sp != tt.want {
			t.Errorf("SpanAt(%d) = (%v, %v), want (%v, %v)", tt.line, sp, ok, tt.want, tt.wantOK)
		}
	}

	var nilMap *SourceMap
	if _, ok := nilMap.SpanAt(1); ok {
		t.Error("nil SourceMap reported a span")
	}
}

func TestSourceMapLineAt(t *testing.T) {
	// Anchors nest: the `if` header's span contains the arms' spans.
	// The smallest containing span is the innermost construct.
	m := &SourceMap{Anchors: []Anchor{
		{Line: 5, Span: SpanIn("a.gom", 10, 90)},  // `match`/if whole span
		{Line: 7, Span: SpanIn("a.gom", 20, 40)},  // first arm
		{Line: 11, Span: SpanIn("a.gom", 45, 60)}, // second arm
		{Line: 13, Span: SpanIn("a.gom", 20, 40)}, // re-anchored, same span as line 7
	}}
	for _, tt := range []struct {
		off    int
		want   int
		wantOK bool
	}{
		{0, 0, false},
		{10, 5, true},  // only the outer span covers the construct's head
		{19, 5, true},  // between the arms, inside the outer span only
		{20, 7, true},  // inside the first arm → earliest generated line wins
		{45, 11, true}, // inside the second arm
		{95, 0, false},
	} {
		line, ok := m.LineAt(tt.off)
		if ok != tt.wantOK || line != tt.want {
			t.Errorf("LineAt(%d) = (%d, %v), want (%d, %v)", tt.off, line, ok, tt.want, tt.wantOK)
		}
	}

	var nilMap *SourceMap
	if _, ok := nilMap.LineAt(0); ok {
		t.Error("nil SourceMap reported a line")
	}
}

// driftFixture mirrors the anchor shape codegen actually produces
// for a file whose constructs expand to more generated lines than
// source lines: three statements share the `m.gom:3` directive
// (the function header, one statement, and the return), then `fn
// use` emits one anchored line per statement while its closing `}`
// drifts unanchored. Line numbers match the generated output probed
// in docs/sketch (the `fn pair`/`fn use` shape).
func driftFixture() (*SourceMap, *LineIndex) {
	src := []byte(strings.Join([]string{
		"package p",            // 1
		"",                     // 2
		"fn pair x = (x, x+1)", // 3
		"",                     // 4
		"fn use () = {",        // 5
		"  let t = pair 5",     // 6
		"  let z = t",          // 7
		"  z",                  // 8
		"}",                    // 9
	}, "\n"))
	li := NewLineIndex(src)
	at := func(line int) Span {
		s, e := li.LineBounds(line)
		return SpanIn("m.gom", s, e)
	}
	m := &SourceMap{Anchors: []Anchor{
		{Line: 10, Span: at(3)}, // func pair header
		{Line: 12, Span: at(3)}, // tmp1
		{Line: 14, Span: at(3)}, // return
		{Line: 18, Span: at(5)}, // func use header — the broad span
		{Line: 20, Span: at(6)}, // tmp2
		{Line: 22, Span: at(6)}, // a := tmp2.V0 — sibling on line 6
		{Line: 24, Span: at(6)}, // b := tmp2.V1 — sibling on line 6
		{Line: 26, Span: at(7)}, // z
		{Line: 28, Span: at(8)}, // return z
	}}
	return m, li
}

func TestSourceMapReportedLine(t *testing.T) {
	m, li := driftFixture()
	for _, tt := range []struct {
		gen    int
		want   int
		wantOK bool
	}{
		{1, 0, false},   // before the first anchor
		{9, 0, false},   // the directive line itself is not governed
		{10, 3, true},   // the pinned line reports its pin
		{11, 4, true},   // a directive line mid-region drifts: 3+1
		{15, 4, true},   // the `}` past the pin-3 anchors: 3+1
		{17, 6, true},   // drift into the next pin's line: 3+3
		{18, 5, true},   // re-anchored — back on station
		{19, 6, true},   // one past pin 5
		{29, 9, true},   // `fn use`'s closing `}` under pin 8
		{100, 80, true}, // the last region is unbounded
	} {
		got, ok := m.ReportedLine(tt.gen, li)
		if ok != tt.wantOK || got != tt.want {
			t.Errorf("ReportedLine(%d) = (%d, %v), want (%d, %v)", tt.gen, got, ok, tt.want, tt.wantOK)
		}
	}

	var nilMap *SourceMap
	if _, ok := nilMap.ReportedLine(10, li); ok {
		t.Error("nil SourceMap reported a line")
	}
}

func TestSourceMapCandidates(t *testing.T) {
	m, li := driftFixture()
	want := map[int][]AnchorMatch{
		// Every pin-3 anchor can print 3 — the reported line of a
		// same-line construct is claimed by all its siblings.
		3: {{Line: 10}, {Line: 12}, {Line: 14}},
		// 4 has no construct — only drift reaches it: the pin-3
		// regions' second lines (directive lines and the `}`).
		4: {{Line: 11, Drifted: true}, {Line: 13, Drifted: true}, {Line: 15, Drifted: true}},
		// 5: the broad `fn use` header's pinned line, plus drift from
		// pin 3 (gen 16 reports 3+2) — the drift and the real line
		// collide.
		5: {{Line: 16, Drifted: true}, {Line: 18}},
		// 6: drift from pin 3 (gen 17) and pin 5 (gen 19) plus the
		// three line-6 siblings.
		6: {{Line: 17, Drifted: true}, {Line: 19, Drifted: true}, {Line: 20}, {Line: 22}, {Line: 24}},
		// 7: the pin-6 regions are two lines each — their second
		// lines drift onto 7 — plus the pinned line-7 statement.
		7: {{Line: 21, Drifted: true}, {Line: 23, Drifted: true}, {Line: 25, Drifted: true}, {Line: 26}},
		// 8: drift from pin 7 (gen 27) plus the pinned `return z`.
		8: {{Line: 27, Drifted: true}, {Line: 28}},
		// 9: only `fn use`'s closing `}` — past its span's extent.
		9: {{Line: 29, Drifted: true}},
	}
	for reported, ms := range want {
		got := m.Candidates(reported, li)
		if len(got) != len(ms) {
			t.Errorf("Candidates(%d) = %v, want %d matches", reported, got, len(ms))
			continue
		}
		for i, am := range got {
			if am.Line != ms[i].Line || am.Drifted != ms[i].Drifted {
				t.Errorf("Candidates(%d)[%d] = {Line:%d Drifted:%v}, want {Line:%d Drifted:%v}",
					reported, i, am.Line, am.Drifted, ms[i].Line, ms[i].Drifted)
			}
			if want := governingAnchor(t, m, am.Line); am.Anchor != want {
				t.Errorf("Candidates(%d)[%d] anchor = %+v, want the anchor governing gen %d: %+v",
					reported, i, am.Anchor, am.Line, want)
			}
		}
	}
	if got := m.Candidates(2, li); len(got) != 0 {
		t.Errorf("Candidates(2) = %v, want none — below every pin", got)
	}

	var nilMap *SourceMap
	if got := nilMap.Candidates(3, li); got != nil {
		t.Error("nil SourceMap reported candidates")
	}
}

// governingAnchor returns the anchor whose region covers the
// generated line gen — the last anchor at or before it.
func governingAnchor(t *testing.T, m *SourceMap, gen int) Anchor {
	t.Helper()
	for i := len(m.Anchors) - 1; i >= 0; i-- {
		if m.Anchors[i].Line <= gen {
			return m.Anchors[i]
		}
	}
	t.Fatalf("no anchor governs generated line %d", gen)
	return Anchor{}
}
