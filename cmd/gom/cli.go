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
	"github.com/podhmo/gom/internal/format"
	"github.com/podhmo/gom/internal/goimport"
	"github.com/podhmo/gom/internal/gomimport"
	"github.com/podhmo/gom/internal/iface"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/resolve"
	gomstd "github.com/podhmo/gom/internal/stdlib"
	"github.com/podhmo/gom/internal/treedump"
)

// input is one source file the command will process.
type input struct {
	path  string // as the user refers to it — also the name spans carry
	src   []byte
	iface bool
}

// compileOptions builds the driver options for one input: a
// goimport.Loader behind the gomimport composite answers both
// `import go` and `import` — the resolve loader, signature oracle,
// constructor-field tables and lowering's GoSig/type/arity hooks
// all come from the two sessions — plus the standard library's
// builtin-module tables and the .gomi export hook of spec section
// 5. The loaders run in the input file's own directory so imported
// packages resolve in the module the file belongs to.
func compileOptions(in input, checkOnly bool) *build.Options {
	dir := filepath.Dir(in.path)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	gl := goimport.New(&goimport.Config{Dir: dir})
	mods := map[string]map[string]*resolve.Symbol{}
	ml := gomimport.New(&gomimport.Config{Dir: dir, Go: gl, Sigs: gl, Modules: mods})
	return gomstd.Apply(&build.Options{
		Loader:         ml,
		Sigs:           ml,
		Modules:        mods,
		Lower:          &lower.Env{GoSig: gl.GoSigOf, Types: ml, FuncArity: ml.ArityOf},
		Exports:        iface.ExportNames,
		SelfImportPath: ml.EntryPath(),
		CheckOnly:      checkOnly,
	})
}

