package gomgen

// The generator: a weighted walk over Grammar rules plus the
// context-sensitive bits EBNF cannot say — the head-expr brace
// restriction (grammar section 5), lexer-level token producers, and
// Options.Typed's scope/arity-aware emission (typed.go).
//
// Output is written one token at a time through emit(); every token
// is separated by a single space and newlines come only from the sep
// machinery (NL or ";"), which keeps the significant-newline rules of
// grammar section 3 working exactly as they do for a human writer.

import (
	"math/rand/v2"
	"strings"
)

// Options tunes Generate. The zero value is a usable mechanical
// generator; withDefaults fills the knobs callers leave alone.
type Options struct {
	// Depth bounds nesting depth of expansions (expressions,
	// patterns, types). 0 uses DefaultDepth. The budget is spent on
	// choice points — alternation branches and multi-member
	// productions — while forced descents (a rule that is just a
	// through-line into another) are free, so the precedence ladder
	// expr → … → atom does not consume it. Deeper budgets are
	// always completable: minDepth tells the walker which
	// alternatives can still finish, so a depth cut never truncates
	// mid-production.
	Depth int

	// MaxDecls bounds the top-level declaration repetition. 0 uses a
	// default of 6.
	MaxDecls int

	// MaxTokens is a soft cap on emitted tokens. Once spent,
	// repetitions stop and alternatives are chosen by minimum depth —
	// output still completes grammatically, it just shrinks. 0 uses a
	// default of 600.
	MaxTokens int

	// Typed switches on the scope/arity/type-aware emitters of
	// typed.go: declarations get signatures first and bodies that
	// aim at them, patterns bind usable names, applications prefer
	// callees whose declared arity matches. Semantic errors remain
	// possible — the mode raises the fraction reaching codegen, it
	// does not promise well-typed output.
	Typed bool

	// Perturb is the per-terminal probability of a near-miss edit
	// (drop or token swap), for feeding the parser fuzzer almost-valid
	// input. 0 (the default) aims at grammar-valid programs; the
	// typed emitters ignore it — perturbation would defeat their
	// purpose.
	Perturb float64
}

// DefaultDepth is the default expansion depth budget.
const DefaultDepth = 7

func (o Options) withDefaults() Options {
	if o.Depth <= 0 {
		o.Depth = DefaultDepth
	}
	if o.MaxDecls <= 0 {
		o.MaxDecls = 6
	}
	if o.MaxTokens <= 0 {
		o.MaxTokens = 600
	}
	return o
}

