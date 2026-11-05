package lower

import (
	"fmt"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// This file is the pattern compiler — the middle-end that turns
// pattern-matching surface syntax into Core decision trees.
//
// The algorithm is a queue-of-obligations variant of the classic
// match compiler: every pending arm (armState) carries a todo list
// of (subject atom × pattern) obligations left to discharge, the
// variable bindings earned so far, and a leaf builder. At each step
// the head obligation of the first surviving arm drives the group:
//
//   - a structural head (variable, wildcard, tuple, record, `as`,
//     `[..rest]`, `()`) never fails — it is expanded in place;
//   - an or-pattern splices its alternatives into the group;
//   - a test head (constructor, literal, list shape) builds a
//     DSwitch whose cases specialize every arm against each outcome.
//
// Because earned binds and the guard/body run under a renaming
// substitution (the arm's pattern symbols map to fresh temporaries
// the binds were lowered to), an arm folded into several leaves —
// or-patterns, wildcard-expanded constructors — duplicates cleanly
// without scope collisions.

// oblig is one pending obligation: the atom `subj` must match `pat`.
type oblig struct {
	subj core.Atom
	pat  check.Pat
}

// pendingBind is a variable binding earned along a path — flushed
// to a fresh-temporary let at the leaf.
type pendingBind struct {
	sym  *resolve.Symbol
	atom core.Atom
}

// armState is one pending alternative inside a decision-tree group.
type armState struct {
	src   *check.MatchArm // nil for a synthetic (destructure) arm
	sp    diag.Span
	todo  []oblig
	binds []pendingBind
	// leaf overrides the leaf-body computation; nil lowers src.Body.
	leaf func(l *lowerer, w *armState, bs *seq, subst map[*resolve.Symbol]*resolve.Symbol) core.Expr
}

func (w *armState) clone() *armState {
	return &armState{
		src:   w.src,
		sp:    w.sp,
		todo:  append([]oblig{}, w.todo...),
		binds: append([]pendingBind{}, w.binds...),
		leaf:  w.leaf,
	}
}

// structural reports whether a head pattern expands without a test —
// it can never fail on its subject.
func structural(p check.Pat) bool {
	switch p := p.(type) {
	case *check.WildPat, *check.VarPat, *check.TuplePat, *check.RecordPat, *check.AsPat:
		return true
	case *check.LitPat:
		return p.Kind == syntax.LitUnit
	case *check.ListPat:
		// `[..rest]` binds the whole list — never fails.
		return len(p.Elems) == 0 && p.Rest != nil
	case *check.CtorPat, *check.OrPat:
		return false
	default:
		panic("unreachable")
	}
}

// compile emits the decision tree for a group of pending arms,
// discriminating `subj` (kept for DPanic fallbacks). Leading
// structural work and leaf-prefix binds land in `s` — which must be
// the sequence of the body that will contain this tree, so the work
// runs exactly on the paths that reach the tree.
func (l *lowerer) compile(states []*armState, subj core.Atom, s *seq, resT types.Type) core.DTree {
	for {
		if len(states) == 0 {
			return l.dpanic(subj, resT)
		}
		w := states[0]
		if len(w.todo) == 0 {
			return l.armLeaf(w, states[1:], subj, s, resT)
		}
		ob := w.todo[0]
		if structural(ob.pat) {
			w.todo = l.expandStructural(w, ob)
			continue
		}
		if orp, ok := ob.pat.(*check.OrPat); ok {
			var alts []*armState
			for _, alt := range orp.Alts {
				nw := w.clone()
				nw.todo = append([]oblig{{subj: ob.subj, pat: alt}}, w.todo[1:]...)
				alts = append(alts, nw)
			}
			states = append(alts, states[1:]...)
			continue
		}
		return l.switchOn(states, s, resT)
	}
}

// expandStructural discharges a never-failing head obligation in
// place — recording bindings and queueing sub-obligations on
// projections — and returns the state's remaining todo.
func (l *lowerer) expandStructural(w *armState, ob oblig) []oblig {
	rest := w.todo[1:]
	switch p := ob.pat.(type) {
	case *check.WildPat:
		return rest
	case *check.LitPat:
		// Unit — carries no information.
		return rest
	case *check.VarPat:
		w.binds = append(w.binds, pendingBind{sym: p.Sym, atom: ob.subj})
		return rest
	case *check.AsPat:
		w.binds = append(w.binds, pendingBind{sym: p.Sym, atom: ob.subj})
		return append([]oblig{{subj: ob.subj, pat: p.Pat}}, rest...)
	case *check.TuplePat:
		var head []oblig
		for i, e := range p.Elems {
			head = append(head, oblig{
				subj: &core.FieldGet{
					Sp: p.Sp, Typ: e.Type(),
					Recv: ob.subj, Index: i,
				},
				pat: e,
			})
		}
		return append(head, rest...)
	case *check.RecordPat:
		var head []oblig
		for _, fp := range p.Fields {
			idx := fp.Index
			if idx < 0 {
				idx = l.fieldIndex(ob.subj.Type(), fp.Sym)
				if idx < 0 {
					l.err(fp.Sp, fmt.Sprintf("record field %s has no known index", fp.Name))
					idx = 0 // keep the tree well-formed; the diagnostic stands
				}
			}
			head = append(head, oblig{
				subj: &core.FieldGet{
					Sp: fp.Sp, Typ: fp.Pat.Type(),
					Recv: ob.subj, Field: fp.Sym, Index: idx,
				},
				pat: fp.Pat,
			})
		}
		return append(head, rest...)
	case *check.ListPat:
		// `[..rest]` — the tail binds the whole subject.
		return append([]oblig{{subj: ob.subj, pat: p.Rest}}, rest...)
	case *check.CtorPat, *check.OrPat:
		panic("unreachable")
	default:
		panic("unreachable")
	}
}

// switchOn builds a DSwitch from the first arm's test head.
func (l *lowerer) switchOn(states []*armState, s *seq, resT types.Type) core.DTree {
	ob := states[0].todo[0]
	switch ob.pat.(type) {
	case *check.CtorPat:
		return l.ctorSwitch(states, s, resT)
	case *check.LitPat:
		return l.litSwitch(states, s, resT)
	case *check.ListPat:
		return l.listSwitch(states, s, resT)
	case *check.VarPat, *check.WildPat, *check.TuplePat, *check.RecordPat, *check.AsPat, *check.OrPat:
		panic("unreachable")
	default:
		panic("unreachable")
	}
}

// continueWith builds a sub-tree continuation (a DGuard's Else, a
// SwitchValue default) that carries its own leading work inside the
// tree — a continuation has no enclosing case body to hold it, so a
// DLeaf body wraps the sequence. Without this, e.g. a trailing arm's
// guard would evaluate before the switch even when a case matched.
func (l *lowerer) continueWith(states []*armState, subj core.Atom, resT types.Type) core.DTree {
	cs := &seq{}
	tree := l.compile(states, nextSubj(states, subj), cs, resT)
	if len(cs.lets) == 0 {
		return tree
	}
	if dl, ok := tree.(*core.DLeaf); ok {
		dl.Body.Lets = append(cs.lets, dl.Body.Lets...)
		return dl
	}
	return &core.DLeaf{
		Sp: tree.Span(), Typ: resT,
		Body: &core.Body{Lets: cs.lets, Tail: &core.Match{
			Sp: tree.Span(), Typ: resT,
			Tree: tree,
		}},
	}
}

// headAlts lists a state's head alternatives — the or-pattern's
// branches, or the single head.
func headAlts(w *armState) []check.Pat {
	if len(w.todo) == 0 {
		return nil
	}
	p := w.todo[0].pat
	if op, ok := p.(*check.OrPat); ok {
		return op.Alts
	}
	return []check.Pat{p}
}

// sameCtor reports constructor identity: the symbol object when both
// sides carry the real one, the (package, name) pair when lowering
// synthesised a builtin ctor reference.
func sameCtor(a, b *resolve.Symbol) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Kind == b.Kind && a.Package == b.Package && a.Name == b.Name
}

