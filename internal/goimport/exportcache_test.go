package goimport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// countingGoBin writes a wrapper shell script that tallies every
// invocation into countFile and forwards to the real `go`, and
// returns a Config pointing at it — same script → same stat → same
// cache key prefix for every Loader built on it.
func countingGoBin(t *testing.T) (cfg *Config, countFile string) {
	t.Helper()
	dir := t.TempDir()
	countFile = filepath.Join(dir, "count")
	bin := filepath.Join(dir, "go")
	script := "#!/bin/sh\necho x >> " + countFile + "\nexec go \"$@\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Config{GoBin: bin}, countFile
}

func countCalls(t *testing.T, countFile string) int {
	t.Helper()
	data, err := os.ReadFile(countFile)
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "\n")
}

// A second Loader (a fresh process would see the same) must answer
// `import go` without running `go list` again — the file cache hit
// is observable because the wrapped gobin counts each call.
func TestExportCacheSecondLoaderSkipsGoList(t *testing.T) {
	if testing.Short() {
		t.Skip("runs `go list -export`")
	}
	dir := t.TempDir()
	t.Setenv(exportCacheDirEnv, dir)

	cfg, countFile := countingGoBin(t)
	l1 := New(cfg)
	membersOf(t, l1, "strconv")
	// Wrapped-gobin calls: `go env` (cache key) + `go list -export`.
	if n := countCalls(t, countFile); n != 2 {
		t.Fatalf("first Load ran gobin %d times, want 2 (env+list)", n)
	}

	l2 := New(cfg)
	pkg := membersOf(t, l2, "strconv")
	if pkg.Members["Atoi"] == nil {
		t.Error("Atoi missing on the cached load")
	}
	// The second loader's key probe reuses the memoized `go env`
	// answer, so no extra wrapped-gobin call happens at all — and
	// `go list` must not re-run either.
	if n := countCalls(t, countFile); n != 2 {
		t.Fatalf("second Load ran gobin %d times, want 0 additional (total %d)", n-2, n)
	}
}

// Editing a package's source must invalidate the cached export
// answer — a wrongly-hit cache would serve the stale export data
// without the new member.
func TestExportCacheSourceEditInvalidates(t *testing.T) {
	if testing.Short() {
		t.Skip("runs `go list -export`")
	}
	dir := t.TempDir()
	t.Setenv(exportCacheDirEnv, dir)

	mod := t.TempDir()
	pkgDir := filepath.Join(mod, "foo")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte("module test.local/m\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(pkgDir, "foo.go")
	write := func(body string) {
		if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		// Push the mtime past any filesystem granularity so the
		// recorded max-source-mtime cannot match by accident.
		future := time.Now().Add(2 * time.Second)
		if err := os.Chtimes(src, future, future); err != nil {
			t.Fatal(err)
		}
	}
	write("package foo\n\nfunc One() int { return 1 }\n")

	cfg := &Config{Dir: mod}
	l1 := New(cfg)
	pkg := membersOf(t, l1, "test.local/m/foo")
	if pkg.Members["One"] == nil || pkg.Members["Two"] != nil {
		t.Fatalf("first load members wrong: One=%v Two=%v", pkg.Members["One"] != nil, pkg.Members["Two"] != nil)
	}

	write("package foo\n\nfunc One() int { return 1 }\n\nfunc Two() int { return 2 }\n")

	l2 := New(cfg)
	pkg2 := membersOf(t, l2, "test.local/m/foo")
	if pkg2.Members["Two"] == nil {
		t.Error("Two missing after the source edit — stale cache served the old export data")
	}
}

// GOM_GOIMPORT_NOCACHE must bypass the file cache entirely.
func TestExportCacheDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("runs `go list -export`")
	}
	dir := t.TempDir()
	t.Setenv(exportCacheDirEnv, dir)
	t.Setenv(exportCacheOffEnv, "1")

	cfg, countFile := countingGoBin(t)
	l1 := New(cfg)
	membersOf(t, l1, "strconv")
	l2 := New(cfg)
	membersOf(t, l2, "strconv")
	// NOCACHE still runs `go env` for the key probe path? No — the
	// cache is disabled before keying, so each Load is one `go list`.
	if n := countCalls(t, countFile); n != 2 {
		t.Fatalf("nocache ran go list %d times, want 2", n)
	}
}

