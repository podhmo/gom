package main

// This file holds the package-level interface machinery: the
// implementation↔.gomi conformance pass shared by `gom check` and
// `gom build`, and `gom info`'s draft generation (docs/spec.md
// section 7; docs/sketch/2026-11-05-moon-cli-command-structure.md).
// Like the rest of cmd/gom it stays a thin layer — the comparison
// itself lives in internal/iface, compilation in internal/build.

import (
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/iface"
	"github.com/podhmo/gom/internal/syntax"
)

// --- the conformance pass ---

// pkgGroup is one package's implementation files within one
// directory, as far as this run knows them: a package is a directory
// concept even though the driver compiles file-by-file.
type pkgGroup struct {
	dir     string
	name    string
	results []*build.Result
	inputs  []input
	errored bool // a member produced an error diagnostic
}

// checkInterfaces runs the spec section 7.4–7.6 pass after every
// input compiled: each library package gets exactly one
// "<package>.gomi"; the implementation must match it (E0603), a
// missing one is E0601, a .gomi whose name fits no package in its
// directory is stray (E0604). Diagnostics append to rep; sources of
// discovered files join srcs so their spans resolve.
//
// The pass is directory-scoped: it discovers sibling .gomi files
// even when the user passed one file, because the contract binds
// the package, not the argument list. Package clauses of
// not-compiled siblings are parsed for the completeness census —
// their own diagnostics stay out (they are not inputs).
func checkInterfaces(inputs []input, results []*build.Result, rep *diag.Report, srcs map[string][]byte) {
	groups := map[string]*pkgGroup{}
	gomiInputs := map[string]int{} // path → input index
	for i, in := range inputs {
		dir := filepath.Dir(in.path)
		if in.iface {
			gomiInputs[in.path] = i
			continue
		}
		r := results[i]
		if r == nil || r.Typed == nil {
			continue
		}
		key := dir + "\x00" + r.Typed.Package
		g := groups[key]
		if g == nil {
			g = &pkgGroup{dir: dir, name: r.Typed.Package}
			groups[key] = g
		}
		g.results = append(g.results, r)
		g.inputs = append(g.inputs, in)
		if r.Diagnostics.HasError() {
			g.errored = true
		}
	}

	// Deterministic order: by directory, then package name.
	keys := slices.Sorted(maps.Keys(groups))
	dirs := map[string]bool{}
	for _, k := range keys {
		dirs[groups[k].dir] = true
	}

	for _, dir := range slices.Sorted(maps.Keys(dirs)) {
		// The .gomi files the directory holds — inputs plus any
		// siblings discovered on disk.
		gomiSet := map[string]bool{}
		for _, m := range filepathGomis(dir) {
			gomiSet[m] = true
		}
		for p := range gomiInputs {
			if filepath.Dir(p) == dir {
				gomiSet[p] = true
			}
		}
		gomis := slices.Sorted(maps.Keys(gomiSet))
		consumed := map[string]bool{}

		var census map[string]int // lazily: package name → .gom count
		for _, key := range keys {
			g := groups[key]
			if g.dir != dir || g.name == "main" {
				// package main is the program root — it has no
				// interface contract (spec section 7.4).
				continue
			}
			want := filepath.Join(dir, g.name+".gomi")
			var gomiRes *build.Result
			if idx, ok := gomiInputs[want]; ok {
				gomiRes = results[idx]
			} else if b, err := os.ReadFile(want); err == nil {
				gomiRes = compileInput(input{path: want, src: b, iface: true})
				if gomiRes != nil {
					rep.Diagnostics = append(rep.Diagnostics, gomiRes.Diagnostics...)
					srcs[want] = b
				}
			}
			if gomiRes == nil {
				rep.Diagnostics = append(rep.Diagnostics, missingInterface(g))
				continue
			}
			consumed[want] = true
			if gomiRes.Diagnostics.HasError() || gomiRes.Typed == nil || g.errored {
				continue // own diagnostics explain; no noisy E0603
			}
			if census == nil {
				census = dirCensus(dir, inputs, results)
			}
			complete := census[g.name] == len(g.inputs)
			impl := &iface.Package{Members: map[string]*iface.Member{}}
			for _, r := range g.results {
				impl.Merge(iface.Surface(r.Typed))
			}
			rep.Diagnostics = append(rep.Diagnostics,
				iface.Check(iface.Surface(gomiRes.Typed), impl, complete)...)
		}

		// Strays: a .gomi is the interface of package <name> iff it
		// is named <name>.gomi — anything else can never be
		// consumed.
		for _, path := range gomis {
			if consumed[path] {
				continue
			}
			declared, sp := gomiDecl(path, gomiInputs, inputs, results)
			if d := strayInterface(path, declared, sp); d != nil {
				rep.Diagnostics = append(rep.Diagnostics, *d)
			}
		}
	}
}