// ctorSwitch builds a SwitchType (sealed-ADT type switch) or
// SwitchTag (Result tag switch) discriminating the head subject.
func (l *lowerer) ctorSwitch(states []*armState, _ *seq, resT types.Type) core.DTree {
	ob := states[0].todo[0]
	subj := ob.subj
	// Chase bound metas — a let-bound subject's type can still be `?n`
	// bound to the ADT when the match runs over an inferred binding.
	con, _ := types.Chase(subj.Type()).(*types.Con)
	kind := core.SwitchType
	if con != nil && l.isResultCon(con) {
		kind = core.SwitchTag
	}
	variants := l.variantsOf(con)
	if variants == nil {
		l.err(ob.pat.Span(), fmt.Sprintf("cannot enumerate variants of %s", typeName(subj.Type())))
		return l.dpanic(subj, resT)
	}
	// Adopt the real constructor symbols the arms carry when they
	// match a variant by name — builtin variants synthesise theirs.
	for i := range variants {
		for _, w := range states {
			for _, p := range headAlts(w) {
				if cp, ok := p.(*check.CtorPat); ok && sameCtor(cp.Ctor, variants[i].Sym) {
					variants[i].Sym = cp.Ctor
				}
			}
		}
	}
	sw := &core.DSwitch{
		Sp: ob.pat.Span(), Typ: resT,
		Subj: subj, Kind: kind,
	}
	for _, v := range variants {
		// Does any state read this variant's payload? Only then is
		// the case bind needed.
		need := false
		for _, w := range states {
			for _, p := range headAlts(w) {
				if cp, ok := p.(*check.CtorPat); ok && sameCtor(cp.Ctor, v.Sym) &&
					(len(cp.Args) > 0 || len(cp.Fields) > 0) {
					need = true
				}
			}
		}
		var binds []*core.Bind
		var narrowed core.Atom
		if need {
			var pt types.Type
			if kind == core.SwitchTag && len(v.Fields) > 0 {
				pt = v.Fields[0].Type
			} else {
				pt = subj.Type()
			}
			nb := l.tmpBind(ob.pat.Span(), pt)
			binds = append(binds, nb)
			narrowed = varOf(ob.pat.Span(), nb)
		}
		var caseStates []*armState
		for _, w := range states {
			caseStates = append(caseStates, l.statesForCtor(w, v, kind, narrowed)...)
		}
		if len(caseStates) == 0 {
			continue // unarmed variant — the panic default covers it
		}
		sw.Cases = append(sw.Cases, core.DCase{
			Sp:    ob.pat.Span(),
			Test:  &core.DCtorTest{Sp: ob.pat.Span(), Ctor: v.Sym},
			Binds: binds,
			Body:  l.caseBody(caseStates, subj, resT),
		})
	}
	if len(sw.Cases) == 0 {
		l.err(ob.pat.Span(), "match over a sum type produced no cases")
	}
	// Malformed Go-side values always panic — even when the wildcard
	// expansion covered every declared variant.
	sw.Default = l.dpanic(subj, resT)
	return sw
}

