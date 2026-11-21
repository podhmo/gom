package linkcheck_test

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
// unit (as `gom build examples/linkcheck` does), including the Gom `test`
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
		inputs = append(inputs, build.Input{Path: filepath.Join(root, "examples/linkcheck", name), Src: source})
	}
	loader := goimport.New(&goimport.Config{Dir: root})
	results, err := build.CompileFiles(inputs, gomstd.Apply(&build.Options{
		Loader: loader, Sigs: loader, Lower: &lower.Env{GoSig: loader.GoSigOf, Tests: true},
	}))
	if err != nil {
		t.Fatalf("compile Gom: %v", err)
	}
	dir = t.TempDir()
	gomod := "module linkcheck\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.0\n\nreplace github.com/podhmo/gom => " + root + "\n"
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
// The generated program runs in subprocesses against httptest only.
func buildCLI(t *testing.T) string {
	t.Helper()
	_, dir := generateModule(t)
	if out, err := goCommand(dir, "vet", ".").CombinedOutput(); err != nil {
		t.Fatalf("vet generated CLI: %v\n%s", err, out)
	}
	cli := filepath.Join(dir, "linkcheck")
	if out, err := goCommand(dir, "build", "-o", cli, ".").CombinedOutput(); err != nil {
		t.Fatalf("build generated CLI: %v\n%s", err, out)
	}
	return cli
}

// TestGomTests runs the `test` blocks written next to the pure parts of
// the CLI (argument grammar, URL extraction, ordering, report shaping).
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

// recorder counts requests per path and observes the HTTP method; each
// path can be given a fixed status and an artificial delay.
type recorder struct {
	mu                    sync.Mutex
	counts                map[string]int
	inflight, maxInflight atomic.Int32
	delay                 time.Duration
}

func (r *recorder) handler(status int) http.HandlerFunc {
	if r.counts == nil {
		r.counts = map[string]int{}
	}
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodHead {
			r.mu.Lock()
			r.counts["METHOD:"+req.Method+req.URL.Path]++
			r.mu.Unlock()
		}
		cur := r.inflight.Add(1)
		for {
			max := r.maxInflight.Load()
			if cur <= max || r.maxInflight.CompareAndSwap(max, cur) {
				break
			}
		}
		defer r.inflight.Add(-1)
		if r.delay > 0 {
			time.Sleep(r.delay)
		}
		r.mu.Lock()
		r.counts[req.URL.Path]++
		r.mu.Unlock()
		w.WriteHeader(status)
	}
}

func (r *recorder) count(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[path]
}

func TestLinkcheck(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 2.60)
	}

	cli := buildCLI(t)
	dir := t.TempDir()

	t.Run("failures_reported_in_input_order_with_refs", func(t *testing.T) {
		rec := &recorder{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			switch req.URL.Path {
			case "/dead":
				rec.handler(404)(w, req)
			case "/slow":
				rec.handler(200)(w, req)
			default:
				rec.handler(200)(w, req)
			}
		}))
		defer server.Close()
		a := writeFile(t, dir, "a.md", "see "+server.URL+"/dead and "+server.URL+"/alive")
		b := writeFile(t, dir, "b.md", "again "+server.URL+"/dead")
		out, stderr, code := execute(t, cli, "--all", a, b)
		want := "FAIL  404  " + server.URL + "/dead  [" + a + ", " + b + "]\n" +
			"ok    200  " + server.URL + "/alive  [" + a + "]\n"
		if code != 1 || out != want {
			t.Fatalf("code=%d stdout=%q stderr=%q; want %q", code, out, stderr, want)
		}
		if !strings.Contains(stderr, "2 urls checked: 1 ok, 1 broken, 0 unreachable, 0 timed out") {
			t.Fatalf("stderr=%q", stderr)
		}
		if got := rec.count("/dead"); got != 1 {
			t.Fatalf("/dead fetched %d times, want deduped to 1", got)
		}
	})

	t.Run("all_ok_exits_zero_and_stays_quiet", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}))
		defer server.Close()
		a := writeFile(t, dir, "ok.md", server.URL+"/a "+server.URL+"/b")
		out, stderr, code := execute(t, cli, a)
		if code != 0 || out != "" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, out, stderr)
		}
		if !strings.Contains(stderr, "2 urls checked: 2 ok, 0 broken, 0 unreachable, 0 timed out") {
			t.Fatalf("stderr=%q", stderr)
		}
	})

	t.Run("head_method_is_used", func(t *testing.T) {
		rec := &recorder{}
		server := httptest.NewServer(rec.handler(200))
		defer server.Close()
		a := writeFile(t, dir, "head.md", server.URL+"/x")
		if _, _, code := execute(t, cli, a); code != 0 {
			t.Fatalf("code=%d", code)
		}
		if got := rec.count("METHOD:GET/x"); got != 0 {
			t.Fatalf("%d GET requests; only HEAD expected", got)
		}
	})

	t.Run("transport_failure", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		endpoint := server.URL
		server.Close()
		a := writeFile(t, dir, "down.md", endpoint+"/gone")
		out, stderr, code := execute(t, cli, a)
		if code != 1 || !strings.Contains(out, "FAIL") || !strings.Contains(out, "connection refused") {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, out, stderr)
		}
		if !strings.Contains(stderr, "0 ok, 0 broken, 1 unreachable") {
			t.Fatalf("stderr=%q", stderr)
		}
	})

	t.Run("global_deadline_marks_the_rest_as_timed_out", func(t *testing.T) {
		rec := &recorder{delay: 500 * time.Millisecond}
		server := httptest.NewServer(rec.handler(200))
		defer server.Close()
		a := writeFile(t, dir, "slow.md", server.URL+"/one "+server.URL+"/two")
		out, stderr, code := execute(t, cli, "--timeout", "1ms", a)
		if code != 1 || strings.Count(out, "FAIL  timed out") != 2 {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, out, stderr)
		}
		if !strings.Contains(stderr, "2 timed out") {
			t.Fatalf("stderr=%q", stderr)
		}
	})

	t.Run("concurrency_is_bounded_by_the_semaphore", func(t *testing.T) {
		rec := &recorder{delay: 100 * time.Millisecond}
		server := httptest.NewServer(rec.handler(200))
		defer server.Close()
		var b strings.Builder
		for i := 0; i < 6; i++ {
			fmt.Fprintf(&b, "%s/u%d\n", server.URL, i)
		}
		a := writeFile(t, dir, "many.md", b.String())
		out, stderr, code := execute(t, cli, "--concurrency", "2", a)
		if code != 0 {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, out, stderr)
		}
		if got := rec.maxInflight.Load(); got > 2 {
			t.Fatalf("max inflight %d, want <= 2", got)
		}
	})

	t.Run("argument_errors", func(t *testing.T) {
		a := writeFile(t, dir, "one.md", "text")
		cases := []struct {
			name string
			args []string
			want string
		}{
			{"no_files", nil, "usage:"},
			{"bad_concurrency", []string{"--concurrency", "x", a}, "--concurrency expects an integer"},
			{"zero_concurrency", []string{"--concurrency", "0", a}, "--concurrency must be >= 1"},
			{"bad_timeout", []string{"--timeout", "later", a}, "--timeout"},
			{"unknown_flag", []string{"--nope", a}, "unknown option"},
			{"missing_file", []string{filepath.Join(dir, "missing.md")}, "no such file"},
			{"no_urls", []string{a}, "no URLs found"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				out, stderr, code := execute(t, cli, tc.args...)
				if code != 1 || out != "" || !strings.Contains(stderr, tc.want) {
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
