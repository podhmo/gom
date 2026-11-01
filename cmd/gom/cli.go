package main

// This file holds the build/check/dump command bodies: flag parsing,
// input expansion, driving internal/build, and emitting the spec
// section 12 message stream. Everything here stays a thin layer —
// all compiler work lives in the internal packages.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/treedump"
)

// input is one source file the command will process.
type input struct {
	path  string // as the user refers to it — also the name spans carry
	src   []byte
	iface bool
}

// runCheck implements `gom check [-format F] [FILE|DIR ...]`:
// diagnostics only, nothing written (spec section 12).
func runCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "json", "diagnostic format: json or text")
	if err := fs.Parse(args); err != nil {
		return flagError(err)
	}
	if err := checkFormat(*format); err != nil {
		fmt.Fprintf(stderr, "gom check: %v\n", err)
		return diag.ExitInternal
	}
	inPaths, err := expandInputs(fs.Args(), true)
	if err != nil {
		fmt.Fprintf(stderr, "gom check: %v\n", err)
		return diag.ExitInternal
	}
	inputs, err := readInputs(inPaths)
	if err != nil {
		fmt.Fprintf(stderr, "gom check: %v\n", err)
		return diag.ExitInternal
	}

	opts := &build.Options{CheckOnly: true}
	rep := diag.Report{Files: inputNames(inputs)}
	srcs := map[string][]byte{}
	for _, in := range inputs {
		srcs[in.path] = in.src
		r, err := build.Compile(build.Input{Path: in.path, Src: in.src, Interface: in.iface}, opts)
		if err != nil {
			fmt.Fprintf(stderr, "gom check: %s: %v\n", in.path, err)
			return diag.ExitInternal
		}
		rep.Diagnostics = append(rep.Diagnostics, r.Diagnostics...)
	}
	if err := emitReport(stdout, *format, rep, srcs); err != nil {
		fmt.Fprintf(stderr, "gom check: %v\n", err)
		return diag.ExitInternal
	}
	return rep.ExitCode()
}

// runBuild implements `gom build [-format F] [-o out] [FILE|DIR ...]`:
// the full pipeline plus writing one .go per .gom input.
func runBuild(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "json", "diagnostic format: json or text")
	out := fs.String("o", "", "output file (single input) or directory")
	if err := fs.Parse(args); err != nil {
		return flagError(err)
	}
	if err := checkFormat(*format); err != nil {
		fmt.Fprintf(stderr, "gom build: %v\n", err)
		return diag.ExitInternal
	}
	inPaths, err := expandInputs(fs.Args(), false)
	if err != nil {
		fmt.Fprintf(stderr, "gom build: %v\n", err)
		return diag.ExitInternal
	}
	inputs, err := readInputs(inPaths)
	if err != nil {
		fmt.Fprintf(stderr, "gom build: %v\n", err)
		return diag.ExitInternal
	}

	rep := diag.Report{Files: inputNames(inputs)}
	srcs := map[string][]byte{}
	var results []*build.Result
	for _, in := range inputs {
		srcs[in.path] = in.src
		r, err := build.Compile(build.Input{Path: in.path, Src: in.src, Interface: in.iface}, nil)
		if err != nil {
			fmt.Fprintf(stderr, "gom build: %s: %v\n", in.path, err)
			return diag.ExitInternal
		}
		results = append(results, r)
		rep.Diagnostics = append(rep.Diagnostics, r.Diagnostics...)
	}

	// Resolve every output path before writing anything so a bad -o
	// fails fast instead of after partial writes.
	dsts := make([]string, len(inputs))
	for i, in := range inputs {
		if results[i].Output == nil {
			continue
		}
		dst, err := outputPath(in.path, *out, len(inputs))
		if err != nil {
			fmt.Fprintf(stderr, "gom build: %v\n", err)
			return diag.ExitInternal
		}
		dsts[i] = dst
	}

	// Write every input that produced output; failures are collected
	// and reported after the message stream — spec's summary lists
	// the files actually written.
	var writeErrs []error
	for i, r := range results {
		if r.Output == nil {
			continue
		}
		dst := dsts[i]
		if err := writeFile(dst, r.Output.Source); err != nil {
			writeErrs = append(writeErrs, fmt.Errorf("%s: %w", dst, err))
			continue
		}
		rep.Outputs = append(rep.Outputs, dst)
	}
	if err := emitReport(stdout, *format, rep, srcs); err != nil {
		fmt.Fprintf(stderr, "gom build: %v\n", err)
		return diag.ExitInternal
	}
	if len(writeErrs) > 0 {
		for _, e := range writeErrs {
			fmt.Fprintf(stderr, "gom build: cannot write output: %v\n", e)
		}
		return diag.ExitInternal
	}
	return rep.ExitCode()
}

