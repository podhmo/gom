package boundary_test

// The Go consumer half of the boundary-aliasing fixture
// (docs/sketch/2026-11-04-boundary-aliasing-hypotheses.md).
//
// H1–H3 run inside the generated Gom functions and report read
// pairs as tuples; H5–H7 are observed from this side by mutating
// what the generated API hands out and re-reading through the same
// API. Every assertion below records *observed* behavior — the
// aliasing results are the finding, not a bug in the test.

import (
	"errors"
	"testing"

	"example.com/gocall/boundary"
	"example.com/gocall/box"

	"github.com/podhmo/gom/stdlib/option"
	"github.com/podhmo/gom/stdlib/result"
)

// H1+H2: a value-typed struct detaches — Go's MutateN never reaches
// the copy Gom holds — while the same field read through a Ptr sees
// every Go-side write.
func TestStructFieldReads(t *testing.T) {
	box.Reset()
	got := boundary.H1Value(struct{}{})
	// (before, b.N, p.N) — the detached copy stays 1 after Go
	// mutated shared to 101; the fresh pointer read sees 101.
	if got.V0 != 1 || got.V1 != 1 || got.V2 != 101 {
		t.Fatalf("H1Value = (%d, %d, %d), want (1, 1, 101)", got.V0, got.V1, got.V2)
	}

	box.Reset()
	got2 := boundary.H2Ptr(struct{}{})
	// (before, p.N after mutation) — the Ptr aliases Go's struct.
	if got2.V0 != 1 || got2.V1 != 101 {
		t.Fatalf("H2Ptr = (%d, %d), want (1, 101)", got2.V0, got2.V1)
	}
}

// H3: a []int64 field reads as `List Int` — each read is a fresh
// boundary copy, so the pre-mutation binding keeps its contents and
// a second read snapshots the mutated ones. The same holds through
// a value copy of the struct (whose own Xs still aliases Go's
// backing — only the List copy insulates it).
func TestSliceFieldReads(t *testing.T) {
	box.Reset()
	got := boundary.H3SliceField(struct{}{})
	// heads of the pre- and post-mutation List copies: (10, 99).
	if got.V0 != 10 || got.V1 != 99 {
		t.Fatalf("H3SliceField = (%d, %d), want (10, 99)", got.V0, got.V1)
	}

	box.Reset()
	got2 := boundary.H3SliceFieldValue(struct{}{})
	if got2.V0 != 10 || got2.V1 != 77 {
		t.Fatalf("H3SliceFieldValue = (%d, %d), want (10, 77)", got2.V0, got2.V1)
	}
}

// H3 one level down: a [][]int64 field reads as
// `List (Go.Slice Int)` — the outer slice copies, the inner slices
// stay shared `Go.Slice` views over Go's backing. The Go-side
// MutateXX write lands in both the pre-mutation "snapshot" and a
// fresh read.
func TestNestedSliceFieldReads(t *testing.T) {
	box.Reset()
	got := boundary.H3NestedField(struct{}{})
	// inner element read via the pre-mutation copy and via a fresh
	// field read — both see 55: the copy is one level deep.
	if got.V0 != 55 || got.V1 != 55 {
		t.Fatalf("H3NestedField = (%d, %d), want (55, 55)", got.V0, got.V1)
	}
}

// H5: a returned `List` is the package var's own []int64 — a Go
// write through it mutates Gom's package state, and the exported
// `let` is an assignable package var on top of that. List
// parameters are not copied at this surface either — the caller's
// own backing is what Gom sees.
func TestSharedListFromGo(t *testing.T) {
	defer func() { boundary.Shared = []int64{1, 2, 3} }() // restore package state
	if got := boundary.SharedHead(struct{}{}); got != 1 {
		t.Fatalf("SharedHead() = %d, want 1", got)
	}
	got := boundary.SharedList(struct{}{})
	got[0] = 99 // writes into the package var's backing array
	if h := boundary.SharedHead(struct{}{}); h != 99 {
		t.Fatalf("after mutating the returned slice, SharedHead() = %d, want 99", h)
	}
	// The exported `let` is itself a writable Go package var — both
	// the elements and the binding are mutable from this side.
	boundary.Shared[1] = 42
	if boundary.SharedList(struct{}{})[1] != 42 {
		t.Fatalf("after writing boundary.Shared[1], SharedList()[1] = %d, want 42", boundary.SharedList(struct{}{})[1])
	}
	boundary.Shared = []int64{7} // wholesale rebinding — Gom reads it
	if h := boundary.SharedHead(struct{}{}); h != 7 {
		t.Fatalf("after rebinding boundary.Shared, SharedHead() = %d, want 7", h)
	}

	// A `List` parameter is not copied at this surface either —
	// Echo hands the caller's own backing back.
	in := []int64{1, 2, 3}
	out := boundary.Echo(in)
	in[0] = 9
	if out[0] != 9 {
		t.Fatalf("Echo: mutating the argument afterwards left out[0] = %d, want 9", out[0])
	}

	// A per-call literal shares nothing — the next call's list is
	// unaffected.
	f := boundary.FreshList(struct{}{})
	f[0] = 99
	if boundary.FreshList(struct{}{})[0] != 7 {
		t.Fatalf("FreshList()[0] = %d, want 7 — mutating a fresh result leaked", boundary.FreshList(struct{}{})[0])
	}
}

