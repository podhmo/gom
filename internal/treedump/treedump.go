package treedump

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// --- printed tree ---

// form is one node of the printed S-expression tree: `(text kids...)`
// — or, when bare, a raw atom glued into the parent's line.
type form struct {
	text string
	kids []*form
	bare bool
}

// nodef builds a `(text kids...)` form.
func nodef(text string, kids ...*form) *form { return &form{text: text, kids: kids} }

// atomf builds a bare atom: no parentheses, joined into the parent
// line (or placed on its own line when siblings are blocks).
func atomf(text string) *form { return &form{text: text, bare: true} }

func render(f *form) string {
	var sb strings.Builder
	f.write(&sb, 0)
	sb.WriteByte('\n')
	return sb.String()
}

// write prints the form at indent ind. A flat form — one whose whole
// subtree fits the single-line rule — stays on the parent line;
// anything bigger breaks every child onto its own line. The rule is
// structural, so layout never depends on line width.
func (f *form) write(sb *strings.Builder, ind int) {
	if f.bare {
		sb.WriteString(f.text)
		return
	}
	sb.WriteByte('(')
	sb.WriteString(f.text)
	multi := !f.flat()
	for _, k := range f.kids {
		if multi {
			sb.WriteByte('\n')
			sb.WriteString(strings.Repeat("  ", ind+1))
			k.write(sb, ind+1)
		} else {
			sb.WriteByte(' ')
			k.write(sb, ind)
		}
	}
	sb.WriteByte(')')
}

// flat reports whether the form renders on a single line: at most one
// block child, itself flat. Two or more block children — the sign
// that a node is a real branching point — force the multi-line layout
// all the way up to the first ancestor that is also too big.
func (f *form) flat() bool {
	blocks := 0
	for _, k := range f.kids {
		if k.bare {
			continue
		}
		if !k.flat() {
			return false
		}
		blocks++
	}
	return blocks <= 1
}

// inline renders a form on a single line regardless of shape — for
// embedding a small subexpression into a parent's head.
func inline(f *form) string {
	var sb strings.Builder
	f.writeInline(&sb)
	return sb.String()
}

func (f *form) writeInline(sb *strings.Builder) {
	if f.bare {
		sb.WriteString(f.text)
		return
	}
	sb.WriteByte('(')
	sb.WriteString(f.text)
	for _, k := range f.kids {
		sb.WriteByte(' ')
		k.writeInline(sb)
	}
	sb.WriteByte(')')
}

// --- atoms, spans, types ---

// atom renders s as a bare token. Strings that would break the
// single-token reading — whitespace, parens, quotes, backslashes —
// are Go-quoted instead; the empty string becomes `""`.
func atom(s string) string {
	if s == "" {
		return `""`
	}
	for _, r := range s {
		if unicode.IsSpace(r) || r == '(' || r == ')' || r == '"' || r == '\\' {
			return strconv.Quote(s)
		}
	}
	return s
}

// quote renders an always-quoted string (comment text, import paths,
// test names) using Go escaping — stable across platforms.
func quote(s string) string { return strconv.Quote(s) }

// at renders a span as `@<start>:<end>` in UTF-8 byte offsets, glued
// to the preceding token. The zero span renders empty.
func at(sp diag.Span) string {
	if sp.IsZero() {
		return ""
	}
	return fmt.Sprintf("@%d:%d", sp.Start, sp.End)
}

// tatom renders a type as a single token, parenthesized when its
// surface rendering contains a space.
func tatom(t types.Type) string {
	if t == nil {
		return "?"
	}
	s := t.String()
	if strings.ContainsRune(s, ' ') {
		return "(" + s + ")"
	}
	return s
}

// typ renders a node's type annotation: `:Int`, `:(List Int)`, or `:?`
// for a missing type.
func typ(t types.Type) string { return ":" + tatom(t) }

// --- the dumper ---

// dumper carries the per-dump state: the symbol interner that gives
// every *resolve.Symbol a stable `%<n>` index in first-encounter
// order, and the resolved side tables consulted when a resolved file
// is dumped — nil for pure syntax and typed dumps.
type dumper struct {
	ids   map[*resolve.Symbol]int
	order []*resolve.Symbol
	defs  map[*syntax.Ident]*resolve.Symbol
	uses  map[*syntax.Ident]*resolve.Symbol
}

func newDumper() *dumper {
	return &dumper{ids: make(map[*resolve.Symbol]int)}
}

// symRef renders a symbol reference `name%<n>` (`%<n>` for unnamed
// symbols). The first occurrence of a symbol assigns its index.
func (d *dumper) symRef(s *resolve.Symbol) string {
	if s == nil {
		return "?%"
	}
	id, ok := d.ids[s]
	if !ok {
		id = len(d.order) + 1
		d.ids[s] = id
		d.order = append(d.order, s)
	}
	if s.Name == "" {
		return fmt.Sprintf("%%%d", id)
	}
	return fmt.Sprintf("%s%%%d", atom(s.Name), id)
}

// bareIdent renders an identifier's name and binding without a span —
// `name` or `name%<n>` — for compounding into a larger atom
// (`io%1.Reader%2@10:18`).
func (d *dumper) bareIdent(id *syntax.Ident) string {
	if id == nil {
		return "?"
	}
	if s := d.defs[id]; s != nil {
		return d.symRef(s)
	}
	if s := d.uses[id]; s != nil {
		return d.symRef(s)
	}
	return atom(id.Name)
}

// ident renders a surface identifier: `name@start:end` when
// unresolved, `name%<n>@start:end` when a resolved dump binds it.
func (d *dumper) ident(id *syntax.Ident) *form {
	return atomf(d.bareIdent(id) + atId(id))
}

// atId renders an identifier's span, tolerating nil.
func atId(id *syntax.Ident) string {
	if id == nil {
		return ""
	}
	return at(id.Sp)
}

// nameText is ident as a raw string, for gluing a name into its
// node's head (`fn@14:79 add@17:20`, `~to@52:55`).
func (d *dumper) nameText(id *syntax.Ident) string {
	return d.bareIdent(id) + atId(id)
}

// syms renders the `(syms)` section: every interned symbol in index
// order with its kind, definition span and origin flags.
func (d *dumper) syms() *form {
	kids := make([]*form, 0, len(d.order))
	for i, s := range d.order {
		var b strings.Builder
		fmt.Fprintf(&b, "sym %%%d %s %s", i+1, atom(s.Name), s.Kind)
		if ds := s.DefSpan(); !ds.IsZero() {
			fmt.Fprintf(&b, " def@%d:%d", ds.Start, ds.End)
		}
		if s.Package != "" {
			fmt.Fprintf(&b, " :pkg %s", quote(s.Package))
		}
		if s.IsGo {
			b.WriteString(" :go")
		}
		if s.TestOnly {
			b.WriteString(" :test")
		}
		kids = append(kids, nodef(b.String()))
	}
	return nodef("syms", kids...)
}
