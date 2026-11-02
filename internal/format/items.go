package format

import (
	"sort"
	"strings"

	"github.com/podhmo/gom/internal/syntax"
)

// This file renders collections of sibling items — block statements,
// match/select arms, sum variants, extern entries, record fields,
// list and tuple elements, top-level declarations — as a list of
// lines joined by hard newlines. It owns three policies:
//
//   - gap trivia: comments between items print in the gap they fell
//     in — glued after the previous item when they shared its source
//     line ("trailing"), on their own lines otherwise
//   - blank preservation: inside blocks, arms, variants and extern
//     entries a blank run collapses to one empty line; inside lists,
//     tuples, records and the declaration sequence they vanish
//   - trailing-comment alignment: trailing comments on consecutive
//     single-line items pad to a common column — spec 1.7's one
//     alignment rule
//
// Comment partitioning is eager (each comment consumed exactly once);
// alignment is lazy — it needs rendered widths, which depend on the
// column the collection lands on, so the sequence is emitted through
// a lazy node that layout() resolves with that column.

// item is one element of a collection.
type item struct {
	lo, hi int      // source span — gap boundaries
	d      *doc     // content
	lead   []string // extra lines printed above the item (doc comments, @test)
}

// seqSpec describes one collection's separators and trivia policy.
type seqSpec struct {
	sep      string // separator after each item when broken ("", ",", ";")
	flatSep  string // separator between items when flat ("; ", ", ")
	trailSep bool   // separator after the last item too (trailing comma)
	blanks   bool   // preserve blank lines between items (collapse to one)
	always   bool   // never render flat (match, select, extern, sum type)
	lines    int    // newlines separating items: 1 in braces, 2 between decls
}

// gap holds the trivia between two adjacent items (or between a
// delimiter and the first/last item).
type gap struct {
	trail  []*cmt // comments on the previous item's end line
	own    []*cmt // comments on later lines
	blanks []int  // blank line numbers in the gap
}

// events returns the gap's own-line comments and blanks merged in
// line order — each becomes one output line.
func (g gap) events(keepBlanks bool) []gevt {
	ev := make([]gevt, 0, len(g.own)+len(g.blanks))
	for _, c := range g.own {
		ev = append(ev, gevt{line: c.startLine, text: c.render()})
	}
	if keepBlanks {
		for _, l := range g.blanks {
			ev = append(ev, gevt{line: l, blank: true})
		}
	}
	sort.SliceStable(ev, func(i, j int) bool { return ev[i].line < ev[j].line })
	// Collapse consecutive blanks to one.
	out := ev[:0]
	for i, e := range ev {
		if e.blank && i > 0 && out[len(out)-1].blank {
			continue
		}
		out = append(out, e)
	}
	return out
}

// flatOK reports whether the gap's contents can render inside a flat
// collection: no preserved blanks and only single-line /* */ comments.
func (g gap) flatOK(keepBlanks bool) bool {
	if keepBlanks && len(g.blanks) > 0 {
		return false
	}
	for _, c := range g.own {
		if c.c.Kind != syntax.CommentBlock || c.multi() {
			return false
		}
	}
	for _, c := range g.trail {
		if c.c.Kind != syntax.CommentBlock || c.multi() {
			return false
		}
	}
	return true
}

// collectGaps consumes the comments between openOff and closeOff and
// partitions them around items: gap i precedes items[i], the final
// gap follows the last item. prevLine for the first gap is the line
// of openOff, so a comment sharing the opener's line is "trailing".
func (p *printer) collectGaps(openOff, closeOff int, items []item) []gap {
	gaps := make([]gap, len(items)+1)
	prevEnd := openOff
	prevLine := p.lineOf(openOff)
	classify := func(g *gap, c *cmt) {
		if c.startLine == prevLine {
			g.trail = append(g.trail, c)
		} else {
			g.own = append(g.own, c)
		}
	}
	for i, it := range items {
		for _, c := range p.take(prevEnd, it.lo) {
			classify(&gaps[i], c)
		}
		// Leading-gap blanks (delimiter → first item) are not
		// preserved: on reparse they land left of the delimiter,
		// outside this gap's range, so keeping them breaks
		// idempotency ("type A = |\n\nA" → blank → none).
		loLine, hiLine := p.lineOf(prevEnd), p.lineOf(it.lo)
		if i > 0 {
			for l := loLine + 1; l < hiLine; l++ {
				if p.blanks[l] {
					gaps[i].blanks = append(gaps[i].blanks, l)
				}
			}
		}
		prevEnd = it.hi
		prevLine = p.lineOf(max(it.hi-1, it.lo))
	}
	for _, c := range p.take(prevEnd, closeOff) {
		classify(&gaps[len(items)], c)
	}
	loLine, hiLine := p.lineOf(prevEnd), p.lineOf(closeOff)
	for l := loLine + 1; l < hiLine; l++ {
		if p.blanks[l] {
			gaps[len(items)].blanks = append(gaps[len(items)].blanks, l)
		}
	}
	return gaps
}

