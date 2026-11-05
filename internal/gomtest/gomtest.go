package gomtest

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

// update implements the `go test -update` convention: when set, the
// runners rewrite expected files from the compiler's actual output
// instead of comparing. Skipped cases are never touched.
var update = flag.Bool("update", false,
	"rewrite expected files under testdata (spec *.expected.json, golden *.golden.go)")

// File extensions and suffixes making up a case, by suite. A spec case
// is <name>.gom plus <name>.expected.json; a golden case is <name>.gom
// plus <name>.golden.go; either may add <name>.case.json. A directory
// case drops the <name> prefix from its side files: <case>/ has bare
// expected.json / golden.go / case.json next to its member .gom files.
const (
	gomExt      = ".gom"
	gomiExt     = ".gomi"
	expectedExt = ".expected.json"
	goldenExt   = ".golden.go"
	caseExt     = ".case.json"
)

// File is one .gom source file handed to a compile function.
type File struct {
	// Path is the slash-separated path of the file relative to Dir
	// (e.g. "eval.gom" or "match/eval.gom"). Diagnostics produced for
	// this file should use it as their span's File so positions line
	// up with the expected files.
	Path string
	// Dir is the absolute path of the suite directory; sibling
	// fixtures live next to Path under it.
	Dir string
	// Src is the contents of the file.
	Src []byte
}

// AbsPath returns the absolute path of the file on disk.
func (f File) AbsPath() string {
	return filepath.Join(f.Dir, filepath.FromSlash(f.Path))
}

// IsInterfaceFile reports whether path names a .gomi interface file.
// A .gomi member of a directory case compiles in its own scope rather
// than joining the group's package unit — the same convention
// build.Input follows.
func IsInterfaceFile(path string) bool {
	return strings.HasSuffix(path, gomiExt)
}

// DiagnosticsFunc compiles a case's files and returns the
// diagnostics produced by the pipeline stages under test. fs has one
// entry for a single-file case and one per member — in filename
// order — for a directory case, where the .gom members compile as
// one package unit and a .gomi member compiles as an interface file
// in its own scope (build.CompileFiles semantics; a one-file unit
// degenerates to the single-file pipeline). The order of the
// returned diagnostics does not matter: the harness sorts a copy
// with diag.List.Sort before comparing. An error fails the case.
type DiagnosticsFunc func(fs []File) ([]diag.Diagnostic, error)

// GeneratedFile is one generated output file of a case — a member's
// package source, or its _test.go half under the "tests" manifest
// option. A single-file case produces one; a directory case one per
// .gom member (a .gomi member produces none) plus any test halves.
type GeneratedFile struct {
	// Path is the output file's suite-relative slash path as
	// `gom build`/`gom test` would write it (e.g. "a.go" for a flat
	// case, "pkgcase/a_test.go" inside a directory case). The golden
	// pinning it is the same path with .golden.go substituted for
	// .go.
	Path string
	// Src is the generated file's contents.
	Src []byte
}

// CaseOptions carries the case manifest's (case.json) per-case
// options a suite runner hands the compile function. It is the
// harness's pipeline-agnostic name for them — the caller maps each
// option to its own compile settings.
type CaseOptions struct {
	// Tests asks for `gom test` mode compilation (spec section 11.6,
	// lower.Env.Tests): test decls and @test helpers stay in the
	// pipeline, so a member's generated output includes its
	// _test.go half.
	Tests bool
}

// GenerateFunc compiles a case's files and returns the generated Go
// files for the golden suite — a case is one compilation unit, so
// like DiagnosticsFunc it takes the whole member list; single-file
// cases get a one-element slice and return a one-element result in
// member order. An error fails the case.
type GenerateFunc func(fs []File, opts CaseOptions) ([]GeneratedFile, error)

// testCase is one discovered case — a single .gom file, or a
// directory whose .gom files compile as one package unit — and its
// manifest.
type testCase struct {
	name  string // Path minus ".gom", or the directory's path; the subtest name
	files []File // the .gom inputs, Src already read; one entry unless isDir

	isDir  bool   // directory case: side files are bare names inside absDir
	absDir string // absolute path of the case directory (isDir only)

	skip    string // manifest skip reason; "" runs the case
	tests   bool   // manifest "tests" option: compile the case in test mode
	loadErr error  // non-nil manifests a broken case, reported as a failure
}

// sidePath returns the path of the case file obtained by replacing
// the .gom extension with ext (e.g. ".expected.json"). A directory
// case drops the name prefix: its side files are the bare extension
// names (<case>/expected.json).
func (c testCase) sidePath(ext string) string {
	if c.isDir {
		return filepath.Join(c.absDir, ext[1:])
	}
	return strings.TrimSuffix(c.files[0].AbsPath(), gomExt) + ext
}