// statesForCtor returns w's continuations under the outcome "subj is
// variant v". Structural and exhausted states match every outcome;
// or-pattern heads splice the matching alternatives.
//
// Every returned state is a clone: compile discharges a state's
// structural prefix destructively (todo pops, binds append), and the
// shared state must not carry one case's work into the next case —
// a survivor whose head advances past a structural oblig gets
// misclassified as a test on the next discriminated subject.
func (l *lowerer) statesForCtor(w *armState, v Variant, kind core.SwitchKind, narrowed core.Atom) []*armState {
	if len(w.todo) == 0 {
		return []*armState{w.clone()}
	}
	head := w.todo[0]
	switch p := head.pat.(type) {
	case *check.CtorPat:
		if !sameCtor(p.Ctor, v.Sym) {
			return nil
		}
		nw := w.clone()
		var obligs []oblig
		if narrowed != nil {
			obligs = l.payloadObligs(p, v, kind, narrowed)
		}
		nw.todo = append(obligs, w.todo[1:]...)
		return []*armState{nw}
	case *check.OrPat:
		var out []*armState
		for _, alt := range p.Alts {
			nw := w.clone()
			nw.todo = append([]oblig{{subj: head.subj, pat: alt}}, w.todo[1:]...)
			out = append(out, l.statesForCtor(nw, v, kind, narrowed)...)
		}
		return out
	case *check.LitPat, *check.ListPat:
		// A test head of another kind never matches this outcome.
		return nil
	case *check.VarPat, *check.WildPat, *check.TuplePat, *check.RecordPat, *check.AsPat:
		// Cannot-fail heads survive into every outcome.
		return []*armState{w.clone()}
	default:
		panic("unreachable")
	}
}

// payloadObligs turns a matched CtorPat's argument patterns into
// obligations on the case binds — FieldGet projections on the
// narrowed subject for SwitchType, the bound payload itself for
// SwitchTag's single-slot variants.
func (l *lowerer) payloadObligs(p *check.CtorPat, v Variant, kind core.SwitchKind, narrowed core.Atom) []oblig {
	slot := func(idx int, typ types.Type) core.Atom {
		if kind == core.SwitchTag {
			return narrowed
		}
		var fs *resolve.Symbol
		ft := typ
		if idx >= 0 && idx < len(v.Fields) {
			fs = v.Fields[idx].Sym
			ft = v.Fields[idx].Type
		}
		return &core.FieldGet{
			Sp: p.Sp, Typ: ft,
			Recv: narrowed, Field: fs, Index: idx,
		}
	}
	var out []oblig
	if len(p.Fields) > 0 {
		for _, fp := range p.Fields {
			idx := fp.Index
			if idx < 0 {
				idx = ctorFieldIndex(v, fp)
			}
			if idx < 0 {
				l.err(fp.Sp, fmt.Sprintf("constructor %s has no field %s", v.Sym.Name, fp.Name))
				continue
			}
			out = append(out, oblig{subj: slot(idx, fp.Pat.Type()), pat: fp.Pat})
		}
		return out
	}
	for i, a := range p.Args {
		idx := i
		if a.Label != "" {
			idx = ctorLabelIndex(v, a.Label)
		}
		if idx < 0 || idx >= len(v.Fields) {
			l.err(p.Sp, fmt.Sprintf("constructor %s has no payload slot %d", v.Sym.Name, i))
			continue
		}
		out = append(out, oblig{subj: slot(idx, a.Pat.Type()), pat: a.Pat})
	}
	return out
}

