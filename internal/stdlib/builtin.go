package stdlib

// The builtin `Chan`, `Task`, and `Ptr` modules — plus the
// `Go`-namespace type-member table `Go.Map` — ship as
// compiler-owned member tables backed by rt functions rather than
// a .gomi stdlib package: the operation surfaces spec sections
// 8.3 and 10 fix are not expressible in Gom signatures.
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
// Go.Map members (`k`/`v`).
var (
	elemTVar = &types.Var{ID: stdlibVarBase, Name: "a"}
	keyTVar  = &types.Var{ID: stdlibVarBase + 1, Name: "k"}
	valTVar  = &types.Var{ID: stdlibVarBase + 2, Name: "v"}
)

// builtinMember is one entry of a hand-built builtin-module member
// table: the Gom surface name, the rt function it forwards to, and
// its two signature views — the Gom-facing check.Signature and the
// Go-facing lower.GoSig the boundary facts come from.
type builtinMember struct {
	name  string          // member name in the Gom surface (`send`, `recv`, ...)
	goSym string          // Go function name inside rt (`ChanSend`, ...)
	sig   check.Signature // Gom view — what the checker unifies against
	gosig lower.GoSig     // Go view — arity and result shape for lowering
}

// symbol mints the Go function symbol the member resolves to.
func (m *builtinMember) symbol() *resolve.Symbol {
	return &resolve.Symbol{
		Name:    m.goSym,
		Kind:    resolve.KindFunc,
		IsGo:    true,
		Package: rtPath,
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
		GoConstraints: []check.GoConstraint{check.GoConstraintAny},
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
		GoConstraints: []check.GoConstraint{check.GoConstraintComparable, check.GoConstraintAny},
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

// builtinModules populates e with the member tables, signatures,
// GoSig oracles and declared arities of the rt-backed builtin
// modules — `Chan`, `Task`, `Ptr` — and of the `Go`-namespace
// type-member tables (`Go.Map`). Unlike the .gomi-derived tables
// of `packages`, every fact here is written by hand because no Gom
// interface file can spell these signatures.
func (e *env) builtinModules() {
	for module, members := range map[string][]builtinMember{
		"Chan":   chanMembers,
		"Task":   taskMembers,
		"Ptr":    ptrMembers,
		"Go.Map": goMapMembers,
	} {
		table := map[string]*resolve.Symbol{}
		for _, m := range members {
			sym := m.symbol()
			sym.Surface = module + "." + m.name
			table[m.name] = sym
			e.sigs[sym] = m.sig
			e.gosigs[sym] = m.gosig
			// DeclArity: every member's declared parameter count is
			// its Go arity — bare references adapt by it.
			e.arities[sym] = m.gosig.Arity
		}
		e.modules[module] = table
	}
}
