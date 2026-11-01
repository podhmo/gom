package pattern

import (
	"strings"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
)

// missingArmsFix builds the "add the missing arms" fix of spec
// section 12: one placeholder-applicability edit inserting
// `p -> todo ()` lines just before the match's closing brace. It
// returns nil when the match span cannot host the edit.
//
// The inserted text follows the spec's own example (spec section
// 12): when the resolver supplies the source, the new arms take the
// closing brace's indentation plus two spaces, and the first arm
// completes the whitespace already on the brace's line. With `}`
// indented two columns the result reads:
//
//	  Neg (Num _) -> todo ()
//	  Neg (Add _ _) -> todo ()
//	}
//
// (the 2-space prefix of the `}` line plus the inserted 2 spaces
// form the first arm's 4-space indent). Without a resolver the arms
// are inserted on fresh lines at a fixed 2-space indent.
func (c *checker) missingArmsFix(m *check.Match, pats []string) *diag.Fix {
	sp := m.Span()
	if !sp.Valid() || len(pats) == 0 {
		return nil
	}
	li := c.lineIndex(sp.File)
	off := braceOffset(sp, li)
	text := insertionText(li, sp, off, pats)
	return &diag.Fix{
		Title:         "add the missing arms",
		Applicability: diag.ApplicabilityPlaceholder,
		Edits: []diag.Edit{
			{Span: diag.Point(sp.File, off), Text: text},
		},
	}
}

// lineIndex resolves the LineIndex of file through the checker's
// resolver, or nil when unavailable.
func (c *checker) lineIndex(file string) *diag.LineIndex {
	if c.res == nil {
		return nil
	}
	li, err := c.res.LineIndex(file)
	if err != nil {
		return nil
	}
	return li
}

// braceOffset locates the match's closing `}` byte — normally
// sp.End-1. With a LineIndex the byte is verified and a
// whitespace-only tail is skipped back over; without one sp.End-1 is
// used unconditionally.
func braceOffset(sp diag.Span, li *diag.LineIndex) int {
	off := sp.End - 1
	if li == nil || off <= sp.Start {
		return off
	}
	pos := li.Pos(off)
	start, _ := li.LineBounds(pos.Line)
	line := li.LineText(pos.Line)
	i := off - start
	for i > 0 && i <= len(line) {
		switch line[i-1] {
		case '}':
			return start + i - 1
		case ' ', '\t':
			i--
		default:
			return sp.End - 1
		}
	}
	return sp.End - 1
}

// insertionText composes the edit text for the missing-arms fix.
func insertionText(li *diag.LineIndex, sp diag.Span, off int, pats []string) string {
	arms := make([]string, len(pats))
	for i, p := range pats {
		arms[i] = p + " -> todo ()"
	}
	if li == nil {
		// No source access: put every arm on its own line at a
		// fixed indent and leave the `}` at the start of the
		// following line.
		return "\n  " + strings.Join(arms, "\n  ") + "\n"
	}
	pos := li.Pos(off)
	braceIndent := pos.Col - 1
	armIndent := braceIndent + 2
	lineStart, _ := li.LineBounds(pos.Line)
	line := li.LineText(pos.Line)
	k := off - lineStart
	if k < 0 || k > len(line) {
		k = len(line)
	}
	prefix := line[:k]
	if allWhitespace(prefix) && len(prefix) <= armIndent {
		// The `}` line is whitespace-only: the first arm fills the
		// remaining indent on that line, following arms get the
		// full indent, and the trailing text restores the `}`'s
		// own indent (the shape of spec section 12's example).
		var sb strings.Builder
		sb.WriteString(strings.Repeat(" ", armIndent-len(prefix)))
		sb.WriteString(arms[0])
		for _, a := range arms[1:] {
			sb.WriteByte('\n')
			sb.WriteString(strings.Repeat(" ", armIndent))
			sb.WriteString(a)
		}
		sb.WriteByte('\n')
		sb.WriteString(strings.Repeat(" ", braceIndent))
		return sb.String()
	}
	// Anything else on the `}` line (a one-line match, a trailing
	// comment): put the arms on fresh lines.
	var sb strings.Builder
	for _, a := range arms {
		sb.WriteByte('\n')
		sb.WriteString(strings.Repeat(" ", armIndent))
		sb.WriteString(a)
	}
	sb.WriteByte('\n')
	sb.WriteString(strings.Repeat(" ", braceIndent))
	return sb.String()
}

// allWhitespace reports whether b is all spaces and tabs.
func allWhitespace(b []byte) bool {
	for _, c := range b {
		if c != ' ' && c != '\t' {
			return false
		}
	}
	return true
}
