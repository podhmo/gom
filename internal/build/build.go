package build

import (
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/codegen"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/pattern"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
)

// Input is one source file handed to Compile.
type Input struct {
	// Path is the name diagnostics and `//line` directives attach to —
	// a slash-separated path as the user refers to the file.
	Path string
	// Src is the file's contents.
	Src []byte
	// Interface marks the input as a .gomi interface file — parsed
	// with syntax.ParseInterface instead of syntax.Parse. A Path
	// ending in ".gomi" implies Interface.
	Interface bool
}

// IsInterfaceFile reports whether path names a .gomi interface file.
func IsInterfaceFile(path string) bool {
	return strings.HasSuffix(path, ".gomi")
}

// Options configures Compile by forwarding the phases' own extension
// points — the driver interprets none of them (see
// docs/decisions/2026-11-02-build-driver.md). The zero value compiles
// a self-contained file: every import reports E0203 and only the
// builtin scope supplies signatures. cmd/gom wires the real
// implementations — one internal/goimport.Loader fills Loader, Sigs
// and Lower.GoSig.
type Options struct {
	// Loader resolves imported packages for resolve (nil: every
	// import reports E0203).
	Loader resolve.Loader
	// Modules injects builtin-scope module member tables
	// (resolve.Config.Modules).
	Modules map[string]map[string]*resolve.Symbol
	// Sigs supplies signatures of symbols the file does not declare
	// (nil: builtins only).
	Sigs check.SignatureSource
	// Exports reports the member names the input's package
	// publishes through its .gomi (spec section 5): declarations
	// whose names appear emit capitalized Go identifiers, the rest
	// stay unexported. The hook is consulted once per compile —
	// after checking, when the package name is known — with the
	// input's directory and declared package name. Nil: nothing is
	// exported (the package-main behavior; package main itself is
	// never consulted).
	Exports func(dir, pkg string) map[string]bool
	// SelfImportPath is the Go import path the input's own output
	// package occupies — set for sources compiled into a package
	// the toolchain already has a path for (internal/stdlib's
	// SelfPath answers for the stdlib directories). References
	// into that path emit unqualified identifiers: a package never
	// imports itself. Empty for ordinary inputs.
	SelfImportPath string
	// Lower supplies the lowering oracles (Types, FuncArity, GoSig).
	// The Lines hook is per-file and is always set by Compile from
	// the input's own source.
	Lower *lower.Env
	// Pattern configures the pattern check. Its Resolver defaults to
	// a resolver over the input's source; set it to cover other
	// sources too.
	Pattern *pattern.Options

	// CheckOnly stops the pipeline after the pattern check: no
	// lowering, no code generation (gom check, spec section 12). It
	// is implied for interface inputs.
	CheckOnly bool
}

// Result is the output of compiling one file: every diagnostic the
// pipeline produced plus the phase artifacts the run reached —
// artifacts of phases that did not run are nil, which is also what
// `gom dump -phase=X` inspects.
type Result struct {
	// Diagnostics is every diagnostic produced, in pipeline order —
	// callers sort with diag.List.Sort for output.
	Diagnostics diag.List

	// Interface reports whether the input was a .gomi file.
	Interface bool

	// File is the parsed surface AST (always set once parsing ran).
	File *syntax.File
	// Resolved is the name-resolved AST (nil if parsing failed).
	Resolved *resolve.Resolved
	// Typed is the typed AST (nil if the run stopped before check,
	// or check was not reached).
	Typed *check.File
	// Core is the lowered Core IR (nil in CheckOnly mode and on
	// interface inputs).
	Core *core.File
	// Output is the generated Go source (nil when the run did not
	// reach codegen).
	Output *codegen.Output
}

