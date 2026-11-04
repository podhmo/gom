package geom_test

// The Go consumer of the generated geom package (P3-6, spec 8.7):
// the .gomi surface arrives as ordinary Go — the ADT as a sealed
// interface plus variant structs, List/Go.Slice per the copy/no-copy
// boundary split, named Go types nominal, Go interfaces by
// assignability, and Result/Option as the stdlib representations.
// Nil and invalid values panic the way spec 8.6 spells out.

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"example.com/gocall/geom"

	"github.com/podhmo/gom/stdlib/list"
	"github.com/podhmo/gom/stdlib/option"
	"github.com/podhmo/gom/stdlib/result"
)

// mustPanic runs f and reports the recovered panic's message.
func mustPanic(t *testing.T, f func()) (msg string) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic, got none")
		}
		msg = fmt.Sprint(r)
	}()
	f()
	return ""
}

func TestADTFromGo(t *testing.T) {
	c := geom.Shape_Circle{Radius: 2.0}
	if got := geom.Area(c); math.Abs(got-12.56636) > 1e-3 {
		t.Fatalf("Area(Circle 2) = %v, want ~12.566", got)
	}
	r := geom.Shape_Rect{W: 2.0, H: 3.0}
	if got := geom.Area(r); got != 6.0 {
		t.Fatalf("Area(Rect 2 3) = %v, want 6", got)
	}
	// The ADT is a sealed interface: Go callers discriminate with a
	// type switch over the variant structs.
	var s geom.Shape = r
	switch v := s.(type) {
	case geom.Shape_Circle:
		t.Fatalf("type switch hit Circle for a Rect value")
	case geom.Shape_Rect:
		if v.W != 2.0 || v.H != 3.0 {
			t.Fatalf("Rect fields = (%v, %v), want (2, 3)", v.W, v.H)
		}
	default:
		t.Fatalf("type switch missed declared variants: %T", s)
	}
	if got := geom.Describe(c); got != "circle" {
		t.Fatalf("Describe(Circle) = %q, want %q", got, "circle")
	}
}

// An invalid ADT value — here nil, the only invalid Shape a Go
// caller can build since the interface is sealed — panics at match
// with the type name in the message (spec 8.6).
func TestInvalidADTFromGo(t *testing.T) {
	msg := mustPanic(t, func() { geom.Describe(nil) })
	if !strings.Contains(msg, "Shape") || !strings.Contains(msg, "invalid") {
		t.Fatalf("Describe(nil) panic = %q, want an invalid-Shape message", msg)
	}

	// The Result zero value is invalid too — IsOk matches on it.
	var zero result.Result[int64, string]
	msg = mustPanic(t, func() { geom.IsOk(zero) })
	if !strings.Contains(msg, "Result") || !strings.Contains(msg, "invalid") {
		t.Fatalf("IsOk(zero Result) panic = %q, want an invalid-Result message", msg)
	}
}

func TestResultFromGo(t *testing.T) {
	ok := geom.HeadOr(list.FromSlice([]int64{7, 8}))
	if ok.Tag() != result.ResultTagOk {
		t.Fatalf("HeadOr([7 8]).Tag() = %v, want Ok", ok.Tag())
	}
	if v, e := ok.Unwrap(); v != 7 || e != "" {
		t.Fatalf("HeadOr([7 8]).Unwrap() = (%v, %q), want (7, %q)", v, e, "")
	}
	e := geom.HeadOr(nil) // nil list is the empty list
	if e.Tag() != result.ResultTagErr {
		t.Fatalf("HeadOr(nil).Tag() = %v, want Err", e.Tag())
	}
	if v, err := e.Unwrap(); v != 0 || err != "empty" {
		t.Fatalf("HeadOr(nil).Unwrap() = (%v, %q), want (0, %q)", v, err, "empty")
	}
	// Go builds Result values with the public constructors and the
	// Gom side matches on them.
	if !geom.IsOk(result.Ok[int64, string](1)) {
		t.Fatal("IsOk(result.Ok(1)) = false")
	}
	if geom.IsOk(result.Err[int64, string]("x")) {
		t.Fatal("IsOk(result.Err(x)) = true")
	}
}

