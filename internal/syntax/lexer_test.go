package syntax

import (
	"fmt"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

// dumpToks renders the post-NL-insertion token stream, one
// "kind@start-end" per token — the shape lexer tests compare.
func dumpToks(src string) string {
	lx := lex([]byte(src), "t.gom")
	var b strings.Builder
	for i, tok := range lx.toks {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%s@%d-%d", tok.kind, tok.start, tok.end)
	}
	return b.String()
}

// dumpTokKinds renders the post-NL-insertion token stream as bare
// kind names — the shape NL-insertion tests compare, where only the
// token sequence matters.
func dumpTokKinds(src string) string {
	lx := lex([]byte(src), "t.gom")
	var b strings.Builder
	for i, tok := range lx.toks {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(tok.kind.String())
	}
	return b.String()
}

func TestLexTokenKinds(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"identifiers and keywords",
			`foo Bar _ if match select go for try send default with as in`,
			`identifier@0-3 identifier@4-7 "_"@8-9 "if"@10-12 "match"@13-18 "select"@19-25 "go"@26-28 "for"@29-32 "try"@33-36 "send"@37-41 "default"@42-49 "with"@50-54 "as"@55-57 "in"@58-60 end of file@60-60`,
		},
		{
			"declaration keywords",
			`package fn let type import extern test else true false`,
			`"package"@0-7 "fn"@8-10 "let"@11-14 "type"@15-19 "import"@20-26 "extern"@27-33 "test"@34-38 "else"@39-43 "true"@44-48 "false"@49-54 end of file@54-54`,
		},
		{
			"delimiters and punctuation",
			`( ) [ ] { } , ; : . .. ~ @ = -> <- |`,
			`"("@0-1 ")"@2-3 "["@4-5 "]"@6-7 "{"@8-9 "}"@10-11 ","@12-13 ";"@14-15 ":"@16-17 "."@18-19 ".."@20-22 "~"@23-24 "@"@25-26 "="@27-28 "->"@29-31 "<-"@32-34 "|"@35-36 end of file@36-36`,
		},
		{
			"operators",
			`|> |?> >> << || && == != < <= > >= + - * / %`,
			`"|>"@0-2 "|?>"@3-6 ">>"@7-9 "<<"@10-12 "||"@13-15 "&&"@16-18 "=="@19-21 "!="@22-24 "<"@25-26 "<="@27-29 ">"@30-31 ">="@32-34 "+"@35-36 "-"@37-38 "*"@39-40 "/"@41-42 "%"@43-44 end of file@44-44`,
		},
		{
			"integer literals",
			`0 42 1_000 0x1F 0o17 0b101`,
			`integer literal@0-1 integer literal@2-4 integer literal@5-10 integer literal@11-15 integer literal@16-20 integer literal@21-26 end of file@26-26`,
		},
		{
			"float literals",
			`1.5 3.14e10 1e-3 0.5e+2`,
			`float literal@0-3 float literal@4-11 float literal@12-16 float literal@17-23 end of file@23-23`,
		},
		{
			"1.x is member access, 1. is not a float",
			`1.x`,
			`integer literal@0-1 "."@1-2 identifier@2-3 end of file@3-3`,
		},
		{
			"string literals",
			`"plain" "with\nesc\t" 'a' '\n'`,
			`string literal@0-7 string literal@8-21 character literal@22-25 character literal@26-30 end of file@30-30`,
		},
		{
			"interpolated string parts",
			`"hi ${name}!"`,
			`"@0-1 string text@1-4 "${"@4-6 identifier@6-10 "}"@10-11 string text@11-12 "@12-13 end of file@13-13`,
		},
		{
			"interpolation expression",
			`"${a + b}"`,
			`"@0-1 "${"@1-3 identifier@3-4 "+"@5-6 identifier@7-8 "}"@8-9 "@9-10 end of file@10-10`,
		},
		{
			"empty interpolated string",
			`"${}"`,
			`"@0-1 "${"@1-3 "}"@3-4 "@4-5 end of file@5-5`,
		},
		{
			"maximal munch: |> beats |, |?> beats |?",
			`| |> |?> ||`,
			`"|"@0-1 "|>"@2-4 "|?>"@5-8 "||"@9-11 end of file@11-11`,
		},
		{
			"maximal munch: << vs <, >> vs >",
			`< << > >> <= >=`,
			`"<"@0-1 "<<"@2-4 ">"@5-6 ">>"@7-9 "<="@10-12 ">="@13-15 end of file@15-15`,
		},
		{
			".. beats . then .",
			`.. .`,
			`".."@0-2 "."@3-4 end of file@4-4`,
		},
		{
			"<- vs < vs <=",
			`<- < <=`,
			`"<-"@0-2 "<"@3-4 "<="@5-7 end of file@7-7`,
		},
		{
			"-> vs -",
			`-> -`,
			`"->"@0-2 "-"@3-4 end of file@4-4`,
		},
		{
			"= vs ==",
			`= ==`,
			`"="@0-1 "=="@2-4 end of file@4-4`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dumpToks(tc.src); got != tc.want {
				t.Errorf("dumpToks(%q):\n got %s\nwant %s", tc.src, got, tc.want)
			}
		})
	}
}

