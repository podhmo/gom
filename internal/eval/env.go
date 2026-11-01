package eval

import (
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// Env is a lexical environment: an immutable-parent chain of frames
// mapping *resolve.Symbol to Value. The root environment also carries
// the shared evaluator context (constructor and record tables) so a
// standalone Expr call gets lazy constructor registration for free.
type Env struct {
	parent *Env
	vars   map[*resolve.Symbol]Value
	ctx    *ctx
}

// ctx is the per-evaluation shared state: constructor metadata
// gathered from TypeDecls plus lazily derived constructors, and the
// record layouts used to order record fields.
type ctx struct {
	ctors   map[*resolve.Symbol]*ctorInfo
	records map[*resolve.Symbol]*recordInfo
}

// ctorInfo is the runtime metadata of one ADT constructor.
type ctorInfo struct {
	sym    *resolve.Symbol
	name   string
	typ    *types.Con // the nominal type the constructor builds
	fields []fieldInfo
}

// fieldInfo is one payload slot of a constructor (declaration order).
type fieldInfo struct {
	name  string // "" for positional payload
	label string // non-empty for ~labeled constructor parameters
	sym   *resolve.Symbol
	index int
}

// recordInfo is the declared field layout of a record type
// (declaration order).
type recordInfo struct {
	sym    *resolve.Symbol
	typ    *types.Con
	fields []fieldInfo
}

// NewEnv returns a fresh root environment with an empty shared
// context — for evaluating standalone expressions in tests.
func NewEnv() *Env {
	return &Env{
		vars: map[*resolve.Symbol]Value{},
		ctx: &ctx{
			ctors:   map[*resolve.Symbol]*ctorInfo{},
			records: map[*resolve.Symbol]*recordInfo{},
		},
	}
}

// Child returns a child environment sharing the parent's context.
func (e *Env) Child() *Env {
	return &Env{parent: e, vars: map[*resolve.Symbol]Value{}, ctx: e.ctx}
}

// Bind introduces (or shadows in this frame) a symbol binding.
func (e *Env) Bind(sym *resolve.Symbol, v Value) {
	e.vars[sym] = v
}

// Lookup resolves a symbol through the chain.
func (e *Env) Lookup(sym *resolve.Symbol) (Value, bool) {
	for cur := e; cur != nil; cur = cur.parent {
		if v, ok := cur.vars[sym]; ok {
			return v, true
		}
	}
	return nil, false
}
