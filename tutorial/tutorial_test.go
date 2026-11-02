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
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/goimport"
	"github.com/podhmo/gom/internal/lower"
)

// Reading fixtures in this process registers them with Go's test cache.
// compileStep gives the test actual compiler dependencies and also checks
// the real CLI's pipeline wiring. Each chapter is a separate module, so
// Embedding cannot cross into a fixture directory in another Go module.
func compileStep(t *testing.T, root, src string, checkOnly bool) *build.Result {
	t.Helper()
	source, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	loader := goimport.New(&goimport.Config{Dir: filepath.Join(root, "tutorial", filepath.Dir(src))})
	compiled, err := build.Compile(build.Input{Path: src, Src: source}, &build.Options{
		Loader: loader, Sigs: loader, Lower: &lower.Env{GoSig: loader.GoSigOf}, CheckOnly: checkOnly,
	})
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
	cmd := exec.Command(gobin, "list", "-deps", "-json", pattern)
	cmd.Dir = dir
	cmd.Env = env
	output, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			t.Fatalf("listing Go dependencies: %v\n%s", err, exit.Stderr)
		}
		t.Fatalf("listing Go dependencies: %v", err)
	}
	type module struct {
		Dir, GoMod string
		Replace    *module
	}
	type pkg struct {
		Dir                                                      string
		GoFiles, CgoFiles, SFiles, HFiles, SysoFiles, EmbedFiles []string
		Module                                                   *module
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
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var dependency pkg
		if err := decoder.Decode(&dependency); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("decoding Go dependencies: %v", err)
		}
		if dependency.Module == nil {
			continue // Standard-library inputs are tied to the test's Go toolchain.
		}
		// Track the directory as well as existing files, so adding a source
		// to an already-used package cannot leave a cached success intact.
		if _, err := os.ReadDir(dependency.Dir); err != nil {
			t.Fatal(err)
		}
		for _, files := range [][]string{dependency.GoFiles, dependency.CgoFiles, dependency.SFiles, dependency.HFiles, dependency.SysoFiles, dependency.EmbedFiles} {
			for _, file := range files {
				read(filepath.Join(dependency.Dir, file), false)
			}
		}
		for manifest := dependency.Module; manifest != nil; manifest = manifest.Replace {
			read(manifest.GoMod, false)
			if manifest.Dir != "" {
				read(filepath.Join(manifest.Dir, "go.sum"), true)
			}
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

	build := exec.Command(gom, "build", "-o", mod+string(filepath.Separator), src)
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
	module, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	gomod := strings.ReplaceAll(string(module), "=> ../..", "=> "+root)
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
			if _, err := os.ReadFile(filepath.Join(dir, "main.gom")); err == nil {
				runStep(t, gom, gobin, root, dir)
			} else {
				checkStep(t, gom, root, dir)
			}
		})
	}
}
