package main

import (
	"path/filepath"
	"strings"
	"testing"
)

const runMainGom = `package main

import go "fmt"
import go "os"

fn main () -> Unit = {
  let _ = fmt.Println "argc" (List.length (Go.conv os.Args : List String))
  os.Exit (if List.length (Go.conv os.Args : List String) > 2 { 7 } else { 0 })
  ()
}
`

// TestRunExecutes: `gom run` compiles, links and execs the package —
// program output reaches stdout unadorned, args after "--" reach
// argv, and the child's exit code is the command's.
func TestRunExecutes(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: go build + exec, skipping in short mode")
	}
	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{"m.gom": runMainGom})
	testModule(t, dir)

	code, out, _ := invoke("run", filepath.Join(dir, "m.gom"), "--", "a", "b")
	if code != 7 {
		t.Fatalf("exit = %d, want 7 (the program's own)", code)
	}
	if !strings.Contains(out, "argc 3") {
		t.Fatalf("stdout = %q, want program output (binary + a + b = argc 3)", out)
	}
}

// TestRunCompileFailure: a .gom that fails checking reports the
// usual message stream and does not run.
func TestRunCompileFailure(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{
		"m.gom": "package main\n\nfn main () -> Unit = oops\n",
	})
	testModule(t, dir)

	code, out, _ := invoke("run", filepath.Join(dir, "m.gom"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	var sawDiag, sawSummary bool
	for _, l := range jsonLines(t, out) {
		if l["kind"] == "diagnostic" {
			sawDiag = true
		}
		if l["kind"] == "summary" {
			sawSummary = true
			if l["ok"] != false {
				t.Fatalf("summary ok = %v", l["ok"])
			}
		}
	}
	if !sawDiag || !sawSummary {
		t.Fatalf("stream missing diagnostic/summary: %q", out)
	}
}

// TestRunNotMain: a package that is not `package main` is refused
// before the link step — `go run` has the same restriction.
func TestRunNotMain(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: gom info + compile, skipping in short mode")
	}
	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{
		"lib/f.gom": "package lib\n\nfn f () -> Int = 1\n",
	})
	testModule(t, dir)
	// Non-main packages carry a .gomi contract — draft it so the
	// compile itself is clean and the refusal is the main check.
	if code, out, _ := invoke("info", filepath.Join(dir, "lib", "f.gom")); code != 0 {
		t.Fatalf("gom info exit = %d: %q", code, out)
	}

	code, _, errOut := invoke("run", filepath.Join(dir, "lib", "f.gom"))
	if code != 2 {
		t.Fatalf("exit = %d, want 2; err=%q", code, errOut)
	}
	if !strings.Contains(errOut, "not a package main") {
		t.Fatalf("stderr = %q", errOut)
	}
}

// TestRunMultipleDirs: inputs spanning several directories name
// several packages — refused like `go run` refusing ./a ./b.
func TestRunMultipleDirs(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{
		"a/m.gom": "package main\n\nfn main () -> Unit = ()\n",
		"b/m.gom": "package main\n\nfn main () -> Unit = ()\n",
	})

	code, _, errOut := invoke("run", filepath.Join(dir, "a"), filepath.Join(dir, "b"))
	if code != 2 {
		t.Fatalf("exit = %d, want 2; err=%q", code, errOut)
	}
	if !strings.Contains(errOut, "single package directory") {
		t.Fatalf("stderr = %q", errOut)
	}
}