// filepathGomis lists the .gomi files directly inside dir.
func filepathGomis(dir string) []string {
	ms, err := filepath.Glob(filepath.Join(dir, "*.gomi"))
	if err != nil {
		return nil
	}
	slices.Sort(ms)
	return ms
}

// dirCensus counts .gom files per package name in dir: compiled
// inputs contribute their typed package; files that were not inputs
// get parsed for their package clause only (their own diagnostics
// stay out — they were not asked for).
func dirCensus(dir string, inputs []input, results []*build.Result) map[string]int {
	byPath := map[string]string{}
	for i, in := range inputs {
		if in.iface {
			continue
		}
		if r := results[i]; r != nil && r.Typed != nil {
			byPath[in.path] = r.Typed.Package
		}
	}
	census := map[string]int{}
	ms, err := filepath.Glob(filepath.Join(dir, "*.gom"))
	if err != nil {
		return census
	}
	for _, p := range ms {
		if pkg, ok := byPath[p]; ok {
			census[pkg]++
			continue
		}
		src, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		f, _ := syntax.Parse(src, p)
		if f != nil {
			census[f.PackageName()]++
		}
	}
	return census
}

// gomiDecl reports the package a .gomi file declares and its clause
// span. Input files reuse their compiled result; discovered strays
// get a cheap parse — the file is flagged for its name, not checked.
func gomiDecl(path string, gomiInputs map[string]int, inputs []input, results []*build.Result) (string, diag.Span) {
	if idx, ok := gomiInputs[path]; ok {
		r := results[idx]
		if r != nil && r.Typed != nil {
			return r.Typed.Package, r.Typed.PkgSp
		}
		return "", diag.Point(path, 0)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return "", diag.Point(path, 0)
	}
	f, _ := syntax.ParseInterface(src, path)
	if f == nil {
		return "", diag.Point(path, 0)
	}
	var sp diag.Span
	if f.Package != nil {
		sp = f.Package.Span()
	} else {
		sp = diag.Point(path, 0)
	}
	return f.PackageName(), sp
}

// missingInterface is the E0601 diagnostic: no <package>.gomi in the
// package's directory.
func missingInterface(g *pkgGroup) diag.Diagnostic {
	want := filepath.Join(g.dir, g.name+".gomi")
	sp := diag.Point(g.inputs[0].path, 0)
	if g.results[0].Typed != nil && g.results[0].Typed.PkgSp.End > 0 {
		sp = g.results[0].Typed.PkgSp
	}
	return diag.Error("E0601",
		fmt.Sprintf("package %s has no interface file %q", g.name, filepath.Base(want))).
		At(sp).
		Set("package", g.name).
		Set("expected", want).
		AddNote("run `gom info` to generate a draft interface")
}

// strayInterface is the E0604 diagnostic for a .gomi that is not its
// directory's <package>.gomi, or nil when it is (unconsumed but
// correctly named — the package's implementation was simply not part
// of this run).
func strayInterface(path, declared string, sp diag.Span) *diag.Diagnostic {
	want := declared + ".gomi"
	if declared == "" || filepath.Base(path) == want {
		return nil
	}
	d := diag.Error("E0604",
		fmt.Sprintf("%s declares package %s but the interface file of package %s is %s",
			filepath.Base(path), declared, declared, want)).
		At(sp).
		Set("file", path).
		Set("package", declared).
		Set("expected", want)
	return &d
}

// compileInput compiles one file with the standard options — the
// seam shared by the check loop and the interface pass's discovery
// of a sibling .gomi.
func compileInput(in input) *build.Result {
	r, err := build.Compile(build.Input{Path: in.path, Src: in.src, Interface: in.iface},
		compileOptions(in, true))
	if err != nil {
		return nil
	}
	return r
}

// --- gom info ---

