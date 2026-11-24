package diag

import "strings"

// AllowDirective scans .gom source text for `//gom:allow <code>...`
// directives (spec section 12): each line names the diagnostic codes
// the file permits to drop from error to warning. It returns the set
// of relaxable codes the file allows plus one W0003 diagnostic per
// unusable entry — an unknown code, a registered error code that is
// not relaxable, or a directive without codes. An allowed error code
// demotes to a warning; an allowed warning code is silenced entirely
// (warning opt-out — warnings have no lower severity to demote to).
// Unusable lines report instead of silently doing nothing.
//
// The scan is textual, like the `//gom:noescape` reader: a stale or
// misplaced comment must not break the parse, so lines are matched on
// the trimmed "//gom:allow " prefix regardless of position — except
// that comment and string structure is honored: a `//gom:allow` line
// inside a `/* ... */` block, a raw string, or a quoted string's
// `${ }` interpolation is literal text, not a directive, and a `/*`
// inside a string or line comment opens no block (block comments do
// not nest, matching the lexer).
func ScanAllow(src []byte, path string) (allow map[Code]bool, diags List) {
	off := 0
	var lex allowLexState
	for _, line := range strings.Split(string(src), "\n") {
		start := off
		off += len(line) + 1
		trimmed, ok := lex.scan(line)
		if !ok || !strings.HasPrefix(trimmed, "//gom:allow") {
			continue
		}
		rest := trimmed[len("//gom:allow"):]
		if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
			// `//gom:allowE0301` is not a directive but its intent is
			// obvious — warn rather than demote anything on a line
			// that only looks like the directive.
			diags.Add(Warning("W0003", "malformed `//gom:allow` directive: expected whitespace after `allow`").
				At(Span{File: path, Start: start, End: start + len(line)}))
			continue
		}
		rest = strings.TrimSpace(rest)
		if rest == "" {
			diags.Add(Warning("W0003", "`//gom:allow` needs at least one diagnostic code").
				At(Span{File: path, Start: start, End: start + len(line)}))
			continue
		}
		for _, f := range strings.Fields(rest) {
			c := Code(f)
			info, ok := LookupCode(c)
			switch {
			case !ok:
				diags.Add(Warning("W0003", "unknown diagnostic code in `//gom:allow`: "+f).
					At(Span{File: path, Start: start, End: start + len(line)}).
					Set("code", f))
			case info.Severity == SeverityError && !info.Relaxable:
				diags.Add(Warning("W0003", "`//gom:allow` cannot demote "+f+": the code is not relaxable").
					At(Span{File: path, Start: start, End: start + len(line)}).
					Set("code", f))
			default:
				if allow == nil {
					allow = map[Code]bool{}
				}
				allow[c] = true
			}
		}
	}
	return allow, diags
}

// DemoteAllowed applies the directive's effect to ds and returns the
// filtered list. An allowed error demotes to a warning — it keeps its
// code and span, so it still reports (a demoted E0301 is never
// silent). An allowed warning is dropped entirely — a warning has no
// lower rung, so opt-out is silence (spec section 12). allows maps a
// file path to its `//gom:allow` code set.
func DemoteAllowed(ds List, allows map[string]map[Code]bool) List {
	out := ds[:0]
	for _, d := range ds {
		if allows[d.Span.File][d.Code] {
			if d.Severity == SeverityError {
				d.Severity = SeverityWarning
			} else {
				continue
			}
		}
		out = append(out, d)
	}
	return out
}

// allowLexState carries the comment/string context across lines so a
// directive-looking line inside a block comment or raw string is not
// read as a directive. It mirrors the lexer's rules: block comments
// do not nest, `//` comments end at the newline, `"`/`'` literals
// honor backslash escapes and `${ }` interpolation (which nests the
// code scan), and a backtick raw string may span lines.
type allowLexState struct {
	block  bool            // inside /* ... */
	raw    bool            // inside `...`
	frames []allowStrFrame // open string literals, innermost last
}

// allowStrFrame is one open `"`/`'` literal: depth 0 is the string
// body, depth ≥ 1 is inside its `${ }` interpolation code.
type allowStrFrame struct {
	quote byte
	depth int
}

// scan walks one line and reports the line's directive comment text
// when the line starts (modulo whitespace) with a `//` comment in
// code context. Anything else — a line inside a block comment, a raw
// or quoted string (bodies and interpolations alike), or a comment
// not starting the line — reports ok=false.
func (st *allowLexState) scan(line string) (text string, ok bool) {
	i := 0
	onlySpace := true
	for i < len(line) {
		if st.block {
			if line[i] == '*' && i+1 < len(line) && line[i+1] == '/' {
				st.block = false
				i += 2
				continue
			}
			i++
			continue
		}
		if st.raw {
			if line[i] == '`' {
				st.raw = false
			}
			i++
			continue
		}
		if n := len(st.frames); n > 0 && st.frames[n-1].depth == 0 {
			// Inside a string body: `\` escapes, the frame's own
			// quote closes it, `${` opens interpolation code.
			top := &st.frames[n-1]
			c := line[i]
			switch {
			case c == '\\':
				i += 2
			case c == top.quote:
				st.frames = st.frames[:n-1]
				i++
			case c == '$' && i+1 < len(line) && line[i+1] == '{':
				top.depth = 1
				i += 2
			default:
				i++
			}
			continue
		}
		// Code context — top level or inside a `${ }` interpolation.
		c := line[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '"' || c == '\'':
			st.frames = append(st.frames, allowStrFrame{quote: c})
			i++
			onlySpace = false
		case c == '`':
			st.raw = true
			i++
			onlySpace = false
		case c == '/' && i+1 < len(line) && line[i+1] == '/':
			if onlySpace && len(st.frames) == 0 {
				return strings.TrimSpace(line[i:]), true
			}
			// A `//` anywhere else still eats the rest of the line.
			return "", false
		case c == '/' && i+1 < len(line) && line[i+1] == '*':
			st.block = true
			i += 2
			onlySpace = false
		case c == '{' && len(st.frames) > 0:
			st.frames[len(st.frames)-1].depth++
			i++
			onlySpace = false
		case c == '}' && len(st.frames) > 0:
			st.frames[len(st.frames)-1].depth--
			i++
			onlySpace = false
		default:
			i++
			onlySpace = false
		}
	}
	// Quoted literals cannot span lines — an unterminated string or
	// char is a lex error the lexer recovers from at the newline
	// (lexer.go scanString), so the next line scans as plain code
	// again. Only backtick raw strings and block comments carry.
	st.frames = st.frames[:0]
	return "", false
}
