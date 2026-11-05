package goimport

// `extern go` binding tests — the declared signature is checked
// against the real Go signature the loaded package carries, and a
// mismatch reports E0509 at the entry (spec section 8.4).

import (
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
)

// compileDiags carries a source through resolve and check and
// returns every diagnostic — the cases here are all errors the
// binding reports.
func compileDiags(t *testing.T, l *Loader, src string) diag.List {
	t.Helper()
	sf, d := syntax.Parse([]byte(src), "main.gom")
	var out diag.List
	out = append(out, d...)
	res, d := resolve.File(sf, &resolve.Config{Loader: l})
	out = append(out, d...)
	_, d = check.Check(res, l)
	out = append(out, d...)
	return out
}

func wantDiag(t *testing.T, ds diag.List, code string, sub string) diag.Diagnostic {
	t.Helper()
	for _, d := range ds {
		if d.Code == diag.Code(code) {
			if sub != "" && !strings.Contains(d.Message, sub) {
				t.Fatalf("%s message %q does not contain %q", code, d.Message, sub)
			}
			return d
		}
	}
	t.Fatalf("missing %s; diags: %v", code, ds)
	return diag.Diagnostic{}
}

// The P3-5 regression: inside `extern go "flag"`, `String` must mean
// the builtin type — not flag.String the three-parameter function —
// and `Ptr` must not inherit a Go member's arity. Neither the bogus
// "takes 3 type argument(s)" (E0401) nor "expected Ptr String"
// (E0402) may fire.
func TestExternFlagLookupSignature(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "flag"

extern go "flag" {
  fn Lookup (name: String) -> Option (Ptr Flag)
}
`)
	for _, d := range ds {
		if d.Severity == diag.SeverityError {
			t.Fatalf("unexpected diagnostic %s: %s", d.Code, d.Message)
		}
	}
}

// An extern entry naming no exported member of the package reports
// E0509 at the entry.
func TestExternMissingMember(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "flag"

extern go "flag" {
  fn Nope (name: String) -> Option (Ptr Flag)
}
`)
	wantDiag(t, ds, "E0509", "names no exported member")
}

// An extern `fn` cannot override a non-function member — `flag.Flag`
// is a type, not a callable.
func TestExternNonFunctionMember(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "flag"

extern go "flag" {
  fn Flag (name: String) -> Unit
}
`)
	wantDiag(t, ds, "E0509", "is a type, not a function")
}

// Parameter count must agree with the Go signature.
func TestExternParamCountMismatch(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "flag"

extern go "flag" {
  fn Lookup (name: String) (extra: Int) -> Option (Ptr Flag)
}
`)
	wantDiag(t, ds, "E0509", "declares 2 parameter(s)")
}

// A declared parameter type must be view-compatible with the real
// Go parameter's — `String` cannot stand for flag.Lookup's string
// only because int and string cannot convert either.
func TestExternParamTypeMismatch(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "flag"

extern go "flag" {
  fn Lookup (name: Int) -> Option (Ptr Flag)
}
`)
	wantDiag(t, ds, "E0509", "declares parameter 0 as Int")
}

// `Option` for a single non-pointer result has no presence channel —
// `strconv.Itoa` returns `string`, never absent.
func TestExternOptionOverPlainResult(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "strconv"

extern go "strconv" {
  fn Itoa (i: Int) -> Option String
}
`)
	wantDiag(t, ds, "E0509", "Option needs a (T, bool), *T, or error result")
}

// `Result` for a comma-ok result is a lie the other way — `bool` is
// not `error`.
func TestExternResultOverCommaOK(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "os"

extern go "os" {
  fn LookupEnv (key: String) -> Result String GoError
}
`)
	wantDiag(t, ds, "E0509", "Option T` or `(T, Bool)")
}

// The `(error, error)` pair admits the three payload readings:
// the honest `Result (Option GoError) GoError` (the derived
// view's own shape) and the never-nil claim `Result GoError
// GoError` — a `(nil, nil)` return then panics at the boundary.
func TestExternErrorPairViews(t *testing.T) {
	l := loaderFor(t)
	for _, decl := range []string{
		"fn TwoErrs (n: Int) -> Result (Option GoError) GoError",
		"fn TwoErrs (n: Int) -> Result GoError GoError",
	} {
		ds := compileDiags(t, l, `package main

import go "github.com/podhmo/gom/testdata/goiface"

extern go "github.com/podhmo/gom/testdata/goiface" {
  `+decl+`
}
`)
		for _, d := range ds {
			if d.Severity == diag.SeverityError {
				t.Fatalf("%s: unexpected diagnostic %s: %s", decl, d.Code, d.Message)
			}
		}
	}
}

// `(*T, error)` admits `Result (Option (Ptr T)) GoError` — the
// Ok-side analogue of `Option` over a lone `*T`: the payload keeps
// the pointer, nil reads as `Ok None`.
func TestExternPtrPairOptionView(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "github.com/podhmo/gom/testdata/goiface"

extern go "github.com/podhmo/gom/testdata/goiface" {
  fn SoftPtr (n: Int) -> Result (Option (Ptr Int)) GoError
}
`)
	for _, d := range ds {
		if d.Severity == diag.SeverityError {
			t.Fatalf("unexpected diagnostic %s: %s", d.Code, d.Message)
		}
	}
}

// `Result (Option T) GoError` needs a nil-able first result —
// `int` has no presence channel to read, so the declaration
// mismatches `(int, error)` exactly like any other shape drift.
func TestExternOptionPairOverPlainResult(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "strconv"

extern go "strconv" {
  fn Atoi (s: String) -> Result (Option Int) GoError
}
`)
	wantDiag(t, ds, "E0509", "declare `Result T GoError`")
}

