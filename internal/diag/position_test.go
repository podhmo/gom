package diag_test

import (
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

func TestSpanConstructors(t *testing.T) {
	sp := diag.SpanIn("a.gom", 41, 228)
	if sp.File != "a.gom" || sp.Start != 41 || sp.End != 228 {
		t.Errorf("SpanIn = %+v", sp)
	}

	pt := diag.Point("a.gom", 226)
	if pt.File != "a.gom" || pt.Start != 226 || pt.End != 226 {
		t.Errorf("Point = %+v", pt)
	}
	if pt.Len() != 0 {
		t.Errorf("Point().Len() = %d, want 0", pt.Len())
	}
}

func TestSpanIsZero(t *testing.T) {
	tests := []struct {
		name string
		span diag.Span
		want bool
	}{
		{"zero value", diag.Span{}, true},
		{"point at offset 0", diag.Point("a.gom", 0), false},
		{"normal span", diag.SpanIn("a.gom", 1, 2), false},
		{"offsets without file", diag.Span{Start: 1, End: 2}, false},
	}
	for _, tt := range tests {
		if got := tt.span.IsZero(); got != tt.want {
			t.Errorf("%s: IsZero() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestSpanValid(t *testing.T) {
	tests := []struct {
		name string
		span diag.Span
		want bool
	}{
		{"normal span", diag.SpanIn("a.gom", 41, 228), true},
		{"zero-width point", diag.Point("a.gom", 0), true},
		{"zero value is not valid", diag.Span{}, false},
		{"missing file", diag.Span{Start: 1, End: 2}, false},
		{"inverted range", diag.SpanIn("a.gom", 228, 41), false},
		{"negative start", diag.SpanIn("a.gom", -1, 2), false},
	}
	for _, tt := range tests {
		if got := tt.span.Valid(); got != tt.want {
			t.Errorf("%s: Valid() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestSpanLenAndContains(t *testing.T) {
	sp := diag.SpanIn("a.gom", 10, 20)
	if got := sp.Len(); got != 10 {
		t.Errorf("Len() = %d, want 10", got)
	}
	for _, off := range []int{10, 15, 19} {
		if !sp.Contains(off) {
			t.Errorf("Contains(%d) = false, want true", off)
		}
	}
	for _, off := range []int{9, 20, 100, -1} {
		if sp.Contains(off) {
			t.Errorf("Contains(%d) = true, want false", off)
		}
	}
	// A zero-width span contains nothing, not even its own offset.
	if diag.Point("a.gom", 5).Contains(5) {
		t.Error("Point().Contains(5) = true, want false")
	}
}
