package resolve

// scope is one level of the lexical scope chain: a linked list of
// name tables from the innermost block out to the builtin scope.
// Lookup walks parents; definition inserts into the current level
// only, so an inner name silently shadows an outer one
// (docs/decisions/2026-11-02-name-resolution.md).
type scope struct {
	parent *scope
	terms  map[string]*Symbol // value-level names: vars, functions, ctors, modules, builtins
	types  map[string]*Symbol // type-level names: declared and builtin type names
}

// pushScope returns a fresh child scope. A nil parent marks the
// outermost (builtin) level.
func pushScope(parent *scope) *scope {
	return &scope{parent: parent}
}

// lookupTerm resolves a term-level name (variable, function,
// constructor, or module alias) through the scope chain.
func (s *scope) lookupTerm(name string) *Symbol {
	for ; s != nil; s = s.parent {
		if sym, ok := s.terms[name]; ok {
			return sym
		}
	}
	return nil
}

// lookupType resolves a type-level name through the scope chain.
// Type variables are not here — they live in tvScope.
func (s *scope) lookupType(name string) *Symbol {
	for ; s != nil; s = s.parent {
		if sym, ok := s.types[name]; ok {
			return sym
		}
	}
	return nil
}

// defineTerm inserts a term-level symbol into this scope level,
// returning the symbol it replaced, or nil. Callers report the
// replacement as a duplicate definition when they require the slot to
// have been empty.
func (s *scope) defineTerm(name string, sym *Symbol) (prev *Symbol) {
	if s.terms == nil {
		s.terms = make(map[string]*Symbol)
	}
	prev, s.terms[name] = s.terms[name], sym
	return prev
}

// lookupThisTerm reports the symbol bound to name in this scope level
// only — the test for same-scope duplicates.
func (s *scope) lookupThisTerm(name string) *Symbol {
	return s.terms[name]
}

// defineType inserts a type-level symbol into this scope level,
// returning the symbol it replaced, or nil.
func (s *scope) defineType(name string, sym *Symbol) (prev *Symbol) {
	if s.types == nil {
		s.types = make(map[string]*Symbol)
	}
	prev, s.types[name] = s.types[name], sym
	return prev
}

// tvScope is the scope of type variables inside a type expression.
// Unlike value scopes it is not tied to AST blocks: one tvScope
// covers a whole signature (every parameter type plus the return
// type), so `fn f (x: Option a) -> a` sees a single `a`.
type tvScope struct {
	parent   *tvScope
	vars     map[string]*Symbol
	implicit bool // unknown type variables are implicitly bound here
}

// lookupVar resolves a type variable through the tvScope chain.
func (t *tvScope) lookupVar(name string) *Symbol {
	for ; t != nil; t = t.parent {
		if sym, ok := t.vars[name]; ok {
			return sym
		}
	}
	return nil
}

// defineVar inserts a type variable into this scope level.
func (t *tvScope) defineVar(name string, sym *Symbol) {
	if t.vars == nil {
		t.vars = make(map[string]*Symbol)
	}
	t.vars[name] = sym
}

// implicitScope reports the innermost scope that may bind a type
// variable implicitly, or nil when implicit binding is not allowed
// (e.g. inside a type declaration body, where every variable must be
// a declared parameter).
func (t *tvScope) implicitScope() *tvScope {
	for ; t != nil; t = t.parent {
		if t.implicit {
			return t
		}
	}
	return nil
}

// builtinScope builds the outermost scope: the builtin types of spec
// section 3.1, the builtin constructors of Option and Result, the
// compiler-provided functions (assert_eq, todo, ...), the `Go`
// interop namespace, and the standard-library modules named after
// builtin types (List.map, Chan.send, Task.spawn, ...).
//
// Builtin module member tables come from Config.Modules, consulted
// at member-lookup time (builtinModuleTable) rather than here.
// internal/stdlib populates List/Option/Result from the shipped
// .gomi files and Chan/Task/Ptr/Ref/Array/View from the rt-backed
// tables of its builtin.go; a lookup on a module with no table is
// E0201 — either a wiring gap or a missing stdlib member set
// (undefinedMember distinguishes them).
func builtinScope() *scope {
	s := &scope{terms: map[string]*Symbol{}, types: map[string]*Symbol{}}
	for _, name := range []string{
		"Unit", "Bool", "Int", "Int32", "Int64", "Float", "Char",
		"Byte", "String", "GoError",
		"List", "Option", "Result", "Ref", "Array", "Ptr", "View",
		"Chan", "RecvChan", "SendChan", "Task", "Scope",
	} {
		s.types[name] = &Symbol{Name: name, Kind: KindType}
	}
	// Constructors of the builtin Option and Result types — spec 3.1
	// makes them builtin types, so their ctors sit in the builtin
	// scope as ordinary KindCtor symbols.
	for _, name := range []string{"Some", "None", "Ok", "Err"} {
		s.terms[name] = &Symbol{Name: name, Kind: KindCtor}
	}
	for _, name := range []string{"assert", "assert_eq", "snapshot", "todo", "not"} {
		s.terms[name] = &Symbol{Name: name, Kind: KindBuiltin}
	}
	// Standard-library modules named after builtin types (spec
	// sections 1.4, 3.3, 8.3, 10): the module names are in scope,
	// their member tables are injected via Config.Modules.
	for _, name := range []string{"List", "Option", "Result", "Ref", "Array", "Chan", "Task", "Ptr", "View", "Scope"} {
		s.terms[name] = &Symbol{Name: name, Kind: KindModule}
	}
	// The compiler-owned `Go` namespace (types.Con reserved qualifier
	// "Go"): Go.conv, Go.Slice, Go.Map (spec sections 8.3–8.4).
	s.terms["Go"] = &Symbol{Name: "Go", Kind: KindModule, Package: "Go"}
	return s
}

