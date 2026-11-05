package goimport

// Standard bindings (docs/decisions/2026-11-05-error-result-bindings.md)
// — the compiler-curated table of declared result views for Go
// members whose type-derived default is wrong. An error constructor
// (`errors.New`, `fmt.Errorf`) returns a lone `error` the by-type
// default presents as `Result Unit GoError`; the binding declares
// `-> GoError`, the never-nil claim ErrorFromGo verifies at the
// boundary. The error combinators (`errors.Join`, `errors.Unwrap`)
// are documented nil-able, so their view is `Option GoError`.
//
// Entries apply at member derivation (sigOf) through the same
// result-admission classification BindExtern runs, so the two
// mechanisms cannot drift apart. A drifted entry — a Go release
// changing a bound signature so the declared view is no longer
// admissible — marks the member Hidden with the drift reason, the
// same channel any unrepresentable member reports through.
//
// An explicit `extern` entry still binds against the raw Go
// signature, so a user declaration overrides a standard binding:
// `extern go "errors" { fn New (msg: String) -> Result Unit
// GoError }` restores the type-derived default.

import (
	"fmt"
	"go/types"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/resolve"
	gt "github.com/podhmo/gom/internal/types"
)

// stdBind is one standard-binding entry: the declared result view
// that replaces the member's type-derived default.
type stdBind struct {
	// result is the declared result view — `GoError` (a never-nil
	// claim, verified at the boundary) or `Option GoError` (a
	// nil-able error value).
	result gt.Type
	// note is why the member carries the view — a comment for
	// readers and part of the drift reason a Hidden entry reports.
	note string
}

// stdBinds is the curated table, indexed package path → member name.
// Inaugural entries cover the errors package's constructors and
// combinators; (T, bool) → Option bindings (os.LookupEnv and
// friends) stay explicit externs until a concrete need shows up —
// see open-questions.md's standard-bindings entry.
var stdBinds = map[string]map[string]stdBind{
	"errors": {
		"New":    {result: gt.GoError, note: "documented constructor — never nil"},
		"Join":   {result: gt.Option(gt.GoError), note: "documented nil-able (nil when every input is nil)"},
		"Unwrap": {result: gt.Option(gt.GoError), note: "nil unless the chain continues"},
	},
	"fmt": {
		"Errorf": {result: gt.GoError, note: "documented constructor — never nil"},
	},
}

// stdBindOf returns the table entry for sym's member, or false.
func stdBindOf(pkg, name string) (stdBind, bool) {
	b, ok := stdBinds[pkg][name]
	return b, ok
}

// applyStdBind re-voices a member's derived signature views for a
// standard-binding entry: parameters stay derived, the declared
// result replaces the type-derived default, and the admission
// classification decides the call's result shape. A drifted entry
// reports its reason — the caller marks the member Hidden.
func (l *Loader) applyStdBind(sym *resolve.Symbol, b stdBind, sig *types.Signature, ck *check.Signature, gs *lower.GoSig) string {
	shape, reason := l.admitResults(
		fmt.Sprintf("the standard binding for `%s.%s`", sym.Package, sym.Name),
		sym, sig.Results(), b.result, gs.ResTypes)
	if reason != "" {
		return fmt.Sprintf("standard binding drifted (%s): %s", b.note, reason)
	}
	if fn, ok := gt.Chase(ck.Type).(*gt.Func); ok {
		ck.Type = &gt.Func{Params: fn.Params, Ret: b.result}
	} else {
		return fmt.Sprintf("standard binding drifted (%s): derived view %s is not a function",
			b.note, ck.Type)
	}
	gs.Results = shape
	return ""
}