func ctorFieldIndex(v Variant, fp check.FieldPat) int {
	for i, f := range v.Fields {
		if fp.Sym != nil && f.Sym == fp.Sym {
			return i
		}
	}
	for i, f := range v.Fields {
		if f.Name == fp.Name {
			return i
		}
	}
	return -1
}

func ctorLabelIndex(v Variant, label string) int {
	for i, f := range v.Fields {
		if f.Label == label {
			return i
		}
	}
	return -1
}

// litSwitch builds a SwitchValue discriminating on literal tests.
func (l *lowerer) litSwitch(states []*armState, _ *seq, resT types.Type) core.DTree {
	ob := states[0].todo[0]
	subj := ob.subj
	key := func(k syntax.LitKind, text string) string {
		return fmt.Sprintf("%d\x00%s", k, text)
	}
	var lits []*check.LitPat
	seen := map[string]bool{}
	addLit := func(p check.Pat) {
		if lp, ok := p.(*check.LitPat); ok && lp.Kind != syntax.LitUnit {
			k := key(lp.Kind, lp.Text)
			if !seen[k] {
				seen[k] = true
				lits = append(lits, lp)
			}
		}
	}
	for _, w := range states {
		for _, p := range headAlts(w) {
			addLit(p)
		}
	}
	sw := &core.DSwitch{
		Sp: ob.pat.Span(), Typ: resT,
		Subj: subj, Kind: core.SwitchValue,
	}
	for _, lp := range lits {
		var cs []*armState
		for _, w := range states {
			cs = append(cs, l.statesForLit(w, lp)...)
		}
		if len(cs) == 0 {
			continue
		}
		sw.Cases = append(sw.Cases, core.DCase{
			Sp:   lp.Sp,
			Test: &core.DLitTest{Sp: lp.Sp, Kind: lp.Kind, Text: lp.Text},
			Body: l.caseBody(cs, subj, resT),
		})
	}
	var ds []*armState
	for _, w := range states {
		ds = append(ds, l.statesForLitDefault(w)...)
	}
	if len(ds) == 0 {
		sw.Default = l.dpanic(subj, resT)
	} else {
		sw.Default = l.continueWith(ds, subj, resT)
	}
	return sw
}

// statesForLit returns continuations under "subj == lp".
func (l *lowerer) statesForLit(w *armState, lp *check.LitPat) []*armState {
	if len(w.todo) == 0 {
		return []*armState{w.clone()}
	}
	head := w.todo[0]
	switch p := head.pat.(type) {
	case *check.LitPat:
		if p.Kind == lp.Kind && p.Text == lp.Text {
			nw := w.clone()
			nw.todo = w.todo[1:]
			return []*armState{nw}
		}
		return nil
	case *check.OrPat:
		var out []*armState
		for _, alt := range p.Alts {
			nw := w.clone()
			nw.todo = append([]oblig{{subj: head.subj, pat: alt}}, w.todo[1:]...)
			out = append(out, l.statesForLit(nw, lp)...)
		}
		return out
	case *check.CtorPat, *check.ListPat:
		return nil
	case *check.VarPat, *check.WildPat, *check.TuplePat, *check.RecordPat, *check.AsPat:
		// Cannot-fail heads survive into every outcome.
		return []*armState{w.clone()}
	default:
		panic("unreachable")
	}
}

