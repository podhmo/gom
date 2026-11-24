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
// inside a `/* ... */` block or a raw string is literal text, not a
// directive, and a `/*` inside a string or line comment opens no
// block (block comments do not nest, matching the lexer).
func ScanAllow(src []byte, path string) (allow map[Code]bool, diags List) {
	off := 0
	var lex allowLexState
	for _, line := range strings.Split(string(src), "\n") {
		start := off
		off += len(line) + 1
		trimmed, comment := lex.scan(line)
		if !comment || !strings.HasPrefix(trimmed, "//gom:allow") {
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
// do not nest, `//` comments end at the newline, quoted strings and
// chars honor backslash escapes within a line, and a backtick raw
// string may span lines.
type allowLexState struct {
	block bool // inside /* ... */
	raw   bool // inside `...`
}

// scan walks one line and reports the line's content trimmed plus
// whether that content is the start of a `//` comment (i.e. a
// possible directive). Lines inside a block comment or raw string
// report comment=false.
func (st *allowLexState) scan(line string) (trimmed string, comment bool) {
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
		c := line[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '"':
			i = skipQuoted(line, i)
			onlySpace = false
		case c == '\'':
			i = skipQuoted(line, i)
			onlySpace = false
		case c == '`':
			st.raw = true
			i++
			onlySpace = false
		case c == '/' && i+1 < len(line) && line[i+1] == '/':
			if onlySpace {
				return strings.TrimSpace(line[i:]), true
			}
			return strings.TrimSpace(line), false
		case c == '/' && i+1 < len(line) && line[i+1] == '*':
			st.block = true
			i += 2
			onlySpace = false
		default:
			i++
			onlySpace = false
		}
	}
	if st.block || st.raw {
		return strings.TrimSpace(line), false
	}
	return strings.TrimSpace(line), false
}

// skipQuoted advances past a `"`/`'` literal starting at i, honoring
// backslash escapes; it stops at the closer or the line end.
func skipQuoted(line string, i int) int {
	q := line[i]
	i++
	for i < len(line) {
		if line[i] == '\\' {
			i += 2
			continue
		}
		if line[i] == q {
			return i + 1
		}
		i++
	}
	return i
}