// H6: a record crosses as a Go struct value — field writes on this
// side stay local, but the `xs` payload still shares backing with
// the package value, so element writes reach Gom.
func TestPairFromGo(t *testing.T) {
	p := boundary.GetPair(struct{}{})
	p.N = 99
	if boundary.GetPair(struct{}{}).N != 7 {
		t.Fatalf("after p.N = 99, GetPair().N = %d, want 7 — scalar field is a copy", boundary.GetPair(struct{}{}).N)
	}
	p.Xs[0] = 99
	if boundary.GetPair(struct{}{}).Xs[0] != 99 {
		t.Fatalf("after p.Xs[0] = 99, GetPair().Xs[0] = %d, want 99 — payload shares backing", boundary.GetPair(struct{}{}).Xs[0])
	}
	if boundary.PairXsHead(struct{}{}) != 99 {
		t.Fatalf("PairXsHead() = %d, want 99 — Gom-side read sees the Go write", boundary.PairXsHead(struct{}{}))
	}
}

// H7: a variant crosses as a value struct behind the sealed
// interface — reassigning the payload struct or its scalar fields
// stays local, but the List payload's backing is shared exactly
// like the record's.
func TestWrapFromGo(t *testing.T) {
	w := boundary.GetWrap(struct{}{})
	wl, ok := w.(boundary.Wrap_WList)
	if !ok {
		t.Fatalf("GetWrap() = %T, want Wrap_WList", w)
	}
	wl.Xs[0] = 99 // same backing as wrapVal's payload
	if boundary.WrapXsHead(struct{}{}) != 99 {
		t.Fatalf("WrapXsHead() = %d, want 99 — Gom-side match sees the Go write", boundary.WrapXsHead(struct{}{}))
	}

	n, ok := boundary.GetNum(struct{}{}).(boundary.Wrap_WNum)
	if !ok {
		t.Fatalf("GetNum() = %T, want Wrap_WNum", boundary.GetNum(struct{}{}))
	}
	n.N = 99 // scalar payload copy — stays local
	if boundary.GetNum(struct{}{}).(boundary.Wrap_WNum).N != 8 {
		t.Fatalf("GetNum().N = %d, want 8 — scalar variant payload is a copy", boundary.GetNum(struct{}{}).(boundary.Wrap_WNum).N)
	}
}

// The `(error, error)` Ok-payload boundary (spec 8.3): the derived
// view `Result (Option GoError) GoError` arrives as
// `result.Result[option.Option[error], error]` — `(nil, nil)` is
// `Ok None`, a non-nil payload `Ok (Some e)`, a failure `Err e` —
// and a Go caller hands the same three shapes back through
// ErrpairKind.
func TestErrorPairViews(t *testing.T) {
	none := boundary.Errpair(0)
	if none.Tag() != result.ResultTagOk {
		t.Fatalf("Errpair(0).Tag() = %v, want Ok", none.Tag())
	}
	if _, ok := none.Value.(option.Option_None[error]); !ok {
		t.Fatalf("Errpair(0).Value = %T, want Option_None — (nil, nil) reads as Ok None", none.Value)
	}

	some := boundary.Errpair(1)
	sv, ok := some.Value.(option.Option_Some[error])
	if !ok || sv.V0 == nil || sv.V0.Error() != "soft" {
		t.Fatalf("Errpair(1).Value = %#v, want Option_Some(soft)", some.Value)
	}

	fatal := boundary.Errpair(-1)
	if fatal.Tag() != result.ResultTagErr || fatal.Error == nil || fatal.Error.Error() != "fatal" {
		t.Fatalf("Errpair(-1) = %#v, want Err(fatal)", fatal)
	}

	// Go → Gom: the same three shapes a Go caller constructs.
	if got := boundary.ErrpairKind(result.Ok[option.Option[error], error](
		option.Option_Some[error]{V0: errors.New("x")})); got != "some:x" {
		t.Fatalf("ErrpairKind(Ok Some x) = %q, want %q", got, "some:x")
	}
	if got := boundary.ErrpairKind(result.Ok[option.Option[error], error](
		option.Option_None[error]{})); got != "none" {
		t.Fatalf("ErrpairKind(Ok None) = %q, want %q", got, "none")
	}
	if got := boundary.ErrpairKind(result.Err[option.Option[error], error](
		errors.New("y"))); got != "err:y" {
		t.Fatalf("ErrpairKind(Err y) = %q, want %q", got, "err:y")
	}
}
