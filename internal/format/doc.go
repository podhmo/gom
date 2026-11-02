package format

import (
	"strings"
	"unicode/utf8"
)

// This file holds the document IR and the layout engine of gom fmt —
// a Wadler-style pretty-printer (docs/design.md section 6). A doc is
// built once per node; layout then walks it left to right and, at
// every group, chooses the flat rendering when it fits the remaining
// columns and the broken one otherwise. Construct-by-construct policy
// is in print.go and siblings; the canonical style is recorded in
// docs/decisions/2026-11-02-gom-fmt-layout.md.
//
// The IR:
//
//	text(s)            literal text — never contains '\n'
//	nl                 breakable space: " " flat, newline broken
//	soft               breakable nothing: "" flat, newline broken
//	hard               unconditional newline
//	concat(d...)       sequence
//	nest(i, d)         add i columns to the indent of lines broken in d
//	group(d)           choose flat if d fits, else broken
//	lazy(f)            deferred doc — f runs at layout with the column
//	                   the doc lands on, for choices that need it
//	                   (flat/broken collections, comment alignment)
//
// A group containing a hard newline never picks flat: "fits" fails on
// hard in flat mode, so comments and preserved blank lines force the
// surrounding collection to break.

type dKind int

const (
	dNil dKind = iota
	dText
	dLine // breakable " "
	dSoft // breakable ""
	dHard // unconditional newline
	dConcat
	dNest
	dGroup
	dLazy
)

type doc struct {
	kind dKind
	text string                  // dText
	ind  int                     // dNest: extra indent
	kids []*doc                  // dConcat
	d    *doc                    // dNest, dGroup
	lazy func(col, ind int) *doc // dLazy: landing column, structural indent
	memo map[lazyKey]*doc        // dLazy: evaluated results, filled on first use
}

// lazyKey keys a lazy doc's result cache: the callback is a pure
// function of its landing column and structural indent.
type lazyKey struct {
	col, ind int
}

var (
	docNil  = &doc{kind: dNil}
	docLine = &doc{kind: dLine}
	docHard = &doc{kind: dHard}
)

func text(s string) *doc {
	if s == "" {
		return docNil
	}
	return &doc{kind: dText, text: s}
}

func concat(ds ...*doc) *doc {
	kids := make([]*doc, 0, len(ds))
	for _, d := range ds {
		if d == nil || d.kind == dNil {
			continue
		}
		kids = append(kids, d)
	}
	switch len(kids) {
	case 0:
		return docNil
	case 1:
		return kids[0]
	}
	return &doc{kind: dConcat, kids: kids}
}

// join is concat with sep between every pair of elements.
func join(sep *doc, ds ...*doc) *doc {
	kids := make([]*doc, 0, 2*len(ds))
	for i, d := range ds {
		if i > 0 {
			kids = append(kids, sep)
		}
		kids = append(kids, d)
	}
	return concat(kids...)
}

func nest(i int, d *doc) *doc { return &doc{kind: dNest, ind: i, d: d} }
func group(d *doc) *doc       { return &doc{kind: dGroup, d: d} }

// lazy defers doc construction until layout knows both the column the
// doc lands on (for flat/broken choices) and the structural indent of
// its enclosing nest (for line-oriented content). f must be pure — it
// may run more than once (once while fits probes, once for the final
// rendering) and must return the same doc for the same arguments.
func lazy(f func(col, ind int) *doc) *doc { return &doc{kind: dLazy, lazy: f} }

// eval runs a lazy doc's callback, caching the result per (col, ind).
// Every probing path — fits, flatWidth, docText, firstByte and layout
// itself — goes through here; without the cache each probe re-ran the
// callback, and since a collection's callback probes its own items
// (flatWidth, needsSep→docText), deeply nested collections re-ran the
// inner callbacks once per outer probe — exponential in depth (the
// deep-nesting bug registered in knownGenFormatBugs). The callback's
// purity contract makes the cached result interchangeable with a
// fresh call.
func (d *doc) eval(col, ind int) *doc {
	k := lazyKey{col, ind}
	if r, ok := d.memo[k]; ok {
		return r
	}
	r := d.lazy(col, ind)
	if d.memo == nil {
		d.memo = map[lazyKey]*doc{}
	}
	d.memo[k] = r
	return r
}

// ------------------------------------------------------------------
// layout
//

type mode int

