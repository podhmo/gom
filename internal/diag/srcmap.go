package diag

import "sort"

// Anchor binds one line of a generated Go file to the span of the
// .gom source the code emitted there came from. The anchor list of a
// generated file is the compiler-side half of the `//line`
// directives the file carries (docs/design.md section 2.2): the
// directives transmit the same correspondence to the Go toolchain at
// line granularity, while the anchors keep the full byte-offset span.
type Anchor struct {
	// Line is the 1-based line in the generated file the anchor
	// pins — the line right after the `//line` directive, where
	// the construct's first tokens are emitted.
	Line int
	// Span is the .gom span of the node emitted at Line.
	Span Span
}

// SourceMap is the generated-Go → .gom position table of one
// generated file (docs/design.md section 2.2). codegen builds it
// while splicing the `//line` sentinels: every directive becomes one
// anchor, so the map and the directives can never disagree about
// which lines a file claims.
//
// Lookups are by generated line (SpanAt) or by .gom byte offset
// (LineAt). Both directions treat the table as advisory rather than
// total: generated lines before the first anchor (the file header
// and the import block) and .gom offsets no emitted construct covers
// have no answer.
type SourceMap struct {
	// Anchors lists the file's anchors in ascending Line order —
	// strictly increasing, because anchors are appended as each
	// `//line` directive is spliced into the output.
	Anchors []Anchor
}

// SpanAt returns the .gom span governing generated 1-based line:
// the nearest anchor at or before it. That mirrors how the Go
// toolchain resolves positions under `//line` — every line belongs
// to the directive that most recently preceded it — with one
// difference: where Go continues the directive's line numbering into
// the following lines (possibly past the construct's real extent),
// SpanAt reports the whole span of the enclosing construct instead
// of a drifted line number. Lines before the first anchor report
// false.
func (m *SourceMap) SpanAt(line int) (Span, bool) {
	if m == nil {
		return Span{}, false
	}
	i := sort.Search(len(m.Anchors), func(i int) bool {
		return m.Anchors[i].Line > line
	}) - 1
	if i < 0 {
		return Span{}, false
	}
	return m.Anchors[i].Span, true
}

// LineAt returns the generated 1-based line of the most specific
// anchor covering .gom byte offset off — the smallest span
// containing it, which is the innermost construct emitted at that
// position. Anchors for nested constructs have nested spans, so the
// smallest containing span is the statement or expression the offset
// actually belongs to. Several anchors may share one span (an escape
// re-anchors its own arm with the same span); the earliest generated
// line wins. An offset inside no anchor's span reports false.
//
// LineAt is a linear scan — position lookups happen on diagnostics
// and editor queries, not in hot paths.
func (m *SourceMap) LineAt(off int) (int, bool) {
	if m == nil {
		return 0, false
	}
	best := -1
	for i := range m.Anchors {
		a := &m.Anchors[i]
		if !a.Span.Contains(off) {
			continue
		}
		if best < 0 || a.Span.Len() < m.Anchors[best].Span.Len() {
			best = i
		}
	}
	if best < 0 {
		return 0, false
	}
	return m.Anchors[best].Line, true
}
