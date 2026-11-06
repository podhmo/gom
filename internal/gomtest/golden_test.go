package gomtest

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoldenEqual(t *testing.T) {
	for _, tt := range []struct {
		name string
		want string
		got  string
		eq   bool
	}{
		{"identical", "package x\n", "package x\n", true},
		{"trailing newline ignored", "package x\n", "package x", true},
		{"extra trailing blank lines ignored", "package x\n\n\n", "package x\n", true},
		{"crlf normalized", "package x\r\n", "package x\n", true},
		{"content differs", "package x\n", "package y\n", false},
		{"inner whitespace differs", "func  f() {}\n", "func f() {}\n", false},
		{"empty both", "", "", true},
		{"empty vs newline", "", "\n", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if eq := goldenEqual([]byte(tt.want), []byte(tt.got)); eq != tt.eq {
				t.Errorf("goldenEqual(%q, %q) = %v, want %v", tt.want, tt.got, eq, tt.eq)
			}
		})
	}
}

func TestSimpleDiff(t *testing.T) {
	want := "package main\n\nfunc main() {\n\tprintln(\"a\")\n}\n"
	got := "package main\n\nfunc main() {\n\tprintln(\"b\")\n}\n"
	d := simpleDiff(want, got)
	for _, part := range []string{
		"--- expected", "+++ actual", "@@",
		`  func main() {`, `- 	println("a")`, `+ 	println("b")`,
	} {
		if !strings.Contains(d, part) {
			t.Errorf("diff missing %q:\n%s", part, d)
		}
	}
}

func TestWriteGolden(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.golden.go")

	// The written file always ends with exactly one newline so it
	// stays gofmt-clean.
	writeGolden(t, path, []byte("package x\n\n\n"))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "package x\n" {
		t.Errorf("written = %q, want %q", data, "package x\\n")
	}
}

// TestGoldenUpdate exercises -update end to end: a missing golden file
// is created from the actual output and a subsequent run compares
// clean.
func TestGoldenUpdate(t *testing.T) {
	old := *update
	*update = true
	defer func() { *update = old }()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "g.gom"), []byte("package g\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gen := func(fs []File, _ CaseOptions) ([]GeneratedFile, error) {
		return []GeneratedFile{{Path: "g.go", Src: []byte("package main\n\nfunc g() {}\n")}}, nil
	}
	RunGoldenTests(t, dir, gen)
	data, err := os.ReadFile(filepath.Join(dir, "g.golden.go"))
	if err != nil {
		t.Fatalf("-update did not write the golden file: %v", err)
	}
	if string(data) != "package main\n\nfunc g() {}\n" {
		t.Errorf("golden = %q", data)
	}
}

