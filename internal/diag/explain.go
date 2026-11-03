package diag

import (
	"fmt"
	"io"
	"strings"
)

// Explanation is the "explanation" message gom explain emits per
// requested error code (docs/spec.md section 12): the error-code
// table's entry for the code rendered to the wire. ExplanationOf
// builds one from a table entry.
type Explanation struct {
	// Code is the stable identifier, e.g. "E0301".
	Code Code
	// Severity is the severity diagnostics with this code carry.
	Severity Severity
	// Title is the one-line description.
	Title string
	// Phase is the compiler component that emits the diagnostic.
	Phase Phase
	// Spec is the spec.md section the rule is defined in, when
	// known; omitted when empty.
	Spec string
	// Explanation is the long-form prose the table carries.
	Explanation string
	// Example is a .gom fragment triggering the diagnostic, when
	// the table provides one; omitted when empty.
	Example string
	// Retired marks a code that is no longer emitted; omitted
	// unless true.
	Retired bool
}

// ExplanationOf returns the Explanation for a table entry.
func ExplanationOf(info CodeInfo) Explanation {
	return Explanation{
		Code:        info.Code,
		Severity:    info.Severity,
		Title:       info.Title,
		Phase:       info.Phase,
		Spec:        info.Spec,
		Explanation: info.Explanation,
		Example:     info.Example,
		Retired:     info.Retired,
	}
}

// wireExplanation is the JSON shape of an "explanation" object
// (docs/spec.md section 12).
type wireExplanation struct {
	Kind        string `json:"kind"`
	Code        string `json:"code"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Phase       string `json:"phase"`
	Spec        string `json:"spec,omitempty"`
	Explanation string `json:"explanation"`
	Example     string `json:"example,omitempty"`
	Retired     bool   `json:"retired,omitempty"`
}

// MarshalExplanation renders e as one spec section 12 "explanation"
// object — a single line of JSON with no trailing newline. Unlike a
// diagnostic it needs no Resolver: nothing it carries is located.
func MarshalExplanation(e Explanation) ([]byte, error) {
	return marshalJSON(wireExplanation{
		Kind:        string(KindExplanation),
		Code:        string(e.Code),
		Severity:    e.Severity.String(),
		Title:       e.Title,
		Phase:       string(e.Phase),
		Spec:        e.Spec,
		Explanation: e.Explanation,
		Example:     e.Example,
		Retired:     e.Retired,
	})
}

// WriteExplanations writes one explanation per entry of es to w:
// JSON lines for format "json"; for format "text" a rustc-style
// block per entry — "error[E0301]: <title>", a phase/spec attribute
// line, the prose, and the example — separated by blank lines.
func WriteExplanations(w io.Writer, format string, es []Explanation) error {
	for i, e := range es {
		if format == "text" {
			if i > 0 {
				if _, err := fmt.Fprintln(w); err != nil {
					return err
				}
			}
			if err := writeExplanationText(w, e); err != nil {
				return err
			}
			continue
		}
		raw, err := MarshalExplanation(e)
		if err != nil {
			return err
		}
		if err := writeLine(w, raw); err != nil {
			return err
		}
	}
	return nil
}

// writeExplanationText renders e for a human reader, mirroring the
// "error[E0301]: …" header of the text diagnostics.
func writeExplanationText(w io.Writer, e Explanation) error {
	retired := ""
	if e.Retired && !strings.Contains(e.Title, "(retired)") {
		retired = " (retired)"
	}
	if _, err := fmt.Fprintf(w, "%s[%s]: %s%s\n", e.Severity, e.Code, e.Title, retired); err != nil {
		return err
	}
	attrs := "phase: " + string(e.Phase)
	if e.Spec != "" {
		attrs += " · spec: §" + e.Spec
	}
	if _, err := fmt.Fprintf(w, "%s\n\n%s\n", attrs, e.Explanation); err != nil {
		return err
	}
	if e.Example != "" {
		if _, err := fmt.Fprintf(w, "\nExample:\n\n%s\n", e.Example); err != nil {
			return err
		}
	}
	return nil
}

// WriteCodeIndex writes the compact per-line code listing a bare
// `gom explain` prints in text format: "E0301  non-exhaustive
// match". Retired codes are marked — unless the title already
// carries the word.
func WriteCodeIndex(w io.Writer, infos []CodeInfo) error {
	for _, info := range infos {
		retired := ""
		if info.Retired && !strings.Contains(info.Title, "(retired)") {
			retired = " (retired)"
		}
		if _, err := fmt.Fprintf(w, "%s  %s%s\n", info.Code, info.Title, retired); err != nil {
			return err
		}
	}
	return nil
}
