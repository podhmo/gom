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
func nilCompile(File) ([]diag.Diagnostic, error) { return nil, nil }

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
	if alpha.file.Path != "alpha.gom" {
		t.Errorf("alpha.file.Path = %q", alpha.file.Path)
	}
	if string(alpha.file.Src) != "package alpha\n" {
		t.Errorf("alpha.file.Src = %q", alpha.file.Src)
	}

	beta, ok := got["sub/beta"]
	if !ok {
		t.Fatalf("case sub/beta not found")
	}
	if beta.skip != "" {
		t.Errorf("sub/beta.skip = %q, want none", beta.skip)
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

func TestRunGoldenTestsSmoke(t *testing.T) {
	RunGoldenTests(t, filepath.Join("testdata", "golden"), func(f File) ([]byte, error) {
		return []byte("package main\n\nfunc main() {}\n"), nil
	})
}

// TestCompileFuncCalledWithFile checks that the compile function
// receives the case's File and that skipped cases never reach it.
// (Case-level failures — compile errors, missing expected files — go
// through t.Fatal and cannot be observed from a parent test.)
func TestCompileFuncCalledWithFile(t *testing.T) {
	var seen []File
	RunSpecTests(t, filepath.Join("testdata", "discover"), func(f File) ([]diag.Diagnostic, error) {
		seen = append(seen, f)
		return nil, nil
	})
	if len(seen) != 1 {
		t.Fatalf("compile called %d times, want 1 (skipped cases bypass it)", len(seen))
	}
	if seen[0].Path != "sub/beta.gom" {
		t.Errorf("compile got Path %q, want sub/beta.gom", seen[0].Path)
	}
	if string(seen[0].Src) != "package beta\n" {
		t.Errorf("compile got Src %q", seen[0].Src)
	}
	abs := seen[0].AbsPath()
	if !filepath.IsAbs(abs) || !strings.HasSuffix(abs, filepath.Join("testdata", "discover", "sub", "beta.gom")) {
		t.Errorf("AbsPath = %q", abs)
	}
}
