package diag

// Pos is a position inside one source file resolved to the coordinate
// system of the message format: a 1-based line, a 1-based column, and
// the 0-based UTF-8 byte offset (docs/spec.md section 12).
//
// Offset is the source of truth. The unit of Col depends on the
// conversion that produced it: LineIndex.Pos counts Unicode code
// points — the column unit of the JSON and text formats — while
// LineIndex.PosUTF16 counts UTF-16 code units for coordinate systems
// such as LSP.
//
// The JSON tags mirror the position objects of the message format so
// that renderings can marshal a Pos directly.
type Pos struct {
	Line   int `json:"line"`
	Col    int `json:"col"`
	Offset int `json:"offset"`
}

// Span identifies a byte range in one source file. Start and End are
// 0-based UTF-8 byte offsets, the source of truth for positions
// (docs/spec.md section 12). The range is half-open: Start is inclusive
// and End exclusive, so a span with Start == End denotes a point, e.g.
// an insertion position.
//
// A diagnostic with no source location uses the zero Span; see IsZero.
type Span struct {
	File  string
	Start int
	End   int
}

// SpanIn returns the span covering the half-open byte range
// [start, end) in file.
func SpanIn(file string, start, end int) Span {
	return Span{File: file, Start: start, End: end}
}

// Point returns the zero-width span at byte offset off in file.
func Point(file string, off int) Span {
	return Span{File: file, Start: off, End: off}
}

// IsZero reports whether s is the zero Span, used for diagnostics that
// carry no source location.
func (s Span) IsZero() bool {
	return s.File == "" && s.Start == 0 && s.End == 0
}

// Valid reports whether s denotes a usable location: a named file and
// 0 <= Start <= End. The zero Span is not valid; callers that accept a
// missing location must test IsZero first.
func (s Span) Valid() bool {
	return s.File != "" && 0 <= s.Start && s.Start <= s.End
}

// Len returns the length of the span in bytes.
func (s Span) Len() int {
	return s.End - s.Start
}

// Contains reports whether byte offset off lies in [s.Start, s.End).
// A zero-width span contains nothing.
func (s Span) Contains(off int) bool {
	return s.Start <= off && off < s.End
}

// SpanPos is a Span with both ends resolved to line and column — the
// shape the message format serializes as "span" (docs/spec.md
// section 12). Build it with LineIndex.Resolve or
// LineIndex.ResolveUTF16.
type SpanPos struct {
	File  string `json:"file"`
	Start Pos    `json:"start"`
	End   Pos    `json:"end"`
}