// trailText renders trailing comments — " // a /* b */" — or "".
func trailText(cs []*cmt) string {
	if len(cs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, c := range cs {
		b.WriteByte(' ')
		b.WriteString(c.render())
	}
	return b.String()
}

// inlineText renders gap comments for a flat rendering, each as a
// trailing "/* ... */ " — // comments cannot appear (flatOK checked).
func inlineText(cs []*cmt) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.renderBlock())
		b.WriteByte(' ')
	}
	return b.String()
}

// seqLines assembles the collection's output lines at column ind —
// one doc per line; the caller joins them with hard newlines and
// supplies the boundary newlines around the whole sequence.
func (p *printer) seqLines(gaps []gap, items []item, s seqSpec, ind int) []*doc {
	var lines []*doc
	emitEvents := func(g gap) {
		for _, e := range g.events(s.blanks) {
			if e.blank {
				lines = append(lines, docNil)
			} else {
				lines = append(lines, text(e.text))
			}
		}
	}
	// Comments sitting on the open-delimiter's own line classify as
	// "trailing" — but there is no previous item to glue them to, so
	// they are emitted as their own line before the first item.
	for _, c := range gaps[0].trail {
		lines = append(lines, text(c.render()))
	}
	emitEvents(gaps[0])

	// Alignment: an item joins a run when it renders single-line,
	// carries a trailing comment, and its following gap has no
	// own-line trivia.
	widths := make([]int, len(items))
	single := make([]bool, len(items))
	for i, it := range items {
		if w, ok := flatWidth(it.d, ind); ok && ind+w <= Width {
			widths[i] = w
			single[i] = true
		}
	}
	align := make([]int, len(items))
	clean := func(i int) bool {
		f := gaps[i+1]
		return len(f.trail) > 0 && single[i] && len(f.own) == 0 &&
			(!s.blanks || len(f.blanks) == 0)
	}
	for i := 0; i < len(items); {
		if !clean(i) {
			i++
			continue
		}
		j, maxW := i, widths[i]
		for j < len(items) && clean(j) {
			if widths[j] > maxW {
				maxW = widths[j]
			}
			j++
		}
		for k := i; k < j; k++ {
			align[k] = maxW - widths[k] + 1
		}
		i = j
	}

	for i, it := range items {
		if i > 0 {
			for n := 0; n < s.lines-1; n++ {
				lines = append(lines, docNil)
			}
			emitEvents(gaps[i])
		}
		for _, l := range it.lead {
			lines = append(lines, text(l))
		}
		d := it.d
		if s.sep != "" && (i < len(items)-1 || s.trailSep) {
			d = concat(d, text(s.sep))
		} else if s.sep == "" && i < len(items)-1 && p.needsSep(it.d, items[i+1].lo, items[i+1].d) {
			// NL-2 emits no newline after a non-canEnd last token
			// (a ".keyword" member name — e.g. "0.type\nf" re-lexes
			// as "0.type f"), so glue the separator explicitly. "|"
			// arms continue without a newline and need none.
			d = concat(d, text(";"))
		}
		if tr := gaps[i+1].trail; len(tr) > 0 {
			var b strings.Builder
			for n := align[i]; n > 0; n-- {
				b.WriteByte(' ')
			}
			if align[i] == 0 {
				b.WriteByte(' ')
			}
			for j, c := range tr {
				if j > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(c.render())
			}
			d = concat(d, text(b.String()))
		}
		lines = append(lines, d)
	}
	// With no items, gaps[0] is both the leading and the trailing gap —
	// it was already emitted above; emitting it again would duplicate
	// every comment.
	if len(items) > 0 {
		emitEvents(gaps[len(items)])
	}
	return lines
}

