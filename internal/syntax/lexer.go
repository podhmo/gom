package syntax

import (
	"fmt"
	"unicode/utf8"

	"github.com/podhmo/gom/internal/diag"
)

// This file implements the lexer: phase 1 of the compiler
// (docs/design.md section 2). It scans the source into raw tokens,
// records trivia (comments and blank lines) for File, emits the
// lexical diagnostics E0101-E0104, and finishes by inserting the
// virtual NL tokens of docs/grammar.ebnf section 3.
//
// NL insertion is a second pass over the raw token stream: the scan
// keeps a delimiter stack — '(', '[', '{' and '${' — and emits a
// newline marker only when a physical '\n' occurs while the
// innermost delimiter is '{' or the stack is empty (NL-1). The pass
// then keeps a marker iff PREV is in can-end and NEXT is not in
// line-start (NL-2), so the parser never re-checks the rules.

// delimKind identifies an entry of the delimiter stack. delimInterp
// is the "${" of an interpolated string: it suppresses newlines like
// '(' and additionally means "a '}' here resumes the string".
type delimKind uint8

const (
	delimParen  delimKind = iota + 1 // (
	delimBrack                       // [
	delimBrace                       // {
	delimInterp                      // "${" inside a string literal
)

// delim is one delimiter-stack entry. strOpen records the start of
// the enclosing string for delimInterp entries so that an
// unterminated interpolated string can be located at EOF.
type delim struct {
	kind    delimKind
	strOpen int // start of the enclosing string (delimInterp only)
}

// commentMeta carries the line information the parser needs to
// attach /// doc comments to declarations. It is recorded per
// *Comment in the same order.
type commentMeta struct {
	startLine int  // 1-based line of the comment's first byte
	endLine   int  // 1-based line of its last byte
	lineOnly  bool // nothing but whitespace precedes it on its start line
}

// lexResult is everything the lexer extracts from one source.
type lexResult struct {
	toks     []token       // real tokens, NL inserted, EOF-terminated
	comments []*Comment    // trivia, in source order
	meta     []commentMeta // parallel to comments
	blanks   []diag.Span   // one span per blank line
	diags    diag.List
}

// lexer is the raw scanner. It produces tokens plus newline markers
// (tokNewline); insertNL turns the markers into tokNL afterwards.
type lexer struct {
	src  []byte
	file string

	pos int // current byte offset

	toks     []token
	comments []*Comment
	meta     []commentMeta
	blanks   []diag.Span
	diags    diag.List

	stack     []delim // open delimiters, innermost last
	resumeStr bool    // next step continues an interpolated string

	// resumeStrOpen is the opening-quote offset of the interpolated
	// string a "}" just returned to (the strOpen of the popped
	// delimInterp). scanString uses it to locate diagnostics when it
	// resumes — the "-1" open sentinel has no position of its own.
	resumeStrOpen int

	line         int  // current 1-based line
	lineStart    int  // byte offset of the current line's start
	lineHasThing bool // a token or comment appeared on this line
}

// lex scans src and returns tokens with NL inserted, trivia, and
// diagnostics. It is the entire first phase: the parser consumes
// only its output.
func lex(src []byte, file string) lexResult {
	l := &lexer{src: src, file: file, line: 1}
	l.scanAll()
	l.closeUnterminated()
	l.emit(tokEOF, len(src), len(src))
	return lexResult{
		toks:     insertNL(l.toks),
		comments: l.comments,
		meta:     l.meta,
		blanks:   l.blanks,
		diags:    l.diags,
	}
}

// emit appends a token covering [start, end).
func (l *lexer) emit(kind tokenKind, start, end int) {
	l.toks = append(l.toks, token{kind: kind, start: start, end: end})
	if kind != tokNewline {
		l.lineHasThing = true
	}
}

// err appends a syntax diagnostic at [start, end).
func (l *lexer) err(start, end int, code diag.Code, msg string) {
	l.diags.Add(diag.Error(code, msg).At(diag.SpanIn(l.file, start, end)))
}

// innermost returns the innermost open delimiter kind, or 0 when the
// stack is empty.
func (l *lexer) innermost() delimKind {
	if n := len(l.stack); n > 0 {
		return l.stack[n-1].kind
	}
	return 0
}

// pop removes the innermost delimiter (caller checked the kind).
func (l *lexer) pop() { l.stack = l.stack[:len(l.stack)-1] }

