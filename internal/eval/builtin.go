package eval

import (
	"errors"
	"fmt"

	"github.com/podhmo/gom/internal/types"
)

// builtinByName returns the function value for a KindBuiltin symbol,
// or nil when the name is not a builtin the evaluator implements.
// typ supplies the arity the checker resolved (a builtin may be used
// at several arities); when nil the canonical arity is used.
func builtinByName(name string, typ types.Type) *VFunc {
	var arity int
	var impl func(args []Value) (Value, error)
	switch name {
	case "todo":
		// `todo ()` panics — a placeholder fix marker (spec 12 item 5).
		arity = 1
		impl = func(args []Value) (Value, error) {
			return nil, errors.New("todo reached")
		}
	case "assert":
		arity = 1
		impl = func(args []Value) (Value, error) {
			b, ok := args[0].(*VBool)
			if !ok {
				return nil, fmt.Errorf("assert applied to %s, want Bool", args[0].String())
			}
			if !b.B {
				return nil, errors.New("assertion failed")
			}
			return &VUnit{}, nil
		}
	case "assert_eq":
		arity = 2
		impl = func(args []Value) (Value, error) {
			if !Equal(args[0], args[1]) {
				return nil, fmt.Errorf("assert_eq failed: %s != %s",
					args[0].String(), args[1].String())
			}
			return &VUnit{}, nil
		}
	case "not":
		// `not x` is the Bool negation codegen emits as `!x`.
		arity = 1
		impl = func(args []Value) (Value, error) {
			b, ok := args[0].(*VBool)
			if !ok {
				return nil, fmt.Errorf("not applied to %s, want Bool", args[0].String())
			}
			return &VBool{B: !b.B}, nil
		}
	default:
		return nil
	}
	if sig, ok := typ.(*types.Func); ok && len(sig.Params) > 0 {
		arity = len(sig.Params)
	}
	f := funcOfType(name, fnBuiltin, &types.Func{
		Params: make([]types.FuncParam, arity), Ret: types.Unit})
	for i := range f.sig.Params {
		f.sig.Params[i] = types.P(types.Unit)
	}
	f.builtin = impl
	return f
}
