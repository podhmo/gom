package tutorial_test

// The executable half of tutorial/: every step directory that holds
// main.gom + expected.txt is compiled with the real `gom` binary and
// run through the real Go toolchain; directories with
// expected-check.txt hold intentionally-broken programs whose
// `gom check` must report the given diagnostic code. run.sh is the
// same driver for people who prefer a shell script.
//
// The test is skipped without a `go` toolchain on PATH (the same
// guard the cmd/gom end-to-end tests use).
import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// repoRoot is the repository root; tutorial_test.go lives one level
// below it.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// buildGom compiles cmd/gom into dir and returns the binary path.
func buildGom(t *testing.T, gobin, root, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "gom")
	cmd := exec.Command(gobin, "build", "-o", bin, "./cmd/gom")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building gom: %v\n%s", err, out)
	}
	return bin
}

// stepDirs returns the sorted tutorial step directories (dirs that
// contain at least one .gom file).
func stepDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(e.Name(), "*.gom"))
		if err != nil || len(matches) == 0 {
			continue
		}
		dirs = append(dirs, e.Name())
	}
	sort.Strings(dirs)
	if len(dirs) == 0 {
		t.Fatal("no tutorial steps found")
	}
	return dirs
}

// runStep builds dir's main.gom into a scratch module that replaces
// github.com/podhmo/gom with the worktree, runs it with `go run`, and
// compares stdout to expected.txt — the same flow run.sh performs.
func runStep(t *testing.T, gom, gobin, root, dir string) {
	t.Helper()
	mod := t.TempDir()
	src := filepath.Join(dir, "main.gom")

	build := exec.Command(gom, "build", "-o", mod+string(filepath.Separator), src)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("gom build: %v\n%s", err, out)
	}

	gomod := "module tutorialrun/" + dir + "\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.0\n\nreplace github.com/podhmo/gom => " + root + "\n"
	if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(gobin, "run", ".")
	cmd.Dir = mod
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go run: %v\n%s", err, stderr.String())
	}

	want, err := os.ReadFile(filepath.Join(dir, "expected.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got, w := stdout.String(), string(want); got != w {
		t.Fatalf("output mismatch\n--- got ---\n%s\n--- want ---\n%s", got, w)
	}
}

// checkStep asserts `gom check` on dir's .gom files exits 1 and
// reports the diagnostic code stored in expected-check.txt.
func checkStep(t *testing.T, gom, dir string) {
	t.Helper()
	wantCode, err := os.ReadFile(filepath.Join(dir, "expected-check.txt"))
	if err != nil {
		t.Fatal(err)
	}
	needle := `"code":"` + strings.TrimSpace(string(wantCode)) + `"`

	matches, err := filepath.Glob(filepath.Join(dir, "*.gom"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("%s: no .gom files", dir)
	}
	for _, src := range matches {
		cmd := exec.Command(gom, "check", src)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("%s: gom check unexpectedly succeeded\n%s", src, out)
		}
		if !bytes.Contains(out, []byte(needle)) {
			t.Fatalf("%s: output missing %s\n%s", src, needle, out)
		}
	}
}

func TestTutorial(t *testing.T) {
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}
	root := repoRoot(t)
	gom := buildGom(t, gobin, root, t.TempDir())

	for _, dir := range stepDirs(t) {
		dir := dir
		t.Run(dir, func(t *testing.T) {
			if _, err := os.Stat(filepath.Join(dir, "main.gom")); err == nil {
				runStep(t, gom, gobin, root, dir)
			} else {
				checkStep(t, gom, dir)
			}
		})
	}
}
