package gistcli_test

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"github.com/podhmo/gom/internal/testbin"
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
)

// Embed non-Go input and the subprocess-built helper so Go test caching
// invalidates this integration suite when either changes. Direct compiler
// imports below likewise track compiler implementation changes.
//
//go:embed *.gom
var exampleSources embed.FS

const resultURL = "https://gist.github.com/octocat/deadbeef"
const fakeToken = "fake-not-a-real-token"

// generateModule compiles every .gom file of the example as one package
// unit (as `gom build examples/gist-cli` does), including the Gom `test`
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
		inputs = append(inputs, build.Input{Path: filepath.Join(root, "examples/gist-cli", name), Src: source})
	}
	loader := goimport.New(&goimport.Config{Dir: root})
	results, err := build.CompileFiles(inputs, gomstd.Apply(&build.Options{
		Loader: loader, Sigs: loader, Lower: &lower.Env{GoSig: loader.GoSigOf, Tests: true},
	}))
	if err != nil {
		t.Fatalf("compile Gom: %v", err)
	}
	dir = t.TempDir()
	gomod := "module gistcli\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.0\n\nreplace github.com/podhmo/gom => " + root + "\n"
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
	cli := filepath.Join(dir, "gist-cli")
	if out, err := goCommand(dir, "build", "-o", cli, ".").CombinedOutput(); err != nil {
		t.Fatalf("build generated CLI: %v\n%s", err, out)
	}
	return cli
}

// TestGomTests runs the `test` blocks written next to the pure parts of the
// CLI (argument grammar, request building, response decoding).
func TestGomTests(t *testing.T) {
	if testing.Short() {
		t.Skip("long test: builds a scratch module")
	}
	_, dir := generateModule(t)
	if out, err := goCommand(dir, "test", ".").CombinedOutput(); err != nil {
		t.Fatalf("Gom tests: %v\n%s", err, out)
	}
}

