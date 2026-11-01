package gomtest

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRunDumpTestsUpdate exercises -update end to end: the dump file
// is created (with the nested "<phase>/<case>" directory) and a
// subsequent non-update run compares clean.
func TestRunDumpTestsUpdate(t *testing.T) {
	old := *update
	*update = true
	defer func() { *update = old }()

	dir := t.TempDir()
	dumps := map[string]DumpFunc{
		"core/sample": func() (string, error) { return "(file p)\n", nil },
	}
	RunDumpTests(t, dir, dumps)

	path := filepath.Join(dir, "core", "sample.dump")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("-update did not write the dump file: %v", err)
	}
	if string(data) != "(file p)\n" {
		t.Errorf("dump = %q", data)
	}

	*update = false
	RunDumpTests(t, dir, dumps)
}

// TestRunDumpTestsSkip checks that a "skip" manifest keeps the case
// from running: the generator is never called and -update leaves the
// (missing) fixture alone.
func TestRunDumpTestsSkip(t *testing.T) {
	old := *update
	*update = true
	defer func() { *update = old }()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "later.case.json"),
		[]byte(`{"skip": "needs P1-7"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	RunDumpTests(t, dir, map[string]DumpFunc{
		"later": func() (string, error) { called = true; return "(x)\n", nil },
	})
	if called {
		t.Error("skipped case called its generator")
	}
	if _, err := os.Stat(filepath.Join(dir, "later.dump")); !os.IsNotExist(err) {
		t.Error("-update wrote a fixture for a skipped case")
	}
}

func TestCheckDumpName(t *testing.T) {
	for _, name := range []string{"x", "core/sample", "a/b/c", "x.case", "a.b.c"} {
		if err := checkDumpName(name); err != nil {
			t.Errorf("checkDumpName(%q) = %v, want nil", name, err)
		}
	}
	for _, name := range []string{"", ".", "..", "/x", "a//b", "a/./b", "../x", "a\\b", "x.dump", "core/../x"} {
		if err := checkDumpName(name); err == nil {
			t.Errorf("checkDumpName(%q) = nil, want error", name)
		}
	}
}

func TestDumpOrphans(t *testing.T) {
	dir := t.TempDir()
	reg := map[string]bool{"keep": true, "sub/one": true}
	mk := func(rel string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("keep.dump")
	mk("keep.case.json")
	mk("sub/one.dump")
	mk("stray.dump")
	mk("sub/two.dump")
	mk("sub/two.case.json")
	mk("notes.txt")          // not a fixture file: ignored
	mk("_helper.dump")       // helper: ignored
	mk(".hidden.dump")       // dotfile: ignored
	mk("_in/sub/three.dump") // helper dir: ignored

	got := dumpOrphans(dir, reg)
	want := []string{"stray.dump", "sub/two.case.json", "sub/two.dump"}
	if len(got) != len(want) {
		t.Fatalf("dumpOrphans = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dumpOrphans = %v, want %v", got, want)
		}
	}

	if got := dumpOrphans(filepath.Join(dir, "missing"), reg); len(got) != 0 {
		t.Errorf("missing dir: dumpOrphans = %v, want none", got)
	}
}
