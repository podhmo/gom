// Package gomtest is the test harness for the file-based test suites
// under testdata/ (docs/design.md sections 3 and 7). It lives outside
// the compiler pipeline (syntax → … → codegen) so that tests of any
// phase — and of the end-to-end build — can drive the same fixtures
// without import cycles.
//
// # Suites
//
// The spec and golden suites discover cases by walking a directory
// for *.gom files (recursively). A case is named by its path relative
// to the suite directory without the .gom extension; '/' in the name
// nests subtests. Files and directories whose base name starts with
// '_' or '.' are ignored, which leaves room for multi-file fixtures
// shared between cases.
//
// A subdirectory is a *directory case* — one case over all its
// member files — when it directly holds a bare side file
// (expected.json, golden.go, or case.json): its non-ignored *.gom
// files compile together as one package unit, in filename order,
// while a *.gomi member compiles as an interface file in its own
// scope. The group's side files are the bare names
// (<case>/expected.json); member-named side files and nested
// directories inside a case are errors. Without a marker the
// directory is a pure namespace and its .gom files are individual
// cases. A bare side file at the suite root is an error too — the
// root is not a case directory, so a stray bare name there marks
// nothing and almost always means a misplaced fixture. Only the
// spec suite supports directory cases; the golden suite rejects
// them.
//
//	testdata/spec/    — expected-diagnostics cases (spec section 12)
//	testdata/golden/  — generated-Go golden cases
//	testdata/dump/    — phase-IR dump cases (see below)
//
// # Dump suite
//
// The dump suite (RunDumpTests) is different: there is no .gom input.
// The caller registers a map of named generators, one per case; the
// name "<phase>/<case>" maps to <dir>/<phase>/<case>.dump holding the
// generator's expected text — conventionally a treedump rendering of
// a phase's IR. Fixture files on disk with no registered generator
// are orphan errors, so deleting a generator deletes its fixture.
//
// # Case files
//
// A spec case <name>.gom pairs with <name>.expected.json — a
// directory case <name>/ with <name>/expected.json:
//
//	{"diagnostics": [ <expected diagnostic objects> ]}
//
// Each expected diagnostic uses the wire shape of spec section 12
// (code, severity, message, span, data, notes, fixes, source). Only
// the fields present in an expected object are checked — see
// RunSpecTests for the matching semantics. A directory case compares
// the aggregated diagnostics of its whole compilation unit; a span's
// "file" names the member by its suite-relative path
// (<case>/<member>.gom).
//
// A golden case <name>.gom pairs with <name>.golden.go holding the
// generated Go source, compared literally after CRLF and trailing
// newline normalization.
//
// Any case may add a manifest — <name>.case.json, or the bare
// case.json inside a directory case — reserved for per-case options.
// The only defined key is "skip": a non-empty string skips the case
// with that text as the reported reason. The .gom input and the
// expectations stay in place while skipped, so a case can be checked
// in before the feature it tests exists.
//
// # Wiring and updating
//
// Runners take the compile function under test as an argument, so the
// harness works before the pipeline exists:
//
//	func TestSpec(t *testing.T) {
//		gomtest.RunSpecTests(t, "", func(fs []gomtest.File) ([]diag.Diagnostic, error) {
//			return compile.CheckFiles(fs)
//		})
//	}
//
// An empty dir defaults to testdata/<suite> at the module root, found
// by walking up from the test's working directory to the nearest
// go.mod. Running `go test -update` rewrites the expected files from
// the current output (skipped cases are untouched); review the diff
// before committing, as with any golden update.
package gomtest
