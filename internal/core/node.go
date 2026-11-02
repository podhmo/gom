package core

import (
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// Base carries the two fields every Core IR node has — its source
// span and its type — and supplies the Span/Type interface methods.
// It is exported so internal/lower and tests can build nodes.
//
// Invariant (docs/design.md section 2.1, item 7): every node keeps
// the span of the Gom source it came from. Synthesized nodes point
// at the span of the construct that produced them.
type Base struct {
	Sp  diag.Span
	Typ types.Type
}

// BaseOf returns the embedded Base — the uniform accessor the
// validator uses on sealed node interfaces.
func (b Base) BaseOf() Base { return b }

// Span returns the node's source span.
func (b Base) Span() diag.Span { return b.Sp }

// Type returns the node's type. For value-producing nodes it is the
// type of the value the node evaluates to; for statement-like nodes
// (Spawn, ForRange, TailCall, the escape checks) it is Unit or the
// consumed value's type as documented per node.
func (b Base) Type() types.Type { return b.Typ }

// Bind is a binder descriptor: the symbol a Let, parameter, case or
// receive arm introduces, with the bound value's type. Typ duplicates
// information resolve.Symbol does not carry (the declared-type table
// is a P1-5 concern), so the binder's type travels on the node.
//
// Discard marks the `_` binder — the value is evaluated and dropped
// (`ignore e`, `for _ in xs`, `_ <- ch`). A discard Bind may leave
// Sym nil; Typ still describes the dropped value.
type Bind struct {
	Sp      diag.Span
	Sym     *resolve.Symbol // KindVar; may be nil when Discard
	Typ     types.Type
	Discard bool // `_` binding
}

// Span returns the binder's source span.
func (b *Bind) Span() diag.Span { return b.Sp }

// Body is the unit of ANF evaluation order: an ordered list of
// bindings followed by the tail expression whose value is the body's
// result. Every complex expression a Body contains sits either at
// Lets[i].X (its result bound to a name) or at Tail (the result is
// the body's value, emitted in Return destination by codegen).
// Intermediate positions hold only atoms — evaluation order is fixed
// by construction (docs/design.md section 2.1, item 1).
type Body struct {
	Lets []*Let
	Tail Expr
}

// Let is one ordered binding `name = x`. The bound symbol is fresh —
// each Bind binds its Sym exactly once in the file. X may be any
// expression; nested complex expressions inside X are impossible
// because operands are atoms.
type Let struct {
	Sp   diag.Span
	Name *Bind
	X    Expr
}

// Span returns the binding's source span.
func (l *Let) Span() diag.Span { return l.Sp }

// Func is a function body shared by top-level FuncDecl, test bodies
// and Closure literals. It is the unit "function boundary" escape
// nodes point at (docs/decisions/2026-11-01-try-boundary.md).
type Func struct {
	Sp diag.Span
	// Sig is the function's signature in canonical flat form —
	// labels included, Ret never a *types.Func. For a FuncDecl it is
	// the declared signature; for a Closure it Equals the closure
	// node's type.
	Sig *types.Func
	// Params are the parameter binders in declaration order —
	// len(Params) is the declared arity. A function whose Sig has
	// more arrows than Params returns another function (P0-2 keeps
	// the declared-arity form for direct calls). A Closure's Func
	// binds at most one parameter — function *values* are the
	// canonical one-Go-func-per-arrow chain.
	Params []*Bind
	Body   *Body
	// TailLoop marks a function containing self-tail calls: codegen
	// wraps the body in `for { ... }` so TailCall nodes can emit
	// reassign-and-continue (docs/design.md section 4.5).
	TailLoop bool
}

// Span returns the function's source span.
func (f *Func) Span() diag.Span { return f.Sp }

// ResultType returns the type a *full* application of the whole
// arrow chain produces — Sig.Ret. For an arity-split function
// (len(Params) < len(Sig.Params), P0-2 rule 2) this is not what the
// emitted Go function returns; that is ResidualType.
func (f *Func) ResultType() types.Type { return f.Sig.Ret }

// ResidualType returns the type the emitted Go function's result
// has — what Body must produce. It is Sig.Ret when the declared
// arity covers the whole arrow chain; for an arity-split function
// such as `fn mk_add (a: Int) -> (Int -> Int)` (one binder, two
// signature params) it is the remaining arrow
// `Func{Sig.Params[len(Params):], Sig.Ret}`. A malformed Func whose
// declared arity exceeds the flat arrow length reports Sig.Ret —
// the validator flags the shape itself separately. Nil Sig reports
// nil so callers can degrade instead of panic.
func (f *Func) ResidualType() types.Type {
	if f.Sig == nil {
		return nil
	}
	if len(f.Params) >= len(f.Sig.Params) {
		return f.Sig.Ret
	}
	return &types.Func{Params: f.Sig.Params[len(f.Params):], Ret: f.Sig.Ret}
}
