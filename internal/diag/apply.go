package diag

import (
	"cmp"
	"fmt"
	"io"
	"slices"
)

// Applied is the "applied" message gom fix emits once per fix it
// applied (docs/spec.md section 12): the code of the diagnostic the
// fix answered, the fix's title, and the edits it landed, described
// in the coordinate space of the pre-edit sources — the same space
// the diagnostic's own spans use.
type Applied struct {
	// Code is the code of the diagnostic the fix was offered on.
	Code Code
	// Title is the fix's title, as it appeared in "fixes".
	Title string
	// Edits are the edits that were applied.
	Edits []Edit
}

// wireApplied is the JSON shape of an "applied" object
// (docs/spec.md section 12).
type wireApplied struct {
	Kind  string     `json:"kind"`
	Code  string     `json:"code"`
	Title string     `json:"title"`
	Edits []wireEdit `json:"edits"`
}

// MarshalApplied renders a as one spec section 12 "applied" object —
// a single line of JSON with no trailing newline. Edit spans resolve
// through res exactly like the fix edits of a diagnostic; the first
// edit whose file cannot be resolved stops the marshal with an error.
func MarshalApplied(a Applied, res Resolver) ([]byte, error) {
	w := wireApplied{
		Kind:  string(KindApplied),
		Code:  string(a.Code),
		Title: a.Title,
		Edits: make([]wireEdit, 0, len(a.Edits)),
	}
	for _, e := range a.Edits {
		rp, err := ResolveSpan(res, e.Span)
		if err != nil {
			return nil, fmt.Errorf("diag: applied fix %q edit: %w", a.Title, err)
		}
		w.Edits = append(w.Edits, wireEdit{SpanPos: rp, Text: e.Text})
	}
	return marshalJSON(w)
}

// WriteApplied writes the run's applied messages to w before the
// summary — JSON lines for format "json"; for format "text" a
// one-line header per fix plus a "--> file:l:c" line per edit, the
// same convention the text diagnostics use.
func WriteApplied(w io.Writer, format string, msgs []Applied, res Resolver) error {
	for _, m := range msgs {
		if format == "text" {
			if _, err := fmt.Fprintf(w, "applied[%s]: %s\n", m.Code, m.Title); err != nil {
				return err
			}
			for _, e := range m.Edits {
				rp, err := ResolveSpan(res, e.Span)
				if err != nil {
					return fmt.Errorf("diag: applied fix %q edit: %w", m.Title, err)
				}
				if _, err := fmt.Fprintf(w, "  --> %s:%d:%d\n",
					rp.File, rp.Start.Line, rp.Start.Col); err != nil {
					return err
				}
			}
			continue
		}
		raw, err := MarshalApplied(m, res)
		if err != nil {
			return err
		}
		if err := writeLine(w, raw); err != nil {
			return err
		}
	}
	return nil
}

// ApplyEdits applies edits to src and returns the resulting source.
// Each edit replaces the byte range Edit.Span by Edit.Text — an
// insertion when the span is empty, a deletion when the text is.
// All offsets refer to src as it was before any edit: the edits
// apply simultaneously. The caller groups edits by file and passes
// only this file's edits (Edit.Span.File is not consulted).
//
// Every edit must be valid and inside src, and no two edits may
// overlap — overlapping edits have no well-defined simultaneous
// result, so an error is returned instead of guessing. Touches are
// fine: an insertion at a replaced range's edge applies cleanly, and
// several insertions at the same point concatenate in the order the
// edits were given.
func ApplyEdits(src []byte, edits []Edit) ([]byte, error) {
	es := slices.Clone(edits)
	slices.SortStableFunc(es, func(a, b Edit) int {
		return cmp.Or(
			cmp.Compare(b.Span.Start, a.Span.Start),
			cmp.Compare(b.Span.End, a.Span.End),
		)
	})
	for i, e := range es {
		if !e.Span.Valid() {
			return nil, fmt.Errorf("diag: edit %d: invalid span {File:%q Start:%d End:%d}",
				i, e.Span.File, e.Span.Start, e.Span.End)
		}
		if e.Span.End > len(src) {
			return nil, fmt.Errorf("diag: edit %d: span %d-%d is outside %q (%d bytes)",
				i, e.Span.Start, e.Span.End, e.Span.File, len(src))
		}
		// Sorted descending by start, an overlap with any earlier
		// edit implies an overlap with the immediate predecessor.
		// Zero-width edits at a span's edge do not overlap it.
		if i > 0 && e.Span.End > es[i-1].Span.Start {
			return nil, fmt.Errorf("diag: edit %d: span %d-%d overlaps edit %d (%d-%d)",
				i, e.Span.Start, e.Span.End, i-1, es[i-1].Span.Start, es[i-1].Span.End)
		}
	}
	out := slices.Clone(src)
	for i := 0; i < len(es); {
		j := i + 1
		text := es[i].Text
		// Equal spans surviving the overlap check are all
		// zero-width: fold same-point insertions into one edit so
		// their texts land in the order the edits were given.
		for j < len(es) && es[j].Span == es[i].Span {
			text += es[j].Text
			j++
		}
		out = slices.Replace(out, es[i].Span.Start, es[i].Span.End, []byte(text)...)
		i = j
	}
	return out, nil
}
