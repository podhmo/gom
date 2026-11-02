package gomgen

// Builtin producers for the lexer-level nonterminals. Grammar section
// 2's lexical rules are token *sets*, not walkable productions (they
// bottom out in "? ... ?" specials and character classes), so the
// walker never expands them — ParseGrammar's reachability check
// verifies no "? ... ?" slips through some other route.

import (
	"fmt"
	"strings"
)

// builtinProducer maps a lexer-level nonterminal to its producer. A
// ref to any of these is emitted directly, never walked — which is
// also what keeps the character-class rules (digit, letter, ...) and
// the "? ... ?" specials out of reach. A function (not a map of
// method values) so package-init dependency analysis stays acyclic.
func builtinProducer(name string) (func(*Gen), bool) {
	switch name {
	case "ident":
		return (*Gen).genIdent, true
	case "UIdent":
		return (*Gen).genUIdent, true
	case "INT":
		return (*Gen).genInt, true
	case "FLOAT":
		return (*Gen).genFloat, true
	case "CHAR":
		return (*Gen).genChar, true
	case "STRING":
		return (*Gen).genString, true
	case "NL":
		return (*Gen).genNL, true
	}
	return nil, false
}

// isBuiltinRule reports whether name has a builtin producer.
func isBuiltinRule(name string) bool {
	_, ok := builtinProducer(name)
	return ok
}

// keywords is grammar section 2.2's reserved set — ident producers
// must never emit one.
var keywords = map[string]bool{
	"as": true, "default": true, "else": true, "extern": true,
	"false": true, "fn": true, "for": true, "go": true, "if": true,
	"ignore": true, "import": true, "in": true, "let": true, "match": true,
	"package": true, "select": true, "send": true, "test": true,
	"true": true, "try": true, "type": true, "with": true,
}

// identPool keeps generated programs readable-ish while staying
// decently varied. Typed mode's scope lookup takes precedence over
// the pool most of the time.
var identPool = []string{
	"x", "y", "z", "n", "m", "k", "v", "acc", "xs", "ys", "rest",
	"item", "total", "count", "idx", "tmp", "res", "cur", "next",
	"value", "data", "seed", "limit", "step", "base",
}

// fnPool is reserved for callable-looking names — mechanical mode
// draws from it for fn names and application heads.
var fnPool = []string{
	"f", "g", "h", "apply", "map", "add", "mul", "sum", "size",
	"head", "last", "mk", "run", "step2", "pick", "fold", "show2",
}

var uidentPool = []string{
	"Shape", "Point", "Tree", "Node", "Leaf", "Item", "Entry",
	"Some2", "None2", "Ok2", "Err2", "Pair", "Box", "Cell", "Kind",
}

// builtinTypes names resolve without any declaration: the scalar
// builtins, plus the generic ones whose bare use fails at arity
// rather than at lookup (a different, also welcome near-miss).
var builtinTypes = []string{
	"Int", "Float", "Bool", "String", "Char",
	"List", "Option", "Result", "Task", "Chan", "GoError",
}

