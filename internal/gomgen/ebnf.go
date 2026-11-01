package gomgen

// An EBNF reader for the ISO/IEC 14977 notation docs/grammar.ebnf is
// written in: rules "name = def ;", terminals in single or double
// quotes, "," concatenation, "|" alternation, "[ ]" optional, "{ }"
// repetition, "( )" grouping, "(* *)" comments, "? ... ?" informal
// specials. The output is a rule table the generator walks; nothing
// here knows Gom syntax beyond that notation.

import (
	"fmt"
	"os"
	"strings"
)

// gexpr is one node of a grammar definition. The set is closed —
// expand() type-switches over it and panics on anything else.
type gexpr interface{ gexpr() }

// term is a terminal symbol, emitted verbatim: "let", "|>", "}".
type term string

// ref is a nonterminal reference: the name of another rule.
type ref string

// special is a "? ... ?" informal sequence. No rule reachable from
// source-file may contain one — lexical rules needing them have
// builtin producers instead, and validation enforces that.
type special string

// seq is concatenation: every member in order.
type seq []gexpr

// alt is alternation: exactly one member is chosen.
type alt []gexpr

// opt is "[ x ]": x or nothing.
type opt struct{ x gexpr }

// rep is "{ x }": x zero or more times.
type rep struct{ x gexpr }

func (term) gexpr()    {}
func (ref) gexpr()     {}
func (special) gexpr() {}
func (seq) gexpr()     {}
func (alt) gexpr()     {}
func (opt) gexpr()     {}
func (rep) gexpr()     {}

// Grammar is a parsed EBNF rule table plus the precomputed
// minimum-expansion-depth of every rule (used to keep generation
// finite under a depth budget), and the set of terminal byte pairs
// that must not be glued together during emission.
type Grammar struct {
	rules map[string]gexpr
	order []string // definition order, for deterministic iteration

	// minDepth[name] is the smallest nesting depth the rule needs to
	// emit a complete expansion. Depth counts *choice* hops: a ref
	// reached through a forced position — the only mandatory member
	// of its sequence, or the whole rule being an alias — is free,
	// so the precedence ladder (expr → … → atom) is not billed as
	// nesting. Alt takes the min, Seq the max, opt/rep 0. Unbounded
	// (recursive with no base case) rules get depthInfinite.
	minDepth map[string]int

	// mergeRisk holds byte pairs (prev, next) that maximal munch
	// would read as the start of a longer token — the first two
	// bytes of every multi-byte terminal in the grammar plus "//"
	// comments. Emission inserts a space between tokens when the
	// boundary pair is in this set.
	mergeRisk map[[2]byte]bool
}

// depthInfinite marks rules that cannot expand finitely; alternatives
// requiring them are never chosen voluntarily.
const depthInfinite = 1 << 30

// SourceFile is the grammar's entry rule for .gom sources.
const SourceFile = "source-file"

// Rules returns the defined rule names in file order.
func (g *Grammar) Rules() []string { return g.order }

// LoadGrammar reads and parses the EBNF file at path — the normal way
// to obtain docs/grammar.ebnf (kept out of the package on purpose:
// the docs file is the single source of truth).
func LoadGrammar(path string) (*Grammar, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseGrammar(src)
}

// ParseGrammar parses EBNF source into a Grammar, then validates the
// fragment generation can actually walk: every nonterminal referenced
// from start must be defined, and no "? ... ?" special may be
// reachable from start (rules that need them are expected to sit
// behind builtin producers). A violation means the grammar changed
// shape in a way this package cannot follow — the error names the
// offending rule.
func ParseGrammar(src []byte) (*Grammar, error) {
	p := &ebnfParser{src: string(src)}
	rules, order, err := p.parse()
	if err != nil {
		return nil, err
	}
	g := &Grammar{rules: rules, order: order}
	g.collectTerminals()
	g.computeMinDepth()
	if err := g.validateReachable(); err != nil {
		return nil, err
	}
	return g, nil
}