// runDump implements `gom dump -phase=P FILE` — a developer tool
// printing one phase's IR tree to stdout. The diagnostic report goes
// to stderr so stdout stays the clean artifact; diagnostics never
// suppress the dump.
func runDump(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dump", flag.ContinueOnError)
	fs.SetOutput(stderr)
	phase := fs.String("phase", "", "phase to dump: syntax, resolve, check, or core")
	format := fs.String("format", "json", "diagnostic format: json or text")
	if err := fs.Parse(args); err != nil {
		return flagError(err)
	}
	if err := checkFormat(*format); err != nil {
		fmt.Fprintf(stderr, "gom dump: %v\n", err)
		return diag.ExitInternal
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "gom dump: exactly one input file is required\n")
		return diag.ExitInternal
	}
	inPaths, err := expandInputs(fs.Args(), true)
	if err != nil {
		fmt.Fprintf(stderr, "gom dump: %v\n", err)
		return diag.ExitInternal
	}
	if len(inPaths) != 1 {
		fmt.Fprintf(stderr, "gom dump: exactly one input file is required (a directory expands to several)\n")
		return diag.ExitInternal
	}
	inputs, err := readInputs(inPaths)
	if err != nil {
		fmt.Fprintf(stderr, "gom dump: %v\n", err)
		return diag.ExitInternal
	}
	in := inputs[0]

	r, err := build.Compile(build.Input{Path: in.path, Src: in.src, Interface: in.iface}, nil)
	if err != nil {
		fmt.Fprintf(stderr, "gom dump: %s: %v\n", in.path, err)
		return diag.ExitInternal
	}
	rep := diag.Report{Diagnostics: r.Diagnostics, Files: []string{in.path}}
	if err := emitReport(stderr, *format, rep, map[string][]byte{in.path: in.src}); err != nil {
		fmt.Fprintf(stderr, "gom dump: %v\n", err)
		return diag.ExitInternal
	}

	var text string
	switch *phase {
	case "syntax":
		text = treedump.DumpSyntax(r.File)
	case "resolve":
		if r.Resolved != nil {
			text = treedump.DumpResolved(r.Resolved)
		}
	case "check":
		if r.Typed != nil {
			text = treedump.DumpTyped(r.Typed)
		}
	case "core":
		if r.Core != nil {
			text = treedump.DumpCore(r.Core)
		}
	default:
		fmt.Fprintf(stderr, "gom dump: unknown phase %q (want syntax, resolve, check, or core)\n", *phase)
		return diag.ExitInternal
	}
	if text == "" {
		fmt.Fprintf(stderr, "gom dump: the pipeline did not reach phase %s\n", *phase)
		return rep.ExitCode()
	}
	fmt.Fprint(stdout, text)
	return rep.ExitCode()
}

