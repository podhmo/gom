package syntax

import "github.com/podhmo/gom/internal/diag"

// Canonical renders src's token stream — token kinds plus the
// literal text of spelling-bearing tokens — with comments, blank
// lines and positions stripped. Two sources differing only in
// trivia produce identical output, so a hash of Canonical(src) is
// the normalized-source fingerprint the incremental build cache
// keys on (docs/design.md section 5). Tokens inside the spans of
// `drop` are omitted: the cache excludes `test` and `@test`
// declarations by passing their spans so a test-only edit does not
// invalidate a package's `gom build` outputs.
func Canonical(src []byte, drop []diag.Span) []byte {
	r := lex(src, "")
	var b []byte
tok:
	for _, t := range r.toks {
		for _, sp := range drop {
			if t.start >= sp.Start && t.end <= sp.End {
				continue tok
			}
		}
		b = append(b, byte(t.kind))
		switch t.kind {
		case tokIdent, tokUIdent, tokInt, tokFloat, tokChar, tokString, tokStrText:
			b = append(b, src[t.start:t.end]...)
			b = append(b, 0)
		}
	}
	return b
}

// TestDeclSpans returns the spans of src's `test` and `@test`
// declarations — the drop list Canonical's build-cache use wants.
// A file that does not parse still gets a canonical token stream;
// its decls just cannot be identified, so no spans are dropped.
func TestDeclSpans(src []byte, file string) []diag.Span {
	f, _ := Parse(src, file)
	if f == nil {
		return nil
	}
	var spans []diag.Span
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *FnDecl:
			if d.TestOnly {
				spans = append(spans, d.Sp)
			}
		case *LetDecl:
			if d.TestOnly {
				spans = append(spans, d.Sp)
			}
		case *TestDecl:
			spans = append(spans, d.Sp)
		case *TypeDecl, *ImportDecl, *ExternDecl:
		default:
			panic("unreachable")
		}
	}
	return spans
}
