package main

// This file holds the fix/explain/schema command bodies — the
// spec section 12 tooling commands beyond build/check/fmt. Like
// cli.go everything here stays a thin layer: the fix machinery is
// policy-free plumbing over internal/build results and the
// internal/diag edit model.

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/podhmo/gom/internal/diag"
)

// runFix implements `gom fix [-format F] [FILE|DIR ...]` (spec
// section 12): compile the inputs, apply every fix the diagnostics
// mark safe, rewrite the touched files, and report each applied fix
// as an "applied" message followed by the "summary" object.
//
// Application policy (decisions/gom-fix-explain-schema):
//   - only ApplicabilitySafe fixes apply — placeholder and unsafe
//     suggestions stay the user's call;
//   - a diagnostic's fixes are alternatives: the first safe one that
//     applies wins, at most one fix per diagnostic;
//   - a fix applies atomically — it is skipped when any of its edits
//     overlaps an already-applied edit or names a file that is not
//     among the inputs;
//   - the run is a single pass: fixes made visible by earlier edits
//     appear on the next `gom fix` run.
//
// The diagnostics the compilation produced are not part of fix's
// stream (the spec's kind table assigns fix only "applied" and
// "summary"), so a run that applied what it could exits 0 even when
// the source still has errors — `gom check` reports them.
func runFix(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fix", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "json", "diagnostic format: json or text")
	if err := fs.Parse(args); err != nil {
		return flagError(err)
	}
	if err := checkFormat(*format); err != nil {
		fmt.Fprintf(stderr, "gom fix: %v\n", err)
		return diag.ExitInternal
	}
	inPaths, err := expandInputs(fs.Args(), true)
	if err != nil {
		fmt.Fprintf(stderr, "gom fix: %v\n", err)
		return diag.ExitInternal
	}
	inputs, err := readInputs(inPaths)
	if err != nil {
		fmt.Fprintf(stderr, "gom fix: %v\n", err)
		return diag.ExitInternal
	}

	srcs := map[string][]byte{}
	for _, in := range inputs {
		srcs[in.path] = in.src
	}
	// The .gom files of one directory compile as one package unit —
	// fixes must see the same diagnostics `gom check` reports, so
	// they attach to the same shared scope (sibling references
	// resolve package-wide).
	results, err := compileInputs(inputs, true, nil)
	if err != nil {
		fmt.Fprintf(stderr, "gom fix: %v\n", err)
		return diag.ExitInternal
	}
	var diags diag.List
	for _, r := range results {
		diags = append(diags, r.Diagnostics...)
	}
	// Apply in output order so the accepted set never depends on
	// collection order.
	diags.Sort()

	applied := planFixes(diags, inputSet(inputs))

	// Land the accepted edits, one write per touched file; summary
	// outputs are the files actually rewritten.
	rep := diag.Report{Files: inputNames(inputs)}
	var writeErrs []error
	for _, in := range inputs {
		var edits []diag.Edit
		for _, a := range applied {
			for _, e := range a.Edits {
				if e.Span.File == in.path {
					edits = append(edits, e)
				}
			}
		}
		if len(edits) == 0 {
			continue
		}
		out, err := diag.ApplyEdits(in.src, edits)
		if err != nil {
			// planFixes already validated bounds and overlap —
			// reaching this is a bug there.
			fmt.Fprintf(stderr, "gom fix: %s: %v\n", in.path, err)
			return diag.ExitInternal
		}
		if bytes.Equal(out, in.src) {
			continue
		}
		if err := os.WriteFile(in.path, out, 0o644); err != nil {
			writeErrs = append(writeErrs, fmt.Errorf("%s: %w", in.path, err))
			continue
		}
		rep.Outputs = append(rep.Outputs, in.path)
	}

	res := diag.NewMapResolver(srcs)
	if err := emitFixStream(stdout, *format, rep, applied, res); err != nil {
		fmt.Fprintf(stderr, "gom fix: %v\n", err)
		return diag.ExitInternal
	}
	for _, e := range writeErrs {
		fmt.Fprintf(stderr, "gom fix: cannot write output: %v\n", e)
	}
	if len(writeErrs) > 0 {
		return diag.ExitInternal
	}
	return rep.ExitCode()
}

// inputSet returns the set of input paths — the files gom fix is
// allowed to rewrite.
func inputSet(inputs []input) map[string]bool {
	s := make(map[string]bool, len(inputs))
	for _, in := range inputs {
		s[in.path] = true
	}
	return s
}

