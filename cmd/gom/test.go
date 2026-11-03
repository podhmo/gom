package main

// This file holds the `gom test` command body (docs/design.md
// section 4.8): compile the inputs in test mode (lower.Env.Tests),
// write the generated .go and _test.go files next to their .gom
// sources, run `go test -json` per package directory, and translate
// the events into the spec section 12 message stream — "test"
// objects per test plus "diagnostic" objects for toolchain errors.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/codegen"
	"github.com/podhmo/gom/internal/diag"
)

// runTest implements `gom test [-format F] [FILE|DIR ...]`.
func runTest(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "json", "diagnostic format: json or text")
	if err := fs.Parse(args); err != nil {
		return flagError(err)
	}
	if err := checkFormat(*format); err != nil {
		fmt.Fprintf(stderr, "gom test: %v\n", err)
		return diag.ExitInternal
	}
	inPaths, err := expandInputs(fs.Args(), false)
	if err != nil {
		fmt.Fprintf(stderr, "gom test: %v\n", err)
		return diag.ExitInternal
	}
	inputs, err := readInputs(inPaths)
	if err != nil {
		fmt.Fprintf(stderr, "gom test: %v\n", err)
		return diag.ExitInternal
	}

	// Tests is non-nil (possibly empty) so the summary reports the
	// "tests" counts even when the run never reaches go test.
	rep := diag.Report{Files: inputNames(inputs), Tests: []diag.TestResult{}}
	srcs := map[string][]byte{}
	results := make([]*build.Result, len(inputs))
	for i, in := range inputs {
		srcs[in.path] = in.src
		opts := compileOptions(in, false)
		opts.Lower.Tests = true
		r, err := build.Compile(build.Input{Path: in.path, Src: in.src, Interface: in.iface}, opts)
		if err != nil {
			fmt.Fprintf(stderr, "gom test: %s: %v\n", in.path, err)
			return diag.ExitInternal
		}
		results[i] = r
		rep.Diagnostics = append(rep.Diagnostics, r.Diagnostics...)
	}

	// Write the generated files of every clean input — `gom test`
	// is a build as well as a run. go test runs only when the whole
	// input set compiled: a partial package would report errors
	// about code that is simply not regenerated.
	var writeErrs []error
	for i, r := range results {
		if r.Output == nil {
			continue
		}
		in := inputs[i]
		dst, err := outputPath(in.path, "", len(inputs))
		if err != nil {
			writeErrs = append(writeErrs, err)
			continue
		}
		if err := writeFile(dst, r.Output.Source); err != nil {
			writeErrs = append(writeErrs, fmt.Errorf("%s: %w", dst, err))
			continue
		}
		rep.Outputs = append(rep.Outputs, dst)
		tst := strings.TrimSuffix(dst, ".go") + "_test.go"
		switch {
		case r.Output.TestSource != nil:
			if err := writeFile(tst, r.Output.TestSource); err != nil {
				writeErrs = append(writeErrs, fmt.Errorf("%s: %w", tst, err))
				continue
			}
			rep.Outputs = append(rep.Outputs, tst)
		default:
			// The input now generates no tests: remove a stale
			// gom-generated test file so a later `go test` does not
			// run tests the source no longer declares. Files without
			// the generated marker are left alone.
			if isGomGenerated(tst) {
				if err := os.Remove(tst); err != nil {
					writeErrs = append(writeErrs, fmt.Errorf("%s: %w", tst, err))
				}
			}
		}
	}
	if len(writeErrs) > 0 {
		if err := emitReport(stdout, *format, rep, srcs); err != nil {
			fmt.Fprintf(stderr, "gom test: %v\n", err)
		}
		for _, e := range writeErrs {
			fmt.Fprintf(stderr, "gom test: cannot write output: %v\n", e)
		}
		return diag.ExitInternal
	}

	if !diag.List(rep.Diagnostics).HasError() {
		runGoTests(inputs, results, &rep)
	}
	if err := emitReport(stdout, *format, rep, srcs); err != nil {
		fmt.Fprintf(stderr, "gom test: %v\n", err)
		return diag.ExitInternal
	}
	return rep.ExitCode()
}

// testDir groups the inputs of one package directory for a single
// `go test` invocation.
type testDir struct {
	dir      string                         // absolute path go test runs in
	inputs   []input                        // .gom inputs of this package
	mappings map[string]codegen.TestMapping // generated Go func → `test "name"` decl
}

