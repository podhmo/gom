// Package gomtest is the test harness for the file-based test suites
// under testdata/ (docs/design.md sections 3 and 7). It lives outside
// the compiler pipeline (syntax → … → codegen) so that tests of any
// phase — and of the end-to-end build — can drive the same fixtures
// without import cycles.
//
// # Suites
//
// Each suite is a directory of cases discovered by walking for *.gom
// files (recursively). A case is named by its path relative to the
// suite directory without the .gom extension; '/' in the name nests
// subtests. Files and directories whose base name starts with '_' or
// '.' are ignored, which leaves room for multi-file fixtures shared
// between cases.
//
//	testdata/spec/    — expected-diagnostics cases (spec section 12)
//	testdata/golden/  — generated-Go golden cases
//
// # Case files
//
// A spec case <name>.gom pairs with <name>.expected.json:
//
//	{"diagnostics": [ <expected diagnostic objects> ]}
//
// Each expected diagnostic uses the wire shape of spec section 12
// (code, severity, message, span, data, notes, fixes, source). Only
// the fields present in an expected object are checked — see
// RunSpecTests for the matching semantics.
//
// A golden case <name>.gom pairs with <name>.golden.go holding the
// generated Go source, compared literally after CRLF and trailing
// newline normalization.
//
// Any case may add <name>.case.json, a manifest reserved for
// per-case options. The only defined key is "skip": a non-empty
// string skips the case with that text as the reported reason. The
// .gom input and the expectations stay in place while skipped, so a
// case can be checked in before the feature it tests exists.
//
// # Wiring and updating
//
// Runners take the compile function under test as an argument, so the
// harness works before the pipeline exists:
//
//	func TestSpec(t *testing.T) {
//		gomtest.RunSpecTests(t, "", func(f gomtest.File) ([]diag.Diagnostic, error) {
//			return compile.Check(f.AbsPath(), f.Src)
//		})
//	}
//
// An empty dir defaults to testdata/<suite> at the module root, found
// by walking up from the test's working directory to the nearest
// go.mod. Running `go test -update` rewrites the expected files from
// the current output (skipped cases are untouched); review the diff
// before committing, as with any golden update.
package gomtest