// genIdent emits a lowercase identifier. Typed mode prefers a name
// already in scope (that is what makes applications resolvable);
// mechanical mode draws from the pool or coins a fresh name, biased
// toward names a pattern or fn bound earlier — a use site that can
// resolve mostly does, and what remains is the deliberate
// unbound-name near-miss.
func (g *Gen) genIdent() {
	// The deterministic subset is meant to be built and run, so its
	// package is always "main" — `go run` needs it.
	if g.opts.Deterministic && g.curRule == "package-decl" {
		g.emit("main")
		return
	}
	if g.defKind != 0 {
		// A definition site: record the name, keep fn names and
		// import aliases unique so redefinitions are not the
		// near-miss that dominates.
		var name string
		switch g.defKind {
		case defFn:
			name = g.freshBound()
			g.boundSet[name] = true
			g.bound = append(g.bound, name)
		case defAlias:
			name = g.freshAlias()
			g.importSet[name] = true
			g.importMod = append(g.importMod, name)
			g.importAliased = true
		case defQual:
			// A qualifier names a module, not a variable —
			// a bound import mostly resolves.
			if len(g.importMod) > 0 && g.rng.Float64() < 0.7 {
				name = g.importMod[g.rng.IntN(len(g.importMod))]
			} else {
				name = g.freshIdent()
			}
		default:
			name = g.freshIdent()
		}
		g.defKind = 0
		g.emit(name)
		return
	}
	// A binding occurrence outside the pattern machinery (e.g. a
	// select recv-arm's name): emit and record.
	if g.bindNext {
		g.bindNext = false
		name := g.freshIdent()
		g.emit(name)
		g.bound = append(g.bound, name)
		return
	}
	var name string
	if g.opts.Typed {
		if s := g.scopeIdent(); s != "" && g.rng.Float64() < 0.8 {
			name = s
		}
	} else if len(g.bound) > 0 && g.inLabel == 0 && g.inType == 0 && g.rng.Float64() < 0.7 {
		// Prefer recently bound names — scope blindness makes an
		// old binding likely out of scope, a recent one likely
		// still inside its own block.
		base := 0
		if n := len(g.bound); n > 8 {
			base = n - 8
		}
		name = g.bound[base+g.rng.IntN(len(g.bound)-base)]
	}
	if name == "" {
		name = g.freshIdent()
	}
	g.emit(name)
	if g.inPat > 0 {
		g.bound = append(g.bound, name)
	}
}

// freshBound picks an ident not already used as a top-level fn name
// — a redefinition would spend the near-miss budget on E0204s.
func (g *Gen) freshBound() string {
	for tries := 0; tries < 20; tries++ {
		if n := g.freshIdent(); !g.boundSet[n] {
			return n
		}
	}
	return fmt.Sprintf("v%d", g.rng.IntN(100000))
}

// freshAlias picks an ident not already used as an import alias.
func (g *Gen) freshAlias() string {
	for tries := 0; tries < 20; tries++ {
		if n := g.freshIdent(); !g.importSet[n] {
			return n
		}
	}
	return fmt.Sprintf("pkg%d", g.rng.IntN(1000))
}

// freshIdent picks any legal ident — pool draw or a coined name —
// guaranteed non-keyword.
func (g *Gen) freshIdent() string {
	for {
		var s string
		switch g.rng.IntN(8) {
		case 0, 1:
			s = fmt.Sprintf("v%d", g.rng.IntN(1000))
		case 2:
			s = identPool[g.rng.IntN(len(identPool))] + fmt.Sprintf("%d", g.rng.IntN(10))
		case 3:
			s = fnPool[g.rng.IntN(len(fnPool))]
		default:
			s = identPool[g.rng.IntN(len(identPool))]
		}
		if !keywords[s] {
			return s
		}
	}
}