// Compile drives one file through the pipeline:
// parse → resolve → check → pattern → lower → codegen.
//
// Error boundaries (docs/decisions/2026-11-02-build-driver.md): a
// phase runs only when every diagnostic emitted so far is below error
// severity — downstream phase contracts all require error-free input
// (an errored check.File can crash pattern; lowering reports
// contract-gap diagnostics that would turn user errors into
// compiler-bug-looking output). Interface (.gomi) inputs stop after
// check: their declarations have no bodies for the expression-level
// phases to analyze.
//
// Diagnostics aggregate across the phases in pipeline order. A nil
// error means the pipeline completed what was asked; a non-nil error
// is an internal failure — codegen rejecting a validated Core file or
// producing ungofmtable source — which the driver surfaces as exit
// code 2 (diag.ExitInternal).
func Compile(in Input, opts *Options) (*Result, error) {
	var o Options
	if opts != nil {
		o = *opts
	}
	r := &Result{Interface: in.Interface || IsInterfaceFile(in.Path)}

	// Reject control characters and invalid UTF-8 in the path before
	// anything consumes it: Path is written verbatim into //line
	// operands (gen.line writes sp.File into a //gomsrc sentinel),
	// diagnostic spans and JSON output — a '\n' would split an
	// emitted line and inject a directive, and undecodable bytes
	// would leak into the generated file. The check lives at the
	// driver boundary so library callers get the same guard as the
	// CLI.
	if ds := pathDiags(in.Path); len(ds) > 0 {
		r.Diagnostics = append(r.Diagnostics, ds...)
		return r, nil
	}

	parse := syntax.Parse
	if r.Interface {
		parse = syntax.ParseInterface
	}
	f, diags := parse(in.Src, in.Path)
	r.File = f
	r.Diagnostics = append(r.Diagnostics, diags...)
	if diags.HasError() {
		return r, nil
	}

	res, diags := resolve.File(f, resolveConfig(&o))
	r.Resolved = res
	r.Diagnostics = append(r.Diagnostics, diags...)
	if diags.HasError() {
		return r, nil
	}

	tf, diags := check.Check(res, o.Sigs)
	r.Typed = tf
	r.Diagnostics = append(r.Diagnostics, diags...)
	// Interface files declare signatures without bodies, so the
	// phases below — which all analyze expressions — do not run on
	// them; checking the declarations is where they stop.
	if diags.HasError() || r.Interface {
		return r, nil
	}

	po := pattern.Options{Resolver: diag.NewMapResolver(map[string][]byte{in.Path: in.Src})}
	if o.Pattern != nil {
		if o.Pattern.Resolver != nil {
			po.Resolver = o.Pattern.Resolver
		}
		po.MaxCounterexamples = o.Pattern.MaxCounterexamples
		po.Decls = append(po.Decls, o.Pattern.Decls...)
	}
	var siblings []check.Decl
	if ts, ok := o.Sigs.(check.TypeDeclSource); ok {
		for _, td := range ts.TypeDecls() {
			siblings = append(siblings, td)
		}
	}
	po.Decls = append(po.Decls, siblings...)
	diags = pattern.CheckFile(tf, po)
	r.Diagnostics = append(r.Diagnostics, diags...)
	if diags.HasError() || o.CheckOnly {
		return r, nil
	}

	le := lower.Env{Lines: diag.NewLineIndex(in.Src), Siblings: siblings}
	if o.Lower != nil {
		le = *o.Lower
		le.Lines = diag.NewLineIndex(in.Src)
		le.Siblings = siblings
	}
	if o.Exports != nil && tf.Package != "main" {
		exports := o.Exports(filepath.Dir(in.Path), tf.Package)
		if len(exports) > 0 {
			base := le.Exported
			le.Exported = func(sym *resolve.Symbol) bool {
				if sym.Package == "" && exports[sym.Name] {
					return true
				}
				return base != nil && base(sym)
			}
		}
	}
	coref, ldiags := lower.File(tf, &le)
	coref.SelfImportPath = o.SelfImportPath
	r.Core = coref
	r.Diagnostics = append(r.Diagnostics, ldiags...)
	if diag.List(ldiags).HasError() {
		return r, nil
	}

	out, err := codegen.File(coref, in.Src)
	if err != nil {
		return r, err
	}
	r.Output = out
	return r, nil
}

