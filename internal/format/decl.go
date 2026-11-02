package format

import (
	"bytes"
	"sort"
	"strings"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
)

// This file renders declarations and assembles the file: the package
// clause, the merged import group, and the declaration sequence with
// exactly one blank line between items (spec 1.7).

// fileDoc renders the whole file.
func (p *printer) fileDoc(f *syntax.File) *doc {
	// Items: non-import decls in source order plus one virtual item
	// for the merged import group at the first import's position.
	var imports []*syntax.ImportDecl
	var items []item
	for _, d := range f.Decls {
		if im, ok := d.(*syntax.ImportDecl); ok {
			imports = append(imports, im)
			continue
		}
		items = append(items, p.declItem(d))
	}
	if len(imports) > 0 {
		imp := p.importsItem(imports)
		pos := 0
		for pos < len(items) && items[pos].lo < imports[0].Sp.Start {
			pos++
		}
		items = append(items[:pos], append([]item{imp}, items[pos:]...)...)
	}

	pkgEnd := 0
	var pkg *doc
	if f.Package != nil {
		kw := bytes.LastIndex(p.src[:f.Package.Sp.Start], []byte("package"))
		mid := p.gapInline(kw+len("package"), f.Package.Sp.Start)
		pkg = concat(text("package"+mid+" "), text(f.Package.Name))
		pkgEnd = f.Package.Sp.End
	}
	gaps := p.collectGaps(pkgEnd, len(p.src), items)

	// Header comments before the package clause.
	var header []*cmt
	if f.Package != nil {
		kw := bytes.LastIndex(p.src[:f.Package.Sp.Start], []byte("package"))
		header = p.take(0, kw)
	}

	var lines []*doc
	for _, c := range header {
		lines = append(lines, text(c.render()))
	}
	if pkg != nil {
		lines = append(lines, concat(pkg, text(trailText(gaps[0].trail))))
	}
	gaps[0].trail = nil

	body := p.seqLines(gaps, items, seqSpec{blanks: false, lines: 2}, 0)
	if pkg != nil && len(body) > 0 {
		// One blank line between the package clause and the body,
		// matching the fixed blank between top-level decls.
		lines = append(lines, docNil)
	}
	lines = append(lines, body...)
	return joinLines(lines)
}

// declItem renders one declaration (not imports) as a collection
// item: doc comments and @test become lead lines.
func (p *printer) declItem(d syntax.Decl) item {
	var lead []string
	var dd *doc
	switch d := d.(type) {
	case *syntax.FnDecl:
		lead = append(lead, docLines(d.Doc)...)
		if d.TestOnly {
			lead = append(lead, "@test")
		}
		if d.TailRec {
			lead = append(lead, "@tailrec")
		}
		dd = p.fnDecl(d)
	case *syntax.LetDecl:
		lead = append(lead, docLines(d.Doc)...)
		if d.TestOnly {
			lead = append(lead, "@test")
		}
		dd = p.letDecl(d)
	case *syntax.TypeDecl:
		lead = append(lead, docLines(d.Doc)...)
		dd = p.typeDecl(d)
	case *syntax.ExternDecl:
		dd = p.externDecl(d)
	case *syntax.TestDecl:
		mid := p.gapInline(d.Sp.Start+len("test"), d.NameSp.Start)
		dd = concat(text("test"+pad(mid)+"\""+d.Name+"\" "), p.expr(d.Body, pAny, false))
	case *syntax.ImportDecl:
		panic("unreachable") // filtered by fileDoc
	default:
		panic("unreachable")
	}
	return item{lo: d.Span().Start, hi: d.Span().End, d: p.sweep(d.Span().Start, d.Span().End, dd), lead: lead}
}

// docLines renders a merged /// doc comment as one line per source
// line.
func docLines(c *syntax.Comment) []string {
	if c == nil {
		return nil
	}
	parts := strings.Split(c.Text, "\n")
	out := make([]string, len(parts))
	for i, s := range parts {
		out[i] = "///" + s
	}
	return out
}

// fnDecl renders "fn name params -> ret [= body]".
func (p *printer) fnDecl(d *syntax.FnDecl) *doc {
	var kids []*doc
	mid0 := p.gapInline(d.Sp.Start+len("fn"), d.Name.Sp.Start)
	kids = append(kids, text("fn"+pad(mid0)+d.Name.Name))
	anchor := d.Name.Sp.End
	for i := range d.Params {
		mid := p.gapInline(anchor, d.Params[i].Sp.Start)
		kids = append(kids, text(" "+mid), p.param(&d.Params[i]))
		anchor = d.Params[i].Sp.End
	}
	if d.Ret != nil {
		mid := p.gapInline(anchor, d.Ret.Span().Start)
		kids = append(kids, text(" ->"+mid+" "), p.typeExpr(d.Ret))
		anchor = d.Ret.Span().End
	}
	if d.Body == nil {
		return concat(kids...)
	}
	return concat(concat(kids...), p.eqBody(anchor, d.Body))
}

