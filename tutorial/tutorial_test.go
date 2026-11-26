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
	"github.com/podhmo/gom/internal/testbin"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/goimport"
	"github.com/podhmo/gom/internal/gomimport"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/resolve"
	gomstd "github.com/podhmo/gom/internal/stdlib"
)

// Reading fixtures in this process registers them with Go's test cache.
// compileStep gives the test actual compiler dependencies and also checks
// the real CLI's pipeline wiring. Chapters share the repository's module;
// the scratch module each `go run` needs is synthesized, as in run.sh.
func compileStep(t *testing.T, root, src string, checkOnly bool) *build.Result {
	t.Helper()
	source, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "tutorial", filepath.Dir(src))
	gl := goimport.New(&goimport.Config{Dir: dir})
	// The CLI resolves `import "mod/path"` (Gom packages, including
	// the stdlib's `list`/`option`/`result`/`cmp` short names)
	// through gomimport's composite on top of the Go loader —
	// mirror it or a plain `import` reports E0203.
	mods := map[string]map[string]*resolve.Symbol{}
	ml := gomimport.New(&gomimport.Config{Dir: dir, Go: gl, Sigs: gl, Modules: mods})
	compiled, err := build.Compile(build.Input{Path: src, Src: source}, gomstd.Apply(&build.Options{
		Loader: ml, Sigs: ml, Modules: mods,
		Lower:          &lower.Env{GoSig: gl.GoSigOf, Types: ml, FuncArity: ml.ArityOf, FuncType: ml.FuncTypeOf, FuncNOfNamed: ml.FuncNOfNamed, GoUnderlyingSliceOrArrayPtr: gl.GoUnderlyingIsSliceOrArrayPtr, GoUnderlyingKind: gl.GoUnderlyingKind},
		SelfImportPath: ml.EntryPath(),
		CheckOnly:      checkOnly,
	}))
	if err != nil {
		t.Fatalf("compiler API: %v", err)
	}
	return compiled
}

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

// trackGoInputs registers the actual module inputs of a subprocess build
// with Go's test cache. Compiler dependencies alone omit packages imported
// only by generated Go (notably rt and stdlib).
func trackGoInputs(t *testing.T, gobin, dir string, env []string, pattern string) {
	t.Helper()
	// -f avoids the full -json package dump: emitting and decoding a
	// struct per dependency dominated this helper's runtime.
	const listFields = `{{.Dir}}|{{join .GoFiles " "}}|{{join .CgoFiles " "}}|{{join .SFiles " "}}|{{join .HFiles " "}}|{{join .SysoFiles " "}}|{{join .EmbedFiles " "}}|{{if .Module}}{{.Module.GoMod}}|{{.Module.Dir}}{{end}}`
	cmd := exec.Command(gobin, "list", "-deps", "-f", listFields, pattern)
	cmd.Dir = dir
	cmd.Env = env
	output, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			t.Fatalf("listing Go dependencies: %v\n%s", err, exit.Stderr)
		}
		t.Fatalf("listing Go dependencies: %v", err)
	}
	seen := map[string]bool{}
	read := func(path string, optional bool) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		if _, err := os.ReadFile(path); err != nil && !(optional && os.IsNotExist(err)) {
			t.Fatalf("reading Go build input %s: %v", path, err)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Split(line, "|")
		if len(fields) < 9 || fields[7] == "" {
			continue // Standard-library inputs are tied to the test's Go toolchain.
		}
		pkgDir := fields[0]
		// Track the directory as well as existing files, so adding a source
		// to an already-used package cannot leave a cached success intact.
		if _, err := os.ReadDir(pkgDir); err != nil {
			t.Fatal(err)
		}
		for _, files := range fields[1:7] {
			for _, file := range strings.Fields(files) {
				read(filepath.Join(pkgDir, file), false)
			}
		}
		read(fields[7], false)
		if fields[8] != "" {
			read(filepath.Join(fields[8], "go.sum"), true)
		}
	}
}

