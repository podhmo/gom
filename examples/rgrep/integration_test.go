package rgrep_test

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/goimport"
	"github.com/podhmo/gom/internal/lower"
	gomstd "github.com/podhmo/gom/internal/stdlib"
	"github.com/podhmo/gom/internal/testbin"
)

// Embed non-Go input and the subprocess-built helper so Go test caching
// invalidates this integration suite when either changes. Direct compiler
// imports below likewise track compiler implementation changes.
//
//go:embed *.gom
var exampleSources embed.FS

// generateModule compiles every .gom file of the example as one package
// unit (as `gom build examples/rgrep` does), including the Gom `test`
// blocks, into a scratch module that resolves the runtime from this
// repository. Generated Go never lands in the source directory.
func generateModule(t *testing.T) (root, dir string) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fs.Glob(exampleSources, "*.gom")
	if err != nil {
		t.Fatal(err)
	}
	var inputs []build.Input
	for _, name := range entries {
		source, err := exampleSources.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, build.Input{Path: filepath.Join(root, "examples/rgrep", name), Src: source})
	}
	loader := goimport.New(&goimport.Config{Dir: root})
	results, err := build.CompileFiles(inputs, gomstd.Apply(&build.Options{
		Loader: loader, Sigs: loader, Lower: &lower.Env{GoSig: loader.GoSigOf, Tests: true},
	}))
	if err != nil {
		t.Fatalf("compile Gom: %v", err)
	}
	dir = t.TempDir()
	gomod := "module rgrep\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.0\n\nreplace github.com/podhmo/gom => " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	for i, compiled := range results {
		if compiled.Diagnostics.HasError() || compiled.Output == nil {
			t.Fatalf("compile Gom %s: %+v", entries[i], compiled.Diagnostics)
		}
		base := strings.TrimSuffix(entries[i], ".gom")
		if err := os.WriteFile(filepath.Join(dir, base+".go"), compiled.Output.Source, 0o644); err != nil {
			t.Fatal(err)
		}
		if compiled.Output.TestSource != nil {
			if err := os.WriteFile(filepath.Join(dir, base+"_test.go"), compiled.Output.TestSource, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root, dir
}

func goCommand(dir string, args ...string) *exec.Cmd {
	command := exec.Command("go", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	return command
}

// Compile the actual Gom source and then the generated Go, once for this suite.
// The generated program runs in subprocesses against a temp directory tree.
func buildCLI(t *testing.T) string {
	t.Helper()
	_, dir := generateModule(t)
	if out, err := goCommand(dir, "vet", ".").CombinedOutput(); err != nil {
		t.Fatalf("vet generated CLI: %v\n%s", err, out)
	}
	cli := filepath.Join(dir, "rgrep")
	if out, err := goCommand(dir, "build", "-o", cli, ".").CombinedOutput(); err != nil {
		t.Fatalf("build generated CLI: %v\n%s", err, out)
	}
	return cli
}

// TestGomTests runs the `test` blocks written next to the pure parts of
// the CLI (argument grammar, line matching) plus the channel helpers
// (`cancel`, `is_done`, `send_item`, `wait_workers`).
func TestGomTests(t *testing.T) {
	if testing.Short() {
		t.Skip("long test: builds a scratch module")
	}
	_, dir := generateModule(t)
	if out, err := goCommand(dir, "test", ".").CombinedOutput(); err != nil {
		t.Fatalf("Gom tests: %v\n%s", err, out)
	}
}

func execute(t *testing.T, cli string, args ...string) (string, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := testbin.CommandContext(ctx, cli, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("execute CLI: %v", err)
		}
		code = exit.ExitCode()
	}
	if ctx.Err() != nil {
		t.Fatalf("CLI timed out: %s", stderr.String())
	}
	return stdout.String(), stderr.String(), code
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	file := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

// matches arrive in nondeterministic order across the worker pool, so
// every comparison sorts the output lines.
func sortedLines(t *testing.T, out string) []string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	sort.Strings(lines)
	return lines
}

func TestRgrep(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 2.60)
	}

	cli := buildCLI(t)
	dir := t.TempDir()

	t.Run("recursive_match_with_line_numbers", func(t *testing.T) {
		tree := filepath.Join(dir, "tree1")
		a := writeFile(t, tree, "a.txt", "hello\nfoo line\nbar\n")
		c := writeFile(t, tree, "sub/deep/c.txt", "baz foo\nlast\n")
		out, stderr, code := execute(t, cli, "foo", tree)
		want := []string{a + ":2:foo line", c + ":1:baz foo"}
		if code != 0 || stderr != "" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, out, stderr)
		}
		if got := sortedLines(t, out); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("got %v want %v", got, want)
		}
	})

	t.Run("regex_and_ignore_case", func(t *testing.T) {
		tree := filepath.Join(dir, "tree2")
		writeFile(t, tree, "b.txt", "nothing\nFOO here\n")
		out, _, code := execute(t, cli, "fo+", tree)
		if code != 1 || out != "" {
			t.Fatalf("case-sensitive: code=%d stdout=%q", code, out)
		}
		out, _, code = execute(t, cli, "-i", "fo+", tree)
		if code != 0 || !strings.Contains(out, ":2:FOO here") {
			t.Fatalf("ignore-case: code=%d stdout=%q", code, out)
		}
	})

	t.Run("no_match_exits_one", func(t *testing.T) {
		a := writeFile(t, dir, "plain.txt", "text\n")
		out, stderr, code := execute(t, cli, "zzz", a)
		if code != 1 || out != "" || stderr != "" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, out, stderr)
		}
	})

	t.Run("file_argument_directly", func(t *testing.T) {
		a := writeFile(t, dir, "single.txt", "x\nneedle\n")
		out, _, code := execute(t, cli, "needle", a)
		if code != 0 || !strings.Contains(out, a+":2:needle") {
			t.Fatalf("code=%d stdout=%q", code, out)
		}
	})

	t.Run("binary_and_missing_paths_warn_and_exit_two", func(t *testing.T) {
		tree := filepath.Join(dir, "tree3")
		bin := writeFile(t, tree, "blob.bin", "bin\xff\x00ary\n")
		missing := filepath.Join(tree, "nope")
		out, stderr, code := execute(t, cli, "x", tree, missing)
		if code != 2 {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, out, stderr)
		}
		if !strings.Contains(stderr, bin+": binary file skipped") ||
			!strings.Contains(stderr, missing+": stat") {
			t.Fatalf("stderr=%q", stderr)
		}
	})

	t.Run("bad_pattern_exits_two", func(t *testing.T) {
		out, stderr, code := execute(t, cli, "(", dir)
		if code != 2 || out != "" || !strings.Contains(stderr, "invalid pattern") {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, out, stderr)
		}
	})

	t.Run("max_count_cuts_output_and_exits_promptly", func(t *testing.T) {
		tree := filepath.Join(dir, "tree4")
		for i := 0; i < 50; i++ {
			writeFile(t, tree, fmt.Sprintf("d%d/f%d.txt", i, i), "hit\n")
		}
		start := time.Now()
		out, stderr, code := execute(t, cli, "--concurrency", "8", "--max-count", "3", "hit", tree)
		if code != 0 || stderr != "" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, out, stderr)
		}
		if got := len(sortedLines(t, out)); got != 3 {
			t.Fatalf("got %d lines, want 3", got)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("cancellation did not bound the run: %s", elapsed)
		}
	})

	t.Run("sigint_stops_cleanly_or_kills_a_wedged_worker", func(t *testing.T) {
		tree := filepath.Join(dir, "tree5")
		if err := os.MkdirAll(tree, 0o755); err != nil {
			t.Fatal(err)
		}
		fifo := filepath.Join(tree, "wait.fifo")
		if err := syscall.Mkfifo(fifo, 0o644); err != nil {
			t.Skipf("mkfifo: %v", err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		cmd := testbin.CommandContext(ctx, cli, "x", tree)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		waited := make(chan error, 1)
		go func() { waited <- cmd.Wait() }()

		time.Sleep(400 * time.Millisecond)
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatalf("first SIGINT: %v", err)
		}
		// The matcher is (almost surely) wedged in ReadFile on the FIFO,
		// which a channel cancel cannot reach; the run stays alive. It can
		// also have exited cleanly if `done` landed before the read — that
		// is the graceful path and must report exit 130.
		select {
		case err := <-waited:
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 130 {
				t.Fatalf("clean exit wanted 130, got %v; stderr=%q", err, stderr.String())
			}
		case <-time.After(2 * time.Second):
			// Wedged path: `signal.Reset` re-armed the default action, so a
			// second SIGINT terminates the process by signal (exit code -1).
			if err := cmd.Process.Signal(os.Interrupt); err != nil {
				t.Fatalf("second SIGINT: %v", err)
			}
			select {
			case err := <-waited:
				exit, ok := err.(*exec.ExitError)
				if !ok || exit.ExitCode() != -1 {
					t.Fatalf("wanted signal termination (-1), got %v; stderr=%q", err, stderr.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("CLI did not die after second SIGINT; stderr=%q", stderr.String())
			}
		}
	})

	t.Run("argument_errors", func(t *testing.T) {
		cases := []struct {
			name string
			args []string
			want string
		}{
			{"no_pattern", nil, "usage:"},
			{"bad_concurrency", []string{"--concurrency", "x", "p"}, "--concurrency expects an integer"},
			{"zero_max_count", []string{"--max-count", "0", "p"}, "--max-count must be >= 1"},
			{"missing_max_count", []string{"--max-count"}, "--max-count requires a value"},
			{"unknown_flag", []string{"--nope", "p"}, "unknown option"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				out, stderr, code := execute(t, cli, tc.args...)
				if code != 2 || out != "" || !strings.Contains(stderr, tc.want) {
					t.Fatalf("code=%d stdout=%q stderr=%q; want %q", code, out, stderr, tc.want)
				}
			})
		}
	})

	t.Run("help", func(t *testing.T) {
		out, stderr, code := execute(t, cli, "--help")
		if code != 0 || !strings.HasPrefix(out, "usage:") || stderr != "" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, out, stderr)
		}
	})
}