// expandInputs resolves command arguments into the ordered,
// deduplicated list of source files to process: .gom files always,
// .gomi files only when includeIface is set (gom check/dump accept
// them; gom build does not). A directory expands to its direct *.gom
// (and *.gomi) children, sorted; a missing path or a file of the
// wrong kind is an error. No arguments means the current directory.
func expandInputs(args []string, includeIface bool) ([]string, error) {
	if len(args) == 0 {
		args = []string{"."}
	}
	var out []string
	seen := map[string]bool{}
	add := func(p string) error {
		if seen[p] {
			return nil
		}
		if build.IsInterfaceFile(p) {
			if !includeIface {
				return fmt.Errorf("%s is an interface file and produces no output", p)
			}
		} else if !strings.HasSuffix(p, ".gom") {
			return fmt.Errorf("%s is not a .gom source file", p)
		}
		seen[p] = true
		out = append(out, p)
		return nil
	}
	for _, arg := range args {
		st, err := os.Stat(arg)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			if err := add(arg); err != nil {
				return nil, err
			}
			continue
		}
		var matches []string
		for _, pat := range []string{"*.gom", "*.gomi"} {
			ms, err := filepath.Glob(filepath.Join(arg, pat))
			if err != nil {
				return nil, err
			}
			matches = append(matches, ms...)
		}
		sort.Strings(matches)
		if len(matches) == 0 {
			return nil, fmt.Errorf("no .gom files in %s", arg)
		}
		var added int
		for _, m := range matches {
			// Directories whose names end in .gom/.gomi are skipped.
			if st, err := os.Stat(m); err != nil || st.IsDir() {
				continue
			}
			if err := add(m); err != nil {
				if build.IsInterfaceFile(m) {
					continue // .gomi is not an error when excluded
				}
				return nil, err
			}
			added++
		}
		if added == 0 {
			return nil, fmt.Errorf("no compilable .gom files in %s", arg)
		}
	}
	return out, nil
}

// readInputs loads each path's contents.
func readInputs(paths []string) ([]input, error) {
	inputs := make([]input, 0, len(paths))
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", p, err)
		}
		inputs = append(inputs, input{path: p, src: src, iface: build.IsInterfaceFile(p)})
	}
	return inputs, nil
}

// inputNames returns the report's file list.
func inputNames(inputs []input) []string {
	names := make([]string, len(inputs))
	for i, in := range inputs {
		names[i] = in.path
	}
	return names
}

// outputPath computes the .go path for one input: beside the input
// by default; under the -o directory when it names one; exactly -o
// when it names a file, which is allowed only for a single input.
func outputPath(inPath, out string, nInputs int) (string, error) {
	base := strings.TrimSuffix(filepath.Base(inPath), ".gom") + ".go"
	switch {
	case out == "":
		return filepath.Join(filepath.Dir(inPath), base), nil
	case out == "-":
		// stdout carries the diagnostic stream (spec section 12);
		// generated Go on the same stream would corrupt it.
		return "", fmt.Errorf("-o - would write generated code to stdout, which is the diagnostic stream; use a file or directory")
	case strings.HasSuffix(out, "/") || strings.HasSuffix(out, string(filepath.Separator)):
		return filepath.Join(strings.TrimSuffix(out, string(filepath.Separator)), base), nil
	default:
		if st, err := os.Stat(out); err == nil && st.IsDir() {
			return filepath.Join(out, base), nil
		}
		if nInputs != 1 {
			return "", fmt.Errorf("-o %s names a file, which requires a single input", out)
		}
		return out, nil
	}
}

// writeFile writes generated source, creating the parent directory.
func writeFile(path string, src []byte) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, src, 0o644)
}

// checkFormat validates -format up front so a typo does not surface
// only after the whole compile ran.
func checkFormat(format string) error {
	switch format {
	case "json", "text":
		return nil
	default:
		return fmt.Errorf("unknown -format %q (want json or text)", format)
	}
}

// emitReport renders rep in format to w: JSON Lines or text per spec
// section 12. The format has already passed checkFormat.
func emitReport(w io.Writer, format string, rep diag.Report, srcs map[string][]byte) error {
	res := diag.NewMapResolver(srcs)
	if format == "text" {
		return rep.WriteText(w, res, diag.TextOptions{})
	}
	return rep.WriteJSONLines(w, res)
}

// flagError maps flag.Parse's result to an exit code: ErrHelp means
// the command's usage was requested (flag prints it to stderr
// already) — success; anything else is a usage error.
func flagError(err error) int {
	if err == flag.ErrHelp {
		return diag.ExitSuccess
	}
	return diag.ExitInternal
}
