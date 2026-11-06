package gomtest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// RunGoldenTests runs the generated-code golden suite in dir against
// generate, one subtest per case ("" selects the repo's
// testdata/golden).
//
// The expected output lives in <name>.golden.go next to the input
// <name>.gom — one golden per generated file: a flat case's
// test-mode _test.go half pins to <name>_test.golden.go, and a
// directory case <name>/ pins each member output the same way,
// <name>/<base>.golden.go for <base>.go and
// <name>/<base>_test.golden.go for its test half. Comparison is a
// literal string match after normalizing CRLF to LF and ignoring any
// difference in trailing newlines — generated code comes from
// go/printer, so whitespace is already canonical and no fuzzier
// normalization is needed.
//
// With -update the golden files are rewritten from the actual output
// (and created if missing); the written files always end with a
// newline so they stay gofmt-clean.
func RunGoldenTests(t *testing.T, dir string, generate GenerateFunc) {
	t.Helper()
	if generate == nil {
		t.Fatal("gomtest: nil GenerateFunc")
	}
	suiteDir := resolveSuiteDir(t, dir, "golden")
	runCases(t, suiteDir, "golden", func(t *testing.T, c testCase) {
		t.Helper()
		hasGom := false
		for _, f := range c.files {
			if strings.HasSuffix(f.Path, gomExt) {
				hasGom = true
				break
			}
		}
		if !hasGom {
			t.Fatalf("gomtest: case %s has no .gom member to generate code from "+
				"(.gomi members produce no output)", c.name)
		}
		got, err := generate(c.files, CaseOptions{Tests: c.tests})
		if err != nil {
			t.Fatalf("generate failed: %v", err)
		}
		if len(got) == 0 {
			t.Fatalf("gomtest: generate returned no files for case %s", c.name)
		}
		// A flat case's outputs are its <name>.go plus — under the
		// manifest's "tests" option — the <name>_test.go half;
		// more outputs belong to directory cases.
		if max := flatMaxOutputs(c.tests); !c.isDir && len(got) > max {
			t.Fatalf("gomtest: case %s generated %d files, want at most %d "+
				"(more outputs are for directory cases)", c.name, len(got), max)
		}
		claimed := make(map[string]bool, len(got))
		// Two outputs may not claim one golden — under -update the
		// later would silently overwrite the earlier, so the check
		// precedes compareGolden and fails in both modes. The map
		// is per-case: a tests-mode case x and an unrelated case
		// x_test can legitimately hold x_test.golden.go-shaped
		// expectations on the same path, and the collision is
		// caught only by the contents disagreeing at compare time.
		seen := make(map[string]string, len(got))
		for _, o := range got {
			goldenPath := goldenFor(t, c, o)
			if prev, dup := seen[goldenPath]; dup {
				t.Fatalf("gomtest: generated files %q and %q both map to golden %s",
					prev, o.Path, goldenPath)
			}
			seen[goldenPath] = o.Path
			if c.isDir {
				claimed[filepath.Base(goldenPath)] = true
			}
			compareGolden(t, goldenPath, o)
		}
		if c.isDir {
			reportStrayGoldens(t, c, claimed)
		}
		if !c.isDir {
			// Any flat golden the outputs did not claim is stale —
			// the generated set shrank under it (the test-mode half
			// disappeared, or the case stopped emitting its .go).
			// Flag it like a directory case's stray golden; -update
			// does not remove it either way.
			base := strings.TrimSuffix(c.files[0].AbsPath(), gomExt)
			strays := []string{base + goldenExt}
			if c.tests {
				strays = append(strays, base+"_test"+goldenExt)
			}
			for _, stray := range strays {
				if _, err := os.Stat(stray); err == nil && seen[stray] == "" {
					t.Errorf("gomtest: golden file %s has no generated output", stray)
				}
			}
		}
	})
}

// flatMaxOutputs is the number of generated files a flat case may
// produce: its <name>.go always, plus <name>_test.go when the
// manifest's "tests" option compiles the case in `gom test` mode.
func flatMaxOutputs(tests bool) int {
	if tests {
		return 2
	}
	return 1
}

// goldenFor maps a case's generated output to its golden file: the
// same suite-relative path with .golden.go substituted for .go. A
// flat case's outputs must be <name>.go — or <name>_test.go under
// the manifest's "tests" option — the goldens then land on
// <name>.golden.go and <name>_test.golden.go exactly as the flat
// convention has them. A directory case's output must sit directly
// inside the case directory, so a generated file can never map a
// golden outside its own case.
func goldenFor(t *testing.T, c testCase, o GeneratedFile) string {
	t.Helper()
	base, ok := strings.CutSuffix(o.Path, ".go")
	if !ok {
		t.Fatalf("gomtest: generated file %q is not a .go file", o.Path)
	}
	if c.isDir {
		if path.Dir(o.Path) != c.name {
			t.Fatalf("gomtest: generated file %q is not directly inside directory case %s",
				o.Path, c.name)
		}
	} else {
		want := strings.TrimSuffix(c.files[0].Path, gomExt)
		if o.Path != want+".go" {
			if o.Path == want+"_test.go" {
				if c.tests {
					return filepath.Join(c.files[0].Dir, filepath.FromSlash(base+".golden.go"))
				}
				t.Fatalf("gomtest: generated file %q is a test-mode output "+
					"but case %s does not set \"tests\": true in its manifest",
					o.Path, c.name)
			}
			t.Fatalf("gomtest: generated file %q does not name case %s (want %q)",
				o.Path, c.name, want+".go")
		}
	}
	return filepath.Join(c.files[0].Dir, filepath.FromSlash(base+".golden.go"))
}

// compareGolden checks one generated file against its golden,
// honoring -update. Missing goldens explain the -update path rather
// than diffing against nothing.
func compareGolden(t *testing.T, goldenPath string, o GeneratedFile) {
	t.Helper()
	if *update {
		writeGolden(t, goldenPath, o.Src)
		return
	}
	want, err := os.ReadFile(goldenPath)
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("gomtest: missing golden file %s (run `go test -update`)", goldenPath)
	}
	if err != nil {
		t.Fatalf("gomtest: cannot read %s: %v", goldenPath, err)
	}
	if !goldenEqual(want, o.Src) {
		t.Errorf("golden mismatch for %s:\n%s",
			o.Path, simpleDiff(normalizeGolden(want), normalizeGolden(o.Src)))
	}
}

// reportStrayGoldens flags *.golden.go files inside a directory case
// that no generated output claimed — the suite-level orphan check
// cannot see inside case directories, so the runner checks the
// correspondence itself.
func reportStrayGoldens(t *testing.T, c testCase, claimed map[string]bool) {
	t.Helper()
	entries, err := os.ReadDir(c.absDir)
	if err != nil {
		t.Fatalf("gomtest: cannot read %s: %v", c.absDir, err)
	}
	for _, e := range entries {
		// ignored() names are the suite's helper convention — a
		// _- or .-prefixed file is not an expectation, so it can
		// never be stray.
		if e.IsDir() || ignored(e.Name()) || !strings.HasSuffix(e.Name(), goldenExt) {
			continue
		}
		if !claimed[e.Name()] {
			t.Errorf("gomtest: golden file %s in directory case %s has no generated output",
				e.Name(), c.name)
		}
	}
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