// scanAll is the main loop over the source bytes.
func (l *lexer) scanAll() {
	for l.pos < len(l.src) {
		if l.resumeStr {
			l.resumeStr = false
			l.scanString(-1)
			continue
		}
		l.step()
	}
}

// step dispatches on the byte at l.pos in normal (non-string) mode.
func (l *lexer) step() {
	pos := l.pos
	b := l.src[pos]
	switch {
	case b == ' ' || b == '\t' || b == '\r':
		l.pos++
	case b == '\n':
		l.newline()
	case b == '/' && l.peek(1) == '/':
		l.lineComment()
	case b == '/' && l.peek(1) == '*':
		l.blockComment()
	case b == '"':
		l.scanString(pos)
	case b == '\'':
		l.scanChar()
	case isDigit(b):
		l.scanNumber()
	case isLower(b) || b == '_':
		l.scanIdent()
	case isUpper(b):
		l.pos++
		for l.pos < len(l.src) && isIdentByte(l.src[l.pos]) {
			l.pos++
		}
		l.emit(tokUIdent, pos, l.pos)
	default:
		l.stepOp()
	}
}

// newline handles a physical '\n': records a blank line when the
// line held nothing but whitespace, then emits a newline marker
// when the innermost delimiter allows a separator (NL-1).
func (l *lexer) newline() {
	if !l.lineHasThing {
		// The line contained only whitespace: one blank-line span
		// covering its bytes (zero-width for a truly empty line).
		l.blanks = append(l.blanks, diag.SpanIn(l.file, l.lineStart, l.pos))
	}
	pos := l.pos
	l.pos++
	l.line++
	l.lineStart = l.pos
	l.lineHasThing = false
	if d := l.innermost(); d == 0 || d == delimBrace {
		l.toks = append(l.toks, token{kind: tokNewline, start: pos, end: l.pos})
	}
}

// peek returns the byte at l.pos+i, or 0 past the end.
func (l *lexer) peek(i int) byte {
	if l.pos+i < len(l.src) {
		return l.src[l.pos+i]
	}
	return 0
}

// ------------------------------------------------------------------
// comments (trivia)

// lineComment scans '//' or '///' to the end of the line (the '\n'
// itself is handled by the main loop).
func (l *lexer) lineComment() {
	start := l.pos
	l.pos += 2
	kind := CommentLine
	if l.peek(0) == '/' && l.peek(1) != '/' {
		// '///' is a doc comment; '////' and beyond stay line comments.
		kind = CommentDoc
		l.pos++
	}
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.pos++
	}
	l.addComment(kind, start, l.pos)
}

// blockComment scans '/*' through the matching '*/'. Comments do
// not nest. Newlines inside count for line tracking but never
// produce NL candidates (NL-3).
func (l *lexer) blockComment() {
	start := l.pos
	l.pos += 2
	for l.pos < len(l.src) {
		if l.src[l.pos] == '*' && l.peek(1) == '/' {
			l.pos += 2
			l.addComment(CommentBlock, start, l.pos)
			return
		}
		if l.src[l.pos] == '\n' {
			// A line inside a block comment holds comment text, so
			// it is never blank.
			l.line++
			l.lineStart = l.pos + 1
			l.lineHasThing = true
		}
		l.pos++
	}
	l.err(start, len(l.src), "E0103", "unterminated block comment")
	l.addComment(CommentBlock, start, len(l.src))
}

// addComment records one comment as file trivia.
func (l *lexer) addComment(kind CommentKind, start, end int) {
	text := string(l.src[start:end])
	// Drop the marker itself: "//", "///" or "/* ... */".
	switch kind {
	case CommentDoc:
		text = text[3:]
	case CommentLine:
		text = text[2:]
	case CommentBlock:
		text = text[2:]
		if n := len(text); n >= 2 && text[n-2:] == "*/" {
			text = text[:n-2]
		}
	}
	l.comments = append(l.comments, &Comment{
		Sp:   diag.SpanIn(l.file, start, end),
		Kind: kind,
		Text: text,
	})
	l.meta = append(l.meta, commentMeta{
		startLine: l.lineOf(start),
		endLine:   l.line,
		lineOnly:  !l.lineHasThing,
	})
	l.lineHasThing = true
}

// lineOf returns the 1-based line containing byte offset off.
func (l *lexer) lineOf(off int) int {
	line := 1
	for _, b := range l.src[:off] {
		if b == '\n' {
			line++
		}
	}
	return line
}

