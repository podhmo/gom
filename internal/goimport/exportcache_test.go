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
	if n := countCalls(t, countFile); n != 1 {
		t.Fatalf("first Load ran go list %d times, want 1", n)
	}

	l2 := New(cfg)
	pkg := membersOf(t, l2, "strconv")
	if pkg.Members["Atoi"] == nil {
		t.Error("Atoi missing on the cached load")
	}
	if n := countCalls(t, countFile); n != 1 {
		t.Fatalf("second Load ran go list %d times, want 0 additional (total %d)", n-1, n)
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

	cfg := &Config{Dir: mod}
	l1 := New(cfg)
	membersOf(t, l1, "test.local/m/foo")

	writeBar("package bar\n\nconst N = 1\n\nfunc NewMember() int { return 2 }\n")

	l2 := New(cfg)
	pkg2 := membersOf(t, l2, "test.local/m/bar")
	if pkg2.Members["NewMember"] == nil {
		t.Error("NewMember missing after the dependency edit — stale cache served the old export data")
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