// statesForLitDefault returns continuations matching no cased
// literal — the wildcard-and-other-pattern residue.
func (l *lowerer) statesForLitDefault(w *armState) []*armState {
	if len(w.todo) == 0 {
		return []*armState{w.clone()}
	}
	head := w.todo[0]
	switch p := head.pat.(type) {
	case *check.LitPat:
		return nil
	case *check.OrPat:
		var out []*armState
		for _, alt := range p.Alts {
			if _, lit := alt.(*check.LitPat); lit {
				continue
			}
			nw := w.clone()
			nw.todo = append([]oblig{{subj: head.subj, pat: alt}}, w.todo[1:]...)
			out = append(out, nw)
		}
		return out
	case *check.CtorPat, *check.ListPat:
		return nil
	case *check.VarPat, *check.WildPat, *check.TuplePat, *check.RecordPat, *check.AsPat:
		// Cannot-fail heads survive into every outcome.
		return []*armState{w.clone()}
	default:
		panic("unreachable")
	}
}

// listSwitch builds a SwitchList on empty/nonempty shape.
func (l *lowerer) listSwitch(states []*armState, _ *seq, resT types.Type) core.DTree {
	ob := states[0].todo[0]
	subj := ob.subj
	sw := &core.DSwitch{
		Sp: ob.pat.Span(), Typ: resT,
		Subj: subj, Kind: core.SwitchList,
	}
	var nilStates, consStates []*armState
	for _, w := range states {
		nilStates = append(nilStates, l.statesForList(w, false)...)
		consStates = append(consStates, l.statesForList(w, true)...)
	}
	if len(nilStates) > 0 {
		sw.Cases = append(sw.Cases, core.DCase{
			Sp:   ob.pat.Span(),
			Test: &core.DListTest{Sp: ob.pat.Span(), Cons: false},
			Body: l.caseBody(nilStates, subj, resT),
		})
	}
	if len(consStates) > 0 {
		// A cons case always binds head and tail; the element type
		// comes from the list type when it is known.
		elemT := l.listElem(subj.Type())
		hb := l.tmpBind(ob.pat.Span(), elemT)
		tb := l.tmpBind(ob.pat.Span(), subj.Type())
		head, tail := varOf(ob.pat.Span(), hb), varOf(ob.pat.Span(), tb)
		var specialized []*armState
		for _, w := range consStates {
			specialized = append(specialized, l.statesForCons(w, head, tail)...)
		}
		sw.Cases = append(sw.Cases, core.DCase{
			Sp:    ob.pat.Span(),
			Test:  &core.DListTest{Sp: ob.pat.Span(), Cons: true},
			Binds: []*core.Bind{hb, tb},
			Body:  l.caseBody(specialized, subj, resT),
		})
	}
	sw.Default = l.dpanic(subj, resT)
	return sw
}

// statesForList partitions a state by list outcome — cons=false is
// the `[]` shape, cons=true any non-empty shape.
func (l *lowerer) statesForList(w *armState, cons bool) []*armState {
	if len(w.todo) == 0 {
		return []*armState{w.clone()}
	}
	head := w.todo[0]
	switch p := head.pat.(type) {
	case *check.ListPat:
		switch {
		case len(p.Elems) == 0 && p.Rest != nil:
			// `[..rest]` never fails — it binds the whole subject
			// on either outcome, like the structural path does.
			nw := w.clone()
			nw.todo = append([]oblig{{subj: head.subj, pat: p.Rest}}, w.todo[1:]...)
			return []*armState{nw}
		case len(p.Elems) == 0:
			// `[]` matches only the nil outcome.
			if cons {
				return nil
			}
			nw := w.clone()
			nw.todo = w.todo[1:]
			return []*armState{nw}
		case !cons:
			// `[x, ...]` needs a cons cell.
			return nil
		default:
			// Keep the state; statesForCons specializes it on the
			// bound head/tail.
			return []*armState{w.clone()}
		}
	case *check.OrPat:
		var out []*armState
		for _, alt := range p.Alts {
			nw := w.clone()
			nw.todo = append([]oblig{{subj: head.subj, pat: alt}}, w.todo[1:]...)
			out = append(out, l.statesForList(nw, cons)...)
		}
		return out
	case *check.CtorPat, *check.LitPat:
		return nil
	case *check.VarPat, *check.WildPat, *check.TuplePat, *check.RecordPat, *check.AsPat:
		// Cannot-fail heads survive into every outcome.
		return []*armState{w.clone()}
	default:
		panic("unreachable")
	}
}