// ------------------------------------------------------------------
// strings and characters

// scanString scans a string literal. open is the byte offset of the
// opening '"' for a fresh string, or -1 when resuming after a "}"
// that closed "${" (the string is already in parts mode then).
//
// A plain string produces one tokString covering the quotes. A
// string containing "${" produces tokStrOpen, then alternating
// tokStrText / tokInterpStart segments; the expression inside ${...}
// is scanned in normal token mode and closed by '}' as
// tokInterpEnd, at which point scanString resumes for the remainder
// and ends with tokStrClose.
func (l *lexer) scanString(open int) {
	resume := open < 0
	if resume {
		// The string's opening quote was recorded when "}" closed
		// the interpolation — diagnostics keep pointing at the
		// string, not at a negative sentinel offset.
		open = l.resumeStrOpen
	} else {
		open = l.pos
		l.pos++ // consume '"'
	}
	chunkStart := l.pos // start of the pending literal chunk
	interp := resume    // resumed strings already emitted tokStrOpen
	badByte := false    // E0105 reported at most once per chunk
	for l.pos < len(l.src) {
		b := l.src[l.pos]
		switch {
		case b == '"':
			end := l.pos + 1
			if !interp {
				l.emit(tokString, open, end)
			} else {
				if l.pos > chunkStart {
					l.emit(tokStrText, chunkStart, l.pos)
				}
				l.emit(tokStrClose, l.pos, end)
			}
			l.pos = end
			return
		case b == '\n':
			l.err(open, l.pos+1, "E0102", "unterminated string literal")
			if !interp {
				l.emit(tokString, open, l.pos)
			} else {
				if l.pos > chunkStart {
					l.emit(tokStrText, chunkStart, l.pos)
				}
				l.emit(tokStrClose, l.pos, l.pos)
			}
			return // do not consume the '\n'
		case b == '\\':
			l.scanEscape()
		case b == '$' && l.peek(1) == '{':
			if !interp {
				l.emit(tokStrOpen, open, open+1)
				interp = true
			}
			if l.pos > chunkStart {
				l.emit(tokStrText, chunkStart, l.pos)
			}
			l.emit(tokInterpStart, l.pos, l.pos+2)
			l.stack = append(l.stack, delim{kind: delimInterp, strOpen: open})
			l.pos += 2
			return // back to normal token mode inside ${...}
		default:
			l.pos += l.checkLitByte(&badByte, "string")
		}
	}
	// EOF inside the string.
	l.err(open, len(l.src), "E0102", "unterminated string literal")
	if !interp {
		l.emit(tokString, open, len(l.src))
	} else {
		if len(l.src) > chunkStart {
			l.emit(tokStrText, chunkStart, len(l.src))
		}
		l.emit(tokStrClose, len(l.src), len(l.src))
	}
}

// checkLitByte decodes the rune at l.pos inside a string or
// character literal and reports E0105 when it is not valid UTF-8 or
// is the NUL byte — both would pass through to the generated Go
// verbatim and fail gofmt there ("illegal UTF-8 encoding",
// "illegal character NUL"), so the literal is rejected here
// instead. reported caps the diagnostic at one per literal chunk; it
// returns the decoded rune's width in bytes.
func (l *lexer) checkLitByte(reported *bool, what string) int {
	r, size := utf8.DecodeRune(l.src[l.pos:])
	if *reported {
		return size
	}
	switch {
	case r == utf8.RuneError && size == 1:
		l.err(l.pos, l.pos+1, "E0105",
			fmt.Sprintf("invalid UTF-8 byte in %s literal", what))
		*reported = true
	case r == 0:
		l.err(l.pos, l.pos+1, "E0105",
			fmt.Sprintf("the NUL byte is not allowed in a %s literal", what))
		*reported = true
	}
	return size
}

