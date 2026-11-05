package gomtest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
)

// RunGoldenTests runs the generated-code golden suite in dir against
// generate, one subtest per case ("" selects the repo's
// testdata/golden).
//
// The expected output lives in <name>.golden.go next to the input
// <name>.gom. Comparison is a literal string match after normalizing
// CRLF to LF and ignoring any difference in trailing newlines —
// generated code comes from go/printer, so whitespace is already
// canonical and no fuzzier normalization is needed.
//
// With -update the golden file is rewritten from the actual output
// (and created if missing); the written file always ends with a
// newline so it stays gofmt-clean.
func RunGoldenTests(t *testing.T, dir string, generate GenerateFunc) {
	t.Helper()
	if generate == nil {
		t.Fatal("gomtest: nil GenerateFunc")
	}
	suiteDir := resolveSuiteDir(t, dir, "golden")
	runCases(t, suiteDir, "golden", func(t *testing.T, c testCase) {
		t.Helper()
		if c.isDir {
			t.Fatalf("gomtest: directory cases are not supported in the golden suite (%s)", c.name)
		}
		got, err := generate(c.files[0])
		if err != nil {
			t.Fatalf("generate failed: %v", err)
		}
		goldenPath := c.sidePath(goldenExt)

		if *update {
			writeGolden(t, goldenPath, got)
			return
		}

		want, err := os.ReadFile(goldenPath)
		if errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("gomtest: missing golden file %s (run `go test -update`)", goldenPath)
		}
		if err != nil {
			t.Fatalf("gomtest: cannot read %s: %v", goldenPath, err)
		}
		if !goldenEqual(want, got) {
			t.Errorf("golden mismatch for %s:\n%s",
				c.files[0].Path, simpleDiff(normalizeGolden(want), normalizeGolden(got)))
		}
	})
}

// normalizeGolden converts CRLF to LF and strips trailing newlines so
// comparison ignores line-ending style and trailing-blank-line count.
func normalizeGolden(b []byte) string {
	return strings.TrimRight(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
}

// goldenEqual reports whether two generated sources are equal after
// normalization.
func goldenEqual(want, got []byte) bool {
	return normalizeGolden(want) == normalizeGolden(got)
}

// writeGolden rewrites a golden file with the actual output, ensuring
// exactly one trailing newline so the file stays gofmt-clean.
func writeGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	out := strings.TrimRight(string(got), "\n") + "\n"
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatalf("gomtest: cannot write %s: %v", path, err)
	}
	t.Logf("updated %s", path)
}

// simpleDiff renders the difference between two texts as a minimal
// unified-style hunk: the common line prefix and suffix are folded,
// and the differing middle is printed with "-" (want) / "+" (got)
// markers and a few lines of context. It is intentionally simple —
// golden files are compiler output reviewed by humans, so locating the
// differing region matters more than a minimal edit script.
func simpleDiff(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")

	pre := 0
	for pre < len(wl) && pre < len(gl) && wl[pre] == gl[pre] {
		pre++
	}
	suf := 0
	for suf < len(wl)-pre && suf < len(gl)-pre && wl[len(wl)-1-suf] == gl[len(gl)-1-suf] {
		suf++
	}

	const context = 3
	var b strings.Builder
	fmt.Fprintf(&b, "--- expected\n+++ actual\n@@ lines %d-%d of %d / %d-%d of %d @@\n",
		pre+1, len(wl)-suf, len(wl), pre+1, len(gl)-suf, len(gl))

	lead := max(pre-context, 0)
	if lead > 0 {
		fmt.Fprintf(&b, " ...\n")
	}
	for i := lead; i < pre; i++ {
		fmt.Fprintf(&b, "  %s\n", wl[i])
	}
	for _, l := range wl[pre : len(wl)-suf] {
		fmt.Fprintf(&b, "- %s\n", l)
	}
	for _, l := range gl[pre : len(gl)-suf] {
		fmt.Fprintf(&b, "+ %s\n", l)
	}
	trail := len(wl) - suf
	for i := trail; i < trail+context && i < len(wl); i++ {
		fmt.Fprintf(&b, "  %s\n", wl[i])
	}
	if trail+context < len(wl) {
		fmt.Fprintf(&b, " ...\n")
	}
	return b.String()
}
