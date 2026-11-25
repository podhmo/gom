// Package testutil contains helpers emitted only into generated
// _test.go files. Keeping the go-cmp dependency here lets production
// builds import rt without loading test-only module metadata.
package testutil

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"
)

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
func Snapshot(t *testing.T, testName, key, value string) {
	t.Helper()
	path := filepath.Join(snapshotRoot(t), "__snapshot__", snapshotName(testName, key))
	if os.Getenv("GOM_TEST_UPDATE_SNAPSHOTS") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("gom: snapshot directory: %v", err)
		}
		if err := writeSnapshotAtomic(path, value); err != nil {
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

// snapshotRoot is captured when the test binary starts — go test
// runs it in the package source directory. A test may chdir later;
// snapshots nevertheless stay beside the generated test file.
var snapshotRootDir, snapshotRootErr = os.Getwd()

func snapshotRoot(t *testing.T) string {
	t.Helper()
	if snapshotRootErr != nil {
		t.Fatalf("gom: snapshot directory: %v", snapshotRootErr)
	}
	return snapshotRootDir
}

var renameSnapshotFile = os.Rename

// writeSnapshotAtomic rewrites a baseline through a sibling temp file
// so a failed update never truncates the previously good snapshot.
func writeSnapshotAtomic(path, value string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".snapshot-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if _, err := tmp.WriteString(value); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := renameSnapshotFile(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// truncateUTF8Bytes truncates s to at most max bytes without cutting
// through a multibyte rune. The byte count matters because filesystem
// name limits are byte limits, not character limits.
func truncateUTF8Bytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
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
	base = truncateUTF8Bytes(base, 80)
	base += "_" + hex.EncodeToString(sum[:])[:12]
	return base + ".snap"
}
