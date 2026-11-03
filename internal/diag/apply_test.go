package diag_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

// TestApplyEdits covers the mechanical splicing: inserts, deletes,
// replacements, out-of-order input, and same-point inserts landing
// in the order the edits were given.
func TestApplyEdits(t *testing.T) {
	src := []byte("hello, world\n")

	t.Run("replace", func(t *testing.T) {
		got, err := diag.ApplyEdits(src, []diag.Edit{
			{Span: diag.SpanIn("f", 7, 12), Text: "gom"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "hello, gom\n" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("insert and delete", func(t *testing.T) {
		got, err := diag.ApplyEdits(src, []diag.Edit{
			{Span: diag.Point("f", 0), Text: "say: "},
			{Span: diag.SpanIn("f", 12, 13), Text: ""},
		})
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "say: hello, world" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("unordered edits apply simultaneously", func(t *testing.T) {
		got, err := diag.ApplyEdits([]byte("ab"), []diag.Edit{
			{Span: diag.SpanIn("f", 0, 1), Text: "AB"},
			{Span: diag.SpanIn("f", 1, 2), Text: "ba"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "ABba" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("same-point inserts keep given order", func(t *testing.T) {
		got, err := diag.ApplyEdits([]byte("xy"), []diag.Edit{
			{Span: diag.Point("f", 1), Text: "1"},
			{Span: diag.Point("f", 1), Text: "2"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "x12y" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("insertion at a replaced edge is not an overlap", func(t *testing.T) {
		got, err := diag.ApplyEdits(src, []diag.Edit{
			{Span: diag.SpanIn("f", 0, 5), Text: "bye"},
			{Span: diag.Point("f", 0), Text: "("},
			{Span: diag.Point("f", 5), Text: ")"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "(bye), world\n" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("the source is not mutated", func(t *testing.T) {
		s := []byte("ab")
		if _, err := diag.ApplyEdits(s, []diag.Edit{
			{Span: diag.SpanIn("f", 0, 2), Text: "xyz"},
		}); err != nil {
			t.Fatal(err)
		}
		if string(s) != "ab" {
			t.Errorf("src mutated: %q", s)
		}
	})
}

// TestApplyEditsErrors: overlapping, out-of-bounds and invalid edits
// are rejected rather than producing a guessed result.
func TestApplyEditsErrors(t *testing.T) {
	src := []byte("hello\n")
	for _, tt := range []struct {
		name  string
		edits []diag.Edit
	}{
		{"overlap", []diag.Edit{
			{Span: diag.SpanIn("f", 0, 4), Text: "a"},
			{Span: diag.SpanIn("f", 2, 5), Text: "b"},
		}},
		{"same span with text", []diag.Edit{
			{Span: diag.SpanIn("f", 0, 2), Text: "a"},
			{Span: diag.SpanIn("f", 0, 2), Text: "b"},
		}},
		{"insert inside a replace", []diag.Edit{
			{Span: diag.SpanIn("f", 0, 5), Text: "a"},
			{Span: diag.Point("f", 3), Text: "b"},
		}},
		{"out of bounds", []diag.Edit{
			{Span: diag.SpanIn("f", 0, 100), Text: "a"},
		}},
		{"invalid span", []diag.Edit{
			{Span: diag.Span{File: "f", Start: 4, End: 2}, Text: "a"},
		}},
	} {
		if _, err := diag.ApplyEdits(src, tt.edits); err == nil {
			t.Errorf("%s: want error", tt.name)
		}
	}
}

// TestMarshalApplied pins the "applied" wire shape: one object per
// applied fix, edits resolved to line/column in the pre-edit source.
func TestMarshalApplied(t *testing.T) {
	src := []byte("let x = 1\nfoo ()\n")
	res := diag.NewMapResolver(map[string][]byte{"a.gom": src})
	a := diag.Applied{
		Code:  "E0402",
		Title: "discard the value with `ignore`",
		Edits: []diag.Edit{
			{Span: diag.Point("a.gom", 10), Text: "ignore ("},
			{Span: diag.Point("a.gom", 16), Text: ")"},
		},
	}
	raw, err := diag.MarshalApplied(a, res)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"kind":"applied","code":"E0402","title":"discard the value with ` + "`ignore`" + `","edits":[` +
		`{"file":"a.gom","start":{"line":2,"col":1,"offset":10},"end":{"line":2,"col":1,"offset":10},"text":"ignore ("},` +
		`{"file":"a.gom","start":{"line":2,"col":7,"offset":16},"end":{"line":2,"col":7,"offset":16},"text":")"}]}`
	if string(raw) != want {
		t.Errorf("marshal =\n%s\nwant\n%s", raw, want)
	}
}

// TestMarshalAppliedUnresolvable reports an edit in an unknown file
// instead of emitting it.
func TestMarshalAppliedUnresolvable(t *testing.T) {
	res := diag.NewMapResolver(nil)
	a := diag.Applied{Code: "E0402", Title: "t",
		Edits: []diag.Edit{{Span: diag.Point("gone.gom", 0), Text: "x"}}}
	if _, err := diag.MarshalApplied(a, res); err == nil {
		t.Error("want error")
	}
}

// TestWriteApplied covers both renderings of the applied stream:
// JSON lines and the text block form.
func TestWriteApplied(t *testing.T) {
	src := []byte("let x = 1\nfoo ()\n")
	res := diag.NewMapResolver(map[string][]byte{"a.gom": src})
	msgs := []diag.Applied{{
		Code:  "E0402",
		Title: "discard the value with `ignore`",
		Edits: []diag.Edit{
			{Span: diag.Point("a.gom", 10), Text: "ignore ("},
			{Span: diag.Point("a.gom", 16), Text: ")"},
		},
	}}

	var jbuf bytes.Buffer
	if err := diag.WriteApplied(&jbuf, "json", msgs, res); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(jbuf.String(), `{"kind":"applied","code":"E0402"`) ||
		!strings.HasSuffix(jbuf.String(), "}\n") {
		t.Errorf("json output = %q", jbuf.String())
	}

	var tbuf bytes.Buffer
	if err := diag.WriteApplied(&tbuf, "text", msgs, res); err != nil {
		t.Fatal(err)
	}
	want := "applied[E0402]: discard the value with `ignore`\n" +
		"  --> a.gom:2:1\n  --> a.gom:2:7\n"
	if tbuf.String() != want {
		t.Errorf("text output = %q, want %q", tbuf.String(), want)
	}
}
