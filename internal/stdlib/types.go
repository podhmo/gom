package stdlib

// Type members of embedded .gomi interfaces — `type Shape = ...`
// inside a builtin-scope module's interface — register the same
// tables gomimport's extract registers for an imported package:
// the module member entries (the type symbol plus its variant
// constructors), the type's own signature, the per-type ctor table
// `pkg.Type.Ctor` resolves against, the payload field tables, the
// foreign type declarations check registers, and the
// by-constructor index the lowering oracle reads.
//
// The helpers below mirror internal/gomimport's requalify /
// instantiate / fieldTable — those are unexported, and the type-node
// sets they walk (types.Type's five cases, check.TypeBody's four)
// are fixed, so keeping the mirror in sync costs a line per case
// when a node kind is added.

import (
	"fmt"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// conKey indexes loaded type declarations by nominal identity — the
// same shape gomimport (and check's type tables) use.
type conKey struct {
	qual, name string
}

// loadType registers one interface type declaration as module
// members and cross-phase tables: table is the module's member
// table, impPath the Go import path generated code reaches the type
// through, and own the type names this interface declares (for
// requalification of self-references).
func (e *env) loadType(table map[string]*resolve.Symbol, impPath string, td *check.TypeDecl, own map[string]bool) {
	td.Sym.Package = impPath
	requalifyDecl(td, own, impPath)
	e.tdecls = append(e.tdecls, td)
	e.byCon[conKey{qual: impPath, name: td.Sym.Name}] = td
	table[td.Sym.Name] = td.Sym
	// The type's own signature is its constructor — what `pkg.T` in
	// annotation position resolves to (arity included for generic
	// declarations). An alias answers the aliased type itself.
	if al, ok := td.Body.(*check.AliasType); ok {
		e.sigs[td.Sym] = check.Signature{Type: requalify(al.Type, own, impPath)}
	} else {
		e.sigs[td.Sym] = check.Signature{
			Type: &types.Con{Qual: impPath, Name: td.Sym.Name,
				Args: conArgs(td.TypeParams)},
		}
	}
	if s, ok := td.Body.(*check.SumType); ok {
		parent := &types.Con{Qual: impPath, Name: td.Sym.Name,
			Args: conArgs(td.TypeParams)}
		// The per-type ctor table `pkg.Type.Ctor` resolves
		// against; the variants also join the member table
		// directly (`pkg.Circle`), like an imported package's.
		ctab := make(map[string]*resolve.Symbol, len(s.Variants))
		for i := range s.Variants {
			v := &s.Variants[i]
			if v.Sym == nil {
				continue
			}
			v.Sym.Package = impPath
			table[v.Sym.Name] = v.Sym
			ctab[v.Sym.Name] = v.Sym
			params := make([]types.FuncParam, len(v.Fields))
			for j, f := range v.Fields {
				params[j] = types.FuncParam{Label: f.Label, Type: f.Type}
			}
			var sig types.Type = parent
			if len(params) > 0 {
				sig = &types.Func{Params: params, Ret: parent}
			}
			e.sigs[v.Sym] = check.Signature{Type: sig}
			e.ctorFields[v.Sym] = fieldTable(v.Fields)
		}
		e.typeCtors[td.Sym] = ctab
	}
	if rc, ok := td.Body.(*check.RecordType); ok {
		e.ctorFields[td.Sym] = fieldTable(rc.Fields)
	}
}

// memberSigs is the signature source Apply chains ahead of the
// caller's: the module members' .gomi signatures plus the loaded
// type declarations — the check.TypeDeclSource protocol, so foreign
// type registration sees the module's type members like an imported
// package's.
type memberSigs struct {
	sigs  check.MapSource
	decls []*check.TypeDecl
}

// SignatureOf returns a member symbol's .gomi signature — the
// check.SignatureSource entry.
func (s memberSigs) SignatureOf(sym *resolve.Symbol) (check.Signature, bool) {
	return s.sigs.SignatureOf(sym)
}

// TypeDecls returns the modules' loaded type declarations — the
// check.TypeDeclSource entry, forwarded through check.Chain like a
// loader's.
func (s memberSigs) TypeDecls() []*check.TypeDecl {
	return s.decls
}

// ctorLoader is the resolve.Loader seam for builtin-module type
// members: resolve probes Options.Loader for CtorSource /
// CtorFieldSource to fill Config.Ctors / Config.CtorFields — the
// `pkg.Type.Ctor` spelling and labeled payload fields — and a
// .gomi-based loader only knows imported types, so stdlib answers
// for its own before delegating. Every other protocol on the
// wrapped loader (the other optional capability interfaces are
// probed on Options.Loader only here, in build.resolveConfig) is
// unaffected.
type ctorLoader struct {
	inner  resolve.Loader
	ctors  map[*resolve.Symbol]map[string]*resolve.Symbol
	fields map[*resolve.Symbol]map[string]*resolve.Symbol
}

// Load delegates to the wrapped loader; nil inner means the compile
// runs without an import oracle, like build.Options' zero value.
func (l *ctorLoader) Load(path string, goPkg bool) (*resolve.Package, error) {
	if l.inner == nil {
		return nil, fmt.Errorf("package %q not found", path)
	}
	return l.inner.Load(path, goPkg)
}

// CtorsOf returns the constructor table of a stdlib module's type —
// resolve.CtorSource — before the wrapped loader's own answer.
func (l *ctorLoader) CtorsOf(typeSym *resolve.Symbol) map[string]*resolve.Symbol {
	if ct, ok := l.ctors[typeSym]; ok {
		return ct
	}
	if cs, ok := l.inner.(resolve.CtorSource); ok {
		return cs.CtorsOf(typeSym)
	}
	return nil
}

// CtorFieldsOf returns the named payload slots of a stdlib
// constructor (or record type) — resolve.CtorFieldSource — before
// the wrapped loader's own answer.
func (l *ctorLoader) CtorFieldsOf(ctor *resolve.Symbol) map[string]*resolve.Symbol {
	if ft, ok := l.fields[ctor]; ok {
		return ft
	}
	if fs, ok := l.inner.(resolve.CtorFieldSource); ok {
		return fs.CtorFieldsOf(ctor)
	}
	return nil
}

// typeOracle is the lower.TypeOracle entry Apply chains ahead of an
// existing one: declaration-level facts about the modules' type
// members (sum variants for wildcard match arms, record field order
// for record literals) that lower cannot recover from a check.File.
type typeOracle struct {
	byCon map[conKey]*check.TypeDecl
	inner lower.TypeOracle
}

// Variants returns the sum type's constructors in declaration order,
// instantiated at the queried constructor's arguments.
func (o *typeOracle) Variants(c *types.Con) []lower.Variant {
	if td := o.byCon[conKey{qual: c.Qual, name: c.Name}]; td != nil {
		if s, ok := td.Body.(*check.SumType); ok {
			sub := instantiate(td.TypeParams, c.Args)
			out := make([]lower.Variant, len(s.Variants))
			for i, v := range s.Variants {
				fields := make([]check.TField, len(v.Fields))
				for j, f := range v.Fields {
					fields[j] = f
					fields[j].Type = sub.Apply(f.Type)
				}
				out[i] = lower.Variant{Sym: v.Sym, Fields: fields}
			}
			return out
		}
	}
	if o.inner != nil {
		return o.inner.Variants(c)
	}
	return nil
}

// Fields returns the record type's fields in declaration order,
// instantiated at the queried constructor's arguments.
func (o *typeOracle) Fields(c *types.Con) []check.TField {
	if td := o.byCon[conKey{qual: c.Qual, name: c.Name}]; td != nil {
		if rc, ok := td.Body.(*check.RecordType); ok {
			sub := instantiate(td.TypeParams, c.Args)
			out := make([]check.TField, len(rc.Fields))
			for i, f := range rc.Fields {
				out[i] = f
				out[i].Type = sub.Apply(f.Type)
			}
			return out
		}
	}
	if o.inner != nil {
		return o.inner.Fields(c)
	}
	return nil
}

// requalifyDecl rewrites a loaded type declaration's body so its
// references to the interface's own type names carry the import
// path — the same requalification gomimport's extract runs.
func requalifyDecl(td *check.TypeDecl, own map[string]bool, path string) {
	switch body := td.Body.(type) {
	case *check.SumType:
		for i := range body.Variants {
			for j := range body.Variants[i].Fields {
				body.Variants[i].Fields[j].Type =
					requalify(body.Variants[i].Fields[j].Type, own, path)
			}
		}
	case *check.RecordType:
		for j := range body.Fields {
			body.Fields[j].Type = requalify(body.Fields[j].Type, own, path)
		}
	case *check.AliasType:
		body.Type = requalify(body.Type, own, path)
	case *check.AbstractType, nil:
	default:
		panic("unreachable")
	}
}

// requalify rewrites t so constructor references to the interface's
// own type names carry the import path. Every other constructor —
// builtin-scope and other-package names — keeps its qualifier;
// Var/Meta are shared leaves.
func requalify(t types.Type, own map[string]bool, path string) types.Type {
	switch t := types.Chase(t).(type) {
	case *types.Var:
		return t
	case *types.Meta:
		return t
	case *types.Con:
		c := &types.Con{Qual: t.Qual, Name: t.Name}
		if c.Qual == "" && own[c.Name] {
			c.Qual = path
		}
		c.Args = requalifyAll(t.Args, own, path)
		return c
	case *types.Func:
		params := make([]types.FuncParam, len(t.Params))
		for i, fp := range t.Params {
			params[i] = types.FuncParam{Label: fp.Label,
				Type: requalify(fp.Type, own, path)}
		}
		return &types.Func{Params: params,
			Ret: requalify(t.Ret, own, path)}
	case *types.Tuple:
		return &types.Tuple{Elems: requalifyAll(t.Elems, own, path)}
	case *types.Go:
		return &types.Go{Ref: t.Ref, Args: requalifyAll(t.Args, own, path)}
	default:
		panic("unreachable")
	}
}

// requalifyAll maps requalify over a type slice.
func requalifyAll(ts []types.Type, own map[string]bool, path string) []types.Type {
	if len(ts) == 0 {
		return ts
	}
	out := make([]types.Type, len(ts))
	for i, t := range ts {
		out[i] = requalify(t, own, path)
	}
	return out
}

// conArgs turns declared type parameters into the constructor's own
// argument list — `type Shape a` applied to itself.
func conArgs(params []*types.Var) []types.Type {
	args := make([]types.Type, len(params))
	for i, v := range params {
		args[i] = v
	}
	return args
}

// instantiate maps a declaration's type parameters to a queried
// constructor's arguments.
func instantiate(params []*types.Var, args []types.Type) types.Subst {
	sub := types.Subst{}
	for i, v := range params {
		if i < len(args) {
			sub[v.ID] = args[i]
		}
	}
	return sub
}

// fieldTable builds the param-name -> field-symbol map
// resolve.Config.CtorFields expects — labeled and named slots only;
// positional payload is addressed by index.
func fieldTable(fields []check.TField) map[string]*resolve.Symbol {
	var table map[string]*resolve.Symbol
	for _, f := range fields {
		name := f.Label
		if name == "" {
			name = f.Name
		}
		if name == "" || f.Sym == nil {
			continue
		}
		if table == nil {
			table = map[string]*resolve.Symbol{}
		}
		table[name] = f.Sym
	}
	return table
}