// statesForCons specializes a cons-matching state on the bound
// head/tail atoms: element 0 against head, the shortened pattern
// against tail.
func (l *lowerer) statesForCons(w *armState, head, tail core.Atom) []*armState {
	if len(w.todo) == 0 {
		return []*armState{w.clone()}
	}
	ob := w.todo[0]
	lp, ok := ob.pat.(*check.ListPat)
	if !ok {
		// Structural and empty states pass through unchanged.
		return []*armState{w.clone()}
	}
	if len(lp.Elems) == 0 {
		if lp.Rest == nil {
			return nil
		}
		// `[..rest]` binds the whole subject — normally discharged
		// by statesForList or the structural path; kept here so a
		// stray occurrence binds rather than indexing an empty
		// Elems.
		nw := w.clone()
		nw.todo = append([]oblig{{subj: ob.subj, pat: lp.Rest}}, w.todo[1:]...)
		return []*armState{nw}
	}
	nw := w.clone()
	var obligs []oblig
	obligs = append(obligs, oblig{subj: head, pat: lp.Elems[0]})
	restPat := check.Pat(&check.ListPat{
		Base: lp.Base, Elems: lp.Elems[1:], Rest: lp.Rest,
	})
	if len(lp.Elems) == 1 && lp.Rest == nil {
		// `[x]` — the tail must additionally be empty.
		restPat = &check.ListPat{Base: lp.Base}
	}
	obligs = append(obligs, oblig{subj: tail, pat: restPat})
	nw.todo = append(obligs, w.todo[1:]...)
	return []*armState{nw}
}

// nextSubj picks the atom a continuation's DPanic should report when
// it runs out of arms — the subject the group will discriminate on
// next — so a nested failure names its own subject's type rather than
// the enclosing switch's.
func nextSubj(states []*armState, fallback core.Atom) core.Atom {
	for _, w := range states {
		if len(w.todo) > 0 {
			return w.todo[0].subj
		}
	}
	return fallback
}

// caseBody compiles a case's continuations into the case Body — an
// immediate leaf inlines to spare a redundant Match node.
func (l *lowerer) caseBody(states []*armState, subj core.Atom, resT types.Type) *core.Body {
	cs := &seq{}
	tree := l.compile(states, nextSubj(states, subj), cs, resT)
	if dl, ok := tree.(*core.DLeaf); ok {
		cs.lets = append(cs.lets, dl.Body.Lets...)
		return &core.Body{Lets: cs.lets, Tail: dl.Body.Tail}
	}
	return &core.Body{
		Lets: cs.lets,
		Tail: &core.Match{
			Sp: tree.Span(), Typ: resT,
			Tree: tree,
		},
	}
}

// armLeaf finishes the first surviving arm — flush earned binds to
// fresh temporaries, honour the guard, build the leaf body under
// the pattern-variable substitution.
func (l *lowerer) armLeaf(w *armState, rest []*armState, subj core.Atom, s *seq, resT types.Type) core.DTree {
	subst := map[*resolve.Symbol]*resolve.Symbol{}
	for _, b := range w.binds {
		nb := l.tmpBind(b.atom.Span(), b.atom.Type())
		s.emit(nb, b.atom)
		subst[b.sym] = nb.Sym
	}
	if w.src != nil && w.src.Guard != nil {
		l.pushSubst(subst)
		cond := l.atom(s, w.src.Guard)
		l.popSubst()
		return &core.DGuard{
			Sp: w.src.Guard.Span(), Typ: resT,
			Cond: cond,
			Then: &core.DLeaf{
				Sp: w.src.Body.Span(), Typ: resT,
				Body: l.armBody(w, subst),
			},
			Else: l.continueWith(rest, subj, resT),
		}
	}
	return &core.DLeaf{
		Sp: w.sp, Typ: resT,
		Body: l.armBody(w, subst),
	}
}

// armBody builds a leaf's body — the arm's expression under the
// substitution, or the synthetic leaf callback.
func (l *lowerer) armBody(w *armState, subst map[*resolve.Symbol]*resolve.Symbol) *core.Body {
	bs := &seq{}
	l.pushSubst(subst)
	var tail core.Expr
	if w.leaf != nil {
		tail = w.leaf(l, w, bs, subst)
	} else {
		tail = l.expr(bs, w.src.Body)
	}
	l.popSubst()
	return &core.Body{Lets: bs.lets, Tail: tail}
}

// dpanic builds the contract-violation terminal. The subject must be
// an in-scope atom — callers keep the group's discriminated atom for
// exactly this.
func (l *lowerer) dpanic(subj core.Atom, resT types.Type) core.DTree {
	return &core.DPanic{
		Sp: subj.Span(), Typ: resT,
		Subj:     subj,
		TypeName: typeName(subj.Type()),
	}
}

// --- destructure (let / parameter / for patterns) ---

// letPat lowers `let pat = value` inside a block — the value has
// already been lowered.
func (l *lowerer) letPat(s *seq, pat check.Pat, val core.Expr) {
	if p, ok := pat.(*check.VarPat); ok {
		s.emit(l.namedBind(p.Sp, p.Sym, val.Type()), val)
		return
	}
	if p, ok := pat.(*check.WildPat); ok {
		s.emit(l.discardBind(p.Sp, val.Type()), val)
		return
	}
	l.destructure(s, pat, l.asAtom(s, pat.Span(), val))
}