// scanEscape consumes one escape sequence starting at '\'. Invalid
// escapes produce E0104 and are consumed anyway so scanning can
// continue (grammar.ebnf section 2.3).
func (l *lexer) scanEscape() {
	start := l.pos
	l.pos++ // consume '\'
	if l.pos >= len(l.src) {
		l.err(start, l.pos, "E0104", "invalid escape sequence")
		return
	}
	c := l.src[l.pos]
	switch c {
	case 'n', 't', 'r', '0', '\\', '\'', '"', '$':
		l.pos++
		return
	case 'u':
		l.pos++
		if l.pos < len(l.src) && l.src[l.pos] == '{' {
			l.pos++
			value := 0
			n := 0
			for l.pos < len(l.src) && isHexDigit(l.src[l.pos]) {
				value = value*16 + hexVal(l.src[l.pos])
				l.pos++
				n++
			}
			if n == 0 || l.pos >= len(l.src) || l.src[l.pos] != '}' {
				l.err(start, l.pos, "E0104", "invalid escape sequence")
				return
			}
			l.pos++ // '}'
			if value > utf8.MaxRune {
				l.err(start, l.pos, "E0104",
					"escape sequence is outside the Unicode range")
			}
			return
		}
		for i := 0; i < 4; i++ {
			if l.pos >= len(l.src) || !isHexDigit(l.src[l.pos]) {
				l.err(start, l.pos, "E0104", "invalid escape sequence")
				return
			}
			l.pos++
		}
		return
	default:
		l.pos++
		l.err(start, l.pos, "E0104", "invalid escape sequence")
	}
}

// scanChar scans a character literal: one character or one escape
// between quotes (grammar.ebnf CHAR).
func (l *lexer) scanChar() {
	start := l.pos
	l.pos++ // consume '\''
	if l.pos >= len(l.src) || l.src[l.pos] == '\n' {
		l.err(start, l.pos, "E0101", "unterminated character literal")
		l.emit(tokChar, start, l.pos)
		return
	}
	switch l.src[l.pos] {
	case '\\':
		l.scanEscape()
	case '\'':
		l.err(start, l.pos+1, "E0101", "empty character literal")
	default:
		badByte := false
		l.pos += l.checkLitByte(&badByte, "character")
	}
	if l.pos < len(l.src) && l.src[l.pos] == '\'' {
		l.pos++
	} else {
		// Ran to a second character or the end: not one char.
		for l.pos < len(l.src) && l.src[l.pos] != '\'' && l.src[l.pos] != '\n' {
			l.pos++
		}
		if l.pos < len(l.src) && l.src[l.pos] == '\'' {
			l.pos++
		}
		l.err(start, l.pos, "E0101", "character literal must contain a single character")
	}
	l.emit(tokChar, start, l.pos)
}

// ------------------------------------------------------------------
// numbers and identifiers

// scanNumber scans INT and FLOAT per grammar.ebnf section 2.3:
// decimal, 0x/0o/0b bases, "_" digit separators, fractions and
// exponents. "1." and ".5" are deliberately not floats.
func (l *lexer) scanNumber() {
	start := l.pos
	if l.src[l.pos] == '0' && l.pos+1 < len(l.src) {
		var base string
		var valid func(byte) bool
		switch l.src[l.pos+1] {
		case 'x', 'X':
			base, valid = "hexadecimal", isHexDigit
		case 'o', 'O':
			base, valid = "octal", isOctDigit
		case 'b', 'B':
			base, valid = "binary", isBinDigit
		}
		if valid != nil {
			l.pos += 2
			l.scanBaseDigits(start, base, valid)
			l.emit(tokInt, start, l.pos)
			return
		}
	}
	isFloat := false
	l.scanDecDigits()
	// Fraction: '.' only counts when a digit follows, so "1.x" is
	// member access on 1 and "1..x" hits "..".
	if l.peek(0) == '.' && isDigit(l.peek(1)) {
		isFloat = true
		l.pos++
		l.scanDecDigits()
	}
	// Exponent.
	if c := l.peek(0); c == 'e' || c == 'E' {
		i := 1
		if l.peek(i) == '+' || l.peek(i) == '-' {
			i++
		}
		if isDigit(l.peek(i)) {
			isFloat = true
			l.pos += i
			l.scanDecDigits()
		}
	}
	if isFloat {
		l.emit(tokFloat, start, l.pos)
	} else {
		l.emit(tokInt, start, l.pos)
	}
}

// scanBaseDigits consumes the digits of a 0x/0o/0b literal,
// requiring at least one real digit before any "_" separator and
// flagging foreign digits.
func (l *lexer) scanBaseDigits(start int, base string, valid func(byte) bool) {
	ndigits := 0
	for l.pos < len(l.src) {
		b := l.src[l.pos]
		switch {
		case valid(b):
			l.pos++
			ndigits++
		case b == '_' && ndigits > 0 && l.pos+1 < len(l.src) && valid(l.src[l.pos+1]):
			l.pos++
		case isDigit(b):
			l.err(l.pos, l.pos+1, "E0101", "invalid digit for "+base+" literal")
			l.pos++
		default:
			goto done
		}
	}
done:
	if ndigits == 0 {
		l.err(start, l.pos, "E0101", "malformed "+base+" integer literal")
	}
}