// TestNLInsertion covers the NL rules of grammar.ebnf section 3:
// delimiter suppression (NL-1) and the can-end/line-start check
// (NL-2). Each case lists the token kinds in the emitted stream;
// "newline" marks the virtual NL token.
func TestNLInsertion(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"newline between two statements",
			"x = 1\ny = 2",
			`identifier "=" integer literal newline identifier "=" integer literal end of file`,
		},
		{
			"suppressed inside parens",
			"x = (a +\n  b) * c",
			`identifier "=" "(" identifier "+" identifier ")" "*" identifier end of file`,
		},
		{
			"suppressed inside brackets",
			"x = [1,\n     2]",
			`identifier "=" "[" integer literal "," integer literal "]" end of file`,
		},
		{
			"suppressed inside interpolation",
			`"${a +
b}"`,
			`" "${" identifier "+" identifier "}" " end of file`,
		},
		{
			"kept inside braces ({ itself ends nothing)",
			"{\n  a\n  b\n}",
			`"{" identifier newline identifier newline "}" end of file`,
		},
		{
			"no NL before a line-start operator",
			"x = f\n  |> g",
			`identifier "=" identifier "|>" identifier end of file`,
		},
		{
			"no NL after a non-can-end token",
			"x = f |>\n  g",
			`identifier "=" identifier "|>" identifier end of file`,
		},
		{
			"no NL after +",
			"x = a +\n  b",
			`identifier "=" identifier "+" identifier end of file`,
		},
		{
			"no NL before + (line-start)",
			"a\n+ b",
			`identifier "+" identifier end of file`,
		},
		{
			"no NL before | (line-start) — variant lines",
			"type X =\n  | A\n  | B",
			`"type" identifier "=" "|" identifier "|" identifier end of file`,
		},
		{
			"no NL before else",
			"if c {a}\nelse {b}",
			`"if" identifier "{" identifier "}" "else" "{" identifier "}" end of file`,
		},
		{
			"NL before -> (not line-start)",
			"p\n-> e",
			`identifier newline "->" identifier end of file`,
		},
		{
			"newline after a line comment",
			"x // comment\ny",
			`identifier newline identifier end of file`,
		},
		{
			"newline after a block comment",
			"x /* c */ y\nz",
			`identifier identifier newline identifier end of file`,
		},
		{
			"newline inside a block comment never surfaces",
			"x /* c\nc */ y\nz",
			`identifier identifier newline identifier end of file`,
		},
		{
			"multiple newlines collapse to one NL",
			"a\n\n\nb",
			`identifier newline identifier end of file`,
		},
		{
			"trailing newline at EOF",
			"x = 1\n",
			`identifier "=" integer literal newline end of file`,
		},
		{
			"a file ending without newline",
			"x = 1",
			`identifier "=" integer literal end of file`,
		},
		{
			"no NL after ;",
			"a; b",
			`identifier ";" identifier end of file`,
		},
		{
			"no NL after { (not can-end), NL before }",
			"x = {\n  a\n}",
			`identifier "=" "{" identifier newline "}" end of file`,
		},
		{
			"NL suppressed before } and after ,",
			"{ a,\n  b }",
			`"{" identifier "," identifier "}" end of file`,
		},
		{
			"; itself separates, so no NL is needed",
			"a;\nb",
			`identifier ";" identifier end of file`,
		},
		{
			"no NL before && (line-start)",
			"a &&\nb",
			`identifier "&&" identifier end of file`,
		},
		{
			"no NL before < (line-start)",
			"a\n< b",
			`identifier "<" identifier end of file`,
		},
		{
			"NL between two idents",
			"a\nb",
			`identifier newline identifier end of file`,
		},
		{
			"no NL before . — member access continues? no: . is not line-start, but NL before . fires",
			"a\n.b",
			`identifier newline "." identifier end of file`,
		},
		{
			"no NL inside ()",
			"(\na\n)",
			`"(" identifier ")" end of file`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dumpTokKinds(tc.src); got != tc.want {
				t.Errorf("dumpTokKinds(%q):\n got %s\nwant %s", tc.src, got, tc.want)
			}
		})
	}
}

func TestLexerTrivia(t *testing.T) {
	src := `// leading
package m

let x = 1 // tail

/* block
   comment */
let y = 2
`
	lx := lex([]byte(src), "t.gom")
	if len(lx.comments) != 3 {
		t.Fatalf("comments = %d, want 3", len(lx.comments))
	}
	for i, want := range []struct {
		kind CommentKind
		text string
	}{
		{CommentLine, " leading"},
		{CommentLine, " tail"},
		{CommentBlock, " block\n   comment "},
	} {
		c := lx.comments[i]
		if c.Kind != want.kind || c.Text != want.text {
			t.Errorf("comments[%d] = %s %q, want %s %q", i, c.Kind, c.Text, want.kind, want.text)
		}
	}
	if len(lx.blanks) != 2 {
		t.Fatalf("blanks = %d, want 2", len(lx.blanks))
	}
	for _, b := range lx.blanks {
		if b.IsZero() {
			t.Errorf("zero-width blank span: %v", b)
		}
	}
}