// genUIdent emits an uppercase identifier — a constructor, type or
// module name. Typed mode prefers a declared constructor/type;
// mechanical mode prefers a name a type-decl or variant already
// declared, keeping "undefined type / constructor" near-misses as
// the exception rather than the default.
func (g *Gen) genUIdent() {
	if g.defKind != 0 {
		// A type head or variant ctor declares its name; keep it
		// unique so the file does not redefine it on the next
		// decl.
		var name string
		switch g.defKind {
		case defType:
			name = g.freshUIdent(g.declTypeS)
			g.declTypeS[name] = true
			g.declTypes = append(g.declTypes, name)
		case defCtor:
			name = g.freshUIdent(g.declCtorS)
			g.declCtorS[name] = true
			g.declCtors = append(g.declCtors, name)
		case defQual:
			// The "UIdent . UIdent" qualified shape — "Go.Map"
			// style module heads.
			if len(g.importMod) > 0 && g.rng.Float64() < 0.5 {
				name = g.importMod[g.rng.IntN(len(g.importMod))]
			} else if g.rng.Float64() < 0.3 {
				name = "Go" // the builtin Go interop module
			} else {
				name = uidentPool[g.rng.IntN(len(uidentPool))]
			}
		default:
			name = uidentPool[g.rng.IntN(len(uidentPool))]
		}
		g.defKind = 0
		g.emit(name)
		return
	}
	if g.opts.Typed {
		if name := g.scopeCtor(); name != "" && g.rng.Float64() < 0.7 {
			g.emit(name)
			return
		}
	} else if g.inType > 0 {
		// A type position wants a name that resolves in the type
		// namespace: a declared type when one exists, a builtin
		// otherwise — the pool draw that remains is the deliberate
		// "undefined type" near-miss.
		switch {
		case len(g.declTypes) > 0 && g.rng.Float64() < 0.55:
			g.emit(g.declTypes[g.rng.IntN(len(g.declTypes))])
		case g.rng.Float64() < 0.6:
			g.emit(builtinTypes[g.rng.IntN(len(builtinTypes))])
		default:
			g.emit(uidentPool[g.rng.IntN(len(uidentPool))])
		}
		return
	} else if n := len(g.declTypes) + len(g.declCtors); n > 0 && g.rng.Float64() < 0.6 {
		// A UIdent use site — type atom, member head, extern
		// item. The kind is not tracked, so some picks land in
		// the wrong namespace and still fail resolve.
		g.emit(g.pickUDecl())
		return
	}
	g.emit(uidentPool[g.rng.IntN(len(uidentPool))])
}

// freshUIdent picks an uppercase name not already present in set;
// once the pool is exhausted it coins a suffixed one.
func (g *Gen) freshUIdent(set map[string]bool) string {
	for tries := 0; tries < 20; tries++ {
		if n := uidentPool[g.rng.IntN(len(uidentPool))]; !set[n] {
			return n
		}
	}
	return uidentPool[g.rng.IntN(len(uidentPool))] + fmt.Sprint(g.rng.IntN(100))
}

// pickUDecl draws from the declared type/ctor names (insertion
// order — map iteration would break generation determinism).
func (g *Gen) pickUDecl() string {
	if len(g.declCtors) > 0 && (len(g.declTypes) == 0 || g.rng.Float64() < 0.6) {
		return g.declCtors[g.rng.IntN(len(g.declCtors))]
	}
	if len(g.declTypes) > 0 {
		return g.declTypes[g.rng.IntN(len(g.declTypes))]
	}
	return g.declCtors[g.rng.IntN(len(g.declCtors))]
}

// genInt emits an integer literal. Decimal dominates; the other bases
// and "_" separators appear for coverage. Leading-zero decimals are
// never emitted ("080" reaches generated Go as an invalid literal —
// the knownBugs registry pins that compiler bug and reproducing it on
// purpose would just burn canary signal).
func (g *Gen) genInt() {
	switch g.rng.IntN(12) {
	case 0:
		g.emitRaw(fmt.Sprintf("0x%x", g.rng.IntN(4096)))
	case 1:
		g.emitRaw(fmt.Sprintf("0b%b", g.rng.IntN(64)))
	case 2:
		g.emitRaw(fmt.Sprintf("0o%o", g.rng.IntN(64)))
	case 3:
		g.emitRaw(fmt.Sprintf("%d_%03d", g.rng.IntN(10), g.rng.IntN(1000)))
	default:
		g.emitRaw(fmt.Sprintf("%d", g.rng.IntN(100)))
	}
}

// genFloat emits a float literal — always with a fraction or an
// exponent (the grammar rejects "5." and ".5").
func (g *Gen) genFloat() {
	switch g.rng.IntN(6) {
	case 0:
		g.emitRaw(fmt.Sprintf("%de%d", g.rng.IntN(100), g.rng.IntN(4)))
	case 1:
		g.emitRaw(fmt.Sprintf("%d.%d", g.rng.IntN(10), g.rng.IntN(1000000)))
	case 2:
		g.emitRaw(fmt.Sprintf("%d.%de-%d", g.rng.IntN(100), g.rng.IntN(100), g.rng.IntN(4)))
	default:
		g.emitRaw(fmt.Sprintf("%d.%02d", g.rng.IntN(1000), g.rng.IntN(100)))
	}
}

