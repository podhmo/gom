package codegen

// The compile smoke test: run the whole fixture through codegen,
// write the generated files into a fresh module alongside a stub
// `gostub` package, and let the real toolchain prove the output is
// buildable and testable — `go build ./...` plus `go test ./...`
// (the generated _test.go runs TestSmoke/TestResult for real).

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCompileSmoke generates the compile fixture, lays it out in a
// temp module, and runs the Go toolchain over it.
func TestCompileSmoke(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 2.43)
	}

	// runtime.GOROOT is deprecated since Go 1.24; the system path is
	// the documented way to locate the `go` binary.
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go toolchain on PATH: %v", err)
	}

	out, err := File(compileFixture(), fixtureSrc())
	if err != nil {
		t.Fatal(err)
	}

	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	write := func(rel, body string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("go.mod", "module genout\n\ngo 1.27\n\n"+
		"require github.com/podhmo/gom v0.0.0\n"+
		"require gostub v0.0.0\n\n"+
		"replace github.com/podhmo/gom => "+root+"\n"+
		"replace gostub => ./gostub\n")
	write("shapes.go", string(out.Source))
	write("shapes_test.go", string(out.TestSource))
	write("gostub/go.mod", "module gostub\n\ngo 1.27\n")
	write("gostub/gostub.go", `package gostub

// Signatures as the Gom fixture sees them: Int is int64, so the Go
// side reports int64 (a real binding inserts Conv nodes when the Go
// signature differs).

func Atoi(s string) (int64, error) {
	var v int64
	for _, c := range s {
		v = v*10 + int64(c-'0')
	}
	return v, nil
}

func Itoa(v int64) string { return "x" }

func Get(x int64) (int64, bool) { return x, x >= 0 }

func DivMod(x int64) (int64, bool) { return x, x != 0 }
`)

	for _, args := range [][]string{
		{"build", "./..."},
		{"vet", "./..."},
		{"test", "./..."},
	} {
		cmd := exec.Command(gobin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s failed: %v\n%s", strings.Join(args, " "), err, b)
		}
	}
}

// repoRoot locates the gom module root from this file's location —
// internal/codegen is two levels down.
func repoRoot() (string, error) {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		return "", os.ErrNotExist
	}
	return filepath.Abs(filepath.Join(filepath.Dir(self), "..", ".."))
}