// scanDecDigits consumes decimal digits and single "_" separators
// (each "_" must sit between digits).
func (l *lexer) scanDecDigits() {
	for l.pos < len(l.src) {
		b := l.src[l.pos]
		switch {
		case isDigit(b):
			l.pos++
		case b == '_' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1]):
			l.pos++
		default:
			return
		}
	}
}

// scanIdent scans a lowercase/"_" identifier, a keyword, or the "_"
// wildcard token.
func (l *lexer) scanIdent() {
	start := l.pos
	l.pos++
	for l.pos < len(l.src) && isIdentByte(l.src[l.pos]) {
		l.pos++
	}
	text := string(l.src[start:l.pos])
	if text == "_" {
		l.emit(tokUnder, start, l.pos)
		return
	}
	if kw, ok := keywords[text]; ok {
		l.emit(kw, start, l.pos)
		return
	}
	l.emit(tokIdent, start, l.pos)
}

// ------------------------------------------------------------------
// operators

// stepOp scans operators and punctuation with maximal munch
// (grammar.ebnf section 2.4).
func (l *lexer) stepOp() {
	pos := l.pos
	b := l.src[pos]
	two := func(k tokenKind) { l.emit(k, pos, pos+2); l.pos += 2 }
	three := func(k tokenKind) { l.emit(k, pos, pos+3); l.pos += 3 }
	switch b {
	case '(':
		l.stack = append(l.stack, delim{kind: delimParen})
		l.emit(tokLParen, pos, pos+1)
		l.pos++
	case '[':
		l.stack = append(l.stack, delim{kind: delimBrack})
		l.emit(tokLBrack, pos, pos+1)
		l.pos++
	case '{':
		l.stack = append(l.stack, delim{kind: delimBrace})
		l.emit(tokLBrace, pos, pos+1)
		l.pos++
	case ')':
		if l.innermost() == delimParen {
			l.pop()
		}
		l.emit(tokRParen, pos, pos+1)
		l.pos++
	case ']':
		if l.innermost() == delimBrack {
			l.pop()
		}
		l.emit(tokRBrack, pos, pos+1)
		l.pos++
	case '}':
		switch l.innermost() {
		case delimInterp:
			l.resumeStrOpen = l.stack[len(l.stack)-1].strOpen
			l.pop()
			l.emit(tokInterpEnd, pos, pos+1)
			l.pos++
			l.resumeStr = true // continue the enclosing string
		case delimBrace:
			l.pop()
			l.emit(tokRBrace, pos, pos+1)
			l.pos++
		default:
			// Mismatched close: emit it and let the parser complain.
			l.emit(tokRBrace, pos, pos+1)
			l.pos++
		}
	case ',':
		l.emit(tokComma, pos, pos+1)
		l.pos++
	case ';':
		l.emit(tokSemi, pos, pos+1)
		l.pos++
	case ':':
		l.emit(tokColon, pos, pos+1)
		l.pos++
	case '~':
		l.emit(tokTilde, pos, pos+1)
		l.pos++
	case '@':
		l.emit(tokAt, pos, pos+1)
		l.pos++
	case '.':
		if l.peek(1) == '.' {
			two(tokDotDot)
		} else {
			l.emit(tokDot, pos, pos+1)
			l.pos++
		}
	case '=':
		if l.peek(1) == '=' {
			two(tokEqEq)
		} else {
			l.emit(tokEq, pos, pos+1)
			l.pos++
		}
	case '-':
		if l.peek(1) == '>' {
			two(tokArrow)
		} else {
			l.emit(tokMinus, pos, pos+1)
			l.pos++
		}
	case '<':
		switch l.peek(1) {
		case '=':
			two(tokLe)
		case '-':
			two(tokLArrow)
		case '|':
			two(tokPipeBack)
		case '<':
			two(tokComposeB)
		default:
			l.emit(tokLt, pos, pos+1)
			l.pos++
		}
	case '>':
		switch l.peek(1) {
		case '=':
			two(tokGe)
		case '>':
			two(tokComposeF)
		default:
			l.emit(tokGt, pos, pos+1)
			l.pos++
		}
	case '|':
		switch {
		case l.peek(1) == '?' && l.peek(2) == '>':
			three(tokPipeTry)
		case l.peek(1) == '>':
			two(tokPipeFwd)
		case l.peek(1) == '|':
			two(tokOr)
		default:
			l.emit(tokBar, pos, pos+1)
			l.pos++
		}
	case '&':
		if l.peek(1) == '&' {
			two(tokAnd)
		} else {
			l.invalidChar(pos)
		}
	case '!':
		if l.peek(1) == '=' {
			two(tokNe)
		} else {
			l.invalidChar(pos)
		}
	case '+':
		l.emit(tokPlus, pos, pos+1)
		l.pos++
	case '*':
		l.emit(tokStar, pos, pos+1)
		l.pos++
	case '/':
		l.emit(tokSlash, pos, pos+1)
		l.pos++
	case '%':
		l.emit(tokPercent, pos, pos+1)
		l.pos++
	default:
		l.invalidChar(pos)
	}
}

