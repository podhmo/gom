package stdlib

// The builtin `Chan`, `Task`, `Ptr`, `Ref`, `Array` and `View`
// modules — plus the `Go`-namespace type-member tables `Go.Map`
// and `Go.Slice` — ship as compiler-owned member tables backed
// by rt functions rather than a .gomi stdlib package: the
// operation surfaces spec sections 3.1, 8.3, 8.5 and 10 fix are
// not expressible in Gom signatures.
// `Chan.send`/`recv`/`close` accept both `Chan a` and the matching
// directional type, which only the implicit channel-direction
// conversion at Go-call boundaries admits (spec section 10.2), and
// `Go.Map`'s `k` carries a `comparable` constraint no .gomi can
// spell. Making each member an IsGo symbol forwarding to rt also
// makes the call a genuine Go boundary, so the boundary
// conversions apply exactly where the spec grants them.

import (
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// rtPath is the Go import path of the compiler's runtime package —
// the backing implementation of the members below.
const rtPath = "github.com/podhmo/gom/rt"

// stdlibVarBase starts the ID range of the rigid type variables in
// the hand-built signatures below, kept clear of the ranges check
// hands out (declared variables count from 0), check's builtinSigs
// (1<<20) and goimport's generic-callee variables (1<<21) — Var
// identity is its ID, so colliding ranges would equate distinct
// variables when a file declares `a` and a builtin uses `a` too.
const stdlibVarBase = 1 << 22

// elemTVar is the shared rigid element/result variable of the
// signatures below — every signature that mentions `a` shares the
// variable, which is safe because each use site instantiates it with
// a fresh meta. keyTVar/valTVar are the same for the two-parameter
// Go.Map members (`k`/`v`), accTVar for the accumulator `b` of
// `Go.Slice.fold`, errTVar for the error `e` of `Result.to_option`.
var (
	elemTVar = &types.Var{ID: stdlibVarBase, Name: "a"}
	keyTVar  = &types.Var{ID: stdlibVarBase + 1, Name: "k"}
	valTVar  = &types.Var{ID: stdlibVarBase + 2, Name: "v"}
	accTVar  = &types.Var{ID: stdlibVarBase + 3, Name: "b"}
	errTVar  = &types.Var{ID: stdlibVarBase + 4, Name: "e"}
)

// builtinMember is one entry of a hand-built builtin-module member
// table: the Gom surface name, the Go function it forwards to, and
// its two signature views — the Gom-facing check.Signature and the
// Go-facing lower.GoSig the boundary facts come from. pkg overrides
// the backing package when the member forwards outside rt —
// `Array.to_list`/`from_list` live in stdlib/list because rt cannot
// import the generated package it is imported by.
type builtinMember struct {
	name  string          // member name in the Gom surface (`send`, `recv`, ...)
	goSym string          // Go function name in the backing package (`ChanSend`, ...)
	pkg   string          // backing Go import path — empty means rt
	sig   check.Signature // Gom view — what the checker unifies against
	gosig lower.GoSig     // Go view — arity and result shape for lowering
}

// symbol mints the Go function symbol the member resolves to.
func (m *builtinMember) symbol() *resolve.Symbol {
	pkg := m.pkg
	if pkg == "" {
		pkg = rtPath
	}
	return &resolve.Symbol{
		Name:    m.goSym,
		Kind:    resolve.KindFunc,
		IsGo:    true,
		Package: pkg,
	}
}

// genericSig builds a one-type-parameter signature for the members
// below — they are generic like the rt functions they forward to
// (`func ChanSend[T any](...)`), so CallInfo.TypeArgs carries the
// solved `T` for the emitted `rt.ChanSend[T](...)` instantiation.
func genericSig(t types.Type) check.Signature {
	return check.Signature{
		Type:          t,
		TypeParams:    []*types.Var{elemTVar},
		GoConstraints: []check.GoConstraint{{Kind: check.GoConstraintAny}},
	}
}

// genericMapSig builds the two-type-parameter signature of the
// Go.Map members — `[k comparable, v any]`: the key constraint is
// what `map[K]V` needs to compile, and the post-inference E0508
// check judges the caller's key choice on its emitted Go
// representation (a `List Int` key never satisfies it).
func genericMapSig(t types.Type) check.Signature {
	return check.Signature{
		Type:          t,
		TypeParams:    []*types.Var{keyTVar, valTVar},
		GoConstraints: []check.GoConstraint{{Kind: check.GoConstraintComparable}, {Kind: check.GoConstraintAny}},
	}
}

// genericPairSig builds the two-type-parameter `[a, b any]`
// signature of `Go.Slice.fold` — the one member whose type mentions
// a second variable (the accumulator). Both constraints are `any`.
func genericPairSig(t types.Type) check.Signature {
	return check.Signature{
		Type:          t,
		TypeParams:    []*types.Var{elemTVar, accTVar},
		GoConstraints: []check.GoConstraint{{Kind: check.GoConstraintAny}, {Kind: check.GoConstraintAny}},
	}
}

// genericErrSig builds the two-type-parameter `[a, e any]`
// signature of `Result.to_option` — `a` is the payload and `e` the
// error the conversion drops. Both constraints are `any`.
func genericErrSig(t types.Type) check.Signature {
	return check.Signature{
		Type:          t,
		TypeParams:    []*types.Var{elemTVar, errTVar},
		GoConstraints: []check.GoConstraint{{Kind: check.GoConstraintAny}, {Kind: check.GoConstraintAny}},
	}
}

// chanMembers is the `Chan` module's operation surface (spec section
// 10, item 3). The directional parameters are what make a plain
// `Chan a` argument convert implicitly at the call — `Chan.send`
// takes `SendChan a`, which `Chan a` reaches by Go's channel
// direction assignability.
var chanMembers = []builtinMember{
	{name: "make", goSym: "MakeChan",
		sig:   genericSig(types.Fn(types.Chan(elemTVar), types.P(types.Int))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne}},
	{name: "send", goSym: "ChanSend",
		sig:   genericSig(types.Fn(types.Unit, types.P(types.SendChan(elemTVar)), types.P(elemTVar))),
		gosig: lower.GoSig{Arity: 2, Results: core.GoResultsNone}},
	{name: "recv", goSym: "ChanRecv",
		sig:   genericSig(types.Fn(types.Option(elemTVar), types.P(types.RecvChan(elemTVar)))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOK}},
	{name: "close", goSym: "ChanClose",
		sig:   genericSig(types.Fn(types.Unit, types.P(types.SendChan(elemTVar)))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsNone}},
	{name: "recvOnly", goSym: "ChanRecvOnly",
		sig:   genericSig(types.Fn(types.RecvChan(elemTVar), types.P(types.Chan(elemTVar)))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne}},
	{name: "sendOnly", goSym: "ChanSendOnly",
		sig:   genericSig(types.Fn(types.SendChan(elemTVar), types.P(types.Chan(elemTVar)))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne}},
}

// taskMembers is the `Task` module's operation surface (spec section
// 10, item 9): spawn takes `Unit -> a` — which the Go side spells
// `func(struct{}) T` — and await blocks for the result, re-throwing
// a panic the task died with.
var taskMembers = []builtinMember{
	{name: "spawn", goSym: "SpawnTask",
		sig:   genericSig(types.Fn(types.Task(elemTVar), types.P(types.Fn(elemTVar, types.P(types.Unit))))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne}},
	{name: "await", goSym: "AwaitTask",
		sig:   genericSig(types.Fn(elemTVar, types.P(types.Task(elemTVar)))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne}},
}

// refMembers is the `Ref` module's operation surface — the mutable
// cell of spec section 3.1 (decisions/2026-11-19-ref-members).
// `Ref a` emits as `*T` like `Ptr a`, but a `Ref` is always
// initialized: `new` allocates the cell holding its argument, and
// get/set are plain dereferences — there is no nil form, so the
// surface has no is_nil/nil member.
var refMembers = []builtinMember{
	{name: "new", goSym: "RefNew",
		sig:   genericSig(types.Fn(types.Ref(elemTVar), types.P(elemTVar))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne}},
	{name: "get", goSym: "RefGet",
		sig:   genericSig(types.Fn(elemTVar, types.P(types.Ref(elemTVar)))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne}},
	{name: "set", goSym: "RefSet",
		sig:   genericSig(types.Fn(types.Unit, types.P(types.Ref(elemTVar)), types.P(elemTVar))),
		gosig: lower.GoSig{Arity: 2, Results: core.GoResultsNone}},
}

// scopeMembers is the `Scope` module's operation surface — scoped
// cleanup (decisions/2026-11-21-scoped-cleanup). run/defer/on_err
// live in rt; the Result-aware run_result/defer_err live in
// stdlib/result because rt cannot import the Result representation.
var scopeMembers = []builtinMember{
	{name: "run", goSym: "ScopeRun",
		sig:   genericSig(types.Fn(elemTVar, types.P(types.Fn(elemTVar, types.P(types.Scope))))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne}},
	{name: "run_result", goSym: "ScopeRunResult", pkg: "github.com/podhmo/gom/stdlib/result",
		sig: genericSig(types.Fn(types.Result(elemTVar, types.GoError),
			types.P(types.Fn(types.Result(elemTVar, types.GoError), types.P(types.Scope))))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne}},
	{name: "defer", goSym: "ScopeDefer",
		sig:   check.Signature{Type: types.Fn(types.Unit, types.P(types.Scope), types.P(types.Fn(types.Unit, types.P(types.Unit))))},
		gosig: lower.GoSig{Arity: 2, Results: core.GoResultsNone}},
	{name: "defer_err", goSym: "ScopeDeferErr", pkg: "github.com/podhmo/gom/stdlib/result",
		sig: check.Signature{Type: types.Fn(types.Unit, types.P(types.Scope),
			types.P(types.Fn(types.Result(types.Unit, types.GoError), types.P(types.Unit))))},
		gosig: lower.GoSig{Arity: 2, Results: core.GoResultsNone}},
	{name: "on_err", goSym: "ScopeOnErr",
		sig:   check.Signature{Type: types.Fn(types.Unit, types.P(types.Scope), types.P(types.Fn(types.Unit, types.P(types.Unit))))},
		gosig: lower.GoSig{Arity: 2, Results: core.GoResultsNone}},
}

// ptrMembers is the `Ptr` module's operation surface — the explicit
// dereference, nil test and construction `Ptr T` needs for real Go
// APIs (decisions/2026-11-05-ptr-ops-surface). get/set panic on nil
// like Go's own dereference; is_nil and to_option are the safe
// forms; new makes the zero-value target out-pointer APIs
// (`errors.As`, decode destinations) want, and nil feeds the
// optional `*T` parameters.
var ptrMembers = []builtinMember{
	{name: "get", goSym: "PtrGet",
		sig:   genericSig(types.Fn(elemTVar, types.P(types.Ptr(elemTVar)))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne}},
	{name: "set", goSym: "PtrSet",
		sig:   genericSig(types.Fn(types.Unit, types.P(types.Ptr(elemTVar)), types.P(elemTVar))),
		gosig: lower.GoSig{Arity: 2, Results: core.GoResultsNone}},
	{name: "is_nil", goSym: "PtrIsNil",
		sig:   genericSig(types.Fn(types.Bool, types.P(types.Ptr(elemTVar)))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne}},
	{name: "new", goSym: "PtrNew",
		sig:   genericSig(types.Fn(types.Ptr(elemTVar))),
		gosig: lower.GoSig{Arity: 0, Results: core.GoResultsOne}},
	{name: "make", goSym: "PtrMake",
		sig:   genericSig(types.Fn(types.Ptr(elemTVar), types.P(elemTVar))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne}},
	{name: "nil", goSym: "PtrNil",
		sig:   genericSig(types.Fn(types.Ptr(elemTVar))),
		gosig: lower.GoSig{Arity: 0, Results: core.GoResultsOne}},
	{name: "to_option", goSym: "PtrToOption",
		sig:   genericSig(types.Fn(types.Option(elemTVar), types.P(types.Ptr(elemTVar)))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOK}},
}

// goMapMembers is `Go.Map`'s operation surface — a member table on
// the *type* member rather than a builtin module, injected under
// the qualified Modules key "Go.Map" that resolve's ctorTable
// consults (decisions/2026-11-05-go-map-ops). `k` is `comparable`
// because Go maps require it; `to_list` is the v1 iteration
// surface — the []rt.Tuple2 result converts to a `List (k, v)`
// through the ordinary Go-slice boundary conversion.
var goMapMembers = []builtinMember{
	{name: "make", goSym: "MapMake",
		sig: genericMapSig(types.Fn(types.GoMap(keyTVar, valTVar))),
		gosig: lower.GoSig{Arity: 0, Results: core.GoResultsOne,
			TypeParams: []*types.Var{keyTVar, valTVar}}},
	{name: "get", goSym: "MapGet",
		sig: genericMapSig(types.Fn(types.Option(valTVar), types.P(types.GoMap(keyTVar, valTVar)), types.P(keyTVar))),
		gosig: lower.GoSig{Arity: 2, Results: core.GoResultsOK,
			TypeParams: []*types.Var{keyTVar, valTVar}}},
	{name: "set", goSym: "MapSet",
		sig: genericMapSig(types.Fn(types.Unit, types.P(types.GoMap(keyTVar, valTVar)), types.P(keyTVar), types.P(valTVar))),
		gosig: lower.GoSig{Arity: 3, Results: core.GoResultsNone,
			TypeParams: []*types.Var{keyTVar, valTVar}}},
	{name: "delete", goSym: "MapDelete",
		sig: genericMapSig(types.Fn(types.Unit, types.P(types.GoMap(keyTVar, valTVar)), types.P(keyTVar))),
		gosig: lower.GoSig{Arity: 2, Results: core.GoResultsNone,
			TypeParams: []*types.Var{keyTVar, valTVar}}},
	{name: "length", goSym: "MapLen",
		sig: genericMapSig(types.Fn(types.Int, types.P(types.GoMap(keyTVar, valTVar)))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne,
			TypeParams: []*types.Var{keyTVar, valTVar}}},
	{name: "to_list", goSym: "MapToList",
		sig: genericMapSig(types.Fn(types.List(types.Tup(keyTVar, valTVar)), types.P(types.GoMap(keyTVar, valTVar)))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne,
			ResTypes:   []types.Type{types.GoSlice(types.Tup(keyTVar, valTVar))},
			TypeParams: []*types.Var{keyTVar, valTVar}}},
}

// goSliceMembers is `Go.Slice`'s read-only operation surface — a
// member table on the *type* member like `Go.Map`'s, injected under
// the qualified Modules key "Go.Slice"
// (decisions/2026-11-04-goconv-list-slice-copies — the copy-avoiding
// read path `Go.conv`'s copy semantics call for; the surface itself
// is decisions/2026-11-05-go-slice-ops). Every member is a read:
// `get`/`to_list` mirror `Go.Map`'s vocabulary, `slice` is the
// `s[lo:hi]` shared sub-view (panics like Go on bad bounds), and
// `fold` iterates without copying. There are no write operations —
// a `Go.Slice` stays Go's own mutable view to write through, Gom
// only reads it.
var goSliceMembers = []builtinMember{
	{name: "length", goSym: "SliceLen",
		sig: genericSig(types.Fn(types.Int, types.P(types.GoSlice(elemTVar)))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne,
			TypeParams: []*types.Var{elemTVar}}},
	{name: "get", goSym: "SliceGet",
		sig: genericSig(types.Fn(types.Option(elemTVar), types.P(types.GoSlice(elemTVar)), types.P(types.Int))),
		gosig: lower.GoSig{Arity: 2, Results: core.GoResultsOK,
			TypeParams: []*types.Var{elemTVar}}},
	{name: "slice", goSym: "SliceSub",
		sig: genericSig(types.Fn(types.GoSlice(elemTVar), types.P(types.GoSlice(elemTVar)), types.P(types.Int), types.P(types.Int))),
		gosig: lower.GoSig{Arity: 3, Results: core.GoResultsOne,
			TypeParams: []*types.Var{elemTVar}}},
	{name: "fold", goSym: "SliceFold",
		sig: genericPairSig(types.Fn(accTVar,
			types.P(types.GoSlice(elemTVar)),
			types.P(types.Fn(accTVar, types.P(accTVar), types.P(elemTVar))),
			types.P(accTVar))),
		gosig: lower.GoSig{Arity: 3, Results: core.GoResultsOne,
			TypeParams: []*types.Var{elemTVar, accTVar}}},
	{name: "to_list", goSym: "SliceToList",
		sig: genericSig(types.Fn(types.List(elemTVar), types.P(types.GoSlice(elemTVar)))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne,
			ResTypes:   []types.Type{types.GoSlice(elemTVar)},
			TypeParams: []*types.Var{elemTVar}}},
}

// arrayMembers is the `Array` module's operation surface — the
// mutable build buffer the cons-built combinator problem calls for
// (decisions/2026-11-18-cons-buffer-array-extern-impl). `Array a`
// emits as `[]A`, so make/set are verbatim Go indexing — out of
// range panics like Go. `to_list` is the zero-copy freeze
// (hand-off: writing the array after freezing violates the List
// immutability contract, spec §8.7); `from_list` copies so array
// writes can never corrupt the source list. The two list-boundary
// members live in stdlib/list — rt is imported BY that package and
// cannot import it back.
var arrayMembers = []builtinMember{
	{name: "make", goSym: "ArrayMake",
		sig: genericSig(types.Fn(types.Array(elemTVar), types.P(types.Int), types.P(elemTVar))),
		gosig: lower.GoSig{Arity: 2, Results: core.GoResultsOne,
			TypeParams: []*types.Var{elemTVar}}},
	{name: "length", goSym: "ArrayLen",
		sig: genericSig(types.Fn(types.Int, types.P(types.Array(elemTVar)))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne,
			TypeParams: []*types.Var{elemTVar}}},
	{name: "get", goSym: "ArrayGet",
		sig: genericSig(types.Fn(elemTVar, types.P(types.Array(elemTVar)), types.P(types.Int))),
		gosig: lower.GoSig{Arity: 2, Results: core.GoResultsOne,
			TypeParams: []*types.Var{elemTVar}}},
	{name: "set", goSym: "ArraySet",
		sig: genericSig(types.Fn(types.Unit, types.P(types.Array(elemTVar)), types.P(types.Int), types.P(elemTVar))),
		gosig: lower.GoSig{Arity: 3, Results: core.GoResultsNone,
			TypeParams: []*types.Var{elemTVar}}},
	{name: "from_list", goSym: "ArrayFromList", pkg: "github.com/podhmo/gom/stdlib/list",
		sig: genericSig(types.Fn(types.Array(elemTVar), types.P(types.List(elemTVar)))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne,
			TypeParams: []*types.Var{elemTVar}}},
	{name: "to_list", goSym: "ArrayFreeze", pkg: "github.com/podhmo/gom/stdlib/list",
		sig: genericSig(types.Fn(types.List(elemTVar), types.P(types.Array(elemTVar)))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne,
			TypeParams: []*types.Var{elemTVar}}},
}

// viewMembers is the `View` module's read-only operation surface —
// the List-derived view type (decision 2026-11-18-list-view). The
// backing functions live in stdlib/list beside `type View[A]`; the
// module itself is compiler-registered like `Chan`/`Array` — there
// is no `view` Gom package to hang a .gomi on.
var viewMembers = []builtinMember{
	{name: "length", goSym: "ViewLen", pkg: "github.com/podhmo/gom/stdlib/list",
		sig: genericSig(types.Fn(types.Int, types.P(types.View(elemTVar)))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne,
			TypeParams: []*types.Var{elemTVar}}},
	{name: "get", goSym: "ViewGet", pkg: "github.com/podhmo/gom/stdlib/list",
		sig: genericSig(types.Fn(types.Option(elemTVar), types.P(types.View(elemTVar)), types.P(types.Int))),
		gosig: lower.GoSig{Arity: 2, Results: core.GoResultsOK,
			TypeParams: []*types.Var{elemTVar}}},
	{name: "slice", goSym: "ViewSub", pkg: "github.com/podhmo/gom/stdlib/list",
		sig: genericSig(types.Fn(types.View(elemTVar), types.P(types.View(elemTVar)), types.P(types.Int), types.P(types.Int))),
		gosig: lower.GoSig{Arity: 3, Results: core.GoResultsOne,
			TypeParams: []*types.Var{elemTVar}}},
	{name: "fold", goSym: "ViewFold", pkg: "github.com/podhmo/gom/stdlib/list",
		sig: genericPairSig(types.Fn(accTVar,
			types.P(types.View(elemTVar)),
			types.P(types.Fn(accTVar, types.P(accTVar), types.P(elemTVar))),
			types.P(accTVar))),
		gosig: lower.GoSig{Arity: 3, Results: core.GoResultsOne,
			TypeParams: []*types.Var{elemTVar, accTVar}}},
	{name: "to_list", goSym: "ViewToList", pkg: "github.com/podhmo/gom/stdlib/list",
		sig: genericSig(types.Fn(types.List(elemTVar), types.P(types.View(elemTVar)))),
		gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne,
			TypeParams: []*types.Var{elemTVar}}},
}