// resolveConfig builds the resolver configuration of a compile:
// the package loader and builtin-module tables pass through, and a
// loader that also publishes constructor field tables (the .gomi
// reader's CtorFieldsOf) fills the CtorFields hook — `{ name = pat
// }` fields and `~label:` arguments of imported-variant patterns
// resolve against it.
func resolveConfig(o *Options) *resolve.Config {
	c := &resolve.Config{Loader: o.Loader, Modules: o.Modules}
	if cf, ok := o.Loader.(resolve.CtorFieldSource); ok {
		c.CtorFields = cf.CtorFieldsOf
	}
	return c
}

// pathDiags is the driver-boundary path guard of Compile, in
// distributable form: nil when the path is safe to write into
// diagnostics and generated output.
func pathDiags(path string) diag.List {
	if !utf8.ValidString(path) {
		return diag.List{
			diag.Error("E0002", "input path is not valid UTF-8").
				Set("path", path),
		}
	}
	if i := strings.IndexFunc(path, unicode.IsControl); i >= 0 {
		return diag.List{
			diag.Error("E0002", "input path contains a control character").
				Set("path", path).
				Set("offset", i),
		}
	}
	return nil
}

// spread attributes each diagnostic to the result whose path its
// span names; a diagnostic with no matching — or no — span attaches
// to first. Unit-level diagnostics carry the span of the file they
// concern, so this keeps per-file Diagnostics honest.
func spread(first *Result, byPath map[string]*Result, ds diag.List) {
	for _, d := range ds {
		if r := byPath[d.Span.File]; r != nil {
			r.Diagnostics = append(r.Diagnostics, d)
			continue
		}
		first.Diagnostics = append(first.Diagnostics, d)
	}
}