// TestLexerDiagnostics covers the lexical error codes.
func TestLexerDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, src, code, msgSub string
	}{
		{"unterminated string at newline", `"abc`, "E0102", "unterminated string literal"},
		{"unterminated string at EOF", `"abc`, "E0102", "unterminated string literal"},
		{"unterminated interp at EOF", `"${x`, "E0102", "unterminated string"},
		{"unterminated block comment", `/* c`, "E0103", "unterminated block comment"},
		{"bad escape", `"\q"`, "E0104", "invalid escape sequence"},
		{"bad escape at EOF", `"\`, "E0104", "invalid escape sequence"},
		{"bad unicode escape", `"\u{ZZZ}"`, "E0104", "invalid escape sequence"},
		{"char bad escape", `'\q'`, "E0104", "invalid escape sequence"},
		{"unterminated char", `'a`, "E0101", "character literal"},
		{"empty char", `''`, "E0101", "empty character literal"},
		{"multi char", `'ab'`, "E0101", "single character"},
		{"invalid digit hex", `0xZZ`, "E0101", "hexadecimal"},
		{"invalid digit octal", `0o89`, "E0101", "invalid digit"},
		{"invalid digit binary", `0b12`, "E0101", "invalid digit"},
		{"underscore before hex digits", `0x_1`, "E0101", "malformed hexadecimal"},
		{"underscore before octal digits", `0o_7`, "E0101", "malformed octal"},
		{"underscore before binary digits", `0b_1`, "E0101", "malformed binary"},
		{"bare & is not a token", `a & b`, "E0101", "not a Gom token"},
		{"bare ! is not a token", `a ! b`, "E0101", "not a Gom token"},
		{"? is not a token", `a ? b`, "E0101", "not a Gom token"},
		{"# is not a token", `#`, "E0101", "not a Gom token"},
		{"invalid UTF-8 in string", "\"a\xebx\"", "E0105", "invalid UTF-8 byte"},
		{"invalid UTF-8 in resumed string", "\"${x}\xeb\"", "E0105", "invalid UTF-8 byte"},
		{"NUL in string", "\"a\x00b\"", "E0105", "NUL byte"},
		{"invalid UTF-8 in char", "'\xeb'", "E0105", "invalid UTF-8 byte"},
		{"NUL in char", "'\x00'", "E0105", "NUL byte"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lx := lex([]byte(tc.src), "t.gom")
			if len(lx.diags) == 0 {
				t.Fatalf("no diagnostics for %q", tc.src)
			}
			found := false
			for _, d := range lx.diags {
				if string(d.Code) == tc.code && strings.Contains(d.Message, tc.msgSub) {
					found = true
				}
			}
			if !found {
				t.Errorf("no %s containing %q in %v", tc.code, tc.msgSub, lx.diags)
			}
		})
	}
}

// TestLexerE0105OncePerChunk pins the deduplication of invalid-byte
// diagnostics: checkLitByte reports at most once per literal chunk,
// and a "${...}" boundary starts a fresh chunk — so two bad bytes in
// one chunk produce ONE E0105 while a bad byte on each side of an
// interpolation produces two.
func TestLexerE0105OncePerChunk(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"two bad bytes one chunk", "\"\xeb\xeb\"", 1},
		{"bad byte each side of interp", "\"a\xeb${x}b\xff\"", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lx := lex([]byte(tc.src), "t.gom")
			got := 0
			for _, d := range lx.diags {
				if d.Code == "E0105" {
					got++
				}
			}
			if got != tc.want {
				t.Errorf("E0105 count = %d, want %d (diags: %v)", got, tc.want, lx.diags)
			}
		})
	}
}

// TestLexerDiagSpans checks that diagnostics point at the bytes of
// the offending construct.
func TestLexerDiagSpans(t *testing.T) {
	lx := lex([]byte(`"abc`), "t.gom")
	if len(lx.diags) != 1 {
		t.Fatalf("diags = %v", lx.diags)
	}
	d := lx.diags[0]
	if d.Span.Start != 0 || d.Span.End != 4 {
		t.Errorf("unterminated string span = [%d,%d), want [0,4)", d.Span.Start, d.Span.End)
	}
}

// TestLexerResumedStringDiagSpan pins the regression where an
// unterminated string resumed after a "}" reported E0102 at a
// -1 offset: the error must point at the string's opening quote.
func TestLexerResumedStringDiagSpan(t *testing.T) {
	lx := lex([]byte(`x = "${}0`), "t.gom")
	var e0102 *diag.Diagnostic
	for i := range lx.diags {
		if lx.diags[i].Code == "E0102" {
			e0102 = &lx.diags[i]
		}
	}
	if e0102 == nil {
		t.Fatalf("no E0102 in %v", lx.diags)
	}
	// The string opens at offset 4 and the file ends at 9.
	if e0102.Span.Start != 4 || e0102.Span.End != 9 {
		t.Errorf("resumed unterminated string span = [%d,%d), want [4,9)",
			e0102.Span.Start, e0102.Span.End)
	}
}
