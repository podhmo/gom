package gomtest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// dumpExt is the fixture suffix of the dump suite: case <name> pairs
// with <dir>/<name>.dump.
const dumpExt = ".dump"

// DumpFunc produces the text of one dump case — typically a treedump
// rendering of a compiler IR at a phase boundary. The generator owns
// the whole pipeline: it may build the IR directly, or drive earlier
// phases from a source it embeds or reads itself. An error fails the
// case.
type DumpFunc func() (string, error)

// RunDumpTests runs the dump suite in dir ("" selects the repo's
// testdata/dump), one subtest per entry of dumps. The map key is the
// case name: it maps to the fixture <dir>/<name>.dump, and '/' nests
// both the fixture and the subtests — the convention for phase dumps
// is "<phase>/<case>", e.g. "core/arith" lands at
// testdata/dump/core/arith.dump.
//
// Unlike the spec and golden suites there is no .gom input to read:
// a dump case is pure generator, which keeps the suite usable while
// the phases that would produce the IR are still being written
// (P1-4/5/7 plug their own generators in later):
//
//	gomtest.RunDumpTests(t, "", map[string]gomtest.DumpFunc{
//		"lower/arith": func() (string, error) {
//			f, err := lowerFile(arithSrc)
//			if err != nil {
//				return "", err
//			}
//			return treedump.DumpCore(f), nil
//		},
//	})
//
// A <name>.case.json manifest skips a case, same as the other
// suites. Every .dump or .case.json file under dir must belong to a
// registered case — leftovers are reported as orphans, so removing a
// generator forces its fixture to be removed too. With -update the
// .dump file is rewritten (and created, with parent directories) from
// the generator's output; skipped cases are untouched. Comparison is
// the same literal match as the golden suite: CRLF normalized, any
// difference in trailing newlines ignored.
func RunDumpTests(t *testing.T, dir string, dumps map[string]DumpFunc) {
	t.Helper()
	if len(dumps) == 0 {
		t.Fatal("gomtest: no dump generators registered")
	}
	names := make([]string, 0, len(dumps))
	for name, fn := range dumps {
		if fn == nil {
			t.Fatalf("gomtest: nil DumpFunc for %q", name)
		}
		if err := checkDumpName(name); err != nil {
			t.Fatalf("gomtest: invalid dump case name: %v", err)
		}
		names = append(names, name)
	}
	slices.Sort(names) // deterministic subtest order — names come from a map
	suiteDir := resolveSuiteDir(t, dir, "dump")

	registered := make(map[string]bool, len(names))
	for _, n := range names {
		registered[n] = true
	}
	for _, o := range dumpOrphans(suiteDir, registered) {
		t.Errorf("gomtest: orphan fixture %s in %s: no registered dump generator", o, suiteDir)
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Helper()
			skip, err := loadManifest(dumpPath(suiteDir, name, caseExt))
			if err != nil {
				t.Fatal(err)
			}
			if skip != "" {
				t.Skipf("skipped: %s", skip)
			}
			got, err := dumps[name]()
			if err != nil {
				t.Fatalf("dump failed: %v", err)
			}
			dumpPath := dumpPath(suiteDir, name, dumpExt)

			if *update {
				if err := os.MkdirAll(filepath.Dir(dumpPath), 0o755); err != nil {
					t.Fatalf("gomtest: cannot create %s: %v", filepath.Dir(dumpPath), err)
				}
				writeGolden(t, dumpPath, []byte(got))
				return
			}

			want, err := os.ReadFile(dumpPath)
			if errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("gomtest: missing dump file %s (run `go test -update`)", dumpPath)
			}
			if err != nil {
				t.Fatalf("gomtest: cannot read %s: %v", dumpPath, err)
			}
			if !goldenEqual(want, []byte(got)) {
				t.Errorf("dump mismatch for %s:\n%s",
					name, simpleDiff(normalizeGolden(want), normalizeGolden([]byte(got))))
			}
		})
	}
}

// dumpPath returns the fixture path of case name under suiteDir with
// the given extension (".dump" or ".case.json").
func dumpPath(suiteDir, name, ext string) string {
	return filepath.Join(suiteDir, filepath.FromSlash(name)) + ext
}

// checkDumpName validates a dump case name: it must be a clean
// slash-separated relative path — the name is appended to the suite
// directory verbatim, so nothing may escape it.
func checkDumpName(name string) error {
	switch {
	case name == "":
		return errors.New("empty name")
	case name == "." || name == "..":
		return fmt.Errorf("%q escapes the suite directory", name)
	case filepath.IsAbs(name) || strings.HasPrefix(name, "/"):
		return fmt.Errorf("%q is absolute", name)
	case strings.ContainsRune(name, '\\'):
		return fmt.Errorf("%q contains a backslash", name)
	case path.Clean(name) != name:
		return fmt.Errorf("%q is not a clean path", name)
	case name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../"):
		return fmt.Errorf("%q escapes the suite directory", name)
	case strings.HasSuffix(name, dumpExt):
		return fmt.Errorf("%q already ends in %s", name, dumpExt)
	}
	return nil
}

// dumpOrphans reports fixture files under suiteDir that no registered
// case owns — every .dump and .case.json file must correspond to a
// generator. A missing suite dir scans as empty.
func dumpOrphans(suiteDir string, registered map[string]bool) (orphans []string) {
	if _, err := os.Stat(suiteDir); err != nil {
		return nil
	}
	_ = filepath.WalkDir(suiteDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if p != suiteDir && ignored(name) {
				return fs.SkipDir
			}
			return nil
		}
		if ignored(name) {
			return nil
		}
		var base string
		switch {
		case strings.HasSuffix(name, dumpExt):
			base = strings.TrimSuffix(name, dumpExt)
		case strings.HasSuffix(name, caseExt):
			base = strings.TrimSuffix(name, caseExt)
		default:
			return nil
		}
		rel, err := filepath.Rel(suiteDir, p)
		if err != nil {
			return nil
		}
		caseName := filepath.ToSlash(filepath.Join(filepath.Dir(rel), base))
		if !registered[caseName] {
			orphans = append(orphans, filepath.ToSlash(rel))
		}
		return nil
	})
	slices.Sort(orphans)
	return orphans
}