func TestOptionFromGo(t *testing.T) {
	switch o := geom.HeadOpt(list.List[int64]{9}).(type) {
	case option.Option_Some[int64]:
		if o.V0 != 9 {
			t.Fatalf("HeadOpt([9]).V0 = %v, want 9", o.V0)
		}
	case option.Option_None[int64]:
		t.Fatal("HeadOpt([9]) = None, want Some 9")
	default:
		t.Fatalf("HeadOpt([9]) = %T, want option.Option", o)
	}
	switch o := geom.HeadOpt(nil).(type) {
	case option.Option_None[int64]:
	default:
		t.Fatalf("HeadOpt(nil) = %T, want Option_None", o)
	}
}

// Go pointers and interfaces may be nil; using them panics Go-style
// (spec 8.6) — Gom adds no nil checks.
func TestNilFromGo(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got := geom.FileName(f); got != f.Name() {
		t.Fatalf("FileName(f) = %q, want %q", got, f.Name())
	}
	if msg := mustPanic(t, func() { geom.FileName(nil) }); msg == "" {
		t.Fatal("FileName(nil) did not panic")
	}
	if msg := mustPanic(t, func() { geom.NameOf(nil) }); msg == "" {
		t.Fatal("NameOf(nil) did not panic")
	}
}

// A Go interface parameter accepts anything go/types-assignable —
// here a named type implementing fmt.Stringer.
func TestInterfaceFromGo(t *testing.T) {
	if got := geom.NameOf(2 * time.Second); got != "2s" {
		t.Fatalf("NameOf(2s) = %q, want %q", got, "2s")
	}
}

// Go.Slice is the no-copy view; List crosses by copy — mutation on
// one side never reaches the other (spec 8.5).
func TestSlicesFromGo(t *testing.T) {
	// keepSlice returns the same backing array.
	in := []int64{1, 2, 3}
	out := geom.KeepSlice(in)
	out[0] = 99
	if in[0] != 99 {
		t.Fatalf("KeepSlice: mutating the result did not reach the caller's slice")
	}

	// freeze copies the Go slice into an immutable List.
	src := []int64{1, 2, 3}
	frozen := geom.Freeze(src)
	src[0] = 99
	if got := frozen.ToSlice()[0]; got != 1 {
		t.Fatalf("Freeze: mutating the source changed the list head to %v", got)
	}

	// thaw copies a List into a fresh Go slice.
	l := list.FromSlice([]int64{4, 5})
	sl := geom.Thaw(l)
	sl[0] = 99
	if got := l.ToSlice()[0]; got != 4 {
		t.Fatalf("Thaw: mutating the slice changed the list head to %v", got)
	}

	if got := geom.Total(list.List[int64]{1, 2, 3}); got != 6 {
		t.Fatalf("Total([1 2 3]) = %v, want 6", got)
	}
	// nil is the empty list on the boundary.
	if got := geom.Total(nil); got != 0 {
		t.Fatalf("Total(nil) = %v, want 0", got)
	}
	if !geom.IsEmpty(nil) || geom.IsEmpty(list.List[int64]{1}) {
		t.Fatal("IsEmpty: nil should be empty, [1] should not")
	}
}

// Named Go types stay nominal both ways: time.Duration in and out,
// converted inside Gom with Go.conv.
func TestNamedTypesFromGo(t *testing.T) {
	if got := geom.Nanos(1500 * time.Millisecond); got != 1500000000 {
		t.Fatalf("Nanos(1.5s) = %v, want 1500000000", got)
	}
	d := geom.Duration(42)
	if d != time.Duration(42) {
		t.Fatalf("Duration(42) = %v (%T), want time.Duration(42)", d, d)
	}
	if got := geom.Nanos(geom.Duration(99)); got != 99 {
		t.Fatalf("Nanos(Duration(99)) = %v, want 99", got)
	}
}

// Boxing into a Go `any` parameter keeps the concrete Gom numeric
// type — %T reports int64 for a Gom Int, not the untyped-literal
// default int — across a literal, a bound variable, and an
// arithmetic result, and through a real Go type switch.
func TestAnyBoxingFromGo(t *testing.T) {
	if got := geom.TypeOfLit(struct{}{}); got != "int64" {
		t.Fatalf("TypeOfLit() = %q, want %q", got, "int64")
	}
	if got := geom.TypeOfVar(7); got != "int64" {
		t.Fatalf("TypeOfVar(7) = %q, want %q", got, "int64")
	}
	if got := geom.TypeOfSum(1, 2); got != "int64" {
		t.Fatalf("TypeOfSum(1, 2) = %q, want %q", got, "int64")
	}
	if got := geom.KindOfLit(struct{}{}); got != "int64" {
		t.Fatalf("KindOfLit() = %q, want %q (Go type switch)", got, "int64")
	}
}