// runInfo implements `gom info [-format F] [-o out] [FILE|DIR ...]`:
// generate or update .gomi interface drafts from the typed
// signatures of the .gom sources (spec section 7.7;
// docs/sketch/2026-11-05-moon-cli-command-structure.md). Sources
// group by directory and package — one draft per package, written
// to <package>.gomi beside the sources only when its contents
// differ. `-o -` prints the draft on stdout and the diagnostic
// stream moves to stderr, keeping the artifact pipeable.
func runInfo(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("info", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "json", "diagnostic format: json or text")
	out := fs.String("o", "", "output file (single package) or directory; \"-\" for stdout")
	if err := fs.Parse(args); err != nil {
		return flagError(err)
	}
	if err := checkFormat(*format); err != nil {
		fmt.Fprintf(stderr, "gom info: %v\n", err)
		return diag.ExitInternal
	}
	inPaths, err := expandInputs(fs.Args(), false)
	if err != nil {
		fmt.Fprintf(stderr, "gom info: %v\n", err)
		return diag.ExitInternal
	}
	inputs, err := readInputs(inPaths)
	if err != nil {
		fmt.Fprintf(stderr, "gom info: %v\n", err)
		return diag.ExitInternal
	}

	// Diagnostics always go to stderr here — stdout is reserved for
	// the draft artifact under -o -, the same split gom fmt makes.
	rep := diag.Report{Files: inputNames(inputs)}
	srcs := map[string][]byte{}

	// Group the inputs into packages: one .gomi describes one
	// package (dir + name), so the draft covers the group's files.
	type grp struct {
		dir   string
		name  string
		srcs  []iface.Source
		erred bool
	}
	groups := map[string]*grp{}
	var order []string
	for _, in := range inputs {
		srcs[in.path] = in.src
		r, err := build.Compile(build.Input{Path: in.path, Src: in.src},
			compileOptions(in, true))
		if err != nil {
			fmt.Fprintf(stderr, "gom info: %s: %v\n", in.path, err)
			return diag.ExitInternal
		}
		rep.Diagnostics = append(rep.Diagnostics, r.Diagnostics...)
		if r.Typed == nil {
			continue
		}
		key := filepath.Dir(in.path) + "\x00" + r.Typed.Package
		g := groups[key]
		if g == nil {
			g = &grp{dir: filepath.Dir(in.path), name: r.Typed.Package}
			groups[key] = g
			order = append(order, key)
		}
		g.srcs = append(g.srcs, iface.Source{AST: r.File, Typed: r.Typed})
		if r.Diagnostics.HasError() {
			g.erred = true
		}
	}

	if *out == "-" && len(order) != 1 {
		fmt.Fprintf(stderr, "gom info: -o - prints one interface; %d packages were given\n", len(order))
		return diag.ExitInternal
	}
	if *out != "" && *out != "-" && len(order) > 1 && !isDirFlag(*out) {
		fmt.Fprintf(stderr, "gom info: -o %s names a file, but %d packages were given\n", *out, len(order))
		return diag.ExitInternal
	}

	var writeErrs []error
	for _, key := range order {
		g := groups[key]
		if g.erred {
			continue // a broken source has no meaningful draft
		}
		draft := iface.Draft(g.srcs)
		if *out == "-" {
			if _, err := stdout.Write(draft); err != nil {
				writeErrs = append(writeErrs, fmt.Errorf("stdout: %w", err))
			}
			continue
		}
		dst := filepath.Join(g.dir, g.name+".gomi")
		if *out != "" {
			if isDirFlag(*out) {
				dst = filepath.Join(strings.TrimSuffix(*out, "/"), g.name+".gomi")
			} else {
				dst = *out
			}
		}
		if same, err := fileEquals(dst, draft); err != nil {
			writeErrs = append(writeErrs, fmt.Errorf("%s: %w", dst, err))
			continue
		} else if same {
			continue // write only on change
		}
		if err := writeFile(dst, draft); err != nil {
			writeErrs = append(writeErrs, fmt.Errorf("%s: %w", dst, err))
			continue
		}
		rep.Outputs = append(rep.Outputs, dst)
	}
	if err := emitReport(stderr, *format, rep, srcs); err != nil {
		fmt.Fprintf(stderr, "gom info: %v\n", err)
		return diag.ExitInternal
	}
	for _, e := range writeErrs {
		fmt.Fprintf(stderr, "gom info: cannot write output: %v\n", e)
	}
	if len(writeErrs) > 0 {
		return diag.ExitInternal
	}
	return rep.ExitCode()
}

// isDirFlag reports whether the -o argument names a directory —
// an existing one, or the trailing-slash convention.
func isDirFlag(out string) bool {
	if strings.HasSuffix(out, "/") || strings.HasSuffix(out, string(filepath.Separator)) {
		return true
	}
	st, err := os.Stat(out)
	return err == nil && st.IsDir()
}

// fileEquals reports whether path exists with exactly src's
// contents — the write-if-different check.
func fileEquals(path string, src []byte) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return string(b) == string(src), nil
}
