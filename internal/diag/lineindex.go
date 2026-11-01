package diag

import (
	"sort"
	"unicode/utf8"
)

// LineIndex maps UTF-8 byte offsets to line and column positions within
// a single source file. Positions stay anchored to byte offsets inside
// the compiler; this index performs the conversion that output formats
// attach to messages (docs/spec.md section 12, docs/design.md
// section 2.3).
//
// LineIndex does not copy src: the caller must keep the source bytes
// alive and unmodified for the lifetime of the index. It is not safe
// for concurrent mutation of src, but concurrent reads are fine.
type LineIndex struct {
	src    []byte
	starts []int // byte offset of each line's first byte; starts[0] == 0
}

// NewLineIndex builds a LineIndex over src. Lines are delimited by
// '\n'; a trailing '\n' leaves a final empty line, and a '\r' before it
// is an ordinary column character.
func NewLineIndex(src []byte) *LineIndex {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return &LineIndex{src: src, starts: starts}
}

// Pos converts a byte offset to a Pos whose column counts Unicode code
// points — the column unit of the message format (docs/spec.md
// section 12). It runs in time proportional to the line's length.
//
// offset is clamped into [0, len(src)]. Offsets inside a multi-byte
// character produce a best-effort column; valid spans always begin and
// end on rune boundaries.
func (li *LineIndex) Pos(offset int) Pos {
	return li.pos(offset, func(r rune) int { return 1 })
}

// PosUTF16 is Pos with the column counted in UTF-16 code units, the
// coordinate unit used by LSP (docs/design.md section 2.3).
func (li *LineIndex) PosUTF16(offset int) Pos {
	return li.pos(offset, utf16Len)
}

// pos computes line and column for offset, measuring columns with
// colWidth, which returns the width of one rune in the target unit
// (code points or UTF-16 code units).
func (li *LineIndex) pos(offset int, colWidth func(rune) int) Pos {
	offset = max(0, min(offset, len(li.src)))

	// Find the last line start <= offset.
	line := sort.Search(len(li.starts), func(i int) bool {
		return li.starts[i] > offset
	}) - 1

	col := 1
	rest := li.src[li.starts[line]:offset]
	for len(rest) > 0 {
		r, size := utf8.DecodeRune(rest)
		col += colWidth(r)
		rest = rest[size:]
	}
	return Pos{Line: line + 1, Col: col, Offset: offset}
}

// Resolve attaches code-point line and column to both ends of sp,
// producing the resolved span rendered by the message format.
func (li *LineIndex) Resolve(sp Span) SpanPos {
	return SpanPos{File: sp.File, Start: li.Pos(sp.Start), End: li.Pos(sp.End)}
}

// ResolveUTF16 is Resolve with UTF-16 code-unit columns, for outputs
// such as LSP.
func (li *LineIndex) ResolveUTF16(sp Span) SpanPos {
	return SpanPos{File: sp.File, Start: li.PosUTF16(sp.Start), End: li.PosUTF16(sp.End)}
}

// LineCount returns the number of lines in the file. A file whose
// source ends with '\n' counts the empty final line.
func (li *LineIndex) LineCount() int {
	return len(li.starts)
}

// LineBounds returns the byte range [start, end) covering the 1-based
// line, excluding the line terminator. line is clamped into
// [1, LineCount].
func (li *LineIndex) LineBounds(line int) (start, end int) {
	line = max(1, min(line, len(li.starts)))
	start = li.starts[line-1]
	if line < len(li.starts) {
		end = li.starts[line] - 1 // drop the '\n'
	} else {
		end = len(li.src)
	}
	return start, end
}

// LineText returns the contents of the 1-based line without its '\n'
// terminator. A '\r' before the terminator is kept.
func (li *LineIndex) LineText(line int) []byte {
	start, end := li.LineBounds(line)
	return li.src[start:end]
}

// utf16Len returns the number of UTF-16 code units needed to encode r.
func utf16Len(r rune) int {
	if r >= 0x10000 {
		return 2 // surrogate pair
	}
	return 1
}
