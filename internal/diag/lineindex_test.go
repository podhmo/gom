package diag_test

import (
	"encoding/json"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

func TestLineIndexPosASCII(t *testing.T) {
	// "abc\ndef\n": a=0 b=1 c=2 \n=3 d=4 e=5 f=6 \n=7, len=8
	li := diag.NewLineIndex([]byte("abc\ndef\n"))

	tests := []struct {
		offset int
		want   diag.Pos
	}{
		{0, diag.Pos{Line: 1, Col: 1, Offset: 0}},
		{2, diag.Pos{Line: 1, Col: 3, Offset: 2}},
		{3, diag.Pos{Line: 1, Col: 4, Offset: 3}}, // the '\n' itself
		{4, diag.Pos{Line: 2, Col: 1, Offset: 4}},
		{6, diag.Pos{Line: 2, Col: 3, Offset: 6}},
		{7, diag.Pos{Line: 2, Col: 4, Offset: 7}},
		{8, diag.Pos{Line: 3, Col: 1, Offset: 8}}, // past the trailing '\n'
	}
	for _, tt := range tests {
		if got := li.Pos(tt.offset); got != tt.want {
			t.Errorf("Pos(%d) = %+v, want %+v", tt.offset, got, tt.want)
		}
	}
}

func TestLineIndexPosMultiByte(t *testing.T) {
	// "あいうx\ny": あ=0-2 い=3-5 う=6-8 x=9 \n=10 y=11, len=12.
	// Columns count Unicode code points, not bytes (spec section 12).
	li := diag.NewLineIndex([]byte("あいうx\ny"))

	tests := []struct {
		offset int
		want   diag.Pos
	}{
		{0, diag.Pos{Line: 1, Col: 1, Offset: 0}},
		{3, diag.Pos{Line: 1, Col: 2, Offset: 3}},
		{6, diag.Pos{Line: 1, Col: 3, Offset: 6}},
		{9, diag.Pos{Line: 1, Col: 4, Offset: 9}},   // x: 4th code point
		{10, diag.Pos{Line: 1, Col: 5, Offset: 10}}, // '\n'
		{11, diag.Pos{Line: 2, Col: 1, Offset: 11}},
		{12, diag.Pos{Line: 2, Col: 2, Offset: 12}}, // EOF
	}
	for _, tt := range tests {
		if got := li.Pos(tt.offset); got != tt.want {
			t.Errorf("Pos(%d) = %+v, want %+v", tt.offset, got, tt.want)
		}
	}
}

func TestLineIndexPosUTF16(t *testing.T) {
	// "𐐀x": 𐐀 is U+10400 — 4 UTF-8 bytes at offsets 0-3 and a surrogate
	// pair (2 code units) in UTF-16; x is at offset 4.
	li := diag.NewLineIndex([]byte("𐐀x"))

	// Code-point columns (the message format's unit).
	if got, want := li.Pos(4), (diag.Pos{Line: 1, Col: 2, Offset: 4}); got != want {
		t.Errorf("Pos(4) = %+v, want %+v", got, want)
	}
	if got, want := li.Pos(5), (diag.Pos{Line: 1, Col: 3, Offset: 5}); got != want {
		t.Errorf("Pos(5) = %+v, want %+v", got, want)
	}

	// UTF-16 columns (LSP's unit): 𐐀 occupies two code units.
	if got, want := li.PosUTF16(4), (diag.Pos{Line: 1, Col: 3, Offset: 4}); got != want {
		t.Errorf("PosUTF16(4) = %+v, want %+v", got, want)
	}
	if got, want := li.PosUTF16(5), (diag.Pos{Line: 1, Col: 4, Offset: 5}); got != want {
		t.Errorf("PosUTF16(5) = %+v, want %+v", got, want)
	}
}

func TestLineIndexPosEdgeCases(t *testing.T) {
	t.Run("empty source", func(t *testing.T) {
		li := diag.NewLineIndex(nil)
		if got := li.LineCount(); got != 1 {
			t.Errorf("LineCount() = %d, want 1", got)
		}
		if got, want := li.Pos(0), (diag.Pos{Line: 1, Col: 1, Offset: 0}); got != want {
			t.Errorf("Pos(0) = %+v, want %+v", got, want)
		}
	})

	t.Run("out-of-range offsets are clamped", func(t *testing.T) {
		li := diag.NewLineIndex([]byte("ab\ncd"))
		if got, want := li.Pos(-5), (diag.Pos{Line: 1, Col: 1, Offset: 0}); got != want {
			t.Errorf("Pos(-5) = %+v, want %+v", got, want)
		}
		if got, want := li.Pos(1000), (diag.Pos{Line: 2, Col: 3, Offset: 5}); got != want {
			t.Errorf("Pos(1000) = %+v, want %+v", got, want)
		}
	})

	t.Run("offset inside a multi-byte character", func(t *testing.T) {
		// Best-effort column: each byte of a truncated sequence counts
		// as one invalid rune (RuneError), keeping the result sane.
		li := diag.NewLineIndex([]byte("あ")) // 3 bytes: E3 81 82
		if got, want := li.Pos(1), (diag.Pos{Line: 1, Col: 2, Offset: 1}); got != want {
			t.Errorf("Pos(1) = %+v, want %+v", got, want)
		}
		if got, want := li.Pos(3), (diag.Pos{Line: 1, Col: 2, Offset: 3}); got != want {
			t.Errorf("Pos(3) = %+v, want %+v", got, want)
		}
	})

	t.Run("CRLF line endings", func(t *testing.T) {
		// '\r' is an ordinary column character: "a\r\nb" puts b on
		// line 2 and counts '\r' in line 1's columns.
		li := diag.NewLineIndex([]byte("a\r\nb"))
		if got, want := li.Pos(1), (diag.Pos{Line: 1, Col: 2, Offset: 1}); got != want {
			t.Errorf("Pos(1) = %+v, want %+v", got, want)
		}
		if got, want := li.Pos(3), (diag.Pos{Line: 2, Col: 1, Offset: 3}); got != want {
			t.Errorf("Pos(3) = %+v, want %+v", got, want)
		}
	})
}

func TestLineIndexLines(t *testing.T) {
	li := diag.NewLineIndex([]byte("abc\ndef\n"))

	if got := li.LineCount(); got != 3 {
		t.Errorf("LineCount() = %d, want 3", got)
	}

	for line, want := range map[int]string{1: "abc", 2: "def", 3: ""} {
		if got := string(li.LineText(line)); got != want {
			t.Errorf("LineText(%d) = %q, want %q", line, got, want)
		}
	}

	if start, end := li.LineBounds(2); start != 4 || end != 7 {
		t.Errorf("LineBounds(2) = (%d, %d), want (4, 7)", start, end)
	}
	// Out-of-range lines are clamped.
	if start, end := li.LineBounds(0); start != 0 || end != 3 {
		t.Errorf("LineBounds(0) = (%d, %d), want (0, 3)", start, end)
	}
	if start, end := li.LineBounds(99); start != 8 || end != 8 {
		t.Errorf("LineBounds(99) = (%d, %d), want (8, 8)", start, end)
	}
}

func TestLineIndexResolve(t *testing.T) {
	// A match expression spanning several lines, like the spec's
	// E0301 example.
	src := "let x = 1\nmatch e {\n  Num n -> n\n  Neg x -> あ\n}\n"
	li := diag.NewLineIndex([]byte(src))

	// "match e {" starts on line 2; the closing '}' is at offset 48.
	sp := diag.SpanIn("eval.gom", 10, 49)
	got := li.Resolve(sp)
	want := diag.SpanPos{
		File:  "eval.gom",
		Start: diag.Pos{Line: 2, Col: 1, Offset: 10},
		End:   diag.Pos{Line: 5, Col: 2, Offset: 49},
	}
	if got != want {
		t.Errorf("Resolve(%+v) = %+v, want %+v", sp, got, want)
	}

	// The あ on line 4 is at offsets 44-46: code-point column 12.
	if got, want := li.Pos(44), (diag.Pos{Line: 4, Col: 12, Offset: 44}); got != want {
		t.Errorf("Pos(44) = %+v, want %+v", got, want)
	}
}

func TestSpanPosJSONShape(t *testing.T) {
	// The resolved span marshals to exactly the position objects of
	// the message format (docs/spec.md section 12).
	li := diag.NewLineIndex([]byte("abc\ndef\n"))
	b, err := json.Marshal(li.Resolve(diag.SpanIn("eval.gom", 4, 6)))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"file":"eval.gom","start":{"line":2,"col":1,"offset":4},"end":{"line":2,"col":3,"offset":6}}`
	if string(b) != want {
		t.Errorf("marshal = %s, want %s", b, want)
	}
}