// letDecl renders "let pat [: T] [= value]".
func (p *printer) letDecl(d *syntax.LetDecl) *doc {
	mid0 := p.gapInline(d.Sp.Start+len("let"), d.Pat.Span().Start)
	kids := []*doc{text("let" + pad(mid0)), p.pat(d.Pat, ppOr)}
	anchor := d.Pat.Span().End
	if d.Type != nil {
		mid := p.gapInline(anchor, d.Type.Span().Start)
		kids = append(kids, text(":"+pad(mid)), p.typeExpr(d.Type))
		anchor = d.Type.Span().End
	}
	if d.Value == nil {
		return concat(kids...)
	}
	return concat(concat(kids...), p.eqBody(anchor, d.Value))
}

// typeDecl renders "type Name params [= body]".
func (p *printer) typeDecl(d *syntax.TypeDecl) *doc {
	var kids []*doc
	mid0 := p.gapInline(d.Sp.Start+len("type"), d.Name.Sp.Start)
	kids = append(kids, text("type"+pad(mid0)+d.Name.Name))
	anchor := d.Name.Sp.End
	for _, prm := range d.Params {
		mid := p.gapInline(anchor, prm.Sp.Start)
		kids = append(kids, text(" "+mid+prm.Name))
		anchor = prm.Sp.End
	}
	switch b := d.Body.(type) {
	case *syntax.AbstractBody:
		return concat(kids...)
	case *syntax.AliasBody:
		mid := p.gapInline(anchor, b.Type.Span().Start)
		return concat(concat(kids...), text(" ="+mid+" "), p.typeExpr(b.Type))
	case *syntax.RecordBody:
		mid := p.gapInline(anchor, b.Sp.Start)
		items := p.fieldTypeItems(b.Fields)
		return concat(concat(kids...), text(" ="+mid),
			p.braceSeq(text(" "), '{', '}', b.Sp.Start, b.Sp.End-1, nil, items,
				seqSpec{sep: ",", flatSep: ", ", trailSep: true, lines: 1}))
	case *syntax.SumBody:
		// The "=" gap claims comments through the first variant name —
		// including ones sitting on the first "|" line ("= | //c\nA").
		// They collapse to "= /* c */" so the leading-gap trail bucket
		// stays empty and the output is idempotent.
		hi := b.Sp.Start
		if len(b.Variants) > 0 {
			hi = b.Variants[0].Sp.Start
		}
		mid := p.gapInline(anchor, hi)
		return concat(concat(kids...), text(" ="+mid), p.sumBody(b, d.Doc))
	default:
		panic("unreachable")
	}
}

// fieldTypeItems wraps record-type fields "name: T" as items.
func (p *printer) fieldTypeItems(fields []syntax.Field) []item {
	items := make([]item, 0, len(fields))
	for i := range fields {
		f := &fields[i]
		var dd *doc
		switch {
		case f.Label != nil:
			mid := p.gapInline(f.Sp.Start, f.Type.Span().Start)
			dd = concat(text("(~"+f.Label.Name+":"+pad(mid)), p.typeExpr(f.Type), text(")"))
		case f.Name != nil:
			mid := p.gapInline(f.Name.Span().End, f.Type.Span().Start)
			dd = concat(text(f.Name.Name+":"+pad(mid)), p.typeExpr(f.Type))
		default:
			dd = p.typeAt(f.Type, tpAtom)
		}
		items = append(items, item{lo: f.Sp.Start, hi: f.Sp.End, d: p.sweep(f.Sp.Start, f.Sp.End, dd)})
	}
	return items
}

// sumBody renders a sum type's variants — one "| V ..." per line at
// +2 under "type X =", always broken (spec's own style). The parser
// may attach one doc comment to several nodes (the type decl AND
// multiple variants share the same span); each doc span prints at
// most once — the decl wins, then the first variant carrying it.
func (p *printer) sumBody(b *syntax.SumBody, declDoc *syntax.Comment) *doc {
	seen := map[diag.Span]bool{}
	if declDoc != nil {
		seen[declDoc.Sp] = true
	}
	items := make([]item, 0, len(b.Variants))
	for i := range b.Variants {
		v := &b.Variants[i]
		var lead []string
		if v.Doc != nil && !seen[v.Doc.Sp] {
			seen[v.Doc.Sp] = true
			lead = append(lead, docLines(v.Doc)...)
		}
		var kids []*doc
		kids = append(kids, text("| "+v.Name.Name))
		for j := range v.Params {
			f := &v.Params[j]
			mid := p.gapInline(v.Name.Span().End, f.Sp.Start)
			kids = append(kids, text(" "+mid), p.variantField(f))
		}
		items = append(items, item{lo: v.Sp.Start, hi: v.Sp.End,
			d: p.sweep(v.Sp.Start, v.Sp.End, concat(kids...)), lead: lead})
	}
	gaps := p.collectGaps(b.Sp.Start, b.Sp.End, items)
	return lazy(func(col, ind int) *doc {
		lines := p.seqLines(gaps, items, seqSpec{blanks: true, lines: 1}, ind+indentStep)
		return nest(indentStep, concat(docHard, joinLines(lines)))
	})
}