func execute(t *testing.T, cli, endpoint, token string, args ...string) (string, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	args = append([]string{"--api-url", endpoint}, args...)
	cmd := testbin.CommandContext(ctx, cli, args...)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "GITHUB_TOKEN=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "GITHUB_TOKEN="+token)
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

func TestGistCLI(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 2.60)
	}

	cli := buildCLI(t)
	dir := t.TempDir()
	first := writeFile(t, dir, "one/a.txt", "a quote: \"; slash: \\; tab:\t; 日本語\n")
	second := writeFile(t, dir, "two/b.txt", "")
	third := writeFile(t, dir, "three/quoted\"名前.txt", "escaped filename\n")
	files := map[string]string{"a.txt": "a quote: \"; slash: \\; tab:\t; 日本語\n", "b.txt": "", "quoted\"名前.txt": "escaped filename\n"}
	t.Run("create_then_update_preserves_other_files", func(t *testing.T) {
		var mu sync.Mutex
		stored := map[string]string{"untouched.txt": "leave me alone"}
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			wantMethod, wantPath := http.MethodPost, "/gists"
			if calls == 2 {
				wantMethod, wantPath = http.MethodPatch, "/gists/deadbeef"
			}
			if r.Method != wantMethod || r.URL.Path != wantPath {
				t.Errorf("request %s %s, want %s %s", r.Method, r.URL.Path, wantMethod, wantPath)
			}
			for key, want := range map[string]string{"Authorization": "Bearer " + fakeToken, "Accept": "application/vnd.github+json", "Content-Type": "application/json", "X-GitHub-Api-Version": "2026-03-10"} {
				if got := r.Header.Get(key); got != want {
					t.Errorf("header %s=%q, want %q", key, got, want)
				}
			}
			var body struct {
				Files map[string]struct {
					Content string `json:"content"`
				} `json:"files"`
				Public *bool `json:"public"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode: %v", err)
			}
			if calls == 1 && (body.Public == nil || *body.Public) {
				t.Error("create must explicitly default to secret")
			}
			if calls == 2 && body.Public != nil {
				t.Error("update must not change visibility")
			}
			expectedFiles := files
			if calls == 2 {
				expectedFiles = map[string]string{"a.txt": "updated\n"}
			}
			if len(body.Files) != len(expectedFiles) {
				t.Errorf("files=%v", body.Files)
			}
			for name, want := range expectedFiles {
				if got, ok := body.Files[name]; !ok || got.Content != want {
					t.Errorf("file %q=%q, want %q", name, got.Content, want)
				}
			}
			for name, file := range body.Files {
				stored[name] = file.Content
			}
			w.Header().Set("Content-Type", "application/json")
			status := http.StatusCreated
			if calls == 2 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			fmt.Fprintf(w, `{"html_url":%q}`, resultURL)
		}))
		defer server.Close()
		out, stderr, code := execute(t, cli, server.URL+"/", fakeToken, first, second, third)
		if code != 0 || out != resultURL+"\n" || stderr != "" {
			t.Fatalf("create code=%d stdout=%q stderr=%q", code, out, stderr)
		}
		writeFile(t, dir, "one/a.txt", "updated\n")
		out, stderr, code = execute(t, cli, server.URL, fakeToken, "--gist", strings.TrimSpace(out), first)
		if code != 0 || out != resultURL+"\n" || stderr != "" {
			t.Fatalf("update code=%d stdout=%q stderr=%q", code, out, stderr)
		}
		mu.Lock()
		defer mu.Unlock()
		if calls != 2 || stored["untouched.txt"] != "leave me alone" || stored["b.txt"] != "" || stored["a.txt"] != "updated\n" {
			t.Fatalf("stored state: calls=%d files=%v", calls, stored)
		}
	})
	t.Run("URL_without_owner_and_trailing_slash", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "PATCH" || r.URL.Path != "/gists/deadbeef" {
				t.Errorf("%s %s", r.Method, r.URL.Path)
			}
			fmt.Fprintf(w, `{"html_url":%q}`, resultURL)
		}))
		defer server.Close()
		out, stderr, code := execute(t, cli, server.URL, fakeToken, "--gist", "https://gist.github.com/deadbeef/", first)
		if code != 0 || out != resultURL+"\n" || stderr != "" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, out, stderr)
		}
	})
	t.Run("input_errors_never_send_request", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(500)
		}))
		defer server.Close()
		duplicate := writeFile(t, dir, "duplicate/a.txt", "second a")
		binary := writeFile(t, dir, "bad.bin", string([]byte{0xff}))
		cases := []struct {
			name        string
			args        []string
			token, want string
		}{
			{"no_files", nil, fakeToken, "usage:"},
			{"missing_file", []string{filepath.Join(dir, "missing.txt")}, fakeToken, "no such file"},
			{"directory", []string{dir}, fakeToken, "is a directory"},
			{"duplicate_basename", []string{first, duplicate}, fakeToken, "duplicate filename"},
			{"binary_file", []string{binary}, fakeToken, "not UTF-8"},
			{"bad_URL", []string{"--gist", "https://example.com/deadbeef", first}, fakeToken, "invalid Gist URL"},
			{"bad_ID", []string{"--gist", "https://gist.github.com/octocat/nope", first}, fakeToken, "invalid Gist URL"},
			{"URL_with_query", []string{"--gist", resultURL + "?x=1", first}, fakeToken, "invalid Gist URL"},
			{"missing_gist_value", []string{"--gist"}, fakeToken, "--gist requires a URL"},
			{"empty_gist_value", []string{"--gist", "", first}, fakeToken, "--gist requires a URL"},
			{"duplicate_gist", []string{"--gist", resultURL, "--gist", resultURL, first}, fakeToken, "only be supplied once"},
			{"missing_api_value", []string{"--api-url"}, fakeToken, "--api-url requires a URL"},
			{"bad_api_URL", []string{"--api-url", "file:///tmp/test", first}, fakeToken, "invalid API URL"},
			{"literal_dash_file", []string{"--", "-a-file-that-does-not-exist"}, fakeToken, "no such file"},
			{"unknown_flag", []string{"--unknown", first}, fakeToken, "unknown option"},
			{"no_token", []string{first}, "", "GITHUB_TOKEN is required"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				out, stderr, code := execute(t, cli, server.URL, tc.token, tc.args...)
				if code != 1 || out != "" || !strings.Contains(stderr, tc.want) {
					t.Fatalf("code=%d stdout=%q stderr=%q; want %q", code, out, stderr, tc.want)
				}
			})
		}
		if calls.Load() != 0 {
			t.Fatalf("%d unexpected calls", calls.Load())
		}
	})
	t.Run("protocol_errors", func(t *testing.T) {
		cases := []struct {
			name       string
			status     int
			body, want string
		}{
			{"unauthorized", 401, `{"message":"Bad credentials"}`, "HTTP 401"},
			{"validation", 422, `{"message":"Validation Failed"}`, "HTTP 422"},
			{"server_error", 500, "not JSON", "HTTP 500"},
			{"unexpected_success_status", 200, `{}`, "HTTP 200"},
			{"invalid_JSON", 201, `not JSON`, "invalid character"},
			{"trailing_JSON", 201, `{"html_url":"` + resultURL + `"} {}`, "trailing JSON"},
			{"no_URL", 201, `{}`, "no html_url"},
			{"null_URL", 201, `{"html_url":null}`, "html_url is empty"},
			{"wrong_URL_type", 201, `{"html_url":42}`, "must be a string"},
			{"invalid_response_URL", 201, `{"html_url":"https://evil.example/"}`, "not a Gist URL"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
				defer server.Close()
				out, stderr, code := execute(t, cli, server.URL, fakeToken, first)
				if code != 1 || out != "" || !strings.Contains(stderr, tc.want) {
					t.Fatalf("code=%d stdout=%q stderr=%q; want %q", code, out, stderr, tc.want)
				}
			})
		}
	})
	t.Run("redirect_is_not_followed", func(t *testing.T) {
		var forwarded atomic.Int32
		destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
		defer destination.Close()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
		}))
		defer server.Close()
		out, stderr, code := execute(t, cli, server.URL, fakeToken, first)
		if code != 1 || out != "" || !strings.Contains(stderr, "HTTP 307") || forwarded.Load() != 0 {
			t.Fatalf("code=%d stdout=%q stderr=%q forwarded=%d", code, out, stderr, forwarded.Load())
		}
	})
	t.Run("transport_failure", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		endpoint := server.URL
		server.Close()
		out, stderr, code := execute(t, cli, endpoint, fakeToken, first)
		if code != 1 || out != "" || !strings.Contains(stderr, "connection refused") {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, out, stderr)
		}
	})
	t.Run("help", func(t *testing.T) {
		out, stderr, code := execute(t, cli, "http://127.0.0.1:1", "", "--help")
		if code != 0 || !strings.HasPrefix(out, "usage:") || stderr != "" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, out, stderr)
		}
	})
}