// builtinGoMembers is the member table of the `Go` namespace. It is a
// fresh map per call because the resolver may overlay it.
func builtinGoMembers() map[string]*Symbol {
	return map[string]*Symbol{
		"conv": {Name: "conv", Kind: KindBuiltin, Package: "Go"},
		// `Go.nil ()` — the universal nil literal for nil-able
		// types (decision 2026-11-23-nil-spelling). Its typing is
		// check-side like conv's (`Unit -> a` with a nil-ability
		// constraint on the solved `a`), so the member is a bare
		// builtin symbol.
		"nil": {Name: "nil", Kind: KindBuiltin, Package: "Go"},
		// `Go.try_as x : Option T` — the comma-ok assertion that is the
		// only extraction surface out of `Go.Any` (decision
		// 2026-11-24-go-any-extraction). Typed check-side like conv.
		"try_as": {Name: "try_as", Kind: KindBuiltin, Package: "Go"},
		"Any":    {Name: "Any", Kind: KindType, Package: "Go"},
		"Slice":  {Name: "Slice", Kind: KindType, Package: "Go"},
		"Map":    {Name: "Map", Kind: KindType, Package: "Go"},
		// The func-typed interop surface: `Go.FuncN` names a Go
		// `func` type and `Go.funcN` is the explicit conversion
		// producing a value of it
		// (docs/decisions/2026-11-17-func-interop.md). `Go.fnN` is
		// the reverse conversion — a `Go.FuncN` value read as a
		// Gom function (docs/decisions/2026-11-18-fn-interop.md).
		"Func0": {Name: "Func0", Kind: KindType, Package: "Go"},
		"Func1": {Name: "Func1", Kind: KindType, Package: "Go"},
		"Func2": {Name: "Func2", Kind: KindType, Package: "Go"},
		"Func3": {Name: "Func3", Kind: KindType, Package: "Go"},
		"func0": {Name: "func0", Kind: KindBuiltin, Package: "Go"},
		"func1": {Name: "func1", Kind: KindBuiltin, Package: "Go"},
		"func2": {Name: "func2", Kind: KindBuiltin, Package: "Go"},
		"func3": {Name: "func3", Kind: KindBuiltin, Package: "Go"},
		"fn0":   {Name: "fn0", Kind: KindBuiltin, Package: "Go"},
		"fn1":   {Name: "fn1", Kind: KindBuiltin, Package: "Go"},
		"fn2":   {Name: "fn2", Kind: KindBuiltin, Package: "Go"},
		"fn3":   {Name: "fn3", Kind: KindBuiltin, Package: "Go"},
	}
}

// stdlibModules names the builtin-scope modules the toolchain ships
// a standard-library implementation for — internal/stdlib's
// `packages` table, mirrored here by hand (resolve cannot import the
// wiring package without a cycle). A member lookup on one of these
// with no injected table is a wiring gap, not an unimplemented
// feature, and the E0201 note says so.
var stdlibModules = map[string]bool{
	"List": true, "Option": true, "Result": true,
	"Chan": true, "Task": true, "Ptr": true, "Ref": true,
	"Array": true, "View": true, "Scope": true,
}

// goTypeModules names the `Go`-namespace types that ship an
// operation member table — `Go.Map.make`/`get`/... and the
// `Go.Slice` read-only surface (`length`/`get`/`slice`/`fold`/
// `to_list`). The tables are compiler-owned and rt-backed like a
// builtin module's, but they hang off a *type* member (`Go.Map`
// resolves through builtinGoMembers), so they inject under the
// qualified Config.Modules key "Go.<Name>" and the member lookup is
// the ctor-table path (ctorTable), not builtinModuleTable. A type
// not in this map genuinely has no member surface — noSuchCtor's
// note distinguishes that from an unwired compile.
var goTypeModules = map[string]bool{"Map": true, "Slice": true}

// builtinModuleTable returns the member table of a builtin-scope
// module: the `Go` namespace's hardcoded table, or a Config.Modules
// entry for the standard-library modules. ok is false when the module
// has no table at all (only the stdlib modules can reach that).
func builtinModuleTable(mod *Symbol, cfg *Config) (table map[string]*Symbol, ok bool) {
	if mod.Package == "Go" {
		return builtinGoMembers(), true
	}
	if cfg != nil {
		if t, found := cfg.Modules[mod.Name]; found {
			return t, true
		}
	}
	return nil, false
}
