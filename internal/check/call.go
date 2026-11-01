package check

import (
	"errors"
	"fmt"

	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// CallKind classifies a typed application — the calling-convention
// decision made at type checking so that lowering never re-derives it
// from names or syntax (docs/design.md sections 2.1 and 4.4).
type CallKind int

const (
	// CallUnknown is an application of an unknown function value —
	// emitted as one-argument-at-a-time curried application `f(x)(y)`.
	CallUnknown CallKind = iota + 1
	// CallDirect is a saturated (or over-applied) call to a known Gom
	// callee — emitted as a direct Go call. When Applied > Arity the
	// extra arguments apply to the returned function value.
	CallDirect
	// CallPartial is an under-applied call to a known Gom callee —
	// emitted as a closure taking the Unapplied remaining parameters.
	CallPartial
	// CallGo is a call to a Go function or method — emitted with Go's
	// calling convention (no currying, tuple-style argument list).
	CallGo
)

// String returns the kind name for debugging.
func (k CallKind) String() string {
	switch k {
	case CallUnknown:
		return "unknown"
	case CallDirect:
		return "direct"
	case CallPartial:
		return "partial"
	case CallGo:
		return "go"
	default:
		return "CallKind(?)"
	}
}

// CallInfo is the structured call information carried by every typed
// Apply node (docs/design.md section 2: "callee symbol, type
// arguments, number of applied and unapplied arguments, Gom/Go
// calling convention, direct-callable"). It is the contract for the
// call: later phases read these fields and nothing else.
type CallInfo struct {
	Kind CallKind

	// Callee is the symbol of the function being called. It is nil
	// exactly when Kind is CallUnknown — the callee is then whatever
	// the Fn expression evaluates to.
	Callee *resolve.Symbol

	// TypeArgs are the type arguments the callee's type parameters
	// were instantiated at (monomorphization happens in Core IR; this
	// records the instantiation). Nil while the compiler is
	// monomorphic.
	TypeArgs []types.Type

	// Arity is the callee's declared parameter count: for a Gom
	// function the length of its parameter list, for a Go function the
	// fixed parameter count (see Variadic). It is the parameter count
	// of the callee's own signature — not of the function type as a
	// curried chain.
	Arity int

	// Applied is the number of arguments supplied by this application.
	Applied int

	// Unapplied is the number of callee parameters left unfilled —
	// positive exactly when Kind is CallPartial.
	Unapplied int

	// ArgToParam maps each entry of Args to the callee parameter it
	// fills — Args[i] supplies parameter ArgToParam[i]. Labeled
	// arguments can fill parameters out of order; the evaluation order
	// stays the Args order and reordering into declaration order is
	// lowering's job (docs/design.md section 2.1). Nil means the
	// identity mapping. Entries index the callee's declared
	// parameters, so an over-applied call (Applied > Arity) has no
	// parameter for the extras and must leave ArgToParam nil.
	ArgToParam []int

	// HoleIndex is the index into Args of the `_` placeholder of a
	// cute-style partial Go call (docs/spec.md section 8.2), or -1.
	// At most one hole is allowed.
	HoleIndex int

	// Variadic marks a Go callee with a variadic final parameter; the
	// number of Args may then exceed Arity-1.
	Variadic bool
}

// Validate checks the structural invariants of the call info —
// primarily a guard for hand-built typed ASTs in tests of later
// phases.
func (c CallInfo) Validate() error {
	switch c.Kind {
	case CallUnknown, CallDirect, CallPartial, CallGo:
	default:
		return fmt.Errorf("check: invalid call kind %d", int(c.Kind))
	}
	if c.Kind == CallUnknown && c.Callee != nil {
		return errors.New("check: CallUnknown with non-nil callee")
	}
	if c.Kind != CallUnknown && c.Callee == nil {
		return errors.New("check: known-convention call with nil callee")
	}
	if c.Arity < 0 {
		return fmt.Errorf("check: negative arity %d", c.Arity)
	}
	if c.Applied < 0 {
		return fmt.Errorf("check: negative applied count %d", c.Applied)
	}
	if c.Unapplied < 0 {
		return fmt.Errorf("check: negative unapplied count %d", c.Unapplied)
	}
	switch c.Kind {
	case CallPartial:
		if c.Unapplied <= 0 {
			return fmt.Errorf("check: CallPartial with unapplied count %d", c.Unapplied)
		}
		if c.Applied+c.Unapplied != c.Arity {
			return fmt.Errorf("check: CallPartial applied %d + unapplied %d != arity %d",
				c.Applied, c.Unapplied, c.Arity)
		}
	case CallDirect:
		if c.Unapplied != 0 {
			return fmt.Errorf("check: CallDirect with unapplied count %d", c.Unapplied)
		}
		if c.Applied < c.Arity {
			return fmt.Errorf("check: CallDirect applied %d < arity %d", c.Applied, c.Arity)
		}
	case CallGo, CallUnknown:
		if c.Unapplied != 0 {
			return fmt.Errorf("check: %s with unapplied count %d", c.Kind, c.Unapplied)
		}
	}
	if c.HoleIndex != -1 && (c.HoleIndex < 0 || c.HoleIndex >= c.Applied) {
		return fmt.Errorf("check: hole index %d out of range [0, %d)", c.HoleIndex, c.Applied)
	}
	if c.Kind != CallGo && c.HoleIndex != -1 {
		return fmt.Errorf("check: %s call with a `_` placeholder", c.Kind)
	}
	if len(c.ArgToParam) != 0 {
		if len(c.ArgToParam) != c.Applied {
			return fmt.Errorf("check: ArgToParam has %d entries for %d applied arguments",
				len(c.ArgToParam), c.Applied)
		}
		if c.Applied > c.Arity {
			return fmt.Errorf("check: over-applied call (applied %d > arity %d) with non-nil ArgToParam",
				c.Applied, c.Arity)
		}
		seen := make(map[int]bool, len(c.ArgToParam))
		for i, p := range c.ArgToParam {
			if p < 0 || p >= c.Arity {
				return fmt.Errorf("check: ArgToParam[%d] = %d out of range [0, %d)", i, p, c.Arity)
			}
			if seen[p] {
				return fmt.Errorf("check: ArgToParam fills parameter %d twice", p)
			}
			seen[p] = true
		}
	}
	return nil
}

// Validate checks the application node's structural invariants: the
// call info itself is valid and it agrees with the argument list —
// len(Args) equals Call.Applied and `_` holes appear exactly at
// Call.HoleIndex. It is a guard for hand-built typed ASTs in tests of
// later phases, like CallInfo.Validate.
func (a *Apply) Validate() error {
	if err := a.Call.Validate(); err != nil {
		return err
	}
	if len(a.Args) != a.Call.Applied {
		return fmt.Errorf("check: %d args for %d applied arguments",
			len(a.Args), a.Call.Applied)
	}
	if a.Call.HoleIndex >= 0 {
		if _, ok := a.Args[a.Call.HoleIndex].Value.(*Hole); !ok {
			return fmt.Errorf("check: HoleIndex %d does not point at a Hole", a.Call.HoleIndex)
		}
	}
	for i, arg := range a.Args {
		if _, ok := arg.Value.(*Hole); ok && i != a.Call.HoleIndex {
			return fmt.Errorf("check: arg %d is a Hole but HoleIndex is %d", i, a.Call.HoleIndex)
		}
	}
	return nil
}