// joinLines joins line docs with hard newlines — the broken interior
// of a collection.
func joinLines(lines []*doc) *doc {
	return join(docHard, lines...)
}

// braceSeq renders  open + delim + head + items + closeDelim — used
// for blocks, match/select arms, extern entries, records, lists and
// tuples. delimOff is the offset of the opening delimiter, closeOff
// of the closing one; head (nil for most) is text glued on the open
// line after the delimiter (" base with" for record updates).
//
// The flat shape is "{ a; b }" / "[a, b]", reached only when every
// gap is flatOK and the flat width fits at the landing column;
// otherwise the broken shape is
//
//	{ // trailing
//	  item,
//	  // own-line
//	  item,
//	}
//
// with items at the enclosing indent +2.
func (p *printer) braceSeq(open *doc, openCh, closeCh byte, delimOff, closeOff int, head *doc, items []item, s seqSpec) *doc {
	gaps := p.collectGaps(delimOff+1, closeOff, items)

	// Comments sharing the delimiter's line glue after it (and head).
	headTr := gaps[0].trail
	headOK := true
	for _, c := range headTr {
		if c.c.Kind != syntax.CommentBlock || c.multi() {
			headOK = false
		}
	}
	headCmt := text(trailText(headTr))
	gaps[0].trail = nil

	// Flat rendering: items joined by flatSep, each gap's comments
	// inline — trailing glued after the previous item, own-line block
	// comments ahead of the next.
	var flatKids []*doc
	for i, it := range items {
		if i > 0 {
			flatKids = append(flatKids, text(s.flatSep))
		}
		flatKids = append(flatKids, text(inlineText(gaps[i].own)))
		flatKids = append(flatKids, it.d, text(inlineText(gaps[i+1].trail)))
	}
	flatKids = append(flatKids, text(inlineText(gaps[len(items)].own)))
	flat := concat(flatKids...)

	return lazy(func(col, ind int) *doc {
		ow, oOk := flatWidth(open, col)
		hw, hOk := 0, true
		if head != nil {
			hw, hOk = flatWidth(head, col+ow+1)
		}
		fw, fOk := flatWidth(flat, col+ow+1+hw)
		gapsOK := true
		for i := range gaps {
			if !gaps[i].flatOK(s.blanks) {
				gapsOK = false
				break
			}
		}
		inner, innerW := flat, fw
		if openCh == '{' && fw > 0 {
			// "{ a; b }" — braces carry inner padding, ()/[] do not.
			inner = concat(text(" "), flat, text(" "))
			innerW = fw + 2
		}
		if !s.always && headOK && gapsOK && oOk && hOk && fOk && col+ow+1+hw+innerW+1 <= Width {
			return concat(open, text(string(openCh)), head, headCmt, inner, text(string(closeCh)))
		}
		if len(items) == 0 && len(gaps[0].own) == 0 && len(gaps[0].blanks) == 0 && headOK {
			return concat(open, text(string(openCh)), head, headCmt, text(string(closeCh)))
		}
		var kids []*doc
		kids = append(kids, open, text(string(openCh)), head, headCmt)
		body := p.seqLines(gaps, items, s, ind+indentStep)
		kids = append(kids, nest(indentStep, concat(docHard, joinLines(body))),
			docHard, text(string(closeCh)))
		return concat(kids...)
	})
}

// findByte returns the offset of the first b in src[lo:hi], or -1.
func findByte(src []byte, lo, hi int, b byte) int {
	if hi > len(src) {
		hi = len(src)
	}
	for i := lo; i < hi; i++ {
		if src[i] == b {
			return i
		}
	}
	return -1
}

func firstLo(items []item, dflt int) int {
	if len(items) > 0 {
		return items[0].lo
	}
	return dflt
}