// builtinModules populates e with the member tables, signatures,
// GoSig oracles and declared arities of the rt-backed builtin
// modules — `Chan`, `Task`, `Ptr`, `Ref`, `Array`, `View` — and
// of the `Go`-namespace type-member tables (`Go.Map`, `Go.Slice`).
// Unlike the .gomi-derived tables of `packages`, every fact here
// is written by hand because no Gom interface file can spell
// these signatures.
func (e *env) builtinModules() {
	for module, members := range map[string][]builtinMember{
		"Chan":     chanMembers,
		"Task":     taskMembers,
		"Ptr":      ptrMembers,
		"Ref":      refMembers,
		"Array":    arrayMembers,
		"View":     viewMembers,
		"Scope":    scopeMembers,
		"Go.Map":   goMapMembers,
		"Go.Slice": goSliceMembers,
	} {
		table := map[string]*resolve.Symbol{}
		for _, m := range members {
			e.addMember(module, table, m)
		}
		e.modules[module] = table
	}
}

// addMember registers one hand-built member in a module's table and
// in the env's cross-phase oracle tables.
func (e *env) addMember(module string, table map[string]*resolve.Symbol, m builtinMember) {
	sym := m.symbol()
	sym.Surface = module + "." + m.name
	table[m.name] = sym
	e.sigs[sym] = m.sig
	e.gosigs[sym] = m.gosig
	// DeclArity: every member's declared parameter count is its Go
	// arity — bare references adapt by it.
	e.arities[sym] = m.gosig.Arity
}

