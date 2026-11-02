package diag

import (
	"fmt"
	"io"
)

// Unformatted is the "unformatted" message gom fmt --check emits once
// per file whose contents differ from gom fmt's output (docs/spec.md
// section 12). It carries the file's unified diff so callers can show
// what would change without rerunning the formatter.
type Unformatted struct {
	File string
	Diff string
}

// MarshalJSON renders u as the wire object
// {"kind":"unformatted","file":…,"diff":…}.
func (u Unformatted) MarshalJSON() ([]byte, error) {
	type wire struct {
		Kind string `json:"kind"`
		File string `json:"file"`
		Diff string `json:"diff"`
	}
	return marshalJSON(wire{string(KindUnformatted), u.File, u.Diff})
}

// WriteUnformatted writes the run's unformatted messages to w before
// the diagnostics/summary stream — JSON lines for the "json" format,
// "file: needs formatting" plus the diff for "text".
func WriteUnformatted(w io.Writer, format string, msgs []Unformatted) error {
	for _, m := range msgs {
		if format == "text" {
			if _, err := fmt.Fprintf(w, "%s: needs formatting\n%s", m.File, m.Diff); err != nil {
				return err
			}
			continue
		}
		data, err := m.MarshalJSON()
		if err != nil {
			return err
		}
		if _, err := w.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	return nil
}
