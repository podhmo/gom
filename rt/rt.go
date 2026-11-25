package rt

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"github.com/google/go-cmp/cmp"
)

// InvalidVariant returns the error used when a value of a Gom sum type
// does not hold any of its declared variants. Generated match
// expressions panic with this error rather than panicking with
// "unreachable", because Go callers can produce values that Gom code
// cannot: a nil interface, a zero struct, or a struct with a corrupted
// tag (docs/spec.md section 8.6).
func InvalidVariant(typeName string, v any) error {
	return fmt.Errorf("gom: invalid %s value %#v: not a declared variant", typeName, v)
}

// Todo is the body of the `todo` builtin: a hole that panics when
// reached. The type parameter lets it stand at any result type.
func Todo[T any]() T {
	panic("gom: todo")
}

// Nil is the body of `Go.nil ()` — the universal nil literal for any
// nil-able type (decision 2026-11-23-nil-spelling). A bare `nil`
// cannot be emitted at an arbitrary type, so the helper returns T's
// zero value, which is nil exactly for the nil-able types check
// admits.
func Nil[T any]() T {
	var z T
	return z
}

// BoxSlice copies a Go slice into a fresh []any, boxing each element —
// the boundary conversion a `..xs` spread into a `...any` variadic
// needs (spec section 8.1 item 4): Go itself cannot splice a []T
// into ...any. The fresh array means the Go callee's writes through
// the slice never reach the operand's backing array, per spec
// section 8.5's copy rule.
func BoxSlice[A any](xs []A) []any {
	out := make([]any, len(xs))
	for i, v := range xs {
		out[i] = v
	}
	return out
}

// Equal is the fallback recipe for Gom's structural `==` (spec
// section 3.3): generated code routes operand types it has no
// per-type comparator for — named Go types, Task, Go.Any — through
// here. Lists, tuples, records and ADTs use compiler-generated
// per-type comparators instead (docs/design.md section 4.9), which
// is what removes reflect.DeepEqual's identical-backing-store
// shortcut from user-visible equality. Equal therefore only has to
// answer for values whose Gom-level recipe does not exist, and its
// reflect.DeepEqual reading is the documented approximation for
// those — including DeepEqual's nil-vs-empty and pointer-shortcut
// quirks, which can still leak through Go-view types.
func Equal(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	// A nil slice is the empty list per the representation contract
	// (stdlib/list: "the zero value (nil) is the empty list"), and
	// eval's element-wise comparison agrees — but DeepEqual
	// distinguishes nil from non-nil-but-empty, so give the top
	// level a second answer for the Go-view slices that still route
	// here. Nested nil-vs-empty is generated code's job: the
	// element recipe inside a composite comparator is never Equal.
	return bothEmptySlices(a, b)
}

// bothEmptySlices reports whether a and b are two empty slices of
// the same type — the nil-vs-empty reading Equal adds on top of
// reflect.DeepEqual.
func bothEmptySlices(a, b any) bool {
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	if !av.IsValid() || !bv.IsValid() || av.Type() != bv.Type() {
		return false
	}
	return av.Kind() == reflect.Slice && av.Len() == 0 && bv.Len() == 0
}

// Assert is the body of the `assert cond` builtin used in test
// blocks.
func Assert(t *testing.T, cond bool) {
	t.Helper()
	if !cond {
		t.Error("gom: assertion failed")
	}
}

// AssertEq is the body of the `assert_eq actual expected` builtin
// used in test blocks. The eq argument is the already-computed
// verdict under the operands' own comparison recipe — the same
// plan `actual == expected` would evaluate (docs/design.md section
// 4.9) — so a failing assert_eq reports exactly what `==` would
// answer, including native comparisons' IEEE NaN verdict. actual and
// expected arrive already rendered to text by the compiler-generated
// display functions of docs/design.md section 4.8 (fmt's %v for
// Go-view values, spec section 11).
func AssertEq(t *testing.T, eq bool, actual, expected string, raw ...any) {
	t.Helper()
	if eq {
		return
	}
	msg := fmt.Sprintf("gom: assertion failed:\n  actual:   %s\n  expected: %s", actual, expected)
	if len(raw) >= 2 {
		if diff, ok := cmpDiff(raw[1], raw[0]); ok && diff != "" {
			msg += "\n  diff (-expected +actual):\n" + indent(diff, "    ")
		}
	}
	t.Error(msg)
}

// cmpDiff renders a go-cmp diff, falling back when a Go-view value
// carries unexported fields or another shape cmp cannot inspect.
func cmpDiff(expected, actual any) (diff string, ok bool) {
	defer func() {
		if recover() != nil {
			diff, ok = "", false
		}
	}()
	return cmp.Diff(expected, actual), true
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

// Snapshot is the body of the `snapshot value key` builtin used in
// test blocks. value arrives pre-rendered through the operand type's
// gomShow/display recipe. Snapshots live under __snapshot__ beside
// the generated test file; `gom test --update` rewrites missing or
// stale files through GOM_TEST_UPDATE_SNAPSHOTS=1.
func Snapshot(t *testing.T, key, value string) {
	t.Helper()
	// Go runs a package's test binary with that package's source
	// directory as cwd. Using cwd rather than runtime.Caller keeps
	// snapshots working when the binary is built with -trimpath.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("gom: snapshot directory: %v", err)
	}
	path := filepath.Join(dir, "__snapshot__", snapshotName(t.Name(), key))
	if os.Getenv("GOM_TEST_UPDATE_SNAPSHOTS") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("gom: snapshot directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			t.Fatalf("gom: snapshot write %s: %v", path, err)
		}
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("gom: snapshot %s is missing (run `gom test --update`): %v", path, err)
		return
	}
	if got := string(data); got != value {
		msg := fmt.Sprintf("gom: snapshot mismatch: %s\n  actual:   %s\n  expected: %s", path, value, got)
		if diff, ok := cmpDiff(got, value); ok && diff != "" {
			msg += "\n  diff (-expected +actual):\n" + indent(diff, "    ")
		}
		t.Error(msg)
	}
}

// snapshotName maps a test/key pair to a stable filesystem-safe
// name. Readable characters survive verbatim; anything else is
// folded. A short hash of the length-separated pair always suffixes
// the readable prefix, so distinct test/key pairs cannot collide.
func snapshotName(testName, key string) string {
	name := testName + "_" + key
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	base := b.String()
	if base == "" || base == "." || base == ".." {
		base = "snapshot"
	}
	sum := sha256.Sum256([]byte(testName + "\x00" + key))
	if len(base) > 80 {
		base = base[:80]
	}
	base += "_" + hex.EncodeToString(sum[:])[:12]
	return base + ".snap"
}

// ShowError renders a GoError value inside a generated display
// function — `error("text")`, the evaluator's VGoErr reading, with a
// nil guard for the Go-side zero value. A typed-nil error whose
// Error method panics is caught the way fmt handles it: `%!v`
// panic text instead of crashing the splice.
func ShowError(err error) (out string) {
	if err == nil {
		return "nil"
	}
	defer func() {
		if r := recover(); r != nil {
			out = fmt.Sprintf("%%!v(PANIC=Error method: %v)", r)
		}
	}()
	return fmt.Sprintf("error(%q)", err.Error())
}