// runGoTests runs `go test -json` in each input directory (first-seen
// order) and folds the events into the report: "test" results per
// Go test function and E0001 diagnostics for toolchain failures.
func runGoTests(inputs []input, results []*build.Result, rep *diag.Report) {
	var dirs []*testDir
	byDir := map[string]*testDir{}
	for i, in := range inputs {
		r := results[i]
		if r.Output == nil {
			continue
		}
		dir, err := filepath.Abs(filepath.Dir(in.path))
		if err != nil {
			dir = filepath.Dir(in.path)
		}
		d := byDir[dir]
		if d == nil {
			d = &testDir{dir: dir, mappings: map[string]codegen.TestMapping{}}
			byDir[dir] = d
			dirs = append(dirs, d)
		}
		d.inputs = append(d.inputs, in)
		for _, m := range r.Output.Tests {
			d.mappings[m.Func] = m
		}
	}

	for _, d := range dirs {
		run, err := build.RunGoTest("", d.dir)
		if err != nil {
			// go test could not be invoked at all or broke its output
			// contract — the toolchain itself failed, which is a
			// go-tool error like a build failure.
			rep.Diagnostics = append(rep.Diagnostics,
				diag.Error("E0001", "go test could not run").
					From(diag.SourceGo).
					Set("tool", "go test").
					Set("error", err.Error()))
			continue
		}
		if len(run.Events) == 0 {
			// go test never reached the test binary — a missing
			// go.mod or unusable flags reports on stderr instead of
			// the event stream.
			if !run.Failed && strings.TrimSpace(run.Stderr) == "" {
				continue
			}
			dg := diag.Error("E0001", "go test failed before running tests").
				From(diag.SourceGo).
				Set("tool", "go test")
			if run.Stderr != "" {
				dg = dg.Set("output", run.Stderr)
			}
			rep.Diagnostics = append(rep.Diagnostics, dg)
			continue
		}
		res := build.FoldGoTest(run.Events)
		if res.BuildFailed {
			rep.Diagnostics = append(rep.Diagnostics, d.buildOutputDiagnostics(res.BuildOutput)...)
		}
		failed := false
		for _, o := range res.Tests {
			tr := diag.TestResult{
				Func:    o.Func,
				OK:      o.OK,
				Skipped: o.Skipped,
				Elapsed: o.Elapsed,
				Output:  o.Output,
			}
			if m, ok := d.mappings[o.Func]; ok {
				tr.Name, tr.Span = m.Name, m.NameSp
			} else {
				tr.Name = o.Func
			}
			if !tr.OK {
				failed = true
			}
			rep.Tests = append(rep.Tests, tr)
		}
		// A failed run no test explains — a panic in package init or
		// TestMain — reports its package-level output as a toolchain
		// error.
		if run.Failed && !failed && !res.BuildFailed {
			out := res.PackageOutput
			if out == "" {
				out = run.Stderr
			}
			dg := diag.Error("E0001", "go test failed without running a test").
				From(diag.SourceGo).
				Set("tool", "go test")
			if out != "" {
				dg = dg.Set("output", out)
			}
			rep.Diagnostics = append(rep.Diagnostics, dg)
		}
	}
}

// goErrorLine matches the "file:line:col: msg" head of a Go build
// error. The file group is greedy so a path containing ':' still
// resolves — the last two colon-separated fields before the message
// win.
var goErrorLine = regexp.MustCompile(`^(.+):([0-9]+):([0-9]+):[ \t]+(.*)$`)

// buildOutputDiagnostics turns a package's build-output events into
// E0001 diagnostics: each "file:line:col: msg" line maps back to the
// .gom input it names — the generated files' //line directives make
// gom positions appear verbatim in build errors. Lines that name no
// input (errors in hand-written .go files) and non-position lines
// stay positionless, carrying the raw text in data.
func (d *testDir) buildOutputDiagnostics(lines []string) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, chunk := range lines {
		for _, line := range strings.Split(strings.TrimRight(chunk, "\n"), "\n") {
			if line == "" || strings.HasPrefix(line, "# ") {
				continue // "# pkg [pkg.test]" headers name the package
			}
			dg := diag.Error("E0001", "go test reported a build error").
				From(diag.SourceGo).
				Set("tool", "go test").
				Set("raw", line)
			if m := goErrorLine.FindStringSubmatch(line); m != nil {
				if m[4] != "" {
					dg.Message = m[4]
				}
				dg = dg.Set("file", m[1])
				if ln, err := strconv.Atoi(m[2]); err == nil {
					dg = dg.Set("line", ln)
				}
				if col, err := strconv.Atoi(m[3]); err == nil {
					dg = dg.Set("col", col)
				}
				if sp, ok := d.resolveGoPos(m[1], atoi(m[2]), atoi(m[3])); ok {
					dg = dg.At(sp)
				}
			}
			out = append(out, dg)
		}
	}
	if len(out) == 0 {
		out = append(out, diag.Error("E0001", "go test reported a build error").
			From(diag.SourceGo).
			Set("tool", "go test"))
	}
	return out
}

// resolveGoPos maps a Go build error's file:line:col to a point span
// in the .gom source — the file is matched against the directory's
// inputs (as given on the command line, or relative to the run
// directory), then Go's 1-based byte column resolves against the
// input's line index.
func (d *testDir) resolveGoPos(file string, line, col int) (diag.Span, bool) {
	clean := strings.TrimPrefix(file, "./")
	for _, in := range d.inputs {
		if file != in.path && clean != in.path {
			abs, err := filepath.Abs(filepath.Join(d.dir, clean))
			if err != nil {
				continue
			}
			inAbs, err := filepath.Abs(in.path)
			if err != nil || abs != inAbs {
				continue
			}
		}
		li := diag.NewLineIndex(in.src)
		ls, le := li.LineBounds(line)
		off := ls + col - 1
		if off > le {
			off = le
		}
		return diag.Point(in.path, off), true
	}
	return diag.Span{}, false
}

// atoi converts a decimal string, returning 0 on any error.
func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// isGomGenerated reports whether path names an existing file that
// starts with gom's generated-code marker — the guard that keeps
// `gom test` from deleting a hand-written _test.go.
func isGomGenerated(path string) bool {
	src, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.HasPrefix(string(src), "// Code generated by gom. DO NOT EDIT.")
}