// TestGoldenFlatTests exercises the flat tests:true convention: a
// case's test-mode _test.go half pins to <name>_test.golden.go next
// to <name>.golden.go, both written by -update.
func TestGoldenFlatTests(t *testing.T) {
	old := *update
	*update = true
	defer func() { *update = old }()

	dir := t.TempDir()
	for name, src := range map[string]string{
		"x.gom":       "package x\n",
		"x.case.json": `{"tests": true}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cases, orphans, err := discoverCases(dir)
	if err != nil {
		t.Fatalf("discoverCases: %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("orphans = %v, want none (x_test.golden.go pairs with the tests case)", orphans)
	}
	if len(cases) != 1 || !cases[0].tests {
		t.Fatalf("cases = %+v, want the single tests case", cases)
	}
	gen := func(fs []File, _ CaseOptions) ([]GeneratedFile, error) {
		return []GeneratedFile{
			{Path: "x.go", Src: []byte("package x\n")},
			{Path: "x_test.go", Src: []byte("package x\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n")},
		}, nil
	}
	RunGoldenTests(t, dir, gen)
	for _, name := range []string{"x.golden.go", "x_test.golden.go"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("-update did not write %s: %v", name, err)
		}
	}
}

// TestGoldenFlatTestsOrphan pins the other half of the pairing rule:
// a <name>_test.golden.go whose case does not set "tests": true is a
// stray expectation and reports as an orphan fixture.
func TestGoldenFlatTestsOrphan(t *testing.T) {
	for _, tt := range []struct {
		name       string
		manifest   string
		wantOrphan bool
	}{
		{"tests true pairs", `{"tests": true}`, false},
		{"tests false orphans", `{"tests": false}`, true},
		{"no manifest orphans", "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, src := range map[string]string{
				"x.gom":            "package x\n",
				"x_test.golden.go": "package x\n",
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tt.manifest != "" {
				if err := os.WriteFile(filepath.Join(dir, "x.case.json"), []byte(tt.manifest), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			_, orphans, err := discoverCases(dir)
			if err != nil {
				t.Fatalf("discoverCases: %v", err)
			}
			if tt.wantOrphan {
				if len(orphans) != 1 || orphans[0] != "x_test.golden.go" {
					t.Errorf("orphans = %v, want [x_test.golden.go]", orphans)
				}
			} else if len(orphans) != 0 {
				t.Errorf("orphans = %v, want none", orphans)
			}
		})
	}
}

// TestGoldenUpdateDirCase exercises -update on a directory case: the
// member goldens are created inside the case directory as
// <member>.golden.go.
func TestGoldenUpdateDirCase(t *testing.T) {
	old := *update
	*update = true
	defer func() { *update = old }()

	dir := t.TempDir()
	pkg := filepath.Join(dir, "pkg")
	if err := os.Mkdir(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]string{
		"a.gom":     "package pkg\n",
		"b.gom":     "package pkg\n",
		"case.json": "{}",
	} {
		if err := os.WriteFile(filepath.Join(pkg, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gen := func(fs []File, _ CaseOptions) ([]GeneratedFile, error) {
		var out []GeneratedFile
		for _, f := range fs {
			out = append(out, GeneratedFile{
				Path: strings.TrimSuffix(f.Path, ".gom") + ".go",
				Src:  []byte("package pkg\n"),
			})
		}
		return out, nil
	}
	RunGoldenTests(t, dir, gen)
	for _, name := range []string{"a.golden.go", "b.golden.go"} {
		data, err := os.ReadFile(filepath.Join(pkg, name))
		if err != nil {
			t.Fatalf("-update did not write %s: %v", name, err)
		}
		if string(data) != "package pkg\n" {
			t.Errorf("%s = %q", name, data)
		}
	}
}

// TestGoldenFatalPaths drives the golden harness's failure paths an
// in-process test cannot observe: t.Fatalf in a case aborts the
// case's goroutine and a stray-golden t.Errorf only marks the
// subtest failed — the parent sees a boolean at best, never the
// message. Each scenario runs in a helper process
// (TestGoldenFatalHelperProcess) whose non-zero exit and test
// output carry the failure, and the parent asserts on both.
func TestGoldenFatalPaths(t *testing.T) {
	for _, tt := range []struct {
		scenario string
		want     string
	}{
		{"stray-dir-golden", "has no generated output"},
		{"stale-flat-test-golden", "has no generated output"},
		{"stale-flat-golden", "x.golden.go has no generated output"},
		{"dup-golden", "both map to golden"},
		{"flat-too-many-outputs", "generated 2 files, want at most 1"},
		{"flat-test-output-without-tests", `does not set "tests": true`},
	} {
		t.Run(tt.scenario, func(t *testing.T) {
			cmd := exec.Command(os.Args[0],
				"-test.run=^TestGoldenFatalHelperProcess$", "-test.v")
			cmd.Env = append(os.Environ(), "GOMTEST_GOLDEN_HELPER="+tt.scenario)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("helper process succeeded; output:\n%s", out)
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("helper exited unexpectedly (want test-failure exit 1): %v\n%s",
					err, out)
			}
			if !strings.Contains(string(out), tt.want) {
				t.Errorf("helper output does not contain %q:\n%s", tt.want, out)
			}
		})
	}
}

// TestGoldenFatalHelperProcess runs one golden-suite failure
// scenario in-process so its parent TestGoldenFatalPaths can assert
// on the child test binary's exit status and output. The parent
// selects the scenario through GOMTEST_GOLDEN_HELPER; without it
// the test skips — it is never a standalone check.
func TestGoldenFatalHelperProcess(t *testing.T) {
	scenario := os.Getenv("GOMTEST_GOLDEN_HELPER")
	if scenario == "" {
		t.Skip("helper process for TestGoldenFatalPaths")
	}
	dir := t.TempDir()
	write := func(rel, src string) {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	src := "package x\n"
	gen := func(outputs ...string) GenerateFunc {
		return func(fs []File, _ CaseOptions) ([]GeneratedFile, error) {
			var out []GeneratedFile
			for _, p := range outputs {
				out = append(out, GeneratedFile{Path: p, Src: []byte(src)})
			}
			return out, nil
		}
	}
	switch scenario {
	case "stray-dir-golden":
		// A golden inside the case directory no output claims.
		write("d/case.json", "{}")
		write("d/a.gom", src)
		write("d/a.golden.go", src)
		write("d/stray.golden.go", src)
		RunGoldenTests(t, dir, gen("d/a.go"))
	case "stale-flat-test-golden":
		// tests is set and x_test.golden.go exists, but the case
		// no longer produces its test half — stale golden.
		write("x.gom", src)
		write("x.case.json", `{"tests": true}`)
		write("x.golden.go", src)
		write("x_test.golden.go", src)
		RunGoldenTests(t, dir, gen("x.go"))
	case "stale-flat-golden":
		// The mirror image: tests is set, both goldens exist, and
		// the case now emits only the test half — x.golden.go goes
		// unclaimed and reports stale.
		write("x.gom", src)
		write("x.case.json", `{"tests": true}`)
		write("x.golden.go", src)
		write("x_test.golden.go", src)
		RunGoldenTests(t, dir, gen("x_test.go"))
	case "dup-golden":
		// Two outputs mapping to one golden — under -update the
		// second would silently overwrite the first.
		write("d/case.json", "{}")
		write("d/a.gom", src)
		write("d/a.golden.go", src)
		RunGoldenTests(t, dir, gen("d/a.go", "d/a.go"))
	case "flat-too-many-outputs":
		// A flat case without tests produces more than its .go.
		write("x.gom", src)
		write("x.golden.go", src)
		RunGoldenTests(t, dir, gen("x.go", "other.go"))
	case "flat-test-output-without-tests":
		// A _test.go output whose manifest never opted into
		// test mode is a generator error, not a golden name —
		// the count check only lets a lone output reach it.
		write("x.gom", src)
		write("x.golden.go", src)
		RunGoldenTests(t, dir, gen("x_test.go"))
	default:
		t.Fatalf("unknown scenario %q", scenario)
	}
}