// conversionModules extends the .gomi-derived `Result` and `Option`
// member tables with the layering conversions — `Result.to_option`
// and `Option.to_list`. Neither can be a .gomi fn: the conversion
// reaches *upward* in the result < option < list DAG, and a
// lower package's .gom implementation importing its upper layer
// would close a Go import cycle. Instead each member is surfaced
// on the lower module but forwards to a hand-written function in
// the upper package — the `Array.to_list` → stdlib/list precedent.
// Called after the packages loop so the .gomi-derived tables it
// mutates exist.
func (e *env) conversionModules() {
	for module, members := range map[string][]builtinMember{
		"Result": {
			{name: "to_option", goSym: "ResultToOption", pkg: "github.com/podhmo/gom/stdlib/option",
				sig: genericErrSig(types.Fn(types.Option(elemTVar), types.P(types.Result(elemTVar, errTVar)))),
				gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne,
					TypeParams: []*types.Var{elemTVar, errTVar}}},
		},
		"List": {
			// `view`/`slice` produce a `View a` — the List-derived
			// read-only view (decision 2026-11-18-list-view). They
			// are compiler-registered rather than .gomi-declared:
			// an @extern impl named `view` would generate `View`,
			// colliding with `type View[A]` in the list package.
			{name: "view", goSym: "ListView", pkg: "github.com/podhmo/gom/stdlib/list",
				sig: genericSig(types.Fn(types.View(elemTVar), types.P(types.List(elemTVar)))),
				gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne,
					TypeParams: []*types.Var{elemTVar}}},
			{name: "slice", goSym: "ListSub", pkg: "github.com/podhmo/gom/stdlib/list",
				sig: genericSig(types.Fn(types.View(elemTVar), types.P(types.List(elemTVar)), types.P(types.Int), types.P(types.Int))),
				gosig: lower.GoSig{Arity: 3, Results: core.GoResultsOne,
					TypeParams: []*types.Var{elemTVar}}},
		},
		"Option": {
			{name: "to_list", goSym: "OptionToList", pkg: "github.com/podhmo/gom/stdlib/list",
				sig: genericSig(types.Fn(types.List(elemTVar), types.P(types.Option(elemTVar)))),
				gosig: lower.GoSig{Arity: 1, Results: core.GoResultsOne,
					TypeParams: []*types.Var{elemTVar}}},
		},
	} {
		for _, m := range members {
			e.addMember(module, e.modules[module], m)
		}
	}
}
