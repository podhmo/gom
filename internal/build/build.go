package build

import (
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
	if !utf8.ValidString(in.Path) {
		r.Diagnostics = append(r.Diagnostics,
			diag.Error("E0002", "input path is not valid UTF-8").
				Set("path", in.Path))
		return r, nil
	}
	if i := strings.IndexFunc(in.Path, unicode.IsControl); i >= 0 {
		r.Diagnostics = append(r.Diagnostics,
			diag.Error("E0002", "input path contains a control character").
				Set("path", in.Path).
				Set("offset", i))
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

	res, diags := resolve.File(f, &resolve.Config{Loader: o.Loader, Modules: o.Modules})
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
	}
	diags = pattern.CheckFile(tf, po)
	r.Diagnostics = append(r.Diagnostics, diags...)
	if diags.HasError() || o.CheckOnly {
		return r, nil
	}

	le := lower.Env{Lines: diag.NewLineIndex(in.Src)}
	if o.Lower != nil {
		le = *o.Lower
		le.Lines = diag.NewLineIndex(in.Src)
	}
	coref, ldiags := lower.File(tf, &le)
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