// Generate expands the grammar's source-file rule under opts using
// rng. Same rng state ⇒ identical output; generation cannot hang
// (depth and token budgets always decrease toward minimum-depth
// alternatives) and never panics on well-formed input.
func Generate(g *Grammar, rng *rand.Rand, opts Options) string {
	opts = opts.withDefaults()
	gen := &Gen{
		g:         g,
		rng:       rng,
		opts:      opts,
		depth:     opts.Depth,
		left:      opts.MaxTokens,
		declTypeS: map[string]bool{},
		declCtorS: map[string]bool{},
		importSet: map[string]bool{},
		boundSet:  map[string]bool{},
	}
	if opts.Typed {
		gen.initTyped()
	}
	gen.expand(ref(SourceFile))
	out := strings.TrimRight(gen.buf.String(), " \t")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

// Gen is the one-shot generation state; use Generate, not Gen
// directly.
type Gen struct {
	g    *Grammar
	rng  *rand.Rand
	opts Options

	buf   strings.Builder
	last  byte // last byte emitted, for spacing decisions
	depth int  // remaining expansion depth
	left  int  // remaining soft token budget

	// headDepth > 0 while expanding a head-expr: grammar section 5
	// bars "{" from starting an atom at that level. Cleared inside
	// paren-expr and list-expr, which nest a fresh non-head context.
	headDepth int

	// curRule is the rule currently being expanded — repetition
	// policy is keyed by it.
	curRule string

	// noInterp > 0 inside decls whose string must be a plain
	// literal (import-decl, test-decl, extern-decl) — the parser
	// rejects interpolated strings there.
	noInterp int

	// Mechanical-mode name tracking. The walker does no scope
	// analysis, but names a declaration site emitted are recorded
	// and preferred at use sites — a reference that CAN resolve
	// mostly does, while the remaining pool draws still produce
	// the unbound-name near-misses a canary wants.
	declTypes []string        // type-decl heads
	declTypeS map[string]bool // dedup for declTypes
	declCtors []string        // variant heads
	declCtorS map[string]bool // dedup for declCtors
	importMod []string        // module names an import binds
	importSet map[string]bool // dedup for importMod
	bound     []string        // pattern/fn bindings so far
	boundSet  map[string]bool // dedup for top-level fn names
	defKind   byte            // != 0 while expanding a name-defining member
	bindNext  bool            // the next ident is a binding occurrence
	inPat     int             // > 0 inside a pattern subtree (its idents bind)
	inType    int             // > 0 inside a type subtree (the type namespace)
	inLabel   int             // > 0 inside a field/member-label subtree

	// typed-mode state (typed.go) — nil/empty in mechanical mode.
	scope   [][]binding
	types   []string             // declared type names, declaration order
	ctors   map[string][]ctorDef // ADT name → constructors
	records map[string][]fieldDef
	fns     []fnDef // declared fns, for applications
	curRet  *gty    // return type of the fn body being generated
	imports map[string]bool
	// inTest > 0 inside a test block — assert/assert_eq may only be
	// emitted there (lowering reports E0901 otherwise; the semantic
	// question is open — docs/problems/2026-11-02-assert-outside-test).
	inTest int
}

// ------------------------------------------------------------------
// emission
// ------------------------------------------------------------------

// emit appends one token. Spacing is mechanical: a space between two
// adjacent "wordy" bytes — anywhere else the file's own spacing rules
// don't apply because no tokens are ever split or glued across
// multi-character boundaries (operators arrive verbatim from their
// terminals).
func (g *Gen) emit(s string) {
	if s == "" {
		return
	}
	if g.opts.Perturb > 0 && !g.opts.Typed {
		s = g.perturb(s)
		if s == "" {
			return // dropped terminal — the near-miss knob
		}
	}
	g.left--
	if g.needsSpace(g.last, s[0]) {
		g.buf.WriteByte(' ')
	}
	g.buf.WriteString(s)
	g.last = s[len(s)-1]
}

// emitRaw appends literal text (already-complete fragments like whole
// string literals or interpolated contents) with the same spacing
// rule as emit but no perturbation.
func (g *Gen) emitRaw(s string) {
	if s == "" {
		return
	}
	g.left--
	if g.needsSpace(g.last, s[0]) {
		g.buf.WriteByte(' ')
	}
	g.buf.WriteString(s)
	g.last = s[len(s)-1]
}

// newline emits the NL token.
func (g *Gen) newline() {
	g.left--
	g.buf.WriteByte('\n')
	g.last = '\n'
}

func (g *Gen) needsSpace(prev, next byte) bool {
	if prev == 0 || prev == '\n' || prev == ' ' {
		return false
	}
	if isWordByte(prev) && isWordByte(next) {
		return true
	}
	// Gluing is only unsafe when prev is the last byte of an
	// operator terminal that next would extend into a different,
	// longer token (maximal munch): "-" ">" would lex as "->".
	// The risky pairs are derived from the grammar's own terminal
	// set at load (Grammar.mergeRisk), so new multi-byte operators
	// are covered automatically. Word bytes never extend an
	// operator, and two word bytes are already covered above.
	return g.g.mergeRisk[[2]byte{prev, next}]
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// perturb implements Options.Perturb: drop the terminal, or swap it
// for a plausibly-confusable token. Only called for mechanical
// emission, so every outcome stays inside the "user diagnostic" side
// of the contract.
func (g *Gen) perturb(s string) string {
	if g.rng.Float64() >= g.opts.Perturb {
		return s
	}
	if g.rng.IntN(2) == 0 {
		return "" // dropped token → unmatched delimiters, missing seps
	}
	return perturbPool[g.rng.IntN(len(perturbPool))]
}

var perturbPool = []string{
	")", "}", "]", "->", "|>", "=", ",", "else", "with", "~", "<-", "in",
	"0", "x", "\"", "'a'", "..", "|", "${", "match", "fn", "let", "@",
}

// ------------------------------------------------------------------
// the walker
// ------------------------------------------------------------------

// expand dispatches on the grammar node. Rule references go through
// expandRef, which checks for builtin producers and the depth budget.
func (g *Gen) expand(e gexpr) {
	switch x := e.(type) {
	case term:
		g.emit(string(x))
	case special:
		panic("unreachable: validated grammars have no reachable specials")
	case ref:
		g.expandRef(string(x), 1)
	case seq:
		g.expandSeq(x, false)
	case alt:
		g.expand(g.pickAlt(x))
	case opt:
		// Optional members are skipped once the budget is tight; the
		// coin is otherwise biased toward including.
		if g.depth > 0 && g.left > 0 && g.rng.Float64() < 0.4 {
			g.expand(x.x)
		}
	case rep:
		n := g.repCount()
		for i := 0; i < n; i++ {
			g.expand(x.x)
		}
	default:
		panic("unreachable")
	}
}

// expandSeq walks one sequence. top marks the sequence that IS the
// current rule's definition — only its member positions can be name
// definition sites (an anonymous seq nested inside an opt/alt member
// has indices of its own, unrelated to the enclosing rule's).
func (g *Gen) expandSeq(x seq, top bool) {
	// A sequence whose only mandatory member is one ref is a
	// forced descent (the precedence ladder's shape): that ref
	// spends no depth budget, matching needCtx's accounting.
	last, n := g.g.soleMandatory(x)
	for i, m := range x {
		if top {
			// Some member positions DEFINE a name (the "type X"
			// head, a variant's ctor, the fn name, an import
			// alias) — flag them so the token producer records
			// what it emits for later use-site preference.
			g.defKind = defSiteOf(g.curRule, i)
			// "let" and "for ... in" patterns must be
			// irrefutable [SEMANTIC, spec 1.1.5] — the grammar
			// admits the full pattern language, so emit only
			// the irrefutable fragment.
			if irrPatSite(g.curRule, i) {
				g.irrefutablePat(2)
				g.defKind = 0
				continue
			}
			// recv-arm's member 0 is the binding "( ident | "_" )"
			// — the arm's exprs are ordinary references.
			if g.curRule == "recv-arm" && i == 0 {
				g.bindNext = true
			}
			// "select {}" with zero arms is E0705 — keep it rare:
			// the arms opt usually includes at least one arm.
			if g.curRule == "select-expr" && i == 2 {
				if o, ok := m.(opt); ok && g.depth > 0 && g.left > 0 && g.rng.Float64() < 0.85 {
					g.expand(o.x)
					g.defKind = 0
					g.bindNext = false
					continue
				}
			}
		} else if g.curRule == "type-atom" && i == 0 && isQualSeq(x) {
			// type-atom's "ident . UIdent" qualified branch:
			// the qualifier only resolves when it names a
			// bound module.
			g.defKind = defQual
		}
		if n == 1 && i == last {
			if r, ok := m.(ref); ok {
				g.expandRef(string(r), 0)
				g.defKind = 0
				g.bindNext = false
				continue
			}
		}
		g.expand(m)
		g.defKind = 0
		g.bindNext = false
	}
}

// defSiteOf maps a (rule, top-level member index) pair to the kind
// of name the member defines — the token producer then records the
// emitted name into the matching set instead of treating it as a
// use. Only positions whose first token is the defined name appear:
// a "type X = ..." head, a variant's ctor, the fn name in fn-sig,
// an import's optional alias.
func defSiteOf(rule string, i int) byte {
	switch rule {
	case "type-decl", "type-decl-iface":
		if i == 1 { // "type" , UIdent , ...
			return defType
		}
	case "variant":
		if i == 0 { // UIdent , { variant-field }
			return defCtor
		}
	case "fn-sig":
		if i == 1 { // "fn" , ident , ...
			return defFn
		}
	case "import-decl":
		if i == 2 { // "import" , [ "go" ] , [ ident ] , STRING
			return defAlias
		}
	}
	return 0
}

// Name-definition kinds for Gen.defKind.
const (
	defType  = byte(1) // declares a type name
	defCtor  = byte(2) // declares a constructor
	defFn    = byte(3) // declares a callable name
	defAlias = byte(4) // declares a module alias
	defQual  = byte(5) // references a module (qualified-name head)
)

// irrPatSite reports whether (rule, member index) is a pattern
// position that must be irrefutable — "let pat = ..." and
// "for pat in ..." both admit only ident/"_"/()/(tuple)/{record}
// fragments [SEMANTIC, spec 1.1.5]. Match arms keep the full
// pattern language — refutability is their point.
func irrPatSite(rule string, i int) bool {
	return (rule == "let-decl" || rule == "for-expr") && i == 1
}

// isQualSeq reports whether x is a qualified-name sequence
// "name . UIdent" — the shape of type-atom's qualified branches,
// whose leading name is a module reference, not a type.
func isQualSeq(x seq) bool {
	return len(x) == 3 && x[1] == term(".") &&
		(x[0] == ref("ident") || x[0] == ref("UIdent")) &&
		x[2] == ref("UIdent")
}

// irrefutablePat emits the irrefutable fragment of the pattern
// language for let/for binding positions: a plain name or "_",
// the Unit pattern, or a tuple/record of the same. d bounds
// nesting so depth starvation still completes.
func (g *Gen) irrefutablePat(d int) {
	switch {
	case d <= 0:
		g.emit("_")
	case g.rng.Float64() < 0.6:
		// Leaf: a bound name (recorded) or "_".
		if g.rng.Float64() < 0.8 {
			name := g.freshIdent()
			g.emit(name)
			g.bound = append(g.bound, name)
		} else {
			g.emit("_")
		}
	case g.rng.Float64() < 0.5:
		// Tuple pattern: "(" p "," p {"," p} [","] ")".
		g.emit("(")
		g.irrefutablePat(d - 1)
		g.emit(",")
		g.irrefutablePat(d - 1)
		for g.rng.Float64() < 0.3 {
			g.emit(",")
			g.irrefutablePat(d - 1)
		}
		g.emit(")")
	default:
		// Record pattern: "{" f ["=" p] {"," ...} "}" — a bare
		// field puns a binding of the same name.
		g.emit("{")
		n := 1 + g.rng.IntN(3)
		for i := 0; i < n; i++ {
			if i > 0 {
				g.emit(",")
			}
			name := g.freshIdent()
			g.emit(name)
			g.bound = append(g.bound, name)
			if g.rng.Float64() < 0.3 {
				g.emit("=")
				g.irrefutablePat(d - 1)
			}
		}
		g.emit("}")
	}
}

// patternRules are the grammar rules inside which an emitted ident
// is a binding occurrence — pattern idents (including "as" names,
// ".." rests and record-puns) bind a name where expression idents
// reference one. The labeled-argument ident of pat-arg and the
// module qualifier of a qualified ctor are over-recorded; the cost
// is an occasional still-unresolvable reference, not a wrong tree.
var patternRules = map[string]bool{
	"pattern": true, "or-pattern": true, "ctor-pattern": true,
	"pat-atom": true, "pat-arg": true, "tuple-pat": true,
	"list-pat": true, "brace-pat": true, "pat-field": true,
	"lambda-param": true, // ident | "_" | "()" — all bind
}

// typeRules are the rules whose tokens live in the type namespace:
// a UIdent there wants a declared type name and an ident is a type
// variable — neither should draw from the value-binding pool.
var typeRules = map[string]bool{
	"type-expr": true, "arrow-type": true, "app-type": true,
	"type-atom": true, "tuple-type": true, "record-type": true,
}

// labelRules are the rules whose idents are field/member LABELS —
// record fields, member names, variant field names, pattern field
// puns. A label is not a variable reference, so the bound-name
// preference is suppressed inside them (repeating a bound name as
// a field label also mints duplicate-field errors for free).
var labelRules = map[string]bool{
	"member-name": true, "record-field": true,
	"pat-field": true, "variant-field": true,
}

// expandRef expands one nonterminal at the given depth cost: builtin
// token producers first, then head-expr's brace suppression, then
// the typed-mode overrides, then the mechanical rule walk. Cost is 0
// for forced descents — aliases and the sole-mandatory-member case —
// so the budget buys real choice points, not ladder plumbing.
func (g *Gen) expandRef(name string, cost int) {
	if g.opts.Typed && g.expandTypedRef(name) {
		return
	}
	if produce, ok := builtinProducer(name); ok {
		produce(g)
		return
	}
	def := g.g.rules[name]
	if def == nil {
		panic("unreachable: validated grammars define every reachable rule")
	}
	// attribute = "@" member-name — the grammar admits any member
	// name but the parser itself rejects anything but "@test"
	// ([SEMANTIC] in the grammar, enforced as E0101). Keeping the
	// random branch rare preserves coverage without drowning the
	// clean-parse rate.
	if name == "attribute" && g.rng.Float64() < 0.92 {
		g.emit("@")
		g.emit("test")
		return
	}
	// ctor-pattern = ctor-name { pat-arg } | pat-atom — a ctor
	// pattern can only resolve when some variant declared a
	// constructor; with none declared every pick is a guaranteed
	// E0202, so collapse to the pat-atom branch until one exists.
	if name == "ctor-pattern" && len(g.declCtors) == 0 && !g.opts.Typed {
		g.expand(ref("pat-atom"))
		return
	}
	// ctor-name = [ ( ident | UIdent ) , "." ] , UIdent — a
	// qualifier resolves only when it names a bound module, and
	// the head only when a variant declared the ctor; both come
	// from the tracked sets, with pool draws left over as the
	// deliberate unbound-name near-miss.
	if name == "ctor-name" {
		if len(g.importMod) > 0 && g.rng.Float64() < 0.15 {
			g.emit(g.importMod[g.rng.IntN(len(g.importMod))])
			g.emit(".")
		}
		if len(g.declCtors) > 0 && g.rng.Float64() < 0.7 {
			g.emit(g.declCtors[g.rng.IntN(len(g.declCtors))])
		} else {
			g.emit(uidentPool[g.rng.IntN(len(uidentPool))])
		}
		return
	}
	// fn-param = "(" ")" | "(" [ "~" ] pattern ":" type-expr ")" —
	// another semantic restriction the grammar writes loosely: the
	// parser binds only a plain name after "~" (E0101 "a labeled
	// parameter binds a plain name"), so the labeled shape is
	// emitted with an ident while the unlabeled pattern slot stays
	// fully general.
	if name == "fn-param" {
		switch {
		case g.depth <= 0, g.left <= 0, g.rng.Float64() < 0.25:
			g.emit("(")
			g.emit(")")
		default:
			g.emit("(")
			if g.rng.Float64() < 0.3 {
				// "~name : T" — the label IS the bound name.
				name := g.freshIdent()
				g.emit("~")
				g.emit(name)
				g.bound = append(g.bound, name)
			} else {
				// Parameter patterns must be irrefutable too
				// [SEMANTIC, spec 4] — same restriction as let.
				g.irrefutablePat(2)
			}
			g.emit(":")
			g.expand(ref("type-expr"))
			g.emit(")")
		}
		return
	}
	save := g.depth
	saveRule := g.curRule
	saveHead := g.headDepth
	saveInterp := g.noInterp
	g.depth -= cost
	g.curRule = name
	switch name {
	case "head-expr":
		g.headDepth++
	case "block":
		// Nested-delimiter contexts reset the head-expr brace ban
		// (grammar section 5: "a '{' nested inside '(', '[' or a
		// nested '{' is unaffected"). Match/select arms always sit
		// inside their construct's "{" — a fresh expr context.
		//
		// A block is also a fresh string context: the plain-literal
		// rule covers only the decl-level strings (import path,
		// test name, extern package), not strings inside the
		// decl's block, which may interpolate.
		g.headDepth = 0
		g.noInterp = 0
	case "paren-expr", "list-expr", "record-literal",
		"record-update", "brace-pat", "tuple-pat", "list-pat",
		"tuple-type", "record-type", "match-arm", "select-arm":
		g.headDepth = 0
	case "import-decl", "test-decl", "extern-decl":
		// The path/name strings of these decls must be plain
		// literals — interpolation makes them not-a-string-literal
		// for the parser (E0101 "expected an import path string").
		g.noInterp++
	}
	// Idents emitted inside a pattern subtree are binding
	// occurrences — they feed the bound-name pool use sites prefer.
	if patternRules[name] {
		g.inPat++
	}
	if typeRules[name] {
		g.inType++
	}
	if labelRules[name] {
		g.inLabel++
	}
	// A rule defined as a bare alias is a forced descent too:
	// expanding its single member costs nothing on top of the call.
	// The rule's own definition sequence is also where member-level
	// name definition sites live (expandSeq's top flag).
	switch d := def.(type) {
	case ref:
		g.expandRef(string(d), 0)
	case seq:
		g.expandSeq(d, true)
	default:
		g.expand(def)
	}
	if labelRules[name] {
		g.inLabel--
	}
	if typeRules[name] {
		g.inType--
	}
	if patternRules[name] {
		g.inPat--
	}
	g.depth = save
	g.curRule = saveRule
	g.headDepth = saveHead
	g.noInterp = saveInterp
}

// pickAlt chooses one alternation branch: branches the remaining
// depth can still complete get their configured weight; when nothing
// fits, the shallowest branch wins so the walk always finishes.
func (g *Gen) pickAlt(a alt) gexpr {
	type cand struct {
		e    gexpr
		need int
	}
	allowed := make([]cand, 0, len(a))
	// fallback is the shallowest branch that is still legal at head
	// level — the same brace ban altWeight enforces below, so depth
	// starvation cannot leak a "{"-led head atom either. At
	// non-head level every branch is legal, so it is simply the
	// shallowest branch overall.
	fallback := cand{e: a[0], need: depthInfinite}
	for _, m := range a {
		need := g.g.need(m)
		if g.headDepth == 0 || !startsWithBrace(m, g.g, map[string]bool{}) {
			if need < fallback.need {
				fallback = cand{e: m, need: need}
			}
		}
		if need <= g.depth {
			allowed = append(allowed, cand{e: m, need: need})
		}
	}
	if len(allowed) == 0 {
		return fallback.e
	}
	// altWeight also encodes the head-expr brace ban: at head level a
	// branch that can start with "{" has weight 0 — the parser would
	// reject it, so it simply is not an alternative there.
	total := 0
	weights := make([]int, len(allowed))
	for i, c := range allowed {
		w := g.altWeight(c.e)
		weights[i] = w
		total += w
	}
	if total <= 0 {
		for _, c := range allowed {
			if g.altWeight(c.e) > 0 {
				return c.e
			}
		}
		return fallback.e
	}
	r := g.rng.IntN(total)
	for i, w := range weights {
		if r < w {
			return allowed[i].e
		}
		r -= w
	}
	return allowed[len(allowed)-1].e
}

// altWeight is the per-alternative weight table. Defaults are flat;
// a few productions get shaped odds so output looks like programs
// rather than token soup, and head-level brace atoms get weight 0.
func (g *Gen) altWeight(e gexpr) int {
	// Under the head-expr ban, any branch that can begin with a
	// brace atom is dead weight — the parser would reject it. The
	// ban is enforced structurally: these branches simply are not
	// alternatives at head level.
	if g.headDepth > 0 {
		if startsWithBrace(e, g.g, map[string]bool{}) {
			return 0
		}
	}
	switch x := e.(type) {
	case term:
		switch string(x) {
		case "_":
			// "_" is an atom everywhere in the grammar, but spec
			// 8.2 admits it only as a Go-call argument — in
			// expression positions (inPat == 0) keep it rare so
			// E0504 stays a near-miss instead of the norm.
			if g.inPat == 0 {
				return 1
			}
			return 4
		case "|?>", "<<", ">>", "<|", "|>":
			return 1 // pipe ops: keep |>/ <frequent enough but not dominant
		}
		return 4
	case ref:
		switch string(x) {
		case "control-expr":
			return 2 // if/match/lambda are big; keep pipe exprs dominant
		case "match-expr", "select-expr", "go-expr", "for-expr":
			return 1
		case "if-expr":
			return 3
		case "lambda":
			return 2
		case "record-literal", "record-update":
			return 1
		case "block":
			return 3
		case "tuple-pat", "brace-pat", "list-pat":
			return 1
		case "paren-expr", "list-expr":
			return 2
		case "import-decl", "extern-decl", "test-decl":
			return 1
		case "fn-decl", "type-decl":
			return 4
		case "let-decl":
			return 3
		case "literal":
			return 5
		case "ident":
			return 4
		case "UIdent":
			return 2
		case "CHAR":
			return 1
		case "STRING", "FLOAT":
			return 2
		case "INT":
			return 5
		case "keyword": // member-name keyword: legal but weird
			return 1
		}
		return 3
	case seq:
		return 3
	case opt, rep:
		return 3
	default:
		panic("unreachable")
	}
}

// startsWithBrace reports whether e's expansion can produce a token
// stream starting with "{" — used under the head-expr ban. Brackets
// ("(", "[") stop the check: a "{" nested inside them is legal.
func startsWithBrace(e gexpr, g *Grammar, seen map[string]bool) bool {
	switch x := e.(type) {
	case term:
		return string(x) == "{"
	case ref:
		name := string(x)
		if isBuiltinRule(name) || seen[name] {
			return false
		}
		seen[name] = true
		def := g.rules[name]
		if def == nil {
			return false
		}
		return startsWithBrace(def, g, seen)
	case seq:
		// The first token comes from the earliest member that must
		// emit something; skippable members (opt/rep, or a ref that
		// can derive empty) can be bypassed, so keep scanning until
		// a mandatory one.
		for _, m := range x {
			if startsWithBrace(m, g, seen) {
				return true
			}
			if !canBeEmpty(m, g, map[string]bool{}) {
				return false
			}
		}
		return false
	case alt:
		for _, m := range x {
			if startsWithBrace(m, g, seen) {
				return true
			}
		}
		return false
	case opt:
		return startsWithBrace(x.x, g, seen)
	case rep:
		return startsWithBrace(x.x, g, seen)
	default:
		panic("unreachable")
	}
}

// canBeEmpty reports whether e can expand to zero tokens.
func canBeEmpty(e gexpr, g *Grammar, seen map[string]bool) bool {
	switch x := e.(type) {
	case term, special:
		return false
	case ref:
		name := string(x)
		if isBuiltinRule(name) || seen[name] {
			return false
		}
		seen[name] = true
		def := g.rules[name]
		if def == nil {
			return false
		}
		return canBeEmpty(def, g, seen)
	case seq:
		for _, m := range x {
			if !canBeEmpty(m, g, seen) {
				return false
			}
		}
		return true
	case alt:
		for _, m := range x {
			if canBeEmpty(m, g, seen) {
				return true
			}
		}
		return false
	case opt, rep:
		return true
	default:
		panic("unreachable")
	}
}

// repCount picks a repetition length: geometric-ish, biased small,
// and forced to 0 once the depth or token budget is spent (which is
// what lets output shrink to a close instead of stopping mid-way).
// Per-rule policies shape the few productions where the default is
// too timid (top-level decls, blocks, match arms).
func (g *Gen) repCount() int {
	if g.depth <= 0 || g.left <= 0 {
		return 0
	}
	cap, p := repPolicyFor(g)
	n := 0
	for n < cap && g.rng.Float64() < p {
		n++
	}
	return n
}

func repPolicyFor(g *Gen) (cap int, p float64) {
	switch g.curRule {
	case SourceFile:
		return g.opts.MaxDecls, 0.75
	case "block", "match-expr", "select-expr":
		return 5, 0.6
	case "extern-decl", "list-expr", "record-type", "record-literal", "record-update":
		return 4, 0.55
	case "apply-expr", "variant", "type-decl", "fn-sig":
		return 3, 0.5
	case "pipe-expr", "bool-or", "bool-and", "add-expr", "mul-expr",
		"postfix-expr", "or-pattern", "ctor-pattern", "tuple-pat",
		"list-pat", "brace-pat", "tuple-type", "app-type", "arrow-type",
		"lambda", "paren-expr", "type-decl-iface":
		return 2, 0.4
	}
	return 3, 0.45
}
