package diag

import "testing"

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
