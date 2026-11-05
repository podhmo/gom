package check

import (
	"fmt"
	"path"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// The mutable-sharing lint of spec section 10 — a post-inference
// sweep over the typed declarations, run as the last step of Check.
// It needs solved types (a `Chan.make`'s element is a fresh meta at
// the call site), the checker's type-declaration tables (containment
// expands declared bodies), and shadowedBuiltin (builtin recognition
// is by declaration, not spelling), so it lives inside check even
// though its findings carry Phase "lint" in the code table. The two
// rules — W0701 on `Chan.make` and W0702 on `go { ... }` captures —
// warn rather than error, so they never gate a later phase. See
// docs/decisions/2026-11-04-mutable-sharing-lint.md.

// rtMakeChan identifies the `Chan.make` member on a typed
// application: the compiler-owned member table forwards it to
// rt.MakeChan (internal/stdlib/builtin.go), so the callee symbol
// carries the runtime package path. Aliasing (`let mk = Chan.make`)
// discards that symbol and is intentionally not recognized.
const (
	rtPath        = "github.com/podhmo/gom/rt"
	rtMakeChanSym = "MakeChan"
)

// lintMutableSharing reports W0701 (a channel made with a mutable
// element type) and W0702 (a `go` body capturing a variable whose
// type contains mutable values). Runs unconditionally after
// finalize: warning-only output cannot produce false errors, and
// every type the sweep consults is a post-default solution — an
// unsolved leftover meta is simply not mutable.
func (c *checker) lintMutableSharing() {
	// W0702's capture walk descends into nested `go` bodies, so the
	// same use site is seen once per enclosing boundary. Those
	// reports are byte-identical — same span, same message, same
	// data — so goWarned collapses them to the first.
	goWarned := map[diag.Span]bool{}
	for _, d := range c.decls {
		WalkDecl(d, func(e Expr) {
			switch e := e.(type) {
			case *Apply:
				c.lintChanMake(e)
				c.lintGoSliceCopy(e)
			case *GoExpr:
				c.lintGoCaptures(e, goWarned)
			case *Lit, *Var, *MemberExpr, *Hole, *StrInterp, *Lambda,
				*If, *Match, *Block, *Let, *Ignore, *Try, *For,
				*SelectExpr, *ListLit, *TupleLit, *Spread, *RecordLit,
				*Ascribe, *Unary, *Binary:
			default:
				panic("unreachable")
			}
		})
	}
}

// lintChanMake warns when a `Chan.make` call's element type contains
// mutable values — the channel carries the value across goroutines,
// so `Chan (Ref Int)` shares mutation by design, not by accident.
// chanElem answers "is this really a builtin channel": under a
// file-local `type Chan` the result type spells the local ADT and the
// shadowed-signature E0415 already covers the site.
func (c *checker) lintChanMake(ap *Apply) {
	if ap.Call.Kind != CallGo || ap.Call.Callee == nil ||
		ap.Call.Callee.Name != rtMakeChanSym ||
		ap.Call.Callee.Package != rtPath {
		return
	}
	elem, _, ok := c.chanElem(ap.Type())
	if !ok {
		return
	}
	if !c.containsMutable(elem, map[conKey]bool{}) {
		return
	}
	elem = types.Chase(elem)
	c.diags.Add(diag.Warning("W0701",
		fmt.Sprintf("a channel's element type %s contains mutable values", elem)).
		At(ap.Span()).Set("type", elem.String()))
}

// lintGoSliceCopy reports W0501 for a Unit-result Go call that
// passes a `List` argument into a `List` (slice) parameter: the
// boundary copies the list into a fresh []T (spec 8.5), so a callee
// whose contract is in-place mutation — sort.Strings is the
// canonical case — has no observable effect on the Gom value. The
// result-shape test is the false-positive control: a read-only
// []T consumer returns a value (strings.Join, sort.SearchStrings),
// so it never trips the rule; the signal.Notify family is the
// documented exception, and the wording stays conditional for it.
// A declared `a` type parameter is not a List — `Ptr.set p xs`
// binds the list itself, no copy — so generic parameter positions
// stay silent. The copy test is over the callee's *declared*
// parameter and the argument's own type after peeling the implicit
// slice Ascribe — and the Ascribe is itself the copy evidence: a
// bare `Go.Slice` reaches its []T slot verbatim (a `..gs` spread
// expands the slice itself, a `...[]T` element shares backing),
// so only an ascribe-materialized `Go.Slice` counts as copied.
func (c *checker) lintGoSliceCopy(ap *Apply) {
	if ap.Call.Kind != CallGo || ap.Call.Callee == nil {
		return
	}
	if !c.isBuiltinCon(types.Chase(ap.Type()), "Unit") {
		return
	}
	sig, ok := c.sigOf(ap.Call.Callee, ap.Sp)
	if !ok {
		return
	}
	params, _ := flattenFunc(sig.Type)
	for i, a := range ap.Args {
		pi := i
		if ap.Call.ArgToParam != nil && i < len(ap.Call.ArgToParam) {
			pi = ap.Call.ArgToParam[i]
		}
		if pi < 0 || pi >= len(params) {
			continue
		}
		got := a.Value
		if sp, ok := got.(*Spread); ok {
			got = sp.X
		}
		// The implicit List ↔ Go.Slice boundary conversion wraps
		// the argument in a Slice-marked Ascribe — the copy
		// question is decided by the value underneath, so peel it.
		materialized := false
		if as, ok := got.(*Ascribe); ok && as.Slice {
			got = as.X
			materialized = true
		}
		gotT := types.Chase(got.Type())
		want := types.Chase(params[pi].Type)
		gotList := c.isBuiltinCon(gotT, "List")
		gotSlice := isGoSliceCon(gotT) && materialized
		wantList := c.isBuiltinCon(want, "List")
		wantSlice := isGoSliceCon(want)
		// An inline `Go.conv xs : Go.Slice T` copies a `List`
		// operand into a fresh slice at the call site — the same
		// miss the implicit boundary conversion warns for, in the
		// Conv-marked spelling. Nested conv ascribes peel to the
		// copied source; a `Go.Slice`-operand conv (the verbatim
		// pun) shares backing and stays silent, and so does the
		// let-bound shape: a variable holding the copy is a live
		// `Go.Slice` value, passed verbatim.
		convCopied := false
		if as, ok := got.(*Ascribe); ok && as.Conv && isGoSliceCon(gotT) {
			src := as.X
			for {
				inner, ok := src.(*Ascribe)
				if !ok || !inner.Conv {
					break
				}
				src = inner.X
			}
			convCopied = c.isBuiltinCon(types.Chase(src.Type()), "List")
		}
		// A `List` argument always crosses the boundary by copy —
		// `.ToSlice()`/boxing — whether it reaches a `List`- or
		// `Go.Slice`-spelled parameter. A `Go.Slice` argument
		// copies only when the checker materialized it into a
		// `List` first (the peeled ascribe); bare `Go.Slice`
		// arguments pass their backing verbatim.
		copied := convCopied && (wantList || wantSlice) ||
			(wantList && (gotList || gotSlice)) || (wantSlice && gotList)
		if !copied {
			continue
		}
		callee := ap.Call.Callee.Name
		if ap.Call.Callee.Package != "" {
			callee = path.Base(ap.Call.Callee.Package) + "." + callee
		}
		argT := got.Type().String()
		c.diags.Add(diag.Warning("W0501",
			fmt.Sprintf("`%s` returns Unit and receives its `%s` argument as a copied []T — a callee that mutates the slice in place has no effect on the Gom value", callee, argT)).
			At(a.Span()).Set("callee", callee).Set("argument", argT))
	}
}

// isGoSliceCon reports whether the chased type is `Go.Slice _`.
func isGoSliceCon(t types.Type) bool {
	con, ok := t.(*types.Con)
	return ok && con.Qual == "Go" && con.Name == "Slice"
}

// lintGoCaptures warns once per variable a `go` body closes over
// whose type contains mutable values. A variable is captured when a
// value-level Var or member selection inside the body names a symbol
// whose binder lies outside the body's span — Def positions make the
// test purely lexical, so lets, lambda parameters and match binders
// introduced inside the body never count. Nested `go` blocks warn
// independently: each goroutine boundary is a separate sharing
// event. When the same use site is reachable through several
// boundaries — `go { go { r } }` — the events produce identical
// warnings, so reported (shared across the sweep) keeps only the
// first; a use inside exactly one boundary still warns once.
func (c *checker) lintGoCaptures(g *GoExpr, reported map[diag.Span]bool) {
	if g.Body == nil {
		return
	}
	bsp := g.Body.Span()
	seen := map[*resolve.Symbol]bool{}
	WalkExpr(g.Body, func(e Expr) {
		var sym *resolve.Symbol
		var t types.Type
		var sp diag.Span
		switch e := e.(type) {
		case *Var:
			sym, t, sp = e.Sym, e.Type(), e.Sp
		case *MemberExpr:
			sym, t, sp = e.Sym, e.Type(), e.Sp
		case *Lit, *Apply, *Hole, *StrInterp, *Lambda, *If, *Match,
			*Block, *Let, *Ignore, *Try, *GoExpr, *For, *SelectExpr,
			*ListLit, *TupleLit, *Spread, *RecordLit, *Ascribe, *Unary,
			*Binary:
			return
		default:
			panic("unreachable")
		}
		if sym == nil || seen[sym] {
			return
		}
		// KindVar covers lets, parameters and pattern variables — the
		// bindings a `go` body can share. A generalized value-let
		// flips to KindFunc for emission but still denotes a value:
		// thunks counts it. Functions, constructors, fields, methods
		// and modules are not capturable state.
		if sym.Kind != resolve.KindVar && !c.thunks[sym] {
			return
		}
		if def := sym.DefSpan(); def.File == bsp.File &&
			def.Start >= bsp.Start && def.End <= bsp.End {
			return // bound inside the body — not a capture
		}
		seen[sym] = true
		if !c.containsMutable(t, map[conKey]bool{}) {
			return
		}
		if reported[sp] {
			// An enclosing boundary already warned at this use
			// site — the duplicate would carry no distinct
			// information (span, message and data are the
			// same). Distinct sites still warn apart.
			return
		}
		reported[sp] = true
		t = types.Chase(t)
		c.diags.Add(diag.Warning("W0702",
			fmt.Sprintf("`go {}` captures `%s` whose type %s contains mutable values", sym.Name, t)).
			At(sp).Set("name", sym.Name).Set("type", t.String()))
	})
}

// containsMutable reports whether a value of type t can reach the
// mutable constructors of spec section 10 — `Ref a`, `Array a`,
// `Ptr T`, `Go.Slice a`, `Go.Map k v` — either directly or through
// type arguments, tuple elements, or the fields and payloads of a
// declared type body (substituted with the use's own arguments). The
// judgment is lexical like every builtin recognition: under a
// file-local `type Ref` the unqualified name denotes the local ADT,
// which is expanded like any declaration instead of auto-flagging.
//
// Function types are opaque to the walk — a function value is itself
// immutable, and what a closure shares is not visible in its type —
// and so are opaque Go named types (`sync.Mutex` cannot be
// inspected). seen guards declared-body expansion so recursive
// declarations terminate.
func (c *checker) containsMutable(t types.Type, seen map[conKey]bool) bool {
	switch t := types.Chase(t).(type) {
	case *types.Meta, *types.Var:
		// An undetermined or abstract type carries no decided
		// mutable constructor — the concrete use sites that pin it
		// warn there instead.
		return false
	case *types.Func:
		return false
	case *types.Tuple:
		for _, e := range t.Elems {
			if c.containsMutable(e, seen) {
				return true
			}
		}
		return false
	case *types.Go:
		for _, a := range t.Args {
			if c.containsMutable(a, seen) {
				return true
			}
		}
		return false
	case *types.Con:
		switch {
		case t.Qual == "Go":
			if t.Name == "Slice" || t.Name == "Map" {
				return true
			}
		case t.Qual == "":
			switch t.Name {
			case "Ref", "Array", "Ptr":
				if !c.shadowedBuiltin(t.Name) {
					return true
				}
			}
		}
		for _, a := range t.Args {
			if c.containsMutable(a, seen) {
				return true
			}
		}
		key := conKey{qual: t.Qual, name: t.Name}
		ti := c.tyCons[key]
		if ti == nil || seen[key] {
			return false
		}
		seen[key] = true
		sub := types.Subst{}
		for i, v := range ti.params {
			if i < len(t.Args) {
				sub[v.ID] = t.Args[i]
			}
		}
		switch ti.kind {
		case kindAbstract:
			return false
		case kindAlias:
			return c.containsMutable(sub.Apply(ti.alias), seen)
		case kindRecord:
			for _, f := range ti.fields {
				if c.containsMutable(sub.Apply(f.typ), seen) {
					return true
				}
			}
			return false
		case kindSum:
			for _, v := range ti.variants {
				for _, f := range v.fields {
					if c.containsMutable(sub.Apply(f.typ), seen) {
						return true
					}
				}
			}
			return false
		default:
			panic("unreachable")
		}
	default:
		panic("unreachable")
	}
}
