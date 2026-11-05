package gomtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

// nilCompile is a DiagnosticsFunc that reports a clean compile for
// every input.
func nilCompile([]File) ([]diag.Diagnostic, error) { return nil, nil }

func TestDiscoverCases(t *testing.T) {
	dir := filepath.Join("testdata", "discover")
	cases, orphans, err := discoverCases(dir)
	if err != nil {
		t.Fatalf("discoverCases: %v", err)
	}
	if len(orphans) != 0 {
		t.Errorf("orphans = %v, want none", orphans)
	}

	got := map[string]testCase{}
	for _, c := range cases {
		got[c.name] = c
	}

	// alpha + sub/beta only: _helper.gom and .hidden.gom are helpers.
	if len(cases) != 2 {
		names := make([]string, 0, len(cases))
		for _, c := range cases {
			names = append(names, c.name)
		}
		t.Fatalf("discovered %d cases %v, want 2 (alpha, sub/beta)", len(cases), names)
	}

	alpha, ok := got["alpha"]
	if !ok {
		t.Fatalf("case alpha not found")
	}
	if alpha.skip != "needs the parser (P1-3)" {
		t.Errorf("alpha.skip = %q, want %q", alpha.skip, "needs the parser (P1-3)")
	}
	if len(alpha.files) != 1 || alpha.files[0].Path != "alpha.gom" {
		t.Errorf("alpha.files = %v, want [alpha.gom]", alpha.files)
	}
	if string(alpha.files[0].Src) != "package alpha\n" {
		t.Errorf("alpha.files[0].Src = %q", alpha.files[0].Src)
	}

	beta, ok := got["sub/beta"]
	if !ok {
		t.Fatalf("case sub/beta not found")
	}
	if beta.skip != "" {
		t.Errorf("sub/beta.skip = %q, want none", beta.skip)
	}
}

// TestDiscoverDirectoryCases covers directory cases: a dir holding a
// bare expected.json/case.json is one case over its member .gom files;
// an unmarked dir stays a namespace of individual cases.
func TestDiscoverDirectoryCases(t *testing.T) {
	dir := filepath.Join("testdata", "discoverpkg")
	cases, orphans, err := discoverCases(dir)
	if err != nil {
		t.Fatalf("discoverCases: %v", err)
	}
	if len(orphans) != 0 {
		t.Errorf("orphans = %v, want none", orphans)
	}

	got := map[string]testCase{}
	for _, c := range cases {
		got[c.name] = c
	}
	// pkgcase + skippedcase + ifacase (three groups) + nons/{a,b} (flat).
	if len(cases) != 5 {
		names := make([]string, 0, len(cases))
		for _, c := range cases {
			names = append(names, c.name)
		}
		t.Fatalf("discovered %d cases %v, want 5", len(cases), names)
	}

	pkg := got["pkgcase"]
	if !pkg.isDir {
		t.Fatalf("pkgcase.isDir = false, want a directory case")
	}
	want := []string{"pkgcase/a.gom", "pkgcase/b.gom"}
	if len(pkg.files) != len(want) {
		t.Fatalf("pkgcase.files = %v, want %v", pkg.files, want)
	}
	for i, f := range pkg.files {
		if f.Path != want[i] {
			t.Errorf("pkgcase.files[%d].Path = %q, want %q", i, f.Path, want[i])
		}
	}
	if p := pkg.sidePath(expectedExt); !strings.HasSuffix(p, filepath.Join("pkgcase", "expected.json")) {
		t.Errorf("pkgcase expected path = %q, want <dir>/pkgcase/expected.json", p)
	}

	sk := got["skippedcase"]
	if !sk.isDir || sk.skip != "group cases not wired yet" {
		t.Errorf("skippedcase = %+v, want a skipped directory case", sk)
	}

	// ifacase mixes a .gom member with a .gomi one — the interface
	// file joins the member list like any other member.
	ifa := got["ifacase"]
	if !ifa.isDir {
		t.Fatalf("ifacase.isDir = false, want a directory case")
	}
	wantIfa := []string{"ifacase/m.gom", "ifacase/m.gomi"}
	if len(ifa.files) != len(wantIfa) {
		t.Fatalf("ifacase.files = %v, want %v", ifa.files, wantIfa)
	}
	for i, f := range ifa.files {
		if f.Path != wantIfa[i] {
			t.Errorf("ifacase.files[%d].Path = %q, want %q", i, f.Path, wantIfa[i])
		}
	}

	// nons/ has no marker: its .gom files stay individual cases.
	for _, n := range []string{"nons/a", "nons/b"} {
		c, ok := got[n]
		if !ok || c.isDir || len(c.files) != 1 {
			t.Errorf("case %s = %+v, want a single-file case", n, c)
		}
	}
}