// discoverCases walks suiteDir for cases: a *.gom file is a
// single-file case, and a directory holding a bare side file
// (expected.json, golden.go or case.json) is a directory case over
// its member *.gom/*.gomi files. It reads each case's manifest,
// reports side files (*.expected.json, *.golden.go, *.case.json)
// whose .gom is missing as orphans, and marks a bare side file
// directly at the suite root a load error — the root is not a case
// directory, so the bare name is a misplaced fixture, not a marker.
func discoverCases(suiteDir string) (cases []testCase, orphans []string, err error) {
	var sideFiles []string
	err = filepath.WalkDir(suiteDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != suiteDir {
				if ignored(name) {
					return fs.SkipDir
				}
				c, isCase, err := dirCase(suiteDir, path)
				if err != nil {
					return err
				}
				if isCase {
					cases = append(cases, c)
					return fs.SkipDir
				}
			}
			return nil
		}
		if ignored(name) {
			return nil
		}
		switch {
		case strings.HasSuffix(name, gomExt):
			rel, err := filepath.Rel(suiteDir, path)
			if err != nil {
				return err
			}
			src, err := os.ReadFile(path)
			c := testCase{
				name:  strings.TrimSuffix(filepath.ToSlash(rel), gomExt),
				files: []File{{Path: filepath.ToSlash(rel), Dir: suiteDir, Src: src}},
			}
			if err != nil {
				c.loadErr = err
			}
			cases = append(cases, c)
		default:
			for _, ext := range []string{expectedExt, goldenExt, caseExt} {
				if strings.HasSuffix(name, ext) {
					sideFiles = append(sideFiles, path)
					break
				}
				if name == ext[1:] && filepath.Dir(path) == suiteDir {
					// A bare side file at the suite root
					// marks nothing — the root is not a
					// case directory, and the name is too
					// short for a file case's prefixed
					// side-file suffix. It almost always
					// means a misplaced fixture, so it
					// reports like a malformed directory
					// group rather than dropping silently.
					cases = append(cases, testCase{
						name: name,
						loadErr: fmt.Errorf("gomtest: bare side file %s at the suite root: "+
							"the bare name marks a directory case inside a subdirectory; "+
							"a file case's side file is prefixed (<name>%s)", name, ext),
					})
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("gomtest: cannot walk %s: %w", suiteDir, err)
	}

	gomPaths := make(map[string]bool, len(cases))
	seenNames := make(map[string]bool, len(cases))
	for i := range cases {
		c := &cases[i]
		if seenNames[c.name] && c.loadErr == nil {
			c.loadErr = fmt.Errorf("gomtest: case name %q is declared by both a .gom file and a directory case", c.name)
		}
		seenNames[c.name] = true
		for _, f := range c.files {
			gomPaths[f.AbsPath()] = true
		}
		if c.loadErr != nil {
			continue
		}
		m, err := loadManifest(c.sidePath(caseExt))
		if err != nil {
			c.loadErr = err
		}
		c.skip = m.skip
		c.tests = m.tests
	}

	for _, p := range sideFiles {
		var gom string
		for _, ext := range []string{expectedExt, goldenExt, caseExt} {
			if before, ok := strings.CutSuffix(p, ext); ok {
				gom = before + gomExt
				break
			}
		}
		if !gomPaths[gom] {
			rel, _ := filepath.Rel(suiteDir, p)
			orphans = append(orphans, filepath.ToSlash(rel))
		}
	}
	return cases, orphans, nil
}

// dirCase reports whether dir is a directory case: a bare side file —
// expected.json, golden.go or case.json, the side-file names without
// their case-name prefix — sits directly inside it. When it is, the
// returned testCase covers every non-ignored *.gom and *.gomi file
// directly in dir, in filename order (os.ReadDir order), as one case
// named by dir's suite-relative path (.gomi members compile as
// interface files in their own scope).
//
// A directory without a marker is a pure namespace: the walk descends
// into it and its .gom files stay individual cases. Inside a case
// directory a member-named expected.json/case.json side file
// (a.expected.json) or a subdirectory is a load error — a group has
// exactly one expectation and is not subdivided further; a case with
// no members is an error too, since a group always starts from real
// inputs. Member-named *.golden.go files are exempt: they are the
// golden suite's per-output expectations (a.golden.go pins member
// a.gom's generated a.go), so a group has one golden per generated
// file rather than one expectation total.
func dirCase(suiteDir, dir string) (testCase, bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return testCase{}, false, fmt.Errorf("gomtest: cannot read %s: %w", dir, err)
	}
	marker := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		for _, ext := range []string{expectedExt, goldenExt, caseExt} {
			if e.Name() == ext[1:] {
				marker = true
			}
		}
	}
	if !marker {
		return testCase{}, false, nil
	}

	rel, err := filepath.Rel(suiteDir, dir)
	if err != nil {
		return testCase{}, false, err
	}
	c := testCase{name: filepath.ToSlash(rel), isDir: true, absDir: dir}
	var named []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() {
			if c.loadErr == nil {
				c.loadErr = fmt.Errorf("gomtest: directory case %s contains subdirectory %s", c.name, n)
			}
			continue
		}
		if ignored(n) {
			continue
		}
		if strings.HasSuffix(n, gomExt) || IsInterfaceFile(n) {
			src, rerr := os.ReadFile(filepath.Join(dir, n))
			if rerr != nil && c.loadErr == nil {
				c.loadErr = rerr
			}
			c.files = append(c.files, File{Path: c.name + "/" + n, Dir: suiteDir, Src: src})
			continue
		}
		for _, ext := range []string{expectedExt, caseExt} {
			if strings.HasSuffix(n, ext) && n != ext[1:] {
				named = append(named, n)
				break
			}
		}
	}
	if c.loadErr == nil && len(named) > 0 {
		c.loadErr = fmt.Errorf("gomtest: named side file %s in directory case %s: "+
			"a group's side files are bare (expected.json, case.json)", named[0], c.name)
	}
	if c.loadErr == nil && len(c.files) == 0 {
		c.loadErr = fmt.Errorf("gomtest: directory case %s has no .gom/.gomi members", c.name)
	}
	return c, true, nil
}

