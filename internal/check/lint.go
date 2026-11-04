package check

import (
	"fmt"

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
	for _, d := range c.decls {
		WalkDecl(d, func(e Expr) {
			switch e := e.(type) {
			case *Apply:
				c.lintChanMake(e)
			case *GoExpr:
				c.lintGoCaptures(e)
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

// lintGoCaptures warns once per variable a `go` body closes over
// whose type contains mutable values. A variable is captured when a
// value-level Var or member selection inside the body names a symbol
// whose binder lies outside the body's span — Def positions make the
// test purely lexical, so lets, lambda parameters and match binders
// introduced inside the body never count. Nested `go` blocks warn
// independently: each goroutine boundary is a separate sharing event.
func (c *checker) lintGoCaptures(g *GoExpr) {
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