// collectTerminals fills mergeRisk from the grammar's own terminal
// set: for every terminal of two or more bytes, the pair of its
// first two bytes is a merge risk — emitting a token ending in
// terminal[0] directly before one starting with terminal[1] would
// let the lexer read the pair as this terminal's opening.
// Deriving the set keeps it correct when the grammar's operator
// table changes.
func (g *Grammar) collectTerminals() {
	g.mergeRisk = map[[2]byte]bool{}
	var walk func(e gexpr)
	walk = func(e gexpr) {
		switch x := e.(type) {
		case term:
			if len(x) >= 2 {
				g.mergeRisk[[2]byte{x[0], x[1]}] = true
			}
		case ref, special:
		case seq:
			for _, m := range x {
				walk(m)
			}
		case alt:
			for _, m := range x {
				walk(m)
			}
		case opt:
			walk(x.x)
		case rep:
			walk(x.x)
		default:
			panic("unreachable")
		}
	}
	for _, name := range g.order {
		walk(g.rules[name])
	}
}

// validateReachable walks the rule graph from SourceFile, skipping
// subtrees behind builtin producers (lexical rules), and reports the
// first problem found.
func (g *Grammar) validateReachable() error {
	if _, ok := g.rules[SourceFile]; !ok {
		return fmt.Errorf("gomgen: grammar has no %q rule", SourceFile)
	}
	seen := map[string]bool{}
	var visit func(e gexpr, via string) error
	visit = func(e gexpr, via string) error {
		switch x := e.(type) {
		case term:
			return nil
		case special:
			return fmt.Errorf("gomgen: informal rule ? %s ? is reachable via %s — it needs a builtin producer or a grammar rewrite", string(x), via)
		case ref:
			name := string(x)
			if isBuiltinRule(name) {
				return nil // produced, never walked
			}
			def, ok := g.rules[name]
			if !ok {
				return fmt.Errorf("gomgen: rule %q referenced via %s is not defined", name, via)
			}
			if seen[name] {
				return nil
			}
			seen[name] = true
			if g.minDepth[name] >= depthInfinite {
				return fmt.Errorf("gomgen: rule %q (via %s) cannot expand finitely — recursion without a base case would make Generate diverge", name, via)
			}
			return visit(def, name)
		case seq:
			for _, m := range x {
				if err := visit(m, via); err != nil {
					return err
				}
			}
			return nil
		case alt:
			for _, m := range x {
				if err := visit(m, via); err != nil {
					return err
				}
			}
			return nil
		case opt:
			return visit(x.x, via)
		case rep:
			return visit(x.x, via)
		default:
			panic("unreachable")
		}
	}
	if err := visit(g.rules[SourceFile], SourceFile); err != nil {
		return err
	}
	seen[SourceFile] = true
	return g.checkForcedCycles(seen)
}

// checkForcedCycles reports a load error when a cycle of *forced*
// descents is reachable: a rule whose only mandatory expansion is a
// ref into another rule (a pure alias, or a sequence whose sole
// non-skippable member is one ref). Such cycles spend no depth
// budget — they are exactly what the forced-position discount is —
// so without this check a loop like "a = b ; b = a ;" would pass
// minDepth (it converges to 0) yet make Generate recurse forever.
func (g *Grammar) checkForcedCycles(seen map[string]bool) error {
	const (
		white = iota // unvisited
		gray         // on the DFS stack
		black        // done
	)
	color := map[string]int{}
	var link func(name string) error
	link = func(name string) error {
		color[name] = gray
		if r, ok := g.forcedRef(name); ok {
			target := string(r)
			if seen[target] && !isBuiltinRule(target) {
				switch color[target] {
				case gray:
					return fmt.Errorf("gomgen: rules %q and %q form a forced-descent cycle — the chain can never emit a token", name, target)
				case white:
					if err := link(target); err != nil {
						return err
					}
				}
			}
		}
		color[name] = black
		return nil
	}
	for _, name := range g.order {
		if seen[name] && color[name] == white {
			if err := link(name); err != nil {
				return err
			}
		}
	}
	return nil
}

// soleMandatory locates the members of s that must emit something —
// everything but opt/rep and refs that can derive the empty string.
// It returns the index of the last such member and their count.
func (g *Grammar) soleMandatory(s seq) (last, n int) {
	last, n = -1, 0
	for i, m := range s {
		if !canBeEmpty(m, g, map[string]bool{}) {
			last, n = i, n+1
		}
	}
	return last, n
}

// forcedRef reports the rule name's forced descent, when its
// expansion is a pure through-line into one other rule: either the
// rule is an alias ("x = y ;") or its definition is a sequence whose
// only mandatory member is that one ref. ok is false when the rule
// offers real choice or produces tokens of its own.
func (g *Grammar) forcedRef(name string) (ref, bool) {
	def := g.rules[name]
	if r, ok := def.(ref); ok {
		return r, true
	}
	if s, ok := def.(seq); ok {
		last, n := g.soleMandatory(s)
		if n == 1 {
			if r, ok := s[last].(ref); ok {
				return r, true
			}
		}
	}
	return "", false
}