const (
	mFlat mode = iota
	mBreak
)

type frame struct {
	ind  int
	mode mode
	d    *doc
}

// layout renders d with the given line width, starting at column ind.
// Lines broken inside a nest write indent spaces after the newline.
func layout(width, ind int, d *doc) string {
	var b strings.Builder
	col := ind
	stack := []frame{{ind: ind, mode: mBreak, d: d}}
	newline := func(n int) {
		b.WriteByte('\n')
		for i := 0; i < n; i++ {
			b.WriteByte(' ')
		}
		col = n
	}
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch c.d.kind {
		case dNil:
		case dText:
			b.WriteString(c.d.text)
			col += utf8.RuneCountInString(c.d.text)
		case dConcat:
			for i := len(c.d.kids) - 1; i >= 0; i-- {
				stack = append(stack, frame{c.ind, c.mode, c.d.kids[i]})
			}
		case dNest:
			stack = append(stack, frame{c.ind + c.d.ind, c.mode, c.d.d})
		case dGroup:
			m := mBreak
			if fits(width-col, col, []frame{{c.ind, mFlat, c.d.d}}) {
				m = mFlat
			}
			stack = append(stack, frame{c.ind, m, c.d.d})
		case dLine:
			if c.mode == mFlat {
				b.WriteByte(' ')
				col++
			} else {
				newline(c.ind)
			}
		case dSoft:
			if c.mode == mFlat {
				// nothing
			} else {
				newline(c.ind)
			}
		case dLazy:
			stack = append(stack, frame{c.ind, c.mode, c.d.eval(col, c.ind)})
		case dHard:
			newline(c.ind)
		}
	}
	return b.String()
}

// fits reports whether cmds render within w columns up to the first
// line end. Scanning continues into the enclosing context (the cmds
// keep their own modes): a breakable newline in break mode ends the
// line — fit so far — while a hard newline inside flat mode means the
// doc cannot be flat at all. base is the column the scan started at,
// used to give lazy docs their landing column.
func fits(w, base int, cmds []frame) bool {
	w0 := w
	for w >= 0 && len(cmds) > 0 {
		c := cmds[len(cmds)-1]
		cmds = cmds[:len(cmds)-1]
		switch c.d.kind {
		case dNil:
		case dText:
			w -= utf8.RuneCountInString(c.d.text)
		case dConcat:
			for i := len(c.d.kids) - 1; i >= 0; i-- {
				cmds = append(cmds, frame{c.ind, c.mode, c.d.kids[i]})
			}
		case dNest:
			cmds = append(cmds, frame{c.ind + c.d.ind, c.mode, c.d.d})
		case dGroup:
			cmds = append(cmds, frame{c.ind, c.mode, c.d.d})
		case dLine:
			if c.mode == mFlat {
				w--
			} else {
				return true
			}
		case dSoft:
			if c.mode == mFlat {
				// 0 columns
			} else {
				return true
			}
		case dLazy:
			cmds = append(cmds, frame{c.ind, c.mode, c.d.eval(base+w0-w, c.ind)})
		case dHard:
			// A hard newline inside a group being probed for flatness
			// means the group cannot be flat; in break mode it simply
			// ends the line, so the probed prefix fit.
			return c.mode == mBreak
		}
	}
	return w >= 0
}

// flatWidth returns the number of columns d occupies when every group
// inside it renders flat, starting at column col. ok is false when d
// cannot render without a newline — it contains a hard break, or a
// lazy inside it decides on broken form at its landing column. Lazy
// docs are probed with the accumulated column (the same value layout
// would hand them); the structural indent is irrelevant for docs
// whose flat branch is being measured.
func flatWidth(d *doc, col int) (w int, ok bool) {
	switch d.kind {
	case dNil:
		return 0, true
	case dText:
		return utf8.RuneCountInString(d.text), true
	case dConcat:
		total := 0
		for _, k := range d.kids {
			kw, ok := flatWidth(k, col+total)
			if !ok {
				return 0, false
			}
			total += kw
		}
		return total, true
	case dNest, dGroup:
		return flatWidth(d.d, col)
	case dLazy:
		next := d.eval(col, 0)
		if next == nil {
			return 0, false
		}
		return flatWidth(next, col)
	case dLine:
		return 1, true
	case dSoft:
		return 0, true
	case dHard:
		return 0, false
	}
	return 0, false
}