// variantField renders one payload slot: "T", "(name: T)" or
// "(~name: T)".
func (p *printer) variantField(f *syntax.Field) *doc {
	return p.sweep(f.Sp.Start, f.Sp.End, p.variantFieldRaw(f))
}

func (p *printer) variantFieldRaw(f *syntax.Field) *doc {
	switch {
	case f.Label != nil:
		mid := p.gapInline(f.Sp.Start, f.Type.Span().Start)
		return concat(text("(~"+f.Label.Name+":"+pad(mid)), p.typeExpr(f.Type), text(")"))
	case f.Name != nil:
		mid0 := p.gapInline(f.Sp.Start+1, f.Name.Span().Start)
		mid := p.gapInline(f.Name.Span().End, f.Type.Span().Start)
		return concat(text("("+mid0+f.Name.Name+":"+pad(mid)), p.typeExpr(f.Type), text(")"))
	default:
		return p.typeAt(f.Type, tpAtom)
	}
}

// externDecl renders "extern go "path" { entries }" — always broken.
func (p *printer) externDecl(d *syntax.ExternDecl) *doc {
	items := make([]item, 0, len(d.Entries))
	for _, e := range d.Entries {
		var dd *doc
		var lead []string
		switch e := e.(type) {
		case *syntax.ExternFn:
			lead = append(lead, docLines(e.Doc)...)
			var kids []*doc
			mid0 := p.gapInline(e.Sp.Start+len("fn"), e.Name.Sp.Start)
			kids = append(kids, text("fn"+pad(mid0)+e.Name.Name))
			anchor := e.Name.Sp.End
			for i := range e.Params {
				mid := p.gapInline(anchor, e.Params[i].Sp.Start)
				kids = append(kids, text(" "+mid), p.param(&e.Params[i]))
				anchor = e.Params[i].Sp.End
			}
			if e.Ret != nil {
				mid := p.gapInline(anchor, e.Ret.Span().Start)
				kids = append(kids, text(" ->"+mid+" "), p.typeExpr(e.Ret))
			}
			dd = concat(kids...)
		default:
			panic("unreachable")
		}
		items = append(items, item{lo: e.Span().Start, hi: e.Span().End, d: p.sweep(e.Span().Start, e.Span().End, dd), lead: lead})
	}
	brace := findByte(p.src, d.PathSp.End, firstLo(items, d.Sp.End), '{')
	if brace < 0 {
		brace = d.PathSp.End
	}
	mid := p.gapInline(d.Sp.Start+len("extern"), d.PathSp.Start)
	open := concat(text("extern" + pad(mid) + "go \"" + d.Path + "\" "))
	return p.braceSeq(open, '{', '}', brace, d.Sp.End-1, nil, items,
		seqSpec{blanks: true, always: true, lines: 1})
}

// ------------------------------------------------------------------
// imports
//

// impEntry is one import plus the comments that move with it.
type impEntry struct {
	d     *syntax.ImportDecl
	lead  []*cmt // own-line comments directly above inside the import run
	trail []*cmt // comments on the import's own line
}

// importsItem merges all imports into one sorted group occupying the
// first import's slot in the declaration sequence.
func (p *printer) importsItem(imports []*syntax.ImportDecl) item {
	entries := make([]*impEntry, len(imports))
	for i, d := range imports {
		e := &impEntry{d: d}
		// Trailing comments on the import's own line.
		endLine := p.lineOf(max(d.Sp.End-1, d.Sp.Start))
		_, lineEnd := p.li.LineBounds(endLine)
		e.trail = append(e.trail, p.take(d.Sp.End, lineEnd)...)
		// Leading comments: own-line comments on the lines directly
		// above, walking up while each line is comment-only.
		if i > 0 {
			line := p.lineOf(d.Sp.Start) - 1
			for line >= 1 {
				ls, le := p.li.LineBounds(line)
				found := false
				for _, c := range p.peek(ls, le) {
					if c.lineOnly && c.startLine == line {
						c.used = true
						e.lead = append([]*cmt{c}, e.lead...)
						found = true
					}
				}
				if !found {
					break
				}
				line--
			}
		}
		entries[i] = e
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i].d, entries[j].d
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Go != b.Go {
			return a.Go
		}
		return a.Name.Name < b.Name.Name
	})

	var kids []*doc
	for i, e := range entries {
		if i > 0 {
			kids = append(kids, docHard)
		}
		for _, c := range e.lead {
			kids = append(kids, text(c.render()), docHard)
		}
		var b strings.Builder
		b.WriteString("import" + pad(p.gapInline(e.d.Sp.Start+len("import"), e.d.PathSp.Start)))
		if e.d.Go {
			b.WriteString("go ")
		}
		if e.d.Alias {
			b.WriteString(e.d.Name.Name + " ")
		}
		b.WriteString("\"" + e.d.Path + "\"")
		b.WriteString(trailText(e.trail))
		kids = append(kids, text(b.String()))
	}
	return item{
		lo: imports[0].Sp.Start,
		hi: imports[0].Sp.End,
		d:  concat(kids...),
	}
}