// destructure emits the bindings a non-trivial pattern earns.
// A deep-irrefutable pattern (tuple / record / `as` / `[..rest]`
// compositions) is pure projection onto the original symbols. A
// pattern that needs a test — the single-variant constructor shapes
// the pattern checker may admit, and anything refutable that slips
// through — compiles to a match producing the bound variables as a
// tuple (or the single variable, or Unit), so malformed values panic
// instead of silently projecting garbage.
func (l *lowerer) destructure(s *seq, pat check.Pat, subj core.Atom) {
	if deepPure(pat) {
		for _, b := range l.pureBinds(pat, subj) {
			s.emit(l.namedBind(b.sp, b.sym, b.atom.Type()), b.atom)
		}
		return
	}
	vars := boundVars(pat)
	resT := boundVarsType(vars)
	states := []*armState{{
		sp:   pat.Span(),
		todo: []oblig{{subj: subj, pat: pat}},
		leaf: boundVarsLeaf(vars),
	}}
	cs := &seq{}
	tree := l.compile(states, subj, cs, resT)
	var matchExpr core.Expr = &core.Match{
		Sp: pat.Span(), Typ: resT, Tree: tree,
	}
	if dl, ok := tree.(*core.DLeaf); ok {
		cs.lets = append(cs.lets, dl.Body.Lets...)
		matchExpr = dl.Body.Tail
	}
	s.lets = append(s.lets, cs.lets...)
	switch len(vars) {
	case 0:
		s.emit(l.discardBind(pat.Span(), resT), matchExpr)
	case 1:
		s.emit(l.namedBind(pat.Span(), vars[0].sym, resT), matchExpr)
	default:
		bound := l.tmpBind(pat.Span(), resT)
		s.emit(bound, matchExpr)
		for i, v := range vars {
			s.emit(l.namedBind(pat.Span(), v.sym, v.typ), &core.FieldGet{
				Sp: pat.Span(), Typ: v.typ,
				Recv: varOf(pat.Span(), bound), Index: i,
			})
		}
	}
}

// boundVarsLeaf is the destructure leaf factory: it yields the path's
// earned bindings — the fresh temporaries the original symbols were
// lowered to — packed in the outer pattern's *declared* binder order
// (vars), not the matched path's order. An or-pattern alternative
// like `B y x` earns the same names in a different order than `A x y`;
// the caller's projections index the tuple by declared position, so
// the pack must follow vars.
func boundVarsLeaf(vars []boundVar) func(l *lowerer, w *armState, bs *seq, subst map[*resolve.Symbol]*resolve.Symbol) core.Expr {
	return func(l *lowerer, w *armState, bs *seq, subst map[*resolve.Symbol]*resolve.Symbol) core.Expr {
		bySym := make(map[*resolve.Symbol]pendingBind, len(w.binds))
		for _, b := range w.binds {
			bySym[b.sym] = b
		}
		atoms := make([]core.Atom, 0, len(vars))
		var ts []types.Type
		for _, v := range vars {
			b, ok := bySym[v.sym]
			if !ok {
				// Every matched path earns exactly the declared
				// names; a missing one is a contract gap.
				l.err(w.sp, fmt.Sprintf("pattern path did not earn the declared binder %s", v.sym.Name))
				return l.litUnit(w.sp)
			}
			atoms = append(atoms, &core.Var{
				Sp: b.atom.Span(), Typ: b.atom.Type(),
				Sym: subst[b.sym],
			})
			ts = append(ts, b.atom.Type())
		}
		switch len(atoms) {
		case 0:
			return l.litUnit(w.sp)
		case 1:
			return atoms[0]
		default:
			return &core.TupleNew{
				Sp: w.sp, Typ: types.Tup(ts...),
				Elems: atoms,
			}
		}
	}
}

// deepPure reports whether a pattern can never fail — wildcard,
// variable, `()` literal, and tuple/record/as/`[..rest]` shapes
// composed of deep-pure parts.
func deepPure(p check.Pat) bool {
	switch p := p.(type) {
	case *check.WildPat, *check.VarPat:
		return true
	case *check.LitPat:
		return p.Kind == syntax.LitUnit
	case *check.TuplePat:
		for _, e := range p.Elems {
			if !deepPure(e) {
				return false
			}
		}
		return true
	case *check.RecordPat:
		for _, f := range p.Fields {
			if !deepPure(f.Pat) {
				return false
			}
		}
		return true
	case *check.AsPat:
		return deepPure(p.Pat)
	case *check.ListPat:
		if len(p.Elems) != 0 || p.Rest == nil {
			return false
		}
		return deepPure(p.Rest)
	case *check.CtorPat, *check.OrPat:
		return false
	default:
		panic("unreachable")
	}
}