// buildGom compiles cmd/gom into dir and returns the binary path.
func buildGom(t *testing.T, gobin, root, dir string) string {
	t.Helper()
	trackGoInputs(t, gobin, root, nil, "./cmd/gom")
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
	compiled := compileStep(t, root, src, false)
	if compiled.Diagnostics.HasError() || compiled.Output == nil {
		t.Fatalf("compiler API failed: %+v", compiled.Diagnostics)
	}

	build := testbin.Command(gom, "build", "-o", mod+string(filepath.Separator), src)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("gom build: %v\n%s", err, out)
	}

	generated, err := os.ReadFile(filepath.Join(mod, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, compiled.Output.Source) {
		t.Fatal("gom build output differs from the compiler API for the same source")
	}
	// The generated file carries the whole program — the scratch module
	// only needs a go.mod pointing at the gom module (rt/stdlib) via
	// this worktree; run.sh writes the same manifest.
	gomod := "module tutorialrun/" + dir + "\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.0\n\nreplace github.com/podhmo/gom => " + root + "\n"
	if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(gobin, "run", ".")
	cmd.Dir = mod
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	trackGoInputs(t, gobin, mod, cmd.Env, ".")
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

// elapsedField normalizes the per-test `elapsed` of `gom test`'s
// JSONL report so a run can be diffed byte-for-byte (the field is a
// float64 — seconds — so fractions must match too).
var elapsedField = regexp.MustCompile(`"elapsed":[0-9]+(\.[0-9]+)?`)

// testStep runs `gom test` on the step directory and diffs the JSONL
// report against expected-test.txt — run for steps that declare test
// blocks, so the harness actually exercises them rather than only the
// main program. The step dir sits inside this repository's module, so
// the generated main.go/main_test.go (gitignored) compile there.
func testStep(t *testing.T, gom, root, dir string) {
	t.Helper()
	want, err := os.ReadFile(filepath.Join(dir, "expected-test.txt"))
	if err != nil {
		t.Fatal(err)
	}
	// Snapshots are read only by the child test binary, outside this
	// process. Read them here too so Go's test cache notices baseline edits.
	snapshotDir := filepath.Join(dir, "__snapshot__")
	if entries, err := os.ReadDir(snapshotDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".snap") {
				continue
			}
			if _, err := os.ReadFile(filepath.Join(snapshotDir, entry.Name())); err != nil {
				t.Fatal(err)
			}
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	// `gom test` writes the generated main.go/main_test.go in place —
	// overwriting even a handwritten one. Snapshot whatever was there
	// and restore it after the run, so the tree (and later
	// `go test ./...` package discovery) looks exactly as before.
	keep := map[string][]byte{}
	for _, gen := range []string{"main.go", "main_test.go"} {
		if data, err := os.ReadFile(filepath.Join(dir, gen)); err == nil {
			keep[gen] = data
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	// Spelled "tutorial/<step>" so the report's span/output paths match
	// expected-test.txt (run.sh passes the same form).
	cmd := testbin.Command(gom, "test", filepath.Join("tutorial", dir))
	cmd.Dir = root
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout
	runErr := cmd.Run()
	for _, gen := range []string{"main.go", "main_test.go"} {
		path := filepath.Join(dir, gen)
		if data, ok := keep[gen]; ok {
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
		} else if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if runErr != nil {
		t.Fatalf("gom test: %v\n%s", runErr, stdout.String())
	}
	got := elapsedField.ReplaceAllString(stdout.String(), `"elapsed":0`)
	if got != string(want) {
		t.Fatalf("gom test output mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// checkStep asserts `gom check` on dir's .gom files exits 1 and
// reports the diagnostic code stored in expected-check.txt.
func checkStep(t *testing.T, gom, root, dir string) {
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
		compiled := compileStep(t, root, src, true)
		found := false
		for _, diagnostic := range compiled.Diagnostics {
			if string(diagnostic.Code) == strings.TrimSpace(string(wantCode)) {
				found = true
			}
		}
		if !compiled.Diagnostics.HasError() || !found {
			t.Fatalf("compiler API: %s missing expected error %s: %+v", src, wantCode, compiled.Diagnostics)
		}
		cmd := testbin.Command(gom, "check", src)
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
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 16.36)
	}

	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}
	root := repoRoot(t)
	gom := buildGom(t, gobin, root, t.TempDir())

	for _, dir := range stepDirs(t) {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()
			if _, err := os.ReadFile(filepath.Join(dir, "main.gom")); err == nil {
				runStep(t, gom, gobin, root, dir)
				if _, err := os.Stat(filepath.Join(dir, "expected-test.txt")); err == nil {
					testStep(t, gom, root, dir)
				}
			} else {
				checkStep(t, gom, root, dir)
			}
		})
	}
}
