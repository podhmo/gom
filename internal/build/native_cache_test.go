package build_test

// These checks launch the real Go toolchain. Keep them in the build
// integration suite, rather than another concurrently scheduled package.

import (
	"fmt"
	"github.com/podhmo/gom/internal/testbin"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeNativeInput(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNativeBinaryCacheInvalidation(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 2.51)
	}

	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no Go toolchain")
	}
	if _, err := exec.LookPath("cp"); err != nil {
		t.Skip("cache adapter requires cp")
	}
	t.Setenv("GOM_TEST_BIN_CACHE", "on")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod -x")
	root := t.TempDir()
	dep := filepath.Join(root, "dependency")
	writeNativeInput(t, filepath.Join(dep, "go.mod"), "module example.test/dep\n\ngo 1.27\n")
	depFile := filepath.Join(dep, "dep.go")
	writeNativeInput(t, depFile, "package dep\nconst Value = \"one\"\n")
	mod := fmt.Sprintf("module example.test/main\n\ngo 1.27\nrequire example.test/dep v0.0.0\nreplace example.test/dep => %s\n", dep)
	const source = `package main
import ("fmt"; "os"; "example.test/dep")
var BuildMarker = "default"
func main() { fmt.Printf("%s:%s:%s\n", BuildMarker, dep.Value, os.Getenv("TESTBIN_VALUE")) }
`
	build := func(src string) testbin.Result {
		t.Helper()
		dir := t.TempDir()
		writeNativeInput(t, filepath.Join(dir, "go.mod"), mod)
		writeNativeInput(t, filepath.Join(dir, "main.go"), src)
		r, err := testbin.Build(t.Context(), goBin, dir, os.Environ())
		if err != nil {
			t.Fatalf("build: %v\n%s", err, r.Output)
		}
		return r
	}
	run := func(r testbin.Result, value, want string) {
		t.Helper()
		cmd := testbin.Command(r.Path)
		cmd.Env = append(os.Environ(), "TESTBIN_VALUE="+value)
		out, err := cmd.CombinedOutput()
		if err != nil || string(out) != want {
			t.Fatalf("execution=%q, %v; want %q", out, err, want)
		}
	}
	// Obtaining a binary must not run it. Otherwise these build outputs
	// would contain the program's output before the explicit executions.
	first := build(source)
	if !first.ReuseEnabled || strings.Contains(string(first.Output), "default:one:") {
		t.Fatalf("adapter ran the program: %s", first.Output)
	}
	run(first, "first", "default:one:first\n")
	second := build(source)
	run(second, "second", "default:one:second\n")
	trace := string(second.Output)
	if strings.Contains(trace, "/link -o ") || strings.Contains(trace, "/compile -o ") {
		t.Fatalf("unchanged program was compiled/linked again:\n%s", trace)
	}

	// Contents, rather than only mtime and size, must invalidate Go's key.
	info, err := os.Stat(depFile)
	if err != nil {
		t.Fatal(err)
	}
	writeNativeInput(t, depFile, "package dep\nconst Value = \"two\"\n")
	if err := os.Chtimes(depFile, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	run(build(source), "changed", "default:two:changed\n")
	run(build(strings.Replace(source, "default", "edited", 1)), "source", "edited:two:source\n")

	// Each caller gets its own copy; damaging one cannot poison later runs.
	if err := os.WriteFile(second.Path, []byte("damaged local copy"), 0o700); err != nil {
		t.Fatal(err)
	}
	run(build(source), "isolated", "default:two:isolated\n")

	// A new dependency file and a module replacement must also be seen.
	writeNativeInput(t, filepath.Join(dep, "extra.go"), "package dep\nfunc init(){ ValueFromInit = \"new\" }\nvar ValueFromInit string\n")
	withInit := strings.Replace(source, "dep.Value,", "dep.Value+dep.ValueFromInit,", 1)
	run(build(withInit), "added", "default:twonew:added\n")
	other := filepath.Join(root, "other")
	writeNativeInput(t, filepath.Join(other, "go.mod"), "module example.test/dep\n\ngo 1.27\n")
	writeNativeInput(t, filepath.Join(other, "dep.go"), "package dep\nconst Value = \"alt\"\n")
	mod = strings.ReplaceAll(mod, dep, other)
	run(build(source), "replace", "default:alt:replace\n")

	// Actual compiler/linker conditions still reach the Go driver.
	t.Setenv("GOFLAGS", "-mod=mod -ldflags=-X=main.BuildMarker=flagged")
	run(build(source), "flags", "flagged:alt:flags\n")
	t.Setenv("GOFLAGS", "-mod=mod -x")
	writeNativeInput(t, filepath.Join(other, "dep.go"), "package dep\nconst Value = unknown\n")
	dir := t.TempDir()
	writeNativeInput(t, filepath.Join(dir, "go.mod"), mod)
	writeNativeInput(t, filepath.Join(dir, "main.go"), source)
	if r, err := testbin.Build(t.Context(), goBin, dir, os.Environ()); err == nil {
		t.Fatalf("invalid dependency reused a binary: %+v", r)
	}
}

func TestNativeBinaryCacheFallback(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no Go toolchain")
	}
	t.Setenv("GOM_TEST_BIN_CACHE", "off")
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	writeNativeInput(t, filepath.Join(dir, "go.mod"), "module example.test/fallback\n\ngo 1.27\n")
	writeNativeInput(t, filepath.Join(dir, "main.go"), "package main\nfunc main(){panic(\"runs only later\")}\n")
	r, err := testbin.Build(t.Context(), goBin, dir, os.Environ())
	if err != nil || r.ReuseEnabled {
		t.Fatalf("fallback: %+v, %v", r, err)
	}
	out, err := testbin.Command(r.Path).CombinedOutput()
	if err == nil || !strings.Contains(string(out), "runs only later") {
		t.Fatalf("execution=%q %v", out, err)
	}
}