// ignored reports whether a file or directory inside a suite is not a
// case: helper sources and dotfiles are skipped so that multi-file
// fixtures can live next to the cases that use them.
func ignored(name string) bool {
	return strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".")
}

// caseManifest is a case's parsed <name>.case.json — the per-case
// options channel shared by the suites.
type caseManifest struct {
	// skip is the "skip" reason; "" runs the case.
	skip string
	// tests is the "tests" option: the golden suite compiles the
	// case in `gom test` mode (lower.Env.Tests), so test-mode
	// outputs — the members' _test.go halves — join the golden
	// expectations.
	tests bool
}

// loadManifest reads a <name>.case.json file. A missing file is not an
// error. The defined keys are "skip" (string) and "tests" (bool).
func loadManifest(path string) (caseManifest, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return caseManifest{}, nil
	}
	if err != nil {
		return caseManifest{}, fmt.Errorf("gomtest: cannot read %s: %w", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return caseManifest{}, fmt.Errorf("gomtest: invalid JSON in %s: %w", path, err)
	}
	var out caseManifest
	if v, ok := m["skip"]; ok {
		s, ok := v.(string)
		if !ok {
			return caseManifest{}, fmt.Errorf("gomtest: %s: \"skip\" must be a string", path)
		}
		out.skip = s
	}
	if v, ok := m["tests"]; ok {
		b, ok := v.(bool)
		if !ok {
			return caseManifest{}, fmt.Errorf("gomtest: %s: \"tests\" must be a bool", path)
		}
		out.tests = b
	}
	return out, nil
}

// resolveSuiteDir turns the dir argument of a runner into an absolute
// suite directory. "" selects <module root>/testdata/<sub>, where the
// module root is found by walking up from the test's working directory
// to the nearest go.mod — go test runs with the package directory as
// working directory. A relative dir resolves against the working
// directory in the usual way.
func resolveSuiteDir(t *testing.T, dir, sub string) string {
	t.Helper()
	if dir == "" {
		dir = filepath.Join(moduleRoot(t), "testdata", sub)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("gomtest: cannot resolve suite dir %q: %v", dir, err)
	}
	return abs
}

// moduleRoot returns the directory containing go.mod closest to the
// working directory.
func moduleRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("gomtest: cannot get working directory: %v", err)
	}
	for dir := cwd; ; {
		if st, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !st.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("gomtest: no go.mod found above %s; pass an explicit suite dir", cwd)
		}
		dir = parent
	}
}

// runCases is the shared body of the suite runners: discovery, orphan
// reporting, per-case subtests with manifest errors and skips.
func runCases(t *testing.T, suiteDir, suiteName string, run func(t *testing.T, c testCase)) {
	t.Helper()
	cases, orphans, err := discoverCases(suiteDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range orphans {
		t.Errorf("gomtest: orphan fixture %s in %s: no matching .gom file", o, suiteName)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Helper()
			if c.loadErr != nil {
				t.Fatal(c.loadErr)
			}
			if c.skip != "" {
				t.Skipf("skipped: %s", c.skip)
			}
			run(t, c)
		})
	}
}