// The payload inside the Option keeps the raw view — `Option
// String` over `(error, error)` claims a different payload type.
func TestExternOptionPairWrongPayload(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "github.com/podhmo/gom/testdata/goiface"

extern go "github.com/podhmo/gom/testdata/goiface" {
  fn TwoErrs (n: Int) -> Result (Option String) GoError
}
`)
	wantDiag(t, ds, "E0509", "declare `Result T GoError` or `Result (Option T) GoError`")
}

// An `(error, error)` pair stays a Result — the second `error` is
// the failure channel, so the raw `(GoError, GoError)` tuple is
// not an admitted view.
func TestExternErrorPairTupleRejected(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "github.com/podhmo/gom/testdata/goiface"

extern go "github.com/podhmo/gom/testdata/goiface" {
  fn TwoErrs (n: Int) -> (GoError, GoError)
}
`)
	wantDiag(t, ds, "E0509", "declare `Result T GoError` or `Result (Option T) GoError`")
}

// A `(T, error)` result keeps its required `Result T GoError` shape —
// declaring `Option` for it is rejected.
func TestExternOptionOverError(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "strconv"

extern go "strconv" {
  fn Atoi (s: String) -> Option Int
}
`)
	wantDiag(t, ds, "E0509", "Result T GoError")
}

// A declared type variable would claim the Go member is polymorphic —
// it is not, so the bind rejects it.
func TestExternPolymorphicDecl(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "flag"

extern go "flag" {
  fn Lookup (name: a) -> Option (Ptr Flag)
}
`)
	wantDiag(t, ds, "E0509", "must be monomorphic")
}

// An Option payload that drops the pointer lies about what the call
// binds — `*flag.Flag` cannot become `Option Flag` (the payload is
// the pointer itself: `Option (Ptr Flag)`).
func TestExternOptionPayloadNotPointer(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "flag"

extern go "flag" {
  fn Lookup (name: String) -> Option Flag
}
`)
	wantDiag(t, ds, "E0509", "Option (Ptr T)")
}

// The zero-argument spelling: `fn F ()` declares a Go function
// taking no parameters — `()` is the Unit pattern, not a counted
// parameter — and a sole `(u: Unit)` parameter spells it too.
// Both bind `os.Getpid` cleanly (no E0509, and no E0407 for the
// `()` pattern's once-dangling meta).
func TestExternZeroArgUnitPattern(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "os"

extern go "os" {
  fn Getpid () -> Int
}
`)
	for _, d := range ds {
		if d.Severity == diag.SeverityError {
			t.Fatalf("unexpected diagnostic %s: %s", d.Code, d.Message)
		}
	}
}

func TestExternZeroArgUnitParam(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "os"

extern go "os" {
  fn Getpid (u: Unit) -> Int
}
`)
	for _, d := range ds {
		if d.Severity == diag.SeverityError {
			t.Fatalf("unexpected diagnostic %s: %s", d.Code, d.Message)
		}
	}
}

// The `()` spelling is not a wildcard — over a Go function that
// takes a real parameter it still mismatches (`strconv.Atoi` takes
// a string the Unit parameter cannot stand for).
func TestExternZeroArgOverNonNullary(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "strconv"

extern go "strconv" {
  fn Atoi () -> Result Int GoError
}
`)
	wantDiag(t, ds, "E0509", "declares parameter 0 as Unit")
}

// The bound signature drops the Unit parameter — the call shape is
// the derived view's: `os.Getpid ()` emits `os.Getpid()`.
func TestExternZeroArgBoundShape(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "os"
import go "fmt"

extern go "os" {
  fn Getpid () -> Int
}

fn main () -> Unit = {
  let _ = fmt.Println(os.Getpid () > 0)
  ()
}
`)
	for _, d := range ds {
		if d.Severity == diag.SeverityError {
			t.Fatalf("unexpected diagnostic %s: %s", d.Code, d.Message)
		}
	}
}

// A misdeclared extern keeps the *derived* member shape at call
// sites: the E0509 stands at the entry and the call falls back to
// the declared signature — no cascade of confusing arity errors.
func TestExternFallbackKeepsDeclaredSig(t *testing.T) {
	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "flag"
import go "fmt"

extern go "flag" {
  fn Lookup (name: Int) -> Option (Ptr Flag)
}

fn main () -> Unit = {
  let _ = flag.Lookup(1)
  ()
}
`)
	wantDiag(t, ds, "E0509", "declares parameter 0 as Int")
}
