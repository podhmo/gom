package diag_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

// TestMarshalExplanation pins the "explanation" wire shape: the
// table entry's fields with spec/example/retired omitted when empty.
func TestMarshalExplanation(t *testing.T) {
	info, ok := diag.LookupCode("E0301")
	if !ok {
		t.Fatal("E0301 not in the table")
	}
	raw, err := diag.MarshalExplanation(diag.ExplanationOf(info))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		`"kind":"explanation"`, `"code":"E0301"`, `"severity":"error"`,
		`"title":"non-exhaustive match"`, `"phase":"pattern"`, `"spec":"4"`,
		`"explanation":"The match arms do not cover`,
		`"example":"fn is_some`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("marshal missing %s: %s", want, s)
		}
	}
	if strings.Contains(s, `"retired"`) {
		t.Errorf("active code should omit retired: %s", s)
	}
}

// TestMarshalExplanationRetired: a retired code serializes
// "retired": true.
func TestMarshalExplanationRetired(t *testing.T) {
	info, ok := diag.LookupCode("E0305")
	if !ok {
		t.Fatal("E0305 not in the table")
	}
	raw, err := diag.MarshalExplanation(diag.ExplanationOf(info))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"retired":true`) {
		t.Errorf("retired code should carry retired:true: %s", raw)
	}
}

// TestWriteExplanations covers both formats: JSON lines, and the
// text block with the diagnostic-style header and example.
func TestWriteExplanations(t *testing.T) {
	es := []diag.Explanation{diag.ExplanationOf(mustCode(t, "E0301"))}

	var jbuf bytes.Buffer
	if err := diag.WriteExplanations(&jbuf, "json", es); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(jbuf.String(), `{"kind":"explanation","code":"E0301"`) {
		t.Errorf("json output = %q", jbuf.String())
	}

	var tbuf bytes.Buffer
	if err := diag.WriteExplanations(&tbuf, "text", es); err != nil {
		t.Fatal(err)
	}
	s := tbuf.String()
	for _, want := range []string{
		"error[E0301]: non-exhaustive match",
		"phase: pattern · spec: §4",
		"The match arms do not cover",
		"Example:\n\nfn is_some",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("text output missing %q:\n%s", want, s)
		}
	}
}

// TestWriteCodeIndex: the bare `gom explain` text list is one
// compact line per code, retired codes marked.
func TestWriteCodeIndex(t *testing.T) {
	var buf bytes.Buffer
	if err := diag.WriteCodeIndex(&buf, diag.CodeTable()); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, "E0301  non-exhaustive match\n") {
		t.Errorf("index missing E0301 line: %q", s)
	}
	if !strings.Contains(s, "E0305  non-exhaustive select (retired)\n") {
		t.Errorf("index missing E0305: %q", s)
	}
	if got, want := len(strings.Split(strings.TrimSpace(s), "\n")), len(diag.CodeTable()); got != want {
		t.Errorf("index lines = %d, want %d (one per table entry)", got, want)
	}
}

func mustCode(t *testing.T, c diag.Code) diag.CodeInfo {
	t.Helper()
	info, ok := diag.LookupCode(c)
	if !ok {
		t.Fatalf("%s not in the table", c)
	}
	return info
}