// Editing a *dependency's* source must also invalidate the entry —
// dep API is baked into the importer's archive, so the importing
// package's unchanged source cannot prove freshness alone.
func TestExportCacheDepEditInvalidates(t *testing.T) {
	if testing.Short() {
		t.Skip("runs `go list -export`")
	}
	dir := t.TempDir()
	t.Setenv(exportCacheDirEnv, dir)

	mod := t.TempDir()
	for _, d := range []string{"foo", "bar"} {
		if err := os.MkdirAll(filepath.Join(mod, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte("module test.local/m\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod, "foo", "foo.go"),
		[]byte("package foo\n\nimport \"test.local/m/bar\"\n\nfunc One() int { return bar.N }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	barSrc := filepath.Join(mod, "bar", "bar.go")
	writeBar := func(body string) {
		if err := os.WriteFile(barSrc, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		future := time.Now().Add(2 * time.Second)
		if err := os.Chtimes(barSrc, future, future); err != nil {
			t.Fatal(err)
		}
	}
	writeBar("package bar\n\nconst N = 1\n")

	cfg, countFile := countingGoBin(t)
	cfg.Dir = mod
	l1 := New(cfg)
	membersOf(t, l1, "test.local/m/foo")

	writeBar("package bar\n\nconst N = 1\n\nfunc NewMember() int { return 2 }\n")

	// Reload the IMPORTER (foo): bar's API is baked into foo's
	// archive, so a fresh answer for foo is only possible if the
	// dep-src-mtime validation noticed bar's edit — reloading bar
	// itself would pass even without that check. The wrapped gobin
	// pins it: the second load must pay for `go list` again.
	before := countCalls(t, countFile)
	l2 := New(cfg)
	membersOf(t, l2, "test.local/m/foo")
	// `go env` is memoized process-wide, so only `go list` re-runs.
	if n := countCalls(t, countFile) - before; n != 1 {
		t.Fatalf("reloading foo after the bar edit ran gobin %d times, want 1 (list)", n)
	}
}

// A relative package path ("./foo") resolves through `go list`'s
// canonical output — the root is the LAST line of `-deps`, not an
// import-path match on the argument.
func TestGoExportRelativePath(t *testing.T) {
	if testing.Short() {
		t.Skip("runs `go list -export`")
	}
	mod := t.TempDir()
	if err := os.MkdirAll(filepath.Join(mod, "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte("module test.local/m\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod, "foo", "foo.go"),
		[]byte("package foo\n\nfunc One() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := New(&Config{Dir: mod})
	pkg := membersOf(t, l, "./foo")
	if pkg.Members["One"] == nil {
		t.Error("One missing — relative path did not reach export data")
	}
}

// Prefetch must settle every `import go` miss of a package with one
// `go list -export` subprocess instead of one per clause — the
// wrapped gobin counts the calls.
func TestPrefetchBatchesGoList(t *testing.T) {
	if testing.Short() {
		t.Skip("runs `go list -export`")
	}
	dir := t.TempDir()
	t.Setenv(exportCacheDirEnv, dir)

	cfg, countFile := countingGoBin(t)
	l := New(cfg)
	l.Prefetch([]string{"strconv", "strings"})
	// Wrapped-gobin calls: `go env` (key, once per gobin) + ONE
	// batched `go list -export` — not one per path.
	if n := countCalls(t, countFile); n != 2 {
		t.Fatalf("Prefetch ran gobin %d times, want 2 (env+one list)", n)
	}
	for _, p := range []string{"strconv", "strings"} {
		pkg := membersOf(t, l, p)
		if len(pkg.Members) == 0 {
			t.Fatalf("%s loaded with no members after prefetch", p)
		}
	}
	if n := countCalls(t, countFile); n != 2 {
		t.Fatalf("loads after prefetch ran gobin %d times total, want 2", n)
	}
}