// TestDiscoverDirectoryCasesBroken covers the malformed directory
// cases: named side files inside a group, nested dirs, a group with
// no members, and a dir/file name collision.
func TestDiscoverDirectoryCasesBroken(t *testing.T) {
	dir := filepath.Join("testdata", "brokendircase")
	cases, orphans, err := discoverCases(dir)
	if err != nil {
		t.Fatalf("discoverCases: %v", err)
	}
	if len(orphans) != 0 {
		t.Errorf("orphans = %v, want none", orphans)
	}
	var broken []string
	for _, c := range cases {
		if c.loadErr == nil {
			continue
		}
		broken = append(broken, c.name+": "+c.loadErr.Error())
	}
	if len(broken) != 4 {
		t.Fatalf("got %d broken cases %v, want 4 (dup .gom, empty, named, nested)", len(broken), broken)
	}
	for _, c := range cases {
		switch {
		case c.name == "dup" && !c.isDir:
			if !strings.Contains(c.loadErr.Error(), "both a .gom file and a directory case") {
				t.Errorf("dup.gom loadErr = %v, want the name-collision error", c.loadErr)
			}
		case c.name == "empty":
			if !strings.Contains(c.loadErr.Error(), "no .gom/.gomi members") {
				t.Errorf("empty loadErr = %v, want the no-members error", c.loadErr)
			}
		case c.name == "named":
			if !strings.Contains(c.loadErr.Error(), "named side file a.expected.json") {
				t.Errorf("named loadErr = %v, want the named-side-file error", c.loadErr)
			}
		case c.name == "nested":
			if !strings.Contains(c.loadErr.Error(), "subdirectory deep") {
				t.Errorf("nested loadErr = %v, want the nested-directory error", c.loadErr)
			}
		}
	}
}

// TestDiscoverRootSideFile covers a bare side file directly under
// the suite root: the root is not a case directory and the bare
// name never matches a file case's prefixed side-file suffix, so
// without the check the file was silently ignored. It is a load
// error like a malformed directory group — while a subdirectory
// marker and a prefixed side file keep working.
func TestDiscoverRootSideFile(t *testing.T) {
	dir := filepath.Join("testdata", "rootside")
	cases, orphans, err := discoverCases(dir)
	if err != nil {
		t.Fatalf("discoverCases: %v", err)
	}
	if len(orphans) != 0 {
		t.Errorf("orphans = %v, want none", orphans)
	}
	got := map[string]testCase{}
	for _, c := range cases {
		got[c.name] = c
	}
	// case.json + expected.json + golden.go (broken) + ok + sub.
	if len(cases) != 5 {
		names := make([]string, 0, len(cases))
		for _, c := range cases {
			names = append(names, c.name)
		}
		t.Fatalf("discovered %d cases %v, want 5", len(cases), names)
	}
	for _, n := range []string{"case.json", "expected.json", "golden.go"} {
		c, ok := got[n]
		if !ok || c.loadErr == nil {
			t.Errorf("case %s: loadErr = %v, want a load error", n, c.loadErr)
			continue
		}
		if !strings.Contains(c.loadErr.Error(), "bare side file "+n) {
			t.Errorf("case %s: loadErr = %v, want the bare-side-file error", n, c.loadErr)
		}
	}
	// The neighbors keep their rules: ok's prefixed side file is
	// not orphaned, and sub's bare marker still makes a group.
	if c := got["ok"]; c.loadErr != nil {
		t.Errorf("ok: loadErr = %v, want none", c.loadErr)
	}
	if c := got["sub"]; !c.isDir || c.loadErr != nil || len(c.files) != 1 {
		t.Errorf("sub = %+v, want a working directory case over a.gom", c)
	}
}

func TestDiscoverCasesOrphans(t *testing.T) {
	dir := filepath.Join("testdata", "orphans")
	cases, orphans, err := discoverCases(dir)
	if err != nil {
		t.Fatalf("discoverCases: %v", err)
	}
	if len(cases) != 0 {
		t.Errorf("cases = %v, want none", cases)
	}
	if len(orphans) != 1 || orphans[0] != "lonely.expected.json" {
		t.Errorf("orphans = %v, want [lonely.expected.json]", orphans)
	}
}

func TestDiscoverCasesBadManifest(t *testing.T) {
	dir := filepath.Join("testdata", "badmanifest")
	cases, orphans, err := discoverCases(dir)
	if err != nil {
		t.Fatalf("discoverCases: %v", err)
	}
	if len(orphans) != 0 {
		t.Errorf("orphans = %v, want none", orphans)
	}
	if len(cases) != 1 {
		t.Fatalf("cases = %v, want [broken]", cases)
	}
	if cases[0].loadErr == nil {
		t.Fatal("broken.case.json should produce a load error")
	}
}

func TestLoadManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.case.json")

	// missing file is fine
	if skip, err := loadManifest(path); err != nil || skip != "" {
		t.Errorf("missing: skip=%q err=%v", skip, err)
	}

	for _, tt := range []struct {
		name    string
		content string
		want    string
		wantErr bool
	}{
		{"skip reason", `{"skip": "not implemented"}`, "not implemented", false},
		{"empty manifest", `{}`, "", false},
		{"unknown keys ignored", `{"flavor": "x"}`, "", false},
		{"empty skip", `{"skip": ""}`, "", false},
		{"non-string skip", `{"skip": true}`, "", true},
		{"invalid json", `{`, "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			skip, err := loadManifest(path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got skip=%q", skip)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if skip != tt.want {
				t.Errorf("skip = %q, want %q", skip, tt.want)
			}
		})
	}
}

func TestResolveSuiteDir(t *testing.T) {
	// "" resolves to <module root>/testdata/<sub>.
	got := resolveSuiteDir(t, "", "spec")
	want := filepath.Join(moduleRoot(t), "testdata", "spec")
	if got != want {
		t.Errorf("resolveSuiteDir(\"\") = %q, want %q", got, want)
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("default suite dir %s should exist: %v", got, err)
	}

	// A relative dir resolves against the package's working directory.
	got = resolveSuiteDir(t, filepath.Join("testdata", "discover"), "spec")
	want, err := filepath.Abs(filepath.Join("testdata", "discover"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("resolveSuiteDir(relative) = %q, want %q", got, want)
	}
}

func TestModuleRoot(t *testing.T) {
	root := moduleRoot(t)
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("moduleRoot %q has no go.mod: %v", root, err)
	}
	if _, err := os.Stat(filepath.Join(root, "internal", "gomtest")); err != nil {
		t.Fatalf("moduleRoot %q is not the gom repo root: %v", root, err)
	}
}

// TestRunSpecTestsSmoke exercises the whole spec path — discovery,
// skip handling and comparison — against the discover fixture with a
// compiler that reports no diagnostics. Run with -v to see the skip
// reason listed for alpha.
func TestRunSpecTestsSmoke(t *testing.T) {
	RunSpecTests(t, filepath.Join("testdata", "discover"), nilCompile)
}

// TestRunSpecTestsDirCaseSmoke runs the spec path over the
// discoverpkg fixture: the directory case reaches compile as one
// two-file unit, the namespace cases as single files, and the
// manifest-skipped case is bypassed.
func TestRunSpecTestsDirCaseSmoke(t *testing.T) {
	var got [][]string
	RunSpecTests(t, filepath.Join("testdata", "discoverpkg"), func(fs []File) ([]diag.Diagnostic, error) {
		var paths []string
		for _, f := range fs {
			paths = append(paths, f.Path)
		}
		got = append(got, paths)
		return nil, nil
	})
	want := [][]string{
		{"ifacase/m.gom", "ifacase/m.gomi"},
		{"nons/a.gom"}, {"nons/b.gom"},
		{"pkgcase/a.gom", "pkgcase/b.gom"},
	}
	if len(got) != len(want) {
		t.Fatalf("compile calls = %v, want %v", got, want)
	}
	for i := range want {
		if strings.Join(got[i], ",") != strings.Join(want[i], ",") {
			t.Errorf("call %d files = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestRunGoldenTestsSmoke(t *testing.T) {
	RunGoldenTests(t, filepath.Join("testdata", "golden"), func(f File) ([]byte, error) {
		return []byte("package main\n\nfunc main() {}\n"), nil
	})
}

// TestCompileFuncCalledWithFile checks that the compile function
// receives the case's files and that skipped cases never reach it.
// (Case-level failures — compile errors, missing expected files — go
// through t.Fatal and cannot be observed from a parent test.)
func TestCompileFuncCalledWithFile(t *testing.T) {
	var seen [][]File
	RunSpecTests(t, filepath.Join("testdata", "discover"), func(fs []File) ([]diag.Diagnostic, error) {
		seen = append(seen, fs)
		return nil, nil
	})
	if len(seen) != 1 {
		t.Fatalf("compile called %d times, want 1 (skipped cases bypass it)", len(seen))
	}
	if len(seen[0]) != 1 || seen[0][0].Path != "sub/beta.gom" {
		t.Fatalf("compile got files %v, want [sub/beta.gom]", seen[0])
	}
	if string(seen[0][0].Src) != "package beta\n" {
		t.Errorf("compile got Src %q", seen[0][0].Src)
	}
	abs := seen[0][0].AbsPath()
	if !filepath.IsAbs(abs) || !strings.HasSuffix(abs, filepath.Join("testdata", "discover", "sub", "beta.gom")) {
		t.Errorf("AbsPath = %q", abs)
	}
}