// pureBind is one projection-earned binding of a deepPure pattern.
type pureBind struct {
	sp   diag.Span
	sym  *resolve.Symbol
	atom core.Atom
}

// pureBinds flattens a deepPure pattern into (symbol, projection)
// pairs — each atom is a FieldGet chain off the bound subject.
func (l *lowerer) pureBinds(pat check.Pat, subj core.Atom) []pureBind {
	var out []pureBind
	var walk func(p check.Pat, a core.Atom)
	walk = func(p check.Pat, a core.Atom) {
		switch p := p.(type) {
		case *check.VarPat:
			out = append(out, pureBind{sp: p.Sp, sym: p.Sym, atom: a})
		case *check.AsPat:
			out = append(out, pureBind{sp: p.Sp, sym: p.Sym, atom: a})
			walk(p.Pat, a)
		case *check.TuplePat:
			for i, e := range p.Elems {
				walk(e, &core.FieldGet{
					Sp: p.Sp, Typ: e.Type(),
					Recv: a, Index: i,
				})
			}
		case *check.RecordPat:
			for _, fp := range p.Fields {
				idx := fp.Index
				if idx < 0 {
					idx = l.fieldIndex(a.Type(), fp.Sym)
					if idx < 0 {
						l.err(fp.Sp, fmt.Sprintf("record field %s has no known index", fp.Name))
						idx = 0 // keep the tree well-formed; the diagnostic stands
					}
				}
				walk(fp.Pat, &core.FieldGet{
					Sp: fp.Sp, Typ: fp.Pat.Type(),
					Recv: a, Field: fp.Sym, Index: idx,
				})
			}
		case *check.ListPat:
			if p.Rest != nil {
				walk(p.Rest, a)
			}
		case *check.WildPat, *check.LitPat:
		case *check.CtorPat, *check.OrPat:
			panic("unreachable")
		default:
			panic("unreachable")
		}
	}
	walk(pat, subj)
	return out
}

// boundVar is one variable a pattern binds, with its declared type
// and the pattern's source span.
type boundVar struct {
	sym *resolve.Symbol
	typ types.Type
	sp  diag.Span
}

// boundVars collects a pattern's bound variables in traversal order.
// Or-patterns read their first alternative — the checker guarantees
// the same names and types on every branch.
func boundVars(p check.Pat) []boundVar {
	var out []boundVar
	var walk func(p check.Pat)
	walk = func(p check.Pat) {
		switch p := p.(type) {
		case *check.VarPat:
			out = append(out, boundVar{sym: p.Sym, typ: p.Type(), sp: p.Span()})
		case *check.AsPat:
			out = append(out, boundVar{sym: p.Sym, typ: p.Type(), sp: p.Span()})
			walk(p.Pat)
		case *check.TuplePat:
			for _, e := range p.Elems {
				walk(e)
			}
		case *check.RecordPat:
			for _, f := range p.Fields {
				walk(f.Pat)
			}
		case *check.CtorPat:
			for _, a := range p.Args {
				walk(a.Pat)
			}
			for _, f := range p.Fields {
				walk(f.Pat)
			}
		case *check.ListPat:
			for _, e := range p.Elems {
				walk(e)
			}
			if p.Rest != nil {
				walk(p.Rest)
			}
		case *check.OrPat:
			if len(p.Alts) > 0 {
				walk(p.Alts[0])
			}
		case *check.WildPat, *check.LitPat:
		default:
			panic("unreachable")
		}
	}
	walk(p)
	return out
}

// boundVarsType is the destructure match's result type — Unit when
// nothing binds, the variable's type for one, a tuple otherwise.
func boundVarsType(vars []boundVar) types.Type {
	switch len(vars) {
	case 0:
		return types.Unit
	case 1:
		return vars[0].typ
	default:
		var ts []types.Type
		for _, v := range vars {
			ts = append(ts, v.typ)
		}
		return types.Tup(ts...)
	}
}

// listElem is the element type of the *builtin* `List a`; nil for
// other shapes — including a file-local `type List`, which is an
// ADT the checker refuses a list pattern against.
func (l *lowerer) listElem(t types.Type) types.Type {
	if c, ok := types.Chase(t).(*types.Con); ok && c.Qual == "" &&
		c.Name == "List" && !l.declaredT(c) && len(c.Args) == 1 {
		return c.Args[0]
	}
	return nil
}
