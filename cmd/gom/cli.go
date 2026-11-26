package main

// This file holds the build/check/dump command bodies: flag parsing,
// input expansion, driving internal/build, and emitting the spec
// section 12 message stream. Everything here stays a thin layer —
// all compiler work lives in the internal packages.

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/format"
	"github.com/podhmo/gom/internal/goimport"
	"github.com/podhmo/gom/internal/gomimport"
	"github.com/podhmo/gom/internal/iface"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/resolve"
	gomstd "github.com/podhmo/gom/internal/stdlib"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/treedump"
)

// input is one source file the command will process.
type input struct {
	path  string // as the user refers to it — also the name spans carry
	src   []byte
	iface bool
	// auto marks a module-local source dependency discovered from
	// imports rather than a file or directory the user named.
	auto bool
}

// compileOptions builds the driver options for one input: a
// goimport.Loader behind the gomimport composite answers both
// `import go` and `import` — the resolve loader, signature oracle,
// constructor-field tables and lowering's GoSig/type/arity hooks
// all come from the two sessions — plus the standard library's
// builtin-module tables and the .gomi export hook of spec section
// 5. The loaders run in the input file's own directory so imported
// packages resolve in the module the file belongs to.
func compileOptions(in input, checkOnly bool, seen *depSeen, gl *goimport.Loader) *build.Options {
	dir := filepath.Dir(in.path)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	mods := map[string]map[string]*resolve.Symbol{}
	gc := &gomimport.Config{Dir: dir, Go: gl, Sigs: gl, Modules: mods}
	if seen != nil {
		gc.OnLoadGomi = func(p string, src []byte) { seen.noteGomi(p, src) }
	}
	ml := gomimport.New(gc)
	return gomstd.Apply(&build.Options{
		Loader:         ml,
		Sigs:           ml,
		Modules:        mods,
		Lower:          &lower.Env{GoSig: gl.GoSigOf, Types: ml, FuncArity: ml.ArityOf, FuncType: ml.FuncTypeOf, FuncNOfNamed: ml.FuncNOfNamed, GoUnderlyingSliceOrArrayPtr: gl.GoUnderlyingIsSliceOrArrayPtr, GoUnderlyingKind: gl.GoUnderlyingKind},
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
func compileInputs(inputs []input, checkOnly bool, bc *buildCache, outFlag string, tweak func(*build.Options)) ([]*build.Result, error) {
	results := make([]*build.Result, len(inputs))
	groups := map[string][]int{}
	var order []string
	for i, in := range inputs {
		if in.iface {
			// An interface's own imports are dependencies too —
			// they load only while the .gomi itself compiles. One-off
			// compiles get an unshared Go loader (the dep set is the
			// interface's own, not a package pool's).
			var seen *depSeen
			var gl *goimport.Loader
			dir := filepath.Dir(filepath.Clean(in.path))
			if bc != nil {
				seen = bc.depSink(dir)
				seen.noteMods(dir)
				seen.noteGomi(in.path, in.src)
			}
			glc := &goimport.Config{Dir: dir}
			if seen != nil {
				glc.OnLoad = func(d string, deps []string) {
					seen.noteGoDir(d)
					for _, dep := range deps {
						seen.noteGoDir(dep)
					}
				}
			}
			gl = goimport.New(glc)
			// A parse-invalid interface fails before resolving imports, so
			// prefetching it would only burn a `go list` subprocess.
			if ast, pdiags := syntax.ParseInterface(in.src, in.path); ast != nil && !pdiags.HasError() {
				var goImports []string
				for _, d := range ast.Decls {
					if imp, ok := d.(*syntax.ImportDecl); ok && imp.Go {
						goImports = append(goImports, imp.Path)
					}
				}
				gl.Prefetch(goImports)
			}
			r, err := build.Compile(build.Input{Path: in.path, Src: in.src, Interface: true},
				compileOptions(in, checkOnly, seen, gl))
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

	// --- dep graph + topological layers (design.md section 5 item 6) ---
	// Each dir's own Go import path inside its module and its import
	// clauses to other input dirs give the scheduling edges; a dir's
	// compile reads another input dir's interfaces (`.gomi` files are
	// read-only inputs here — only `gom info` regenerates them, after
	// all compiles finish), so dependents schedule after their
	// dependencies for stable read ordering.
	selfPath, modKey := map[string]string{}, map[string]string{}
	for _, dir := range order {
		mp, md, ok := moduleOf(dir)
		if !ok {
			// No module context — nothing resolves the dir's Gom
			// imports anyway; pool it under its own dir.
			modKey[dir] = dir
			continue
		}
		modKey[dir] = md
		// The group key is a CLI-given (possibly relative) path;
		// Rel needs both sides absolute to reach the self path.
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(md, absDir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if rel == "." {
			selfPath[dir] = mp
		} else {
			selfPath[dir] = mp + "/" + filepath.ToSlash(rel)
		}
	}
	deps := map[string]map[string]bool{}
	goImports := map[string][]string{}
	badParse := map[string]bool{}
	for _, dir := range order {
		for _, i := range groups[dir] {
			ast, pdiags := syntax.Parse(inputs[i].src, inputs[i].path)
			if ast == nil {
				badParse[dir] = true
				continue
			}
			// One parse-invalid file makes CompileFiles reject the
			// whole package before its imports resolve — prefetching
			// it would only burn a `go list` subprocess (which may
			// even touch go.mod under -mod=mod).
			if pdiags.HasError() {
				badParse[dir] = true
			}
			for _, d := range ast.Decls {
				imp, ok := d.(*syntax.ImportDecl)
				if !ok {
					continue
				}
				if imp.Go {
					goImports[dir] = append(goImports[dir], imp.Path)
					continue
				}
				path := canonicalImport(imp.Path)
				for other, sp := range selfPath {
					if other != dir && path == sp {
						if deps[dir] == nil {
							deps[dir] = map[string]bool{}
						}
						deps[dir][other] = true
					}
				}
			}
		}
	}
	// Kahn layers in CLI-arg order: everything a dir depends on sits
	// in an earlier layer, so dependents never start before their
	// dependencies finish.
	remaining := append([]string{}, order...)
	var layers [][]string
	for len(remaining) > 0 {
		done := map[string]bool{}
		for _, l := range layers {
			for _, d := range l {
				done[d] = true
			}
		}
		var layer, rest []string
		for _, dir := range remaining {
			ready := true
			for dep := range deps[dir] {
				if !done[dep] {
					ready = false
					break
				}
			}
			if ready {
				layer = append(layer, dir)
			} else {
				rest = append(rest, dir)
			}
		}
		if layer == nil {
			// An import cycle among the inputs — E0606 reports it
			// inside the compiles; the remainder runs serially in
			// CLI-arg order, one dir per layer.
			for _, d := range rest {
				layers = append(layers, []string{d})
			}
			break
		}
		layers = append(layers, layer)
		remaining = rest
	}

	// Shared Go import loaders, pooled by module context (modKey):
	// `go list -export` resolution is module-scoped, so one loader
	// per module dedupes the subprocess+export-data work across dirs.
	// The loader serializes every entry point internally.
	type goPool struct {
		gl   *goimport.Loader
		seen *depSeen
	}
	var poolMu sync.Mutex
	pools := map[string]*goPool{}
	poolLoader := func(dir string) (*goimport.Loader, *depSeen) {
		key := modKey[dir]
		poolMu.Lock()
		defer poolMu.Unlock()
		p := pools[key]
		if p == nil {
			p = &goPool{}
			glc := &goimport.Config{Dir: dir}
			if bc != nil {
				// Go-side deps pool at module granularity — a dep
				// edit conservatively invalidates every dir sharing
				// the loader (the dep set is the union).
				p.seen = bc.depSink(goPoolKey(dir))
				glc.OnLoad = func(d string, deps []string) {
					p.seen.noteGoDir(d)
					for _, dep := range deps {
						p.seen.noteGoDir(dep)
					}
				}
			}
			p.gl = goimport.New(glc)
			pools[key] = p
		}
		return p.gl, p.seen
	}

	compileDir := func(dir string) error {
		idxs := groups[dir]
		ins := make([]build.Input, len(idxs))
		for j, i := range idxs {
			ins[j] = build.Input{Path: inputs[i].path, Src: inputs[i].src}
		}
		if bc != nil {
			if _, ok := bc.Hit(dir, dirCacheOut(inputs, idxs, outFlag), ins); ok {
				// The manifest still holds and its outputs are on
				// disk — mark the inputs cached; runBuild lists
				// their dsts without compiling or writing.
				for _, i := range idxs {
					results[i] = &build.Result{Cached: true}
				}
				return nil
			}
		}
		var seen *depSeen
		if bc != nil {
			seen = bc.depSink(dir)
			seen.noteMods(dir)
		}
		gl, _ := poolLoader(dir)
		if !badParse[dir] {
			gl.Prefetch(goImports[dir])
		}
		opts := compileOptions(inputs[idxs[0]], checkOnly, seen, gl)
		if tweak != nil {
			tweak(opts)
		}
		rs, err := build.CompileFiles(ins, opts)
		if err != nil {
			return fmt.Errorf("%s: %w", dir, err)
		}
		for j, i := range idxs {
			results[i] = rs[j]
		}
		if bc != nil {
			bc.note(dir, ins)
		}
		return nil
	}

	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	for _, layer := range layers {
		var wg sync.WaitGroup
		sem := make(chan struct{}, workers)
		errs := make([]error, len(layer))
		for li, dir := range layer {
			wg.Add(1)
			go func(li int, dir string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				errs[li] = compileDir(dir)
			}(li, dir)
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				return nil, err
			}
		}
	}
	return results, nil
}

// moduleOf walks up from dir for the nearest go.mod and parses its
// module line — the module path and root dir a dir's imports resolve
// against. Mirrors gomimport's own module lookup; kept local because
// scheduling (not loading) needs it here.
func moduleOf(dir string) (modPath, modDir string, ok bool) {
	d, err := filepath.Abs(dir)
	if err != nil {
		return "", "", false
	}
	for {
		b, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err == nil {
			for line := range strings.SplitSeq(string(b), "\n") {
				if f := strings.Fields(line); len(f) >= 2 && f[0] == "module" {
					return f[1], d, true
				}
			}
			return "", "", false
		}
		if !os.IsNotExist(err) {
			return "", "", false
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", "", false
		}
		d = parent
	}
}

// goPoolKey is the depSeen bucket shared by every dir in a module —
// the key the pooled goimport.Loader's OnLoad appends Go dep dirs
// under, and the one commit() merges back into a dir's manifest. It
// carries the effective workspace file too: a `go list` in a dir
// resolves through the nearest go.work (or $GOWORK), so dirs sharing
// a go.mod but seeing different workspaces must not share a loader.
func goPoolKey(dir string) string {
	if _, md, ok := moduleOf(dir); ok {
		return "go:" + md + "|" + workFile(dir)
	}
	return "go:" + dir + "|" + workFile(dir)
}

// workFile is the workspace `go list` would resolve through for dir:
// $GOWORK when set ("" lets the walk run; "off" disables), else the
// nearest ancestor go.work, else "".
func workFile(dir string) string {
	if w := os.Getenv("GOWORK"); w != "" {
		return w
	}
	d, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		p := filepath.Join(d, "go.work")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// canonicalImport unfolds the stdlib short names through
// gomimport's table — kept as a call-site shim so the layer-order
// code reads locally.
func canonicalImport(path string) string {
	return gomimport.CanonicalPath(path)
}

// cacheOutForInput is the output mode a cache manifest can rely on.
// Auto-added dependencies ignore -o and write beside their sources.
func cacheOutForInput(in input, outFlag string) string {
	if in.auto {
		return ""
	}
	return outFlag
}

// dirCacheOut keys a compiled directory's manifest by the output
// mode of its members. Expansion never mixes auto dependencies with
// explicit inputs in one directory, so the first member represents
// the group.
func dirCacheOut(inputs []input, idxs []int, outFlag string) string {
	if len(idxs) == 0 {
		return outFlag
	}
	return cacheOutForInput(inputs[idxs[0]], outFlag)
}

// inputKey canonicalizes a path for identity comparisons without
// changing the user-facing spelling carried in input.path.
func inputKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

// expandModuleLocalDeps adds same-module .gom source dependencies
// imported by the explicit inputs. Package directories are added as
// complete direct-child *.gom sets because directory identity is the
// Go package identity. Directories the user already named are not
// widened, preserving file-wise input control.
func expandModuleLocalDeps(inputs []input) ([]input, error) {
	out := slices.Clone(inputs)
	queue := slices.Clone(inputs)
	seenFiles := map[string]bool{}
	seenDirs := map[string]bool{}
	for _, in := range inputs {
		seenFiles[inputKey(in.path)] = true
		seenDirs[inputKey(filepath.Dir(filepath.Clean(in.path)))] = true
	}
	for len(queue) > 0 {
		in := queue[0]
		queue = queue[1:]
		if in.iface {
			continue
		}
		f, ds := syntax.Parse(in.src, in.path)
		if f == nil || ds.HasError() {
			continue // the normal compile reports the parse error once
		}
		dir := filepath.Dir(filepath.Clean(in.path))
		modPath, modDir, ok := moduleOf(dir)
		if !ok {
			continue
		}
		for _, d := range f.Decls {
			imp, ok := d.(*syntax.ImportDecl)
			if !ok || imp.Go {
				continue
			}
			impPath := canonicalImport(imp.Path)
			if gomimport.IsStdlibPath(impPath) {
				continue // toolchain artifacts are built through the stdlib workflow
			}
			var rel string
			switch {
			case impPath == modPath:
				rel = ""
			case strings.HasPrefix(impPath, modPath+"/"):
				rel = strings.TrimPrefix(impPath, modPath+"/")
			default:
				continue // stdlib, external module, or workspace sibling
			}
			depDir := filepath.Clean(filepath.Join(modDir, filepath.FromSlash(rel)))
			if !filepath.IsAbs(in.path) {
				// Keep discovered paths in the same user-facing style as
				// the input that reached them: relative CLI inputs should
				// not turn dependency diagnostics and outputs absolute.
				if cwd, err := os.Getwd(); err == nil {
					if relDir, err := filepath.Rel(cwd, depDir); err == nil {
						depDir = relDir
					}
				}
			}
			checkDir := depDir
			if resolved, err := filepath.EvalSymlinks(depDir); err == nil {
				checkDir = resolved
			}
			if depModPath, depModDir, ok := moduleOf(checkDir); !ok || depModPath != modPath || depModDir != modDir {
				continue // a nested or symlinked module is external even when its path prefixes ours
			}
			depKey := inputKey(depDir)
			if seenDirs[depKey] {
				continue
			}
			seenDirs[depKey] = true
			matches, err := filepath.Glob(filepath.Join(depDir, "*.gom"))
			if err != nil {
				return nil, err
			}
			sort.Strings(matches)
			for _, path := range matches {
				if st, err := os.Stat(path); err != nil || st.IsDir() {
					continue
				}
				clean := filepath.Clean(path)
				fileKey := inputKey(clean)
				if seenFiles[fileKey] {
					continue
				}
				src, err := os.ReadFile(clean)
				if err != nil {
					return nil, fmt.Errorf("read %s: %w", clean, err)
				}
				dep := input{path: clean, src: src, auto: true}
				seenFiles[fileKey] = true
				out = append(out, dep)
				queue = append(queue, dep)
			}
		}
	}
	return out, nil
}

// runCheck implements `gom check [-format F] [FILE|DIR ...]`:
// diagnostics only, nothing written (spec section 12).
func runCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "json", "diagnostic format: json or text")
	strict := &strictFlag{}
	fs.Var(strict, "strict", "exit non-zero on warnings; -strict=CODES limits to the listed codes")
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
	results, err := compileInputs(inputs, true, nil, "", nil)
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
	checkInterfaces(inputs, results, &rep, srcs, nil)
	if err := emitReport(stdout, *format, rep, srcs); err != nil {
		fmt.Fprintf(stderr, "gom check: %v\n", err)
		return diag.ExitInternal
	}
	return strictExit(rep, strict)
}

// runBuild implements `gom build [-format F] [-o out] [-go] [FILE|DIR ...]`:
// the full pipeline plus writing one .go per .gom input. `-go`
// additionally runs `go build` (`-go`) and `go vet` (`-vet`) in
// each directory that received output and folds the tool's
// diagnostics back onto .gom spans
// (docs/decisions/2026-11-17-cli-gotool-invocation.md).
func runBuild(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "json", "diagnostic format: json or text")
	out := fs.String("o", "", "output file (single input) or directory")
	goBuild := fs.Bool("go", false, "run `go build` on written packages and fold its diagnostics")
	goVet := fs.Bool("vet", false, "run `go vet` on written packages and fold its diagnostics")
	strict := &strictFlag{}
	fs.Var(strict, "strict", "exit non-zero on warnings; -strict=CODES limits to the listed codes")
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
	inputs, err = expandModuleLocalDeps(inputs)
	if err != nil {
		fmt.Fprintf(stderr, "gom build: %v\n", err)
		return diag.ExitInternal
	}
	nOutInputs := 0
	for _, in := range inputs {
		if !in.auto {
			nOutInputs++
		}
	}

	rep := diag.Report{Files: inputNames(inputs)}
	srcs := map[string][]byte{}
	for _, in := range inputs {
		srcs[in.path] = in.src
	}
	// The incremental cache is off under -go/-vet/--strict: a hit
	// produces no compiled output to hand the go tool check and no
	// diagnostics for --strict to judge, so the flags force a fresh
	// compile (design.md section 5's opt-out is still
	// GOM_BUILD_NOCACHE).
	var bc *buildCache
	if !*goBuild && !*goVet && !strict.on {
		bc = newBuildCache()
	}
	results, err := compileInputs(inputs, false, bc, *out, nil)
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
	failed := checkInterfaces(inputs, results, &rep, srcs, bc)

	// Resolve every output path before writing anything so a bad -o
	// fails fast instead of after partial writes.
	dsts := make([]string, len(inputs))
	dstSeen := map[string]string{}
	for i, in := range inputs {
		if results[i].Output == nil || failed[results[i]] {
			continue
		}
		dst, err := outputPath(in.path, cacheOutForInput(in, *out), nOutInputs)
		if err != nil {
			fmt.Fprintf(stderr, "gom build: %v\n", err)
			return diag.ExitInternal
		}
		key := inputKey(dst)
		if prev, ok := dstSeen[key]; ok {
			fmt.Fprintf(stderr, "gom build: output path %s is assigned to both %s and %s\n", dst, prev, in.path)
			return diag.ExitInternal
		}
		dstSeen[key] = in.path
		dsts[i] = dst
	}

	// Write every input that produced output; failures are collected
	// and reported after the message stream — spec's summary lists
	// the files actually written.
	var writeErrs []error
	writeOK := map[*build.Result]bool{}
	for i, r := range results {
		if r == nil || r.Cached {
			if r != nil {
				// Cache hit: the recorded outputs are still on
				// disk — list them like a fresh write (design.md
				// section 5's summary contract).
				dst, err := outputPath(inputs[i].path, cacheOutForInput(inputs[i], *out), nOutInputs)
				if err != nil {
					fmt.Fprintf(stderr, "gom build: %v\n", err)
					return diag.ExitInternal
				}
				rep.Outputs = append(rep.Outputs, dst)
			}
			continue
		}
		if r.Output == nil || failed[r] {
			continue
		}
		dst := dsts[i]
		if err := writeGenerated(dst, r.Output.Source); err != nil {
			writeErrs = append(writeErrs, fmt.Errorf("%s: %w", dst, err))
			continue
		}
		writeOK[r] = true
		rep.Outputs = append(rep.Outputs, dst)
	}
	// Manifests commit only for packages that wrote every output
	// clean — a failed member or interface leaves nothing to
	// seed a hit from (the withheld package recompiles next run).
	if bc != nil {
		wrote := map[string][]string{}
		outByDir := map[string]string{}
		broken := map[string]bool{}
		for i, r := range results {
			dir := filepath.Dir(filepath.Clean(inputs[i].path))
			if _, ok := outByDir[dir]; !ok {
				outByDir[dir] = cacheOutForInput(inputs[i], *out)
			}
			if r == nil || r.Cached || r.Output == nil || failed[r] || !writeOK[r] {
				broken[dir] = true
				continue
			}
			wrote[dir] = append(wrote[dir], dsts[i])
		}
		for dir, outs := range wrote {
			if !broken[dir] {
				bc.commit(dir, outByDir[dir], outs)
			}
		}
	}

	for _, e := range writeErrs {
		rep.Diagnostics = append(rep.Diagnostics,
			diag.Error("E0003", "output could not be written").Set("error", e.Error()))
	}
	// -go/-vet invoke the go tool on what was just written — the
	// only surface where a real generated .go exists for `go build`
	// or `go vet` to check. The fold runs even when a write failed:
	// the error a half-written directory produces is the honest
	// report.
	if *goBuild || *goVet {
		runGoTools(inputs, results, failed, dsts, &rep, *goBuild, *goVet)
	}
	if err := emitReport(stdout, *format, rep, srcs); err != nil {
		fmt.Fprintf(stderr, "gom build: %v\n", err)
		return diag.ExitInternal
	}
	return strictExit(rep, strict)
}

// runDump implements `gom dump -phase=P FILE` — a developer tool
// printing one phase's IR tree to stdout. The diagnostic report goes
// to stderr so stdout stays the clean artifact; diagnostics never
// suppress the dump.
func runDump(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dump", flag.ContinueOnError)
	fs.SetOutput(stderr)
	phase := fs.String("phase", "", "phase to dump: syntax, resolve, check, core, or emit")
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
			opts := compileOptions(in, false, nil, goimport.New(&goimport.Config{Dir: dir}))
			if *phase == "emit" {
				opts.Lower.Tests = true
			}
			rs, err := build.CompileFiles(ins, opts)
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
		opts := compileOptions(in, false, nil, goimport.New(&goimport.Config{Dir: filepath.Dir(in.path)}))
		if *phase == "emit" {
			opts.Lower.Tests = true
		}
		r, err = build.Compile(build.Input{Path: in.path, Src: in.src, Interface: in.iface},
			opts)
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
	case "emit":
		// The listing survives a withheld Output — on an emit
		// error (E0608) the colliding names are exactly what the
		// dump is for.
		names := r.EmitNames
		if names == nil && r.Output != nil {
			names = r.Output.EmitNames
		}
		if names != nil {
			var b strings.Builder
			for _, n := range names {
				fmt.Fprintf(&b, "%s\t%s\t%s", n.Kind, n.GomName, n.Name)
				if n.Name != n.Base {
					fmt.Fprintf(&b, "\t(base %s)", n.Base)
				}
				b.WriteByte('\n')
			}
			text = b.String()
		}
	default:
		fmt.Fprintf(stderr, "gom dump: unknown phase %q (want syntax, resolve, check, core, or emit)\n", *phase)
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

// writeFile writes generated source, creating the parent
// directory. Identical content is not rewritten — the mtime stays
// so the Go build cache keeps its hit (design.md section 5's
// write-if-different rule).
func writeFile(path string, src []byte) error {
	if same, err := fileEquals(path, src); err == nil && same {
		return nil
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, src, 0o644)
}

// writeGenerated is writeFile for generated .go outputs, with the
// clobber guard: an existing file WITHOUT the generated marker is
// never overwritten. `host.gom` emits `host.go` beside it, and a
// handwritten file of the same name would be clobbered silently
// otherwise — the error names the shim.go convention. Only the
// generated-.go paths use this; fmt and .gomi emission write files
// the user owns.
func writeGenerated(path string, src []byte) error {
	if same, err := fileEquals(path, src); err == nil && same {
		return nil
	}
	if _, statErr := os.Stat(path); statErr == nil {
		existing, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("refusing to overwrite %s: the file exists but could not be read (%v) — remove it if it is stale generated output", path, err)
		}
		if !bytes.HasPrefix(existing, []byte("// Code generated by gom. DO NOT EDIT.")) {
			shim := "shim.go"
			if strings.HasSuffix(path, "_test.go") {
				shim = "shim_test.go"
			}
			return fmt.Errorf("refusing to overwrite %s: the file exists without the gom generated-code marker — move handwritten code aside (the convention is %s) or remove the file", path, shim)
		}
	}
	return writeFile(path, src)
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

// strictFlag implements `--strict` (spec section 12): a bare
// `--strict` promotes every warning diagnostic to a failing run, and
// `--strict=CODES` (comma-separated) fails only when a warning
// carries one of the listed codes. IsBoolFlag lets the flag stand
// without a value — flag passes "true" for the bare form.
type strictFlag struct {
	on    bool
	codes map[string]bool // nil: every warning counts
}

// String satisfies flag.Value.
func (s *strictFlag) String() string {
	if !s.on {
		return ""
	}
	if s.codes == nil {
		return "true"
	}
	var cs []string
	for c := range s.codes {
		cs = append(cs, c)
	}
	slices.Sort(cs)
	return strings.Join(cs, ",")
}

// Set parses `-strict[=value]`: "true" and "" enable the bare form,
// "false" disables, anything else is a comma-separated code list.
func (s *strictFlag) Set(v string) error {
	switch v {
	case "", "true":
		s.on = true
		return nil
	case "false":
		s.on = false
		return nil
	}
	codes := map[string]bool{}
	for _, f := range strings.Split(v, ",") {
		f = strings.TrimSpace(f)
		if !diag.WellFormed(diag.Code(f)) {
			return fmt.Errorf("--strict: malformed diagnostic code %q", f)
		}
		codes[f] = true
	}
	s.on = true
	s.codes = codes
	return nil
}

// IsBoolFlag lets `--strict` appear without a value.
func (s *strictFlag) IsBoolFlag() bool { return true }

// strictExit maps a report to its exit code under --strict: when the
// run would pass but a warning the flag selects exists, the run
// fails. The warnings themselves stay warnings — only the exit code
// tightens (CI use).
func strictExit(rep diag.Report, sf *strictFlag) int {
	code := rep.ExitCode()
	if !sf.on || code == diag.ExitError {
		return code
	}
	for _, d := range rep.Diagnostics {
		if d.Severity == diag.SeverityWarning &&
			(sf.codes == nil || sf.codes[string(d.Code)]) {
			return diag.ExitError
		}
	}
	return code
}