// computeMinDepth fills minDepth by fixpoint iteration. The
// recurrence is monotone, so the iteration converges in at most
// len(order) productive rounds when every rule can expand finitely
// (each round propagates depth one more edge along the dependency
// graph). Rules still moving after that are divergent — recursion
// with no base case feeds itself +1 depth per round — and only those
// collapse to depthInfinite; converged rules keep their real depths.
func (g *Grammar) computeMinDepth() {
	g.minDepth = map[string]int{}
	for i := 0; i < len(g.order)+8; i++ {
		changed := false
		for _, name := range g.order {
			d := g.needCtx(g.rules[name], true)
			if d > depthInfinite {
				d = depthInfinite
			}
			if d != g.minDepth[name] {
				g.minDepth[name] = d
				changed = true
			}
		}
		if !changed {
			return
		}
	}
	for _, name := range g.order {
		if g.needCtx(g.rules[name], true) != g.minDepth[name] {
			g.minDepth[name] = depthInfinite
		}
	}
}

// need reports the minimum nesting depth e needs — the expansion-time
// side of minDepth. Equivalent to needCtx(e, false).
func (g *Grammar) need(e gexpr) int { return g.needCtx(e, false) }

// needCtx is need with a position flag: forced marks e as the
// through-line of its production — a reference the grammar gives no
// choice about — so it costs no depth budget. (The precedence ladder
// expr → pipe-expr → … → atom is a chain of forced descents; billing
// it as nesting would starve every expression to leaf forms.)
func (g *Grammar) needCtx(e gexpr, forced bool) int {
	switch x := e.(type) {
	case term:
		return 0
	case special:
		return 1
	case ref:
		if forced {
			return g.minDepth[string(x)]
		}
		return 1 + g.minDepth[string(x)]
	case seq:
		// A sequence whose only mandatory member is one ref is a
		// descent, not a choice: that ref is expanded for free.
		last, n := g.soleMandatory(x)
		m := 0
		for i, c := range x {
			if d := g.needCtx(c, n == 1 && i == last); d > m {
				m = d
			}
		}
		return m
	case alt:
		m := depthInfinite
		for _, c := range x {
			if d := g.needCtx(c, false); d < m {
				m = d
			}
		}
		return m
	case opt:
		return 0
	case rep:
		return 0
	default:
		panic("unreachable")
	}
}

// ------------------------------------------------------------------
// scanner + parser
// ------------------------------------------------------------------

type ebnfToken struct {
	kind byte   // 'n' name, 't' terminal, '?' special, or the punctuation byte
	text string // name / terminal / special text
}

// scan tokenizes the EBNF notation: whitespace and "(* ... *)"
// comments are trivia, "..." / '...' are terminals (quotes cannot
// contain their own delimiter — the grammar writes '"' and "'"),
// "? ... ?" is a special sequence, names are [A-Za-z][A-Za-z0-9-]*.
func scan(src string) ([]ebnfToken, error) {
	var toks []ebnfToken
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*)")
			if end < 0 {
				return nil, fmt.Errorf("gomgen: unterminated comment at byte %d", i)
			}
			i += 2 + end + 2
		case c == '"' || c == '\'':
			end := strings.IndexByte(src[i+1:], c)
			if end < 0 {
				return nil, fmt.Errorf("gomgen: unterminated terminal at byte %d", i)
			}
			toks = append(toks, ebnfToken{kind: 't', text: src[i+1 : i+1+end]})
			i += 1 + end + 1
		case c == '?':
			end := strings.IndexByte(src[i+1:], '?')
			if end < 0 {
				return nil, fmt.Errorf("gomgen: unterminated special at byte %d", i)
			}
			toks = append(toks, ebnfToken{kind: '?', text: strings.TrimSpace(src[i+1 : i+1+end])})
			i += 1 + end + 1
		case c == '=' || c == ';' || c == ',' || c == '|' ||
			c == '[' || c == ']' || c == '{' || c == '}' ||
			c == '(' || c == ')':
			toks = append(toks, ebnfToken{kind: c})
			i++
		case isNameStart(c):
			j := i + 1
			for j < len(src) && isNameChar(src[j]) {
				j++
			}
			toks = append(toks, ebnfToken{kind: 'n', text: src[i:j]})
			i = j
		default:
			return nil, fmt.Errorf("gomgen: unexpected byte %q at %d", c, i)
		}
	}
	return toks, nil
}

func isNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isNameChar(c byte) bool {
	return isNameStart(c) || (c >= '0' && c <= '9') || c == '-'
}

type ebnfParser struct {
	src  string
	toks []ebnfToken
	pos  int
}

func (p *ebnfParser) parse() (map[string]gexpr, []string, error) {
	toks, err := scan(p.src)
	if err != nil {
		return nil, nil, err
	}
	p.toks = toks
	rules := map[string]gexpr{}
	var order []string
	for p.pos < len(p.toks) {
		t := p.toks[p.pos]
		if t.kind != 'n' {
			return nil, nil, fmt.Errorf("gomgen: expected rule name, found %q", p.describe(p.pos))
		}
		name := t.text
		p.pos++
		if err := p.expect('='); err != nil {
			return nil, nil, err
		}
		def, err := p.parseAlt()
		if err != nil {
			return nil, nil, fmt.Errorf("gomgen: rule %s: %w", name, err)
		}
		if err := p.expect(';'); err != nil {
			return nil, nil, fmt.Errorf("gomgen: rule %s: %w", name, err)
		}
		if _, dup := rules[name]; dup {
			return nil, nil, fmt.Errorf("gomgen: rule %q defined twice", name)
		}
		rules[name] = def
		order = append(order, name)
	}
	return rules, order, nil
}

func (p *ebnfParser) describe(pos int) string {
	if pos >= len(p.toks) {
		return "<eof>"
	}
	t := p.toks[pos]
	if t.kind == 'n' || t.kind == 't' || t.kind == '?' {
		return fmt.Sprintf("%c %q", t.kind, t.text)
	}
	return fmt.Sprintf("%q", string(t.kind))
}

func (p *ebnfParser) expect(kind byte) error {
	if p.pos >= len(p.toks) || p.toks[p.pos].kind != kind {
		return fmt.Errorf("gomgen: expected %q, found %s", string(kind), p.describe(p.pos))
	}
	p.pos++
	return nil
}

func (p *ebnfParser) parseAlt() (gexpr, error) {
	first, err := p.parseSeq()
	if err != nil {
		return nil, err
	}
	branches := []gexpr{first}
	for p.pos < len(p.toks) && p.toks[p.pos].kind == '|' {
		p.pos++
		next, err := p.parseSeq()
		if err != nil {
			return nil, err
		}
		branches = append(branches, next)
	}
	if len(branches) == 1 {
		return first, nil
	}
	return alt(branches), nil
}

func (p *ebnfParser) parseSeq() (gexpr, error) {
	first, err := p.parseFactor()
	if err != nil {
		return nil, err
	}
	items := []gexpr{first}
	for p.pos < len(p.toks) && p.toks[p.pos].kind == ',' {
		p.pos++
		next, err := p.parseFactor()
		if err != nil {
			return nil, err
		}
		items = append(items, next)
	}
	if len(items) == 1 {
		return first, nil
	}
	return seq(items), nil
}

func (p *ebnfParser) parseFactor() (gexpr, error) {
	if p.pos >= len(p.toks) {
		return nil, fmt.Errorf("unexpected end of input")
	}
	t := p.toks[p.pos]
	switch t.kind {
	case 'n':
		p.pos++
		return ref(t.text), nil
	case 't':
		p.pos++
		return term(t.text), nil
	case '?':
		p.pos++
		return special(t.text), nil
	case '(':
		p.pos++
		e, err := p.parseAlt()
		if err != nil {
			return nil, err
		}
		if err := p.expect(')'); err != nil {
			return nil, err
		}
		return e, nil
	case '[':
		p.pos++
		e, err := p.parseAlt()
		if err != nil {
			return nil, err
		}
		if err := p.expect(']'); err != nil {
			return nil, err
		}
		return opt{e}, nil
	case '{':
		p.pos++
		e, err := p.parseAlt()
		if err != nil {
			return nil, err
		}
		if err := p.expect('}'); err != nil {
			return nil, err
		}
		return rep{e}, nil
	default:
		return nil, fmt.Errorf("expected definition, found %s", p.describe(p.pos))
	}
}