// invalidChar reports an E0101 for a byte that cannot start any
// token and skips it (one rune for non-ASCII) so scanning proceeds.
func (l *lexer) invalidChar(pos int) {
	r, size := utf8.DecodeRune(l.src[pos:])
	l.err(pos, pos+size, "E0101",
		"unexpected character "+quoteRune(r)+"; it is not a Gom token")
	l.pos += size
	l.lineHasThing = true
}

// closeUnterminated finishes an interpolated string left open at
// EOF (the scanner sits inside "${" in token mode). One E0102 is
// reported at the enclosing string's opening quote and the closing
// tokens are synthesized so the parser's expectations succeed.
func (l *lexer) closeUnterminated() {
	for len(l.stack) > 0 {
		d := l.stack[len(l.stack)-1]
		l.pop()
		if d.kind == delimInterp {
			l.err(d.strOpen, len(l.src), "E0102",
				"unterminated string literal: \"${\" was never closed")
			l.emit(tokInterpEnd, len(l.src), len(l.src))
			l.emit(tokStrClose, len(l.src), len(l.src))
		}
	}
}

// ------------------------------------------------------------------
// NL insertion (docs/grammar.ebnf section 3)

// insertNL converts the raw newline markers to virtual NL tokens
// under NL-2: a marker survives iff the previous real token is in
// can-end and the next real token is not in line-start. A run of
// markers produces a single NL spanning the run. At EOF a pending
// marker produces one final NL.
func insertNL(raw []token) []token {
	out := make([]token, 0, len(raw))
	prev := tokEOF // kind of the last real token emitted
	for i := 0; i < len(raw); i++ {
		t := raw[i]
		if t.kind != tokNewline {
			out = append(out, t)
			prev = t.kind
			continue
		}
		// Collect the run of consecutive markers.
		j := i
		for j+1 < len(raw) && raw[j+1].kind == tokNewline {
			j++
		}
		next := raw[j+1].kind // first real token after the run (EOF always terminates)
		if prev.canEnd() && !next.lineStart() {
			out = append(out, token{kind: tokNL, start: t.start, end: raw[j].end})
		}
		i = j
	}
	return out
}

// ------------------------------------------------------------------
// character classes (grammar.ebnf section 9, ASCII only)

func isDigit(b byte) bool  { return b >= '0' && b <= '9' }
func isLower(b byte) bool  { return b >= 'a' && b <= 'z' }
func isUpper(b byte) bool  { return b >= 'A' && b <= 'Z' }
func isLetter(b byte) bool { return isLower(b) || isUpper(b) }

func isHexDigit(b byte) bool {
	return isDigit(b) || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}
func isOctDigit(b byte) bool { return b >= '0' && b <= '7' }
func isBinDigit(b byte) bool { return b == '0' || b == '1' }

// isIdentByte reports whether b may continue an identifier:
// letters, digits and "_" (grammar.ebnf ident, ASCII only).
func isIdentByte(b byte) bool { return isLetter(b) || isDigit(b) || b == '_' }

// hexVal is the value of a hex digit; callers check isHexDigit.
func hexVal(b byte) int {
	switch {
	case isDigit(b):
		return int(b - '0')
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10
	default:
		return int(b-'A') + 10
	}
}

// quoteRune renders a rune for a diagnostic message.
func quoteRune(r rune) string {
	if r == utf8.RuneError {
		return "'\\ufffd'"
	}
	switch r {
	case '\n':
		return "'\\n'"
	case '\t':
		return "'\\t'"
	default:
		return "'" + string(r) + "'"
	}
}