// compileInputs compiles every input and returns one *build.Result
// per input, in input order. The .gom files of one directory go
// through build.CompileFiles as one package unit — the package is
// the resolve/check unit (docs/decisions/2026-11-05-package-
// compilation-unit.md) — while each .gomi input compiles
// individually through build.Compile (an interface is its own
// scope). Diagnostics of all results concatenate in input order.
func compileInputs(inputs []input, checkOnly bool, tweak func(*build.Options)) ([]*build.Result, error) {
	results := make([]*build.Result, len(inputs))
	groups := map[string][]int{}
	var order []string
	for i, in := range inputs {
		if in.iface {
			r, err := build.Compile(build.Input{Path: in.path, Src: in.src, Interface: true},
				compileOptions(in, checkOnly))
			if err != nil {
				return nil, fmt.Errorf("%s: %w", in.path, err)
			}
			results[i] = r
			continue
		}
		// Clean the path before grouping — `pkg/../pkg/b.gom` and
		// `pkg/a.gom` name the same directory, and an unnormalized dir
		// would split one package into two units.
		dir := filepath.Dir(filepath.Clean(in.path))
		if _, ok := groups[dir]; !ok {
			order = append(order, dir)
		}
		groups[dir] = append(groups[dir], i)
	}
	for _, dir := range order {
		idxs := groups[dir]
		ins := make([]build.Input, len(idxs))
		for j, i := range idxs {
			ins[j] = build.Input{Path: inputs[i].path, Src: inputs[i].src}
		}
		opts := compileOptions(inputs[idxs[0]], checkOnly)
		if tweak != nil {
			tweak(opts)
		}
		rs, err := build.CompileFiles(ins, opts)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", dir, err)
		}
		for j, i := range idxs {
			results[i] = rs[j]
		}
	}
	return results, nil
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

	rep := diag.Report{Files: inputNames(inputs)}
	srcs := map[string][]byte{}
	for _, in := range inputs {
		srcs[in.path] = in.src
	}
	results, err := compileInputs(inputs, true, nil)
	if err != nil {
		fmt.Fprintf(stderr, "gom check: %v\n", err)
		return diag.ExitInternal
	}
	for _, r := range results {
		rep.Diagnostics = append(rep.Diagnostics, r.Diagnostics...)
	}
	// The interface contract binds the package, not the argument
	// list: sibling .gomi files are discovered and compared
	// (spec section 7.4–7.6).
	checkInterfaces(inputs, results, &rep, srcs)
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
	// -o is a usage contract checked before compiling: stdout can
	// never carry generated code (it is the diagnostic stream), and
	// a file destination only takes a single input — both must fail
	// even when every package's outputs end up withheld.
	if *out == "-" {
		fmt.Fprintf(stderr, "gom build: -o - would write generated code to stdout, which is the diagnostic stream; use a file or directory\n")
		return diag.ExitInternal
	}
	if *out != "" && !isDirFlag(*out) && len(inputs) != 1 {
		fmt.Fprintf(stderr, "gom build: -o %s names a file, which requires a single input\n", *out)
		return diag.ExitInternal
	}

	rep := diag.Report{Files: inputNames(inputs)}
	srcs := map[string][]byte{}
	for _, in := range inputs {
		srcs[in.path] = in.src
	}
	results, err := compileInputs(inputs, false, nil)
	if err != nil {
		fmt.Fprintf(stderr, "gom build: %v\n", err)
		return diag.ExitInternal
	}
	for _, r := range results {
		rep.Diagnostics = append(rep.Diagnostics, r.Diagnostics...)
	}
	// Same package-level interface pass as gom check — the .gomi
	// contract binds the package even though codegen is per-file.
	// Results of failed groups are withheld below: a package whose
	// contract failed (E0601/E0603, or an unverifiable interface)
	// writes no output — generated code is only kept for packages
	// that pass whole.
	failed := checkInterfaces(inputs, results, &rep, srcs)

	// Resolve every output path before writing anything so a bad -o
	// fails fast instead of after partial writes.
	dsts := make([]string, len(inputs))
	for i, in := range inputs {
		if results[i].Output == nil || failed[results[i]] {
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
		if r.Output == nil || failed[r] {
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

	// A .gom file is not a compile unit by itself — the package is.
	// Compiling only the named file would attribute E0201s to names
	// its sibling files declare, so the dump gathers the directory's
	// .gom files into one CompileFiles unit (the same grouping
	// `gom build` gives a directory argument). The dump itself still
	// prints only the named file's IR.
	var r *build.Result
	rep := diag.Report{}
	sources := map[string][]byte{}
	if !in.iface {
		dir := filepath.Dir(filepath.Clean(in.path))
		sibs, _ := filepath.Glob(filepath.Join(dir, "*.gom"))
		var ins []build.Input
		sources[in.path] = in.src
		self := filepath.Clean(in.path)
		for _, s := range sibs {
			if filepath.Clean(s) == self {
				ins = append(ins, build.Input{Path: in.path, Src: in.src})
				continue
			}
			b, err := os.ReadFile(s)
			if err != nil {
				continue
			}
			sources[s] = b
			ins = append(ins, build.Input{Path: s, Src: b})
		}
		if len(ins) > 1 {
			rs, err := build.CompileFiles(ins, compileOptions(in, false))
			if err != nil {
				fmt.Fprintf(stderr, "gom dump: %s: %v\n", dir, err)
				return diag.ExitInternal
			}
			for j, sj := range ins {
				rep.Diagnostics = append(rep.Diagnostics, rs[j].Diagnostics...)
				rep.Files = append(rep.Files, sj.Path)
				if filepath.Clean(sj.Path) == self {
					r = rs[j]
				}
			}
		}
	}
	if r == nil {
		r, err = build.Compile(build.Input{Path: in.path, Src: in.src, Interface: in.iface},
			compileOptions(in, false))
		if err != nil {
			fmt.Fprintf(stderr, "gom dump: %s: %v\n", in.path, err)
			return diag.ExitInternal
		}
		rep = diag.Report{Diagnostics: r.Diagnostics, Files: []string{in.path}}
		sources = map[string][]byte{in.path: in.src}
	}
	if err := emitReport(stderr, *format, rep, sources); err != nil {
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

// runFmt implements `gom fmt [flags] [FILE|DIR ...]` — spec 1.7's
// canonical formatter. stdout is reserved for the formatted artifact:
// the spec-12 message stream (diagnostics, unformatted messages and
// the summary, rendered per -format) goes to stderr, mirroring gofmt's
// quiet-stdout convention and the same split `gom dump` uses. That
// keeps `gom fmt a.gom > a.gom` safe and `-l` / `-d` output pipeable.
//
//	gom fmt FILE...       formatted output on stdout
//	gom fmt -w FILE...    rewrite files in place
//	gom fmt -d FILE...    print unified diffs
//	gom fmt -l FILE...    list files whose formatting differs
//	gom fmt --check F...  "unformatted" messages + diff (exit 1 when dirty)
//	gom fmt -             read stdin, write stdout
func runFmt(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fmt", flag.ContinueOnError)
	fs.SetOutput(stderr)
	w := fs.Bool("w", false, "write result to source file instead of stdout")
	d := fs.Bool("d", false, "display diffs instead of rewriting files")
	l := fs.Bool("l", false, "list files whose formatting differs from gom fmt's")
	check := fs.Bool("check", false, "report files needing formatting as unformatted messages and fail")
	fmtFormat := fs.String("format", "json", "diagnostic format: json or text")
	if err := fs.Parse(args); err != nil {
		return flagError(err)
	}
	if err := checkFormat(*fmtFormat); err != nil {
		fmt.Fprintf(stderr, "gom fmt: %v\n", err)
		return diag.ExitInternal
	}
	if *check && (*w || *d || *l) {
		fmt.Fprintf(stderr, "gom fmt: --check cannot be combined with -w, -d or -l\n")
		return diag.ExitInternal
	}

	// Stdin mode: exactly "-" reads the source from stdin.
	if fs.NArg() == 1 && fs.Arg(0) == "-" {
		if *w || *d || *l || *check {
			fmt.Fprintf(stderr, "gom fmt: - reads stdin and writes stdout; combine it with no other flag\n")
			return diag.ExitInternal
		}
		src, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(stderr, "gom fmt: cannot read stdin: %v\n", err)
			return diag.ExitInternal
		}
		out, diags := format.Source(src, "<stdin>", false)
		rep := diag.Report{Diagnostics: diags, Files: []string{"<stdin>"}}
		if err := emitReport(stderr, *fmtFormat, rep, map[string][]byte{"<stdin>": src}); err != nil {
			fmt.Fprintf(stderr, "gom fmt: %v\n", err)
			return diag.ExitInternal
		}
		if diags.HasError() {
			return rep.ExitCode()
		}
		if _, err := stdout.Write(out); err != nil {
			fmt.Fprintf(stderr, "gom fmt: cannot write stdout: %v\n", err)
			return diag.ExitInternal
		}
		return rep.ExitCode()
	}

	inPaths, err := expandInputs(fs.Args(), true)
	if err != nil {
		fmt.Fprintf(stderr, "gom fmt: %v\n", err)
		return diag.ExitInternal
	}
	inputs, err := readInputs(inPaths)
	if err != nil {
		fmt.Fprintf(stderr, "gom fmt: %v\n", err)
		return diag.ExitInternal
	}

	rep := diag.Report{Files: inputNames(inputs)}
	srcs := map[string][]byte{}
	var unformatted []diag.Unformatted
	var writeErrs []error
	for _, in := range inputs {
		srcs[in.path] = in.src
		out, diags := format.Source(in.src, in.path, in.iface)
		rep.Diagnostics = append(rep.Diagnostics, diags...)
		if diags.HasError() {
			continue
		}
		changed := string(out) != string(in.src)
		switch {
		case *check:
			if changed {
				unformatted = append(unformatted,
					diag.Unformatted{File: in.path, Diff: format.Diff(in.path, in.src, out)})
			}
		case *w:
			if !changed {
				continue
			}
			if err := writeFile(in.path, out); err != nil {
				writeErrs = append(writeErrs, fmt.Errorf("%s: %w", in.path, err))
				continue
			}
			rep.Outputs = append(rep.Outputs, in.path)
		case *d:
			if changed {
				fmt.Fprint(stdout, format.Diff(in.path, in.src, out))
			}
		case *l:
			if changed {
				fmt.Fprintln(stdout, in.path)
			}
		default:
			if _, err := stdout.Write(out); err != nil {
				writeErrs = append(writeErrs, fmt.Errorf("stdout: %w", err))
			}
		}
	}
	// stdout carries only the artifact above; the unformatted/summary
	// message stream goes to stderr so the artifact pipes cleanly
	// (the same split the stdin path uses).
	if err := diag.WriteUnformatted(stderr, *fmtFormat, unformatted); err != nil {
		fmt.Fprintf(stderr, "gom fmt: %v\n", err)
		return diag.ExitInternal
	}
	if err := emitReport(stderr, *fmtFormat, rep, srcs); err != nil {
		fmt.Fprintf(stderr, "gom fmt: %v\n", err)
		return diag.ExitInternal
	}
	for _, e := range writeErrs {
		fmt.Fprintf(stderr, "gom fmt: cannot write output: %v\n", e)
	}
	if len(writeErrs) > 0 {
		return diag.ExitInternal
	}
	// --check is a gate: unformatted files fail the run the way error
	// diagnostics do.
	if *check && len(unformatted) > 0 {
		return diag.ExitError
	}
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
