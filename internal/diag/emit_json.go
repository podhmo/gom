package diag

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// marshalJSON is json.Marshal without HTML escaping: messages carry
// Gom operators such as "->", "|>" and "&&", which must reach the wire
// readable rather than as < escapes.
func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// MarshalDiagnostic renders d as one spec section 12 "diagnostic"
// object — a single line of JSON with no trailing newline. Spans are
// resolved to line and column through res; a diagnostic without a
// source location omits "span". An error is returned when d is
// invalid, a span's file cannot be resolved, or Data is not
// JSON-representable.
//
// The result can be embedded in custom streams as json.RawMessage, or
// written followed by '\n' as one JSON Lines row.
func MarshalDiagnostic(d Diagnostic, res Resolver) ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("diag: cannot marshal diagnostic %s: %w", d.Code, err)
	}
	w, err := wireOf(d, res)
	if err != nil {
		return nil, err
	}
	return marshalJSON(w)
}

// WriteJSONLines renders the report in the default message format
// (docs/spec.md section 12): one "diagnostic" object per line, then
// the run-phase "test" objects when the report carries test results,
// closed by the "summary" object — including when the report is
// empty, in which case the stream is just the summary line.
//
// Diagnostics are emitted in List.Sort order on a copy of the list.
// Each diagnostic must pass Validate and every span (including fix
// edits) must resolve through res; the first failure stops the write
// and is returned — the driver reports such failures as internal
// errors (exit code 2), and the stream may be left partially written.
func (r Report) WriteJSONLines(w io.Writer, res Resolver) error {
	for i, d := range r.orderedDiagnostics() {
		raw, err := MarshalDiagnostic(d, res)
		if err != nil {
			return fmt.Errorf("diag: cannot emit diagnostics[%d]: %w", i, err)
		}
		if err := writeLine(w, raw); err != nil {
			return err
		}
	}
	if err := r.writeTestsJSON(w, res); err != nil {
		return err
	}
	raw, err := r.Summary().MarshalJSON()
	if err != nil {
		return err
	}
	return writeLine(w, raw)
}

// writeLine writes b followed by a newline — one JSON Lines row.
func writeLine(w io.Writer, b []byte) error {
	_, err := w.Write(append(b, '\n'))
	return err
}

// wireDiagnostic is the JSON shape of a "diagnostic" object
// (docs/spec.md section 12), in the spec's field order. SpanPos and
// Pos already carry the matching JSON tags.
type wireDiagnostic struct {
	Kind     string         `json:"kind"`
	Code     string         `json:"code"`
	Severity string         `json:"severity"`
	Message  string         `json:"message"`
	Span     *SpanPos       `json:"span,omitempty"`
	Source   string         `json:"source,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	Notes    []string       `json:"notes,omitempty"`
	Fixes    []wireFix      `json:"fixes,omitempty"`
}

// wireFix is the JSON shape of one entry of "fixes".
type wireFix struct {
	Title         string     `json:"title"`
	Applicability string     `json:"applicability"`
	Edits         []wireEdit `json:"edits"`
}

// wireEdit is the JSON shape of one entry of a fix's "edits": the
// resolved span plus the replacement text.
type wireEdit struct {
	SpanPos
	Text string `json:"text"`
}

// wireOf converts d to its wire form, resolving spans through res.
func wireOf(d Diagnostic, res Resolver) (wireDiagnostic, error) {
	w := wireDiagnostic{
		Kind:     string(KindDiagnostic),
		Code:     string(d.Code),
		Severity: d.Severity.String(),
		Message:  d.Message,
		Source:   string(d.Source),
		Data:     d.Data,
		Notes:    d.Notes,
	}
	if !d.Span.IsZero() {
		rp, err := ResolveSpan(res, d.Span)
		if err != nil {
			return w, fmt.Errorf("diag: diagnostic %s: cannot resolve span %s:%d-%d: %w",
				d.Code, d.Span.File, d.Span.Start, d.Span.End, err)
		}
		w.Span = &rp
	}
	for _, f := range d.Fixes {
		wf := wireFix{
			Title:         f.Title,
			Applicability: f.Applicability.String(),
			Edits:         make([]wireEdit, 0, len(f.Edits)),
		}
		for _, e := range f.Edits {
			rp, err := ResolveSpan(res, e.Span)
			if err != nil {
				return w, fmt.Errorf("diag: diagnostic %s: fix %q edit: %w",
					d.Code, f.Title, err)
			}
			wf.Edits = append(wf.Edits, wireEdit{SpanPos: rp, Text: e.Text})
		}
		w.Fixes = append(w.Fixes, wf)
	}
	return w, nil
}