// planFixes selects the fixes gom fix applies: the first safe fix of
// each diagnostic whose edits all stay inside the allowed files and
// do not overlap an already-accepted edit. Diagnostics are expected
// in output order (List.Sort) — the order fixes are considered in,
// which the accepted set depends on.
func planFixes(diags diag.List, files map[string]bool) []diag.Applied {
	taken := map[string][]diag.Span{} // file → accepted edit spans
	var applied []diag.Applied
	for _, d := range diags {
		for _, f := range d.Fixes {
			if f.Applicability != diag.ApplicabilitySafe || len(f.Edits) == 0 {
				continue
			}
			if !fixApplicable(f, files, taken) {
				continue
			}
			applied = append(applied,
				diag.Applied{Code: d.Code, Title: f.Title, Edits: f.Edits})
			for _, e := range f.Edits {
				taken[e.Span.File] = append(taken[e.Span.File], e.Span)
			}
			break // one fix per diagnostic
		}
	}
	return applied
}

// fixApplicable reports whether every edit of f lands inside the
// allowed files without overlapping an already-accepted edit.
// Insertions at a span's edge do not overlap it.
func fixApplicable(f diag.Fix, files map[string]bool, taken map[string][]diag.Span) bool {
	for _, e := range f.Edits {
		if !e.Span.Valid() || !files[e.Span.File] {
			return false
		}
		for _, t := range taken[e.Span.File] {
			if e.Span.Start < t.End && t.Start < e.Span.End {
				return false
			}
		}
	}
	return true
}

// emitFixStream renders the run's "applied" messages followed by the
// trailing summary in the requested format — the spec section 12
// stream for `gom fix`.
func emitFixStream(w io.Writer, format string, rep diag.Report, applied []diag.Applied, res diag.Resolver) error {
	if err := diag.WriteApplied(w, format, applied, res); err != nil {
		return err
	}
	if format == "text" {
		return rep.WriteText(w, res, diag.TextOptions{})
	}
	raw, err := rep.Summary().MarshalJSON()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", raw)
	return err
}

// runExplain implements `gom explain [-format F] [CODE ...]` (spec
// section 12): each code's error-code-table entry rendered as an
// "explanation" object (json) or a rustc-style block (text). With
// no code the JSON stream holds every registered code's entry and
// the text output is the compact index; a summary closes the JSON
// stream either way.
func runExplain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "json", "diagnostic format: json or text")
	if err := fs.Parse(args); err != nil {
		return flagError(err)
	}
	if err := checkFormat(*format); err != nil {
		fmt.Fprintf(stderr, "gom explain: %v\n", err)
		return diag.ExitInternal
	}

	if fs.NArg() == 0 {
		if *format == "text" {
			if err := diag.WriteCodeIndex(stdout, diag.CodeTable()); err != nil {
				fmt.Fprintf(stderr, "gom explain: %v\n", err)
				return diag.ExitInternal
			}
			return diag.ExitSuccess
		}
		var es []diag.Explanation
		for _, info := range diag.CodeTable() {
			es = append(es, diag.ExplanationOf(info))
		}
		if err := diag.WriteExplanations(stdout, *format, es); err != nil {
			fmt.Fprintf(stderr, "gom explain: %v\n", err)
			return diag.ExitInternal
		}
		return writeSummaryLine(stdout)
	}

	var es []diag.Explanation
	for _, a := range fs.Args() {
		c := diag.Code(strings.ToUpper(a))
		if !diag.WellFormed(c) {
			fmt.Fprintf(stderr, "gom explain: malformed code %q (want LCCNN, e.g. E0301)\n", a)
			return diag.ExitInternal
		}
		info, ok := diag.LookupCode(c)
		if !ok {
			fmt.Fprintf(stderr, "gom explain: no diagnostic code %s is registered\n", c)
			return diag.ExitInternal
		}
		es = append(es, diag.ExplanationOf(info))
	}
	if err := diag.WriteExplanations(stdout, *format, es); err != nil {
		fmt.Fprintf(stderr, "gom explain: %v\n", err)
		return diag.ExitInternal
	}
	if *format == "json" {
		return writeSummaryLine(stdout)
	}
	return diag.ExitSuccess
}

// writeSummaryLine emits the closing "summary" object of a stream
// that carries no diagnostics — for gom explain, which processes no
// source files.
func writeSummaryLine(w io.Writer) int {
	raw, err := (diag.Report{}).Summary().MarshalJSON()
	if err != nil {
		return diag.ExitInternal
	}
	if _, err := fmt.Fprintf(w, "%s\n", raw); err != nil {
		return diag.ExitInternal
	}
	return diag.ExitSuccess
}

// runSchema implements `gom schema` (spec section 12, item 7): the
// message format's JSON Schema on stdout. It is an artifact like
// `gom fmt`'s output, not a message-stream row, so no summary
// follows it.
func runSchema(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("schema", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return flagError(err)
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "gom schema: takes no arguments\n")
		return diag.ExitInternal
	}
	if _, err := stdout.Write(diag.MessageSchema()); err != nil {
		fmt.Fprintf(stderr, "gom schema: %v\n", err)
		return diag.ExitInternal
	}
	return diag.ExitSuccess
}