// CompileFiles drives the .gom files of one package through the
// pipeline as a unit — the package-granular form of Compile
// (docs/decisions/2026-11-05-package-compilation-unit.md). Results
// come back one per input, in input order.
//
// Interface (.gomi) inputs are not part of the unit — an interface
// file has its own scope and stops after check — so each compiles
// singly through Compile while the implementation files of the batch
// share one package scope: a top-level name one file declares is
// visible in the others, a duplicate across files is E0202, and
// conflicting package clauses are E0605.
//
// The unit boundary covers the phases whose state is shared:
// parse → resolve → check run for the batch only when the batch is
// clean so far (a file whose sibling failed to parse sees no
// cascaded diagnostics). From pattern on the phases are per-file
// again — each file's env is the shared one plus the unit's other
// declarations — so a pattern or lowering error stops just that
// file.
func CompileFiles(ins []Input, opts *Options) ([]*Result, error) {
	if len(ins) == 0 {
		return nil, nil
	}
	var o Options
	if opts != nil {
		o = *opts
	}
	results := make([]*Result, len(ins))

	// Partition: interface files compile singly.
	var impl []int
	for i, in := range ins {
		if in.Interface || IsInterfaceFile(in.Path) {
			r, err := Compile(in, opts)
			if err != nil {
				return nil, err
			}
			results[i] = r
			continue
		}
		results[i] = &Result{}
		impl = append(impl, i)
	}
	if len(impl) == 0 {
		return results, nil
	}
	byPath := make(map[string]*Result, len(impl))
	for _, i := range impl {
		byPath[ins[i].Path] = results[i]
	}
	if len(impl) == 1 {
		// A one-file unit is the single-file pipeline — the shared
		// scope just has no siblings to share with.
		r, err := Compile(ins[impl[0]], opts)
		if err != nil {
			return nil, err
		}
		results[impl[0]] = r
		return results, nil
	}

	// Parse every implementation file. The unit's next phase runs
	// only when every file parsed: a failed sibling would leave the
	// package scope incomplete and cascade E0201s through the rest.
	files := make([]*syntax.File, 0, len(impl))
	failed := false
	for _, i := range impl {
		in := ins[i]
		r := results[i]
		if ds := pathDiags(in.Path); len(ds) > 0 {
			r.Diagnostics = append(r.Diagnostics, ds...)
			failed = true
			continue
		}
		f, diags := syntax.Parse(in.Src, in.Path)
		r.File = f
		r.Diagnostics = append(r.Diagnostics, diags...)
		files = append(files, f)
		if diags.HasError() {
			failed = true
		}
	}
	if failed {
		return results, nil
	}

	ress, diags := resolve.Files(files, resolveConfig(&o))
	for i, res := range ress {
		results[impl[i]].Resolved = res
	}
	spread(results[impl[0]], byPath, diags)
	if diags.HasError() {
		return results, nil
	}

	tfs, diags := check.Files(ress, o.Sigs)
	for i, tf := range tfs {
		results[impl[i]].Typed = tf
	}
	spread(results[impl[0]], byPath, diags)
	if diags.HasError() {
		return results, nil
	}

	// The unit's declaration universe, for the per-file envs below:
	// every file's typed declarations, plus the requalified type
	// declarations the signature source loaded (imported ADTs).
	var unitDecls []check.Decl
	for _, tf := range tfs {
		unitDecls = append(unitDecls, tf.Decls...)
	}
	if ts, ok := o.Sigs.(check.TypeDeclSource); ok {
		for _, td := range ts.TypeDecls() {
			unitDecls = append(unitDecls, td)
		}
	}

	// The unit's per-file phases run independently, but the package
	// is still atomic on failure: a pattern or lowering error in any
	// member withholds every output — a partial package would emit
	// files that reference declarations which never materialized.
	unitFailed := false
	for k, i := range impl {
		in := ins[i]
		r := results[i]
		tf := tfs[k]

		var siblings []check.Decl
		for _, d := range unitDecls {
			if d.Span().File == in.Path {
				continue // the file's own declarations index from f.Decls
			}
			siblings = append(siblings, d)
		}

		po := pattern.Options{Resolver: diag.NewMapResolver(map[string][]byte{in.Path: in.Src})}
		if o.Pattern != nil {
			if o.Pattern.Resolver != nil {
				po.Resolver = o.Pattern.Resolver
			}
			po.MaxCounterexamples = o.Pattern.MaxCounterexamples
			po.Decls = append(po.Decls, o.Pattern.Decls...)
		}
		po.Decls = append(po.Decls, siblings...)
		pdiags := pattern.CheckFile(tf, po)
		r.Diagnostics = append(r.Diagnostics, pdiags...)
		if diag.List(pdiags).HasError() || o.CheckOnly {
			unitFailed = unitFailed || diag.List(pdiags).HasError()
			continue
		}

		le := lower.Env{Lines: diag.NewLineIndex(in.Src), Siblings: siblings}
		if o.Lower != nil {
			le = *o.Lower
			le.Lines = diag.NewLineIndex(in.Src)
			le.Siblings = siblings
		}
		if o.Exports != nil && tf.Package != "main" {
			exports := o.Exports(filepath.Dir(in.Path), tf.Package)
			if len(exports) > 0 {
				base := le.Exported
				le.Exported = func(sym *resolve.Symbol) bool {
					if sym.Package == "" && exports[sym.Name] {
						return true
					}
					return base != nil && base(sym)
				}
			}
		}
		coref, ldiags := lower.File(tf, &le)
		coref.SelfImportPath = o.SelfImportPath
		r.Core = coref
		r.Diagnostics = append(r.Diagnostics, ldiags...)
		if diag.List(ldiags).HasError() {
			unitFailed = true
			continue
		}

		out, err := codegen.File(coref, in.Src)
		if err != nil {
			return nil, err
		}
		r.Output = out
	}
	if unitFailed {
		for _, i := range impl {
			results[i].Output = nil
		}
	}
	return results, nil
}
