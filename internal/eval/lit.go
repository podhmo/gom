package eval

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// isFloatCon reports whether t is the builtin Float constructor —
// a numeric literal can never carry a file-local `type Float` ADT,
// so the unqualified name check suffices here.
func isFloatCon(t types.Type) bool {
	c, ok := types.Chase(t).(*types.Con)
	return ok && c.Qual == "" && c.Name == "Float"
}

// litValue builds the value of a literal node. Text is the source
// lexeme (see doc.go for the quoting contract); failures are
// CodeInternal diagnostics since check fixes literal meaning. typ
// is the node's checked type — an integer literal spelled `1` but
// typed Float (`let m = 1` under a Float binder, a `1` pattern
// under a Float scrutinee) is a float *value*, matching codegen's
// per-literal `float64(...)` materialization.
func litValue(kind syntax.LitKind, text string, typ types.Type, sp diag.Span) Value {
	switch kind {
	case syntax.LitUnit:
		return &VUnit{}
	case syntax.LitBool:
		switch text {
		case "true":
			return &VBool{B: true}
		case "false":
			return &VBool{B: false}
		}
		failData(CodeInternal, "invalid bool literal", sp, "text", text)
	case syntax.LitInt:
		n, err := strconv.ParseInt(text, 0, 64)
		if err != nil {
			failData(CodeInternal, "invalid int literal", sp, "text", text)
		}
		if isFloatCon(typ) {
			return &VFloat{F: float64(n)}
		}
		return &VInt{N: n}
	case syntax.LitFloat:
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			failData(CodeInternal, "invalid float literal", sp, "text", text)
		}
		return &VFloat{F: f}
	case syntax.LitStr:
		return &VStr{S: unquoteString(text, sp)}
	case syntax.LitChar:
		return &VChar{R: unquoteChar(text, sp)}
	default:
		failData(CodeInternal, "unknown literal kind", sp, "kind", int(kind))
	}
	panic("unreachable")
}

// unquoteString decodes a string literal: a `"..."`-quoted lexeme is
// stripped of its quotes; a bare text is taken as-is. Gom escapes are
// decoded either way.
func unquoteString(text string, sp diag.Span) string {
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		text = text[1 : len(text)-1]
	}
	return unescape(text, sp)
}

// unquoteChar decodes a char literal to its code point; a bare
// single-rune text is taken as-is.
func unquoteChar(text string, sp diag.Span) rune {
	if len(text) >= 2 && text[0] == '\'' && text[len(text)-1] == '\'' {
		text = text[1 : len(text)-1]
	}
	s := unescape(text, sp)
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 || n != len(s) {
		failData(CodeInternal, "char literal is not a single code point", sp, "text", text)
	}
	return r
}

// unescape decodes Gom escape sequences (docs/grammar.ebnf):
// \n \t \r \0 \\ \' \" \$ \uXXXX \u{...}.
func unescape(s string, sp diag.Span) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c != '\\' {
			sb.WriteByte(c)
			i++
			continue
		}
		i++
		if i >= len(s) {
			failData(CodeInternal, "dangling escape at end of literal", sp, "text", s)
		}
		switch e := s[i]; e {
		case 'n':
			sb.WriteByte('\n')
			i++
		case 't':
			sb.WriteByte('\t')
			i++
		case 'r':
			sb.WriteByte('\r')
			i++
		case '0':
			sb.WriteByte(0)
			i++
		case '\\':
			sb.WriteByte('\\')
			i++
		case '\'':
			sb.WriteByte('\'')
			i++
		case '"':
			sb.WriteByte('"')
			i++
		case '$':
			sb.WriteByte('$')
			i++
		case 'u':
			i++
			if i < len(s) && s[i] == '{' {
				end := strings.IndexByte(s[i:], '}')
				if end < 0 {
					failData(CodeInternal, "unterminated \\u{...} escape", sp, "text", s)
				}
				sb.WriteRune(parseHexRune(s[i+1:i+end], s, sp))
				i += end + 1
			} else {
				if i+4 > len(s) {
					failData(CodeInternal, "truncated \\uXXXX escape", sp, "text", s)
				}
				sb.WriteRune(parseHexRune(s[i:i+4], s, sp))
				i += 4
			}
		default:
			failData(CodeInternal, "unknown escape sequence", sp,
				"escape", fmt.Sprintf("\\%c", e))
		}
	}
	return sb.String()
}

// parseHexRune parses hex digits as a code point.
func parseHexRune(hex, whole string, sp diag.Span) rune {
	n, err := strconv.ParseUint(hex, 16, 32)
	if err != nil || !utf8.ValidRune(rune(n)) {
		failData(CodeInternal, "invalid unicode escape", sp, "text", whole)
	}
	return rune(n)
}