// genChar emits a char literal from a small safe set.
func (g *Gen) genChar() {
	set := []string{"'a'", "'b'", "'x'", "'0'", "'Z'", "'\\n'", "'\\t'", "'\\''", "' '", "'#'"}
	g.emitRaw(set[g.rng.IntN(len(set))])
}

// stringFragments avoids the lexer-hostile bytes (quote, backslash,
// "${") inside literal runs; interpolation is added separately below.
var stringFragments = []string{
	"hello", "gom", "ok", "err happened", "abc", "x y z", "v=",
	"lit", "some text", "n:", "done", "line", "a,b,c", "...", "q",
	"tab\\there", "quote\\\"in", "back\\\\slash",
}

// goImportPaths is the std-package pool import-decl strings draw
// from — real paths, so "import go" resolves under an actual loader
// (the canary's allowlist is exactly this set) instead of every
// import being an E0203 coin flip.
var goImportPaths = []string{
	"bytes", "errors", "fmt", "io", "math", "os", "slices",
	"sort", "strconv", "strings", "time", "unicode/utf8",
}

// deterministicImportPaths is goImportPaths minus the packages whose
// members read the environment, the clock or process state — the
// diff subset must be reproducible run to run.
var deterministicImportPaths = []string{
	"bytes", "errors", "fmt", "math", "slices",
	"sort", "strconv", "strings", "unicode/utf8",
}

// genString emits a string literal. Most are plain text; some embed
// "${expr}" interpolation — the interpolation's own expansion goes
// through the grammar's expr rule, staying grammar-driven. Inside
// import/test/extern declarations (noInterp) only plain literals are
// allowed, so interpolation is suppressed there.
func (g *Gen) genString() {
	g.emitRaw("\"")
	// An import path is not free text: draw from real package
	// paths and record the bound module name (the basename, when
	// there is no alias) so qualified references can resolve.
	if g.curRule == "import-decl" {
		paths := goImportPaths
		if g.opts.Deterministic {
			paths = deterministicImportPaths
		}
		path := paths[g.rng.IntN(len(paths))]
		g.emitRaw(path + "\"")
		if g.importAliased {
			// `import go x "pkg"`: the alias site already recorded
			// the bound module name — the basename binds nothing.
			g.importAliased = false
			return
		}
		base := path
		if i := strings.LastIndexByte(path, '/'); i >= 0 {
			base = path[i+1:]
		}
		if !g.importSet[base] {
			g.importSet[base] = true
			g.importMod = append(g.importMod, base)
		}
		return
	}
	if g.noInterp > 0 || (g.opts.Deterministic && !g.opts.Typed) {
		// Plain literal: decl strings never interpolate, and under
		// the deterministic subset mechanical-mode splices are
		// suppressed entirely — a splice's type is untracked, so it
		// can render a composite whose display format is a known
		// eval-vs-compiled divergence (typed-mode splices are
		// scalar-typed and stay).
		g.emitRaw(stringFragments[g.rng.IntN(len(stringFragments))])
		g.emitRaw("\"")
		return
	}
	parts := 1 + g.rng.IntN(3)
	for i := 0; i < parts; i++ {
		if g.depth > 2 && g.rng.Float64() < 0.2 {
			// Interpolation: "${" counts as an opening delimiter for
			// the lexer, so embedded exprs may contain anything a
			// normal expr may — the depth cut keeps them small.
			g.emitRaw("${")
			save := g.depth
			g.depth = 2
			saveHead := g.headDepth
			g.headDepth = 0
			g.expand(ref("expr"))
			g.headDepth = saveHead
			g.depth = save
			g.emitRaw("}")
		} else {
			g.emitRaw(stringFragments[g.rng.IntN(len(stringFragments))])
		}
	}
	g.emitRaw("\"")
}

// genNL emits the newline token. sep derives from it — and an
// occasional ";" alternative falls out of the grammar's own
// (NL | ";") alternation.
func (g *Gen) genNL() {
	g.newline()
}
