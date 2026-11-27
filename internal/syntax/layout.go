package syntax

import "sort"

// Layout maps byte offsets in one source file to source lines and
// byte columns. It is parser-provided trivia for layout-sensitive
// diagnostics; positions in the AST itself remain byte offsets.
type Layout struct {
	starts []int
	len    int
}

func newLayout(src []byte) *Layout {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return &Layout{starts: starts, len: len(src)}
}

// LineCol returns the 1-based line and byte column of offset. The
// column is a byte count, not the display column of diagnostics; it is
// suitable for comparing indentation depth because leading whitespace
// is ASCII in source layout.
func (l *Layout) LineCol(offset int) (line, col int) {
	if l == nil {
		return 0, 0
	}
	offset = max(0, min(offset, l.len))
	i := sort.Search(len(l.starts), func(i int) bool {
		return l.starts[i] > offset
	}) - 1
	return i + 1, offset - l.starts[i] + 1
}
